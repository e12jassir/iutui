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
