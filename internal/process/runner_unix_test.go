//go:build unix

package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "addon")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunReturnsTheExitStatusAndPassesArgumentsStreamsDirAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := script(t, `printf '%s|' "$@"; pwd; echo "env=$RC_TEST" >&2; read line; echo "in=$line"; exit 3`)
	var out, errOut bytes.Buffer
	code, err := OS{}.Run(context.Background(), Spec{
		Path: path, Args: []string{"a b", "--", "-x", ""}, Dir: dir, Env: []string{"RC_TEST=yes"},
		Stdin: strings.NewReader("typed\n"), Stdout: &out, Stderr: &errOut,
	})
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if got, want := out.String(), "a b|--|-x||"+resolved+"\nin=typed\n"; got != want {
		t.Fatalf("stdout %q, want %q", got, want)
	}
	if errOut.String() != "env=yes\n" {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRunReportsAKilledChildAsTheShellDoes(t *testing.T) {
	code, err := OS{}.Run(context.Background(), Spec{Path: script(t, `kill -TERM $$`)})
	if err != nil || code != 128+int(syscall.SIGTERM) {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestRunFailsWhenTheChildCannotStart(t *testing.T) {
	code, err := OS{}.Run(context.Background(), Spec{Path: filepath.Join(t.TempDir(), "missing")})
	if err == nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestCancellationStopsTheChildAndItsDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	path := script(t, `sleep 60 & echo $! > "`+pidFile+`"; wait`)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := OS{}.Run(ctx, Spec{Path: path})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d is still running", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
