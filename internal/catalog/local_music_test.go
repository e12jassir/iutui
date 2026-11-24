package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/store"
)

func TestBuildLocalMusicFeed_ColdStartFallsBackAndSeasonedUsesLocalTaste(t *testing.T) {
	t.Run("cold start under 5 local music tracks returns handled=false", func(t *testing.T) {
		data := LocalMusicData{
			History: []model.Video{
				{ID: "vid00000001", Title: "Song 1", Channel: "Artist A", Duration: "03:30"},
				{ID: "vid00000002", Title: "Song 2", Channel: "Artist B", Duration: "04:10"},
				// Non-music long/gameplay entry should not count toward music seed threshold.
				{ID: "vid00000003", Title: "GTA Gameplay Part 1", Channel: "Gamer", Duration: "45:00"},
			},
		}
		videos, handled, err := BuildLocalMusicFeed(context.Background(), data, t.TempDir(), time.Hour, false, 20, nil, "")
		if err != nil || handled || len(videos) != 0 {
			t.Fatalf("expected cold-start handled=false, got handled=%v len=%d err=%v", handled, len(videos), err)
		}
	})

	t.Run("seasoned user builds personalized cookieless local feed ignoring skipped artists", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Errorf("local music feed must be cookieless, got cookie=%q auth=%q", r.Header.Get("Cookie"), r.Header.Get("Authorization"))
			}
			var req struct {
				VideoID string `json:"videoId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			_, _ = w.Write([]byte(`{
				"contents": {
					"playlistPanelRenderer": {
						"contents": [
							{
								"playlistPanelVideoRenderer": {
									"videoId": "consTrack01",
									"title": {"runs": [{"text": "La Ciudad de la Furia"}]},
									"longBylineText": {"runs": [{"text": "Soda Stereo"}]},
									"lengthText": {"runs": [{"text": "5:45"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "consTrack01", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
								}
							},
							{
								"playlistPanelVideoRenderer": {
									"videoId": "skipBadTrk1",
									"title": {"runs": [{"text": "Bad Repeated Track"}]},
									"longBylineText": {"runs": [{"text": "Skipped Artist"}]},
									"lengthText": {"runs": [{"text": "3:20"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "skipBadTrk1", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
								}
							},
							{
								"playlistPanelVideoRenderer": {
									"videoId": "junkMix9999",
									"title": {"runs": [{"text": "10 Hours Lo-Fi Nightcore"}]},
									"longBylineText": {"runs": [{"text": "Spam"}]},
									"lengthText": {"runs": [{"text": "3:00"}]}
								}
							},
							{
								"playlistPanelVideoRenderer": {
									"videoId": "ceratiFresh",
									"title": {"runs": [{"text": "Crimen"}]},
									"longBylineText": {"runs": [{"text": "Gustavo Cerati"}]},
									"lengthText": {"runs": [{"text": "3:52"}]},
									"navigationEndpoint": {"watchEndpoint": {"videoId": "ceratiFresh", "watchEndpointMusicSupportedConfigs": {"watchEndpointMusicConfig": {"musicVideoType": "MUSIC_VIDEO_TYPE_ATV"}}}}
								}
							}
						]
					}
				}
			}`))
		}))
		defer srv.Close()

		var history []model.Video
		for i := 1; i <= 6; i++ {
			history = append(history, model.Video{
				ID:       "locMusic00" + strconv.Itoa(i),
				Title:    "Local Track " + strconv.Itoa(i),
				Channel:  "Artist " + strconv.Itoa(i),
				Duration: "03:45",
			})
		}
		liked := []model.Video{
			{ID: "likedSoda01", Title: "De Música Ligera", Channel: "Soda Stereo", Duration: "04:49"},
		}
		stats := map[string]store.TrackStat{
			"skipBadTrk1": {VideoID: "skipBadTrk1", Title: "Bad Repeated Track", Artist: "Skipped Artist", Plays: 3, Skips: 4},
			"likedSoda01": {VideoID: "likedSoda01", Title: "De Música Ligera", Artist: "Soda Stereo", Plays: 5, Completes: 5},
		}

		cacheDir := t.TempDir()
		hc := &HybridCatalog{
			Direct: &YTDLPCatalog{CacheDir: cacheDir, CacheTTL: time.Hour, FeedSize: 12},
			LocalMusicProvider: func(context.Context) (LocalMusicData, error) {
				return LocalMusicData{Liked: liked, History: history, Stats: stats}, nil
			},
			LocalMusicBaseURL: srv.URL,
		}

		videos, err := hc.Feed(context.Background(), model.TabMusic)
		if err != nil {
			t.Fatalf("HybridCatalog.Feed(TabMusic): %v", err)
		}
		if len(videos) < 2 {
			t.Fatalf("expected personalized local music feed >= 2 items, got %d (%+v)", len(videos), videos)
		}
		for _, v := range videos {
			if v.ID == "junkMix9999" || v.ID == "skipBadTrk1" {
				t.Fatalf("expected junk/heavily-skipped track filtered from local music feed, got %+v", v)
			}
		}
		if videos[0].ID != "consTrack01" {
			t.Fatalf("expected liked artist multi-seed consensus track consTrack01 ranked #1, got %+v", videos)
		}
	})
}
