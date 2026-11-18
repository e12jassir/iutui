package views

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/e12jassir/iutui/internal/tui/thumbs"
)

var update = flag.Bool("update", false, "update golden files")

func fixedVideos() []model.Video {
	return []model.Video{
		{
			ID:        "vid001",
			Title:     "Soda Stereo - De Música Ligera (En Vivo Estadio River)",
			Channel:   "Soda Stereo Oficial",
			Duration:  "05:18",
			Views:     "142M vistas",
			Published: "hace 5 años",
		},
		{
			ID:      "vid002",
			Title:   "Lofi Hip Hop Radio - Beats to Relax/Study to",
			Channel: "Lofi Girl",
			Views:   "38K viendo",
			IsLive:  true,
		},
		{
			ID:        "vid003",
			Title:     "Go Concurrency Patterns & Context Internals",
			Channel:   "GopherCon",
			Duration:  "42:10",
			Views:     "410K vistas",
			Published: "hace 1 año",
		},
		{
			ID:        "vid004",
			Title:     "Pink Floyd - Time (Pulse Live 1994 Remastered)",
			Channel:   "Pink Floyd",
			Duration:  "07:05",
			Views:     "89M vistas",
			Published: "hace 3 años",
		},
		{
			ID:        "vid005",
			Title:     "Linux Kernel Context Switching Deep Dive",
			Channel:   "Systems Lab",
			Duration:  "28:44",
			Views:     "125K vistas",
			Published: "hace 6 meses",
		},
		{
			ID:        "vid006",
			Title:     "Diomedes Díaz - Tú Eres La Reina (Clásico Vallenato)",
			Channel:   "Codiscos",
			Duration:  "04:52",
			Views:     "64M vistas",
			Published: "hace 8 años",
		},
	}
}

func newTestState(view app.ViewMode, w, h int) *app.State {
	s := app.NewState(app.Dependencies{})
	videos := fixedVideos()
	s.View = view
	s.ActiveTab = model.TabHome
	s.Focus = app.FocusMain
	s.Width = w
	s.Height = h
	s.Ready = true
	s.Feeds[model.TabHome] = videos
	s.SearchQuery = "soda stereo"
	s.SearchResults = videos[:4]
	s.SavedIDs["vid001"] = true
	s.SavedIDs["vid004"] = true
	s.HistoryIDs["vid003"] = true
	s.Subscriptions = []string{"Soda Stereo", "GopherCon", "Pink Floyd"}
	s.Queue = []model.Video{videos[3]}
	s.PlayerStatus = player.Status{
		Title:         videos[0].Title,
		TimePos:       125,
		Duration:      318,
		Paused:        false,
		Volume:        85,
		PlaylistCount: 1,
		IsRunning:     true,
	}
	s.Toast = "★ Guardado: Soda Stereo"
	return s
}

func assertBounds(t *testing.T, out string, maxW, maxH int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > maxH {
		t.Fatalf("line count %d exceeds height %d", len(lines), maxH)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > maxW {
			t.Errorf("line %d width %d exceeds max width %d: %q", i+1, w, maxW, line)
		}
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
		return
	}
	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to generate): %v", goldenPath, err)
	}
	want := string(wantBytes)
	if got != want {
		t.Errorf("golden mismatch for %s:\n--- GOT ---\n%s\n--- WANT ---\n%s", name, got, want)
	}
}

func TestRenderGoldenAndBounds(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	cases := []struct {
		name string
		view app.ViewMode
		w    int
		h    int
	}{
		{name: "home_80x24", view: app.ViewHome, w: 80, h: 24},
		{name: "search_80x24", view: app.ViewSearch, w: 80, h: 24},
		{name: "home_120x36", view: app.ViewHome, w: 120, h: 36},
		{name: "search_120x36", view: app.ViewSearch, w: 120, h: 36},
		{name: "home_156x45", view: app.ViewHome, w: 156, h: 45},
		{name: "search_156x45", view: app.ViewSearch, w: 156, h: 45},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestState(tc.view, tc.w, tc.h)
			defer s.Close()

			out := Render(s, th, tr)
			if out == "" {
				t.Fatal("rendered output is empty")
			}
			assertBounds(t, out, tc.w, tc.h)
			assertGolden(t, tc.name, out)
		})
	}
}

func TestRenderResponsiveSidebarRules(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("full_sidebar_at_140_home", func(t *testing.T) {
		s := newTestState(app.ViewHome, 140, 36)
		defer s.Close()
		out := Render(s, th, tr)
		assertBounds(t, out, 140, 36)
		for _, want := range []string{"PRINCIPAL", "BIBLIOTECA", "SUSCRIPCIONES"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected full sidebar section %q in output", want)
			}
		}
	})

	t.Run("full_sidebar_kept_in_search_at_156", func(t *testing.T) {
		s := newTestState(app.ViewSearch, 156, 45)
		defer s.Close()
		out := Render(s, th, tr)
		assertBounds(t, out, 156, 45)
		if !strings.Contains(out, "SUSCRIPCIONES") {
			t.Error("expected unified full sidebar to remain visible in ViewSearch at 156")
		}
		if !strings.Contains(out, "│") {
			t.Error("expected vertical divider between sidebar and main")
		}
	})

	t.Run("no_sidebar_under_100", func(t *testing.T) {
		s := newTestState(app.ViewHome, 80, 24)
		defer s.Close()
		out := Render(s, th, tr)
		assertBounds(t, out, 80, 24)
		if strings.Contains(out, "SUSCRIPCIONES") {
			t.Error("did not expect full sidebar under width 100")
		}
	})
}

func TestRenderMinimumSizeGuard(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	cases := []struct {
		name string
		w, h int
	}{
		{name: "zero", w: 0, h: 0},
		{name: "narrow_79x24", w: 79, h: 24},
		{name: "short_80x23", w: 80, h: 23},
		{name: "tiny_40x12", w: 40, h: 12},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestState(app.ViewHome, tc.w, tc.h)
			defer s.Close()

			out := Render(s, th, tr)
			if tc.w > 0 && tc.h > 0 {
				assertBounds(t, out, tc.w, tc.h)
				if !strings.Contains(out, "80x24") {
					t.Errorf("expected minimum size message mentioning 80x24, got:\n%s", out)
				}
			}
		})
	}
}

func TestRenderHelpModalAndNilSafety(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("help_overlay", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowHelp = true

		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		if !strings.Contains(out, "Atajos de teclado") {
			t.Error("expected help modal title in output")
		}
	})

	t.Run("nil_state", func(t *testing.T) {
		if got := Render(nil, th, tr); got != "" {
			t.Errorf("expected empty string for nil state, got %q", got)
		}
	})

	t.Run("nil_renderer_fallback", func(t *testing.T) {
		s := newTestState(app.ViewHome, 80, 24)
		defer s.Close()
		out := Render(s, th, nil)
		assertBounds(t, out, 80, 24)
		if !strings.Contains(out, "vid001") {
			t.Error("expected fallback TextRenderer to render video ID")
		}
	})
}

func TestRenderCollapsiblePlayerBarAndToastFallback(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	s := newTestState(app.ViewHome, 120, 36)
	defer s.Close()
	s.PlayerStatus = player.Status{IsRunning: false}
	s.PlayLoading = false
	s.Toast = "★ Guardado: Soda Stereo"

	out := Render(s, th, tr)
	assertBounds(t, out, 120, 36)
	if strings.Contains(out, "Reproductor (mpv IPC") {
		t.Fatalf("expected collapsed bottom player panel to be omitted when idle, got:\n%s", out)
	}
	if !strings.Contains(out, "Guardado: Soda Stereo") {
		t.Fatalf("expected toast to be visible on chip bar when player bar collapsed, got:\n%s", out)
	}
}

func TestRenderLoadingAnimations(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("home_sync_animation", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.PlayerStatus = player.Status{IsRunning: false}
		s.Feeds[model.TabHome] = nil
		s.Syncing = true
		s.AnimFrame = 0
		out0 := Render(s, th, tr)
		assertBounds(t, out0, 120, 36)
		if !strings.Contains(out0, "Cargando videos...") {
			t.Fatalf("expected loading spinner title in home sync, got:\n%s", out0)
		}
		if strings.Contains(out0, "━━●━━") {
			t.Fatalf("expected no bouncing wave bar in home sync, got:\n%s", out0)
		}
		if !strings.Contains(out0, "[⠋]") {
			t.Fatalf("expected animated top badge [⠋], got:\n%s", out0)
		}

		s.AnimFrame = 3
		out3 := Render(s, th, tr)
		assertBounds(t, out3, 120, 36)
		if out0 == out3 {
			t.Fatal("expected Syncing output to change across AnimFrame 0 vs 3")
		}
	})

	t.Run("search_pending_animation", func(t *testing.T) {
		s := newTestState(app.ViewSearch, 120, 36)
		defer s.Close()
		s.PlayerStatus = player.Status{IsRunning: false}
		s.SearchResults = nil
		s.SearchPending = true
		s.AnimFrame = 0
		out0 := Render(s, th, tr)
		assertBounds(t, out0, 120, 36)
		if !strings.Contains(out0, "Buscando") {
			t.Fatalf("expected loading spinner title in search pending, got:\n%s", out0)
		}
		if strings.Contains(out0, "━━●━━") {
			t.Fatalf("expected no bouncing wave bar in search pending, got:\n%s", out0)
		}

		s.AnimFrame = 3
		out3 := Render(s, th, tr)
		if out0 == out3 {
			t.Fatal("expected SearchPending output to change across AnimFrame 0 vs 3")
		}
	})

	t.Run("play_loading_player_bar", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.PlayerStatus = player.Status{IsRunning: false}
		s.PlayLoading = true
		s.PlayLoadingTitle = "Soda Stereo - De Música Ligera"
		s.AnimFrame = 0
		out0 := Render(s, th, tr)
		assertBounds(t, out0, 120, 36)
		if !strings.Contains(out0, "Abriendo en mpv: Soda Stereo") || !strings.Contains(out0, "Abriendo...") {
			t.Fatalf("expected instant PlayLoading player bar text, got:\n%s", out0)
		}

		s.AnimFrame = 3
		out3 := Render(s, th, tr)
		if out0 == out3 {
			t.Fatal("expected PlayLoading player bar output to change across AnimFrame 0 vs 3")
		}
	})
}

func TestRenderStillWatchingAndSettingsAutoplay(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("still_watching_overlay", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowStillWatching = true
		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		if !strings.Contains(out, "¿Todavía estás viendo?") {
			t.Fatalf("expected Still Watching prompt in output, got:\n%s", out)
		}
	})

	t.Run("settings_and_help_autoplay", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowSettings = true
		s.Autoplay = true
		outOn := Render(s, th, tr)
		assertBounds(t, outOn, 120, 36)
		if !strings.Contains(outOn, "Autoplay [A]:") || !strings.Contains(outOn, "ACTIVADO") {
			t.Fatalf("expected Autoplay [A]: ACTIVADO in settings overlay, got:\n%s", outOn)
		}
		s.Autoplay = false
		outOff := Render(s, th, tr)
		if !strings.Contains(outOff, "DESACTIVADO") {
			t.Fatalf("expected DESACTIVADO in settings overlay, got:\n%s", outOff)
		}

		s.ShowSettings = false
		s.ShowHelp = true
		outHelp := Render(s, th, tr)
		assertBounds(t, outHelp, 120, 36)
		if !strings.Contains(outHelp, "Activar/desactivar Autoplay") {
			t.Fatalf("expected Autoplay shortcut in help overlay, got:\n%s", outHelp)
		}
	})
}

func TestRenderChannelChaptersLyricsAndThemes(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("channel_view_and_loading", func(t *testing.T) {
		s := newTestState(app.ViewChannel, 120, 36)
		defer s.Close()
		s.ChannelTitle = "Soda Stereo"
		s.ChannelVideos = fixedVideos()[:2]
		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		if !strings.Contains(out, "Canal: Soda Stereo") || !strings.Contains(out, "De Música Ligera") {
			t.Fatalf("expected channel title and videos in output, got:\n%s", out)
		}

		s.ChannelVideos = nil
		s.ChannelPending = true
		outLoad := Render(s, th, tr)
		assertBounds(t, outLoad, 120, 36)
		if !strings.Contains(outLoad, "Cargando canal...") {
			t.Fatalf("expected channel loading box, got:\n%s", outLoad)
		}
	})

	t.Run("chapters_overlay", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowChapters = true
		s.Chapters = []player.Chapter{
			{Title: "Intro acústica", Time: 0},
			{Title: "Solo de guitarra", Time: 135},
		}
		s.ChapterIdx = 1
		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		if !strings.Contains(out, "Capítulos del video") || !strings.Contains(out, "▶ 02:15  Solo de guitarra") {
			t.Fatalf("expected active chapter highlight, got:\n%s", out)
		}
	})

	t.Run("lyrics_panel_and_overlay_synced_line", func(t *testing.T) {
		for _, w := range []int{120, 88} {
			s := newTestState(app.ViewHome, w, 36)
			s.ShowLyrics = true
			s.LyricsTrack = "De Música Ligera"
			s.PlayerStatus.TimePos = 15
			s.Lyrics = lyrics.TrackLyrics{
				TrackName:  "De Música Ligera",
				ArtistName: "Soda Stereo",
				Synced: []lyrics.Line{
					{Time: 5, Text: "Ella durmió"},
					{Time: 12, Text: "Al calor de las masas"},
					{Time: 22, Text: "Y yo desperté"},
				},
			}
			out := Render(s, th, tr)
			s.Close()
			assertBounds(t, out, w, 36)
			if !strings.Contains(out, "Letras") || !strings.Contains(out, "▶ Al calor de las masas") {
				t.Fatalf("width=%d expected active synced lyric line, got:\n%s", w, out)
			}
		}
	})

	t.Run("catppuccin_and_no_color_bounds", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ThemeName = "catppuccin"
		outCat := Render(s, th, tr)
		assertBounds(t, outCat, 120, 36)

		t.Setenv("NO_COLOR", "1")
		outNoColor := Render(s, th, tr)
		assertBounds(t, outNoColor, 120, 36)
	})
}
