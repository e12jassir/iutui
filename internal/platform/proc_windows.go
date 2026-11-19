//go:build windows

package platform

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	windowsCreateNewProcessGroup    uint32 = 0x00000200
	windowsDetachedProcess          uint32 = 0x00000008
	windowsProcessQueryLimitedInfo  uint32 = 0x1000
	windowsStatusPendingStillActive uint32 = 259
)

// SetProcessGroup configures cmd to start in its own Windows process group
// with its console window hidden.
func SetProcessGroup(cmd *exec.Cmd) {
	configureWindowsProcAttr(cmd, windowsCreateNewProcessGroup)
}

// DetachProcess configures cmd to run detached in a new Windows process group
// with its console window hidden.
func DetachProcess(cmd *exec.Cmd) {
	configureWindowsProcAttr(cmd, windowsDetachedProcess|windowsCreateNewProcessGroup)
}

func configureWindowsProcAttr(cmd *exec.Cmd, creationFlags uint32) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= creationFlags
	cmd.SysProcAttr.HideWindow = true
}

// KillProcessGroup terminates the Windows process identified by pid.
func KillProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// TerminateProcessGroup terminates the Windows process identified by pid.
func TerminateProcessGroup(pid int) error {
	return KillProcessGroup(pid)
}

// ProcessAlive reports whether a Windows process with the given pid is still
// active without panicking on invalid or exited PIDs.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(windowsProcessQueryLimitedInfo, false, uint32(pid))
	if err != nil || handle == 0 {
		return false
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == windowsStatusPendingStillActive
}
