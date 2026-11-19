package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestHybridCatalogAndSearchCache(t *testing.T) {
	t.Run("YTDLPCatalog Search caches repeated queries in memory", func(t *testing.T) {
		tmp := t.TempDir()
		countFile := filepath.Join(tmp, "calls")
		script := filepath.Join(tmp, "yt-dlp")
		src := `#!/bin/sh
echo "1" >> "` + countFile + `"
printf '{"id":"abc123def45","title":"Pink Floyd Time","channel":"Pink Floyd","duration":417,"view_count":1000}\n'
`
		if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
			t.Fatal(err)
		}

		cat := NewYTDLPCatalog(tmp, 5*time.Minute, script)
		ctx := context.Background()

		res1, err := cat.Search(ctx, "pink floyd")
		if err != nil {
			t.Fatalf("first search failed: %v", err)
		}
		if len(res1) != 1 || res1[0].ID != "abc123def45" {
			t.Fatalf("unexpected results: %+v", res1)
		}

		res2, err := cat.Search(ctx, "pink floyd")
		if err != nil {
			t.Fatalf("second search failed: %v", err)
		}
		if len(res2) != 1 || res2[0].ID != "abc123def45" {
			t.Fatalf("unexpected cached results: %+v", res2)
		}

		raw, err := os.ReadFile(countFile)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != 2 { // "1\n" once
			t.Fatalf("expected yt-dlp called once, count file = %q", string(raw))
		}
	})

	t.Run("HybridCatalog uses local TSV feed and falls back to BashCatalog on search failure", func(t *testing.T) {
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		stateDir := filepath.Join(tmp, "state")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}

		tsv := "vid001\thttps://www.youtube.com/watch?v=vid001\tCached Video\tChan\t03:00\t1K\tCached Video\n"
		if err := os.WriteFile(filepath.Join(cacheDir, "feed_foryou.tsv"), []byte(tsv), 0o644); err != nil {
			t.Fatal(err)
		}

		ytStub := filepath.Join(tmp, "yt")
		if err := os.WriteFile(ytStub, []byte("#!/bin/sh\nprintf 'fb001\\thttps://www.youtube.com/watch?v=fb001\\tFallback\\tChan\\t01:00\\t500\\tFallback\\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		badYtdlp := filepath.Join(tmp, "bad-yt-dlp")
		if err := os.WriteFile(badYtdlp, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		h := NewHybridCatalog(
			NewBashCatalog(cacheDir, stateDir, ytStub),
			NewYTDLPCatalog(cacheDir, 5*time.Minute, badYtdlp),
		)

		ctx := context.Background()
		feed, err := h.Feed(ctx, model.TabHome)
		if err != nil || len(feed) != 1 || feed[0].ID != "vid001" {
			t.Fatalf("Feed = %+v, err=%v", feed, err)
		}

		res, err := h.Search(ctx, "fallback")
		if err != nil || len(res) != 1 || res[0].ID != "fb001" {
			t.Fatalf("Search fallback = %+v, err=%v", res, err)
		}
	})
}

func TestHybridCatalog_GuestRSSSubscriptions(t *testing.T) {
	const (
		chanA = "UCguestSubA1111111111111"
		chanB = "UCguestSubB2222222222222"
	)

	feedA := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns="http://www.w3.org/2005/Atom">
  <title>Chan A</title>
  <entry>
    <yt:videoId>vidAAAAAAA1</yt:videoId>
    <title>Guest RSS One</title>
    <author><name>Chan A</name></author>
    <published>2025-02-01T10:00:00Z</published>
  </entry>
</feed>`

	feedB := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns="http://www.w3.org/2005/Atom">
  <title>Chan B</title>
  <entry>
    <yt:videoId>vidBBBBBBB2</yt:videoId>
    <title>Guest RSS Two</title>
    <author><name>Chan B</name></author>
    <published>2025-02-05T12:00:00Z</published>
  </entry>
</feed>`

	newSubServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("channel_id") {
			case chanA:
				_, _ = w.Write([]byte(feedA))
			case chanB:
				_, _ = w.Write([]byte(feedB))
			default:
				http.NotFound(w, r)
			}
		}))
	}

	t.Run("serves guest RSS subscriptions when Direct lacks cookies and avoids stale TSV fallback", func(t *testing.T) {
		srv := newSubServer()
		defer srv.Close()

		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		stateDir := filepath.Join(tmp, "state")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}

		// Stale TSV feed must NOT be returned when guest RSS returns >0 videos.
		staleTSV := "stale001\thttps://www.youtube.com/watch?v=stale001\tStale Sub\tOld\t03:00\t100\n"
		if err := os.WriteFile(filepath.Join(cacheDir, "feed_subs.tsv"), []byte(staleTSV), 0o644); err != nil {
			t.Fatal(err)
		}

		// Pre-seed video_meta.json to verify Direct.applyCachedMeta enriches RSS videos.
		metaData, err := json.Marshal(map[string]cachedVideoMeta{
			"vidAAAAAAA1": {
				Views:     "42K vistas",
				Published: "2025-02-01",
				ChannelID: chanA,
				FetchedAt: time.Now(),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cacheDir, metaCacheFileName), metaData, 0o644); err != nil {
			t.Fatal(err)
		}

		ytdlpCalled := filepath.Join(tmp, "ytdlp.called")
		badYtdlp := writeFakeYTDLP(t, tmp, fmt.Sprintf("touch %s\nexit 1\n", shellQuote(ytdlpCalled)))
		badBash := filepath.Join(tmp, "yt")
		if err := os.WriteFile(badBash, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		direct := NewYTDLPCatalog(cacheDir, 5*time.Minute, badYtdlp)
		defer direct.Wait()

		h := NewHybridCatalog(NewBashCatalog(cacheDir, stateDir, badBash), direct)
		h.RSSClient = newRSSRedirectClient(srv)
		h.SubscriptionsProvider = func(context.Context) ([]string, error) {
			return []string{chanA, chanB, "@ignoredHandle"}, nil
		}

		for _, refresh := range []bool{false, true} {
			var (
				videos []model.Video
				err    error
			)
			if refresh {
				videos, err = h.RefreshFeed(context.Background(), model.TabSubscriptions)
			} else {
				videos, err = h.Feed(context.Background(), model.TabSubscriptions)
			}
			if err != nil {
				t.Fatalf("refresh=%v TabSubscriptions error: %v", refresh, err)
			}
			if len(videos) != 2 {
				t.Fatalf("refresh=%v got %d videos, want 2: %+v", refresh, len(videos), videos)
			}
			if videos[0].ID != "vidBBBBBBB2" || videos[1].ID != "vidAAAAAAA1" {
				t.Fatalf("refresh=%v unexpected order: %+v", refresh, videos)
			}
			if videos[0].ChannelID != chanB || videos[1].ChannelID != chanA {
				t.Fatalf("refresh=%v expected ChannelID populated, got %+v", refresh, videos)
			}
			if videos[1].Views != "42K vistas" {
				t.Errorf("refresh=%v expected cached meta Views applied, got %q", refresh, videos[1].Views)
			}
		}

		if _, err := os.Stat(ytdlpCalled); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected yt-dlp not to be invoked in guest mode without cookies")
		}
	})

	t.Run("falls back to guest RSS subscriptions when Direct with cookies fails", func(t *testing.T) {
		srv := newSubServer()
		defer srv.Close()

		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		jar := filepath.Join(tmp, "cookies.txt")
		if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\t1\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		failYtdlp := writeFakeYTDLP(t, tmp, `echo "ERROR: Sign in" >&2; exit 1`)
		direct := NewYTDLPCatalog(cacheDir, 5*time.Minute, failYtdlp)
		direct.CookieJarPath = jar
		defer direct.Wait()

		h := NewHybridCatalog(nil, direct)
		h.RSSClient = newRSSRedirectClient(srv)
		h.SubscriptionsProvider = func(context.Context) ([]string, error) {
			return []string{chanA}, nil
		}

		videos, err := h.Feed(context.Background(), model.TabSubscriptions)
		if err != nil {
			t.Fatalf("Feed error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "vidAAAAAAA1" {
			t.Fatalf("got %+v, want vidAAAAAAA1", videos)
		}
	})

	t.Run("uses Direct signed-in subscriptions when cookies exist and DisableCookieAuthSubs is false", func(t *testing.T) {
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		jar := filepath.Join(tmp, "cookies.txt")
		if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\t1\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		okYtdlp := writeFakeYTDLP(t, tmp, `printf '{"id":"authSub0001","title":"Signed In Sub","channel":"AuthChan","channel_id":"UCauthChan11111111111111","duration":90}\n'`)
		direct := NewYTDLPCatalog(cacheDir, 5*time.Minute, okYtdlp)
		direct.CookieJarPath = jar
		defer direct.Wait()

		rssCalled := false
		h := NewHybridCatalog(nil, direct)
		h.SubscriptionsProvider = func(context.Context) ([]string, error) {
			rssCalled = true
			return []string{chanA}, nil
		}

		videos, err := h.Feed(context.Background(), model.TabSubscriptions)
		if err != nil {
			t.Fatalf("Feed error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "authSub0001" || videos[0].ChannelID != "UCauthChan11111111111111" {
			t.Fatalf("got %+v, want authSub0001 with ChannelID", videos)
		}
		if rssCalled {
			t.Fatalf("expected signed-in Direct feed to avoid calling SubscriptionsProvider")
		}
	})

	t.Run("DisableCookieAuthSubs bypasses Direct even with valid cookies", func(t *testing.T) {
		srv := newSubServer()
		defer srv.Close()

		tmp := t.TempDir()
		jar := filepath.Join(tmp, "cookies.txt")
		if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\t1\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		ytdlpCalled := filepath.Join(tmp, "ytdlp.called")
		failYtdlp := writeFakeYTDLP(t, tmp, fmt.Sprintf("touch %s\nexit 1\n", shellQuote(ytdlpCalled)))
		direct := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, failYtdlp)
		direct.CookieJarPath = jar
		defer direct.Wait()

		h := NewHybridCatalog(nil, direct)
		h.DisableCookieAuthSubs = true
		h.RSSClient = newRSSRedirectClient(srv)
		h.SubscriptionsProvider = func(context.Context) ([]string, error) {
			return []string{chanB}, nil
		}

		videos, err := h.Feed(context.Background(), model.TabSubscriptions)
		if err != nil {
			t.Fatalf("Feed error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "vidBBBBBBB2" {
			t.Fatalf("got %+v, want vidBBBBBBB2", videos)
		}
		if _, err := os.Stat(ytdlpCalled); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected yt-dlp not to be invoked when DisableCookieAuthSubs is true")
		}
	})
}

func TestChannelIDPropagationAndMusicFallback(t *testing.T) {
	t.Run("entryToVideo maps channel_id into Video.ChannelID", func(t *testing.T) {
		raw := []byte(`{"id":"vid12345678","title":"Track","channel":"Artist","channel_id":"  UCartist1111111111111111  ","duration":180}`)
		videos, err := parseYTDLPOutput(raw)
		if err != nil {
			t.Fatalf("parseYTDLPOutput error: %v", err)
		}
		if len(videos) != 1 || videos[0].ChannelID != "UCartist1111111111111111" {
			t.Fatalf("got %+v, want trimmed ChannelID", videos)
		}
	})

	t.Run("ChannelVideos prefers Video.ChannelID over Channel name", func(t *testing.T) {
		tmp := t.TempDir()
		argsFile := filepath.Join(tmp, "args.txt")
		script := fmt.Sprintf(
			"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\nprintf '{\"id\":\"chvid111111\",\"title\":\"Channel Video\",\"channel\":\"Display Name\",\"channel_id\":\"UC_x5XG1OV2P6uZZ5FSM9Ttw\",\"duration\":120}\\n'\n",
			shellQuote(argsFile),
		)
		bin := writeFakeYTDLP(t, tmp, script)
		direct := NewYTDLPCatalog(filepath.Join(tmp, "cache"), time.Minute, bin)
		defer direct.Wait()

		h := NewHybridCatalog(nil, direct)
		input := model.Video{
			ID:        "seed1234567",
			Channel:   "Pink Floyd",
			ChannelID: "UC_x5XG1OV2P6uZZ5FSM9Ttw",
		}

		videos, err := h.ChannelVideos(context.Background(), input, 12)
		if err != nil {
			t.Fatalf("HybridCatalog.ChannelVideos error: %v", err)
		}
		if len(videos) != 1 || videos[0].ChannelID != "UC_x5XG1OV2P6uZZ5FSM9Ttw" {
			t.Fatalf("unexpected videos: %+v", videos)
		}
		if gotTarget := lastArg(readRecordedArgs(t, argsFile)); gotTarget != "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos" {
			t.Fatalf("target = %q, want channel URL from Video.ChannelID", gotTarget)
		}

		// Also verify HybridCatalog fallback path when Direct is nil.
		t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
		hNoDirect := NewHybridCatalog(nil, nil)
		if _, err := hNoDirect.ChannelVideos(context.Background(), input, 8); err != nil {
			t.Fatalf("HybridCatalog(nil Direct).ChannelVideos error: %v", err)
		}
		if gotTarget := lastArg(readRecordedArgs(t, argsFile)); gotTarget != "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos" {
			t.Fatalf("nil Direct target = %q, want channel URL from Video.ChannelID", gotTarget)
		}
	})

	t.Run("MetadataEnricher extracts, caches, and applies ChannelID", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"videoDetails": map[string]any{
					"viewCount": "1200",
					"channelId": "UCmetaChan11111111111111",
				},
				"microformat": map[string]any{
					"playerMicroformatRenderer": map[string]any{
						"publishDate": "2025-03-01",
					},
				},
			})
		}))
		defer srv.Close()

		cacheDir := filepath.Join(t.TempDir(), "cache")
		enr := NewMetadataEnricher(cacheDir)
		enr.EndpointURL = srv.URL

		meta, err := enr.Enrich(context.Background(), []string{"vidMeta1111"})
		if err != nil {
			t.Fatalf("Enrich error: %v", err)
		}
		if meta["vidMeta1111"].ChannelID != "UCmetaChan11111111111111" {
			t.Fatalf("got meta = %+v, want ChannelID", meta["vidMeta1111"])
		}

		reloaded := NewMetadataEnricher(cacheDir)
		cached, ok := reloaded.Cached("vidMeta1111")
		if !ok || cached.ChannelID != "UCmetaChan11111111111111" {
			t.Fatalf("Cached = (%+v, %v), want ChannelID preserved", cached, ok)
		}

		videos := []model.Video{
			{ID: "vidMeta1111", Views: "Preserved", Published: "2025-03-01"},
		}
		reloaded.ApplyCached(videos)
		if videos[0].ChannelID != "UCmetaChan11111111111111" || videos[0].Views != "Preserved" {
			t.Fatalf("ApplyCached = %+v, expected ChannelID populated while preserving Views", videos[0])
		}
	})

	t.Run("YTDLPCatalog TabMusic falls back to ytsearch30:music official video when primary is empty", func(t *testing.T) {
		tmp := t.TempDir()
		targetsFile := filepath.Join(tmp, "targets.txt")
		script := fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
printf '%%s\n' "$target" >> %s
case "$target" in
	%s)
		# Empty discovery output triggers music search fallback.
		;;
	"ytsearch30:music official video")
		printf '{"id":"musFB111111","title":"Official Track","channel":"Artist","channel_id":"UCmusicFallback111111111","duration":200}\n'
		;;
esac
`, shellQuote(targetsFile), shellQuote(youtubeMusicDiscoveryURL))

		bin := writeFakeYTDLP(t, tmp, script)
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabMusic)
		if err != nil {
			t.Fatalf("Feed(TabMusic) error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "musFB111111" || videos[0].ChannelID != "UCmusicFallback111111111" {
			t.Fatalf("Feed(TabMusic) = %+v, want musFB111111", videos)
		}

		rawTargets, err := os.ReadFile(targetsFile)
		if err != nil {
			t.Fatalf("read targets: %v", err)
		}
		gotTargets := strings.Split(strings.TrimSpace(string(rawTargets)), "\n")
		wantTargets := []string{youtubeMusicDiscoveryURL, "ytsearch30:music official video"}
		if !slices.Equal(gotTargets, wantTargets) {
			t.Fatalf("targets invoked = %v, want %v", gotTargets, wantTargets)
		}
	})
}
