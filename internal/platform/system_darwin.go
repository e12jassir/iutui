//go:build darwin

package platform

import (
	"context"
	"os/exec"
	"strings"
)

type darwinSystem struct{}

// NewSystem returns a macOS implementation of System using open, pbcopy, and osascript.
func NewSystem() System {
	return &darwinSystem{}
}

func (s *darwinSystem) OpenURL(ctx context.Context, url string) error {
	return exec.CommandContext(ctx, "open", url).Run()
}

func (s *darwinSystem) CopyText(ctx context.Context, text string) error {
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func (s *darwinSystem) Notify(ctx context.Context, title, body string) error {
	script := buildDarwinNotificationScript(title, body)
	return exec.CommandContext(ctx, "osascript", "-e", script).Run()
}

var appleScriptEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\r", `\r`,
	"\n", `\n`,
)

func buildDarwinNotificationScript(title, body string) string {
	return `display notification "` + appleScriptEscaper.Replace(body) + `" with title "` + appleScriptEscaper.Replace(title) + `"`
}
