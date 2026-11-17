package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCleanQuery(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		channel string
		want    string
	}{
		{
			name:    "strips common English and Spanish YouTube tags",
			title:   "De Música Ligera (Official Video) [Official Audio] (Lyrics) [HD] [4K] (En Vivo) (Audio Oficial)",
			channel: "Soda Stereo",
			want:    "Soda Stereo De Música Ligera",
		},
		{
			name:    "strips Official Music Video and avoids duplicating artist already in title",
			title:   "Pink Floyd - Comfortably Numb (Official Music Video) [HD]",
			channel: "Pink Floyd",
			want:    "Pink Floyd - Comfortably Numb",
		},
		{
			name:    "strips Topic and Oficial suffixes from channel",
			title:   "Time [2011 Remastered] (Official Audio)",
			channel: "Pink Floyd - Topic",
			want:    "Pink Floyd Time",
		},
		{
			name:    "strips standalone ft. and feat. and parenthesized feat",
			title:   "Daft Punk - Get Lucky ft. Pharrell Williams (feat. Nile Rodgers) feat. Guest",
			channel: "Daft Punk Official",
			want:    "Daft Punk - Get Lucky Pharrell Williams Guest",
		},
		{
			name:    "does not mangle words containing ft or Unofficial channel",
			title:   "Left Behind (1999)",
			channel: "Unofficial",
			want:    "Unofficial Left Behind (1999)",
		},
		{
			name:    "preserves meaningful parenthesized title part",
			title:   "(Don't Fear) The Reaper [Lyrics]",
			channel: "Blue Oyster Cult",
			want:    "Blue Oyster Cult (Don't Fear) The Reaper",
		},
		{
			name:    "ignores YouTube placeholder channel and collapses spacing",
			title:   "   Jeremías 17:5   (Video Oficial)   ",
			channel: "YouTube",
			want:    "Jeremías 17:5",
		},
		{
			name:    "does not prepend random uploader channel when title already has Artist - Track",
			title:   "Queen - Bohemian Rhapsody (Lyrics)",
			channel: "Classic Rock Lyrics Hub",
			want:    "Queen - Bohemian Rhapsody",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanQuery(tc.title, tc.channel)
			if got != tc.want {
				t.Fatalf("CleanQuery(%q, %q) = %q, want %q", tc.title, tc.channel, got, tc.want)
			}
		})
	}
}

func TestParseLRC(t *testing.T) {
	raw := "\r\n" +
		"[ti:Time]\r\n" +
		"[ar:Pink Floyd]\r\n" +
		"[al:The Dark Side of the Moon]\r\n" +
		"[offset:+250]\r\n" +
		"\r\n" +
		"[01:05.500] Shorter of breath\r\n" +
		"[0:09] Ticking away\r\n" +
		"[00:25.75][02:10.25] Chorus line\r\n" +
		"[00:30.00]   \r\n" +
		"   [00:12.34] The moments that make up a dull day\r\n"

	got := ParseLRC(raw)
	want := []Line{
		{Time: 9.0, Text: "Ticking away"},
		{Time: 12.34, Text: "The moments that make up a dull day"},
		{Time: 25.75, Text: "Chorus line"},
		{Time: 65.5, Text: "Shorter of breath"},
		{Time: 130.25, Text: "Chorus line"},
	}

	if len(got) != len(want) {
		t.Fatalf("ParseLRC len = %d, want %d; got=%+v", len(got), len(want), got)
	}
	for i := range want {
		if math.Abs(got[i].Time-want[i].Time) > 1e-9 || got[i].Text != want[i].Text {
			t.Errorf("line[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	if empty := ParseLRC("[ti:Only Metadata]\n[ar:Nobody]\n\n"); empty != nil {
		t.Errorf("expected nil for metadata-only LRC, got %+v", empty)
	}
}

func TestActiveLineIndex(t *testing.T) {
	lines := []Line{
		{Time: 5.0, Text: "Line 0"},
		{Time: 10.0, Text: "Line 1"},
		{Time: 10.0, Text: "Line 2"},
		{Time: 20.5, Text: "Line 3"},
	}

	tests := []struct {
		name string
		in   []Line
		pos  float64
		want int
	}{
		{name: "empty lines", in: nil, pos: 10.0, want: -1},
		{name: "before first timestamp", in: lines, pos: 4.99, want: -1},
		{name: "negative position", in: lines, pos: -1.0, want: -1},
		{name: "NaN position", in: lines, pos: math.NaN(), want: -1},
		{name: "exact first timestamp", in: lines, pos: 5.0, want: 0},
		{name: "between line 0 and line 1", in: lines, pos: 7.5, want: 0},
		{name: "exact duplicate timestamp selects last matching", in: lines, pos: 10.0, want: 2},
		{name: "between line 2 and line 3", in: lines, pos: 19.0, want: 2},
		{name: "exact last timestamp", in: lines, pos: 20.5, want: 3},
		{name: "after last timestamp", in: lines, pos: 120.0, want: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ActiveLineIndex(tc.in, tc.pos)
			if got != tc.want {
				t.Fatalf("ActiveLineIndex(%v) = %d, want %d", tc.pos, got, tc.want)
			}
		})
	}
}

func TestClientFetch_SyncedAndPlainAndCache(t *testing.T) {
	t.Run("prefers first result with valid syncedLyrics and caches by videoID", func(t *testing.T) {
		var calls atomic.Int32
		var gotQuery, gotUA string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotQuery = r.URL.Query().Get("q")
			gotUA = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"trackName":    "Plain Only",
					"artistName":   "Artist A",
					"syncedLyrics": "[ti:Metadata Only]",
					"plainLyrics":  "Only plain line",
				},
				{
					"trackName":    "Time",
					"artistName":   "Pink Floyd",
					"syncedLyrics": "[00:10.00] Second\n[00:05.00] First",
					"plainLyrics":  "First\n\nSecond\n",
				},
			})
		}))
		defer srv.Close()

		client := NewClient()
		client.BaseURL = srv.URL

		ctx := context.Background()
		got, err := client.Fetch(ctx, "vid-time", "Time (Official Audio)", "Pink Floyd - Topic")
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		if gotQuery != "Pink Floyd Time" {
			t.Errorf("query = %q, want %q", gotQuery, "Pink Floyd Time")
		}
		if gotUA != "iutui/0.4" {
			t.Errorf("User-Agent = %q, want %q", gotUA, "iutui/0.4")
		}
		if got.Source != "lrclib" || got.TrackName != "Time" || got.ArtistName != "Pink Floyd" {
			t.Errorf("unexpected metadata: %+v", got)
		}
		if len(got.Synced) != 2 || got.Synced[0].Text != "First" || got.Synced[1].Text != "Second" {
			t.Errorf("unexpected synced lines: %+v", got.Synced)
		}

		// Mutating returned slice must not corrupt cache.
		got.Synced[0].Text = "MUTATED"

		got2, err := client.Fetch(ctx, "vid-time", "Different Title", "Different Channel")
		if err != nil {
			t.Fatalf("second Fetch error: %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("expected 1 HTTP call due to videoID cache, got %d", calls.Load())
		}
		if got2.Synced[0].Text != "First" {
			t.Fatalf("cached entry mutated: %+v", got2.Synced)
		}
	})

	t.Run("falls back to plainLyrics split by non-empty lines and caches by cleaned query", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"name":         "De Música Ligera",
					"artistName":   "Soda Stereo",
					"syncedLyrics": "",
					"plainLyrics":  "Ella durmió\r\n\r\n   Al calor de las masas   \n",
				},
			})
		}))
		defer srv.Close()

		client := NewClient()
		client.BaseURL = srv.URL

		got, err := client.Fetch(context.Background(), "", "De Música Ligera (En Vivo)", "Soda Stereo")
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		if len(got.Synced) != 0 {
			t.Errorf("expected no synced lines, got %+v", got.Synced)
		}
		wantPlain := []string{"Ella durmió", "Al calor de las masas"}
		if !slices.Equal(got.Plain, wantPlain) || got.Source != "lrclib" || got.TrackName != "De Música Ligera" {
			t.Fatalf("unexpected plain result: %+v", got)
		}

		// Different noise, same cleaned query -> cache hit.
		_, err = client.Fetch(context.Background(), "", "de música ligera [HD]", "Soda Stereo Oficial")
		if err != nil {
			t.Fatalf("cached Fetch error: %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("expected 1 HTTP call due to cleaned query cache, got %d", calls.Load())
		}
	})

	t.Run("caches 404 and empty search misses as ErrNotFound", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := calls.Add(1)
			if n == 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode([]any{})
		}))
		defer srv.Close()

		client := NewClient()
		client.BaseURL = srv.URL

		if _, err := client.Fetch(context.Background(), "miss-404", "Unknown Track", "Nobody"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for 404, got %v", err)
		}
		if _, err := client.Fetch(context.Background(), "miss-404", "Unknown Track", "Nobody"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected cached ErrNotFound for 404, got %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("expected 404 miss to be cached (1 call), got %d", calls.Load())
		}

		if _, err := client.Fetch(context.Background(), "miss-empty", "Empty Track", "Nobody"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for empty array, got %v", err)
		}
		if _, err := client.Fetch(context.Background(), "miss-empty", "Empty Track", "Nobody"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected cached ErrNotFound for empty array, got %v", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("expected empty miss to be cached (2 total calls), got %d", calls.Load())
		}
	})

	t.Run("does not cache transient 500 error", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"trackName":    "Recovered",
					"artistName":   "Artist",
					"syncedLyrics": "[00:01.00] Back online",
				},
			})
		}))
		defer srv.Close()

		client := NewClient()
		client.BaseURL = srv.URL

		if _, err := client.Fetch(context.Background(), "retry-1", "Recovered", "Artist"); err == nil {
			t.Fatal("expected error on HTTP 500, got nil")
		}
		got, err := client.Fetch(context.Background(), "retry-1", "Recovered", "Artist")
		if err != nil {
			t.Fatalf("expected retry to succeed, got %v", err)
		}
		if len(got.Synced) != 1 || got.Synced[0].Text != "Back online" {
			t.Fatalf("unexpected retry lyrics: %+v", got)
		}
		if calls.Load() != 2 {
			t.Fatalf("expected 2 calls after transient error, got %d", calls.Load())
		}
	})
}

func TestClientFetch_Concurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"trackName":    "Concurrent Track",
				"artistName":   "Tester",
				"syncedLyrics": "[00:02.00] Line B\n[00:01.00] Line A",
			},
		})
	}))
	defer srv.Close()

	client := NewClient()
	client.BaseURL = srv.URL

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vid := "shared-id"
			if idx%3 == 0 {
				vid = ""
			}
			lyr, err := client.Fetch(context.Background(), vid, "Concurrent Track (Official Video)", "Tester")
			if err != nil {
				errs <- err
				return
			}
			if len(lyr.Synced) != 2 || lyr.Synced[0].Text != "Line A" {
				errs <- errors.New("unexpected lyrics payload")
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Fetch failed: %v", err)
	}
}
