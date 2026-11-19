//go:build unix

package platform

import (
	"errors"
	"os/exec"
	"syscall"
)

// SetProcessGroup configures cmd to start in its own process group so the
// child process tree can be signaled together.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// DetachProcess configures cmd to run in a new session detached from the
// parent's controlling terminal.
func DetachProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

// KillProcessGroup sends SIGKILL to the process group identified by pid,
// falling back to signaling pid directly if signaling the group fails with
// ESRCH or EPERM.
func KillProcessGroup(pid int) error {
	return signalProcessGroup(pid, syscall.SIGKILL)
}

// TerminateProcessGroup sends SIGTERM to the process group identified by pid,
// falling back to signaling pid directly if signaling the group fails with
// ESRCH or EPERM.
func TerminateProcessGroup(pid int) error {
	return signalProcessGroup(pid, syscall.SIGTERM)
}

func signalProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) {
		return syscall.Kill(pid, sig)
	}
	return err
}

// ProcessAlive reports whether a process with the given pid currently exists
// and can be signaled by the caller.
func ProcessAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}
