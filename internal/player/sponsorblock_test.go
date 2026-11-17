package player

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewSponsorBlockClient_Defaults(t *testing.T) {
	c := NewSponsorBlockClient()
	if c == nil {
		t.Fatal("NewSponsorBlockClient returned nil")
	}
	if c.BaseURL != "https://sponsor.ajay.app" {
		t.Fatalf("BaseURL = %q, want %q", c.BaseURL, "https://sponsor.ajay.app")
	}
	if c.HTTPClient == nil || c.HTTPClient.Timeout != 3*time.Second {
		t.Fatalf("HTTPClient = %+v, want non-nil with 3s timeout", c.HTTPClient)
	}
}

func TestSponsorBlockClient_FetchSegments_ValidAndCached(t *testing.T) {
	var hits atomic.Int32
	var gotRawQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/skipSegments" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"segment":[12.5,45.25],"category":"sponsor","UUID":"uuid-sponsor-1"},
			{"segment":[90.0,110.0],"category":"selfpromo","UUID":"uuid-self-2"},
			{"segment":[50.0],"category":"sponsor","UUID":"malformed-short"},
			{"segment":[30.0,20.0],"category":"sponsor","UUID":"malformed-inverted"}
		]`))
	}))
	defer srv.Close()

	client := NewSponsorBlockClient()
	client.BaseURL = srv.URL + "/"

	segs, err := client.FetchSegments(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("FetchSegments() unexpected error: %v", err)
	}

	wantQuery := "videoID=dQw4w9WgXcQ&categories=%5B%22sponsor%22%2C%22selfpromo%22%5D"
	if gotRawQuery != wantQuery {
		t.Fatalf("RawQuery = %q, want %q", gotRawQuery, wantQuery)
	}

	want := []Segment{
		{Start: 12.5, End: 45.25, Category: "sponsor", UUID: "uuid-sponsor-1"},
		{Start: 90.0, End: 110.0, Category: "selfpromo", UUID: "uuid-self-2"},
	}
	if !reflect.DeepEqual(segs, want) {
		t.Fatalf("FetchSegments() = %+v, want %+v", segs, want)
	}

	// Mutate caller slice and verify cache returns an uncorrupted copy without re-hitting server.
	segs[0].Start = 999
	segs2, err := client.FetchSegments(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("second FetchSegments() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(segs2, want) {
		t.Fatalf("cached FetchSegments() = %+v, want %+v", segs2, want)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server hit count = %d, want 1", got)
	}
}

func TestSponsorBlockClient_FetchSegments_404Cached(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewSponsorBlockClient()
	client.BaseURL = srv.URL

	segs, err := client.FetchSegments(context.Background(), "noSegVideo1")
	if err != nil {
		t.Fatalf("404 FetchSegments() unexpected error: %v", err)
	}
	if segs != nil {
		t.Fatalf("404 FetchSegments() = %+v, want nil", segs)
	}

	// Verify empty slice is cached under key so a second call never hits the server.
	client.mu.RLock()
	cached, ok := client.cache["noSegVideo1"]
	client.mu.RUnlock()
	if !ok || cached == nil || len(cached) != 0 {
		t.Fatalf("cache entry = (%#v, %v), want cached non-nil empty slice", cached, ok)
	}

	segs2, err := client.FetchSegments(context.Background(), "noSegVideo1")
	if err != nil {
		t.Fatalf("cached 404 FetchSegments() unexpected error: %v", err)
	}
	if segs2 != nil {
		t.Fatalf("cached 404 FetchSegments() = %+v, want nil", segs2)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server hit count after 404 = %d, want 1", got)
	}
}

func TestSponsorBlockClient_FetchSegments_ValidationAndErrors(t *testing.T) {
	var hits atomic.Int32
	var returnStatus atomic.Int32
	returnStatus.Store(http.StatusInternalServerError)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		status := int(returnStatus.Load())
		if status != http.StatusOK {
			http.Error(w, "upstream error", status)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"segment": []float64{5, 10}, "category": "sponsor", "UUID": "recovered-1"},
		})
	}))
	defer srv.Close()

	client := NewSponsorBlockClient()
	client.BaseURL = srv.URL

	if _, err := client.FetchSegments(context.Background(), "   "); err == nil {
		t.Fatal("expected error for empty videoID, got nil")
	}
	if hits.Load() != 0 {
		t.Fatalf("expected 0 server hits for empty videoID, got %d", hits.Load())
	}

	// 500 should return error and NOT be cached.
	if _, err := client.FetchSegments(context.Background(), "errVid12345"); err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
	returnStatus.Store(http.StatusOK)
	segs, err := client.FetchSegments(context.Background(), "errVid12345")
	if err != nil {
		t.Fatalf("retry after 500 unexpected error: %v", err)
	}
	if len(segs) != 1 || segs[0].UUID != "recovered-1" {
		t.Fatalf("unexpected retry segments: %+v", segs)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected 2 server hits (initial 500 + retry 200), got %d", hits.Load())
	}

	// Invalid JSON response must fail without caching.
	badJSONSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer badJSONSrv.Close()

	badClient := &SponsorBlockClient{BaseURL: badJSONSrv.URL}
	if _, err := badClient.FetchSegments(context.Background(), "badJsonVid1"); err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}

	// Network failure on closed server must return error.
	closedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedSrv.URL
	closedSrv.Close()

	netClient := NewSponsorBlockClient()
	netClient.BaseURL = closedURL
	if _, err := netClient.FetchSegments(context.Background(), "netFailVid1"); err == nil {
		t.Fatal("expected error on closed server network failure, got nil")
	}
}

func TestSponsorBlockClient_ConcurrentRaceSafety(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vid := r.URL.Query().Get("videoID")
		switch vid {
		case "miss404vid1":
			http.NotFound(w, r)
		case "err500vid01":
			http.Error(w, "fail", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `[{"segment":[1.0,5.0],"category":"sponsor","UUID":"uuid-%s"}]`, vid)
		}
	}))
	defer srv.Close()

	client := NewSponsorBlockClient()
	client.BaseURL = srv.URL

	videoIDs := []string{
		"vidAlpha001",
		"vidBravo002",
		"miss404vid1",
		"err500vid01",
		"vidCharlie3",
	}

	const workers = 16
	const iterations = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterations)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				vid := videoIDs[(worker+i)%len(videoIDs)]
				segs, err := client.FetchSegments(context.Background(), vid)
				switch vid {
				case "err500vid01":
					if err == nil {
						errCh <- fmt.Errorf("expected error for %s, got nil", vid)
					}
				case "miss404vid1":
					if err != nil || len(segs) != 0 {
						errCh <- fmt.Errorf("404 video %s got (%+v, %v)", vid, segs, err)
					}
				default:
					if err != nil || len(segs) != 1 || segs[0].UUID != "uuid-"+vid {
						errCh <- fmt.Errorf("valid video %s got (%+v, %v)", vid, segs, err)
					}
				}
			}
		}(w)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}
