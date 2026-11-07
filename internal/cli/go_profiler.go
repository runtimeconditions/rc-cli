package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor"
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

// The Go profiler is called as a library. A working Go toolchain is still used
// by that library to inspect the workload; no profiler executable is required.
func runLinkedGoProfiler(ctx context.Context, args []string, streams IOStreams) error {
	operation := "generate"
	if len(args) > 0 {
		switch args[0] {
		case "--version", "version":
			identity := linkedGoProfilerIdentity()
			_, err := fmt.Fprintf(streams.Out, "%s %s (linked into rc)\n", identity.Name, identity.Version)
			return err
		case "generate", "validate-extension", "validate-extensions":
			operation, args = args[0], args[1:]
		default:
			if !strings.HasPrefix(args[0], "-") {
				return usageError{fmt.Errorf("unknown Go profiler command %q", args[0])}
			}
		}
	}
	flags := flag.NewFlagSet("rc profile go "+operation, flag.ContinueOnError)
	flags.SetOutput(streams.ErrOut)
	dir := flags.String("dir", ".", "directory containing Go source declarations")
	var name, uri, version, output, packagePath string
	if operation == "generate" {
		flags.StringVar(&name, "name", "", "profile metadata.name")
		flags.StringVar(&uri, "workload-uri", "", "workload.uri")
		flags.StringVar(&version, "workload-version", "dev", "workload.version")
		flags.StringVar(&output, "out", "", "output file path; defaults to stdout")
	}
	if operation == "validate-extension" {
		flags.StringVar(&packagePath, "package", "", "import path of a generated extension binding package")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return profilerExitError(2) // flag.Parse already printed its diagnostic.
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(streams.ErrOut, "unexpected positional arguments: %s\n", strings.Join(flags.Args(), " "))
		return profilerExitError(2)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	if operation != "generate" {
		var packages []string
		if operation == "validate-extension" {
			if packagePath == "" {
				return errors.New("-package is required")
			}
			packages = []string{packagePath}
		}
		if _, err := extensioncheck.ValidateImportedGoPackages(ctx, directory, packages); err != nil {
			return err
		}
		_, err := fmt.Fprintln(streams.ErrOut, "runtimeconditions: generated binding structures validated")
		return err
	}
	if name == "" {
		name = filepath.Base(directory)
	}
	if uri == "" {
		uri = goWorkloadURI(directory)
	}
	// The current upstream extraction API does not accept a context. Check
	// cancellation before and after it without leaving analysis in a goroutine.
	profile, err := extractor.ExtractGeneratedDir(directory, extractor.GeneratedOptions{
		Name: name, WorkloadURI: uri, WorkloadVersion: version,
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := extractor.MarshalProfileYAML(profile)
	if err != nil {
		return err
	}
	if output != "" {
		return writeGoProfileAtomically(output, data)
	}
	_, err = streams.Out.Write(data)
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
