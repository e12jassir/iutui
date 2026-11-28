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
	"sync"
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

	t.Run("HybridCatalog uses local TSV feed on Direct failure and returns nil on search failure", func(t *testing.T) {
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

		badYtdlp := writeFakeYTDLP(t, tmp, `exit 1`)

		h := NewHybridCatalog(
			NewBashCatalog(cacheDir, stateDir, ""),
			NewYTDLPCatalog(cacheDir, 5*time.Minute, badYtdlp),
		)

		ctx := context.Background()
		feed, err := h.Feed(ctx, model.TabHome)
		if err != nil || len(feed) != 1 || feed[0].ID != "vid001" {
			t.Fatalf("Feed = %+v, err=%v", feed, err)
		}

		res, err := h.Search(ctx, "fallback")
		if err == nil || res != nil {
			t.Fatalf("Search fallback = %+v, err=%v, want (nil, non-nil err)", res, err)
		}
	})

	t.Run("YTDLPCatalog Search never passes cookies, ignores empty cache, and RefreshSearch invalidates cache", func(t *testing.T) {
		tmp := t.TempDir()
		cookieJar := filepath.Join(tmp, "cookies.txt")
		if err := os.WriteFile(cookieJar, []byte("# Netscape\n.youtube.com\tTRUE\t/\tTRUE\t2000000000\tSID\tx\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		argsLog := filepath.Join(tmp, "args.log")
		emptyFlag := filepath.Join(tmp, "empty")
		if err := os.WriteFile(emptyFlag, []byte("1"), 0o600); err != nil {
			t.Fatal(err)
		}
		script := fmt.Sprintf(`
printf '%%s\n' "$*" >> %s
if [ -f %s ]; then
  rm -f %s
  exit 0
fi
printf '{"id":"srchOK11111","title":"Fresh Search","channel":"Chan","duration":120}\n'
`, shellQuote(argsLog), shellQuote(emptyFlag), shellQuote(emptyFlag))

		bin := writeFakeYTDLP(t, tmp, script)
		cat := NewYTDLPCatalog(tmp, 5*time.Minute, bin)
		cat.CookieJarPath = cookieJar
		ctx := context.Background()

		// First search returns 0 rows -> must NOT poison the cache.
		res1, err := cat.Search(ctx, "diomedes")
		if err != nil {
			t.Fatalf("first search failed: %v", err)
		}
		if len(res1) != 0 {
			t.Fatalf("expected 0 results on first search, got %d", len(res1))
		}

		// Immediate second search must re-run yt-dlp and succeed without --cookies.
		res2, err := cat.Search(ctx, "diomedes")
		if err != nil {
			t.Fatalf("second search failed: %v", err)
		}
		if len(res2) != 1 || res2[0].ID != "srchOK11111" {
			t.Fatalf("expected fresh search result, got %+v", res2)
		}

		// RefreshSearch deletes cache entry and calls yt-dlp again.
		res3, err := cat.RefreshSearch(ctx, "diomedes")
		if err != nil || len(res3) != 1 {
			t.Fatalf("RefreshSearch failed: res=%+v err=%v", res3, err)
		}

		rawArgs, err := os.ReadFile(argsLog)
		if err != nil {
			t.Fatalf("read args log: %v", err)
		}
		if strings.Contains(string(rawArgs), "--cookies") {
			t.Fatalf("ytsearch must not pass --cookies, got args:\n%s", string(rawArgs))
		}
		lines := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
		if len(lines) != 3 {
			t.Fatalf("expected 3 yt-dlp invocations (empty + uncached retry + RefreshSearch), got %d:\n%s", len(lines), string(rawArgs))
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

		direct := NewYTDLPCatalog(cacheDir, 5*time.Minute, badYtdlp)
		defer direct.Wait()

		h := NewHybridCatalog(NewBashCatalog(cacheDir, stateDir, ""), direct)
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

func TestCatalogLiveSettersConcurrencyAndBehavior(t *testing.T) {
	tmp := t.TempDir()
	argsLog := filepath.Join(tmp, "args.log")
	countFile := filepath.Join(tmp, "count.txt")
	jarPath := filepath.Join(tmp, "cookies.txt")
	if err := os.WriteFile(jarPath, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := fmt.Sprintf(`
printf '%%s\n' "$@" >> %s
printf 'x' >> %s
printf '{"id":"liveSet1111","title":"Live Result","channel":"Tester","duration":200}\n'
`, shellQuote(argsLog), shellQuote(countFile))
	bin := writeFakeYTDLP(t, tmp, script)

	cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
	cat.SetFeedSize(25)
	cat.SetSearchSize(15)
	defer cat.Wait()

	h := NewHybridCatalog(nil, cat)
	h.SetDisableCookieAuthSubs(true)

	// 1. First Search(15) populates searchCache; SetSearchSize(55) clears cache and uses ytsearch55.
	ctx := context.Background()
	if _, err := cat.Search(ctx, "golang"); err != nil {
		t.Fatalf("Search 1 error: %v", err)
	}
	if _, err := cat.Search(ctx, "golang"); err != nil {
		t.Fatalf("Search 2 (cached) error: %v", err)
	}
	rawCnt, _ := os.ReadFile(countFile)
	if len(rawCnt) != 1 {
		t.Fatalf("expected 1 yt-dlp invocation with cached search, got %d", len(rawCnt))
	}

	cat.SetSearchSize(55)
	if _, err := cat.Search(ctx, "golang"); err != nil {
		t.Fatalf("Search after SetSearchSize(55) error: %v", err)
	}
	rawCnt, _ = os.ReadFile(countFile)
	if len(rawCnt) != 2 {
		t.Fatalf("expected SetSearchSize to invalidate search cache and invoke yt-dlp again, got %d calls", len(rawCnt))
	}

	// 2. SetCacheTTL(0) disables search caching.
	cat.SetCacheTTL(0)
	if _, err := cat.Search(ctx, "zero"); err != nil {
		t.Fatalf("Search zero 1 error: %v", err)
	}
	if _, err := cat.Search(ctx, "zero"); err != nil {
		t.Fatalf("Search zero 2 error: %v", err)
	}
	rawCnt, _ = os.ReadFile(countFile)
	if len(rawCnt) != 4 {
		t.Fatalf("expected CacheTTL=0 to bypass searchCache on repeat, got %d total calls", len(rawCnt))
	}

	// 3. Feed respects SetFeedSize, SetQuality, and SetCookieJar live.
	cat.SetFeedSize(77)
	cat.SetQuality(480)
	cat.SetCookieJar(jarPath)
	if _, err := cat.Feed(ctx, model.TabHome); err != nil {
		t.Fatalf("Feed after live setters error: %v", err)
	}
	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	argsStr := string(argsRaw)
	for _, want := range []string{"ytsearch15:golang", "ytsearch55:golang", "--playlist-end\n77", BuildFormat(480), jarPath} {
		if !strings.Contains(argsStr, want) {
			t.Fatalf("expected %q in logged yt-dlp args:\n%s", want, argsStr)
		}
	}

	// 4. Race test across concurrent setters and readers.
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				cat.SetFeedSize(30 + (id+i)%5*10)
				cat.SetSearchSize(20 + (id+i)%4*10)
				cat.SetQuality([]int{0, 1080, 720, 480, 360}[(id+i)%5])
				if i%2 == 0 {
					cat.SetCacheTTL(time.Duration(i+1) * time.Minute)
					cat.SetCookieJar(jarPath)
					h.SetDisableCookieAuthSubs(false)
				} else {
					cat.SetCacheTTL(0)
					cat.SetCookieJar("")
					h.SetDisableCookieAuthSubs(true)
				}
				_ = cat.FeedSizeLimit()
				_ = cat.searchSize()
				_, _ = cat.CacheTTLDuration()
				_ = cat.CookieJar()
				_ = cat.quality()
				_ = h.disableCookieAuthSubs()
			}
		}(g)
	}
	wg.Wait()
}

func TestHybridCatalog_ConcurrentFeedAndLiveSettersRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"contents": {
				"playlistPanelRenderer": {
					"contents": [
						{
							"playlistPanelVideoRenderer": {
								"videoId": "raceTrack01",
								"title": {"runs": [{"text": "Persiana Americana"}]},
								"longBylineText": {"runs": [{"text": "Soda Stereo"}]},
								"lengthText": {"runs": [{"text": "4:52"}]}
							}
						}
					]
				}
			}
		}`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	bin := writeFakeYTDLP(t, tmp, `printf '{"id":"raceHome001","title":"Race Home","channel":"Chan","duration":180}\n'`)
	direct := NewYTDLPCatalog(filepath.Join(tmp, "cache"), time.Minute, bin)
	defer direct.Wait()

	var history []model.Video
	for i := 1; i <= 6; i++ {
		history = append(history, model.Video{
			ID:       fmt.Sprintf("seedRace%03d", i),
			Title:    fmt.Sprintf("Track %d", i),
			Channel:  fmt.Sprintf("Artist %d", i),
			Duration: "03:30",
		})
	}

	h := NewHybridCatalog(nil, direct)
	h.RSSClient = srv.Client()
	h.LocalMusicBaseURL = srv.URL
	h.LocalMusicProvider = func(context.Context) (LocalMusicData, error) {
		return LocalMusicData{History: history}, nil
	}

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				direct.SetFeedSize(20 + (id+i)%5*10)
				if i%2 == 0 {
					direct.SetCacheTTL(time.Duration(i+1) * time.Second)
				} else {
					direct.SetCacheTTL(0)
				}
				direct.SetExtraArgs([]string{"--geo-bypass"})
			}
		}(g)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_, _ = h.Feed(context.Background(), model.TabMusic)
				_, _ = h.Feed(context.Background(), model.TabHome)
			}
		}()
	}
	wg.Wait()
}

func TestHybridCatalog_TabMusicAuthenticatedDoesNotEclipseFEmusicHome(t *testing.T) {
	tmp := t.TempDir()
	jar := writeTestYTMusicCookieJar(t, tmp, true)

	ytmBrowseJSON := `{
		"responseContext": {"mainAppWebResponseContext": {"loggedOut": false}},
		"contents": {
			"singleColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"musicShelfRenderer": {
										"contents": [
											{
												"musicResponsiveListItemRenderer": {
													"playlistItemData": {"videoId": "ytmHome0001"},
													"flexColumns": [
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Zoom"}]}}},
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Soda Stereo"}]}}}
													],
													"fixedColumns": [
														{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "3:26"}]}}}
													]
												}
											},
											{
												"musicResponsiveListItemRenderer": {
													"playlistItemData": {"videoId": "sharedDup01"},
													"flexColumns": [
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Shared Song"}]}}},
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Cerati"}]}}}
													],
													"fixedColumns": [
														{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "4:00"}]}}}
													]
												}
											}
										]
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`

	localRadioSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"contents": {
				"playlistPanelRenderer": {
					"contents": [
						{
							"playlistPanelVideoRenderer": {
								"videoId": "sharedDup01",
								"title": {"runs": [{"text": "Shared Song"}]},
								"longBylineText": {"runs": [{"text": "Cerati"}]},
								"lengthText": {"runs": [{"text": "4:00"}]},
								"navigationEndpoint": {"watchEndpoint": {"videoId": "sharedDup01", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
							}
						},
						{
							"playlistPanelVideoRenderer": {
								"videoId": "localDisc02",
								"title": {"runs": [{"text": "Puente"}]},
								"longBylineText": {"runs": [{"text": "Gustavo Cerati"}]},
								"lengthText": {"runs": [{"text": "4:33"}]},
								"navigationEndpoint": {"watchEndpoint": {"videoId": "localDisc02", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
							}
						}
					]
				}
			}
		}`))
	}))
	defer localRadioSrv.Close()

	var ytmHomeHits int
	var mu sync.Mutex
	ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ytmHomeHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ytmBrowseJSON))
	}))
	defer ytmSrv.Close()

	var history []model.Video
	for i := 1; i <= 6; i++ {
		history = append(history, model.Video{
			ID:       fmt.Sprintf("histMus000%d", i),
			Title:    fmt.Sprintf("Favorite %d", i),
			Channel:  fmt.Sprintf("Band %d", i),
			Duration: "03:40",
		})
	}

	bin := writeFakeYTDLP(t, tmp, `exit 1`)
	direct := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
	direct.SetCookieJar(jar)
	direct.musicEndpoint = ytmSrv.URL
	direct.musicHTTPClient = ytmSrv.Client()
	defer direct.Wait()

	h := NewHybridCatalog(nil, direct)
	h.RSSClient = localRadioSrv.Client()
	h.LocalMusicBaseURL = localRadioSrv.URL
	h.LocalMusicProvider = func(context.Context) (LocalMusicData, error) {
		return LocalMusicData{History: history}, nil
	}

	videos, err := h.Feed(context.Background(), model.TabMusic)
	if err != nil {
		t.Fatalf("Feed(TabMusic) error: %v", err)
	}
	mu.Lock()
	hits := ytmHomeHits
	mu.Unlock()
	if hits == 0 {
		t.Fatal("expected FEmusic_home to be queried when valid cookies are configured, got 0 hits")
	}
	if len(videos) == 0 || videos[0].ID != "ytmHome0001" {
		t.Fatalf("expected FEmusic_home track ytmHome0001 first, got %+v", videos)
	}

	seen := make(map[string]int)
	for _, v := range videos {
		seen[v.ID]++
	}
	if seen["ytmHome0001"] != 1 || seen["sharedDup01"] != 1 || seen["localDisc02"] != 1 {
		t.Fatalf("expected merged unique tracks from both FEmusic_home and local music without duplicates, got counts=%v videos=%+v", seen, videos)
	}
}

func TestCacheDisabledSkipsWritingFeedJSONAndExtraArgsInjected(t *testing.T) {
	t.Run("SetCacheTTL(0) prevents Feed, RefreshFeed, and BuildLocalMusicFeed from writing feed_*.json", func(t *testing.T) {
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		bin := writeFakeYTDLP(t, tmp, `printf '{"id":"nocache0001","title":"No Cache","channel":"Chan","duration":120}\n'`)

		cat := NewYTDLPCatalog(cacheDir, 5*time.Minute, bin)
		cat.SetCacheTTL(0)
		defer cat.Wait()

		if _, err := cat.Feed(context.Background(), model.TabHome); err != nil {
			t.Fatalf("Feed(TabHome) error: %v", err)
		}
		if _, err := cat.RefreshFeed(context.Background(), model.TabHome); err != nil {
			t.Fatalf("RefreshFeed(TabHome) error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(cacheDir, "feed_home.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected feed_home.json NOT to be written when cacheDisabled=true, stat err=%v", err)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"contents":{"playlistPanelRenderer":{"contents":[{"playlistPanelVideoRenderer":{"videoId":"locNoCache1","title":{"runs":[{"text":"Track"}]},"longBylineText":{"runs":[{"text":"Artist"}]},"lengthText":{"runs":[{"text":"3:30"}]}}}]}}}`))
		}))
		defer srv.Close()

		var history []model.Video
		for i := 1; i <= 5; i++ {
			history = append(history, model.Video{
				ID:       fmt.Sprintf("locSeed000%d", i),
				Title:    fmt.Sprintf("Seed %d", i),
				Channel:  fmt.Sprintf("Artist %d", i),
				Duration: "03:20",
			})
		}
		h := NewHybridCatalog(nil, cat)
		h.RSSClient = srv.Client()
		h.LocalMusicBaseURL = srv.URL
		h.LocalMusicProvider = func(context.Context) (LocalMusicData, error) {
			return LocalMusicData{History: history}, nil
		}
		if _, err := h.Feed(context.Background(), model.TabMusic); err != nil {
			t.Fatalf("Hybrid Feed(TabMusic) error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(cacheDir, localMusicCacheFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %s NOT to be written when cacheDisabled=true, stat err=%v", localMusicCacheFile, err)
		}
	})

	t.Run("SetExtraArgs injects extra flags into fetchTarget and ChannelVideos", func(t *testing.T) {
		tmp := t.TempDir()
		argsFile := filepath.Join(tmp, "args.txt")
		script := fmt.Sprintf(
			"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\nprintf '{\"id\":\"extra111111\",\"title\":\"Extra\",\"channel\":\"Chan\",\"duration\":100}\\n'\n",
			shellQuote(argsFile),
		)
		bin := writeFakeYTDLP(t, tmp, script)
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), time.Minute, bin)
		cat.SetExtraArgs([]string{"--js-runtimes", "deno", "--geo-bypass"})
		defer cat.Wait()

		if _, err := cat.Search(context.Background(), "pink floyd"); err != nil {
			t.Fatalf("Search error: %v", err)
		}
		searchArgs := readRecordedArgs(t, argsFile)
		if !hasArgPair(searchArgs, "--js-runtimes", "deno") || !slices.Contains(searchArgs, "--geo-bypass") {
			t.Fatalf("expected extra args in Search call, got %v", searchArgs)
		}
		if lastArg(searchArgs) != "ytsearch20:pink floyd" {
			t.Fatalf("expected target last in Search call, got %q", lastArg(searchArgs))
		}

		if _, err := cat.ChannelVideos(context.Background(), model.Video{ChannelID: "UC_x5XG1OV2P6uZZ5FSM9Ttw"}, 10); err != nil {
			t.Fatalf("ChannelVideos error: %v", err)
		}
		chanArgs := readRecordedArgs(t, argsFile)
		if !hasArgPair(chanArgs, "--js-runtimes", "deno") || !slices.Contains(chanArgs, "--geo-bypass") {
			t.Fatalf("expected extra args in ChannelVideos call, got %v", chanArgs)
		}
		if lastArg(chanArgs) != "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos" {
			t.Fatalf("expected target last in ChannelVideos call, got %q", lastArg(chanArgs))
		}
	})
}

func TestResolveChannelReference(t *testing.T) {
	t.Run("extracts UC ID directly without invoking yt-dlp", func(t *testing.T) {
		tmp := t.TempDir()
		called := filepath.Join(tmp, "called")
		bin := writeFakeYTDLP(t, tmp, fmt.Sprintf("touch %s\nexit 1\n", shellQuote(called)))

		for _, raw := range []string{
			"UC_x5XG1OV2P6uZZ5FSM9Ttw",
			"  https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos  ",
			"youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
		} {
			cid, label, err := ResolveChannelReference(context.Background(), bin, raw)
			if err != nil {
				t.Fatalf("ResolveChannelReference(%q) unexpected error: %v", raw, err)
			}
			if cid != "UC_x5XG1OV2P6uZZ5FSM9Ttw" || label != "" {
				t.Fatalf("ResolveChannelReference(%q) = (%q, %q), want (UC_x5XG1OV2P6uZZ5FSM9Ttw, \"\")", raw, cid, label)
			}
		}
		if _, err := os.Stat(called); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("expected yt-dlp NOT to be called for direct UC references")
		}
	})

	t.Run("resolves @handle,/@handle URL, /c/, and /user/ via yt-dlp --dump-single-json", func(t *testing.T) {
		tmp := t.TempDir()
		argsFile := filepath.Join(tmp, "args.txt")
		script := fmt.Sprintf(
			"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\nprintf '{\"channel_id\":\"UCsodaStereo111111111111\",\"channel\":\"Soda Stereo\",\"uploader\":\"Soda Stereo Oficial\"}\\n'\n",
			shellQuote(argsFile),
		)
		bin := writeFakeYTDLP(t, tmp, script)

		cases := []struct {
			raw     string
			wantURL string
		}{
			{raw: "@SodaStereo", wantURL: "https://www.youtube.com/@SodaStereo"},
			{raw: "https://www.youtube.com/@SodaStereo/videos", wantURL: "https://www.youtube.com/@SodaStereo/videos"},
			{raw: "youtube.com/c/SodaStereo", wantURL: "https://www.youtube.com/c/SodaStereo"},
			{raw: "https://m.youtube.com/user/SodaStereoVEVO/", wantURL: "https://www.youtube.com/user/SodaStereoVEVO"},
		}
		for _, tc := range cases {
			cid, label, err := ResolveChannelReference(context.Background(), bin, tc.raw)
			if err != nil {
				t.Fatalf("ResolveChannelReference(%q) error: %v", tc.raw, err)
			}
			if cid != "UCsodaStereo111111111111" || label != "Soda Stereo" {
				t.Fatalf("ResolveChannelReference(%q) = (%q, %q), want (UCsodaStereo111111111111, Soda Stereo)", tc.raw, cid, label)
			}
			args := readRecordedArgs(t, argsFile)
			wantFlags := []string{"--no-warnings", "--playlist-items", "1", "--flat-playlist", "--dump-single-json", tc.wantURL}
			if !slices.Equal(args, wantFlags) {
				t.Fatalf("ResolveChannelReference(%q) args = %v, want %v", tc.raw, args, wantFlags)
			}
		}
	})

	t.Run("falls back to uploader and entries[0] and respects context timeout", func(t *testing.T) {
		tmp := t.TempDir()
		bin := writeFakeYTDLP(t, tmp, `printf '{"entries":[{"channel_id":"UCpinkFloyd2222222222222","uploader":"Pink Floyd"}]}\n'`)
		cid, label, err := ResolveChannelReference(context.Background(), bin, "@PinkFloyd")
		if err != nil || cid != "UCpinkFloyd2222222222222" || label != "Pink Floyd" {
			t.Fatalf("got (%q, %q, %v), want (UCpinkFloyd2222222222222, Pink Floyd, nil)", cid, label, err)
		}

		slowBin := writeFakeYTDLP(t, tmp, `sleep 5`)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, _, err := ResolveChannelReference(ctx, slowBin, "@SlowChan"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	})
}
