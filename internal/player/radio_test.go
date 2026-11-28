package player

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/e12jassir/iutui/internal/model"
)

func TestExtractYouTubeID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare id", raw: "dQw4w9WgXcQ", want: "dQw4w9WgXcQ"},
		{name: "watch url", raw: "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ", want: "dQw4w9WgXcQ"},
		{name: "music url", raw: "https://music.youtube.com/watch?v=abcDEF12345&si=xyz", want: "abcDEF12345"},
		{name: "youtu.be short", raw: "https://youtu.be/abcDEF12345?t=12", want: "abcDEF12345"},
		{name: "shorts url", raw: "https://www.youtube.com/shorts/abcDEF12345", want: "abcDEF12345"},
		{name: "local path invalid", raw: "/tmp/song.mp3", want: ""},
		{name: "empty", raw: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractYouTubeID(tt.raw); got != tt.want {
				t.Fatalf("ExtractYouTubeID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCurateRadioQueue_SpotifyQualityFiltersAndDiversity(t *testing.T) {
	seedID := "seed0000001"
	taste := BuildTasteProfile(
		[]model.Video{
			{ID: "liked000001", Title: "De Música Ligera", Channel: "Soda Stereo"},
		},
		[]model.Video{
			{ID: "hist0000001", Title: "Bocanada", Channel: "Gustavo Cerati"},
		},
	)

	candidates := []RadioTrack{
		{ID: seedID, Title: "Signos", Artist: "Soda Stereo", VideoType: musicVideoTypeATV, DurationSec: 315},
		{ID: "recent00001", Title: "Ya Sonada", Artist: "Soda Stereo", VideoType: musicVideoTypeATV, DurationSec: 210},
		// Same song twice: UGC lyric upload vs clean ATV track -> ATV must win dedup.
		{ID: "dup_ugc0001", Title: "Persiana Americana (Official Lyrics)", Artist: "Soda Stereo", VideoType: musicVideoTypeUGC, DurationSec: 292},
		{ID: "dup_atv0001", Title: "Persiana Americana", Artist: "Soda Stereo", VideoType: musicVideoTypeATV, DurationSec: 292},
		// Invalid lengths & junk titles.
		{ID: "short000001", Title: "Intro Short", Artist: "Soda Stereo", VideoType: musicVideoTypeATV, DurationSec: 45},
		{ID: "marathon001", Title: "Concierto Completo", Artist: "Soda Stereo", VideoType: musicVideoTypeOMV, DurationSec: 3600},
		{ID: "junk0000001", Title: "Lo-Fi Nightcore Sped Up", Artist: "Random", VideoType: musicVideoTypeOMV, DurationSec: 180},
		// Valid diverse pool (note multiple Gustavo Cerati + Charly García + Spinetta tracks).
		{ID: "cerati00001", Title: "Puente", Artist: "Gustavo Cerati", VideoType: musicVideoTypeATV, DurationSec: 273},
		{ID: "cerati00002", Title: "Crimen", Artist: "Gustavo Cerati", VideoType: musicVideoTypeATV, DurationSec: 232},
		{ID: "cerati00003", Title: "Lago en el Cielo", Artist: "Gustavo Cerati", VideoType: musicVideoTypeATV, DurationSec: 310},
		{ID: "charly00001", Title: "Nos Siguen Pegando Abajo", Artist: "Charly García", VideoType: musicVideoTypeATV, DurationSec: 208},
		{ID: "spinetta001", Title: "Seguir Viviendo Sin Tu Amor", Artist: "Luis Alberto Spinetta", VideoType: musicVideoTypeOMV, DurationSec: 160},
		{ID: "virus000001", Title: "Pronta Entrega", Artist: "Virus", VideoType: musicVideoTypeATV, DurationSec: 275},
	}

	got := CurateRadioQueue(seedID, candidates, taste, []string{"recent00001"}, 8)
	if len(got) != 7 {
		t.Fatalf("CurateRadioQueue len = %d, want 7 (%+v)", len(got), got)
	}

	gotIDs := make(map[string]bool, len(got))
	for _, tr := range got {
		gotIDs[tr.ID] = true
		if tr.ID == seedID || tr.ID == "recent00001" {
			t.Fatalf("CurateRadioQueue leaked excluded track %q", tr.ID)
		}
		if tr.ID == "short000001" || tr.ID == "marathon001" || tr.ID == "junk0000001" {
			t.Fatalf("CurateRadioQueue leaked filtered track %q (%q)", tr.ID, tr.Title)
		}
	}
	if !gotIDs["dup_atv0001"] || gotIDs["dup_ugc0001"] {
		t.Fatalf("expected ATV version dup_atv0001 to win canonical dedup over UGC, gotIDs=%v", gotIDs)
	}

	// Verify artist separation: first 3 tracks must not contain two tracks by the same primary artist.
	firstArtists := make(map[string]bool)
	for i := 0; i < 3; i++ {
		ak := primaryArtistKey(got[i].Artist, got[i].Title)
		if firstArtists[ak] {
			t.Fatalf("expected artist diversity in top 3 tracks, got consecutive duplicate artist %q in %+v", ak, got[:4])
		}
		firstArtists[ak] = true
	}

	// Verify legacy no-metadata order preservation.
	bare := CurateRadioQueue("s1", []RadioTrack{{ID: "s1"}, {ID: "a1"}, {ID: "b2"}, {ID: "a1"}}, TasteProfile{}, nil, 5)
	if len(bare) != 2 || bare[0].ID != "a1" || bare[1].ID != "b2" {
		t.Fatalf("expected legacy no-metadata fallback [a1 b2], got %+v", bare)
	}
}

func TestRadioClient_FetchRadioAndNonMusicUGCGate(t *testing.T) {
	cookiePath := filepath.Join(t.TempDir(), "cookies.txt")
	cookieContent := "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tsecret_sapisid_val\n"
	if err := os.WriteFile(cookiePath, []byte(cookieContent), 0o600); err != nil {
		t.Fatalf("write cookies: %v", err)
	}

	t.Run("YTMusic WEB_REMIX curated tracks with SAPISIDHASH", func(t *testing.T) {
		var gotAuth, gotCookie, gotPlaylistID string
		ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			gotCookie = r.Header.Get("Cookie")
			var reqBody struct {
				PlaylistID string `json:"playlistId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			gotPlaylistID = reqBody.PlaylistID
			_, _ = w.Write([]byte(`{
				"contents": {
					"singleColumnMusicWatchNextResultsRenderer": {
						"tabbedRenderer": {
							"watchNextTabbedResultsRenderer": {
								"tabs": [{
									"tabRenderer": {
										"content": {
											"musicQueueRenderer": {
												"content": {
													"playlistPanelRenderer": {
														"contents": [
															{
																"playlistPanelVideoRenderer": {
																	"videoId": "seed1234567",
																	"title": {"runs": [{"text": "Signos"}]},
																	"longBylineText": {"runs": [{"text": "Soda Stereo"}, {"text": " • "}, {"text": "Signos"}]},
																	"lengthText": {"runs": [{"text": "5:15"}]},
																	"navigationEndpoint": {"watchEndpoint": {"videoId": "seed1234567", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
																}
															},
															{
																"playlistPanelVideoRenderer": {
																	"videoId": "next7654321",
																	"title": {"runs": [{"text": "Adiós"}]},
																	"longBylineText": {"runs": [{"text": "Gustavo Cerati"}]},
																	"lengthText": {"runs": [{"text": "3:52"}]},
																	"navigationEndpoint": {"watchEndpoint": {"videoId": "next7654321", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
																}
															}
														]
													}
												}
											}
										}
									}
								}]
							}
						}
					}
				}
			}`))
		}))
		defer ytmSrv.Close()

		rc := NewRadioClient(WithRadioBaseURL(ytmSrv.URL), WithRadioCookiesPath(cookiePath))
		tracks, err := rc.FetchRadio(context.Background(), "seed1234567", 20)
		if err != nil {
			t.Fatalf("FetchRadio: %v", err)
		}
		if gotPlaylistID != "RDAMVMseed1234567" {
			t.Fatalf("playlistId = %q, want RDAMVMseed1234567", gotPlaylistID)
		}
		if !strings.HasPrefix(gotAuth, "SAPISIDHASH ") || !strings.Contains(gotCookie, "SAPISID=secret_sapisid_val") {
			t.Fatalf("expected SAPISIDHASH auth and Cookie headers, got auth=%q cookie=%q", gotAuth, gotCookie)
		}
		if len(tracks) != 2 || tracks[1].DisplayTitle() != "Gustavo Cerati - Adiós" || tracks[1].DurationSec != 232 {
			t.Fatalf("unexpected tracks: %+v", tracks)
		}
	})

	t.Run("Non-music UGC gate falls back to standard YouTube RD mix", func(t *testing.T) {
		ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{
				"contents": {
					"playlistPanelRenderer": {
						"contents": [
							{
								"playlistPanelVideoRenderer": {
									"videoId": "ugcSeed1234",
									"title": {"runs": [{"text": "Podcast Clip"}]},
									"longBylineText": {"runs": [{"text": "Some Channel"}]},
									"lengthText": {"runs": [{"text": "4:10"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "ugcSeed1234", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_UGC"}}}}
								}
							}
						]
					}
				}
			}`))
		}))
		defer ytmSrv.Close()

		var fallbackPlaylistID string
		ytSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqBody struct {
				PlaylistID string `json:"playlistId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			fallbackPlaylistID = reqBody.PlaylistID
			_, _ = w.Write([]byte(`{
				"contents": {
					"twoColumnWatchNextResults": {
						"playlist": {
							"playlist": {
								"contents": [
									{
										"playlistPanelVideoRenderer": {
											"videoId": "ytNext12345",
											"title": {"simpleText": "Next Standard Mix Video"},
											"shortBylineText": {"runs": [{"text": "Tech Channel"}]},
											"lengthText": {"simpleText": "6:00"}
										}
									}
								]
							}
						}
					}
				}
			}`))
		}))
		defer ytSrv.Close()

		rc := NewRadioClient(
			WithRadioBaseURL(ytmSrv.URL),
			WithRadioYouTubeBaseURL(ytSrv.URL),
			WithRadioCookiesPath(filepath.Join(t.TempDir(), "missing.txt")),
		)
		tracks, err := rc.FetchRadio(context.Background(), "ugcSeed1234", 20)
		if err != nil {
			t.Fatalf("FetchRadio fallback: %v", err)
		}
		if fallbackPlaylistID != "RDugcSeed1234" {
			t.Fatalf("fallback playlistId = %q, want RDugcSeed1234", fallbackPlaylistID)
		}
		if len(tracks) != 1 || tracks[0].ID != "ytNext12345" {
			t.Fatalf("expected standard YT fallback track ytNext12345, got %+v", tracks)
		}
	})

	t.Run("UGC non-music seed surrounded by generic ATV trending tracks triggers fallback", func(t *testing.T) {
		ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{
				"contents": {
					"playlistPanelRenderer": {
						"contents": [
							{
								"playlistPanelVideoRenderer": {
									"videoId": "xdAHChvTT9s",
									"title": {"runs": [{"text": "GTA 6 - Comienza la Aventura #1"}]},
									"longBylineText": {"runs": [{"text": "VEGETTA777"}]},
									"lengthText": {"runs": [{"text": "1:38:02"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "xdAHChvTT9s", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_UGC"}}}}
								}
							},
							{
								"playlistPanelVideoRenderer": {
									"videoId": "PWIQFC5YZPA",
									"title": {"runs": [{"text": "Tremendo Desastre"}]},
									"longBylineText": {"runs": [{"text": "Myke Towers"}]},
									"lengthText": {"runs": [{"text": "3:23"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "PWIQFC5YZPA", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
								}
							}
						]
					}
				}
			}`))
		}))
		defer ytmSrv.Close()

		ytSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{
				"contents": {
					"twoColumnWatchNextResults": {
						"secondaryResults": {
							"secondaryResults": {
								"results": [
									{
										"lockupViewModel": {
											"contentId": "liveVid0001",
											"contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
											"contentImage": {
												"thumbnailViewModel": {
													"overlays": [{"thumbnailBottomOverlayViewModel": {"badges": [{"thumbnailBadgeViewModel": {"text": "EN DIRECTO", "badgeStyle": "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE"}}]}}]
												}
											},
											"metadata": {
												"lockupMetadataViewModel": {
													"title": {"content": "Directo 24/7"},
													"metadata": {"contentMetadataViewModel": {"metadataRows": [{"metadataParts": [{"text": {"content": "Live Chan"}}]}]}}
												}
											}
										}
									},
									{
										"lockupViewModel": {
											"contentId": "jAJyfTkwJ1w",
											"contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
											"contentImage": {
												"thumbnailViewModel": {
													"overlays": [{"thumbnailBottomOverlayViewModel": {"badges": [{"thumbnailBadgeViewModel": {"text": "33:45", "badgeStyle": "THUMBNAIL_OVERLAY_BADGE_STYLE_DEFAULT"}}]}}]
												}
											},
											"metadata": {
												"lockupMetadataViewModel": {
													"title": {"content": "GTA 6 Ep 2"},
													"metadata": {"contentMetadataViewModel": {"metadataRows": [{"metadataParts": [{"text": {"content": "VEGETTA777"}}]}]}}
												}
											}
										}
									}
								]
							}
						}
					}
				}
			}`))
		}))
		defer ytSrv.Close()

		rc := NewRadioClient(
			WithRadioBaseURL(ytmSrv.URL),
			WithRadioYouTubeBaseURL(ytSrv.URL),
			WithRadioCookiesPath(filepath.Join(t.TempDir(), "missing.txt")),
		)
		tracks, err := rc.FetchRadio(context.Background(), "xdAHChvTT9s", 20)
		if err != nil {
			t.Fatalf("FetchRadio: %v", err)
		}
		if len(tracks) != 1 || tracks[0].ID != "jAJyfTkwJ1w" || tracks[0].Title != "GTA 6 Ep 2" || tracks[0].Artist != "VEGETTA777" || tracks[0].DurationSec != 2025 {
			t.Fatalf("expected non-live lockupViewModel fallback track jAJyfTkwJ1w, got %+v", tracks)
		}
		// Non-music long videos must survive CurateRadioQueue instead of being dropped by the 12-min music cap.
		curated := CurateRadioQueue("xdAHChvTT9s", tracks, TasteProfile{}, nil, 10)
		if len(curated) != 1 || curated[0].ID != "jAJyfTkwJ1w" {
			t.Fatalf("expected long non-music video to survive CurateRadioQueue, got %+v", curated)
		}
	})

	t.Run("dropped non-music seed never leaks generic ATV trending playlist when YT fallback is empty", func(t *testing.T) {
		ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{
				"contents": {
					"playlistPanelRenderer": {
						"contents": [
							{
								"playlistPanelVideoRenderer": {
									"videoId": "676fiG_rJbM",
									"title": {"runs": [{"text": "I'm The Big Bad"}]},
									"longBylineText": {"runs": [{"text": "CG5"}]},
									"lengthText": {"runs": [{"text": "3:01"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "676fiG_rJbM", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
								}
							}
						]
					}
				}
			}`))
		}))
		defer ytmSrv.Close()

		ytSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"contents": {}}`))
		}))
		defer ytSrv.Close()

		rc := NewRadioClient(
			WithRadioBaseURL(ytmSrv.URL),
			WithRadioYouTubeBaseURL(ytSrv.URL),
			WithRadioCookiesPath(filepath.Join(t.TempDir(), "missing.txt")),
		)
		tracks, _ := rc.FetchRadio(context.Background(), "KrY03Vv4O1s", 20)
		if len(tracks) != 0 {
			t.Fatalf("expected rejected generic YTM playlist not to leak when YT fallback is empty, got %+v", tracks)
		}
	})
}

func TestCurateRadioQueue_SkipPenaltyAndMultiSeedConsensus(t *testing.T) {
	taste := BuildTasteProfile(nil, nil)
	taste.SkippedTracks["skipExact01"] = true
	taste.SkippedArtists["bad artist"] = 2.0

	candidates := []RadioTrack{
		{ID: "skipExact01", Title: "Skipped Song", Artist: "Neutral", VideoType: musicVideoTypeATV, DurationSec: 200},
		{ID: "badArtist01", Title: "Another One", Artist: "Bad Artist", VideoType: musicVideoTypeATV, DurationSec: 200},
		{ID: "singleSeed1", Title: "Solo Candidate", Artist: "Artist A", VideoType: musicVideoTypeATV, DurationSec: 210, ConsensusCount: 1},
		{ID: "multiSeed01", Title: "Mood Consensus Track", Artist: "Artist B", VideoType: musicVideoTypeATV, DurationSec: 220, ConsensusCount: 3},
	}

	got := CurateRadioQueue("seed0000001", candidates, taste, nil, 5)
	if len(got) < 2 {
		t.Fatalf("expected >= 2 curated tracks, got %+v", got)
	}
	if got[0].ID != "multiSeed01" {
		t.Fatalf("expected multi-seed consensus track #1, got %+v", got)
	}
	for _, tr := range got {
		if tr.ID == "skipExact01" {
			t.Fatalf("skipped track should be excluded, got %+v", got)
		}
	}
	if got[len(got)-1].ID != "badArtist01" {
		t.Fatalf("expected skipped artist track demoted to bottom, got %+v", got)
	}
}

func TestRadioClient_FetchMultiSeedRadio(t *testing.T) {
	ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VideoID string `json:"videoId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.VideoID == "primarySeed" {
			_, _ = w.Write([]byte(`{
				"contents": {"playlistPanelRenderer": {"contents": [
					{"playlistPanelVideoRenderer": {"videoId": "primarySeed", "title": {"runs": [{"text": "Seed Song"}]}, "longBylineText": {"runs": [{"text": "Soda Stereo"}]}, "lengthText": {"runs": [{"text": "4:00"}]}, "navigationEndpoint": {"watchEndpoint": {"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}}},
					{"playlistPanelVideoRenderer": {"videoId": "sharedSong1", "title": {"runs": [{"text": "Prófugos"}]}, "longBylineText": {"runs": [{"text": "Soda Stereo"}]}, "lengthText": {"runs": [{"text": "5:18"}]}, "navigationEndpoint": {"watchEndpoint": {"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}}},
					{"playlistPanelVideoRenderer": {"videoId": "onlyPrim001", "title": {"runs": [{"text": "Solo Prim"}]}, "longBylineText": {"runs": [{"text": "Soda Stereo"}]}, "lengthText": {"runs": [{"text": "3:10"}]}, "navigationEndpoint": {"watchEndpoint": {"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}}}
				]}}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"contents": {"playlistPanelRenderer": {"contents": [
				{"playlistPanelVideoRenderer": {"videoId": "extraSeed01", "title": {"runs": [{"text": "Extra Seed"}]}, "longBylineText": {"runs": [{"text": "Cerati"}]}, "lengthText": {"runs": [{"text": "3:45"}]}, "navigationEndpoint": {"watchEndpoint": {"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}}},
				{"playlistPanelVideoRenderer": {"videoId": "sharedSong1", "title": {"runs": [{"text": "Prófugos"}]}, "longBylineText": {"runs": [{"text": "Soda Stereo"}]}, "lengthText": {"runs": [{"text": "5:18"}]}, "navigationEndpoint": {"watchEndpoint": {"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}}}
			]}}
		}`))
	}))
	defer ytmSrv.Close()

	rc := NewRadioClient(WithRadioBaseURL(ytmSrv.URL), WithRadioCookiesPath(filepath.Join(t.TempDir(), "missing.txt")))
	tracks, err := rc.FetchMultiSeedRadio(context.Background(), "primarySeed", []string{"extraSeed01"}, 20)
	if err != nil {
		t.Fatalf("FetchMultiSeedRadio: %v", err)
	}
	var shared RadioTrack
	for _, tr := range tracks {
		if tr.ID == "sharedSong1" {
			shared = tr
		}
	}
	if shared.ConsensusCount != 2 {
		t.Fatalf("expected sharedSong1 ConsensusCount=2 across primary+extra seed, got %+v", tracks)
	}
}

func TestRadioClient_DefaultCacheCookiesAndConcurrentSetCookiesPath(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	rc := NewRadioClient()
	wantDefault := filepath.Join(cacheHome, "iutui", "cookies.txt")
	if rc.cookiesPath != wantDefault {
		t.Fatalf("NewRadioClient() default cookiesPath = %q, want %q", rc.cookiesPath, wantDefault)
	}

	cookiePath := filepath.Join(t.TempDir(), "updated-cookies.txt")
	cookieContent := "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tdynamic_sapisid\n"
	if err := os.WriteFile(cookiePath, []byte(cookieContent), 0o600); err != nil {
		t.Fatalf("write cookies: %v", err)
	}

	var mu sync.Mutex
	var sawDynamicAuth bool
	ytmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Cookie"), "SAPISID=dynamic_sapisid") && strings.HasPrefix(r.Header.Get("Authorization"), "SAPISIDHASH ") {
			mu.Lock()
			sawDynamicAuth = true
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{
			"contents": {"playlistPanelRenderer": {"contents": [
				{"playlistPanelVideoRenderer": {"videoId": "seedDyn0001", "title": {"runs": [{"text": "Song"}]}, "longBylineText": {"runs": [{"text": "Artist"}]}, "lengthText": {"runs": [{"text": "3:30"}]}, "navigationEndpoint": {"watchEndpoint": {"watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}}}
			]}}
		}`))
	}))
	defer ytmSrv.Close()

	rc = NewRadioClient(WithRadioBaseURL(ytmSrv.URL))
	rc.SetCookiesPath(cookiePath)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				rc.SetCookiesPath(cookiePath)
			}
			_, _ = rc.FetchRadio(context.Background(), "seedDyn0001", 10)
		}(i)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if !sawDynamicAuth {
		t.Fatal("expected SetCookiesPath to supply SAPISID cookies to FetchRadio")
	}
}
