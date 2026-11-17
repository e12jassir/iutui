package views

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/tui/theme"
	"github.com/e12jassir/ytx/internal/tui/thumbs"
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
