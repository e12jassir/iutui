package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
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
		{
			name:     "watch later reads watchlater.tsv from stateDir",
			tab:      model.TabWatchLater,
			inState:  true,
			filename: "watchlater.tsv",
			content:  "later1234\tLater Video\tChanL\t08:40\thttps://youtu.be/later1234\tNA\n",
			wantID:   "later1234",
		},
		{
			name:     "liked reads liked.tsv from stateDir",
			tab:      model.TabLiked,
			inState:  true,
			filename: "liked.tsv",
			content:  "liked1234\tLiked Video\tChanK\t03:45\thttps://youtu.be/liked1234\tNA\n",
			wantID:   "liked1234",
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

			cat := NewBashCatalog(cacheDir, stateDir, "")
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

func TestBashCatalogDownloadsAndCacheMiss(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "empty-cache")
	stateDir := filepath.Join(tmp, "state")
	dlDir := filepath.Join(stateDir, "downloads")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dlDir, "dlvid1234.meta"), []byte("Offline Track\nOffline Artist\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cat := NewBashCatalog(cacheDir, stateDir, filepath.Join(tmp, "nonexistent-yt"))
	ctx := context.Background()

	t.Run("downloads reads local meta entries on Feed and RefreshFeed", func(t *testing.T) {
		for _, refresh := range []bool{false, true} {
			var (
				got []model.Video
				err error
			)
			if refresh {
				got, err = cat.RefreshFeed(ctx, model.TabDownloads)
			} else {
				got, err = cat.Feed(ctx, model.TabDownloads)
			}
			if err != nil {
				t.Fatalf("refresh=%v downloads error: %v", refresh, err)
			}
			if len(got) != 1 || got[0].ID != "dlvid1234" || got[0].Title != "Offline Track" || got[0].Channel != "Offline Artist" {
				t.Fatalf("refresh=%v downloads = %+v", refresh, got)
			}
		}
	})

	t.Run("feed returns nil without error on cache miss", func(t *testing.T) {
		for _, tab := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabWatchLater, model.TabLiked} {
			got, err := cat.Feed(ctx, tab)
			if err != nil {
				t.Fatalf("Feed(%v) error: %v", tab, err)
			}
			if got != nil {
				t.Fatalf("Feed(%v) = %+v, want nil on cache miss", tab, got)
			}
		}
	})

	t.Run("refresh returns nil for non-downloads tabs without spawning binary", func(t *testing.T) {
		for _, tab := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabWatchLater, model.TabLiked} {
			got, err := cat.RefreshFeed(ctx, tab)
			if err != nil {
				t.Fatalf("RefreshFeed(%v) error: %v", tab, err)
			}
			if got != nil {
				t.Fatalf("RefreshFeed(%v) = %+v, want nil", tab, got)
			}
		}
	})

	t.Run("search returns nil without spawning binary", func(t *testing.T) {
		for _, q := range []string{"", "   ", "golang"} {
			got, err := cat.Search(ctx, q)
			if err != nil {
				t.Fatalf("Search(%q) error: %v", q, err)
			}
			if got != nil {
				t.Fatalf("Search(%q) = %+v, want nil", q, got)
			}
		}
	})
}

func TestBashCatalogContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	cat := NewBashCatalog(filepath.Join(tmp, "cache"), filepath.Join(tmp, "state"), "")

	t.Run("pre-cancelled context returns context.Canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := cat.Feed(ctx, model.TabHome); !errors.Is(err, context.Canceled) {
			t.Fatalf("Feed err = %v, want context.Canceled", err)
		}
		if _, err := cat.RefreshFeed(ctx, model.TabDownloads); !errors.Is(err, context.Canceled) {
			t.Fatalf("RefreshFeed err = %v, want context.Canceled", err)
		}
		if _, err := cat.Search(ctx, "test"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Search err = %v, want context.Canceled", err)
		}
	})

	t.Run("expired deadline returns context.DeadlineExceeded", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		if _, err := cat.Feed(ctx, model.TabHome); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Feed err = %v, want context.DeadlineExceeded", err)
		}
		if _, err := cat.RefreshFeed(ctx, model.TabHome); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("RefreshFeed err = %v, want context.DeadlineExceeded", err)
		}
		if _, err := cat.Search(ctx, "slow"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Search err = %v, want context.DeadlineExceeded", err)
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

	stateDir := filepath.Join(stateBase, "yt")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(stateDir, "watchlater.tsv"),
		[]byte("xdglater1\tXDG Later\tChanL\t04:00\thttps://youtu.be/xdglater1\tNA\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	cat := NewBashCatalog("", "", "")
	got, err := cat.Feed(context.Background(), model.TabMusic)
	if err != nil {
		t.Fatalf("Feed(TabMusic) error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "xdg123456" {
		t.Fatalf("unexpected music videos: %+v", got)
	}

	gotLater, err := cat.Feed(context.Background(), model.TabWatchLater)
	if err != nil {
		t.Fatalf("Feed(TabWatchLater) error: %v", err)
	}
	if len(gotLater) != 1 || gotLater[0].ID != "xdglater1" {
		t.Fatalf("unexpected watchlater videos: %+v", gotLater)
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

func TestNormalizeMixFeedItems(t *testing.T) {
	input := strings.Join([]string{
		"RDkNvTIyNpYSE\tMix - Cerati\tNA\tlive\thttps://www.youtube.com/playlist?list=RDkNvTIyNpYSE\tNA\tlive",
		"RDdQw4w9WgXcQ\tMix - Rick\tNA\tNA\thttps://www.youtube.com/playlist?list=RDdQw4w9WgXcQ\tNA\tNA",
		"live1234567\tReal Stream\tGamer\tNA\thttps://www.youtube.com/watch?v=live1234567\tNA\tlive",
	}, "\n")
	got, err := ParseTSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTSV failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	for i := 0; i < 2; i++ {
		if got[i].IsLive {
			t.Errorf("row[%d] (%s) IsLive = true, want false for Mix", i, got[i].ID)
		}
		if got[i].Duration != "MIX" {
			t.Errorf("row[%d] (%s) Duration = %q, want MIX", i, got[i].ID, got[i].Duration)
		}
	}
	if !got[2].IsLive {
		t.Errorf("row[2] real livestream should stay IsLive=true")
	}
}
