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
				"--volume=80",
				"--force-window=yes",
				"--ytdl-format=bestvideo[height<=720]+bestaudio/best[height<=720]/best",
				"--ytdl-raw-options=cookies=/tmp/cookies.txt",
				"--",
				"https://www.youtube.com/watch?v=abc123",
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
