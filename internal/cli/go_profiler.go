package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

const goProfilerModule = "github.com/runtimeconditions/go-rc-profiler"

func linkedGoProfilerIdentity() goProfilerIdentity {
	identity := goProfilerIdentity{Name: "go-rc-profiler", Origin: "linked", Module: goProfilerModule, Version: "unknown"}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range build.Deps {
			if dependency.Path == goProfilerModule {
				if dependency.Replace != nil {
					dependency = dependency.Replace
				}
				identity.Version, identity.ModuleSum = dependency.Version, dependency.Sum
				break
			}
		}
	}
	return identity
}

type goProfilerAPI struct {
	Extract  func(string, extractor.GeneratedOptions) (map[string]any, error)
	Marshal  func(map[string]any) ([]byte, error)
	Validate func(context.Context, string, []string) ([]extensioncheck.ImportedGoPackage, error)
}

func linkedGoProfilerAPI() goProfilerAPI {
	return goProfilerAPI{
		Extract: extractor.ExtractGeneratedDir, Marshal: extractor.MarshalProfileYAML,
		Validate: extensioncheck.ValidateImportedGoPackages,
	}
}

type goGenerateOptions struct {
	Directory string
	Output    string
	Identity  extractor.GeneratedOptions
}

func newGoGenerateCommand(api goProfilerAPI) *cobra.Command {
	var options goGenerateOptions
	cmd := &cobra.Command{
		Use: "generate", Short: "Generate a validated profile from Go declarations",
		Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return generateGoProfile(cmd.Context(), options, api, cmd.OutOrStdout())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&options.Directory, "dir", ".", "directory containing Go source declarations")
	flags.StringVar(&options.Identity.Name, "name", "", "profile metadata.name")
	flags.StringVar(&options.Identity.WorkloadURI, "workload-uri", "", "workload.uri")
	flags.StringVar(&options.Identity.WorkloadVersion, "workload-version", "dev", "workload.version")
	flags.StringVar(&options.Output, "out", "", "output file path; defaults to stdout")
	return cmd
}

func newGoValidateCommand(api goProfilerAPI, single bool) *cobra.Command {
	var directory, packagePath string
	cmd := &cobra.Command{
		Use: "validate-extensions", Short: "Validate imported Go binding packages",
		Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			absolute, err := filepath.Abs(directory)
			if err != nil {
				return err
			}
			var packages []string
			if single {
				packages = []string{packagePath}
			}
			if _, err := api.Validate(cmd.Context(), absolute, packages); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.ErrOrStderr(), "runtimeconditions: generated binding structures validated")
			return err
		},
	}
	cmd.Flags().StringVar(&directory, "dir", ".", "Go workload module directory")
	if single {
		cmd.Use, cmd.Short = "validate-extension", "Validate an imported Go binding package"
		cmd.Flags().StringVar(&packagePath, "package", "", "import path of a generated extension binding package")
		_ = cmd.MarkFlagRequired("package")
		cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
			if packagePath == "" {
				return usageError{errors.New("--package is required")}
			}
			return nil
		}
	}
	return cmd
}

func newGoVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use: "version", Short: "Show the linked Go profiler version",
		Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			identity := linkedGoProfilerIdentity()
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (linked into rc)\n", identity.Name, identity.Version)
			return err
		},
	}
}

func generateGoProfile(ctx context.Context, options goGenerateOptions, api goProfilerAPI, out io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := filepath.Abs(options.Directory)
	if err != nil {
		return err
	}
	if options.Identity.Name == "" {
		options.Identity.Name = filepath.Base(directory)
	}
	if options.Identity.WorkloadURI == "" {
		options.Identity.WorkloadURI = goWorkloadURI(directory)
	}
	// The upstream extraction API has no context parameter. Check cancellation
	// around the call without leaving analysis running in a goroutine.
	profile, err := api.Extract(directory, options.Identity)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := api.Marshal(profile)
	if err != nil {
		return err
	}
	if options.Output != "" {
		return writeGoProfileAtomically(options.Output, data)
	}
	_, err = out.Write(data)
	return err
}

func goWorkloadURI(directory string) string {
	for current := directory; ; current = filepath.Dir(current) {
		if data, err := os.ReadFile(filepath.Join(current, "go.mod")); err == nil {
			if module := modfile.ModulePath(data); module != "" {
				return module
			}
		}
		if parent := filepath.Dir(current); parent == current {
			return directory
		}
	}
}

func writeGoProfileAtomically(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".runtimeconditions-profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0644); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
