package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1E212D")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Background(lipgloss.Color("#14161F")).
			Padding(0, 1)

	playerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00E5FF")).
			Background(lipgloss.Color("#1B2A32")).
			Padding(0, 1)
)

// View renders the complete TUI screen.
func (m Model) View() string {
	if !m.Ready {
		return "\n  Iniciando ytx-cli TUI..."
	}

	// 1. Header
	headerText := fmt.Sprintf(" ytx-cli • %s %s   |   Buscar: [/]   Panel: [Tab]   Salir: [q]",
		m.ActiveTab.Icon(), m.ActiveTab.String())
	header := headerStyle.Width(m.Width).Render(headerText)

	// 2. Main content: Sidebar + Grid
	mainHeight := m.Height - 3 // room for header and footer
	if mainHeight < 10 {
		mainHeight = 10
	}

	sidebarView := RenderSidebar(m.ActiveTab, m.Focus == FocusSidebar, mainHeight)
	gridView := RenderGrid(&m.Grid, m.gridWidth(), mainHeight, m.Focus == FocusGrid)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, gridView)

	// 3. Bottom Player & Status bar
	var bottomBar string
	if m.PlayingVideo != nil {
		playStatus := "▶ REPRODUCIENDO"
		if !m.IsPlaying {
			playStatus = "⏸ PAUSADO"
		}
		playerLine := fmt.Sprintf("%s: %s [%s] • %s",
			playStatus, m.PlayingVideo.Title, m.PlayingVideo.Badge(), m.PlayingVideo.Channel)
		bottomBar = playerStyle.Width(m.Width).Render(playerLine)
	} else {
		bottomBar = footerStyle.Width(m.Width).Render(m.StatusMessage)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		mainContent,
		bottomBar,
	)
}
