package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/player"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#13151F")).
			Padding(0, 1)

	searchBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00E5FF")).
			Background(lipgloss.Color("#1B2236")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#00E5FF")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8890A6")).
			Background(lipgloss.Color("#10121A")).
			Padding(0, 1)

	nowPlayingContainer = lipgloss.NewStyle().
				Background(lipgloss.Color("#131622")).
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
				BorderForeground(lipgloss.Color("#262A3B")).
				Padding(0, 1)

	nowPlayingTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF"))

	nowPlayingStatus = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00E5FF"))

	progressTimeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6C728A"))

	progressBarFilledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF2A55"))
)

// View renders the complete modern YouTube TUI screen.
func (m Model) View() string {
	if !m.Ready {
		return "\n  ⚡ Cargando ytx-cli Modern TUI..."
	}

	// 1. Header or Search Omnibar
	var header string
	if m.Searching {
		header = searchBarStyle.Width(m.Width - 2).Render(fmt.Sprintf("🔍 Buscar en YouTube: %s█", m.SearchQuery))
	} else {
		headerText := fmt.Sprintf(" ▶ YTX-CLI • %s %s   |   [/] Buscar   [Tab] Paneles   [r] Recargar   [q] Salir",
			m.ActiveTab.Icon(), m.ActiveTab.String())
		header = headerStyle.Width(m.Width).Render(headerText)
	}

	// 2. Main content area: Sidebar + Card Grid
	bottomBarHeight := 2
	if m.PlayerStatus.IsRunning && m.PlayerStatus.Title != "" {
		bottomBarHeight = 3
	}

	mainHeight := m.Height - bottomBarHeight - 1
	if mainHeight < 10 {
		mainHeight = 10
	}

	sidebarView := RenderSidebar(m.ActiveTab, m.Focus == FocusSidebar, mainHeight)
	gridView := RenderGrid(&m.Grid, m.gridWidth(), mainHeight, m.Focus == FocusGrid)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, gridView)

	// 3. Bottom persistent Now Playing bar
	var bottomBar string
	if m.PlayerStatus.IsRunning && m.PlayerStatus.Title != "" {
		stText := "▶ REPRODUCIENDO"
		if m.PlayerStatus.Paused {
			stText = "⏸ PAUSADO"
		}

		timePos := player.FormatTime(m.PlayerStatus.TimePos)
		duration := player.FormatTime(m.PlayerStatus.Duration)

		barWidth := m.Width - len(timePos) - len(duration) - 40
		if barWidth < 10 {
			barWidth = 10
		}
		if barWidth > 50 {
			barWidth = 50
		}
		barStr := player.ProgressBar(m.PlayerStatus.TimePos, m.PlayerStatus.Duration, barWidth)

		titleShort := truncate(m.PlayerStatus.Title, 45)
		line1 := fmt.Sprintf("%s  %s", nowPlayingStatus.Render(stText), nowPlayingTitle.Render(titleShort))
		line2 := fmt.Sprintf("%s %s %s   [Espacio] Pausa  [[ / ]] ±10s",
			progressTimeStyle.Render(timePos),
			progressBarFilledStyle.Render(barStr),
			progressTimeStyle.Render(duration),
		)

		playerBody := lipgloss.JoinVertical(lipgloss.Left, line1, line2)
		bottomBar = nowPlayingContainer.Width(m.Width - 2).Render(playerBody)
	} else {
		bottomBar = footerStyle.Width(m.Width).Render(" " + m.StatusMessage)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		mainContent,
		bottomBar,
	)
}
