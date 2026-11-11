package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator"
	"github.com/spf13/cobra"
)

type bindingExecution func(context.Context, string, string, orchestrator.Options, io.Writer, io.Writer) error
type bindingProfiler func() (orchestrator.ProfilerIdentity, error)

func newBindingsCommand() *cobra.Command {
	return newBindingCommands(orchestrator.Execute, func() (orchestrator.ProfilerIdentity, error) {
		info, _ := debug.ReadBuildInfo()
		return bindingProfilerIdentity(info)
	})
}

// Execution is injected only for command tests; production calls Execute directly.
func newBindingCommands(execute bindingExecution, profiler bindingProfiler) *cobra.Command {
	parent := &cobra.Command{
		Use: "bindings", Short: "Resolve, generate, and verify extension bindings",
		Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, operation := range []struct{ name, description string }{
		{"resolve", "Resolve extension definitions and dependency locks"},
		{"normalize", "Produce a normalized binding model"},
		{"generate", "Generate binding package trees"},
		{"verify", "Verify generated or committed binding packages"},
		{"package", "Verify and archive committed binding packages"},
		{"plan-release", "Compare packages and report release requirements"},
		{"check", "Regenerate, verify, and compare committed bindings"},
		{"update", "Generate, verify, and update committed bindings"},
	} {
		name := operation.name
		o := orchestrator.DefaultOptions()
		o.UseGoLibraries = true
		// Installed Go libraries replace standalone executable defaults, not flags.
		o.Normalizer, o.GoEmitter, o.GoProfiler = "", "", ""
		var targets, roots, overrides, catalogs []string
		cmd := &cobra.Command{
			Use: name, Short: operation.description,
			Args: usageArgs(cobra.NoArgs), ValidArgsFunction: cobra.NoFileCompletions,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				if (name == "generate" || name == "package") && o.Output == "" {
					return usageError{fmt.Errorf("%s requires --output", name)}
				}
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				o.Targets, o.ExtensionRoots, o.Overrides, o.CatalogRoots = targets, roots, overrides, catalogs
				if !cmd.Flags().Changed("tooling-dir") {
					executable, err := os.Executable()
					if err != nil {
						return err
					}
					o.ToolingDir, err = installedBindingTooling(executable)
					if err != nil {
						return err
					}
				}
				// Resolve and normalize do not record or require profiler provenance.
				o.LinkedGoProfiler = nil
				if name != "resolve" && name != "normalize" && !cmd.Flags().Changed("go-profiler") {
					identity, err := profiler()
					if err != nil {
						return err
					}
					o.LinkedGoProfiler = &identity
				}
				return execute(cmd.Context(), cwd, name, o, cmd.OutOrStdout(), cmd.ErrOrStderr())
			},
		}
		f := cmd.Flags()
		f.StringVar(&o.Packages, "packages", "", "package catalog path")
		f.StringVar(&o.ToolchainLock, "toolchain-lock", "", "toolchain lock path")
		f.StringVar(&o.Cache, "cache", "", "explicit persistent extension cache")
		f.StringVar(&o.WorkDir, "work-dir", "", "retain intermediates in this directory")
		f.StringVar(&o.ToolingDir, "tooling-dir", "", "directory containing binding schemas and optional language resources")
		f.StringArrayVar(&targets, "target", nil, "package-key:language; repeatable, defaults to all targets")
		if name == "resolve" || name == "normalize" || name == "generate" || name == "package" || name == "check" || name == "update" {
			f.StringArrayVar(&roots, "extension-root", nil, "exact root extension identifier; repeatable")
			f.StringArrayVar(&overrides, "extension-override", nil, "identifier=local-path; repeatable, development only")
			f.StringArrayVar(&catalogs, "catalog-root", nil, "local definition catalog; repeatable")
			f.StringVar(&o.DependencyLock, "dependency-lock", "", "existing exact dependency lock")
		}
		if name == "resolve" {
			f.StringVar(&o.ClosureOutput, "closure-output", "", "write semantic closure here")
		}
		if name == "resolve" || name == "normalize" {
			f.StringVar(&o.DependencyLockOutput, "dependency-lock-output", "", "write resolved dependency lock here")
		}
		if name == "normalize" || name == "generate" || name == "package" || name == "plan-release" {
			f.StringVar(&o.Output, "output", "", "requested output file or directory; required for generate and package")
		}
		if name == "verify" {
			f.StringVar(&o.Input, "input", "", "generated tree root; defaults to committed targets")
		}
		if name == "verify" || name == "check" {
			f.StringVar(&o.Report, "report", "", "write the YAML verification summary here")
		}
		f.StringVar(&o.CoreSchema, "core-schema", "", "explicit core profile schema file")
		f.StringVar(&o.Go, "go", o.Go, "native Go command")
		f.StringVar(&o.Python, "python", o.Python, "native Python interpreter")
		f.StringVar(&o.Normalizer, "normalizer", o.Normalizer, "standalone normalizer override; conflicts with linked Go libraries")
		f.StringVar(&o.GoEmitter, "go-emitter", o.GoEmitter, "standalone Go emitter override; conflicts with linked Go libraries")
		f.StringVar(&o.GoProfiler, "go-profiler", o.GoProfiler, "standalone Go profiler; defaults to the linked profiler")
		f.StringVar(&o.PythonProfiler, "python-profiler", o.PythonProfiler, "independently installed Python profiler")
		f.StringVar(&o.PythonProfilerArtifact, "python-profiler-artifact", o.PythonProfilerArtifact, "installed Python profiler wheel for identity verification")
		parent.AddCommand(cmd)
	}
	return parent
}

func bindingProfilerIdentity(info *debug.BuildInfo) (orchestrator.ProfilerIdentity, error) {
	if info != nil {
		for _, dependency := range info.Deps {
			if dependency.Path == goProfilerModule {
				if dependency.Replace != nil {
					return orchestrator.ProfilerIdentity{}, fmt.Errorf("linked Go profiler is replaced; use a verified published module or select --go-profiler")
				}
				return orchestrator.NewLinkedGoProfilerIdentity(dependency.Version, dependency.Sum)
			}
		}
	}
	return orchestrator.ProfilerIdentity{}, fmt.Errorf("linked Go profiler build metadata unavailable; build rc with its pinned dependencies or select --go-profiler")
}

func installedBindingTooling(executable string) (string, error) {
	path, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	directory := filepath.Clean(filepath.Join(filepath.Dir(path), "..", "share", "rc", "extension-bindings"))
	info, err := os.Stat(filepath.Join(directory, "model", "runtimeconditions.binding-model.schema.yaml"))
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("binding schemas unavailable at %s; install the complete rc package or supply --tooling-dir", directory)
	}
	return directory, nil
}
