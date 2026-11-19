package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuggest_ParsesYouTubeJSONAndEscapesQuery(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
	}{
		{
			name:     "query suffix",
			basePath: "/complete/search?client=firefox&ds=yt&q=",
		},
		{
			name:     "query template placeholder",
			basePath: "/complete/search?client=firefox&ds=yt&q={query}",
		},
		{
			name:     "bare server URL",
			basePath: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotMethod    string
				gotUserAgent string
				gotRawQuery  string
				gotQ         string
				hits         atomic.Int32
			)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				gotMethod = r.Method
				gotUserAgent = r.Header.Get("User-Agent")
				gotRawQuery = r.URL.RawQuery
				gotQ = r.URL.Query().Get("q")

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[
					"soda stereo en vivo",
					[
						"soda stereo en vivo 1997",
						"soda stereo en vivo obras",
						"soda stereo en vivo river"
					],
					[],
					{"google:suggestsubtypes": [[512], [512], [512]]}
				]`))
			}))
			defer srv.Close()

			client := NewSuggestClient()
			client.BaseURL = srv.URL + tc.basePath
			client.HTTPClient = srv.Client()

			got, err := client.Suggest(context.Background(), "soda stereo en vivo")
			if err != nil {
				t.Fatalf("Suggest() unexpected error: %v", err)
			}

			want := []string{
				"soda stereo en vivo 1997",
				"soda stereo en vivo obras",
				"soda stereo en vivo river",
			}
			if !slices.Equal(got, want) {
				t.Fatalf("Suggest() = %v, want %v", got, want)
			}
			if hits.Load() != 1 {
				t.Fatalf("HTTP hits = %d, want 1", hits.Load())
			}
			if gotMethod != http.MethodGet {
				t.Fatalf("HTTP method = %q, want GET", gotMethod)
			}
			if gotUserAgent != "Mozilla/5.0" {
				t.Fatalf("User-Agent = %q, want %q", gotUserAgent, "Mozilla/5.0")
			}
			if gotQ != "soda stereo en vivo" {
				t.Fatalf("decoded query param q = %q, want %q", gotQ, "soda stereo en vivo")
			}
			if !strings.Contains(gotRawQuery, "q=soda+stereo+en+vivo") {
				t.Fatalf("RawQuery = %q, expected escaped q=soda+stereo+en+vivo", gotRawQuery)
			}
		})
	}
}

func TestSuggest_TrimDedupeSkipExactAndCapSix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := []any{
			"soda stereo",
			[]any{
				"",
				"   ",
				42,
				nil,
				"soda stereo",
				"  SODA STEREO ",
				"  soda stereo en vivo  ",
				"Soda Stereo En Vivo",
				"soda stereo de musica ligera",
				"soda stereo persiana americana",
				"soda stereo ciudad de la furia",
				"soda stereo cuando pase el temblor",
				"soda stereo profugos",
				"soda stereo signitos",
				"soda stereo zoom",
			},
			[]any{},
			map[string]any{"google:suggestsubtypes": [][]int{{512}}},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := &SuggestClient{
		BaseURL:    srv.URL + "?q=",
		HTTPClient: srv.Client(),
	}

	got, err := client.Suggest(context.Background(), "  soda stereo  ")
	if err != nil {
		t.Fatalf("Suggest() unexpected error: %v", err)
	}

	want := []string{
		"soda stereo en vivo",
		"soda stereo de musica ligera",
		"soda stereo persiana americana",
		"soda stereo ciudad de la furia",
		"soda stereo cuando pase el temblor",
		"soda stereo profugos",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Suggest() = %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
}

func TestSuggest_EmptyQueryShortCircuit(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewSuggestClient()
	client.BaseURL = srv.URL + "?q="
	client.HTTPClient = srv.Client()

	for _, q := range []string{"", "   ", "\t\n "} {
		got, err := client.Suggest(context.Background(), q)
		if err != nil {
			t.Fatalf("Suggest(%q) unexpected error: %v", q, err)
		}
		if got != nil {
			t.Fatalf("Suggest(%q) = %v, want nil", q, got)
		}
	}

	if hits.Load() != 0 {
		t.Fatalf("empty queries triggered %d HTTP requests, want 0", hits.Load())
	}
}

func TestSuggest_CacheHitAndSliceCloning(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		q := r.URL.Query().Get("q")
		if q == "empty-results" {
			_, _ = w.Write([]byte(`["empty-results", []]`))
			return
		}
		_, _ = w.Write([]byte(`["pink floyd", ["pink floyd echoes", "pink floyd time"]]`))
	}))
	defer srv.Close()

	client := NewSuggestClient()
	client.BaseURL = srv.URL + "?q="
	client.HTTPClient = srv.Client()

	first, err := client.Suggest(context.Background(), "pink floyd")
	if err != nil {
		t.Fatalf("first Suggest() error: %v", err)
	}
	if !slices.Equal(first, []string{"pink floyd echoes", "pink floyd time"}) {
		t.Fatalf("first Suggest() = %v", first)
	}

	// Mutating caller slice must not corrupt cached copy.
	first[0] = "mutated"

	second, err := client.Suggest(context.Background(), "  pink floyd ")
	if err != nil {
		t.Fatalf("second Suggest() error: %v", err)
	}
	if !slices.Equal(second, []string{"pink floyd echoes", "pink floyd time"}) {
		t.Fatalf("second Suggest() = %v, cache slice was mutated", second)
	}
	if hits.Load() != 1 {
		t.Fatalf("HTTP hits = %d, want 1", hits.Load())
	}

	// Empty valid responses should also be cached cleanly.
	empty1, err := client.Suggest(context.Background(), "empty-results")
	if err != nil || empty1 != nil {
		t.Fatalf("empty1 = (%v, %v), want (nil, nil)", empty1, err)
	}
	empty2, err := client.Suggest(context.Background(), "empty-results")
	if err != nil || empty2 != nil {
		t.Fatalf("empty2 = (%v, %v), want (nil, nil)", empty2, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("HTTP hits after cached empty query = %d, want 2", hits.Load())
	}
}

func TestSuggest_BoundedCacheEviction(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		q := r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode([]any{q, []string{q + " live"}})
	}))
	defer srv.Close()

	client := NewSuggestClient()
	client.BaseURL = srv.URL + "?q="
	client.HTTPClient = srv.Client()

	// Fill cache to capacity (64 distinct entries).
	for i := 0; i < maxSuggestCacheSize; i++ {
		q := fmt.Sprintf("query-%02d", i)
		got, err := client.Suggest(context.Background(), q)
		if err != nil {
			t.Fatalf("Suggest(%q) error: %v", q, err)
		}
		if !slices.Equal(got, []string{q + " live"}) {
			t.Fatalf("Suggest(%q) = %v", q, got)
		}
	}
	if client.cacheLen() != maxSuggestCacheSize {
		t.Fatalf("cacheLen() = %d, want %d", client.cacheLen(), maxSuggestCacheSize)
	}
	if hits.Load() != maxSuggestCacheSize {
		t.Fatalf("hits = %d, want %d", hits.Load(), maxSuggestCacheSize)
	}

	// Touch query-00 (cache hit) so query-01 becomes the LRU entry.
	if _, err := client.Suggest(context.Background(), "query-00"); err != nil {
		t.Fatalf("Suggest(query-00) error: %v", err)
	}
	if hits.Load() != maxSuggestCacheSize {
		t.Fatalf("cache hit incremented hits to %d", hits.Load())
	}

	// Insert 65th distinct query -> cache remains bounded at 64, evicting query-01.
	if _, err := client.Suggest(context.Background(), "query-64"); err != nil {
		t.Fatalf("Suggest(query-64) error: %v", err)
	}
	if client.cacheLen() != maxSuggestCacheSize {
		t.Fatalf("cacheLen() after eviction = %d, want %d", client.cacheLen(), maxSuggestCacheSize)
	}
	if hits.Load() != maxSuggestCacheSize+1 {
		t.Fatalf("hits = %d, want %d", hits.Load(), maxSuggestCacheSize+1)
	}

	// query-00 should still be a cache hit (0 HTTP requests).
	if _, err := client.Suggest(context.Background(), "query-00"); err != nil {
		t.Fatalf("Suggest(query-00) recheck error: %v", err)
	}
	if hits.Load() != maxSuggestCacheSize+1 {
		t.Fatalf("query-00 should remain cached, hits = %d", hits.Load())
	}

	// query-01 was evicted, so fetching it must hit the server again.
	if _, err := client.Suggest(context.Background(), "query-01"); err != nil {
		t.Fatalf("Suggest(query-01) after eviction error: %v", err)
	}
	if hits.Load() != maxSuggestCacheSize+2 {
		t.Fatalf("evicted query-01 did not re-fetch, hits = %d", hits.Load())
	}
	if client.cacheLen() != maxSuggestCacheSize {
		t.Fatalf("cacheLen() exceeded limit: %d", client.cacheLen())
	}
}

func TestSuggest_Non200AndInvalidPayloadsNotCached(t *testing.T) {
	var mode atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 0:
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case 1:
			_, _ = w.Write([]byte(`not json`))
		case 2:
			_, _ = w.Write([]byte(`["only-one-item"]`))
		case 3:
			_, _ = w.Write([]byte(`["q", {"not": "an array"}]`))
		default:
			_, _ = w.Write([]byte(`["ok", ["ok result"]]`))
		}
	}))
	defer srv.Close()

	client := NewSuggestClient()
	client.BaseURL = srv.URL + "?q="
	client.HTTPClient = srv.Client()

	for m := int32(0); m <= 3; m++ {
		mode.Store(m)
		if got, err := client.Suggest(context.Background(), "ok"); err == nil || got != nil {
			t.Fatalf("mode %d: Suggest() = (%v, %v), want non-nil error and nil slice", m, got, err)
		}
		if client.cacheLen() != 0 {
			t.Fatalf("mode %d: failed response must not be cached, cacheLen = %d", m, client.cacheLen())
		}
	}

	// Subsequent recovery succeeds cleanly.
	mode.Store(4)
	got, err := client.Suggest(context.Background(), "ok")
	if err != nil {
		t.Fatalf("recovery Suggest() error: %v", err)
	}
	if !slices.Equal(got, []string{"ok result"}) {
		t.Fatalf("recovery Suggest() = %v, want [ok result]", got)
	}
}

func TestSuggest_ContextCancellationAndNilSafety(t *testing.T) {
	var hits atomic.Int32
	unblock := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	}))
	defer srv.Close()
	defer close(unblock)

	client := NewSuggestClient()
	client.BaseURL = srv.URL + "?q="
	client.HTTPClient = srv.Client()

	// Pre-canceled context makes zero HTTP calls.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Suggest(canceledCtx, "gustavo cerati"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled Suggest() err = %v, want context.Canceled", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("pre-canceled context made %d HTTP calls, want 0", hits.Load())
	}

	// In-flight request aborts on deadline exceeded.
	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelTimeout()
	if _, err := client.Suggest(timeoutCtx, "gustavo cerati"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out Suggest() err = %v, want context.DeadlineExceeded", err)
	}

	// Nil receiver safely returns nil, nil.
	var nilClient *SuggestClient
	if got, err := nilClient.Suggest(context.Background(), "cerati"); err != nil || got != nil {
		t.Fatalf("nil SuggestClient returned (%v, %v), want (nil, nil)", got, err)
	}
}

func TestSuggest_ConcurrentAccessRaceFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode([]any{q, []string{q + " one", q + " two"}})
	}))
	defer srv.Close()

	client := &SuggestClient{
		BaseURL:    srv.URL + "?q=",
		HTTPClient: srv.Client(),
	}

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				q := fmt.Sprintf("concurrent-%02d", (id+i)%80)
				got, err := client.Suggest(context.Background(), q)
				if err != nil {
					t.Errorf("Suggest(%q) error: %v", q, err)
					return
				}
				if len(got) != 2 {
					t.Errorf("Suggest(%q) len = %d, want 2", q, len(got))
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if client.cacheLen() > maxSuggestCacheSize {
		t.Fatalf("cacheLen() = %d, exceeded max %d", client.cacheLen(), maxSuggestCacheSize)
	}
}

type suggestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f suggestRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSuggest_AppliesTwoSecondDeadlineCap(t *testing.T) {
	checkDeadline := func(t *testing.T, ctx context.Context, query string, maxExpected time.Duration) {
		t.Helper()
		var gotRemaining time.Duration
		var hasDeadline bool

		client := NewSuggestClient()
		client.HTTPClient = &http.Client{
			Transport: suggestRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				var dl time.Time
				dl, hasDeadline = r.Context().Deadline()
				if hasDeadline {
					gotRemaining = time.Until(dl)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`["q", ["q result"]]`)),
					Header:     make(http.Header),
				}, nil
			}),
		}

		if _, err := client.Suggest(ctx, query); err != nil {
			t.Fatalf("Suggest() unexpected error: %v", err)
		}
		if !hasDeadline {
			t.Fatalf("expected request context to have deadline")
		}
		if gotRemaining <= 0 || gotRemaining > maxExpected {
			t.Fatalf("request deadline remaining = %v, want (0, %v]", gotRemaining, maxExpected)
		}
	}

	checkDeadline(t, context.Background(), "no-deadline", defaultSuggestTimeout+50*time.Millisecond)

	longCtx, cancelLong := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLong()
	checkDeadline(t, longCtx, "long-deadline", defaultSuggestTimeout+50*time.Millisecond)

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelShort()
	checkDeadline(t, shortCtx, "short-deadline", 150*time.Millisecond+50*time.Millisecond)
}
