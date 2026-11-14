// Package plugins finds optional language profilers and checks that they fit this rc.
//
// A language add-on is a separate executable. rc never enumerates arbitrary files:
// each supported language names the executables it looks for.
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/runtimeconditions/rc-cli/internal/process"
)

// Language is one optional profiler rc can run.
type Language struct {
	// ID is the command word: rc profile <ID> ...
	ID string
	// Display is the name used in messages.
	Display string
	// Executables are looked up on PATH, in order.
	Executables []string
	// Runtime lists the programs an add-on installed from the language's own tooling needs on PATH,
	// such as node. A system package that carries its own runtime does not need them.
	Runtime []string
	// OverrideEnv names an environment variable that holds an absolute executable path.
	OverrideEnv string
	// InstallHint is the command that installs the add-on with the language's own tooling.
	InstallHint string
}

// TypeScript is the TypeScript profiler, published to npm as @runtimeconditions/typescript-profiler.
var TypeScript = Language{
	ID:          "typescript",
	Display:     "TypeScript",
	Executables: []string{"rc-profiler-typescript", "runtimeconditions-typescript-profiler"},
	Runtime:     []string{"node"},
	OverrideEnv: "RC_TYPESCRIPT_PROFILER",
	InstallHint: "npm install --global @runtimeconditions/typescript-profiler",
}

// Languages returns the optional languages rc supports.
func Languages() []Language { return []Language{TypeScript} }

// Finder looks up executables. Tests replace both functions.
type Finder struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
}

// Found is an installed add-on.
type Found struct {
	Language Language
	Path     string
	// Origin is "override" for an explicit environment path, or "path".
	Origin string
}

// NotInstalledError reports that no executable for the language was found.
type NotInstalledError struct{ Language Language }

func (e NotInstalledError) Error() string {
	l := e.Language
	return fmt.Sprintf("the %s profiler is not installed: %s was not found on PATH; install it with: %s; or set %s to its absolute path",
		l.Display, strings.Join(l.Executables, " or "), l.InstallHint, l.OverrideEnv)
}

// RuntimeMissingError reports that the add-on could not run and a program it needs is not on PATH.
type RuntimeMissingError struct {
	Language Language
	Runtime  string
}

func (e RuntimeMissingError) Error() string {
	return fmt.Sprintf("the %s profiler needs %s, which was not found on PATH", e.Language.Display, e.Runtime)
}

// Find returns the add-on for a language. An explicit override wins over PATH.
func (f Finder) Find(language Language) (Found, error) {
	found := Found{Language: language}
	if override := f.Getenv(language.OverrideEnv); override != "" {
		if !filepath.IsAbs(override) {
			return Found{}, fmt.Errorf("%s must be an absolute path, got %q", language.OverrideEnv, override)
		}
		path, err := f.LookPath(override)
		if err != nil {
			return Found{}, fmt.Errorf("%s=%s is not an executable file: %w", language.OverrideEnv, override, err)
		}
		found.Path, found.Origin = path, "override"
	} else {
		for _, name := range language.Executables {
			if path, err := f.LookPath(name); err == nil {
				found.Path, found.Origin = path, "path"
				break
			}
		}
		if found.Path == "" {
			return Found{}, NotInstalledError{language}
		}
	}
	return found, nil
}

// MissingRuntime returns the first program the add-on needs that is not on PATH, or "" when
// they are all there. An add-on that ships its own runtime, such as the system package of the
// TypeScript profiler, runs without them, so this is only asked after an add-on failed to run.
func (f Finder) MissingRuntime(language Language) string {
	for _, runtime := range language.Runtime {
		if _, err := f.LookPath(runtime); err != nil {
			return runtime
		}
	}
	return ""
}

// Info is what an add-on reports about itself for `--version --json`.
type Info struct {
	Name        string   `json:"name" yaml:"name"`
	Version     string   `json:"version" yaml:"version"`
	CoreSchemas []string `json:"coreSchemas" yaml:"coreSchemas"`
}

// Supports reports whether the add-on emits the core profile schema version.
func (i Info) Supports(coreSchema string) bool { return slices.Contains(i.CoreSchemas, coreSchema) }

const (
	describeTimeout = 15 * time.Second
	describeLimit   = 64 << 10
)

// capped refuses output beyond a limit so a misbehaving add-on cannot fill memory.
type capped struct {
	bytes.Buffer
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.Len()+len(p) > c.limit {
		return 0, errors.New("output is too large")
	}
	return c.Buffer.Write(p)
}

// Describe runs `<path> --version --json` with a time and size limit and reads the answer.
func Describe(ctx context.Context, runner process.Runner, path string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, describeTimeout)
	defer cancel()
	out, errOut := &capped{limit: describeLimit}, &capped{limit: describeLimit}
	code, err := runner.Run(ctx, process.Spec{Path: path, Args: []string{"--version", "--json"}, Stdout: out, Stderr: errOut})
	if err != nil {
		return Info{}, fmt.Errorf("could not run %s --version --json: %w", path, err)
	}
	if code != 0 {
		return Info{}, fmt.Errorf("%s --version --json exited with status %d: %s", path, code, strings.TrimSpace(errOut.String()))
	}
	var info Info
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		return Info{}, fmt.Errorf("%s --version --json did not print the expected JSON: %w", path, err)
	}
	if info.Name == "" || info.Version == "" {
		return Info{}, fmt.Errorf("%s --version --json must report name and version", path)
	}
	return info, nil
}
