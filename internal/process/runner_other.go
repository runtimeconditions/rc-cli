//go:build !unix

package process

import (
	"os"
	"os/exec"
)

func prepare(*exec.Cmd) {}

func terminate(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func killGroup(*exec.Cmd) {}

func exitCode(state *os.ProcessState) int { return state.ExitCode() }
