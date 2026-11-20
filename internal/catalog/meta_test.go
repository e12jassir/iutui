package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestMetadataEnricher_DurationAndCache(t *testing.T) {
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
		case "durShort111":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"lengthSeconds": "214",
				},
			})
		case "durLong2222":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"viewCount":     "1200",
					"lengthSeconds": "3665",
				},
			})
		case "durZero3333":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"lengthSeconds": "0",
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

	got, err := enr.Enrich(context.Background(), []string{"durShort111", "durLong2222", "durZero3333"})
	if err != nil {
		t.Fatalf("Enrich error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 enriched entries (zero-duration-only skipped), got %+v", got)
	}
	if got["durShort111"].Duration != "03:34" {
		t.Fatalf("durShort111 Duration = %q, want %q", got["durShort111"].Duration, "03:34")
	}
	if got["durLong2222"].Duration != "1:01:05" || got["durLong2222"].Views != "1.2K vistas" {
		t.Fatalf("durLong2222 meta = %+v, want Duration=1:01:05 Views=1.2K vistas", got["durLong2222"])
	}

	// Verify disk cache persistence with a fresh MetadataEnricher instance.
	reloaded := NewMetadataEnricher(cacheDir)
	reloaded.EndpointURL = srv.URL

	hits.Store(0)
	got2, err := reloaded.Enrich(context.Background(), []string{"durShort111", "durLong2222"})
	if err != nil {
		t.Fatalf("reloaded Enrich error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("expected 0 HTTP hits from disk cache, got %d", hits.Load())
	}
	if got2["durShort111"].Duration != "03:34" || got2["durLong2222"].Duration != "1:01:05" {
		t.Fatalf("unexpected reloaded duration meta: %+v", got2)
	}
	if cached, ok := reloaded.Cached("durShort111"); !ok || cached.Duration != "03:34" {
		t.Fatalf("Cached(durShort111) = (%+v, %v), want Duration=03:34", cached, ok)
	}

	videos := []model.Video{
		{ID: "durShort111", Title: "Short", Views: "10 vistas", Published: "2026-01-01", ChannelID: "UC111"},
		{ID: "durLong2222", Title: "Long", Duration: "9:99"},
	}
	reloaded.ApplyCached(videos)
	if videos[0].Duration != "03:34" {
		t.Fatalf("ApplyCached did not fill missing Duration: %+v", videos[0])
	}
	if videos[1].Duration != "9:99" || videos[1].Views != "1.2K vistas" {
		t.Fatalf("ApplyCached should preserve existing Duration and fill missing Views: %+v", videos[1])
	}
}

func TestMetadataEnricher_IsLiveAndCache(t *testing.T) {
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
		case "liveStat111":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"lengthSeconds": "0",
					"isLive":        true,
				},
			})
		case "liveMic2222":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"lengthSeconds": "0",
				},
				"microformat": map[string]any{
					"playerMicroformatRenderer": map[string]any{
						"liveBroadcastDetails": map[string]any{
							"isLiveNow": true,
						},
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

	got, err := enr.Enrich(context.Background(), []string{"liveStat111", "liveMic2222"})
	if err != nil {
		t.Fatalf("Enrich error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 enriched live entries, got %+v", got)
	}
	if !got["liveStat111"].IsLive || got["liveStat111"].Duration != "" {
		t.Fatalf("liveStat111 meta = %+v, want IsLive=true Duration=\"\"", got["liveStat111"])
	}
	if !got["liveMic2222"].IsLive {
		t.Fatalf("liveMic2222 meta = %+v, want IsLive=true", got["liveMic2222"])
	}

	rawDisk, err := os.ReadFile(filepath.Join(cacheDir, metaCacheFileName))
	if err != nil {
		t.Fatalf("expected video_meta.json to be persisted: %v", err)
	}
	var disk map[string]cachedVideoMeta
	if err := json.Unmarshal(rawDisk, &disk); err != nil {
		t.Fatalf("unmarshal video_meta.json: %v", err)
	}
	if !disk["liveStat111"].IsLive || !disk["liveMic2222"].IsLive {
		t.Fatalf("expected IsLive persisted in video_meta.json, got %+v", disk)
	}

	// Verify disk cache reload and ApplyCached on a fresh MetadataEnricher.
	reloaded := NewMetadataEnricher(cacheDir)
	reloaded.EndpointURL = srv.URL

	hits.Store(0)
	got2, err := reloaded.Enrich(context.Background(), []string{"liveStat111", "liveMic2222"})
	if err != nil {
		t.Fatalf("reloaded Enrich error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("expected 0 HTTP hits from disk cache, got %d", hits.Load())
	}
	if !got2["liveStat111"].IsLive || !got2["liveMic2222"].IsLive {
		t.Fatalf("unexpected reloaded IsLive meta: %+v", got2)
	}
	if cached, ok := reloaded.Cached("liveStat111"); !ok || !cached.IsLive {
		t.Fatalf("Cached(liveStat111) = (%+v, %v), want IsLive=true", cached, ok)
	}

	videos := []model.Video{
		{ID: "liveStat111", Title: "24/7 Lofi Station", Views: "1K vistas", Published: "2026-01-01", ChannelID: "UC111"},
		{ID: "liveMic2222", Title: "Jazz Radio"},
	}
	reloaded.ApplyCached(videos)
	if !videos[0].IsLive || videos[0].Badge() != "🔴 EN VIVO" {
		t.Fatalf("ApplyCached did not set IsLive for liveStat111: %+v (badge=%q)", videos[0], videos[0].Badge())
	}
	if !videos[1].IsLive || videos[1].Badge() != "🔴 EN VIVO" {
		t.Fatalf("ApplyCached did not set IsLive for liveMic2222: %+v (badge=%q)", videos[1], videos[1].Badge())
	}
}
