package player

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildMPVArgs_Modes(t *testing.T) {
	baseURL := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	tests := []struct {
		name     string
		mode     string
		wantArgs []string
		denyArgs []string
	}{
		{
			name:     "audio mode",
			mode:     "audio",
			wantArgs: []string{"--no-video", "--force-window=no"},
			denyArgs: []string{"--force-window=yes", "--ontop", "--fs"},
		},
		{
			name:     "pip mode",
			mode:     "pip",
			wantArgs: []string{"--force-window=yes", "--ontop", "--autofit=35%"},
			denyArgs: []string{"--no-video", "--fs"},
		},
		{
			name:     "fs mode",
			mode:     "fs",
			wantArgs: []string{"--force-window=yes", "--fs"},
			denyArgs: []string{"--no-video", "--ontop"},
		},
		{
			name:     "window mode",
			mode:     "window",
			wantArgs: []string{"--force-window=yes"},
			denyArgs: []string{"--no-video", "--ontop", "--fs"},
		},
		{
			name:     "auto mode",
			mode:     "auto",
			wantArgs: []string{"--force-window=yes"},
			denyArgs: []string{"--no-video", "--ontop", "--fs"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildMPVArgs(LaunchOpts{
				Mode: tc.mode,
				URL:  baseURL,
			})
			for _, want := range tc.wantArgs {
				if !slices.Contains(got, want) {
					t.Errorf("BuildMPVArgs(mode=%q) = %v, missing %q", tc.mode, got, want)
				}
			}
			for _, deny := range tc.denyArgs {
				if slices.Contains(got, deny) {
					t.Errorf("BuildMPVArgs(mode=%q) = %v, should not contain %q", tc.mode, got, deny)
				}
			}
		})
	}
}

func TestBuildMPVArgs_Quality(t *testing.T) {
	tests := []struct {
		name    string
		quality int
		want    string
	}{
		{
			name:    "quality 0 uses best",
			quality: 0,
			want:    "--ytdl-format=bestvideo+bestaudio/best",
		},
		{
			name:    "negative quality uses best",
			quality: -1,
			want:    "--ytdl-format=bestvideo+bestaudio/best",
		},
		{
			name:    "quality 1080",
			quality: 1080,
			want:    "--ytdl-format=bestvideo[height<=1080]+bestaudio/best[height<=1080]/best",
		},
		{
			name:    "quality 720",
			quality: 720,
			want:    "--ytdl-format=bestvideo[height<=720]+bestaudio/best[height<=720]/best",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildMPVArgs(LaunchOpts{
				Mode:    "window",
				Quality: tc.quality,
				URL:     "https://www.youtube.com/watch?v=abc123",
			})
			if !slices.Contains(got, tc.want) {
				t.Errorf("BuildMPVArgs(quality=%d) = %v, missing %q", tc.quality, got, tc.want)
			}
		})
	}
}

func TestBuildMPVArgs_OptionsAndFlagInjection(t *testing.T) {
	tests := []struct {
		name string
		opts LaunchOpts
		want []string
	}{
		{
			name: "cookies, socket, volume, and URL",
			opts: LaunchOpts{
				VideoID:       "abc123",
				URL:           "https://www.youtube.com/watch?v=abc123",
				Mode:          "window",
				SocketPath:    "/tmp/yt-1000/mpv.sock",
				Quality:       720,
				Volume:        80,
				CookieJarPath: "/tmp/cookies.txt",
			},
			want: []string{
				"--input-ipc-server=/tmp/yt-1000/mpv.sock",
				"--idle=yes",
				"--keep-open=no",
				"--volume=80",
				"--force-window=yes",
				"--ytdl-format=bestvideo[height<=720]+bestaudio/best[height<=720]/best",
				"--ytdl-raw-options=cookies=/tmp/cookies.txt,no-playlist=",
				"--",
				"https://www.youtube.com/watch?v=abc123",
			},
		},
		{
			name: "fast playback adds warm flags, no-playlist, and android,web extractor args",
			opts: LaunchOpts{
				URL:          "https://www.youtube.com/watch?v=abc123",
				Mode:         "window",
				SocketPath:   "/tmp/yt-1000/mpv.sock",
				FastPlayback: true,
			},
			want: []string{
				"--hwdec=auto-safe",
				"--really-quiet",
				"--cache=yes",
				"--cache-pause=no",
				"--demuxer-readahead-secs=3",
				"--input-ipc-server=/tmp/yt-1000/mpv.sock",
				"--idle=yes",
				"--keep-open=no",
				"--force-window=immediate",
				"--ytdl-format=bestvideo+bestaudio/best",
				"--ytdl-raw-options=no-playlist=,extractor-args=youtube:player_client=android,web",
				"--",
				"https://www.youtube.com/watch?v=abc123",
			},
		},
		{
			name: "allow playlist with fast playback uses yes-playlist=",
			opts: LaunchOpts{
				URL:           "https://www.youtube.com/playlist?list=PL123",
				Mode:          "window",
				FastPlayback:  true,
				AllowPlaylist: true,
			},
			want: []string{
				"--hwdec=auto-safe",
				"--really-quiet",
				"--cache=yes",
				"--cache-pause=no",
				"--demuxer-readahead-secs=3",
				"--force-window=immediate",
				"--ytdl-format=bestvideo+bestaudio/best",
				"--ytdl-raw-options=yes-playlist=,extractor-args=youtube:player_client=android,web",
				"--",
				"https://www.youtube.com/playlist?list=PL123",
			},
		},
		{
			name: "allow playlist without fast playback or cookies emits yes-playlist=",
			opts: LaunchOpts{
				URL:           "https://www.youtube.com/playlist?list=PL123",
				Mode:          "window",
				AllowPlaylist: true,
			},
			want: []string{
				"--force-window=yes",
				"--ytdl-format=bestvideo+bestaudio/best",
				"--ytdl-raw-options=yes-playlist=",
				"--",
				"https://www.youtube.com/playlist?list=PL123",
			},
		},
		{
			name: "cookies with allow playlist preserves cookies and yes-playlist=",
			opts: LaunchOpts{
				URL:           "https://www.youtube.com/playlist?list=PL123",
				Mode:          "window",
				CookieJarPath: "/tmp/cookies.txt",
				AllowPlaylist: true,
			},
			want: []string{
				"--force-window=yes",
				"--ytdl-format=bestvideo+bestaudio/best",
				"--ytdl-raw-options=cookies=/tmp/cookies.txt,yes-playlist=",
				"--",
				"https://www.youtube.com/playlist?list=PL123",
			},
		},
		{
			name: "flag injection guard places -- before dash-prefixed URL",
			opts: LaunchOpts{
				Mode: "window",
				URL:  "--ytdl-raw-options=evil=1",
			},
			want: []string{
				"--force-window=yes",
				"--ytdl-format=bestvideo+bestaudio/best",
				"--",
				"--ytdl-raw-options=evil=1",
			},
		},
		{
			name: "omits optional flags when empty or zero",
			opts: LaunchOpts{
				Mode: "auto",
				URL:  "-evil",
			},
			want: []string{
				"--force-window=yes",
				"--ytdl-format=bestvideo+bestaudio/best",
				"--",
				"-evil",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildMPVArgs(tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BuildMPVArgs() =\n  %v\nwant:\n  %v", got, tc.want)
			}
		})
	}
}

func TestLaunchMPV(t *testing.T) {
	t.Run("starts detached process with built args", func(t *testing.T) {
		tmpDir := t.TempDir()
		outFile := filepath.Join(tmpDir, "args.txt")
		scriptPath := filepath.Join(tmpDir, "fake-mpv.sh")

		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + outFile + "\"\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake mpv script: %v", err)
		}

		opts := LaunchOpts{
			URL:  "https://www.youtube.com/watch?v=test",
			Mode: "pip",
		}
		if err := LaunchMPV(scriptPath, opts); err != nil {
			t.Fatalf("LaunchMPV() unexpected error: %v", err)
		}

		deadline := time.Now().Add(2 * time.Second)
		var content []byte
		var err error
		for time.Now().Before(deadline) {
			content, err = os.ReadFile(outFile)
			if err == nil && len(content) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("read recorded args: %v", err)
		}

		gotArgs := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
		wantArgs := BuildMPVArgs(opts)
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			t.Errorf("launched args = %v, want %v", gotArgs, wantArgs)
		}
	})

	t.Run("returns error for missing binary", func(t *testing.T) {
		missingBin := filepath.Join(t.TempDir(), "does-not-exist")
		if err := LaunchMPV(missingBin, LaunchOpts{URL: "https://example.com"}); err == nil {
			t.Fatal("expected error for missing mpv binary, got nil")
		}
	})
}

func TestStartMPVProcess(t *testing.T) {
	t.Run("returns live process handle with positive PID", func(t *testing.T) {
		tmpDir := t.TempDir()
		pidFile := filepath.Join(tmpDir, "pid.txt")
		scriptPath := filepath.Join(tmpDir, "sleep-mpv.sh")

		script := "#!/bin/sh\necho $$ > \"" + pidFile + "\"\nsleep 10\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake mpv script: %v", err)
		}

		proc, err := StartMPVProcess(scriptPath, LaunchOpts{
			URL:      "https://www.youtube.com/watch?v=test",
			YtdlpBin: "yt-dlp",
		})
		if err != nil {
			t.Fatalf("StartMPVProcess() unexpected error: %v", err)
		}
		if proc == nil || proc.Pid <= 0 {
			t.Fatalf("expected non-nil process with positive PID, got %+v", proc)
		}
		t.Cleanup(func() {
			_ = proc.Kill()
		})

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("timed out waiting for started process to run")
	})

	t.Run("returns error for missing binary", func(t *testing.T) {
		missingBin := filepath.Join(t.TempDir(), "does-not-exist")
		proc, err := StartMPVProcess(missingBin, LaunchOpts{URL: "https://example.com"})
		if err == nil {
			if proc != nil {
				_ = proc.Kill()
			}
			t.Fatal("expected error for missing mpv binary, got nil")
		}
	})
}
