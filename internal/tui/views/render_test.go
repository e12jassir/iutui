package views

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/download"
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
	s := app.NewState(app.Dependencies{AppVersion: "1.0.0"})
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

		s.ThemeName = "terminal"
		outTerminal := Render(s, th, tr)
		assertBounds(t, outTerminal, 120, 36)

		s.ShowSettings = true
		outSettings := Render(s, th, tr)
		assertBounds(t, outSettings, 120, 36)
		for _, want := range []string{"APARIENCIA & TERMINAL", "Tema interfaz", "terminal (Nativo)", "REPRODUCCIÓN & CONSUMO"} {
			if !strings.Contains(outSettings, want) {
				t.Fatalf("expected %q in upgraded settings overlay, got:\n%s", want, outSettings)
			}
		}
		s.ShowSettings = false

		t.Setenv("NO_COLOR", "1")
		outNoColor := Render(s, th, tr)
		assertBounds(t, outNoColor, 120, 36)
	})

	t.Run("lyrics_word_wrap_and_plain_scroll", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowLyrics = true
		s.LyricsTrack = "Wrap Test"
		s.PlayerStatus.TimePos = 10
		s.PlayerStatus.Duration = 100
		longVerse := "Esta noche vamos a cantar toda la cancion completa sin cortarse"
		s.Lyrics = lyrics.TrackLyrics{
			TrackName:  "Track",
			ArtistName: "Artist",
			Synced: []lyrics.Line{
				{Time: 5, Text: longVerse},
				{Time: 25, Text: "Siguiente estrofa"},
			},
		}
		outSynced := Render(s, th, tr)
		assertBounds(t, outSynced, 120, 36)
		for _, part := range []string{
			"▶ Esta noche vamos a cantar toda",
			"la cancion completa sin",
			"  cortarse",
			"Siguiente estrofa",
		} {
			if !strings.Contains(outSynced, part) {
				t.Fatalf("expected long synced lyric to word-wrap cleanly (missing %q), got:\n%s", part, outSynced)
			}
		}

		var plain []string
		for i := 1; i <= 35; i++ {
			plain = append(plain, fmt.Sprintf("Verso plain numero %02d con palabras completas", i))
		}
		s.Lyrics = lyrics.TrackLyrics{TrackName: "Track", ArtistName: "Artist", Plain: plain}
		s.PlayerStatus.TimePos = 98
		outPlain := Render(s, th, tr)
		assertBounds(t, outPlain, 120, 36)
		if !strings.Contains(outPlain, "Verso plain numero 35") {
			t.Fatalf("expected plain lyrics near end of track to auto-scroll to line 35, got:\n%s", outPlain)
		}
	})
}

func TestRenderBrandVersionAndNoItemCounter(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	s := newTestState(app.ViewHome, 156, 45)
	defer s.Close()
	s.AppVersion = "0.4.6"

	outHome := Render(s, th, tr)
	assertBounds(t, outHome, 156, 45)
	if !strings.Contains(outHome, "iutui · v0.4.6") {
		t.Fatalf("expected version badge 'iutui · v0.4.6' in wide top bar, got:\n%s", outHome)
	}
	if strings.Contains(outHome, "· tui") {
		t.Fatalf("unexpected legacy '· tui' label in top bar, got:\n%s", outHome)
	}
	if strings.Contains(outHome, "(1/6)") || strings.Contains(outHome, "(1/") {
		t.Fatalf("expected Home panel title without item counter, got:\n%s", outHome)
	}

	s.View = app.ViewSearch
	outSearch := Render(s, th, tr)
	assertBounds(t, outSearch, 156, 45)
	if !strings.Contains(outSearch, "Búsqueda: soda stereo") {
		t.Fatalf("expected clean search panel title, got:\n%s", outSearch)
	}
	if strings.Contains(outSearch, "(1/4)") {
		t.Fatalf("expected Search panel title without item counter, got:\n%s", outSearch)
	}
}

func TestRenderPhase2Features(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("search_suggestions_strip_only_when_focused", func(t *testing.T) {
		s := newTestState(app.ViewHome, 140, 42)
		defer s.Close()
		s.SearchSuggestions = []string{"pink floyd live", "pink floyd pulse"}
		s.SearchDraft = "pink"
		s.SuggestIdx = 0

		s.Focus = app.FocusMain
		outUnfocused := Render(s, th, tr)
		assertBounds(t, outUnfocused, 140, 42)
		if strings.Contains(outUnfocused, "pink floyd live") {
			t.Fatalf("did not expect suggestions strip when FocusMain, got:\n%s", outUnfocused)
		}

		s.Focus = app.FocusSearch
		outFocused := Render(s, th, tr)
		assertBounds(t, outFocused, 140, 42)
		if !strings.Contains(outFocused, "Sugerencias") || !strings.Contains(outFocused, "pink floyd live") {
			t.Fatalf("expected suggestions strip when FocusSearch, got:\n%s", outFocused)
		}

		s.SearchDraft = ""
		outEmpty := Render(s, th, tr)
		assertBounds(t, outEmpty, 140, 42)
		if strings.Contains(outEmpty, "Búsquedas recientes") {
			t.Fatalf("did not expect 'Búsquedas recientes' when SearchDraft empty, got:\n%s", outEmpty)
		}
		if strings.Contains(outEmpty, "pink floyd live") {
			t.Fatalf("did not expect suggestions strip when SearchDraft empty, got:\n%s", outEmpty)
		}
	})

	t.Run("queue_overlay_empty_and_populated", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowQueue = true

		outEmpty := Render(s, th, tr)
		assertBounds(t, outEmpty, 120, 36)
		if !strings.Contains(outEmpty, "Cola de reproducción (0)") || !strings.Contains(outEmpty, "Cola vacía") {
			t.Fatalf("expected empty queue overlay message, got:\n%s", outEmpty)
		}

		s.MPVPlaylist = []player.PlaylistItem{
			{Index: 0, Title: "First Track", Current: true},
			{Index: 1, Title: "Second Track"},
		}
		s.QueueIdx = 1
		outPop := Render(s, th, tr)
		assertBounds(t, outPop, 120, 36)
		if !strings.Contains(outPop, "Cola de reproducción (2)") || !strings.Contains(outPop, "First Track") || !strings.Contains(outPop, "Second Track") {
			t.Fatalf("expected populated queue overlay items, got:\n%s", outPop)
		}
	})

	t.Run("player_speed_and_loop_badges", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.PlayerStatus = player.Status{
			IsRunning: true,
			Title:     "Soda Stereo - De Música Ligera",
			Volume:    75,
			Speed:     1.25,
			LoopFile:  true,
		}
		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		if !strings.Contains(out, "1.25x") || !strings.Contains(out, "↻") {
			t.Fatalf("expected speed '1.25x' and loop '↻' in player bar title, got:\n%s", out)
		}
	})

	t.Run("download_progress_badge_on_card", func(t *testing.T) {
		s := newTestState(app.ViewHome, 140, 42)
		defer s.Close()
		s.DownloadProgress["vid003"] = download.Item{
			Video:    model.Video{ID: "vid003"},
			Status:   download.StatusDownloading,
			Progress: 62,
			Speed:    "3.1MiB/s",
		}
		out := Render(s, th, tr)
		assertBounds(t, out, 140, 42)
		if !strings.Contains(out, "62%") || !strings.Contains(out, "3.1MiB/s") {
			t.Fatalf("expected live download progress badge '62%% · 3.1MiB/s' on card, got:\n%s", out)
		}
	})

	t.Run("help_overlay_phase2_shortcuts", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.ShowHelp = true
		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		for _, want := range []string{"Ver Cola", "Velocidad", "Repetir pista", "Me gusta"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in help overlay, got:\n%s", want, out)
			}
		}
	})
}

func TestRenderSearchCursorAndWindowing(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()
	pal := resolveRenderPalette("youtube-dark")

	t.Run("empty_draft_placeholder_and_query_hint", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.Focus = app.FocusSearch
		s.SearchQuery = ""
		s.SearchDraft = ""

		outPlaceholder := Render(s, th, tr)
		assertBounds(t, outPlaceholder, 120, 36)
		if !strings.Contains(outPlaceholder, "Buscar en YouTube...") || !strings.Contains(outPlaceholder, "█") {
			t.Fatalf("expected empty search placeholder and cursor, got:\n%s", outPlaceholder)
		}

		s.SearchQuery = "soda stereo"
		outHint := Render(s, th, tr)
		assertBounds(t, outHint, 120, 36)
		if !strings.Contains(outHint, "█") || !strings.Contains(outHint, "soda stereo") {
			t.Fatalf("expected cursor + dimmed query hint, got:\n%s", outHint)
		}
	})

	t.Run("cursor_end_start_and_middle_highlight", func(t *testing.T) {
		prevProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(0)
		defer lipgloss.SetColorProfile(prevProfile)

		lineEnd := renderSearchInputLine("pink floyd", 10, 40, pal)
		if !strings.Contains(lineEnd, "pink floyd█") {
			t.Fatalf("expected block cursor at end, got %q", lineEnd)
		}

		lineStart := renderSearchInputLine("pink floyd", 0, 40, pal)
		if strings.Contains(lineStart, "█") {
			t.Fatalf("did not expect trailing block cursor when cursor=0, got %q", lineStart)
		}
		if (!strings.Contains(lineStart, ";7;") && !strings.Contains(lineStart, "[7;")) || !strings.Contains(lineStart, "mp\x1b[0m") {
			t.Fatalf("expected reverse-video highlight on first rune 'p' when cursor=0, got %q", lineStart)
		}

		lineMid := renderSearchInputLine("pink floyd", 4, 40, pal)
		if strings.Contains(lineMid, "█") {
			t.Fatalf("did not expect trailing block cursor when cursor=4, got %q", lineMid)
		}
		if (!strings.Contains(lineMid, ";7;") && !strings.Contains(lineMid, "[7;")) || !strings.Contains(lineMid, "m \x1b[0m") {
			t.Fatalf("expected reverse-video highlight on space at index 4, got %q", lineMid)
		}
	})

	t.Run("long_draft_horizontal_windowing", func(t *testing.T) {
		longDraft := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
		avail := 24

		lineStart := renderSearchInputLine(longDraft, 2, avail, pal)
		if w := lipgloss.Width(lineStart); w > avail {
			t.Fatalf("start window width %d > %d", w, avail)
		}
		if !strings.Contains(lineStart, "012") || !strings.Contains(lineStart, "…") {
			t.Fatalf("expected prefix window with trailing ellipsis, got %q", lineStart)
		}

		lineMid := renderSearchInputLine(longDraft, 30, avail, pal)
		if w := lipgloss.Width(lineMid); w > avail {
			t.Fatalf("mid window width %d > %d", w, avail)
		}
		if !strings.Contains(lineMid, string(longDraft[30])) || strings.Count(lineMid, "…") != 2 {
			t.Fatalf("expected mid window around rune %q with both ellipses, got %q", string(longDraft[30]), lineMid)
		}

		lineEnd := renderSearchInputLine(longDraft, len([]rune(longDraft)), avail, pal)
		if w := lipgloss.Width(lineEnd); w > avail {
			t.Fatalf("end window width %d > %d", w, avail)
		}
		if !strings.Contains(lineEnd, "…") || !strings.Contains(lineEnd, "XYZ█") {
			t.Fatalf("expected tail window with leading ellipsis and block cursor, got %q", lineEnd)
		}

		s := newTestState(app.ViewHome, 80, 24)
		defer s.Close()
		s.Focus = app.FocusSearch
		s.SearchDraft = "pink floyd"
		s.SearchCursor = 4
		out := Render(s, th, tr)
		assertBounds(t, out, 80, 24)
	})
}

func TestRenderSettingsStudioOverlay(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	for _, sz := range [][2]int{{80, 24}, {120, 36}, {156, 45}} {
		s := newTestState(app.ViewHome, sz[0], sz[1])
		s.ShowSettings = true
		out := Render(s, th, tr)
		s.Close()
		assertBounds(t, out, sz[0], sz[1])
		for _, want := range []string{"Configuración", "◆", "1/16", "ⓘ", "[?] Atajos"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%dx%d: expected %q in settings studio overlay, got:\n%s", sz[0], sz[1], want, out)
			}
		}
	}

	t.Run("scrolled_to_cookies_shows_desc_and_indicator", func(t *testing.T) {
		for _, sz := range [][2]int{{80, 24}, {120, 36}} {
			s := newTestState(app.ViewHome, sz[0], sz[1])
			s.ShowSettings = true
			s.SettingsIdx = app.SettingIdxCookies
			out := Render(s, th, tr)
			s.Close()
			assertBounds(t, out, sz[0], sz[1])
			for _, want := range []string{"Cookies navegador", "16/16", "Usa cookies de Zen/Firefox"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%dx%d: expected %q in scrolled settings overlay, got:\n%s", sz[0], sz[1], want, out)
				}
			}
			if sz[1] == 24 && !strings.Contains(out, "↑") {
				t.Fatalf("80x24: expected scroll-up indicator ↑ when scrolled to bottom, got:\n%s", out)
			}
		}
	})
}

func TestRenderHelpStudioOverlay(t *testing.T) {
	th := theme.DefaultTheme()
	tr := thumbs.NewTextRenderer()

	t.Run("wide_two_column_mode", func(t *testing.T) {
		s := newTestState(app.ViewHome, 120, 36)
		defer s.Close()
		s.AppVersion = "0.4.6"
		s.ShowHelp = true
		out := Render(s, th, tr)
		assertBounds(t, out, 120, 36)
		for _, want := range []string{
			"Atajos de teclado",
			"v0.4.6",
			"│",
			"[S] Configuración",
			"NAVEGACIÓN & VISTAS",
			"BIBLIOTECA & COLA",
			"REPRODUCCIÓN (MPV)",
			"SISTEMA & AJUSTES",
			"Activar/desactivar Autoplay",
			"SponsorBlock · Cambiar tema",
			"Ver Cola",
			"Velocidad",
			"Repetir pista",
			"Me gusta",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in 2-column help overlay, got:\n%s", want, out)
			}
		}
		helpOnly := renderHelpOverlay(s, 120, 36, th, resolveRenderPalette(s.ThemeName))
		if strings.Contains(helpOnly, "…") {
			t.Fatalf("expected zero ellipsis truncation in wide 120x36 help overlay, got:\n%s", helpOnly)
		}
	})

	t.Run("compact_single_column_80x24", func(t *testing.T) {
		s := newTestState(app.ViewHome, 80, 24)
		defer s.Close()
		s.ShowHelp = true
		out := Render(s, th, tr)
		assertBounds(t, out, 80, 24)
		for _, want := range []string{"Atajos de teclado", "[S] Configuración", "NAVEGACIÓN & VISTAS"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in compact help overlay, got:\n%s", want, out)
			}
		}
	})
}
