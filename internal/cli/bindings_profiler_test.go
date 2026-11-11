package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

func TestGeneratedLinkedMetadataAcceptedByProfiler(t *testing.T) {
	o, _ := bindingParityFixture(t)
	// Query the selected dependency, rather than inventing a profiler version or sum.
	data, err := exec.Command(o.Go, "list", "-m", "-json", goProfilerModule).Output()
	if err != nil {
		t.Fatal(err)
	}
	var dependency struct {
		Version, Sum string
		Replace      *json.RawMessage
	}
	if err := json.Unmarshal(data, &dependency); err != nil {
		t.Fatal(err)
	}
	if dependency.Replace != nil {
		t.Fatal("regression test requires the published profiler dependency")
	}
	identity, err := orchestrator.NewLinkedGoProfilerIdentity(dependency.Version, dependency.Sum)
	if err != nil {
		t.Fatal(err)
	}
	o.LinkedGoProfiler, o.Output = &identity, filepath.Join(t.TempDir(), "generated")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Execute(context.Background(), cwd, "generate", o, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(o.Output, "bindings", "fixture", "go")
	data, err = os.ReadFile(filepath.Join(directory, "runtimeconditions.binding-release.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var release struct {
		Package    struct{ Coordinate, Version string }
		Provenance struct{ Profiler orchestrator.ProfilerIdentity }
	}
	if err := yaml.Unmarshal(data, &release); err != nil {
		t.Fatal(err)
	}
	if err := identity.Match(release.Provenance.Profiler); err != nil {
		t.Fatal("generated linked profiler identity differs", err)
	}

	// Install the generated package through Go's proxy protocol, without replaces.
	proxy := t.TempDir()
	path, err := module.EscapePath(release.Package.Coordinate)
	if err != nil {
		t.Fatal(err)
	}
	version, err := module.EscapeVersion(release.Package.Version)
	if err != nil {
		t.Fatal(err)
	}
	versionDirectory := filepath.Join(proxy, filepath.FromSlash(path), "@v")
	if err := os.MkdirAll(versionDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := modzip.CreateFromDir(&archive, module.Version{Path: release.Package.Coordinate, Version: release.Package.Version}, directory); err != nil {
		t.Fatal(err)
	}
	goMod, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := json.Marshal(struct{ Version, Time string }{release.Package.Version, "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{version + ".zip": archive.Bytes(), version + ".mod": goMod, version + ".info": info, "list": []byte(release.Package.Version + "\n")} {
		if err := os.WriteFile(filepath.Join(versionDirectory, name), contents, 0644); err != nil {
			t.Fatal(err)
		}
	}
	consumer := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod":  fmt.Sprintf("module example.test/consumer\n\ngo %s\n\nrequire %s %s\n", strings.TrimPrefix(runtime.Version(), "go"), release.Package.Coordinate, release.Package.Version),
		"main.go": fmt.Sprintf("package main\nimport _ %q\nfunc main() {}\n", release.Package.Coordinate),
	} {
		if err := os.WriteFile(filepath.Join(consumer, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cache, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Go extracts read-only module directories; make only this test cache writable
	// before testing.TempDir removes it.
	t.Cleanup(func() {
		err := filepath.WalkDir(cache, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(path, 0755)
			}
			return nil
		})
		if err != nil {
			t.Error("make temporary module cache removable", err)
		}
	})
	t.Setenv("GOMODCACHE", cache)
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String())
	t.Setenv("GOSUMDB", "off") // Only this test's private module proxy is available.
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-mod=readonly -buildvcs=false")
	packages, err := extensioncheck.ValidateImportedGoPackages(context.Background(), consumer, []string{release.Package.Coordinate})
	if err != nil {
		t.Fatal("linked profiler rejected generated binding metadata", err)
	}
	if len(packages) != 1 || packages[0].ImportPath != release.Package.Coordinate || packages[0].Version != release.Package.Version || !strings.HasPrefix(packages[0].Dir, cache+string(filepath.Separator)) {
		t.Fatalf("profiler did not validate the installed versioned module: %+v", packages)
	}
	if _, err := os.Stat(filepath.Join(consumer, "go.sum")); err != nil {
		t.Fatal("consumer did not retain module checksums", err)
	}
}
