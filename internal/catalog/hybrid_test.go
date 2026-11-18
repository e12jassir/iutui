package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestHybridCatalogAndSearchCache(t *testing.T) {
	t.Run("YTDLPCatalog Search caches repeated queries in memory", func(t *testing.T) {
		tmp := t.TempDir()
		countFile := filepath.Join(tmp, "calls")
		script := filepath.Join(tmp, "yt-dlp")
		src := `#!/bin/sh
echo "1" >> "` + countFile + `"
printf '{"id":"abc123def45","title":"Pink Floyd Time","channel":"Pink Floyd","duration":417,"view_count":1000}\n'
`
		if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
			t.Fatal(err)
		}

		cat := NewYTDLPCatalog(tmp, 5*time.Minute, script)
		ctx := context.Background()

		res1, err := cat.Search(ctx, "pink floyd")
		if err != nil {
			t.Fatalf("first search failed: %v", err)
		}
		if len(res1) != 1 || res1[0].ID != "abc123def45" {
			t.Fatalf("unexpected results: %+v", res1)
		}

		res2, err := cat.Search(ctx, "pink floyd")
		if err != nil {
			t.Fatalf("second search failed: %v", err)
		}
		if len(res2) != 1 || res2[0].ID != "abc123def45" {
			t.Fatalf("unexpected cached results: %+v", res2)
		}

		raw, err := os.ReadFile(countFile)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != 2 { // "1\n" once
			t.Fatalf("expected yt-dlp called once, count file = %q", string(raw))
		}
	})

	t.Run("HybridCatalog uses local TSV feed and falls back to BashCatalog on search failure", func(t *testing.T) {
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		stateDir := filepath.Join(tmp, "state")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}

		tsv := "vid001\thttps://www.youtube.com/watch?v=vid001\tCached Video\tChan\t03:00\t1K\tCached Video\n"
		if err := os.WriteFile(filepath.Join(cacheDir, "feed_foryou.tsv"), []byte(tsv), 0o644); err != nil {
			t.Fatal(err)
		}

		ytStub := filepath.Join(tmp, "yt")
		if err := os.WriteFile(ytStub, []byte("#!/bin/sh\nprintf 'fb001\\thttps://www.youtube.com/watch?v=fb001\\tFallback\\tChan\\t01:00\\t500\\tFallback\\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		badYtdlp := filepath.Join(tmp, "bad-yt-dlp")
		if err := os.WriteFile(badYtdlp, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		h := NewHybridCatalog(
			NewBashCatalog(cacheDir, stateDir, ytStub),
			NewYTDLPCatalog(cacheDir, 5*time.Minute, badYtdlp),
		)

		ctx := context.Background()
		feed, err := h.Feed(ctx, model.TabHome)
		if err != nil || len(feed) != 1 || feed[0].ID != "vid001" {
			t.Fatalf("Feed = %+v, err=%v", feed, err)
		}

		res, err := h.Search(ctx, "fallback")
		if err != nil || len(res) != 1 || res[0].ID != "fb001" {
			t.Fatalf("Search fallback = %+v, err=%v", res, err)
		}
	})
}
