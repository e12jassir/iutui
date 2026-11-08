package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/player"
	"github.com/mattn/go-runewidth"
)

// View renders the complete Spotatui-style interface with purple panel focus.
func (m Model) View() string {
	if !m.Ready || m.Width < 40 || m.Height < 15 {
		return "\n  ⚡ Cargando ytx-cli Spotatui TUI..."
	}

	// 1. Top Bar: [Search] [Help] [Settings]
	helpWidth := 14
	settingsWidth := 18
	searchWidth := m.Width - helpWidth - settingsWidth
	if searchWidth < 20 {
		searchWidth = 20
	}

	var searchContent string
	if m.Searching || m.SearchQuery != "" {
		cursor := " "
		if m.Focus == FocusSearch {
			cursor = "█"
		}
		searchContent = m.SearchQuery + cursor
	} else {
		searchContent = lipgloss.NewStyle().Foreground(colorTextMuted).Render("Type to search YouTube (or press /)...")
	}

	searchBox := RenderPanel("Search", []string{searchContent}, searchWidth, 3, m.Focus == FocusSearch)
	helpBox := RenderPanel("Help", []string{"Type ?"}, helpWidth, 3, m.ShowHelp)
	settingsBox := RenderPanel("Settings", []string{"Type Alt+,"}, settingsWidth, 3, m.ShowSettings)

	topBar := lipgloss.JoinHorizontal(lipgloss.Top, searchBox, helpBox, settingsBox)

	// 2. Middle Section: [Sidebar (Library + Playlists)] + [Tracks Table]
	playerHeight := 5
	middleHeight := m.Height - 3 - playerHeight
	if middleHeight < 8 {
		middleHeight = 8
	}

	sidebarWidth := SidebarWidth
	tableWidth := m.Width - sidebarWidth
	if tableWidth < 25 {
		tableWidth = 25
	}

	sidebarView := RenderSidebarSpotatui(
		m.ActiveTab,
		m.Focus == FocusSidebar,
		m.InPlaylists,
		sidebarWidth,
		middleHeight,
		m.Playlists,
		m.SelectedPlaylistIdx,
	)

	// Determine table title
	var tableTitle string
	if m.SearchPending {
		tableTitle = fmt.Sprintf("Searching: %s… ⏳", m.SearchQuery)
	} else if m.SearchQuery != "" {
		tableTitle = fmt.Sprintf("Search: %s (%d tracks)", m.SearchQuery, len(m.Table.Videos))
	} else {
		tableTitle = fmt.Sprintf("%s (%d tracks)", m.ActiveTab.String(), len(m.Table.Videos))
	}

	nowPlayingID := ""
	// Match title to identify current playing track in table
	if m.PlayerStatus.IsRunning && m.PlayerStatus.Title != "" {
		for _, v := range m.Table.Videos {
			if strings.Contains(strings.ToLower(m.PlayerStatus.Title), strings.ToLower(v.Title)) ||
				strings.Contains(strings.ToLower(v.Title), strings.ToLower(m.PlayerStatus.Title)) {
				nowPlayingID = v.ID
				break
			}
		}
	}

	tableLines := RenderTableContent(&m.Table, tableWidth-2, middleHeight-2, m.Focus == FocusTable, nowPlayingID)
	tableBox := RenderPanel(tableTitle, tableLines, tableWidth, middleHeight, m.Focus == FocusTable)

	middleSection := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, tableBox)

	// 3. Bottom Playing Bar
	var statusText string
	if m.PlayerStatus.IsRunning {
		if m.PlayerStatus.Paused {
			statusText = "Paused"
		} else {
			statusText = "Playing"
		}
	} else {
		statusText = "Idle"
	}

	vol := int(m.PlayerStatus.Volume)
	if vol == 0 && m.PlayerStatus.IsRunning {
		vol = 100
	}
	playingTitle := fmt.Sprintf("Playing (Soloist-PC | mpv IPC | Status: %s | Volume: %d%%)", statusText, vol)

	// Line 1: Track Title
	var titleLine string
	if m.PlayerStatus.Title != "" {
		t := runewidth.Truncate(m.PlayerStatus.Title, m.Width-6, "…")
		titleLine = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render("  " + t)
	} else {
		titleLine = lipgloss.NewStyle().Foreground(colorTextMuted).Render("  (Ninguna reproducción activa)")
	}

	// Line 2: Progress timeline
	timePos := player.FormatTime(m.PlayerStatus.TimePos)
	duration := player.FormatTime(m.PlayerStatus.Duration)
	barBudget := m.Width - len(timePos) - len(duration) - 16
	if barBudget < 10 {
		barBudget = 10
	}
	bar := player.ProgressBar(m.PlayerStatus.TimePos, m.PlayerStatus.Duration, barBudget)
	progressLine := fmt.Sprintf("  %s %s %s",
		lipgloss.NewStyle().Foreground(colorTextMuted).Render(timePos),
		lipgloss.NewStyle().Foreground(colorFocusedPurple).Render(bar),
		lipgloss.NewStyle().Foreground(colorTextMuted).Render(duration),
	)

	// Line 3: Interactive controls (clickable)
	btnStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	controlsLine := fmt.Sprintf("  %s   %s   %s   %s   %s   %s   %s",
		btnStyle.Render("[Prev]"),
		btnStyle.Render("[Play/Pause]"),
		btnStyle.Render("[Next]"),
		btnStyle.Render("[Seek -10]"),
		btnStyle.Render("[Seek +10]"),
		btnStyle.Render("[Vol-]"),
		btnStyle.Render("[Vol+]"),
	)

	playerLines := []string{titleLine, progressLine, controlsLine}
	playerBox := RenderPanel(playingTitle, playerLines, m.Width, playerHeight, m.Focus == FocusPlayer)

	// Overlay Help or Settings if active
	if m.ShowHelp {
		return m.renderHelpModal()
	}

	return lipgloss.JoinVertical(lipgloss.Left, topBar, middleSection, playerBox)
}

func (m Model) renderHelpModal() string {
	helpText := []string{
		"  ⌨ ATAJOS DE TECLADO:",
		"  -------------------------------------------------------------",
		"  Tab / Shift+Tab   Cambiar de panel (Search / Sidebar / Table)",
		"  /                 Enfocar buscador directamente",
		"  h / j / k / l     Navegación estilo Vim",
		"  Enter             Reproducir pista seleccionada",
		"  Espacio           Pausar / Reanudar (mpv IPC)",
		"  [ / ]             Retroceder / Avanzar 10 segundos",
		"  + / -             Subir / Bajar volumen",
		"  n / p             Siguiente / Anterior en cola",
		"  ?                 Cerrar esta ventana de ayuda",
		"  q / Esc           Salir de búsqueda o cerrar TUI",
		"",
		"  🖱 CONTROLES DE MOUSE:",
		"  -------------------------------------------------------------",
		"  • Clic en buscador: enfoca la barra para escribir",
		"  • Clic en Library/Playlists: cambia de vista",
		"  • Clic en una pista: la selecciona (cuadro morado)",
		"  • Doble clic en pista: la reproduce inmediatamente",
		"  • Rueda del ratón: scroll fluido en listas",
		"  • Clic en botones inferiores: control de reproducción directo",
	}

	box := RenderPanel("Ayuda & Atajos de Spotatui", helpText, m.Width-8, len(helpText)+2, true)
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}
