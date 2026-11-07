package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

type profilerExitError int

func (e profilerExitError) Error() string { return fmt.Sprintf("profiler exited with status %d", e) }

type goProfilerIdentity struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path,omitempty"`
	Origin    string `yaml:"origin"`
	Version   string `yaml:"version,omitempty"`
	Commit    string `yaml:"commit,omitempty"`
	SHA256    string `yaml:"sha256,omitempty"`
	Module    string `yaml:"module,omitempty"`
	ModuleSum string `yaml:"moduleSum,omitempty"`
}

type goProfilerAsset struct {
	URL              string `yaml:"url"`
	ArchiveSHA256    string `yaml:"archiveSHA256"`
	ExecutableSHA256 string `yaml:"executableSHA256"`
}

type goProfilerLock struct {
	APIVersion string `yaml:"apiVersion"`
	GoProfiler struct {
		Version    string                     `yaml:"version"`
		Commit     string                     `yaml:"commit"`
		Protocol   string                     `yaml:"protocol"`
		CoreSchema map[string]string          `yaml:"coreSchema"`
		Platforms  map[string]goProfilerAsset `yaml:"platforms"`
	} `yaml:"goProfiler"`
}

var errGoProfilerMissing = errors.New("Go profiler unavailable; install the complete rc-cli bundle or set RC_GO_PROFILER_BIN to an absolute executable path")

func newProfileCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "profile [go] [profiler arguments...]",
		Short: "Generate profiles with the built-in Go profiler",
		Long: "Generate profiles with the Go profiler linked into rc. Go analysis requires a working Go toolchain.\n" +
			"Arguments after go, or an unqualified Go operation, pass unchanged to the profiler.",
		Example:            "  rc profile generate -dir ./service\n  rc profile go generate -dir ./service\n  rc profile go --help",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 && strings.HasPrefix("go", prefix) {
				return []string{"go"}, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
				return cmd.Help()
			}
			switch args[0] {
			case "go":
				args = args[1:]
			case "generate", "validate-extension", "validate-extensions", "version":
			default:
				if !strings.HasPrefix(args[0], "-") {
					return usageError{fmt.Errorf("unsupported profiler language %q; Go is included by default", args[0])}
				}
			}
			profiler, err := findGoProfiler()
			if err != nil {
				return err
			}
			return runGoProfiler(cmd.Context(), profiler.Path, args,
				IOStreams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), ErrOut: cmd.ErrOrStderr()})
		},
	}
}

func findGoProfiler() (goProfilerIdentity, error) {
	if os.Getenv("RC_GO_PROFILER_BIN") == "" {
		return linkedGoProfilerIdentity(), nil
	}
	self, err := os.Executable()
	if err != nil {
		return goProfilerIdentity{}, err
	}
	return locateGoProfiler(self, os.Getenv("RC_GO_PROFILER_BIN"))
}

func locateGoProfiler(self, override string) (goProfilerIdentity, error) {
	self, err := filepath.EvalSymlinks(self)
	if err != nil {
		return goProfilerIdentity{}, err
	}
	if override != "" {
		if !filepath.IsAbs(override) {
			return goProfilerIdentity{}, errors.New("RC_GO_PROFILER_BIN must be an absolute path")
		}
		return identifyGoProfiler(override, self, "override")
	}
	prefix := filepath.Dir(filepath.Dir(self))
	paths := []string{filepath.Join(prefix, "libexec", "rc-cli", "go-rc-profiler"), filepath.Join(prefix, "lib", "rc-cli", "go-rc-profiler")}
	manifest := filepath.Join(prefix, "share", "rc-cli", "components.lock.yaml")
	file, err := os.Open(manifest)
	if err == nil {
		defer file.Close()
		decoder := yaml.NewDecoder(io.LimitReader(file, 64*1024+1))
		decoder.KnownFields(true)
		var lock goProfilerLock
		if stat, statErr := file.Stat(); statErr != nil || stat.Size() > 64*1024 {
			return goProfilerIdentity{}, errors.New("invalid or oversized component lock")
		}
		if err = decoder.Decode(&lock); err != nil {
			return goProfilerIdentity{}, fmt.Errorf("component lock: %w", err)
		}
		if decoder.Decode(new(any)) != io.EOF || lock.APIVersion != "runtimeconditions.io/cli-components/v1alpha1" ||
			lock.GoProfiler.Protocol != "go-profiler-argv/v1" || lock.GoProfiler.Version == "" || lock.GoProfiler.Commit == "" {
			return goProfilerIdentity{}, errors.New("unsupported or incomplete component lock")
		}
		asset, ok := lock.GoProfiler.Platforms[runtime.GOOS+"/"+runtime.GOARCH]
		if !ok {
			return goProfilerIdentity{}, errors.New("component lock does not support this platform")
		}
		for _, path := range paths {
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			}
			identity, err := identifyGoProfiler(path, self, "bundled")
			if err != nil {
				return identity, err
			}
			if identity.SHA256 != asset.ExecutableSHA256 {
				return identity, errors.New("bundled Go profiler checksum mismatch; reinstall rc-cli")
			}
			identity.Version, identity.Commit = lock.GoProfiler.Version, lock.GoProfiler.Commit
			return identity, nil
		}
		return goProfilerIdentity{}, errors.New("component lock exists but bundled Go profiler is missing")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return goProfilerIdentity{}, err
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return goProfilerIdentity{}, errors.New("bundled Go profiler has no component lock")
		}
	}
	path, err := exec.LookPath("go-rc-profiler")
	if err != nil {
		return goProfilerIdentity{}, errGoProfilerMissing
	}
	return identifyGoProfiler(path, self, "path")
}

func identifyGoProfiler(path, self, origin string) (goProfilerIdentity, error) {
	identity := goProfilerIdentity{Name: "go-rc-profiler", Origin: origin}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return identity, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return identity, err
	}
	if origin == "bundled" {
		relative, err := filepath.Rel(filepath.Dir(filepath.Dir(self)), path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return identity, errors.New("bundled Go profiler escapes the installation prefix")
		}
	}
	stat, err := os.Stat(path)
	if err != nil {
		return identity, err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0111 == 0 {
		return identity, errors.New("Go profiler must be a regular executable file")
	}
	if current, err := os.Stat(self); err != nil || os.SameFile(stat, current) {
		return identity, errors.New("Go profiler cannot resolve to the rc executable")
	}
	file, err := os.Open(path)
	if err != nil {
		return identity, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return identity, err
	}
	identity.Path, identity.SHA256 = path, fmt.Sprintf("%x", hash.Sum(nil))
	return identity, nil
}

func runGoProfiler(ctx context.Context, path string, args []string, streams IOStreams) error {
	if path == "" {
		return runLinkedGoProfiler(ctx, args, streams)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = streams.In, streams.Out, streams.ErrOut
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}
		return profilerExitError(code)
	}
	return err
}

func goProfilerComponents() ([]goProfilerIdentity, error) {
	identity, err := findGoProfiler()
	if errors.Is(err, errGoProfilerMissing) {
		return []goProfilerIdentity{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []goProfilerIdentity{identity}, nil
}
