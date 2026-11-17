package player

import (
	"context"
	"fmt"
	"github.com/e12jassir/ytx/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyReprefetchAfterQualityChange(t *testing.T) {
	d := t.TempDir()
	started := filepath.Join(d, "started")
	gate := filepath.Join(d, "gate")
	calls := filepath.Join(d, "calls")
	bin := writeFakeYTDLP(t, fmt.Sprintf("echo call >> %q\ntouch %q\nwhile [ ! -f %q ]; do sleep 0.01; done\nprintf 'https://example.com/stream\\n'", calls, started, gate))
	r := NewStreamResolver(bin, 720)
	v := model.Video{ID: "video123"}
	r.Prefetch(context.Background(), v)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(started); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not started")
		}
		time.Sleep(time.Millisecond * 5)
	}
	r.SetQuality(1080)
	r.Prefetch(context.Background(), v)
	os.WriteFile(gate, nil, 0600)
	for {
		r.mu.RLock()
		busy := r.inFlight[v.ID]
		r.mu.RUnlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not finished")
		}
		time.Sleep(time.Millisecond * 5)
	}
	if _, ok := r.GetCached(v.ID); !ok {
		b, _ := os.ReadFile(calls)
		t.Fatalf("re-prefetch suppressed; no current-quality cached stream; calls=%q", b)
	}
}
