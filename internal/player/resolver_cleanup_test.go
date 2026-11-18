package player

import (
	"context"
	"fmt"
	"github.com/e12jassir/iutui/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyReprefetchAndOldCleanup(t *testing.T) {
	d := t.TempDir()
	oldStart := filepath.Join(d, "old-start")
	newStart := filepath.Join(d, "new-start")
	oldGate := filepath.Join(d, "old-gate")
	newGate := filepath.Join(d, "new-gate")
	calls := filepath.Join(d, "calls")
	script := fmt.Sprintf(`case "$*" in
 *height\<\=720*) echo old >> %q; touch %q; while [ ! -f %q ]; do sleep 0.01; done; printf 'https://example.com/old\n';;
 *) echo new >> %q; touch %q; while [ ! -f %q ]; do sleep 0.01; done; printf 'https://example.com/new\n';;
 esac`, calls, oldStart, oldGate, calls, newStart, newGate)
	r := NewStreamResolver(writeFakeYTDLP(t, script), 720)
	v := model.Video{ID: "video123"}
	deadline := time.Now().Add(5 * time.Second)
	wait := func(label string, cond func() bool) {
		t.Helper()
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timeout: %s", label)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	exists := func(path string) bool { _, err := os.Stat(path); return err == nil }
	defer os.WriteFile(oldGate, nil, 0600)
	defer os.WriteFile(newGate, nil, 0600)
	r.Prefetch(context.Background(), v)
	wait("old starts", func() bool { return exists(oldStart) })
	r.SetQuality(1080)
	r.Prefetch(context.Background(), v)
	wait("new starts", func() bool { return exists(newStart) })
	if err := os.WriteFile(oldGate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	wait("old finishes", func() bool { return len(r.sem) == 1 })
	// Synchronize with old deferred tracking cleanup, which follows semaphore release.
	wait("old cleanup runs", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.inFlight[v.ID] })
	time.Sleep(50 * time.Millisecond)
	r.mu.RLock()
	busy := r.inFlight[v.ID]
	r.mu.RUnlock()
	if !busy {
		t.Fatal("old cleanup removed newer tracking")
	}
	r.Prefetch(context.Background(), v)
	time.Sleep(50 * time.Millisecond)
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "new\n"); n != 1 {
		t.Fatalf("new calls=%d; calls=%q", n, data)
	}
	if err := os.WriteFile(newGate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	wait("new result cached", func() bool { s, ok := r.GetCached(v.ID); return ok && s.VideoURL == "https://example.com/new" })
}
