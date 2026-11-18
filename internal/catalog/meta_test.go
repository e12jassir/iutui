package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/e12jassir/iutui/internal/model"
)

func TestMetadataEnricher_EnrichAndCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var body struct {
			VideoID string `json:"videoId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch body.VideoID {
		case "vidAAAAAAA1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"viewCount": "2633540",
				},
				"microformat": map[string]any{
					"playerMicroformatRenderer": map[string]any{
						"publishDate": "2026-11-17T12:13:00-08:00",
					},
				},
			})
		case "vidBBBBBBB2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"viewCount": "450",
				},
				"microformat": map[string]any{
					"playerMicroformatRenderer": map[string]any{
						"uploadDate": "2024-05-10",
					},
				},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	enr := NewMetadataEnricher(cacheDir)
	enr.EndpointURL = srv.URL

	got, err := enr.Enrich(context.Background(), []string{
		"vidAAAAAAA1",
		"vidAAAAAAA1", // duplicate should only trigger 1 request
		"RDmix12345678",
		"short",
		"vidBBBBBBB2",
		"vidCCCCCCC3", // fails 404, skipped best-effort
	})
	if err != nil {
		t.Fatalf("Enrich error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 enriched entries, got %+v", got)
	}
	if got["vidAAAAAAA1"].Views != "2.6M vistas" || got["vidAAAAAAA1"].Published != "2026-11-17T12:13:00-08:00" {
		t.Fatalf("unexpected meta for vidAAAAAAA1: %+v", got["vidAAAAAAA1"])
	}
	if got["vidBBBBBBB2"].Views != "450 vistas" || got["vidBBBBBBB2"].Published != "2024-05-10" {
		t.Fatalf("unexpected meta for vidBBBBBBB2: %+v", got["vidBBBBBBB2"])
	}
	if gotHits := hits.Load(); gotHits != 3 {
		t.Fatalf("expected 3 HTTP calls for unique single IDs, got %d", gotHits)
	}

	// Verify disk cache persistence with a fresh MetadataEnricher instance.
	reloaded := NewMetadataEnricher(cacheDir)
	reloaded.EndpointURL = srv.URL

	hits.Store(0)
	got2, err := reloaded.Enrich(context.Background(), []string{"vidAAAAAAA1", "vidBBBBBBB2"})
	if err != nil {
		t.Fatalf("reloaded Enrich error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("expected 0 HTTP hits from disk cache, got %d", hits.Load())
	}
	if got2["vidAAAAAAA1"].Views != "2.6M vistas" || got2["vidBBBBBBB2"].Published != "2024-05-10" {
		t.Fatalf("unexpected reloaded meta: %+v", got2)
	}

	videos := []model.Video{
		{ID: "vidAAAAAAA1", Title: "One"},
		{ID: "vidBBBBBBB2", Title: "Two", Views: "Preserved"},
		{ID: "RDmix12345678", Title: "Mix"},
	}
	reloaded.ApplyCached(videos)
	if videos[0].Views != "2.6M vistas" || videos[0].Published != "2026-11-17T12:13:00-08:00" {
		t.Fatalf("ApplyCached did not populate video 0: %+v", videos[0])
	}
	if videos[1].Views != "Preserved" || videos[1].Published != "2024-05-10" {
		t.Fatalf("ApplyCached should keep non-empty Views and fill empty Published: %+v", videos[1])
	}
	if videos[2].Views != "" || videos[2].Published != "" {
		t.Fatalf("ApplyCached should ignore playlist IDs: %+v", videos[2])
	}
}
