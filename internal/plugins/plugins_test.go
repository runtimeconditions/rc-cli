package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/runtimeconditions/rc-cli/internal/process"
)

func finder(installed map[string]string, env map[string]string) Finder {
	return Finder{
		LookPath: func(name string) (string, error) {
			if path, ok := installed[name]; ok {
				return path, nil
			}
			return "", errors.New("not found")
		},
		Getenv: func(name string) string { return env[name] },
	}
}

func TestFindPrefersTheFirstNamedExecutable(t *testing.T) {
	found, err := finder(map[string]string{
		"runtimeconditions-typescript-profiler": "/bin/long", "rc-profiler-typescript": "/bin/short", "node": "/bin/node",
	}, nil).Find(TypeScript)
	if err != nil || found.Path != "/bin/short" || found.Origin != "path" {
		t.Fatalf("%+v %v", found, err)
	}
	found, err = finder(map[string]string{"runtimeconditions-typescript-profiler": "/bin/long", "node": "/bin/node"}, nil).Find(TypeScript)
	if err != nil || found.Path != "/bin/long" {
		t.Fatalf("%+v %v", found, err)
	}
}

func TestFindReportsAMissingAddOnWithHowToInstallIt(t *testing.T) {
	_, err := finder(map[string]string{"node": "/bin/node"}, nil).Find(TypeScript)
	var missing NotInstalledError
	if !errors.As(err, &missing) {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"TypeScript profiler is not installed", "rc-profiler-typescript", TypeScript.InstallHint, TypeScript.OverrideEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q missing from %q", want, err)
		}
	}
}

func TestFindReportsAMissingRuntimeSeparately(t *testing.T) {
	_, err := finder(map[string]string{"rc-profiler-typescript": "/bin/short"}, nil).Find(TypeScript)
	var runtime RuntimeMissingError
	if !errors.As(err, &runtime) || runtime.Runtime != "node" || !strings.Contains(err.Error(), "needs node") {
		t.Fatalf("got %v", err)
	}
}

func TestAnExplicitOverrideWinsAndMustBeAnAbsoluteExecutable(t *testing.T) {
	installed := map[string]string{"rc-profiler-typescript": "/bin/short", "node": "/bin/node", "/opt/ts/profiler": "/opt/ts/profiler"}
	found, err := finder(installed, map[string]string{TypeScript.OverrideEnv: "/opt/ts/profiler"}).Find(TypeScript)
	if err != nil || found.Path != "/opt/ts/profiler" || found.Origin != "override" {
		t.Fatalf("%+v %v", found, err)
	}
	if _, err := finder(installed, map[string]string{TypeScript.OverrideEnv: "profiler"}).Find(TypeScript); err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("relative override: %v", err)
	}
	if _, err := finder(installed, map[string]string{TypeScript.OverrideEnv: "/opt/gone"}).Find(TypeScript); err == nil || !strings.Contains(err.Error(), "not an executable file") {
		t.Fatalf("missing override must not fall back to PATH: %v", err)
	}
}

func TestLanguageIDsDoNotShadowGoCommands(t *testing.T) {
	reserved := []string{"go", "generate", "validate-extension", "validate-extensions", "version", "help", "completion"}
	seen := map[string]bool{}
	for _, language := range Languages() {
		if slices.Contains(reserved, language.ID) || seen[language.ID] {
			t.Errorf("language id %q is reserved or duplicated", language.ID)
		}
		seen[language.ID] = true
		if len(language.Executables) == 0 || language.OverrideEnv == "" || language.InstallHint == "" {
			t.Errorf("%s needs executables, an override variable, and an install hint", language.ID)
		}
	}
}

type answer struct {
	stdout, stderr string
	code           int
	err            error
}

func (a answer) Run(_ context.Context, spec process.Spec) (int, error) {
	if !slices.Equal(spec.Args, []string{"--version", "--json"}) {
		return 0, errors.New("unexpected arguments")
	}
	spec.Stdout.Write([]byte(a.stdout))
	spec.Stderr.Write([]byte(a.stderr))
	return a.code, a.err
}

func TestDescribeReadsTheVersionAndCoreSchemas(t *testing.T) {
	info, err := Describe(context.Background(), answer{stdout: `{"name":"@runtimeconditions/typescript-profiler","version":"0.2.0","coreSchemas":["0.4.0"],"extra":1}` + "\n"}, "/bin/p")
	if err != nil || info.Version != "0.2.0" || !info.Supports("0.4.0") || info.Supports("0.3.0") {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDescribeRejectsAnythingElse(t *testing.T) {
	for name, a := range map[string]answer{
		"nonzero exit":     {code: 2, stderr: "boom"},
		"not json":         {stdout: "0.2.0\n"},
		"no version":       {stdout: `{"name":"x","coreSchemas":["0.4.0"]}`},
		"run error":        {err: errors.New("cannot start")},
		"oversized output": {stdout: strings.Repeat("x", describeLimit+1)},
		"empty":            {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Describe(context.Background(), a, "/bin/p"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDescribeRunsARealExecutable(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("uses a shell script")
	}
	path := filepath.Join(t.TempDir(), "addon")
	body := "#!/bin/sh\n[ \"$1 $2\" = \"--version --json\" ] && echo '{\"name\":\"n\",\"version\":\"1.2.3\",\"coreSchemas\":[\"0.4.0\"]}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := Describe(context.Background(), process.OS{}, path)
	if err != nil || info.Version != "1.2.3" {
		t.Fatalf("%+v %v", info, err)
	}
}
