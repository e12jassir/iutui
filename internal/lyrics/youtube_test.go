package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIsValidYouTubeVideoID(t *testing.T) {
	valid := []string{"dQw4w9WgXcQ", "abc_123-XYZ", "01234567890"}
	for _, id := range valid {
		if !isValidYouTubeVideoID(id) {
			t.Errorf("isValidYouTubeVideoID(%q) = false, want true", id)
		}
	}

	invalid := []string{"", "short", "waytoolong1234", "bad!char123", "vid-time"}
	for _, id := range invalid {
		if isValidYouTubeVideoID(id) {
			t.Errorf("isValidYouTubeVideoID(%q) = true, want false", id)
		}
	}
}

func TestParseYouTubeJSON3(t *testing.T) {
	raw := []byte(`{
		"events": [
			{"tStartMs": 5000, "segs": [{"utf8": "Second   line\n"}, {"utf8": "here &amp; now"}]},
			{"tStartMs": 1200, "segs": [{"utf8": "First &#39;line&#39;"}]},
			{"tStartMs": 2000, "segs": [{"utf8": "   \n  "}]},
			{"tStartMs": 6000, "segs": [{"utf8": "Second line here & now"}]},
			{"tStartMs": 8500}
		]
	}`)

	got := ParseYouTubeJSON3(raw)
	want := []Line{
		{Time: 1.2, Text: "First 'line'"},
		{Time: 5.0, Text: "Second line here & now"},
	}

	if len(got) != len(want) {
		t.Fatalf("ParseYouTubeJSON3 len = %d, want %d; got=%+v", len(got), len(want), got)
	}
	for i := range want {
		if math.Abs(got[i].Time-want[i].Time) > 1e-9 || got[i].Text != want[i].Text {
			t.Errorf("line[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	if empty := ParseYouTubeJSON3([]byte(`{"events": []}`)); empty != nil {
		t.Errorf("expected nil for empty events, got %+v", empty)
	}
	if bad := ParseYouTubeJSON3([]byte(`not json`)); bad != nil {
		t.Errorf("expected nil for invalid json, got %+v", bad)
	}
}

func TestParseYouTubeTimedTextXML(t *testing.T) {
	t.Run("timedtext format 3 with nested s and direct p", func(t *testing.T) {
		raw := []byte(`<?xml version="1.0" encoding="utf-8"?>
<timedtext format="3">
	<body>
		<p t="4500" d="2000"><s>Second</s> <s>line &amp; more</s></p>
		<p t="1250" d="1000">Direct   &amp;#39;first&amp;#39;<br/>line</p>
		<p t="5000" d="1000"><s>Second</s> <s>line &amp; more</s></p>
		<p t="7000" d="500">   </p>
		<p d="500">No timestamp</p>
	</body>
</timedtext>`)

		got := ParseYouTubeTimedTextXML(raw)
		want := []Line{
			{Time: 1.25, Text: "Direct 'first' line"},
			{Time: 4.5, Text: "Second line & more"},
		}
		if len(got) != len(want) {
			t.Fatalf("ParseYouTubeTimedTextXML format=3 len = %d, want %d; got=%+v", len(got), len(want), got)
		}
		for i := range want {
			if math.Abs(got[i].Time-want[i].Time) > 1e-9 || got[i].Text != want[i].Text {
				t.Errorf("line[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("legacy transcript text format", func(t *testing.T) {
		raw := []byte(`<?xml version="1.0" encoding="utf-8"?>
<transcript>
	<text start="10.5" dur="2.1">  Hola   mundo  </text>
	<text start="3.25" dur="1.5">Primera &amp; línea</text>
	<text start="11.0" dur="1.0">Hola mundo</text>
	<text start="15.0" dur="1.0"></text>
</transcript>`)

		got := ParseYouTubeTimedTextXML(raw)
		want := []Line{
			{Time: 3.25, Text: "Primera & línea"},
			{Time: 10.5, Text: "Hola mundo"},
		}
		if len(got) != len(want) {
			t.Fatalf("ParseYouTubeTimedTextXML transcript len = %d, want %d; got=%+v", len(got), len(want), got)
		}
		for i := range want {
			if math.Abs(got[i].Time-want[i].Time) > 1e-9 || got[i].Text != want[i].Text {
				t.Errorf("line[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})
}

func TestSelectCaptionTrack(t *testing.T) {
	tracks := []ytCaptionTrack{
		{BaseURL: "https://example.com/asr-es", LanguageCode: "es", Kind: "asr"},
		{BaseURL: "https://example.com/man-fr", LanguageCode: "fr", Kind: ""},
		{BaseURL: "https://example.com/man-en", LanguageCode: "en-US", Kind: ""},
		{BaseURL: "https://example.com/man-es419", LanguageCode: "es-419", Kind: ""},
	}

	// Default prefLang ("es"): manual es-419 (100) beats manual en-US (90) and asr es (60).
	best, ok := selectCaptionTrack(tracks, "")
	if !ok || best.LanguageCode != "es-419" || best.Kind != "" {
		t.Fatalf("selectCaptionTrack default = %+v (ok=%v), want es-419 manual", best, ok)
	}

	// Explicit prefLang ("en"): manual en-US (100) beats manual es-419 (80).
	best, ok = selectCaptionTrack(tracks, "en")
	if !ok || best.LanguageCode != "en-US" {
		t.Fatalf("selectCaptionTrack en = %+v (ok=%v), want en-US", best, ok)
	}

	// Only ASR tracks: preferred lang (60) beats en (50) beats other (40).
	asrOnly := []ytCaptionTrack{
		{BaseURL: "https://example.com/asr-fr", LanguageCode: "fr", Kind: "asr"},
		{BaseURL: "https://example.com/asr-en", LanguageCode: "en", Kind: "asr"},
		{BaseURL: "https://example.com/asr-es", LanguageCode: "es-419", Kind: "asr"},
	}
	best, ok = selectCaptionTrack(asrOnly, "es")
	if !ok || best.LanguageCode != "es-419" {
		t.Fatalf("selectCaptionTrack asr-only = %+v (ok=%v), want es-419", best, ok)
	}
}

func TestFetchYouTubeCaptions(t *testing.T) {
	t.Run("fetches json3 captions and verifies player payload and headers", func(t *testing.T) {
		var srv *httptest.Server
		var gotUA, gotCT, gotFmt, gotOrigParam string
		var gotVideoID, gotClientName string

		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/player" {
				gotUA = r.Header.Get("User-Agent")
				gotCT = r.Header.Get("Content-Type")
				var reqPayload ytPlayerRequest
				if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
					t.Fatalf("decode player request: %v", err)
				}
				gotVideoID = reqPayload.VideoID
				gotClientName = reqPayload.Context.Client.ClientName

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{
						"title":  "Persiana Americana",
						"author": "Soda Stereo",
					},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{
									"baseUrl":      srv.URL + "/timedtext?lang=es&v=dQw4w9WgXcQ",
									"languageCode": "es",
									"kind":         "",
								},
							},
						},
					},
				})
				return
			}

			if r.URL.Path == "/timedtext" {
				gotFmt = r.URL.Query().Get("fmt")
				gotOrigParam = r.URL.Query().Get("lang")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"events": [
						{"tStartMs": 1000, "segs": [{"utf8": "Yo te prefiero"}]},
						{"tStartMs": 3500, "segs": [{"utf8": "fuera de foco"}]}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		got, err := FetchYouTubeCaptions(context.Background(), srv.Client(), srv.URL+"/player", "dQw4w9WgXcQ", "es")
		if err != nil {
			t.Fatalf("FetchYouTubeCaptions error: %v", err)
		}
		if gotUA != youtubeAndroidUserAgent || gotCT != "application/json" {
			t.Errorf("unexpected headers ua=%q ct=%q", gotUA, gotCT)
		}
		if gotVideoID != "dQw4w9WgXcQ" || gotClientName != "ANDROID" {
			t.Errorf("unexpected player request payload videoId=%q client=%q", gotVideoID, gotClientName)
		}
		if gotFmt != "json3" || gotOrigParam != "es" {
			t.Errorf("unexpected caption query fmt=%q lang=%q", gotFmt, gotOrigParam)
		}
		if got.Source != SourceYouTubeCC || got.Language != "es" || got.TrackName != "Persiana Americana" || got.ArtistName != "Soda Stereo" {
			t.Errorf("unexpected metadata: %+v", got)
		}
		if len(got.Synced) != 2 || got.Synced[0].Text != "Yo te prefiero" || got.Synced[1].Text != "fuera de foco" {
			t.Errorf("unexpected synced lines: %+v", got.Synced)
		}
		if !slices.Equal(got.Plain, []string{"Yo te prefiero", "fuera de foco"}) {
			t.Errorf("unexpected plain lines: %+v", got.Plain)
		}
	})

	t.Run("falls back to XML and marks ASR source", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/player" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"videoDetails": map[string]any{"title": "Podcast Ep 1", "author": "Host"},
					"captions": map[string]any{
						"playerCaptionsTracklistRenderer": map[string]any{
							"captionTracks": []map[string]any{
								{
									"baseUrl":      srv.URL + "/timedtext",
									"languageCode": "en",
									"kind":         "asr",
								},
							},
						},
					},
				})
				return
			}
			_, _ = w.Write([]byte(`<transcript><text start="2.5">Auto caption line</text></transcript>`))
		}))
		defer srv.Close()

		got, err := FetchYouTubeCaptions(context.Background(), srv.Client(), srv.URL+"/player", "dQw4w9WgXcQ", "es")
		if err != nil {
			t.Fatalf("FetchYouTubeCaptions error: %v", err)
		}
		if got.Source != SourceYouTubeASR || got.Language != "en" {
			t.Fatalf("unexpected ASR metadata: %+v", got)
		}
		if len(got.Synced) != 1 || got.Synced[0].Time != 2.5 || got.Synced[0].Text != "Auto caption line" {
			t.Fatalf("unexpected XML fallback lines: %+v", got.Synced)
		}
	})

	t.Run("returns ErrNotFound when no captionTracks or invalid videoID", func(t *testing.T) {
		if _, err := FetchYouTubeCaptions(context.Background(), nil, "http://127.0.0.1:0", "invalid", "es"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for invalid videoID, got %v", err)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{"title": "Instrumental"},
			})
		}))
		defer srv.Close()

		if _, err := FetchYouTubeCaptions(context.Background(), srv.Client(), srv.URL, "dQw4w9WgXcQ", "es"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound when captionTracks missing, got %v", err)
		}
	})
}

func TestClientOffsets_PersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	c := NewClient()
	c.CacheDir = dir

	if got := c.GetOffset("dQw4w9WgXcQ"); got != 0 {
		t.Fatalf("initial offset = %v, want 0", got)
	}

	// Rounds to 0.1s.
	if err := c.SetOffset("dQw4w9WgXcQ", 1.24); err != nil {
		t.Fatalf("SetOffset error: %v", err)
	}
	if got := c.GetOffset("dQw4w9WgXcQ"); got != 1.2 {
		t.Fatalf("GetOffset rounded = %v, want 1.2", got)
	}

	// Clamps to [-60, 60].
	if err := c.SetOffset("abc12345678", 95.0); err != nil {
		t.Fatalf("SetOffset clamp high error: %v", err)
	}
	if err := c.SetOffset("xyz12345678", -80.0); err != nil {
		t.Fatalf("SetOffset clamp low error: %v", err)
	}

	offsetsPath := filepath.Join(dir, "lyrics_offsets.json")
	info, err := os.Stat(offsetsPath)
	if err != nil {
		t.Fatalf("Stat(%q) error: %v", offsetsPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("offsets file perm = %o, want 0600", perm)
	}

	// New client lazy-loads persisted offsets.
	c2 := NewClient()
	c2.CacheDir = dir
	if got := c2.GetOffset("dQw4w9WgXcQ"); got != 1.2 {
		t.Errorf("reloaded offset = %v, want 1.2", got)
	}
	if got := c2.GetOffset("abc12345678"); got != 60.0 {
		t.Errorf("reloaded high clamp = %v, want 60.0", got)
	}
	if got := c2.GetOffset("xyz12345678"); got != -60.0 {
		t.Errorf("reloaded low clamp = %v, want -60.0", got)
	}

	// Near-zero deletes entry.
	if err := c2.SetOffset("dQw4w9WgXcQ", 0.04); err != nil {
		t.Fatalf("SetOffset near zero error: %v", err)
	}
	if got := c2.GetOffset("dQw4w9WgXcQ"); got != 0 {
		t.Errorf("cleared offset = %v, want 0", got)
	}

	raw, err := os.ReadFile(offsetsPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	var disk map[string]float64
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("Unmarshal offsets error: %v", err)
	}
	if _, exists := disk["dQw4w9WgXcQ"]; exists {
		t.Errorf("expected near-zero key to be deleted from disk, got %+v", disk)
	}
}
