package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestRefreshFeedIsNotOverwrittenByOlderRevalidation(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "feed_home.json")
	if err := writeCacheFile(cache, []model.Video{{ID: "cached123"}}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	started, gate := filepath.Join(dir, "started"), filepath.Join(dir, "gate")
	script := fmt.Sprintf(`if [ ! -f %q ]; then
 touch %q
 while [ ! -f %q ]; do sleep 0.01; done
 printf '{"id":"older123","title":"old"}\n'
else
 printf '{"id":"newer123","title":"new"}\n'
fi`, started, started, gate)
	cat := NewYTDLPCatalog(dir, time.Minute, writeFakeYTDLP(t, dir, script))
	if _, err := cat.Feed(context.Background(), model.TabHome); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revalidation did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := cat.RefreshFeed(context.Background(), model.TabHome); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cat.Wait()
	videos, _, err := readCacheFile(cache)
	if err != nil || len(videos) != 1 || videos[0].ID != "newer123" {
		t.Fatalf("cache = %+v, err = %v; want newer result", videos, err)
	}
}
