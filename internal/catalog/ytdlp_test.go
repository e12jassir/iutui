package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/e12jassir/ytx/internal/model"
)

func TestBuildFormat(t *testing.T) {
	tests := []struct {
		name    string
		quality int
		want    string
	}{
		{
			name:    "zero quality",
			quality: 0,
			want:    "bestvideo+bestaudio/best",
		},
		{
			name:    "negative quality",
			quality: -1080,
			want:    "bestvideo+bestaudio/best",
		},
		{
			name:    "720p",
			quality: 720,
			want:    "bestvideo[height<=720]+bestaudio/best[height<=720]/best",
		},
		{
			name:    "1080p",
			quality: 1080,
			want:    "bestvideo[height<=1080]+bestaudio/best[height<=1080]/best",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildFormat(tc.quality)
			if got != tc.want {
				t.Fatalf("BuildFormat(%d) = %q, want %q", tc.quality, got, tc.want)
			}
		})
	}
}

func TestClassifyErr(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ErrKind
	}{
		{
			name:   "empty",
			stderr: "",
			want:   ErrUnknown,
		},
		{
			name:   "rate limit 429",
			stderr: "ERROR: HTTP Error 429: Too Many Requests",
			want:   ErrRateLimit,
		},
		{
			name:   "forbidden 403",
			stderr: "ERROR: unable to download video data: HTTP Error 403: Forbidden",
			want:   ErrForbidden,
		},
		{
			name:   "auth sign in",
			stderr: "ERROR: Sign in to confirm you're not a bot. Use --cookies-from-browser",
			want:   ErrAuth,
		},
		{
			name:   "auth private video",
			stderr: "ERROR: Private video. Sign in if you've been granted access",
			want:   ErrAuth,
		},
		{
			name:   "network timeout",
			stderr: "ERROR: Unable to download webpage: <urlopen error timed out>",
			want:   ErrNetwork,
		},
		{
			name:   "network dns",
			stderr: "ERROR: dial tcp: lookup www.youtube.com: no such host",
			want:   ErrNetwork,
		},
		{
			name:   "unknown error",
			stderr: "ERROR: Unsupported URL: https://example.com/nope",
			want:   ErrUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyErr(tc.stderr)
			if got != tc.want {
				t.Fatalf("ClassifyErr(%q) = %v, want %v", tc.stderr, got, tc.want)
			}
		})
	}
}

func writeFakeYTDLP(t *testing.T, dir, script string) string {
	t.Helper()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"+script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return bin
}

func TestYTDLPCatalog_CacheAndFallback(t *testing.T) {
	cachedVideos := []model.Video{
		{ID: "cached123", Title: "Cached Title", Channel: "Cached Chan", Duration: "04:10", Views: "10K vistas"},
	}

	tests := []struct {
		name        string
		setupCache  bool
		staleCache  bool
		binScript   string
		binExitFail bool
		wantErr     bool
		wantFirstID string
		wantRefetch bool
	}{
		{
			name:        "fresh cache hit skips yt-dlp",
			setupCache:  true,
			staleCache:  false,
			binExitFail: true,
			wantFirstID: "cached123",
		},
		{
			name:        "stale cache serves fallback while revalidating",
			setupCache:  true,
			staleCache:  true,
			binScript:   `printf '{"id":"fresh9999","title":"Fresh Video","channel":"Fresh Chan","duration":125,"view_count":5000}\n'`,
			wantFirstID: "cached123",
			wantRefetch: true,
		},
		{
			name:        "stale cache with failing binary still returns fallback",
			setupCache:  true,
			staleCache:  true,
			binScript:   `echo "ERROR: HTTP Error 429: Too Many Requests" >&2; exit 1`,
			wantFirstID: "cached123",
		},
		{
			name:        "cache miss runs yt-dlp and populates cache",
			setupCache:  false,
			binScript:   `printf '{"entries":[{"id":"miss11111","title":"Fetched Video","uploader":"UploaderX","duration_string":"02:05","view_count":1200000}]}\n'`,
			wantFirstID: "miss11111",
			wantRefetch: true,
		},
		{
			name:       "cache miss with failing binary returns classified error",
			setupCache: false,
			binScript:  `echo "ERROR: HTTP Error 403: Forbidden" >&2; exit 1`,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			cacheDir := filepath.Join(tmp, "cache")

			if tc.setupCache {
				if err := os.MkdirAll(cacheDir, 0o755); err != nil {
					t.Fatal(err)
				}
				cachePath := filepath.Join(cacheDir, "feed_home.json")
				data, err := json.Marshal(cachedVideos)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, data, 0o644); err != nil {
					t.Fatal(err)
				}
				if tc.staleCache {
					old := time.Now().Add(-10 * time.Minute)
					if err := os.Chtimes(cachePath, old, old); err != nil {
						t.Fatal(err)
					}
				}
			}

			bin := filepath.Join(tmp, "nonexistent-yt-dlp")
			if !tc.binExitFail {
				bin = writeFakeYTDLP(t, tmp, tc.binScript)
			}

			cat := NewYTDLPCatalog(cacheDir, 2*time.Minute, bin)
			defer cat.Wait()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			videos, err := cat.Feed(ctx, model.TabHome)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !IsErrKind(err, ErrForbidden) {
					t.Errorf("expected ErrForbidden, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(videos) == 0 || videos[0].ID != tc.wantFirstID {
				t.Fatalf("got videos=%+v, want first ID %q", videos, tc.wantFirstID)
			}

			if tc.wantRefetch {
				cat.Wait()
				reloaded, _, err := readCacheFile(filepath.Join(cacheDir, "feed_home.json"))
				if err != nil {
					t.Fatalf("expected cache file to be written/updated: %v", err)
				}
				if len(reloaded) == 0 {
					t.Fatalf("expected non-empty cached videos")
				}
				if tc.staleCache && reloaded[0].ID != "fresh9999" {
					t.Errorf("expected stale cache to be revalidated to fresh9999, got %q", reloaded[0].ID)
				}
			}
		})
	}
}

func TestYTDLPCatalog_SearchTSV(t *testing.T) {
	tmp := t.TempDir()
	bin := writeFakeYTDLP(t, tmp, `printf 'vid12345\tGo Concurrency\tGopherChan\t10:00\thttps://youtu.be/vid12345\t42000\n'`)

	cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), time.Minute, bin)
	defer cat.Wait()

	videos, err := cat.Search(context.Background(), "golang")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("got %d videos, want 1", len(videos))
	}
	if videos[0].ID != "vid12345" || videos[0].Title != "Go Concurrency" || videos[0].Views != "42K vistas" {
		t.Errorf("unexpected video: %+v", videos[0])
	}
}
