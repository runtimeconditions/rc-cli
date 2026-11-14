package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/runtimeconditions/rc-cli/internal/plugins"
	"github.com/runtimeconditions/rc-cli/internal/process"
	"go.yaml.in/yaml/v3"
)

// fakeRunner answers `--version --json` itself and records every other invocation.
type fakeRunner struct {
	version string
	code    int
	output  string
	calls   []process.Spec
}

func (f *fakeRunner) Run(_ context.Context, spec process.Spec) (int, error) {
	if slices.Equal(spec.Args, []string{"--version", "--json"}) {
		spec.Stdout.Write([]byte(f.version))
		return 0, nil
	}
	f.calls = append(f.calls, spec)
	spec.Stdout.Write([]byte(f.output))
	return f.code, nil
}

const readyVersion = `{"name":"@runtimeconditions/typescript-profiler","version":"0.2.0","coreSchemas":["0.4.0"]}`

func deps(runner process.Runner, installed ...string) Dependencies {
	present := map[string]bool{}
	for _, name := range installed {
		present[name] = true
	}
	return Dependencies{
		Finder: plugins.Finder{
			LookPath: func(name string) (string, error) {
				if present[name] {
					return "/usr/bin/" + name, nil
				}
				return "", errors.New("not found")
			},
			Getenv: func(string) string { return "" },
		},
		Runner: runner,
	}
}

func run(t *testing.T, d Dependencies, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := ExecuteWith(context.Background(), args, BuildInfo{Version: "v0.1.0", Commit: "abc"}, IOStreams{Out: &out, ErrOut: &errOut}, d)
	return code, out.String(), errOut.String()
}

func TestAddOnReceivesItsArgumentsUnchangedAndItsOutputPassesThrough(t *testing.T) {
	runner := &fakeRunner{version: readyVersion, output: "profile: yaml\n"}
	args := []string{"generate", "--project", "./svc", "--name", "orders", "--workload-uri", "https://example.com/o", "--out", "-h", "--", "--odd", "a b", "--project", "again"}
	code, out, errOut := run(t, deps(runner, "rc-profiler-typescript", "node"), append([]string{"profile", "typescript"}, args...)...)
	if code != 0 || out != "profile: yaml\n" || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if len(runner.calls) != 1 || runner.calls[0].Path != "/usr/bin/rc-profiler-typescript" || !slices.Equal(runner.calls[0].Args, args) {
		t.Fatalf("calls %+v", runner.calls)
	}
}

func TestAddOnHelpAndVersionFlagsReachTheAddOn(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--version"}, {"generate", "--help"}, {}} {
		runner := &fakeRunner{version: readyVersion}
		if code, _, _ := run(t, deps(runner, "rc-profiler-typescript", "node"), append([]string{"profile", "typescript"}, args...)...); code != 0 {
			t.Fatalf("%v: code %d", args, code)
		}
		if len(runner.calls) != 1 || !slices.Equal(runner.calls[0].Args, args) {
			t.Fatalf("%v: calls %+v", args, runner.calls)
		}
	}
}

func TestAddOnExitStatusIsTheExitStatusOfRcWithoutExtraMessages(t *testing.T) {
	runner := &fakeRunner{version: readyVersion, code: 7}
	code, _, errOut := run(t, deps(runner, "rc-profiler-typescript", "node"), "profile", "typescript", "generate")
	if code != 7 || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestMissingAddOnIsReportedOnceWithWhatToInstall(t *testing.T) {
	runner := &fakeRunner{version: readyVersion}
	code, _, errOut := run(t, deps(runner, "node"), "profile", "typescript", "generate")
	if code != 1 || strings.Count(errOut, "rc:") != 1 || !strings.Contains(errOut, "TypeScript profiler is not installed") ||
		!strings.Contains(errOut, "npm install --global @runtimeconditions/typescript-profiler") || strings.Contains(errOut, "Usage:") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("nothing may run: %+v", runner.calls)
	}
}

func TestAnAddOnThatCarriesItsOwnRuntimeRunsWithoutNodeOnPath(t *testing.T) {
	runner := &fakeRunner{version: readyVersion, output: "profile: yaml\n"}
	code, out, errOut := run(t, deps(runner, "rc-profiler-typescript"), "profile", "typescript", "generate")
	if code != 0 || out != "profile: yaml\n" || errOut != "" || len(runner.calls) != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%d", code, out, errOut, len(runner.calls))
	}
}

func TestAnAddOnThatCannotRunIsBlamedOnNodeOnlyWhenNodeIsMissing(t *testing.T) {
	runner := &fakeRunner{}
	code, _, errOut := run(t, deps(runner, "rc-profiler-typescript"), "profile", "typescript", "generate")
	if code != 1 || strings.Count(errOut, "rc:") != 1 || !strings.Contains(errOut, "needs node") || len(runner.calls) != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	runner = &fakeRunner{}
	code, _, errOut = run(t, deps(runner, "rc-profiler-typescript", "node"), "profile", "typescript", "generate")
	if code != 1 || strings.Contains(errOut, "needs node") || !strings.Contains(errOut, "did not print the expected JSON") {
		t.Fatalf("with node present the real error must show: code=%d stderr=%q", code, errOut)
	}
}

func TestAnAddOnForAnotherCoreSchemaIsRejectedBeforeItRuns(t *testing.T) {
	runner := &fakeRunner{version: `{"name":"x","version":"0.1.0","coreSchemas":["0.3.0"]}`}
	code, _, errOut := run(t, deps(runner, "rc-profiler-typescript", "node"), "profile", "typescript", "generate")
	if code != 1 || !strings.Contains(errOut, "supports core profile schema 0.3.0") || !strings.Contains(errOut, "targets 0.4.0") || len(runner.calls) != 0 {
		t.Fatalf("code=%d stderr=%q calls=%+v", code, errOut, runner.calls)
	}
	runner = &fakeRunner{version: "0.2.0\n"}
	code, _, errOut = run(t, deps(runner, "rc-profiler-typescript", "node"), "profile", "typescript", "generate")
	if code != 1 || !strings.Contains(errOut, "did not print the expected JSON") || len(runner.calls) != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestPluginsListReportsEveryStatus(t *testing.T) {
	read := func(d Dependencies) []pluginRow {
		code, out, errOut := run(t, d, "plugins", "list", "--output", "yaml")
		if code != 0 || errOut != "" {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
		var document struct {
			Plugins []pluginRow `yaml:"plugins"`
		}
		decoder := yaml.NewDecoder(strings.NewReader(out))
		decoder.KnownFields(true)
		if err := decoder.Decode(&document); err != nil {
			t.Fatal(err)
		}
		return document.Plugins
	}
	packaged := read(deps(&fakeRunner{version: readyVersion}, "rc-profiler-typescript"))
	if packaged[1].Status != "ready" {
		t.Fatalf("an add-on with its own runtime is ready without node: %+v", packaged[1])
	}
	ready := read(deps(&fakeRunner{version: readyVersion}, "rc-profiler-typescript", "node"))
	if len(ready) != 2 || ready[0].Language != "go" || ready[0].Status != "built-in" ||
		ready[1].Language != "typescript" || ready[1].Status != "ready" || ready[1].Version != "0.2.0" ||
		ready[1].Path != "/usr/bin/rc-profiler-typescript" || ready[1].Origin != "path" {
		t.Fatalf("%+v", ready)
	}
	for status, d := range map[string]Dependencies{
		"missing":         deps(&fakeRunner{}),
		"runtime-missing": deps(&fakeRunner{}, "rc-profiler-typescript"),
		"unreadable":      deps(&fakeRunner{version: "not json"}, "rc-profiler-typescript", "node"),
		"incompatible":    deps(&fakeRunner{version: `{"name":"x","version":"1","coreSchemas":["0.3.0"]}`}, "rc-profiler-typescript", "node"),
	} {
		if rows := read(d); rows[1].Status != status || rows[1].Detail == "" {
			t.Errorf("want %s, got %+v", status, rows[1])
		}
	}
}

func TestPluginsListTextAndHelp(t *testing.T) {
	code, out, _ := run(t, deps(&fakeRunner{version: readyVersion}, "rc-profiler-typescript", "node"), "plugins", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || !strings.HasPrefix(lines[0], "LANGUAGE") ||
		!slices.Equal(strings.Fields(lines[2]), []string{"typescript", "ready", "0.2.0", "/usr/bin/rc-profiler-typescript"}) {
		t.Fatalf("code=%d %q", code, out)
	}
	if code, _, errOut := run(t, deps(&fakeRunner{}), "plugins", "list", "--output", "json"); code != 2 || !strings.Contains(errOut, "unsupported output format") {
		t.Fatalf("code=%d %q", code, errOut)
	}
	code, out, _ = run(t, deps(&fakeRunner{}), "profile", "--help")
	if code != 0 || !strings.Contains(out, "typescript") || !strings.Contains(out, "go ") {
		t.Fatalf("profile help %q", out)
	}
}

func TestCoreSchemaVersionMatchesTheComponentLock(t *testing.T) {
	data, err := os.ReadFile("../../distribution/components.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		GoProfiler struct {
			CoreSchema struct {
				Version string `yaml:"version"`
			} `yaml:"coreSchema"`
		} `yaml:"goProfiler"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil || lock.GoProfiler.CoreSchema.Version != coreSchemaVersion {
		t.Fatalf("lock says %q, rc targets %q (%v)", lock.GoProfiler.CoreSchema.Version, coreSchemaVersion, err)
	}
}
