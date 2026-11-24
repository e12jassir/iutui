package store

import (
	"fmt"
	"slices"
	"testing"
	"time"
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
