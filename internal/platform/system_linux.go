//go:build linux

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type linuxSystem struct{}

// NewSystem returns a Linux implementation of System using xdg-open,
// wl-copy/xclip, and notify-send.
func NewSystem() System {
	return &linuxSystem{}
}

func (s *linuxSystem) OpenURL(ctx context.Context, url string) error {
	return exec.CommandContext(ctx, "xdg-open", url).Run()
}

func (s *linuxSystem) CopyText(ctx context.Context, text string) error {
	var cmd *exec.Cmd

	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && hasCmd("wl-copy"):
		cmd = exec.CommandContext(ctx, "wl-copy")
	case hasCmd("xclip"):
		cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
	case hasCmd("wl-copy"):
		cmd = exec.CommandContext(ctx, "wl-copy")
	default:
		return fmt.Errorf("no clipboard utility found (wl-copy or xclip): %w", exec.ErrNotFound)
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func (s *linuxSystem) Notify(ctx context.Context, title, body string) error {
	return exec.CommandContext(ctx, "notify-send", title, body).Run()
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
