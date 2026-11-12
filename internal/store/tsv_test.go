package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/e12jassir/ytx/internal/model"
)

func TestTSVStoreToggleSaved(t *testing.T) {
	v1 := model.Video{ID: "vid11111", Title: "First Video", Channel: "ChanA", Duration: "05:00"}
	v2 := model.Video{ID: "vid22222", Title: "Second Video", Channel: "ChanB", Duration: "10:00"}

	cases := []struct {
		name      string
		actions   []model.Video
		wantState []bool
		wantIDs   []string
	}{
		{
			name:      "save then unsave single video",
			actions:   []model.Video{v1, v1},
			wantState: []bool{true, false},
			wantIDs:   nil,
		},
		{
			name:      "save two, unsave first, second remains",
			actions:   []model.Video{v1, v2, v1},
			wantState: []bool{true, true, false},
			wantIDs:   []string{"vid22222"},
		},
		{
			name:      "save two, unsave second, first remains",
			actions:   []model.Video{v1, v2, v2},
			wantState: []bool{true, true, false},
			wantIDs:   []string{"vid11111"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := NewTSVStore(dir)
			ctx := context.Background()

			for i, v := range tc.actions {
				gotSaved, err := s.ToggleSaved(ctx, v)
				if err != nil {
					t.Fatalf("step %d ToggleSaved error: %v", i, err)
				}
				if gotSaved != tc.wantState[i] {
					t.Errorf("step %d ToggleSaved = %v, want %v", i, gotSaved, tc.wantState[i])
				}
			}

			gotVideos, err := s.Saved(ctx)
			if err != nil {
				t.Fatalf("Saved error: %v", err)
			}

			var gotIDs []string
			for _, v := range gotVideos {
				gotIDs = append(gotIDs, v.ID)
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Errorf("Saved IDs = %v, want %v", gotIDs, tc.wantIDs)
			}

			// Verify atomic writes left no temp files behind.
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".tmp-") {
					t.Errorf("leftover temp file %q", e.Name())
				}
			}
		})
	}
}

func TestTSVStoreRecordHistoryPrependDedup(t *testing.T) {
	vA := model.Video{ID: "vidAAAAA", Title: "Alpha", Channel: "ChanA", Duration: "01:00"}
	vB := model.Video{ID: "vidBBBBB", Title: "Beta", Channel: "ChanB", Duration: "02:00"}
	vC := model.Video{ID: "vidCCCCC", Title: "Gamma", Channel: "ChanC", Duration: "03:00"}

	cases := []struct {
		name    string
		record  []model.Video
		wantIDs []string
	}{
		{
			name:    "prepends newest first",
			record:  []model.Video{vA, vB, vC},
			wantIDs: []string{"vidCCCCC", "vidBBBBB", "vidAAAAA"},
		},
		{
			name:    "deduplicates and moves re-watched video to front",
			record:  []model.Video{vA, vB, vC, vA},
			wantIDs: []string{"vidAAAAA", "vidCCCCC", "vidBBBBB"},
		},
		{
			name:    "repeated consecutive plays keep single entry",
			record:  []model.Video{vA, vA, vA},
			wantIDs: []string{"vidAAAAA"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTSVStore(t.TempDir())
			ctx := context.Background()

			for _, v := range tc.record {
				if err := s.RecordHistory(ctx, v); err != nil {
					t.Fatalf("RecordHistory(%s) error: %v", v.ID, err)
				}
			}

			got, err := s.History(ctx)
			if err != nil {
				t.Fatalf("History error: %v", err)
			}
			var gotIDs []string
			for _, v := range got {
				gotIDs = append(gotIDs, v.ID)
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Errorf("History IDs = %v, want %v", gotIDs, tc.wantIDs)
			}
		})
	}
}

func TestTSVStoreSubscriptions(t *testing.T) {
	cases := []struct {
		name    string
		content string
		create  bool
		want    []string
	}{
		{
			name:   "missing file returns nil",
			create: false,
			want:   nil,
		},
		{
			name:    "reads channels skipping comments and blank lines",
			create:  true,
			content: "# my subscriptions\n\nUC1234567890\n@fireship\n\t@3blue1brown\t3Blue1Brown\n",
			want:    []string{"UC1234567890", "@fireship", "@3blue1brown"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.create {
				path := filepath.Join(dir, subscriptionsFile)
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			s := NewTSVStore(dir)
			got, err := s.Subscriptions(context.Background())
			if err != nil {
				t.Fatalf("Subscriptions error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Subscriptions() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTSVStoreDefaultsAndContext(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	s := NewTSVStore("")
	wantDir := filepath.Join(tmp, "yt")
	if s.StateDir != wantDir {
		t.Fatalf("StateDir = %q, want %q", s.StateDir, wantDir)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Saved(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Saved(canceled) = %v, want context.Canceled", err)
	}
	if _, err := s.ToggleSaved(ctx, model.Video{ID: "abcde"}); !errors.Is(err, context.Canceled) {
		t.Errorf("ToggleSaved(canceled) = %v, want context.Canceled", err)
	}
	if _, err := s.History(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("History(canceled) = %v, want context.Canceled", err)
	}
	if err := s.RecordHistory(ctx, model.Video{ID: "abcde"}); !errors.Is(err, context.Canceled) {
		t.Errorf("RecordHistory(canceled) = %v, want context.Canceled", err)
	}
	if _, err := s.Subscriptions(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Subscriptions(canceled) = %v, want context.Canceled", err)
	}
}

func TestFakeStore(t *testing.T) {
	ctx := context.Background()
	fs := NewFakeStore()
	fs.Subs = []string{"@gopher"}

	v1 := model.Video{ID: "v1", Title: "One"}
	v2 := model.Video{ID: "v2", Title: "Two"}

	saved, err := fs.ToggleSaved(ctx, v1)
	if err != nil || !saved {
		t.Fatalf("ToggleSaved add = (%v, %v), want (true, nil)", saved, err)
	}
	saved, err = fs.ToggleSaved(ctx, v1)
	if err != nil || saved {
		t.Fatalf("ToggleSaved remove = (%v, %v), want (false, nil)", saved, err)
	}

	if err := fs.RecordHistory(ctx, v1); err != nil {
		t.Fatal(err)
	}
	if err := fs.RecordHistory(ctx, v2); err != nil {
		t.Fatal(err)
	}
	if err := fs.RecordHistory(ctx, v1); err != nil {
		t.Fatal(err)
	}

	hist, err := fs.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].ID != "v1" || hist[1].ID != "v2" {
		t.Fatalf("unexpected history: %+v", hist)
	}

	subs, err := fs.Subscriptions(ctx)
	if err != nil || !slices.Equal(subs, []string{"@gopher"}) {
		t.Fatalf("Subscriptions = (%v, %v)", subs, err)
	}
}
