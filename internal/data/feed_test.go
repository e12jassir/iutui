package data

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/e12jassir/ytx/internal/model"
)

func TestParseTSV(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []model.Video
	}{
		{
			name:    "Formatted TSV with URL in col 2",
			content: "# comment\n\ndQw4w9WgXcQ\thttps://youtu.be/dQw4w9WgXcQ\tNever Gonna\tRick\t03:33\t1500000\n",
			want: []model.Video{
				{ID: "dQw4w9WgXcQ", Title: "Never Gonna", Channel: "Rick", Duration: "03:33", Views: "1.5M vistas"},
			},
		},
		{
			name:    "Raw TSV with live flag and NA normalization",
			content: "live12345\tStream Title\tNA\tNA\thttps://youtube.com/live\tNA\tlive\n",
			want: []model.Video{
				{ID: "live12345", Title: "Stream Title", Channel: "YouTube", Duration: "", Views: "", IsLive: true},
			},
		},
		{
			name:    "Legacy history row with timestamp prefix",
			content: "2026-10-10 12:00\tOld Video\tOldChan\t05:00\tabcde12345\thttps://youtube.com/watch?v=abcde12345\n",
			want: []model.Video{
				{ID: "abcde12345", Title: "Old Video", Channel: "OldChan", Duration: "05:00"},
			},
		},
		{
			name:    "Skips short IDs and malformed lines",
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

			got, err := parseTSV(path)
			if err != nil {
				t.Fatalf("parseTSV error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d videos, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("video[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseTSVMissingFile(t *testing.T) {
	_, err := parseTSV(filepath.Join(t.TempDir(), "missing.tsv"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestFormatHumanViews(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", ""},
		{"1.7M", "1.7M"},
		{"500", "500 vistas"},
		{"1500", "1.5K vistas"},
		{"250000", "250K vistas"},
		{"1500000", "1.5M vistas"},
		{"2000000000", "2B vistas"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if got := formatHumanViews(tc.raw); got != tc.want {
				t.Errorf("formatHumanViews(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDefaultPathsAndLoadDownloads(t *testing.T) {
	tmp := t.TempDir()
	cacheBase := filepath.Join(tmp, "cache")
	stateBase := filepath.Join(tmp, "state")
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	t.Setenv("XDG_STATE_HOME", stateBase)

	gotCache, gotState := DefaultPaths()
	if gotCache != filepath.Join(cacheBase, "yt_terminal") {
		t.Errorf("cacheDir = %q", gotCache)
	}
	if gotState != filepath.Join(stateBase, "yt") {
		t.Errorf("stateDir = %q", gotState)
	}

	dlDir := filepath.Join(gotState, "downloads")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dlDir, "vid999.meta"), []byte("Mi Video\nMi Canal\n"), 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dlDir, "ignore.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	videos, err := LoadFeed(model.TabDownloads)
	if err != nil {
		t.Fatalf("LoadFeed(TabDownloads) error: %v", err)
	}
	if len(videos) != 1 || videos[0].ID != "vid999" || videos[0].Title != "Mi Video" || videos[0].Duration != "Descargado" {
		t.Fatalf("unexpected downloads: %+v", videos)
	}
}
