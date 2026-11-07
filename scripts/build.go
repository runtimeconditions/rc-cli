//go:build ignore

// Build the single rc executable, including its linked Go profiler and verified
// core schema. Nothing is downloaded or installed when the resulting rc runs.
// go run ./scripts/build.go --output dist/rc --version v0.1.0 --commit <commit>
// For an offline build, add --core-schema /path/to/the/approved/schema.yaml.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

const coreSchemaURL = "https://github.com/runtimeconditions/spec/releases/download/v0.4.0/runtimeconditions.profile.schema.yaml"
const coreSchemaSHA256 = "96d430c7936fcf7334aa9613f63bb306592e07cf56fe4154af57933d2c1480e6"

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, "build rc:", err)
		os.Exit(1)
	}
}

func build() error {
	output := flag.String("output", "dist/rc", "output executable path")
	version := flag.String("version", "dev", "CLI release tag")
	commit := flag.String("commit", "unknown", "CLI source commit")
	schemaPath := flag.String("core-schema", "", "approved local core schema; otherwise fetched during build")
	flag.Parse()
	safe := regexp.MustCompile(`^[A-Za-z0-9.+-]+$`)
	if flag.NArg() != 0 || !safe.MatchString(*version) || !safe.MatchString(*commit) {
		return errors.New("expected safe version/commit values and no positional arguments")
	}
	var schema []byte
	var err error
	if *schemaPath != "" {
		schema, err = os.ReadFile(*schemaPath)
	} else {
		client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("invalid schema download redirect")
			}
			return nil
		}}
		response, fetchErr := client.Get(coreSchemaURL)
		if fetchErr != nil {
			return fetchErr
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("schema download returned %s", response.Status)
		}
		schema, err = io.ReadAll(io.LimitReader(response.Body, 64*1024*1024+1))
	}
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(schema)) != coreSchemaSHA256 {
		return errors.New("core schema checksum mismatch")
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		return err
	}
	linkerFlags := "-X main.version=" + *version + " -X main.commit=" + *commit +
		" -X github.com/runtimeconditions/go-rc-profiler/extensioncheck.installedCoreSchemaBase64=" +
		base64.StdEncoding.EncodeToString(schema)
	command := exec.Command("go", "build", "-trimpath", "-ldflags", linkerFlags, "-o", *output, "./cmd/rc")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}
