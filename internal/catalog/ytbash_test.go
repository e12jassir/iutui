package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/e12jassir/ytx/internal/model"
)

func TestParseTSVFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []model.Video
	}{
		{
			name:    "formatted TSV with URL in col 2",
			content: "# comment\n\ndQw4w9WgXcQ\thttps://youtu.be/dQw4w9WgXcQ\tNever Gonna\tRick\t03:33\t1500000\n",
			want: []model.Video{
				{ID: "dQw4w9WgXcQ", Title: "Never Gonna", Channel: "Rick", Duration: "03:33", Views: "1.5M vistas"},
			},
		},
		{
			name:    "raw TSV with live flag and NA normalization",
			content: "live12345\tStream Title\tNA\tNA\thttps://youtube.com/live\tNA\tlive\n",
			want: []model.Video{
				{ID: "live12345", Title: "Stream Title", Channel: "YouTube", Duration: "", Views: "", IsLive: true},
			},
		},
		{
			name:    "legacy history row with timestamp prefix",
			content: "2026-10-10 12:00\tOld Video\tOldChan\t05:00\tabcde12345\thttps://youtube.com/watch?v=abcde12345\n",
			want: []model.Video{
				{ID: "abcde12345", Title: "Old Video", Channel: "OldChan", Duration: "05:00"},
			},
		},
		{
			name:    "skips short IDs, spaces in ID, and malformed lines",
			content: "bad\tShort\tChan\t01:00\nhas space\tTitle\tChan\t01:00\nonlyonecol\n",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "feed.tsv")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("write temp tsv: %v", err)
			}

			got, err := ParseTSVFile(path)
			if err != nil {
				t.Fatalf("ParseTSVFile error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("ParseTSVFile() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBashCatalogCachedFeeds(t *testing.T) {
	cases := []struct {
		name     string
		tab      model.Tab
		inState  bool
		filename string
		content  string
		wantID   string
	}{
		{
			name:     "home reads feed_foryou.tsv",
			tab:      model.TabHome,
			filename: "feed_foryou.tsv",
			content:  "home12345\tHome Video\tChanA\t04:10\thttps://youtu.be/home12345\t1200\n",
			wantID:   "home12345",
		},
		{
			name:     "music reads feed_music.tsv",
			tab:      model.TabMusic,
			filename: "feed_music.tsv",
			content:  "music1234\tTrack One\tArtist\t03:20\thttps://youtu.be/music1234\t500\n",
			wantID:   "music1234",
		},
		{
			name:     "subs reads feed_subs.tsv",
			tab:      model.TabSubscriptions,
			filename: "feed_subs.tsv",
			content:  "subs12345\tSub Video\tCreator\t10:00\thttps://youtu.be/subs12345\t250000\n",
			wantID:   "subs12345",
		},
		{
			name:     "history reads history.tsv from stateDir",
			tab:      model.TabHistory,
			inState:  true,
			filename: "history.tsv",
			content:  "hist12345\tWatched Video\tChanH\t02:15\thttps://youtu.be/hist12345\tNA\n",
			wantID:   "hist12345",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			cacheDir := filepath.Join(tmp, "cache")
			stateDir := filepath.Join(tmp, "state")
			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatal(err)
			}

			targetDir := cacheDir
			if tc.inState {
				targetDir = stateDir
			}
			if err := os.WriteFile(filepath.Join(targetDir, tc.filename), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			cat := NewBashCatalog(cacheDir, stateDir, filepath.Join(tmp, "nonexistent-yt"))
			got, err := cat.Feed(context.Background(), tc.tab)
			if err != nil {
				t.Fatalf("Feed(%v) error: %v", tc.tab, err)
			}
			if len(got) != 1 || got[0].ID != tc.wantID {
				t.Fatalf("Feed(%v) = %+v, want ID %q", tc.tab, got, tc.wantID)
			}
		})
	}
}

func TestBashCatalogSearchAndFallback(t *testing.T) {
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "yt")
	script := `#!/bin/sh
if [ "$1" = "__list" ] && [ "$2" = "search" ]; then
  printf 'srch12345\thttps://youtu.be/srch12345\tFound %s\tSearchChan\t06:00\t4200\n' "$3"
  exit 0
fi
if [ "$1" = "__list" ] && [ "$2" = "foryou" ]; then
  printf 'cli123456\tCLI Video\tCLIChan\t01:30\thttps://youtu.be/cli123456\t100\n'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cat := NewBashCatalog(filepath.Join(tmp, "empty-cache"), filepath.Join(tmp, "empty-state"), fakeBin)

	t.Run("search invokes bin/yt __list search", func(t *testing.T) {
		got, err := cat.Search(context.Background(), "golang")
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "srch12345" || got[0].Title != "Found golang" {
			t.Fatalf("unexpected search result: %+v", got)
		}
	})

	t.Run("empty search returns nil without running command", func(t *testing.T) {
		got, err := cat.Search(context.Background(), "   ")
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("feed falls back to bin/yt __list when cache missing", func(t *testing.T) {
		got, err := cat.Feed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("Feed error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "cli123456" {
			t.Fatalf("unexpected fallback result: %+v", got)
		}
	})
}

func TestBashCatalogContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	slowBin := filepath.Join(tmp, "yt")
	script := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(slowBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cat := NewBashCatalog(filepath.Join(tmp, "cache"), filepath.Join(tmp, "state"), slowBin)

	t.Run("pre-cancelled context returns context.Canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := cat.Feed(ctx, model.TabHome)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Feed err = %v, want context.Canceled", err)
		}

		_, err = cat.Search(ctx, "test")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Search err = %v, want context.Canceled", err)
		}
	})

	t.Run("command timeout cancels running process", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := cat.Search(ctx, "slow")
		elapsed := time.Since(start)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Search err = %v, want context.DeadlineExceeded", err)
		}
		if elapsed > time.Second {
			t.Fatalf("cancellation took too long: %v", elapsed)
		}
	})
}

func TestBashCatalogXDGDefaults(t *testing.T) {
	tmp := t.TempDir()
	cacheBase := filepath.Join(tmp, "xdg-cache")
	stateBase := filepath.Join(tmp, "xdg-state")
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	t.Setenv("XDG_STATE_HOME", stateBase)

	cacheDir := filepath.Join(cacheBase, "yt_terminal")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cacheDir, "feed_music.tsv"),
		[]byte("xdg123456\tXDG Music\tChan\t03:00\thttps://youtu.be/xdg123456\t10\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	cat := NewBashCatalog("", "", "")
	got, err := cat.Feed(context.Background(), model.TabMusic)
	if err != nil {
		t.Fatalf("Feed error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "xdg123456" {
		t.Fatalf("unexpected videos: %+v", got)
	}
}

func TestFakeCatalog(t *testing.T) {
	fake := NewFakeCatalog()
	v1 := model.Video{ID: "vid1", Title: "Go Concurrency", Channel: "Gopher"}
	v2 := model.Video{ID: "vid2", Title: "Lo-Fi Beats", Channel: "Chill"}
	fake.Feeds[model.TabHome] = []model.Video{v1}
	fake.Results["music"] = []model.Video{v2}

	t.Run("feed returns configured tab videos", func(t *testing.T) {
		got, err := fake.Feed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("Feed error: %v", err)
		}
		if !slices.Equal(got, []model.Video{v1}) {
			t.Fatalf("got %+v, want [%+v]", got, v1)
		}
	})

	t.Run("search returns configured query results", func(t *testing.T) {
		got, err := fake.Search(context.Background(), "music")
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if !slices.Equal(got, []model.Video{v2}) {
			t.Fatalf("got %+v, want [%+v]", got, v2)
		}
	})

	t.Run("error injection", func(t *testing.T) {
		wantErr := errors.New("boom")
		fake.Err = wantErr
		defer func() { fake.Err = nil }()

		if _, err := fake.Feed(context.Background(), model.TabHome); !errors.Is(err, wantErr) {
			t.Fatalf("got %v, want %v", err, wantErr)
		}
		if _, err := fake.Search(context.Background(), "music"); !errors.Is(err, wantErr) {
			t.Fatalf("got %v, want %v", err, wantErr)
		}
	})
}
