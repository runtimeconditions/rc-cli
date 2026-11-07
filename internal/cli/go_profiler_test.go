package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor"
	"github.com/spf13/cobra"
)

func testGoAPI() goProfilerAPI {
	return goProfilerAPI{
		Extract: func(string, extractor.GeneratedOptions) (map[string]any, error) {
			return map[string]any{"kind": "RuntimeConditionsProfile"}, nil
		},
		Marshal: func(map[string]any) ([]byte, error) { return []byte("kind: RuntimeConditionsProfile\n"), nil },
		Validate: func(context.Context, string, []string) ([]extensioncheck.ImportedGoPackage, error) {
			return nil, nil
		},
	}
}

func testGoRoot(api goProfilerAPI) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(BuildInfo{}, IOStreams{Out: &out, ErrOut: &errOut})
	current, _, _ := root.Find([]string{"profile"})
	root.RemoveCommand(current)
	root.AddCommand(newGoProfileCommands(api))
	return root, &out, &errOut
}

func TestGoGenerateTypedOptions(t *testing.T) {
	project := t.TempDir()
	for _, prefix := range [][]string{{"profile"}, {"profile", "go"}} {
		api := testGoAPI()
		var directory string
		var options extractor.GeneratedOptions
		api.Extract = func(dir string, opts extractor.GeneratedOptions) (map[string]any, error) {
			directory, options = dir, opts
			return map[string]any{"kind": "RuntimeConditionsProfile"}, nil
		}
		root, out, errOut := testGoRoot(api)
		args := append(append([]string{}, prefix...), "generate", "--dir", project,
			"--name", "first", "--name", "service with spaces", "--workload-uri", "https://example.com/service", "--workload-version", "1.2.3")
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := extractor.GeneratedOptions{Name: "service with spaces", WorkloadURI: "https://example.com/service", WorkloadVersion: "1.2.3"}
		if directory != project || options != want || out.String() != "kind: RuntimeConditionsProfile\n" || errOut.Len() != 0 {
			t.Fatalf("%v: directory=%q options=%+v stdout=%q stderr=%q", prefix, directory, options, out.String(), errOut.String())
		}
	}
}

func TestGoGenerateDefaultsAndFileOutput(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/service\n"), 0644); err != nil {
		t.Fatal(err)
	}
	api := testGoAPI()
	api.Extract = func(dir string, options extractor.GeneratedOptions) (map[string]any, error) {
		if dir != project || options.Name != filepath.Base(project) || options.WorkloadURI != "example.com/service" || options.WorkloadVersion != "dev" {
			t.Fatalf("directory=%q options=%+v", dir, options)
		}
		return map[string]any{}, nil
	}
	path := filepath.Join(project, "profile.yaml")
	root, out, errOut := testGoRoot(api)
	root.SetArgs([]string{"profile", "generate", "--dir", project, "--out", path})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "kind: RuntimeConditionsProfile\n" || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("data=%q err=%v stdout=%q stderr=%q", data, err, out.String(), errOut.String())
	}
	if temporary, _ := filepath.Glob(filepath.Join(project, ".runtimeconditions-profile-*")); len(temporary) != 0 {
		t.Fatalf("temporary output remains: %v", temporary)
	}
}

func TestGoValidationLibraryCalls(t *testing.T) {
	project := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, prefix := range [][]string{{"profile"}, {"profile", "go"}} {
		for _, single := range []bool{false, true} {
			api := testGoAPI()
			called := false
			api.Validate = func(actual context.Context, dir string, packages []string) ([]extensioncheck.ImportedGoPackage, error) {
				called = true
				var want []string
				if single {
					want = []string{"example.com/binding"}
				}
				if actual != ctx || dir != project || !reflect.DeepEqual(packages, want) {
					t.Fatalf("context propagated=%t directory=%q packages=%v", actual == ctx, dir, packages)
				}
				return nil, nil
			}
			args := append(append([]string{}, prefix...), "validate-extensions", "--dir", project)
			if single {
				args[len(prefix)] = "validate-extension"
				args = append(args, "--package", "example.com/binding")
			}
			root, out, errOut := testGoRoot(api)
			root.SetArgs(args)
			if err := root.ExecuteContext(ctx); err != nil || !called || out.Len() != 0 ||
				errOut.String() != "runtimeconditions: generated binding structures validated\n" {
				t.Fatalf("%v: called=%t err=%v stdout=%q stderr=%q", args, called, err, out.String(), errOut.String())
			}
		}
	}
}

func TestGoUsageAndHelp(t *testing.T) {
	for _, args := range [][]string{
		{"profile", "generate", "--unknown"}, {"profile", "generate", "-dir", "."},
		{"profile", "go", "generate", "extra"}, {"profile", "java", "generate"},
		{"profile", "validate-extension"}, {"profile", "go", "validate-extension", "--package", ""},
	} {
		code, out, errOut := invoke(t, args...)
		if code != 2 || out != "" || strings.Count(errOut, "rc: ") != 1 || !strings.Contains(errOut, "Usage:") {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
	t.Setenv("PATH", "")
	t.Setenv("RC_GO_PROFILER_BIN", "/does/not/exist")
	for _, args := range [][]string{{"profile"}, {"profile", "go"}, {"help", "profile", "go", "generate"}, {"profile", "generate", "--help"}} {
		code, out, errOut := invoke(t, args...)
		if code != 0 || !strings.Contains(out, "Usage:") || errOut != "" {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
	code, out, errOut := invoke(t, "profile", "go", "generate", "--help")
	if code != 0 || !strings.Contains(out, "--dir") || !strings.Contains(out, "--out") || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestGoAliasFlagIsolationAndCompletion(t *testing.T) {
	root, _, _ := testGoRoot(testGoAPI())
	defaultCommand, _, _ := root.Find([]string{"profile", "generate"})
	explicitCommand, _, _ := root.Find([]string{"profile", "go", "generate"})
	if defaultCommand == explicitCommand {
		t.Fatal("aliases share a command instance")
	}
	if err := defaultCommand.Flags().Set("name", "default route"); err != nil {
		t.Fatal(err)
	}
	if name, _ := explicitCommand.Flags().GetString("name"); name != "" {
		t.Fatal("alias flags share state")
	}
	code, out, _ := invoke(t, "__complete", "profile", "go", "generate", "--na")
	if code != 0 || !strings.Contains(out, "--name") {
		t.Fatalf("code=%d completion=%q", code, out)
	}
}

func TestGoLibraryFailuresPreserveOutput(t *testing.T) {
	for _, stage := range []string{"extract", "marshal", "validate"} {
		want := errors.New("library failure")
		api := testGoAPI()
		switch stage {
		case "extract":
			api.Extract = func(string, extractor.GeneratedOptions) (map[string]any, error) { return nil, want }
		case "marshal":
			api.Marshal = func(map[string]any) ([]byte, error) { return nil, want }
		case "validate":
			api.Validate = func(context.Context, string, []string) ([]extensioncheck.ImportedGoPackage, error) { return nil, want }
		}
		directory := t.TempDir()
		path := filepath.Join(directory, "profile.yaml")
		if err := os.WriteFile(path, []byte("preserve"), 0644); err != nil {
			t.Fatal(err)
		}
		root, out, errOut := testGoRoot(api)
		args := []string{"profile", "generate", "--dir", directory, "--out", path}
		if stage == "validate" {
			args = []string{"profile", "validate-extensions", "--dir", directory}
		}
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); !errors.Is(err, want) {
			t.Fatalf("%s: %v", stage, err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "preserve" || out.Len() != 0 || errOut.Len() != 0 {
			t.Fatalf("%s: output changed", stage)
		}
	}
}

func TestGoCancellationAndOutputFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := testGoAPI()
	api.Extract = func(string, extractor.GeneratedOptions) (map[string]any, error) {
		cancel()
		return map[string]any{}, nil
	}
	root, out, _ := testGoRoot(api)
	root.SetArgs([]string{"profile", "generate"})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, out.String())
	}
	root, _, _ = testGoRoot(testGoAPI())
	root.SetOut(failingWriter{})
	root.SetArgs([]string{"profile", "generate"})
	if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "output closed") {
		t.Fatalf("output failure: %v", err)
	}
}
