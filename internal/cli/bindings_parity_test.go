package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator"
	"github.com/spf13/pflag"
)

func TestBindingsInterfaceMatchesPinnedUpstream(t *testing.T) {
	var names, help bytes.Buffer
	if err := orchestrator.Run(context.Background(), []string{"bindings", "--help"}, &names, &help); err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(strings.TrimPrefix(strings.TrimSpace(names.String()), "Commands:"))
	var got []string
	commands := newBindingsCommand().Commands()
	for _, cmd := range commands {
		got = append(got, cmd.Name())
		help.Reset()
		if err := orchestrator.Run(context.Background(), []string{"bindings", cmd.Name(), "--help"}, &names, &help); err != nil {
			t.Fatal(err)
		}
		var expectedFlags, actualFlags []string
		for _, match := range regexp.MustCompile(`(?m)^  -([a-z][a-z0-9-]*)\b`).FindAllStringSubmatch(help.String(), -1) {
			expectedFlags = append(expectedFlags, match[1])
		}
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Name != "help" {
				actualFlags = append(actualFlags, flag.Name)
			}
		})
		slices.Sort(expectedFlags)
		slices.Sort(actualFlags)
		if !reflect.DeepEqual(actualFlags, expectedFlags) {
			t.Fatalf("%s flags differ from upstream: %v; want %v", cmd.Name(), actualFlags, expectedFlags)
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands differ from upstream: %v; want %v", got, want)
	}
}

func bindingParityFixture(t *testing.T) (orchestrator.Options, []string) {
	t.Helper()
	tooling := os.Getenv("RC_BINDINGS_TEST_TOOLING")
	if tooling == "" {
		tooling = filepath.Join(os.Getenv("RUNNER_TEMP"), "rc-ci", "share", "rc", "extension-bindings")
	}
	if _, err := os.Stat(filepath.Join(tooling, "model", "runtimeconditions.binding-model.schema.yaml")); err != nil {
		t.Skip("requires CI-staged schemas or RC_BINDINGS_TEST_TOOLING pointing to the pinned YAML resources")
	}
	tooling, err := filepath.Abs(tooling)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, dir := range []string{".git", "catalog"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"catalog/runtimeconditions.extension.yaml": `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata: {id: 'https://bindings.example.test/parity:0.1.0', version: 0.1.0}
spec:
  kinds: [{name: parity.service}]
`,
		"packages.yaml": `apiVersion: runtimeconditions.io/package-catalog/v1alpha1
kind: RuntimeConditionsPackageCatalog
repositoryUrl: https://github.com/example/bindings
packages:
  fixture:
    rootExtension: https://bindings.example.test/parity:0.1.0
    coreProfileSchemaVersion: 0.4.0
    languages:
      go:
        coordinate: example.test/bindings/fixture
        name: fixture
        version: 0.1.0
        sourceDirectory: bindings/fixture/go
        languageVersion: ` + strings.TrimPrefix(runtime.Version(), "go") + `
        publicationMode: github-tag
`,
		"toolchain.lock.yaml": "apiVersion: runtimeconditions.io/toolchain-lock/v1alpha1\nkind: RuntimeConditionsToolchainLock\nstatus: development\n",
		"core.yaml":           "$id: https://runtimeconditions.io/schemas/profile/0.4.0/runtimeconditions.profile.schema.yaml\ntype: object\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	o := orchestrator.DefaultOptions()
	o.Normalizer, o.GoEmitter, o.GoProfiler, o.UseGoLibraries = "", "", "", true
	o.Packages, o.ToolchainLock, o.CoreSchema, o.ToolingDir = filepath.Join(root, "packages.yaml"), filepath.Join(root, "toolchain.lock.yaml"), filepath.Join(root, "core.yaml"), tooling
	return o, []string{"--packages", o.Packages, "--toolchain-lock", o.ToolchainLock, "--core-schema", o.CoreSchema, "--tooling-dir", tooling}
}

func TestBindingsResultsMatchLibraryAndStandaloneResolution(t *testing.T) {
	o, flags := bindingParityFixture(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"resolve", "normalize", "generate", "verify"} {
		t.Run(name, func(t *testing.T) {
			selected := o
			args := append([]string{"bindings", name}, flags...)
			if name == "generate" || name == "verify" {
				identity, _ := testBindingProfiler()
				selected.LinkedGoProfiler = &identity
			}
			cliDirectory, apiDirectory := filepath.Join(t.TempDir(), "result"), filepath.Join(t.TempDir(), "result")
			if name == "generate" {
				selected.Output = apiDirectory
				args = append(args, "--output", cliDirectory)
			}
			if name == "verify" {
				generate := selected
				generate.Output = apiDirectory
				if err := orchestrator.Execute(context.Background(), cwd, "generate", generate, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
					t.Fatal(err)
				}
				selected.Input = apiDirectory
				args = append(args, "--input", apiDirectory)
			}
			var apiOut, apiErr bytes.Buffer
			if err := orchestrator.Execute(context.Background(), cwd, name, selected, &apiOut, &apiErr); err != nil {
				t.Fatal(err)
			}
			root, cliOut, cliErr := bindingTestRoot(orchestrator.Execute)
			root.SetArgs(args)
			if err := root.Execute(); err != nil || cliOut.String() != apiOut.String() || cliErr.String() != apiErr.String() {
				t.Fatalf("%s result differs: %v\nCLI: %s %s\nAPI: %s %s", name, err, cliOut, cliErr, &apiOut, &apiErr)
			}
			if name == "generate" && !reflect.DeepEqual(bindingTree(t, cliDirectory), bindingTree(t, apiDirectory)) {
				t.Fatal("generated package bytes differ from the library")
			}
			if name == "resolve" {
				var standaloneOut, standaloneErr bytes.Buffer
				if err := orchestrator.Run(context.Background(), args, &standaloneOut, &standaloneErr); err != nil || standaloneOut.String() != cliOut.String() || standaloneErr.String() != cliErr.String() {
					t.Fatalf("standalone resolution differs: %v", err)
				}
				var diagnostics bytes.Buffer
				code := Execute(context.Background(), args, BuildInfo{}, IOStreams{Out: failingWriter{}, ErrOut: &diagnostics})
				if code != 1 || strings.Count(diagnostics.String(), "rc: ") != 1 || !strings.Contains(diagnostics.String(), "output closed") {
					t.Fatalf("output failure: exit %d, %s", code, &diagnostics)
				}
			}
		})
	}
}

func bindingTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		files[relative] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
