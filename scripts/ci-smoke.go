//go:build ignore

package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const bindingModule = "runtimeconditions.io/x/rc/common"
const profilerModule = "github.com/runtimeconditions/go-rc-profiler"

type options struct{ rc, fixture, version, commit, goVersion string }
type result struct{ out, stderr string }

func main() {
	var o options
	flag.StringVar(&o.rc, "rc", "", "built rc executable")
	flag.StringVar(&o.fixture, "fixture", "", "source workload fixture")
	flag.StringVar(&o.version, "version", "ci", "expected CLI version")
	flag.StringVar(&o.commit, "commit", "", "expected CLI commit")
	flag.StringVar(&o.goVersion, "go-version", "go1.25.0", "expected workload toolchain")
	flag.Parse()
	if err := smoke(o); err != nil {
		fmt.Fprintln(os.Stderr, "ci-smoke:", err)
		os.Exit(1)
	}
	fmt.Println("PASS: isolated executable, linked profiler, generation, validation, and failure handling")
}

func smoke(o options) error {
	if o.rc == "" || o.fixture == "" || o.commit == "" || flag.NArg() != 0 {
		return fmt.Errorf("--rc, --fixture, and --commit are required; positional arguments are unsupported")
	}
	tmp, err := os.MkdirTemp("", "rc-ci-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	binary := filepath.Join(tmp, "rc")
	if err := copyFile(o.rc, binary, 0755); err != nil {
		return err
	}
	workload := filepath.Join(tmp, "workload")
	if err := os.Mkdir(workload, 0755); err != nil {
		return err
	}
	for _, name := range []string{"go.mod", "go.sum", "main.go"} {
		if err := copyFile(filepath.Join(o.fixture, name), filepath.Join(workload, name), 0644); err != nil {
			return err
		}
	}
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-mod=readonly",
		"GOPATH="+filepath.Join(tmp, "go"), "GOMODCACHE="+filepath.Join(tmp, "go", "pkg", "mod"),
		"RC_GO_PROFILER_BIN="+filepath.Join(tmp, "must-not-be-used"))
	toolchain, err := command(0, workload, env, "go", "env", "GOVERSION")
	if err != nil {
		return err
	}
	if strings.TrimSpace(toolchain.out) != o.goVersion {
		return fmt.Errorf("analysis toolchain: got %q, want %s", toolchain.out, o.goVersion)
	}
	for _, args := range [][]string{{"mod", "download", "all"}, {"mod", "verify"}} {
		if _, err := command(0, workload, env, "go", args...); err != nil {
			return err
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		before, err := os.ReadFile(filepath.Join(o.fixture, name))
		if err != nil {
			return err
		}
		after, err := os.ReadFile(filepath.Join(workload, name))
		if err != nil || !bytes.Equal(before, after) {
			return fmt.Errorf("fixture dependency lock changed: %s: %v", name, err)
		}
	}
	// All profile commands must work using only the dependencies fetched above.
	env = append(env, "GOPROXY=off")
	rc := func(want int, args ...string) (result, error) {
		return command(want, workload, env, binary, args...)
	}
	build, err := buildinfo.ReadFile(binary)
	if err != nil {
		return err
	}
	dependencyVersion, dependencySum := "", ""
	for _, dependency := range build.Deps {
		if dependency.Path == profilerModule && dependency.Replace == nil {
			dependencyVersion, dependencySum = dependency.Version, dependency.Sum
		}
	}
	if dependencyVersion == "" || !strings.HasPrefix(dependencySum, "h1:") {
		return fmt.Errorf("executable must link a published, checksummed Go profiler module")
	}
	release, err := rc(0, "--version")
	if err != nil {
		return err
	}
	if release.out != fmt.Sprintf("rc %s (%s)\n", o.version, o.commit) || release.stderr != "" {
		return fmt.Errorf("incorrect linker metadata: %+v", release)
	}
	details, err := rc(0, "version", "--output", "yaml")
	if err != nil {
		return err
	}
	var identity struct {
		CLI        struct{ Version, Commit string }
		Components []struct {
			Name, Origin, Module, Version string
			Sum                           string `yaml:"moduleSum"`
		}
	}
	if err := yaml.Unmarshal([]byte(details.out), &identity); err != nil {
		return err
	}
	if details.stderr != "" || identity.CLI.Version != o.version || identity.CLI.Commit != o.commit ||
		len(identity.Components) != 1 || identity.Components[0].Name != "go-rc-profiler" ||
		identity.Components[0].Origin != "linked" || identity.Components[0].Module != profilerModule ||
		identity.Components[0].Version != dependencyVersion || identity.Components[0].Sum != dependencySum {
		return fmt.Errorf("incorrect linked component metadata: %s", details.out)
	}
	var expected string
	output := filepath.Join(workload, "profile.yaml")
	for _, route := range [][]string{{"profile"}, {"profile", "go"}} {
		args := append(append([]string{}, route...), "generate", "--dir", workload, "--name", "ci-fixture",
			"--workload-uri", "https://example.com/ci-fixture", "--workload-version", "1.0.0")
		generated, err := rc(0, args...)
		if err != nil {
			return err
		}
		if generated.stderr != "" {
			return fmt.Errorf("generation stderr: %s", generated.stderr)
		}
		if err := checkProfile(generated.out); err != nil {
			return err
		}
		if expected != "" && expected != generated.out {
			return fmt.Errorf("default and explicit Go routes produced different profiles")
		}
		expected = generated.out
		written, err := rc(0, append(args, "--out", output)...)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != expected || written.out != "" || written.stderr != "" {
			return fmt.Errorf("incorrect --out result: %+v: %v", written, err)
		}
		for _, validation := range [][]string{
			{"validate-extension", "--dir", workload, "--package", bindingModule},
			{"validate-extensions", "--dir", workload},
		} {
			if _, err := rc(0, append(append([]string{}, route...), validation...)...); err != nil {
				return err
			}
		}
	}
	source, err := os.ReadFile(filepath.Join(workload, "main.go"))
	if err != nil {
		return err
	}
	invalid := strings.Replace(string(source), "common.KeyValueEngineRedis", `common.KeyValueEngine("invalid-ci-engine")`, 1)
	if err := os.WriteFile(filepath.Join(workload, "main.go"), []byte(invalid), 0644); err != nil {
		return err
	}
	for _, route := range [][]string{{"profile"}, {"profile", "go"}} {
		failed, err := rc(1, append(append([]string{}, route...), "generate", "--dir", workload, "--out", output)...)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != expected || failed.out != "" ||
			!strings.Contains(failed.stderr, "invalid-ci-engine") || strings.Contains(failed.stderr, "Usage:") {
			return fmt.Errorf("invalid declaration must fail without replacing output: %+v: %v", failed, err)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(workload, ".runtimeconditions-profile-*"))
	if err != nil || len(leftovers) != 0 {
		return fmt.Errorf("temporary output files remain: %v: %v", leftovers, err)
	}
	return nil
}

func checkProfile(data string) error {
	var profile struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string
		Metadata   struct{ Name string }
		Workload   struct{ URI, Version string }
		Extensions []string
		Conditions []struct {
			Kind      string
			Interface map[string]string
		}
	}
	if err := yaml.Unmarshal([]byte(data), &profile); err != nil {
		return err
	}
	if profile.APIVersion != "runtimeconditions.io/v1alpha1" || profile.Kind != "RuntimeConditionsProfile" ||
		profile.Metadata.Name != "ci-fixture" || profile.Workload.URI != "https://example.com/ci-fixture" ||
		profile.Workload.Version != "1.0.0" || len(profile.Extensions) != 1 ||
		!strings.HasPrefix(profile.Extensions[0], "https://runtimeconditions.io/extensions/rc/common-integrations") ||
		len(profile.Conditions) != 1 || profile.Conditions[0].Kind != "cache" ||
		profile.Conditions[0].Interface["type"] != "key_value" || profile.Conditions[0].Interface["engine"] != "redis" {
		return fmt.Errorf("unexpected generated profile:\n%s", data)
	}
	return nil
}

func command(want int, dir string, env []string, executable string, args ...string) (result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, 5*time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			return result{}, fmt.Errorf("%s %v: %w", executable, args, err)
		}
		code = exit.ExitCode()
	}
	got := result{out.String(), stderr.String()}
	if code != want {
		return got, fmt.Errorf("%s %v: exit %d, want %d\nstdout: %s\nstderr: %s", executable, args, code, want, got.out, got.stderr)
	}
	return got, nil
}

func copyFile(source, target string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, mode)
}
