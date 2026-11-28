package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestResumeStore_LongVideosOnlyAndCap(t *testing.T) {
	dir := t.TempDir()
	rs := NewResumeStore(dir)
	rs.MaxEntries = 3

	// Any video duration is stored when position is within [5s, duration-5s) and < 95%.
	if err := rs.SavePosition("short1", 120, 240); err != nil {
		t.Fatalf("SavePosition short error: %v", err)
	}
	if pos, ok := rs.GetPosition("short1"); !ok || pos != 120 {
		t.Fatalf("expected short video saved at 120, got (%v, %v)", pos, ok)
	}
	if ratio := rs.ProgressRatio("short1"); ratio < 0.49 || ratio > 0.51 {
		t.Fatalf("ProgressRatio(short1) = %v, want ~0.5", ratio)
	}

	// Below 5s start or near EOF (>= duration-5 or >= 95%) clears saved state.
	if err := rs.SavePosition("long1", 400, 1800); err != nil {
		t.Fatalf("SavePosition long1 error: %v", err)
	}
	if pos, ok := rs.GetPosition("long1"); !ok || pos != 400 {
		t.Fatalf("GetPosition(long1) = (%v, %v), want (400, true)", pos, ok)
	}
	if err := rs.SavePosition("long1", 1785, 1800); err != nil {
		t.Fatalf("SavePosition near EOF error: %v", err)
	}
	if _, ok := rs.GetPosition("long1"); ok {
		t.Fatal("expected near-EOF save to clear resume entry")
	}

	// Verify eviction keeps at most MaxEntries newest entries.
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("vid-%d", i)
		if err := rs.SavePosition(id, float64(60*i), 2000); err != nil {
			t.Fatalf("SavePosition(%s) error: %v", id, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	for i := 1; i <= 2; i++ {
		if _, ok := rs.GetPosition(fmt.Sprintf("vid-%d", i)); ok {
			t.Fatalf("expected oldest vid-%d to be evicted", i)
		}
	}
	for i := 3; i <= 5; i++ {
		if pos, ok := rs.GetPosition(fmt.Sprintf("vid-%d", i)); !ok || pos != float64(60*i) {
			t.Fatalf("expected vid-%d at %v, got (%v, %v)", i, 60*i, pos, ok)
		}
	}
}

func TestSearchHistory_RecordDedupeAndCap(t *testing.T) {
	dir := t.TempDir()
	sh := NewSearchHistory(dir)
	sh.MaxEntries = 3

	for _, q := range []string{"pink floyd", "soda stereo", "diomedes diaz", "PINK FLOYD", "canserbero"} {
		if err := sh.Record(q); err != nil {
			t.Fatalf("Record(%q) error: %v", q, err)
		}
	}

	got, err := sh.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	want := []string{"canserbero", "PINK FLOYD", "diomedes diaz"}
	if !slices.Equal(got, want) {
		t.Fatalf("SearchHistory.List() = %v, want %v", got, want)
	}
}

func TestListeningStats_BoundsEntriesAndPersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	seeded := make(map[string]TrackStat, 505)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 505; i++ {
		id := fmt.Sprintf("trk%08d", i)
		seeded[id] = TrackStat{
			VideoID:      id,
			Title:        fmt.Sprintf("Track %d", i),
			Artist:       "Artist",
			Plays:        1,
			LastPlayedAt: base.Add(time.Duration(i) * time.Minute),
		}
	}
	rawSeed, err := json.Marshal(seeded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, musicStatsFile), rawSeed, 0o600); err != nil {
		t.Fatal(err)
	}

	ls := NewListeningStats(dir)
	if err := ls.RecordPlay(model.Video{
		ID:       "trk00000506",
		Title:    "Newest Track",
		Channel:  "Soda Stereo",
		Duration: "03:30",
	}); err != nil {
		t.Fatalf("RecordPlay error: %v", err)
	}

	snap := ls.Snapshot()
	if len(snap) != 500 {
		t.Fatalf("Snapshot() len = %d, want bounded 500", len(snap))
	}
	if _, ok := snap["trk00000506"]; !ok {
		t.Fatal("expected newest track trk00000506 to be retained")
	}
	if _, ok := snap["trk00000001"]; ok {
		t.Fatal("expected oldest track trk00000001 to be evicted")
	}
}
