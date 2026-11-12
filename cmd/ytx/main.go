package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/catalog"
	"github.com/e12jassir/ytx/internal/platform"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/store"
	"github.com/e12jassir/ytx/internal/tui"
	"github.com/e12jassir/ytx/internal/tui/theme"
	"github.com/e12jassir/ytx/internal/tui/thumbs"
	"github.com/e12jassir/ytx/internal/tui/views"
)

type nativeThumbs struct {
	backend thumbs.Backend
}

func newNativeThumbs() nativeThumbs {
	b := thumbs.DetectBackend(
		os.Getenv("YT_PREVIEW"),
		os.Getenv("KITTY_WINDOW_ID"),
		os.Getenv("TERM"),
		os.Getenv("TERM_PROGRAM"),
	)
	return nativeThumbs{backend: b}
}

func (n nativeThumbs) Render(id string, width, height int) string {
	if n.backend == thumbs.BackendText {
		return thumbs.NewTextRenderer().Render(id, width, height)
	}
	return tui.RenderThumbnailANSI(id, width, height)
}

func (n nativeThumbs) Prefetch(id string) {
	if n.backend != thumbs.BackendText {
		tui.FetchThumbnailAsync(id)
	}
}

func (n nativeThumbs) Backend() thumbs.Backend {
	return n.backend
}

type teaModel struct {
	state  *app.State
	theme  theme.Theme
	thumbs thumbs.Renderer
}

func (m teaModel) Init() tea.Cmd {
	return m.state.Init()
}

func (m teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.state.Update(msg)
	m.state = next
	return m, cmd
}

func (m teaModel) View() string {
	return views.Render(m.state, m.theme, m.thumbs)
}

func main() {
	paths := platform.ResolvePaths()
	_, _ = platform.MigrateFromYT(paths)

	cat := catalog.NewBashCatalog(paths.CacheDir, paths.StateDir, "")
	st := store.NewTSVStore(paths.StateDir)
	eng := player.NewIPCClient()
	defer eng.Close()

	state := app.NewState(app.Dependencies{
		Catalog: cat,
		Engine:  eng,
		Store:   st,
	})

	p := tea.NewProgram(
		teaModel{
			state:  state,
			theme:  theme.DefaultTheme(),
			thumbs: newNativeThumbs(),
		},
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error ejecutando ytx TUI: %v\n", err)
		os.Exit(1)
	}
}
