package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestYTDLPCatalog_RefreshFeed(t *testing.T) {
	tests := []struct {
		name        string
		tab         model.Tab
		binScript   string
		wantErr     bool
		wantIDs     []string
		wantCache   string
		checkCached bool
	}{
		{
			name:        "bypasses fresh cache and updates cache file on supported tab",
			tab:         model.TabHome,
			binScript:   `printf '{"id":"fresh7777","title":"Refreshed Home","channel":"Chan","duration":90,"view_count":4200}\n'`,
			wantIDs:     []string{"fresh7777"},
			wantCache:   "feed_home.json",
			checkCached: true,
		},
		{
			name:        "refreshes music tab and writes feed_music.json",
			tab:         model.TabMusic,
			binScript:   `printf '{"id":"music8888","title":"Refreshed Track","channel":"Artist","duration":210,"view_count":9000}\n'`,
			wantIDs:     []string{"music8888"},
			wantCache:   "feed_music.json",
			checkCached: true,
		},
		{
			name:      "returns nil for unsupported local tab without invoking yt-dlp",
			tab:       model.TabDownloads,
			binScript: `exit 1`,
			wantIDs:   nil,
		},
		{
			name:      "propagates classified error when yt-dlp fails",
			tab:       model.TabSubscriptions,
			binScript: `echo "ERROR: HTTP Error 429: Too Many Requests" >&2; exit 1`,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			cacheDir := filepath.Join(tmp, "cache")
			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				t.Fatal(err)
			}

			if tc.wantCache != "" {
				staleSeed, err := json.Marshal([]model.Video{
					{ID: "oldcache1", Title: "Old Cached", Channel: "OldChan"},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cacheDir, tc.wantCache), staleSeed, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			bin := writeFakeYTDLP(t, tmp, tc.binScript)
			cat := NewYTDLPCatalog(cacheDir, 10*time.Minute, bin)
			defer cat.Wait()

			got, err := cat.RefreshFeed(context.Background(), tc.tab)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !IsErrKind(err, ErrRateLimit) {
					t.Errorf("expected ErrRateLimit, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tc.wantIDs) {
				t.Fatalf("got %d videos (%+v), want %d", len(got), got, len(tc.wantIDs))
			}
			for i, wantID := range tc.wantIDs {
				if got[i].ID != wantID {
					t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, wantID)
				}
			}

			if tc.checkCached {
				reloaded, _, err := readCacheFile(filepath.Join(cacheDir, tc.wantCache))
				if err != nil {
					t.Fatalf("readCacheFile: %v", err)
				}
				if len(reloaded) != len(tc.wantIDs) || reloaded[0].ID != tc.wantIDs[0] {
					t.Fatalf("cached videos = %+v, want %v", reloaded, tc.wantIDs)
				}
			}
		})
	}
}

func TestHybridCatalog_NativeFirstAndFallbacks(t *testing.T) {
	t.Run("Feed and RefreshFeed prefer Direct YTDLPCatalog over BashCatalog on supported tabs", func(t *testing.T) {
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		stateDir := filepath.Join(tmp, "state")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}

		// Seed a legacy TSV cache that BashCatalog would serve if it ran first.
		tsv := "tsv00001\thttps://www.youtube.com/watch?v=tsv00001\tTSV Video\tChan\t03:00\t1000\n"
		if err := os.WriteFile(filepath.Join(cacheDir, "feed_foryou.tsv"), []byte(tsv), 0o644); err != nil {
			t.Fatal(err)
		}

		ytdlpBin := writeFakeYTDLP(t, tmp, `printf '{"id":"native001","title":"Native Go Video","channel":"GoChan","duration":180,"view_count":5000}\n'`)
		bashBin := filepath.Join(tmp, "yt")
		if err := os.WriteFile(bashBin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		h := NewHybridCatalog(
			NewBashCatalog(cacheDir, stateDir, bashBin),
			NewYTDLPCatalog(cacheDir, 5*time.Minute, ytdlpBin),
		)

		feed, err := h.Feed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("Feed error: %v", err)
		}
		if len(feed) != 1 || feed[0].ID != "native001" {
			t.Fatalf("Feed = %+v, want native001", feed)
		}

		refreshed, err := h.RefreshFeed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("RefreshFeed error: %v", err)
		}
		if len(refreshed) != 1 || refreshed[0].ID != "native001" {
			t.Fatalf("RefreshFeed = %+v, want native001", refreshed)
		}
	})

	t.Run("RefreshFeed falls back to BashCatalog when Direct fails and handles local tabs", func(t *testing.T) {
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		stateDir := filepath.Join(tmp, "state")
		if err := os.MkdirAll(filepath.Join(stateDir, "downloads"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "downloads", "dl00001.meta"), []byte("Saved Song\nSaved Artist\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "history.tsv"), []byte("hist0001\tWatched\tChan\t02:00\thttps://youtu.be/hist0001\t100\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		ytStub := filepath.Join(tmp, "yt")
		if err := os.WriteFile(ytStub, []byte("#!/bin/sh\nprintf 'bash0001\thttps://www.youtube.com/watch?v=bash0001\tBash Refreshed\tChan\t02:30\t300\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		badYtdlp := writeFakeYTDLP(t, tmp, `exit 1`)

		h := NewHybridCatalog(
			NewBashCatalog(cacheDir, stateDir, ytStub),
			NewYTDLPCatalog(cacheDir, 5*time.Minute, badYtdlp),
		)

		ctx := context.Background()
		refreshed, err := h.RefreshFeed(ctx, model.TabHome)
		if err != nil || len(refreshed) != 1 || refreshed[0].ID != "bash0001" {
			t.Fatalf("RefreshFeed fallback = %+v, err=%v", refreshed, err)
		}

		dls, err := h.Feed(ctx, model.TabDownloads)
		if err != nil || len(dls) != 1 || dls[0].ID != "dl00001" {
			t.Fatalf("TabDownloads = %+v, err=%v", dls, err)
		}

		hist, err := h.Feed(ctx, model.TabHistory)
		if err != nil || len(hist) != 1 || hist[0].ID != "hist0001" {
			t.Fatalf("TabHistory = %+v, err=%v", hist, err)
		}
	})

	t.Run("works when Feeds is nil and Direct is configured", func(t *testing.T) {
		tmp := t.TempDir()
		ytdlpBin := writeFakeYTDLP(t, tmp, `printf '{"id":"onlydir01","title":"Direct Only","channel":"Chan","duration":60,"view_count":100}\n'`)
		h := NewHybridCatalog(nil, NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, ytdlpBin))

		ctx := context.Background()
		feed, err := h.Feed(ctx, model.TabHome)
		if err != nil || len(feed) != 1 || feed[0].ID != "onlydir01" {
			t.Fatalf("Direct-only Feed = %+v, err=%v", feed, err)
		}

		refreshed, err := h.RefreshFeed(ctx, model.TabHome)
		if err != nil || len(refreshed) != 1 || refreshed[0].ID != "onlydir01" {
			t.Fatalf("Direct-only RefreshFeed = %+v, err=%v", refreshed, err)
		}
	})
}
