package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator"
	"github.com/spf13/cobra"
)

var bindingOperations = []string{"resolve", "normalize", "generate", "verify", "package", "plan-release", "check", "update"}

func testBindingProfiler() (orchestrator.ProfilerIdentity, error) {
	return orchestrator.NewLinkedGoProfilerIdentity("v0.6.0", "h1:"+strings.Repeat("A", 43)+"=")
}

func bindingTestRoot(execute bindingExecution) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "rc", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newBindingCommands(execute, testBindingProfiler))
	return root, &out, &errOut
}

func TestBindingsHelpCompletionAndUsage(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("RC_GO_PROFILER_BIN", "/must/not/run")
	for _, name := range append([]string{""}, bindingOperations...) {
		args := []string{"bindings", "--help"}
		if name != "" {
			args = []string{"bindings", name, "--help"}
		}
		if code, out, errOut := invoke(t, args...); code != 0 || !strings.Contains(out, "Usage:") || errOut != "" {
			t.Fatalf("help %s: %d %q %q", name, code, out, errOut)
		}
	}
	if code, out, _ := invoke(t, "__complete", "bindings", ""); code != 0 || !strings.Contains(out, "plan-release\t") {
		t.Fatalf("binding completion: %d %q", code, out)
	}
	for _, args := range [][]string{{"missing"}, {"resolve", "extra"}, {"verify", "--closure-output", "x"}, {"generate"}, {"package", "--output", ""}} {
		code, out, errOut := invoke(t, append([]string{"bindings"}, args...)...)
		if code != 2 || out != "" || strings.Count(errOut, "rc: ") != 1 || !strings.Contains(errOut, "Usage:") {
			t.Fatalf("usage %v: %d %q %q", args, code, out, errOut)
		}
	}
}

func TestBindingsMapEveryCommandAndFlag(t *testing.T) {
	fields := [][2]string{{"packages", "Packages"}, {"toolchain-lock", "ToolchainLock"}, {"tooling-dir", "ToolingDir"}, {"cache", "Cache"}, {"work-dir", "WorkDir"}, {"dependency-lock", "DependencyLock"}, {"closure-output", "ClosureOutput"}, {"dependency-lock-output", "DependencyLockOutput"}, {"output", "Output"}, {"input", "Input"}, {"report", "Report"}, {"core-schema", "CoreSchema"}, {"go", "Go"}, {"python", "Python"}, {"normalizer", "Normalizer"}, {"go-emitter", "GoEmitter"}, {"go-profiler", "GoProfiler"}, {"python-profiler", "PythonProfiler"}, {"python-profiler-artifact", "PythonProfilerArtifact"}}
	for _, operation := range bindingOperations {
		t.Run(operation, func(t *testing.T) {
			want := orchestrator.DefaultOptions()
			want.Normalizer, want.GoEmitter, want.GoProfiler, want.UseGoLibraries = "", "", "", true
			calls := 0
			ctx := context.WithValue(context.Background(), struct{}{}, "sentinel")
			root, out, errOut := bindingTestRoot(func(gotCtx context.Context, cwd, name string, got orchestrator.Options, stdout, stderr io.Writer) error {
				calls++
				if gotCtx != ctx || name != operation || !filepath.IsAbs(cwd) || !reflect.DeepEqual(got, want) {
					t.Fatalf("dispatch %s: cwd=%q options=%+v; want %+v", name, cwd, got, want)
				}
				_, _ = io.WriteString(stdout, "data")
				_, _ = io.WriteString(stderr, "diagnostic")
				return nil
			})
			cmd, _, _ := root.Find([]string{"bindings", operation})
			args := []string{"bindings", operation}
			for _, field := range fields {
				if cmd.Flags().Lookup(field[0]) != nil {
					value := field[0] + " with spaces,commas"
					args = append(args, "--"+field[0], value)
					reflect.ValueOf(&want).Elem().FieldByName(field[1]).SetString(value)
				}
			}
			for _, field := range [][2]string{{"target", "Targets"}, {"extension-root", "ExtensionRoots"}, {"extension-override", "Overrides"}, {"catalog-root", "CatalogRoots"}} {
				if cmd.Flags().Lookup(field[0]) != nil {
					values := []string{"one with spaces,commas", "two"}
					args = append(args, "--"+field[0], values[0], "--"+field[0], values[1])
					destination := reflect.ValueOf(&want).Elem().FieldByName(field[1])
					destination.Set(reflect.ValueOf(values).Convert(destination.Type()))
				}
			}
			root.SetArgs(append(args, "--"))
			if err := root.ExecuteContext(ctx); err != nil || calls != 1 || out.String() != "data" || errOut.String() != "diagnostic" {
				t.Fatalf("dispatch failed: %v calls=%d stdout=%q stderr=%q", err, calls, out, errOut)
			}
		})
	}
}

func TestBindingsExplicitEmptyToolsAndIsolation(t *testing.T) {
	root, _, _ := bindingTestRoot(func(_ context.Context, _, _ string, o orchestrator.Options, _, _ io.Writer) error {
		if o.Go != "" || o.Python != "" || o.GoProfiler != "" || o.LinkedGoProfiler != nil || o.ToolingDir != "" {
			t.Fatalf("explicit empty selections replaced: %+v", o)
		}
		return nil
	})
	root.SetArgs([]string{"bindings", "generate", "--output", "result", "--tooling-dir=", "--go=", "--python=", "--go-profiler="})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	generate, _, _ := root.Find([]string{"bindings", "generate"})
	verify, _, _ := root.Find([]string{"bindings", "verify"})
	if generate == verify || verify.Flags().Changed("go") {
		t.Fatal("binding commands share flag state")
	}
}

func TestBindingsCancellationAndLibraryErrors(t *testing.T) {
	want := errors.New("library failure")
	calls := 0
	execute := func(context.Context, string, string, orchestrator.Options, io.Writer, io.Writer) error {
		calls++
		return want
	}
	root, _, _ := bindingTestRoot(execute)
	root.SetArgs([]string{"bindings", "resolve", "--tooling-dir=chosen"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled dispatch: %v calls=%d", err, calls)
	}
	root, _, _ = bindingTestRoot(execute)
	root.SetArgs([]string{"bindings", "resolve", "--tooling-dir=chosen"})
	if err := root.ExecuteContext(context.Background()); !errors.Is(err, want) || calls != 1 {
		t.Fatalf("library error: %v calls=%d", err, calls)
	}
}

func TestBindingsProfilerMetadata(t *testing.T) {
	valid := debug.Module{Path: goProfilerModule, Version: "v0.6.0", Sum: "h1:" + strings.Repeat("A", 43) + "="}
	if got, err := bindingProfilerIdentity(&debug.BuildInfo{Deps: []*debug.Module{&valid}}); err != nil || got.ModuleSum != valid.Sum || got.Version != valid.Version {
		t.Fatalf("linked profiler: %+v %v", got, err)
	}
	for _, field := range []string{"absent", "replacement", "version", "checksum"} {
		module := valid
		info := &debug.BuildInfo{Deps: []*debug.Module{&module}}
		switch field {
		case "absent":
			info = nil
		case "replacement":
			module.Replace = &valid
		case "version":
			module.Version = "(devel)"
		case "checksum":
			module.Sum = ""
		}
		if _, err := bindingProfilerIdentity(info); err == nil {
			t.Fatalf("accepted profiler %s", field)
		}
	}
}

func TestBindingsInstalledResourceLookup(t *testing.T) {
	prefix := t.TempDir()
	executable := filepath.Join(prefix, "bin", "rc")
	resources := filepath.Join(prefix, "share", "rc", "extension-bindings")
	for _, path := range []string{filepath.Dir(executable), filepath.Join(resources, "model")} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	model := filepath.Join(resources, "model", "runtimeconditions.binding-model.schema.yaml")
	for _, path := range []string{executable, model} {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(t.TempDir(), "rc")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	resources, _ = filepath.EvalSymlinks(resources)
	if got, err := installedBindingTooling(link); err != nil || got != resources {
		t.Fatalf("installed schema lookup: %q %v", got, err)
	}
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}
	if _, err := installedBindingTooling(executable); err == nil || !strings.Contains(err.Error(), "--tooling-dir") {
		t.Fatalf("missing schemas: %v", err)
	}
}

func TestBindingsDefaultsMatchUpstreamWithDeclaredLibraryOverrides(t *testing.T) {
	for key, value := range map[string]string{"RC_BINDINGS_GO": "native-go", "RC_BINDINGS_PYTHON": "native-python", "RC_BINDINGS_NORMALIZER": "external-normalizer", "RC_BINDINGS_GO_EMITTER": "external-emitter", "RC_GO_PROFILER_BIN": "external-profiler", "RC_PYTHON_PROFILER_BIN": "python-profiler", "RC_PYTHON_PROFILER_ARTIFACT": "profiler.whl"} {
		t.Setenv(key, value)
	}
	for _, name := range bindingOperations {
		want := orchestrator.DefaultOptions()
		want.Normalizer, want.GoEmitter, want.GoProfiler, want.UseGoLibraries = "", "", "", true
		want.ToolingDir = "selected"
		if name != "resolve" && name != "normalize" {
			identity, _ := testBindingProfiler()
			want.LinkedGoProfiler = &identity
		}
		args := []string{"bindings", name, "--tooling-dir=selected"}
		if name == "generate" || name == "package" {
			want.Output = "result"
			args = append(args, "--output=result")
		}
		root, _, _ := bindingTestRoot(func(_ context.Context, _, _ string, got orchestrator.Options, _, _ io.Writer) error {
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s defaults differ: %+v; want %+v", name, got, want)
			}
			return nil
		})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBindingsProfilerFailureDoesNotSelectStandaloneTool(t *testing.T) {
	want := errors.New("unverified profiler metadata")
	cmd := newBindingCommands(func(context.Context, string, string, orchestrator.Options, io.Writer, io.Writer) error {
		t.Fatal("executed after profiler identity failed")
		return nil
	}, func() (orchestrator.ProfilerIdentity, error) { return orchestrator.ProfilerIdentity{}, want })
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"generate", "--output=result", "--tooling-dir=selected"})
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Fatalf("profiler error replaced: %v", err)
	}
}
