package catalog

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestParseYouTubeCookies(t *testing.T) {
	now := time.Now().Unix()
	future := strconv.FormatInt(now+3600, 10)
	expired := strconv.FormatInt(now-3600, 10)

	t.Run("parses HttpOnly, filters domains and expiry, deduplicates, prefers SAPISID", func(t *testing.T) {
		jar := filepath.Join(t.TempDir(), "cookies.txt")
		content := strings.Join([]string{
			"# Netscape HTTP Cookie File",
			"",
			".google.com\tTRUE\t/\tTRUE\t" + future + "\tSID\tgoogle-sid",
			"notyoutube.com\tFALSE\t/\tTRUE\t" + future + "\tSAPISID\twrong-domain",
			".youtube.com\tTRUE\t/\tTRUE\t" + expired + "\tSAPISID\texpired-sapisid",
			"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t0\tHSID\thttp-only-hsid",
			".youtube.com\tTRUE\t/\tTRUE\t" + future + "\t__Secure-3PAPISID\tfallback-3p",
			".YouTube.COM\tTRUE\t/\tTRUE\t" + future + "\tSAPISID\told-sapisid",
			"music.youtube.com\tFALSE\t/\tTRUE\t" + future + "\tSAPISID\tfresh-sapisid",
			".youtube.com\tTRUE\t/\tTRUE\t" + future + "\t\tempty-name",
		}, "\n")
		if err := os.WriteFile(jar, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		header, sapisid, ok := parseYouTubeCookies(jar)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if sapisid != "fresh-sapisid" {
			t.Fatalf("sapisid = %q, want %q", sapisid, "fresh-sapisid")
		}
		wantHeader := "HSID=http-only-hsid; __Secure-3PAPISID=fallback-3p; SAPISID=fresh-sapisid"
		if header != wantHeader {
			t.Fatalf("cookieHeader = %q, want %q", header, wantHeader)
		}
	})

	t.Run("falls back to __Secure-3PAPISID when SAPISID missing", func(t *testing.T) {
		jar := filepath.Join(t.TempDir(), "cookies.txt")
		content := ".youtube.com\tTRUE\t/\tTRUE\t" + future + "\t__Secure-3PAPISID\tonly-3p\n"
		if err := os.WriteFile(jar, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		header, sapisid, ok := parseYouTubeCookies(jar)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if sapisid != "only-3p" {
			t.Fatalf("sapisid = %q, want %q", sapisid, "only-3p")
		}
		if header != "__Secure-3PAPISID=only-3p" {
			t.Fatalf("cookieHeader = %q, want %q", header, "__Secure-3PAPISID=only-3p")
		}
	})

	t.Run("returns ok=false when no auth cookie or missing jar", func(t *testing.T) {
		jar := filepath.Join(t.TempDir(), "cookies.txt")
		content := ".youtube.com\tTRUE\t/\tTRUE\t" + future + "\tVISITOR_INFO1_LIVE\tabc\n"
		if err := os.WriteFile(jar, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := parseYouTubeCookies(jar); ok {
			t.Fatal("expected ok=false when neither SAPISID nor __Secure-3PAPISID present")
		}
		if _, _, ok := parseYouTubeCookies(filepath.Join(t.TempDir(), "missing.txt")); ok {
			t.Fatal("expected ok=false when file does not exist")
		}
	})
}

func TestBuildSAPISIDHash(t *testing.T) {
	ts := int64(1700000000)
	sapisid := "secret-sapisid-value"
	origin := defaultYTMusicOrigin

	got := buildSAPISIDHash(ts, sapisid, origin)
	sum := sha1.Sum([]byte("1700000000 secret-sapisid-value https://music.youtube.com"))
	want := fmt.Sprintf("SAPISIDHASH 1700000000_%x", sum)
	if got != want {
		t.Fatalf("buildSAPISIDHash() = %q, want %q", got, want)
	}
	if !regexp.MustCompile(`^SAPISIDHASH \d+_[0-9a-f]{40}$`).MatchString(got) {
		t.Fatalf("unexpected SAPISIDHASH format: %q", got)
	}
}

func writeTestYTMusicCookieJar(t *testing.T, dir string, withSAPISID bool) string {
	t.Helper()
	future := strconv.FormatInt(time.Now().Unix()+3600, 10)
	lines := []string{
		"# Netscape HTTP Cookie File",
		"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t" + future + "\tSID\tsid-val",
	}
	if withSAPISID {
		lines = append(lines, ".youtube.com\tTRUE\t/\tTRUE\t"+future+"\tSAPISID\tsapi-val")
	}
	jar := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(jar, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestYTDLPCatalog_TabMusicInnerTube(t *testing.T) {
	sampleBrowseJSON := `{
		"responseContext": {
			"mainAppWebResponseContext": {
				"loggedOut": false
			}
		},
		"contents": {
			"singleColumnBrowseResultsRenderer": {
				"tabs": [
					{
						"tabRenderer": {
							"content": {
								"sectionListRenderer": {
									"contents": [
										{
											"musicCarouselShelfRenderer": {
												"contents": [
													{
														"musicResponsiveListItemRenderer": {
															"playlistItemData": {"videoId": "trackResp111"},
															"flexColumns": [
																{
																	"musicResponsiveListItemFlexColumnRenderer": {
																		"text": {"runs": [{"text": "Verano Rosa"}]}
																	}
																},
																{
																	"musicResponsiveListItemFlexColumnRenderer": {
																		"text": {
																			"runs": [
																				{"text": "Song"},
																				{"text": " • "},
																				{"text": "KAROL G, Judeline & rusowsky", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCkarolG11111111111111"}}},
																				{"text": " • "},
																				{"text": "427M plays"},
																				{"text": " • "},
																				{"text": "03:15"}
																			]
																		}
																	}
																}
															],
															"thumbnail": {
																"musicThumbnailRenderer": {
																	"thumbnail": {
																		"thumbnails": [
																			{"url": "https://img.example/small.jpg", "width": 120, "height": 120},
																			{"url": "https://img.example/large.jpg", "width": 544, "height": 544}
																		]
																	}
																}
															}
														}
													},
													{
														"musicTwoRowItemRenderer": {
															"title": {"runs": [{"text": "Playlist Only"}]},
															"subtitle": {"runs": [{"text": "Playlist • YouTube Music"}]},
															"navigationEndpoint": {"browseEndpoint": {"browseId": "VLPL123456"}}
														}
													},
													{
														"musicTwoRowItemRenderer": {
															"title": {"runs": [{"text": "DeBÍ TiRAR MáS FOToS"}]},
															"subtitle": {
																"runs": [
																	{"text": "Canción · "},
																	{"text": "Bad Bunny", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCbadBunny222222222222"}}},
																	{"text": " · 12M reproducciones · 4:02"}
																]
															},
															"thumbnailOverlay": {
																"musicItemThumbnailOverlayRenderer": {
																	"content": {
																		"musicPlayButtonRenderer": {
																			"playNavigationEndpoint": {
																				"watchEndpoint": {"videoId": "trackTwoRow2"}
																			}
																		}
																	}
																}
															}
														}
													},
													{
														"musicResponsiveListItemRenderer": {
															"playlistItemData": {"videoId": "trackResp111"},
															"flexColumns": [
																{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Duplicate Track"}]}}}
															]
														}
													}
												]
											}
										},
										{
											"musicShelfRenderer": {
												"contents": [
													{
														"musicResponsiveListItemRenderer": {
															"overlay": {
																"musicItemThumbnailOverlayRenderer": {
																	"content": {
																		"musicPlayButtonRenderer": {
																			"playNavigationEndpoint": {
																				"watchEndpoint": {"videoId": "trackShelf33"}
																			}
																		}
																	}
																}
															},
															"flexColumns": [
																{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Persiana Americana"}]}}},
																{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Soda Stereo", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCsodaStereo3333333333"}}}]}}}
															],
															"fixedColumns": [
																{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "4:52"}]}}}
															]
														}
													}
												]
											}
										}
									]
								}
							}
						}
					}
				]
			}
		}
	}`

	t.Run("authenticated request headers, payload, parsing, deduplication, and deterministic order", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeTestYTMusicCookieJar(t, tmp, true)

		var gotAuth, gotCookie, gotClientName, gotClientVer, gotOrigin, gotReferer, gotXOrigin, gotAuthUser, gotCT string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			gotAuth = r.Header.Get("Authorization")
			gotCookie = r.Header.Get("Cookie")
			gotClientName = r.Header.Get("X-YouTube-Client-Name")
			gotClientVer = r.Header.Get("X-YouTube-Client-Version")
			gotOrigin = r.Header.Get("Origin")
			gotReferer = r.Header.Get("Referer")
			gotXOrigin = r.Header.Get("X-Origin")
			gotAuthUser = r.Header.Get("X-Goog-AuthUser")
			gotCT = r.Header.Get("Content-Type")

			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &gotBody); err != nil {
				t.Fatalf("unmarshal request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sampleBrowseJSON))
		}))
		defer srv.Close()

		// Fake yt-dlp must NOT be invoked when InnerTube succeeds.
		bin := writeFakeYTDLP(t, tmp, `echo "yt-dlp should not run" >&2; exit 1`)
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.SetCacheTTL(0)
		cat.SetCookieJar(jar)
		cat.musicEndpoint = srv.URL
		cat.musicHTTPClient = srv.Client()
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabMusic)
		if err != nil {
			t.Fatalf("Feed(TabMusic) error: %v", err)
		}

		if !regexp.MustCompile(`^SAPISIDHASH \d+_[0-9a-f]{40}$`).MatchString(gotAuth) {
			t.Fatalf("Authorization = %q, want SAPISIDHASH", gotAuth)
		}
		if gotCookie != "SID=sid-val; SAPISID=sapi-val" {
			t.Fatalf("Cookie = %q", gotCookie)
		}
		if gotClientName != "67" || gotClientVer != defaultYTMusicClientVersion {
			t.Fatalf("client headers = (%q, %q)", gotClientName, gotClientVer)
		}
		if gotOrigin != defaultYTMusicOrigin || gotReferer != defaultYTMusicOrigin+"/" || gotXOrigin != defaultYTMusicOrigin || gotAuthUser != "0" || gotCT != "application/json" {
			t.Fatalf("unexpected origin/auth headers: origin=%q referer=%q x-origin=%q authuser=%q ct=%q", gotOrigin, gotReferer, gotXOrigin, gotAuthUser, gotCT)
		}

		if gotBody["browseId"] != "FEmusic_home" {
			t.Fatalf("browseId = %v, want FEmusic_home", gotBody["browseId"])
		}
		ctxMap, _ := gotBody["context"].(map[string]any)
		clientMap, _ := ctxMap["client"].(map[string]any)
		if clientMap["clientName"] != "WEB_REMIX" {
			t.Fatalf("clientName = %v, want WEB_REMIX", clientMap["clientName"])
		}
		if _, hasGL := clientMap["gl"]; hasGL {
			t.Fatal("did not expect hardcoded gl in client context")
		}
		if _, hasHL := clientMap["hl"]; hasHL {
			t.Fatal("did not expect hardcoded hl in client context")
		}

		if len(videos) != 3 {
			t.Fatalf("len(videos) = %d (%+v), want 3", len(videos), videos)
		}

		v0 := videos[0]
		if v0.ID != "trackResp111" || v0.Title != "Verano Rosa" || v0.Channel != "KAROL G, Judeline & rusowsky" ||
			v0.ChannelID != "UCkarolG11111111111111" || v0.Views != "427M plays" || v0.Duration != "03:15" ||
			v0.Thumbnail != "https://img.example/large.jpg" {
			t.Fatalf("videos[0] = %+v", v0)
		}

		v1 := videos[1]
		if v1.ID != "trackTwoRow2" || v1.Title != "DeBÍ TiRAR MáS FOToS" || v1.Channel != "Bad Bunny" ||
			v1.ChannelID != "UCbadBunny222222222222" || v1.Views != "12M reproducciones" || v1.Duration != "4:02" ||
			v1.Thumbnail != "https://i.ytimg.com/vi/trackTwoRow2/hqdefault.jpg" {
			t.Fatalf("videos[1] = %+v", v1)
		}

		v2 := videos[2]
		if v2.ID != "trackShelf33" || v2.Title != "Persiana Americana" || v2.Channel != "Soda Stereo" ||
			v2.ChannelID != "UCsodaStereo3333333333" || v2.Duration != "4:52" {
			t.Fatalf("videos[2] = %+v", v2)
		}

		wantIDs := []string{"trackResp111", "trackTwoRow2", "trackShelf33"}
		for i := range 10 {
			again, err := cat.Feed(context.Background(), model.TabMusic)
			if err != nil {
				t.Fatalf("run %d error: %v", i, err)
			}
			var gotIDs []string
			for _, v := range again {
				gotIDs = append(gotIDs, v.ID)
			}
			if !slices.Equal(gotIDs, wantIDs) {
				t.Fatalf("run %d order = %v, want %v", i, gotIDs, wantIDs)
			}
		}
	})

	t.Run("loggedOut response invokes OnCookiesStale and falls back to yt-dlp", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeTestYTMusicCookieJar(t, tmp, true)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"responseContext":{"mainAppWebResponseContext":{"loggedOut":true}}}`))
		}))
		defer srv.Close()

		bin := writeFakeYTDLP(t, tmp, fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
case "$target" in
	%s) ;;
	%s)
		printf '{"id":"fallback1111","title":"Fallback Song","channel":"Artist","duration":180}\n'
		;;
esac
`, shellQuote(youtubeMusicDiscoveryURL), shellQuote(youtubeMusicSearchFallback)))

		var staleCount atomic.Int32
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.SetCacheTTL(0)
		cat.SetCookieJar(jar)
		cat.OnCookiesStale = func() { staleCount.Add(1) }
		cat.musicEndpoint = srv.URL
		cat.musicHTTPClient = srv.Client()
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabMusic)
		if err != nil {
			t.Fatalf("Feed(TabMusic) error: %v", err)
		}
		if staleCount.Load() != 1 {
			t.Fatalf("OnCookiesStale called %d times, want 1", staleCount.Load())
		}
		if len(videos) != 1 || videos[0].ID != "fallback1111" {
			t.Fatalf("videos = %+v, want fallback1111", videos)
		}
	})

	t.Run("HTTP 500 falls back cleanly to yt-dlp without error", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeTestYTMusicCookieJar(t, tmp, true)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		bin := writeFakeYTDLP(t, tmp, fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
case "$target" in
	%s) ;;
	%s)
		printf '{"id":"http500fb111","title":"Fallback After 500","channel":"Artist","duration":190}\n'
		;;
esac
`, shellQuote(youtubeMusicDiscoveryURL), shellQuote(youtubeMusicSearchFallback)))

		var staleCount atomic.Int32
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.SetCacheTTL(0)
		cat.SetCookieJar(jar)
		cat.OnCookiesStale = func() { staleCount.Add(1) }
		cat.musicEndpoint = srv.URL
		cat.musicHTTPClient = srv.Client()
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabMusic)
		if err != nil {
			t.Fatalf("Feed(TabMusic) error: %v", err)
		}
		if staleCount.Load() != 0 {
			t.Fatalf("OnCookiesStale called %d times on HTTP 500, want 0", staleCount.Load())
		}
		if len(videos) != 1 || videos[0].ID != "http500fb111" {
			t.Fatalf("videos = %+v, want http500fb111", videos)
		}
	})

	t.Run("missing SAPISID skips InnerTube and falls back to yt-dlp", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeTestYTMusicCookieJar(t, tmp, false)

		var httpHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpHits.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		bin := writeFakeYTDLP(t, tmp, fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
case "$target" in
	%s) ;;
	%s)
		printf '{"id":"noSapiFB1111","title":"Fallback No SAPISID","channel":"Artist","duration":205}\n'
		;;
esac
`, shellQuote(youtubeMusicDiscoveryURL), shellQuote(youtubeMusicSearchFallback)))

		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.SetCacheTTL(0)
		cat.SetCookieJar(jar)
		cat.musicEndpoint = srv.URL
		cat.musicHTTPClient = srv.Client()
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabMusic)
		if err != nil {
			t.Fatalf("Feed(TabMusic) error: %v", err)
		}
		if httpHits.Load() != 0 {
			t.Fatalf("expected 0 HTTP requests without SAPISID, got %d", httpHits.Load())
		}
		if len(videos) != 1 || videos[0].ID != "noSapiFB1111" {
			t.Fatalf("videos = %+v, want noSapiFB1111", videos)
		}
	})
}
