package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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

func TestClientFetch_FallbackStripsAllParens(t *testing.T) {
	var queries []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		if strings.Contains(q, "Argentina") {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"trackName":    "De Música Ligera",
				"artistName":   "Soda Stereo",
				"syncedLyrics": "[00:10.00] Ella durmió al calor de las masas",
			},
		})
	}))
	defer srv.Close()

	client := NewClient()
	client.BaseURL = srv.URL

	got, err := client.Fetch(context.Background(), "soda-fallback-1", "Soda Stereo - De Música Ligera (Argentina 1997) | Bonus", "Soda Stereo Oficial")
	if err != nil {
		t.Fatalf("expected fallback query to succeed, got %v (queries=%v)", err, queries)
	}
	if len(got.Synced) != 1 || got.Synced[0].Text != "Ella durmió al calor de las masas" {
		t.Fatalf("unexpected fallback lyrics: %+v", got)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 search calls (primary + fallback), got %d: %v", len(queries), queries)
	}
}

func TestClientFetch_UpgradesUnsyncedAndCoreQuery(t *testing.T) {
	if got := CoreQuery("Bad Bunny - Titi Me Pregunto ft. Someone - Video Oficial", "BadBunnyVEVO"); got != "Bad Bunny - Titi Me Pregunto" {
		t.Fatalf("CoreQuery double hyphen/ft = %q", got)
	}
	if got := CoreQuery("Song Title (Audio)", "ArtistVEVO"); got != "Artist Song Title" {
		t.Fatalf("CoreQuery VEVO = %q", got)
	}

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(q, "Argentina") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"trackName":   "De Música Ligera",
					"artistName":  "Soda Stereo",
					"plainLyrics": "Ella durmió",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"trackName":    "De Música Ligera",
				"artistName":   "Soda Stereo",
				"syncedLyrics": "[00:05.00] Ella durmió",
			},
		})
	}))
	defer srv.Close()

	client := NewClient()
	client.BaseURL = srv.URL

	got, err := client.Fetch(context.Background(), "upgrade-1", "Soda Stereo - De Música Ligera (Argentina 1997)", "Soda Stereo")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if len(got.Synced) != 1 || got.Synced[0].Text != "Ella durmió" {
		t.Fatalf("expected upgrade to synced lyrics via FallbackQuery, got %+v", got)
	}
	if calls != 2 {
		t.Fatalf("expected 2 search calls (unsynced primary -> synced fallback), got %d", calls)
	}
}

func TestNewClient_BackwardCompatible(t *testing.T) {
	c := NewClient()
	if c.YouTubeEndpointURL != "" {
		t.Fatalf("NewClient().YouTubeEndpointURL = %q, want empty", c.YouTubeEndpointURL)
	}
	if c.CacheDir != "" {
		t.Fatalf("NewClient().CacheDir = %q, want empty", c.CacheDir)
	}
}

func TestSelectBestLyricsForDuration(t *testing.T) {
	items := []lrclibSearchItem{
		{
			TrackName:    "Song Unknown Dur",
			ArtistName:   "Artist",
			Duration:     0,
			SyncedLyrics: "[00:01.00] Unknown dur",
		},
		{
			TrackName:    "Song Radio Edit",
			ArtistName:   "Artist",
			Duration:     180,
			SyncedLyrics: "[00:01.00] Radio edit",
		},
		{
			TrackName:    "Song Live Version",
			ArtistName:   "Artist",
			Duration:     245,
			SyncedLyrics: "[00:01.00] Live version",
		},
		{
			TrackName:    "Song Tie Second",
			ArtistName:   "Artist",
			Duration:     245,
			SyncedLyrics: "[00:01.00] Tie second",
		},
	}

	// targetDuration <= 0 preserves first-match behavior while recording Duration.
	gotZero, ok := selectBestLyricsForDuration(items, 0)
	if !ok || gotZero.TrackName != "Song Unknown Dur" || gotZero.Duration != 0 {
		t.Fatalf("targetDuration=0 got %+v (ok=%v), want Song Unknown Dur", gotZero, ok)
	}

	// targetDuration > 0 prefers Duration > 0 over Duration == 0 and picks closest, tie-breaking earlier.
	got245, ok := selectBestLyricsForDuration(items, 244)
	if !ok || got245.TrackName != "Song Live Version" || got245.Duration != 245 {
		t.Fatalf("targetDuration=244 got %+v (ok=%v), want Song Live Version", got245, ok)
	}

	// Plain fallback also ranks by duration.
	plainItems := []lrclibSearchItem{
		{TrackName: "Plain Unknown", Duration: 0, PlainLyrics: "line 1"},
		{TrackName: "Plain Far", Duration: 400, PlainLyrics: "line 2"},
		{TrackName: "Plain Close", Duration: 205, PlainLyrics: "line 3"},
	}
	gotPlain, ok := selectBestLyricsForDuration(plainItems, 200)
	if !ok || gotPlain.TrackName != "Plain Close" || gotPlain.Duration != 205 {
		t.Fatalf("plain targetDuration=200 got %+v (ok=%v), want Plain Close", gotPlain, ok)
	}
}

func TestClientFetchWithOptions_DurationAndSourceRanking(t *testing.T) {
	t.Run("long-form >600s prefers YouTube captions over close LRCLib", func(t *testing.T) {
		lrcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"trackName":    "Long Mix",
					"artistName":   "DJ",
					"duration":     900,
					"syncedLyrics": "[00:05.00] LRCLib line",
				},
			})
		}))
		defer lrcSrv.Close()

		var ytSrv *httptest.Server
		ytSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/player" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{"title": "Long Mix", "author": "DJ"},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{"baseUrl": ytSrv.URL + "/tt", "languageCode": "es", "kind": "asr"},
							},
						},
					},
				})
				return
			}
			_, _ = w.Write([]byte(`{"events":[{"tStartMs":1000,"segs":[{"utf8":"YT long-form line"}]}]}`))
		}))
		defer ytSrv.Close()

		c := NewClient()
		c.BaseURL = lrcSrv.URL
		c.YouTubeEndpointURL = ytSrv.URL + "/player"

		got, err := c.FetchWithOptions(context.Background(), "longvid1234", "Long Mix", "DJ", FetchOptions{
			Duration: 902,
		})
		if err != nil {
			t.Fatalf("FetchWithOptions error: %v", err)
		}
		if got.Source != SourceYouTubeASR || !got.HasAlternate || got.AltSource != SourceLRCLib {
			t.Fatalf("expected YT winner with LRCLib alternate, got %+v", got)
		}
	})

	t.Run("short track prefers LRCLib within 6s and toggles PreferSource cleanly", func(t *testing.T) {
		var lrcCalls, ytCalls atomic.Int32
		lrcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lrcCalls.Add(1)
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"trackName":    "Radio Track",
					"artistName":   "Band",
					"duration":     210,
					"syncedLyrics": "[00:05.00] LRCLib synced",
				},
			})
		}))
		defer lrcSrv.Close()

		var ytSrv *httptest.Server
		ytSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/player" {
				ytCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{"title": "Radio Track", "author": "Band"},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{"baseUrl": ytSrv.URL + "/tt", "languageCode": "es", "kind": ""},
							},
						},
					},
				})
				return
			}
			_, _ = w.Write([]byte(`{"events":[{"tStartMs":1000,"segs":[{"utf8":"YT manual CC"}]}]}`))
		}))
		defer ytSrv.Close()

		c := NewClient()
		c.BaseURL = lrcSrv.URL
		c.YouTubeEndpointURL = ytSrv.URL + "/player"

		got, err := c.FetchWithOptions(context.Background(), "shortvid123", "Radio Track", "Band", FetchOptions{
			Duration: 213,
		})
		if err != nil {
			t.Fatalf("FetchWithOptions error: %v", err)
		}
		if got.Source != SourceLRCLib || !got.HasAlternate || got.AltSource != SourceYouTube {
			t.Fatalf("expected LRCLib winner with YouTube alternate, got %+v", got)
		}

		// Toggle to YouTube using cached entries.
		gotYT, err := c.FetchWithOptions(context.Background(), "shortvid123", "Radio Track", "Band", FetchOptions{
			Duration:     213,
			PreferSource: SourceYouTube,
		})
		if err != nil {
			t.Fatalf("PreferSource=YouTube error: %v", err)
		}
		if gotYT.Source != SourceYouTubeCC || !gotYT.HasAlternate || gotYT.AltSource != SourceLRCLib {
			t.Fatalf("expected YouTubeCC with LRCLib alternate, got %+v", gotYT)
		}

		// Toggle back to LRCLib using cached entries.
		gotLRC, err := c.FetchWithOptions(context.Background(), "shortvid123", "Radio Track", "Band", FetchOptions{
			Duration:     213,
			PreferSource: SourceLRCLib,
		})
		if err != nil {
			t.Fatalf("PreferSource=LRCLib error: %v", err)
		}
		if gotLRC.Source != SourceLRCLib || !gotLRC.HasAlternate || gotLRC.AltSource != SourceYouTube {
			t.Fatalf("expected LRCLib with YouTube alternate, got %+v", gotLRC)
		}
		if lrcCalls.Load() != 1 || ytCalls.Load() != 1 {
			t.Fatalf("expected separate caches to prevent extra calls (lrc=%d, yt=%d)", lrcCalls.Load(), ytCalls.Load())
		}
	})

	t.Run("mid duration diff 6s-25s yields to manual CC but beats ASR", func(t *testing.T) {
		lrcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"trackName":    "Mid Diff",
					"artistName":   "Artist",
					"duration":     215,
					"syncedLyrics": "[00:05.00] LRCLib synced",
				},
			})
		}))
		defer lrcSrv.Close()

		var kind atomic.Value
		kind.Store("")
		var ytSrv *httptest.Server
		ytSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/player" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{"title": "Mid Diff", "author": "Artist"},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{"baseUrl": ytSrv.URL + "/tt", "languageCode": "es", "kind": kind.Load().(string)},
							},
						},
					},
				})
				return
			}
			_, _ = w.Write([]byte(`{"events":[{"tStartMs":1000,"segs":[{"utf8":"YT line"}]}]}`))
		}))
		defer ytSrv.Close()

		// With manual CC and diff=15s (<=25, >6), YT manual wins.
		c1 := NewClient()
		c1.BaseURL = lrcSrv.URL
		c1.YouTubeEndpointURL = ytSrv.URL + "/player"
		got1, err := c1.FetchWithOptions(context.Background(), "midvid00001", "Mid Diff", "Artist", FetchOptions{Duration: 200})
		if err != nil {
			t.Fatalf("got1 err: %v", err)
		}
		if got1.Source != SourceYouTubeCC || !got1.HasAlternate || got1.AltSource != SourceLRCLib {
			t.Fatalf("expected YT manual CC to beat 15s-diff LRCLib, got %+v", got1)
		}

		// With ASR and diff=15s (<=25), LRCLib wins.
		kind.Store("asr")
		c2 := NewClient()
		c2.BaseURL = lrcSrv.URL
		c2.YouTubeEndpointURL = ytSrv.URL + "/player"
		got2, err := c2.FetchWithOptions(context.Background(), "midvid00002", "Mid Diff", "Artist", FetchOptions{Duration: 200})
		if err != nil {
			t.Fatalf("got2 err: %v", err)
		}
		if got2.Source != SourceLRCLib || !got2.HasAlternate || got2.AltSource != SourceYouTube {
			t.Fatalf("expected 15s-diff LRCLib to beat YT ASR, got %+v", got2)
		}
	})

	t.Run("missing LRCLib falls back to YouTube ASR", func(t *testing.T) {
		lrcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer lrcSrv.Close()

		var ytSrv *httptest.Server
		ytSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/player" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{"title": "Talk", "author": "Channel"},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{"baseUrl": ytSrv.URL + "/tt", "languageCode": "es", "kind": "asr"},
							},
						},
					},
				})
				return
			}
			_, _ = w.Write([]byte(`{"events":[{"tStartMs":500,"segs":[{"utf8":"Fallback ASR"}]}]}`))
		}))
		defer ytSrv.Close()

		c := NewClient()
		c.BaseURL = lrcSrv.URL
		c.YouTubeEndpointURL = ytSrv.URL + "/player"

		got, err := c.FetchWithOptions(context.Background(), "asrvideon01", "Talk", "Channel", FetchOptions{Duration: 180})
		if err != nil {
			t.Fatalf("FetchWithOptions error: %v", err)
		}
		if got.Source != SourceYouTubeASR || got.HasAlternate || got.AltSource != "" {
			t.Fatalf("expected YT ASR without alternate, got %+v", got)
		}
	})

	t.Run("separates ytCache by normalized language and forwards HL to InnerTube player", func(t *testing.T) {
		lrcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer lrcSrv.Close()

		var mu sync.Mutex
		var seenHL []string
		var ytSrv *httptest.Server
		ytSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/player":
				var req ytPlayerRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode player req: %v", err)
				}
				mu.Lock()
				seenHL = append(seenHL, req.Context.Client.HL)
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{"title": "Bilingual", "author": "Artist"},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{"baseUrl": ytSrv.URL + "/tt-es", "languageCode": "es", "kind": ""},
								{"baseUrl": ytSrv.URL + "/tt-en", "languageCode": "en", "kind": ""},
							},
						},
					},
				})
			case "/tt-es":
				_, _ = w.Write([]byte(`{"events":[{"tStartMs":1000,"segs":[{"utf8":"Línea en español"}]}]}`))
			case "/tt-en":
				_, _ = w.Write([]byte(`{"events":[{"tStartMs":1000,"segs":[{"utf8":"English line"}]}]}`))
			}
		}))
		defer ytSrv.Close()

		c := NewClient()
		c.BaseURL = lrcSrv.URL
		c.YouTubeEndpointURL = ytSrv.URL + "/player"

		gotES, err := c.FetchWithOptions(context.Background(), "biling12345", "Bilingual", "Artist", FetchOptions{
			PreferSource: SourceYouTube,
			Lang:         "es",
		})
		if err != nil {
			t.Fatalf("FetchWithOptions(es) error: %v", err)
		}
		if gotES.Language != "es" || len(gotES.Synced) != 1 || gotES.Synced[0].Text != "Línea en español" {
			t.Fatalf("unexpected ES captions: %+v", gotES)
		}

		gotEN, err := c.FetchWithOptions(context.Background(), "biling12345", "Bilingual", "Artist", FetchOptions{
			PreferSource: SourceYouTube,
			Lang:         "en",
		})
		if err != nil {
			t.Fatalf("FetchWithOptions(en) error: %v", err)
		}
		if gotEN.Language != "en" || len(gotEN.Synced) != 1 || gotEN.Synced[0].Text != "English line" {
			t.Fatalf("expected EN captions instead of stale ES cache hit, got %+v", gotEN)
		}

		mu.Lock()
		gotHLs := append([]string(nil), seenHL...)
		mu.Unlock()
		if !slices.Equal(gotHLs, []string{"es", "en"}) {
			t.Fatalf("InnerTube HL sequence = %v, want [es en]", gotHLs)
		}
	})
}
