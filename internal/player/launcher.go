package player

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/e12jassir/iutui/internal/platform"
)

// LaunchOpts configures an mpv process launch.
type LaunchOpts struct {
	VideoID       string
	Title         string
	URL           string
	AudioFile     string
	Mode          string
	AudioOnly     bool
	SocketPath    string
	Quality       int
	Volume        int
	Speed         float64
	CookieJarPath string
	FastPlayback  bool
	YtdlpBin      string
	YtdlpArgs     []string
	PrefetchLimit *int
	AllowPlaylist bool
	IsLive        bool
	StartPos      float64
	StartPaused   bool
}

// BuildMPVArgs constructs the command-line argument slice for mpv from opts.
func BuildMPVArgs(opts LaunchOpts) []string {
	var args []string
	if opts.FastPlayback {
		// Cap forward/backward demuxer buffers so mpv does not pre-fetch 150+ MiB ahead per track.
		args = append(args,
			"--hwdec=auto-safe",
			"--really-quiet",
			"--cache=yes",
			"--cache-pause=no",
			"--demuxer-readahead-secs=3",
			"--demuxer-max-bytes=24MiB",
			"--demuxer-max-back-bytes=8MiB",
		)
	}

	if opts.SocketPath != "" {
		args = append(args,
			"--input-ipc-server="+opts.SocketPath,
			"--idle=yes",
			"--keep-open=no",
		)
	}
	if opts.Volume > 0 {
		args = append(args, fmt.Sprintf("--volume=%d", opts.Volume))
	}
	if opts.Speed > 0 && math.Abs(opts.Speed-1.0) > 0.001 {
		args = append(args, fmt.Sprintf("--speed=%s", strconv.FormatFloat(clampSpeed(opts.Speed), 'f', -1, 64)))
	}
	var perFileArgs []string
	if opts.Title != "" {
		perFileArgs = append(perFileArgs, "--force-media-title="+opts.Title)
	}

	winFlag := "--force-window=yes"
	if opts.FastPlayback {
		winFlag = "--force-window=immediate"
	}

	if opts.AudioOnly || opts.Mode == "audio" {
		args = append(args, "--no-video", "--force-window=no", "--ytdl-format=bestaudio/best")
	} else {
		switch opts.Mode {
		case "pip":
			args = append(args, winFlag, "--ontop", "--autofit=35%")
		case "fs":
			args = append(args, winFlag, "--fs")
		default: // "window", "auto", or unspecified
			args = append(args, winFlag)
		}
		if opts.IsLive {
			if opts.Quality <= 0 {
				args = append(args, "--ytdl-format=best/bestvideo+bestaudio")
			} else {
				args = append(args, fmt.Sprintf("--ytdl-format=best[height<=%d]/bestvideo[height<=%d]+bestaudio/best", opts.Quality, opts.Quality))
			}
		} else if opts.Quality <= 0 {
			args = append(args, "--ytdl-format=bestvideo+bestaudio/best")
		} else {
			args = append(args, fmt.Sprintf("--ytdl-format=bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", opts.Quality, opts.Quality))
		}
	}

	if opts.StartPos > 0.5 {
		perFileArgs = append(perFileArgs, fmt.Sprintf("--start=%.1f", opts.StartPos))
	}
	if opts.StartPaused {
		perFileArgs = append(perFileArgs, "--pause=yes")
	}

	playlistOpt := "no-playlist="
	if opts.AllowPlaylist {
		playlistOpt = "yes-playlist="
	}
	if opts.FastPlayback && !opts.AllowPlaylist {
		if opts.AudioOnly || opts.Mode == "audio" || (opts.Quality > 0 && opts.Quality <= 480) {
			args = append(args, "--ytdl-raw-options=no-playlist=,extractor-args="+fastAndroidExtractorArgs)
		} else {
			args = append(args, "--ytdl-raw-options=no-playlist=,extractor-args="+fastHDExtractorArgs)
		}
	} else if opts.CookieJarPath != "" {
		args = append(args, "--ytdl-raw-options=cookies="+opts.CookieJarPath+","+playlistOpt)
	} else if opts.FastPlayback {
		args = append(args, "--ytdl-raw-options="+playlistOpt)
	} else if opts.AllowPlaylist {
		args = append(args, "--ytdl-raw-options=yes-playlist=")
	}

	args = appendYtdlpRawArgs(args, opts.YtdlpArgs)

	if opts.AudioFile != "" {
		perFileArgs = append(perFileArgs, "--audio-file="+opts.AudioFile)
	}

	// Scope per-file launch options inside --{ ... --} so start offsets,
	// pause state, titles, and split audio never leak to subsequent tracks.
	if opts.URL != "" && !strings.HasPrefix(opts.URL, "-") && len(perFileArgs) > 0 {
		args = append(args, "--{", opts.URL)
		args = append(args, perFileArgs...)
		args = append(args, "--}")
	} else {
		args = append(args, perFileArgs...)
		if opts.URL != "" {
			args = append(args, "--", opts.URL)
		}
	}

	return args
}

func appendYtdlpRawArgs(args []string, ytdlpArgs []string) []string {
	for i := 0; i < len(ytdlpArgs); i++ {
		raw := strings.TrimSpace(ytdlpArgs[i])
		if raw == "" {
			continue
		}
		switch raw {
		case "-4":
			args = append(args, "--ytdl-raw-options-append=force-ipv4=")
			continue
		case "-6":
			args = append(args, "--ytdl-raw-options-append=force-ipv6=")
			continue
		}
		if strings.HasPrefix(raw, "--") {
			kv := strings.TrimPrefix(raw, "--")
			if kv == "" {
				continue
			}
			if strings.Contains(kv, "=") {
				args = append(args, "--ytdl-raw-options-append="+kv)
			} else if i+1 < len(ytdlpArgs) && !strings.HasPrefix(strings.TrimSpace(ytdlpArgs[i+1]), "-") {
				val := strings.TrimSpace(ytdlpArgs[i+1])
				args = append(args, "--ytdl-raw-options-append="+kv+"="+val)
				i++
			} else {
				args = append(args, "--ytdl-raw-options-append="+kv+"=")
			}
		} else if strings.Contains(raw, "=") {
			args = append(args, "--ytdl-raw-options-append="+raw)
		}
	}
	return args
}

// StartMPVProcess starts a detached mpv process with the given binary and
// options, asynchronously reaps it, and returns its OS process handle.
func StartMPVProcess(mpvBin string, opts LaunchOpts) (*os.Process, error) {
	if mpvBin == "" {
		mpvBin = "mpv"
	}

	cmd := exec.Command(mpvBin, BuildMPVArgs(opts)...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	platform.DetachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch mpv: %w", err)
	}

	proc := cmd.Process
	// Reap the child process asynchronously so it does not become a zombie.
	go func() {
		_ = cmd.Wait()
	}()

	return proc, nil
}

// LaunchMPV starts a detached mpv process with the given binary and options
// without blocking the caller.
func LaunchMPV(mpvBin string, opts LaunchOpts) error {
	_, err := StartMPVProcess(mpvBin, opts)
	return err
}
