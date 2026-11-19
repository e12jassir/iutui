package platform

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

func TestProcessAttrNilSafetyAndPreservation(t *testing.T) {
	SetProcessGroup(nil)
	DetachProcess(nil)

	cmd := exec.Command("dummy")
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SetProcessGroup left SysProcAttr nil")
	}

	existing := &syscall.SysProcAttr{}
	cmd2 := exec.Command("dummy")
	cmd2.SysProcAttr = existing
	DetachProcess(cmd2)
	if cmd2.SysProcAttr != existing {
		t.Fatal("DetachProcess replaced existing SysProcAttr pointer instead of preserving it")
	}

	cmd3 := exec.Command("dummy")
	cmd3.SysProcAttr = existing
	SetProcessGroup(cmd3)
	if cmd3.SysProcAttr != existing {
		t.Fatal("SetProcessGroup replaced existing SysProcAttr pointer instead of preserving it")
	}
}

func TestInvalidPIDSafety(t *testing.T) {
	for _, pid := range []int{0, -1, -42} {
		if err := KillProcessGroup(pid); err != nil {
			t.Errorf("KillProcessGroup(%d) = %v, want nil", pid, err)
		}
		if err := TerminateProcessGroup(pid); err != nil {
			t.Errorf("TerminateProcessGroup(%d) = %v, want nil", pid, err)
		}
		if ProcessAlive(pid) {
			t.Errorf("ProcessAlive(%d) = true, want false", pid)
		}
	}
}

func TestUnixSubprocessLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix subprocess test skipped on windows")
	}

	cases := []struct {
		name      string
		setupAttr func(*exec.Cmd)
		stopFn    func(int) error
	}{
		{
			name:      "SetProcessGroup and TerminateProcessGroup",
			setupAttr: SetProcessGroup,
			stopFn:    TerminateProcessGroup,
		},
		{
			name:      "SetProcessGroup and KillProcessGroup",
			setupAttr: SetProcessGroup,
			stopFn:    KillProcessGroup,
		},
		{
			name:      "DetachProcess and KillProcessGroup",
			setupAttr: DetachProcess,
			stopFn:    KillProcessGroup,
		},
		{
			name:      "No process group fallback KillProcessGroup",
			setupAttr: func(*exec.Cmd) {},
			stopFn:    KillProcessGroup,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sleep", "5")
			tc.setupAttr(cmd)
			if err := cmd.Start(); err != nil {
				t.Fatalf("Start sleep: %v", err)
			}

			pid := cmd.Process.Pid
			waited := false
			t.Cleanup(func() {
				if !waited {
					_ = KillProcessGroup(pid)
					_ = cmd.Wait()
				}
			})

			if !ProcessAlive(pid) {
				t.Fatalf("ProcessAlive(%d) = false after Start, want true", pid)
			}

			if err := tc.stopFn(pid); err != nil {
				t.Fatalf("stop(%d) error: %v", pid, err)
			}

			_ = cmd.Wait()
			waited = true

			if ProcessAlive(pid) {
				t.Fatalf("ProcessAlive(%d) = true after Wait, want false", pid)
			}
		})
	}
}
