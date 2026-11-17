package player

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/e12jassir/ytx/internal/model"
)

func TestPrefetchDiscardsResultFromPreviousQuality(t *testing.T) {
	dir := t.TempDir()
	started, gate := filepath.Join(dir, "started"), filepath.Join(dir, "gate")
	bin := writeFakeYTDLP(t, fmt.Sprintf("touch %q\nwhile [ ! -f %q ]; do sleep 0.01; done\nprintf 'https://example.com/old\\n'", started, gate))
	r := NewStreamResolver(bin, 720)
	r.Prefetch(context.Background(), model.Video{ID: "video123"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lookup did not start")
		}
		time.Sleep(time.Millisecond * 10)
	}
	r.SetQuality(1080)
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		busy := len(r.sem) != 0
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lookup did not finish")
		}
		time.Sleep(time.Millisecond * 10)
	}
	if st, ok := r.GetCached("video123"); ok {
		t.Fatalf("stale stream cached: %+v", st)
	}
}
