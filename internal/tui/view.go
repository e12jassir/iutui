package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/player"
	"github.com/mattn/go-runewidth"
)

// View renders YouTube Web's terminal interface with real ANSI thumbnail cards and sleek drawers.
func (m Model) View() string {
	if !m.Ready || m.Width < 40 || m.Height < 15 {
		return "\n  ⚡ Cargando YouTube Terminal (yt)..."
	}

	// 1. Top Bar: [Buscar en YouTube] [Ayuda] [Ajustes]
	helpWidth := 12
	settingsWidth := 14
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
		searchContent = "🔍 " + m.SearchQuery + cursor
	} else {
		searchContent = lipgloss.NewStyle().Foreground(colorTextMuted).Render("🔍 Buscar en YouTube (o presiona /)...")
	}

	searchBox := RenderPanel("Buscar", []string{searchContent}, searchWidth, 3, m.Focus == FocusSearch)
	helpBox := RenderPanel("Ayuda", []string{"Type ?"}, helpWidth, 3, m.ShowHelp)
	settingsBox := RenderPanel("Ajustes", []string{"[q] Salir"}, settingsWidth, 3, m.ShowSettings)

	topBar := lipgloss.JoinHorizontal(lipgloss.Top, searchBox, helpBox, settingsBox)

	// 2. Middle Section: [Sidebar (YouTube Drawer)] + [YouTube Video Grid with Thumbnails]
	playerHeight := 5
	middleHeight := m.Height - 3 - playerHeight
	if middleHeight < 8 {
		middleHeight = 8
	}

	sidebarWidth := SidebarWidth
	gridWidth := m.Width - sidebarWidth - 2
	if gridWidth < CardTotalWidth {
		gridWidth = CardTotalWidth
	}

	sidebarView := RenderSidebarYouTube(
		m.ActiveTab,
		m.Focus == FocusSidebar,
		m.InPlaylists,
		sidebarWidth,
		middleHeight,
		m.Playlists,
		m.SelectedPlaylistIdx,
	)

	gridView := RenderGrid(&m.Grid, gridWidth, middleHeight, m.Focus == FocusGrid)

	middleSection := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, " ", gridView)

	// 3. Bottom Player Bar (YouTube Miniplayer)
	var statusText string
	if m.PlayerStatus.IsRunning {
		if m.PlayerStatus.Paused {
			statusText = "Pausado"
		} else {
			statusText = "Reproduciendo"
		}
	} else {
		statusText = "Inactivo"
	}

	vol := int(m.PlayerStatus.Volume)
	if vol == 0 && m.PlayerStatus.IsRunning {
		vol = 100
	}
	playingTitle := fmt.Sprintf("Reproductor (mpv IPC • %s • Vol: %d%%)", statusText, vol)

	// Line 1: Track Title
	var titleLine string
	if m.PlayerStatus.Title != "" {
		t := runewidth.Truncate(m.PlayerStatus.Title, m.Width-6, "…")
		titleLine = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render("  ▶ " + t)
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
	controlsLine := fmt.Sprintf("  %s   %s   %s   %s   %s   %s   %s   %s",
		btnStyle.Render("[⏮ Anterior]"),
		btnStyle.Render("[⏯ Pausa]"),
		btnStyle.Render("[⏭ Siguiente]"),
		btnStyle.Render("[⏪ -10s]"),
		btnStyle.Render("[⏩ +10s]"),
		btnStyle.Render("[󰕾 Vol-]"),
		btnStyle.Render("[󰕾 Vol+]"),
		btnStyle.Render("[🗔 Ventana]"),
	)

	playerLines := []string{titleLine, progressLine, controlsLine}
	playerBox := RenderPanel(playingTitle, playerLines, m.Width, playerHeight, m.Focus == FocusPlayer)

	// Overlay Help modal if active
	if m.ShowHelp {
		return m.renderHelpModal()
	}

	return lipgloss.JoinVertical(lipgloss.Left, topBar, middleSection, playerBox)
}

func (m Model) renderHelpModal() string {
	helpText := []string{
		"  ⌨ ATAJOS DE TECLADO:",
		"  -------------------------------------------------------------",
		"  Tab / Shift+Tab   Cambiar de panel (Buscador / Guía / Cuadrícula)",
		"  /                 Enfocar buscador de YouTube directamente",
		"  h / j / k / l     Navegar tarjetas de video (estilo Vim)",
		"  Enter             Reproducir video en ventana mpv",
		"  v                 Alternar / Levantar ventana de video mpv",
		"  Espacio           Pausar / Reanudar reproducción",
		"  [ / ]             Retroceder / Avanzar 10 segundos",
		"  + / -             Subir / Bajar volumen",
		"  n / p             Siguiente / Anterior en cola",
		"  ?                 Cerrar esta ventana de ayuda",
		"  q / Esc           Salir de búsqueda o cerrar TUI",
		"",
		"  🖱 CONTROLES DE MOUSE:",
		"  -------------------------------------------------------------",
		"  • Clic en buscador: activa el cursor para escribir",
		"  • Clic en panel izquierdo: cambia de sección de YouTube",
		"  • Clic en una tarjeta: la selecciona (borde morado #BD93F9)",
		"  • Doble clic en tarjeta: reproduce el video en ventana mpv",
		"  • Rueda del ratón: scroll fluido en la cuadrícula de videos",
		"  • Clic en botones inferiores: control de reproducción directo",
	}

	box := RenderPanel("Ayuda & Atajos de YouTube Terminal", helpText, m.Width-8, len(helpText)+2, true)
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}
