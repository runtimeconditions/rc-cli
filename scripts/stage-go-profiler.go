//go:build ignore

// Stage a verified published Go profiler without rebuilding it.
// From the repository root:
// go run ./scripts/stage-go-profiler.go --archive /path/to/release.tar.gz --prefix /new/staging/root
// Use --platform linux/arm64 for cross-packaging and --layout linux for Debian paths.
// Download the archive URL from distribution/components.lock.yaml before running this helper.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"go.yaml.in/yaml/v3"
)

type asset struct {
	URL              string `yaml:"url"`
	ArchiveSHA256    string `yaml:"archiveSHA256"`
	ExecutableSHA256 string `yaml:"executableSHA256"`
}

type componentLock struct {
	APIVersion string `yaml:"apiVersion"`
	Extensions struct {
		Repository string `yaml:"repository"`
		Commit     string `yaml:"commit"`
	} `yaml:"extensions"`
	GoProfiler struct {
		Version    string            `yaml:"version"`
		Commit     string            `yaml:"commit"`
		Protocol   string            `yaml:"protocol"`
		CoreSchema map[string]string `yaml:"coreSchema"`
		Platforms  map[string]asset  `yaml:"platforms"`
	} `yaml:"goProfiler"`
}

func main() {
	if err := stage(); err != nil {
		fmt.Fprintln(os.Stderr, "stage Go profiler:", err)
		os.Exit(1)
	}
}

func stage() error {
	lockPath := flag.String("lock", "distribution/components.lock.yaml", "reviewed component lock")
	archivePath := flag.String("archive", "", "previously downloaded release archive")
	prefix := flag.String("prefix", "", "new staging directory; must not exist")
	platform := flag.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "target OS/architecture")
	layout := flag.String("layout", "archive", "archive (also Homebrew) or linux (Debian)")
	flag.Parse()
	if *archivePath == "" || *prefix == "" || flag.NArg() != 0 {
		return errors.New("--archive and --prefix are required; no positional arguments")
	}
	toolDir := "libexec"
	if *layout == "linux" {
		toolDir = "lib"
	} else if *layout != "archive" {
		return errors.New("--layout must be archive or linux")
	}
	lockBytes, err := os.ReadFile(*lockPath)
	if err != nil {
		return err
	}
	if len(lockBytes) > 64*1024 {
		return errors.New("component lock exceeds 64 KiB")
	}
	var lock componentLock
	decoder := yaml.NewDecoder(bytes.NewReader(lockBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&lock); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF || lock.APIVersion != "runtimeconditions.io/cli-components/v1alpha1" ||
		lock.GoProfiler.Protocol != "go-profiler-argv/v1" || lock.GoProfiler.Version == "" || lock.GoProfiler.Commit == "" {
		return errors.New("unsupported or incomplete component lock")
	}
	target, ok := lock.GoProfiler.Platforms[*platform]
	if !ok {
		return fmt.Errorf("no locked profiler for %s", *platform)
	}
	archive, err := os.Open(*archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	stat, err := archive.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 64*1024*1024 {
		return errors.New("archive must be a regular file no larger than 64 MiB")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, archive); err != nil {
		return err
	}
	if fmt.Sprintf("%x", digest.Sum(nil)) != target.ArchiveSHA256 {
		return errors.New("release archive checksum mismatch")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(io.LimitReader(compressed, 130*1024*1024))
	files := make(map[string][]byte)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		limit := int64(128 * 1024 * 1024)
		if header.Name == "README.md" {
			limit = 1024 * 1024
		} else if header.Name != "go-rc-profiler" {
			return fmt.Errorf("unexpected archive entry %q", header.Name)
		}
		if header.Typeflag != tar.TypeReg || header.Size > limit || files[header.Name] != nil {
			return fmt.Errorf("invalid or duplicate archive entry %q", header.Name)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		files[header.Name] = data
	}
	binary := files["go-rc-profiler"]
	if len(binary) == 0 || len(files["README.md"]) == 0 {
		return errors.New("archive must contain the profiler and README.md")
	}
	if fmt.Sprintf("%x", sha256.Sum256(binary)) != target.ExecutableSHA256 {
		return errors.New("extracted profiler checksum mismatch")
	}
	destination, err := filepath.Abs(*prefix)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("--prefix must name a new directory")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(filepath.Dir(destination), ".rc-go-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	outputs := map[string][]byte{
		filepath.Join(toolDir, "rc-cli", "go-rc-profiler"): binary,
		"share/rc-cli/components.lock.yaml":                lockBytes,
		"share/doc/rc-cli/go-profiler/README.md":           files["README.md"],
	}
	for relative, data := range outputs {
		path := filepath.Join(temporary, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if filepath.Base(path) == "go-rc-profiler" {
			mode = 0755
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			return err
		}
	}
	if *platform == runtime.GOOS+"/"+runtime.GOARCH {
		path := filepath.Join(temporary, toolDir, "rc-cli", "go-rc-profiler")
		banner, err := exec.Command(path, "--version").Output()
		expected := fmt.Sprintf("go-rc-profiler %s (%s)\n", lock.GoProfiler.Version, lock.GoProfiler.Commit)
		if err != nil || string(banner) != expected {
			return errors.New("profiler version does not match the locked release")
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	fmt.Println(destination)
	return nil
}
