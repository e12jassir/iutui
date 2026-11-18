package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/e12jassir/iutui/internal/model"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name      string
		ytdlpBin  string
		wantYtdlp string
	}{
		{
			name:      "default binary when empty",
			ytdlpBin:  "",
			wantYtdlp: "yt-dlp",
		},
		{
			name:      "custom binary preserved",
			ytdlpBin:  "/usr/local/bin/yt-dlp",
			wantYtdlp: "/usr/local/bin/yt-dlp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			dlDir := t.TempDir()
			m := NewManager(stateDir, dlDir, tc.ytdlpBin)
			if m.YtdlpBin != tc.wantYtdlp {
				t.Fatalf("YtdlpBin = %q, want %q", m.YtdlpBin, tc.wantYtdlp)
			}
			if m.StateDir != stateDir {
				t.Fatalf("StateDir = %q, want %q", m.StateDir, stateDir)
			}
			if m.DownloadDir != dlDir {
				t.Fatalf("DownloadDir = %q, want %q", m.DownloadDir, dlDir)
			}
		})
	}
}

func TestEnqueueArgsAndMeta(t *testing.T) {
	tests := []struct {
		name      string
		video     model.Video
		audioOnly bool
		wantArgs  func(dlDir, id string) []string
	}{
		{
			name: "video download",
			video: model.Video{
				ID:      "dQw4w9WgXcQ",
				Title:   "Never Gonna Give You Up",
				Channel: "Rick Astley",
			},
			audioOnly: false,
			wantArgs: func(dlDir, id string) []string {
				return []string{
					"-o", filepath.Join(dlDir, "%(title)s.%(ext)s"),
					"--", "https://www.youtube.com/watch?v=" + id,
				}
			},
		},
		{
			name: "audio only download",
			video: model.Video{
				ID:      "abc123XYZ",
				Title:   "Track One",
				Channel: "Artist",
			},
			audioOnly: true,
			wantArgs: func(dlDir, id string) []string {
				return []string{
					"-x", "--audio-format", "mp3", "--embed-thumbnail",
					"-o", filepath.Join(dlDir, "%(title)s.%(ext)s"),
					"--", "https://www.youtube.com/watch?v=" + id,
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			dlDir := t.TempDir()
			m := NewManager(stateDir, dlDir, "yt-dlp-mock")

			var gotBin string
			var gotArgs []string
			var statusDuringRun Status

			m.Runner = func(ctx context.Context, bin string, args ...string) error {
				gotBin = bin
				gotArgs = append([]string(nil), args...)

				items, err := m.List()
				if err != nil {
					return err
				}
				for _, it := range items {
					if it.Video.ID == tc.video.ID {
						statusDuringRun = it.Status
					}
				}
				return nil
			}

			if err := m.Enqueue(context.Background(), tc.video, tc.audioOnly); err != nil {
				t.Fatalf("Enqueue() unexpected error: %v", err)
			}

			if gotBin != "yt-dlp-mock" {
				t.Fatalf("runner bin = %q, want %q", gotBin, "yt-dlp-mock")
			}

			wantArgs := tc.wantArgs(dlDir, tc.video.ID)
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("runner args = %v, want %v", gotArgs, wantArgs)
			}

			if statusDuringRun != StatusDownloading {
				t.Fatalf("status during run = %q, want %q", statusDuringRun, StatusDownloading)
			}

			metaPath := filepath.Join(stateDir, "downloads", tc.video.ID+".meta")
			raw, err := os.ReadFile(metaPath)
			if err != nil {
				t.Fatalf("read meta file: %v", err)
			}
			wantMeta := tc.video.Title + "\n" + tc.video.Channel + "\n"
			if string(raw) != wantMeta {
				t.Fatalf("meta content = %q, want %q", string(raw), wantMeta)
			}

			items, err := m.List()
			if err != nil {
				t.Fatalf("List() unexpected error: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("List() len = %d, want 1", len(items))
			}
			if items[0].Status != StatusDone {
				t.Fatalf("item status = %q, want %q", items[0].Status, StatusDone)
			}
			if items[0].AudioOnly != tc.audioOnly {
				t.Fatalf("item AudioOnly = %v, want %v", items[0].AudioOnly, tc.audioOnly)
			}
			if items[0].OutputPath != filepath.Join(dlDir, "%(title)s.%(ext)s") {
				t.Fatalf("item OutputPath = %q, want %q", items[0].OutputPath, filepath.Join(dlDir, "%(title)s.%(ext)s"))
			}
		})
	}
}

func TestEnqueueFailureAndCancellation(t *testing.T) {
	tests := []struct {
		name           string
		video          model.Video
		setupCtx       func() (context.Context, context.CancelFunc)
		runner         Runner
		wantErrSubstr  string
		wantItemInList bool
		wantStatus     Status
	}{
		{
			name:  "runner returns error",
			video: model.Video{ID: "fail1", Title: "Broken Video", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				return errors.New("network timeout")
			},
			wantErrSubstr:  "network timeout",
			wantItemInList: true,
			wantStatus:     StatusFailed,
		},
		{
			name:  "context cancelled",
			video: model.Video{ID: "cancel1", Title: "Cancelled Video", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				return ctx.Err()
			},
			wantErrSubstr:  context.Canceled.Error(),
			wantItemInList: true,
			wantStatus:     StatusFailed,
		},
		{
			name:  "empty video ID rejected",
			video: model.Video{ID: "", Title: "No ID", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				t.Fatal("runner should not be called for invalid ID")
				return nil
			},
			wantErrSubstr:  "required",
			wantItemInList: false,
		},
		{
			name:  "path traversal ID rejected",
			video: model.Video{ID: "../evil", Title: "Evil", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				t.Fatal("runner should not be called for invalid ID")
				return nil
			},
			wantErrSubstr:  "invalid video ID",
			wantItemInList: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			dlDir := t.TempDir()
			m := NewManager(stateDir, dlDir, "")
			m.Runner = tc.runner

			ctx, cancel := tc.setupCtx()
			defer cancel()

			err := m.Enqueue(ctx, tc.video, false)
			if err == nil {
				t.Fatalf("Enqueue() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("Enqueue() error = %q, want substring %q", err.Error(), tc.wantErrSubstr)
			}

			items, listErr := m.List()
			if listErr != nil {
				t.Fatalf("List() unexpected error: %v", listErr)
			}

			if !tc.wantItemInList {
				if len(items) != 0 {
					t.Fatalf("List() len = %d, want 0", len(items))
				}
				return
			}

			if len(items) != 1 {
				t.Fatalf("List() len = %d, want 1", len(items))
			}
			if items[0].Status != tc.wantStatus {
				t.Fatalf("item status = %q, want %q", items[0].Status, tc.wantStatus)
			}
			if !strings.Contains(items[0].Err, tc.wantErrSubstr) {
				t.Fatalf("item Err = %q, want substring %q", items[0].Err, tc.wantErrSubstr)
			}
		})
	}
}

func TestListMergesDiskMetaAndActiveQueue(t *testing.T) {
	stateDir := t.TempDir()
	dlDir := t.TempDir()
	metaDir := filepath.Join(stateDir, "downloads")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "old123.meta"), []byte("Old Title\nOld Channel\n"), 0o644); err != nil {
		t.Fatalf("write old meta: %v", err)
	}

	m := NewManager(stateDir, dlDir, "")
	m.Runner = func(ctx context.Context, bin string, args ...string) error {
		return nil
	}

	activeVideo := model.Video{
		ID:      "new456",
		Title:   "New Title",
		Channel: "New Channel",
	}
	if err := m.Enqueue(context.Background(), activeVideo, true); err != nil {
		t.Fatalf("Enqueue() error: %v", err)
	}

	items, err := m.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List() len = %d, want 2", len(items))
	}

	if items[0].Video.ID != "new456" || !items[0].AudioOnly || items[0].Status != StatusDone {
		t.Fatalf("items[0] = %+v, want active new456 AudioOnly=true Status=done", items[0])
	}
	if items[1].Video.ID != "old123" || items[1].Video.Title != "Old Title" || items[1].Video.Channel != "Old Channel" {
		t.Fatalf("items[1] = %+v, want disk old123", items[1])
	}
}
