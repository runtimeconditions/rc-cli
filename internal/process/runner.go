// Package process runs the subprocesses behind optional language add-ons.
package process

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// terminationGrace is how long a child gets to exit after it is asked to stop.
const terminationGrace = 5 * time.Second

// Spec describes one subprocess invocation. The arguments are passed as a slice and
// never through a shell. An empty Dir and a nil Env inherit the caller's.
type Spec struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Runner runs a subprocess to completion. A child that exits with a nonzero status
// is not an error: its status is returned. Errors mean the child could not be started,
// its I/O failed, or the context ended first.
type Runner interface {
	Run(ctx context.Context, spec Spec) (int, error)
}

// OS runs real subprocesses. On Unix the child leads its own process group. When the
// context ends it receives SIGTERM, and after a grace period the whole group is killed.
type OS struct{}

// Run starts the child and waits for it.
func (OS) Run(ctx context.Context, spec Spec) (int, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = spec.Stdin, spec.Stdout, spec.Stderr
	cmd.Cancel = func() error { return terminate(cmd) }
	cmd.WaitDelay = terminationGrace
	prepare(cmd)
	err := cmd.Run()
	if cmd.Process != nil && ctx.Err() != nil {
		killGroup(cmd)
	}
	if err == nil {
		return 0, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return 0, ctxErr
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exitCode(exit.ProcessState), nil
	}
	return 0, err
}
