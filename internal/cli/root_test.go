package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestMain(m *testing.M) {
	if os.Getenv("RC_CLI_TEST_HELPER") == "1" {
		switch os.Getenv("RC_CLI_TEST_HELPER_MODE") {
		case "exit":
			fmt.Fprintln(os.Stderr, "backend diagnostic")
			os.Exit(23)
		case "signal":
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		case "wait":
			child := exec.Command(os.Args[0])
			child.Env = append(os.Environ(), "RC_CLI_TEST_HELPER_MODE=sleep")
			if err := child.Start(); err != nil {
				panic(err)
			}
			if err := os.WriteFile(os.Getenv("RC_CLI_TEST_READY"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
				panic(err)
			}
			time.Sleep(30 * time.Second)
		case "sleep":
			time.Sleep(30 * time.Second)
		default:
			input, _ := io.ReadAll(os.Stdin)
			cwd, _ := os.Getwd()
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"args": os.Args[1:], "input": string(input), "cwd": cwd, "value": os.Getenv("RC_CLI_TEST_VALUE"),
			})
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeGoProfiler(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "go profiler")
	if err := os.WriteFile(path, binary, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RC_GO_PROFILER_BIN", path)
	t.Setenv("RC_CLI_TEST_HELPER", "1")
	t.Setenv("RC_CLI_TEST_HELPER_MODE", "args")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return path
}

func TestProfileArgumentAndStreamPreservation(t *testing.T) {
	fakeGoProfiler(t)
	t.Setenv("RC_CLI_TEST_VALUE", "inherited")
	cwd, _ := os.Getwd()
	cases := [][]string{
		{"generate", "-dir", "path with spaces", "--name", "first", "--name", "second", "--", "tail"},
		{"go", "generate", "-dir", "path with spaces", "--name", "first", "--name", "second", "--", "tail"},
		{"go", "--help"}, {"go", "--version"}, {"--version"}, {"version"},
		{"validate-extension", "-package", "example/binding"}, {"validate-extensions"}, {"go"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), append([]string{"profile"}, args...), BuildInfo{},
			IOStreams{In: strings.NewReader("input bytes"), Out: &out, ErrOut: &errOut})
		var got struct {
			Args              []string
			Input, Cwd, Value string
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("%v: %v; %q", args, err, out.String())
		}
		want := args
		if args[0] == "go" {
			want = args[1:]
		}
		if code != 0 || errOut.Len() != 0 || !reflect.DeepEqual(got.Args, want) ||
			got.Input != "input bytes" || got.Cwd != cwd || got.Value != "inherited" {
			t.Fatalf("%v: code=%d got=%+v stderr=%q", args, code, got, errOut.String())
		}
	}
}

func TestProfileHostHelpAndUnknownLanguage(t *testing.T) {
	t.Setenv("RC_GO_PROFILER_BIN", "/missing/profiler")
	for _, args := range [][]string{{"profile"}, {"profile", "--help"}, {"profile", "-h"}, {"help", "profile"}} {
		code, out, errOut := invoke(t, args...)
		if code != 0 || !strings.Contains(out, "rc profile go") || errOut != "" {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
	code, out, errOut := invoke(t, "profile", "java", "generate")
	if code != 2 || out != "" || !strings.Contains(errOut, "unsupported profiler language") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	code, out, _ = invoke(t, "__complete", "profile", "")
	if code != 0 || !strings.Contains(out, "go\n") {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
}

func TestProfileExitStatus(t *testing.T) {
	fakeGoProfiler(t)
	t.Setenv("RC_CLI_TEST_HELPER_MODE", "exit")
	code, out, errOut := invoke(t, "profile", "go", "--unknown")
	if code != 23 || out != "" || errOut != "backend diagnostic\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	t.Setenv("RC_CLI_TEST_HELPER_MODE", "signal")
	code, _, _ = invoke(t, "profile", "version")
	if code != 143 {
		t.Fatalf("signaled backend: exit=%d", code)
	}
}

func TestProfileCancellationStopsDescendants(t *testing.T) {
	fakeGoProfiler(t)
	t.Setenv("RC_CLI_TEST_HELPER_MODE", "wait")
	ready := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("RC_CLI_TEST_READY", ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Execute(ctx, []string{"profile", "generate"}, BuildInfo{}, IOStreams{}) }()
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for childPID == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(ready); err == nil {
			childPID, _ = strconv.Atoi(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("canceled profiler: exit=%d", code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("profiler cancellation timed out")
	}
	if childPID == 0 {
		t.Fatal("profiler did not start its child")
	}
	defer syscall.Kill(childPID, syscall.SIGKILL)
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(childPID, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("profiler descendant survived cancellation")
}

func TestGoProfilerOverrideAndVersionIdentity(t *testing.T) {
	path := fakeGoProfiler(t)
	path, _ = filepath.EvalSymlinks(path)
	code, out, errOut := invoke(t, "version", "--output", "yaml")
	var doc struct {
		Components []goProfilerIdentity `yaml:"components"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if code != 0 || errOut != "" || len(doc.Components) != 1 ||
		doc.Components[0].Origin != "override" || doc.Components[0].Path != path || len(doc.Components[0].SHA256) != 64 {
		t.Fatalf("code=%d document=%+v stderr=%q", code, doc, errOut)
	}
	t.Setenv("RC_GO_PROFILER_BIN", "relative")
	if code, _, _ := invoke(t, "profile", "generate"); code != 1 {
		t.Fatalf("relative override: exit=%d", code)
	}
	self, _ := os.Executable()
	t.Setenv("RC_GO_PROFILER_BIN", self)
	if code, _, errOut := invoke(t, "profile", "generate"); code != 1 || !strings.Contains(errOut, "cannot resolve to the rc executable") {
		t.Fatalf("recursive override: code=%d stderr=%q", code, errOut)
	}
}

func TestBundledGoProfilerDiscoveryAndIntegrity(t *testing.T) {
	for _, layout := range []string{"libexec", "lib"} {
		t.Run(layout, func(t *testing.T) {
			prefix := t.TempDir()
			self := filepath.Join(prefix, "bin", "rc")
			binary := filepath.Join(prefix, layout, "rc-cli", "go-rc-profiler")
			manifest := filepath.Join(prefix, "share", "rc-cli", "components.lock.yaml")
			for _, path := range []string{self, binary, manifest} {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(self, []byte("cli"), 0755); err != nil {
				t.Fatal(err)
			}
			data := []byte("profiler")
			if err := os.WriteFile(binary, data, 0755); err != nil {
				t.Fatal(err)
			}
			lockBytes, err := os.ReadFile("../../distribution/components.lock.yaml")
			if err != nil {
				t.Fatal(err)
			}
			var lock goProfilerLock
			if err := yaml.Unmarshal(lockBytes, &lock); err != nil {
				t.Fatal(err)
			}
			key := runtime.GOOS + "/" + runtime.GOARCH
			asset := lock.GoProfiler.Platforms[key]
			asset.ExecutableSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
			lock.GoProfiler.Platforms[key] = asset
			lockBytes, err = yaml.Marshal(lock)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, lockBytes, 0644); err != nil {
				t.Fatal(err)
			}
			found, err := locateGoProfiler(self, "")
			if err != nil || found.Origin != "bundled" || found.Version != lock.GoProfiler.Version {
				t.Fatalf("identity=%+v err=%v", found, err)
			}
			if err := os.WriteFile(binary, []byte("tampered"), 0755); err != nil {
				t.Fatal(err)
			}
			if _, err := locateGoProfiler(self, ""); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
				t.Fatalf("tampered bundle accepted: %v", err)
			}
		})
	}
}
