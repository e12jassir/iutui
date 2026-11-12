package player

import (
	"fmt"
	"os/exec"
	"syscall"
)

// LaunchOpts configures an mpv process launch.
type LaunchOpts struct {
	VideoID       string
	URL           string
	Mode          string
	SocketPath    string
	Quality       int
	Volume        int
	CookieJarPath string
}

// BuildMPVArgs constructs the command-line argument slice for mpv from opts.
func BuildMPVArgs(opts LaunchOpts) []string {
	var args []string

	if opts.SocketPath != "" {
		args = append(args, "--input-ipc-server="+opts.SocketPath)
	}
	if opts.Volume > 0 {
		args = append(args, fmt.Sprintf("--volume=%d", opts.Volume))
	}

	switch opts.Mode {
	case "audio":
		args = append(args, "--no-video", "--force-window=no")
	case "pip":
		args = append(args, "--force-window=yes", "--ontop", "--autofit=35%")
	case "fs":
		args = append(args, "--force-window=yes", "--fs")
	default: // "window", "auto", or unspecified
		args = append(args, "--force-window=yes")
	}

	if opts.Quality <= 0 {
		args = append(args, "--ytdl-format=bestvideo+bestaudio/best")
	} else {
		args = append(args, fmt.Sprintf("--ytdl-format=bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", opts.Quality, opts.Quality))
	}

	if opts.CookieJarPath != "" {
		args = append(args, "--ytdl-raw-options=cookies="+opts.CookieJarPath)
	}

	if opts.URL != "" {
		args = append(args, "--", opts.URL)
	}

	return args
}

// LaunchMPV starts a detached mpv process with the given binary and options
// without blocking the caller.
func LaunchMPV(mpvBin string, opts LaunchOpts) error {
	if mpvBin == "" {
		mpvBin = "mpv"
	}

	cmd := exec.Command(mpvBin, BuildMPVArgs(opts)...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch mpv: %w", err)
	}

	// Reap the child process asynchronously so it does not become a zombie.
	go func() {
		_ = cmd.Wait()
	}()

	return nil
}
