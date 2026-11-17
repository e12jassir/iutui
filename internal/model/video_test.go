package model

import "testing"

func TestTabStringAndIcon(t *testing.T) {
	cases := []struct {
		name     string
		tab      Tab
		wantName string
		wantIcon string
	}{
		{"Home", TabHome, "Inicio", "🏠"},
		{"Music", TabMusic, "Música", "🎵"},
		{"Subscriptions", TabSubscriptions, "Suscripciones", "🔔"},
		{"Downloads", TabDownloads, "Descargas", "📥"},
		{"History", TabHistory, "Historial", "🕒"},
		{"Unknown", Tab(99), "Desconocido", "•"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tab.String(); got != tc.wantName {
				t.Errorf("Tab.String() = %q, want %q", got, tc.wantName)
			}
			if got := tc.tab.Icon(); got != tc.wantIcon {
				t.Errorf("Tab.Icon() = %q, want %q", got, tc.wantIcon)
			}
		})
	}
}

func TestVideoBadgeAndString(t *testing.T) {
	cases := []struct {
		name      string
		video     Video
		wantBadge string
		wantStr   string
	}{
		{
			name:      "Live stream",
			video:     Video{ID: "live123", Title: "News", Channel: "CNN", IsLive: true, Duration: "00:00"},
			wantBadge: "🔴 EN VIVO",
			wantStr:   "[live123] News (CNN)",
		},
		{
			name:      "Regular video with duration",
			video:     Video{ID: "vid1234", Title: "Go Tips", Channel: "Dev", Duration: "12:34"},
			wantBadge: "12:34",
			wantStr:   "[vid1234] Go Tips (Dev)",
		},
		{
			name:      "Missing duration fallback",
			video:     Video{ID: "vid0000", Title: "Mystery", Channel: "Unknown"},
			wantBadge: "--:--",
			wantStr:   "[vid0000] Mystery (Unknown)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.video.Badge(); got != tc.wantBadge {
				t.Errorf("Badge() = %q, want %q", got, tc.wantBadge)
			}
			if got := tc.video.String(); got != tc.wantStr {
				t.Errorf("String() = %q, want %q", got, tc.wantStr)
			}
		})
	}
}

func TestMixAndPlaylistHelpers(t *testing.T) {
	t.Run("IsMixID", func(t *testing.T) {
		cases := []struct {
			name string
			id   string
			want bool
		}{
			{"video mix RD11", "RDdQw4w9WgXcQ", true},
			{"trimmed mix", "  RDCLAK5uy_abc  ", true},
			{"bare RD prefix", "RD", false},
			{"bare RD whitespace", " RD ", false},
			{"regular playlist", "PL12345", false},
			{"regular video", "dQw4w9WgXcQ", false},
			{"empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := IsMixID(tc.id); got != tc.want {
					t.Errorf("IsMixID(%q) = %v, want %v", tc.id, got, tc.want)
				}
			})
		}
	})

	t.Run("IsPlaylistID", func(t *testing.T) {
		cases := []struct {
			name string
			id   string
			want bool
		}{
			{"RD mix", "RDdQw4w9WgXcQ", true},
			{"RDCLAK mix", "RDCLAK5uy_xyz", true},
			{"PL playlist", "PLrAXtmErZgOeiKm4sgNOknGvNjby9efdf", true},
			{"OLAK5uy_ album", "OLAK5uy_k12345", true},
			{"UU uploads", "UU_x5XG1OV2P6uZZ5FSM9Ttw", true},
			{"FL favorites", "FL12345", true},
			{"LP playlist", "LP12345", true},
			{"trimmed PL", "  PL123  ", true},
			{"bare PL", "PL", false},
			{"bare OLAK5uy_", "OLAK5uy_", false},
			{"bare UU", "UU", false},
			{"bare FL", "FL", false},
			{"bare LP", "LP", false},
			{"regular video", "dQw4w9WgXcQ", false},
			{"empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := IsPlaylistID(tc.id); got != tc.want {
					t.Errorf("IsPlaylistID(%q) = %v, want %v", tc.id, got, tc.want)
				}
			})
		}
	})

	t.Run("MixSeedVideoID", func(t *testing.T) {
		cases := []struct {
			name string
			id   string
			want string
		}{
			{"valid RD + 11 chars", "RDdQw4w9WgXcQ", "dQw4w9WgXcQ"},
			{"trimmed RD + 11 chars", "  RDdQw4w9WgXcQ  ", "dQw4w9WgXcQ"},
			{"longer RDCLAK playlist", "RDCLAK5uy_k1234567890", ""},
			{"shorter RD", "RD12345", ""},
			{"regular playlist", "PL12345678901", ""},
			{"regular video", "dQw4w9WgXcQ", ""},
			{"empty", "", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := MixSeedVideoID(tc.id); got != tc.want {
					t.Errorf("MixSeedVideoID(%q) = %q, want %q", tc.id, got, tc.want)
				}
			})
		}
	})

	t.Run("BuildTargetURL", func(t *testing.T) {
		cases := []struct {
			name          string
			id            string
			asMix         bool
			wantURL       string
			wantPlaylist  bool
			wantSeedVideo string
		}{
			{
				name:          "regular video",
				id:            "dQw4w9WgXcQ",
				asMix:         false,
				wantURL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
				wantPlaylist:  false,
				wantSeedVideo: "dQw4w9WgXcQ",
			},
			{
				name:          "regular video as mix",
				id:            "  dQw4w9WgXcQ  ",
				asMix:         true,
				wantURL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ",
				wantPlaylist:  true,
				wantSeedVideo: "dQw4w9WgXcQ",
			},
			{
				name:          "RD seed mix",
				id:            "  RDdQw4w9WgXcQ ",
				asMix:         false,
				wantURL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ",
				wantPlaylist:  true,
				wantSeedVideo: "dQw4w9WgXcQ",
			},
			{
				name:          "RDCLAK playlist without 11-char seed",
				id:            "RDCLAK5uy_abc123",
				asMix:         true,
				wantURL:       "https://www.youtube.com/playlist?list=RDCLAK5uy_abc123",
				wantPlaylist:  true,
				wantSeedVideo: "",
			},
			{
				name:          "PL playlist",
				id:            "PLxyz789",
				asMix:         false,
				wantURL:       "https://www.youtube.com/playlist?list=PLxyz789",
				wantPlaylist:  true,
				wantSeedVideo: "",
			},
			{
				name:          "empty id with asMix",
				id:            "   ",
				asMix:         true,
				wantURL:       "https://www.youtube.com/watch?v=",
				wantPlaylist:  false,
				wantSeedVideo: "",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				gotURL, gotPlaylist, gotSeed := BuildTargetURL(tc.id, tc.asMix)
				if gotURL != tc.wantURL || gotPlaylist != tc.wantPlaylist || gotSeed != tc.wantSeedVideo {
					t.Errorf("BuildTargetURL(%q, %v) = (%q, %v, %q), want (%q, %v, %q)",
						tc.id, tc.asMix, gotURL, gotPlaylist, gotSeed, tc.wantURL, tc.wantPlaylist, tc.wantSeedVideo)
				}
			})
		}
	})
}
