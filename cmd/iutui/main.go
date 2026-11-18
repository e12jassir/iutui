package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/tui/theme"
	"github.com/e12jassir/ytx/internal/tui/thumbs"
	"github.com/e12jassir/ytx/internal/tui/views"
)

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
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "ytx: %v\n", err)
		os.Exit(1)
	}
}
