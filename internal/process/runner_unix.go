//go:build unix

package process

import (
	"os"
	"os/exec"
	"syscall"
)

func prepare(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// terminate asks the child's whole process group to stop.
func terminate(cmd *exec.Cmd) error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }

// killGroup removes any descendants the child left behind.
func killGroup(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

// exitCode follows the shell convention of 128 plus the signal number for a killed child.
func exitCode(state *os.ProcessState) int {
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return state.ExitCode()
}
