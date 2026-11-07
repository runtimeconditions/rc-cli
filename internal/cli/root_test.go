package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(context.Background(), args, BuildInfo{Version: "v0.1.0", Commit: "abc123"},
		IOStreams{Out: &out, ErrOut: &errOut})
	return code, out.String(), errOut.String()
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"help"}, {"help", "version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, errOut := invoke(t, args...)
			if code != 0 || !strings.Contains(out, "Usage:") || errOut != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"version", "--output", "text"}} {
		code, out, errOut := invoke(t, args...)
		if code != 0 || out != "rc v0.1.0 (abc123)\n" || errOut != "" {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

func TestVersionYAML(t *testing.T) {
	code, out, errOut := invoke(t, "version", "--output", "yaml")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var document struct {
		CLI        BuildInfo            `yaml:"cli"`
		GoVersion  string               `yaml:"goVersion"`
		Platform   string               `yaml:"platform"`
		Components []goProfilerIdentity `yaml:"components"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(out))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	if document.CLI != (BuildInfo{Version: "v0.1.0", Commit: "abc123"}) ||
		document.GoVersion != runtime.Version() || document.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("unexpected version document: %+v", document)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("unexpected trailing output: %v", err)
	}
}

func TestUsageFailures(t *testing.T) {
	for _, args := range [][]string{
		{"missing"}, {"--unknown"}, {"version", "extra"},
		{"help", "missing"}, {"help", "version", "extra"},
		{"version", "--output", "json"}, {"version", "--output"},
		{"completion"}, {"completion", "unknown"}, {"completion", "bash", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, errOut := invoke(t, args...)
			if code != 2 || out != "" || strings.Count(errOut, "rc: ") != 1 ||
				!strings.Contains(errOut, "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
			}
		})
	}
}

func TestShellCompletions(t *testing.T) {
	markers := map[string]string{
		"bash": "__start_rc", "zsh": "#compdef rc",
		"fish": "complete -c rc", "powershell": "Register-ArgumentCompleter",
	}
	for shell, marker := range markers {
		t.Run(shell, func(t *testing.T) {
			code, out, errOut := invoke(t, "completion", shell)
			if code != 0 || !strings.Contains(out, marker) || errOut != "" {
				t.Fatalf("code=%d script contains marker=%t stderr=%q", code, strings.Contains(out, marker), errOut)
			}
		})
	}
}

func TestCompletionCandidates(t *testing.T) {
	code, out, errOut := invoke(t, "__complete", "completion", "")
	if code != 0 || !strings.Contains(out, "bash\n") || !strings.Contains(out, "powershell\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output closed") }

func TestOutputFailure(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"version", "--output", "yaml"}, {"completion", "bash"}} {
		var errOut bytes.Buffer
		code := Execute(context.Background(), args, BuildInfo{}, IOStreams{Out: failingWriter{}, ErrOut: &errOut})
		if code != 1 || strings.Count(errOut.String(), "rc: ") != 1 ||
			!strings.Contains(errOut.String(), "output closed") || strings.Contains(errOut.String(), "Usage:") {
			t.Fatalf("%v: code=%d stderr=%q", args, code, errOut.String())
		}
	}
}

func TestCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	code := Execute(ctx, []string{"version"}, BuildInfo{}, IOStreams{Out: &out, ErrOut: &errOut})
	if code != 130 || out.Len() != 0 || errOut.String() != "rc: context canceled\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestCommandIsolationAndInjectedStreams(t *testing.T) {
	for _, version := range []string{"v0.1.0", "v0.2.0"} {
		var out bytes.Buffer
		root := NewRootCommand(BuildInfo{Version: version}, IOStreams{In: strings.NewReader("input"), Out: &out})
		root.AddCommand(&cobra.Command{
			Use: "echo", RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := io.Copy(cmd.OutOrStdout(), cmd.InOrStdin())
				return err
			},
		})
		root.SetArgs([]string{"echo"})
		if err := root.ExecuteContext(context.Background()); err != nil || out.String() != "input" {
			t.Fatalf("err=%v stdout=%q", err, out.String())
		}
		if root.Version != version || root.PersistentFlags().HasFlags() {
			t.Fatal("build metadata leaked or root flags became persistent")
		}
	}
	if code := Execute(context.Background(), []string{"version"}, BuildInfo{}, IOStreams{}); code != 0 {
		t.Fatalf("nil stream defaults: exit %d", code)
	}
}
