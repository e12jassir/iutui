package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/mattn/go-runewidth"
)

const (
	TopHelpWidth     = 14
	TopSettingsWidth = 16
)

// View renders YouTube Web's terminal interface with real ANSI thumbnail cards and sleek drawers.
func (m Model) View() string {
	if !m.Ready || m.Width < 40 || m.Height < 15 {
		return "\n  ⚡ Cargando YouTube Terminal (yt)..."
	}

	// 1. Top Bar: [Buscar en YouTube] [Ayuda] [Ajustes]
	helpWidth := TopHelpWidth
	settingsWidth := TopSettingsWidth
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
		searchContent = " 🔍 " + m.SearchQuery + cursor
	} else {
		searchContent = lipgloss.NewStyle().Foreground(colorTextMuted).Render(" 🔍 Buscar en YouTube (presiona / o haz clic)...")
	}

	searchBox := RenderPanel("Buscar", []string{searchContent}, searchWidth, 3, m.Focus == FocusSearch)
	helpBox := RenderPanel("Ayuda", []string{" [?] Atajos"}, helpWidth, 3, m.ShowHelp)
	settingsBox := RenderPanel("Ajustes", []string{" [q] Salir"}, settingsWidth, 3, m.ShowSettings)

	topBar := lipgloss.JoinHorizontal(lipgloss.Top, searchBox, helpBox, settingsBox)

	// 2. Middle Section: [Sidebar (YouTube Drawer)] + [YouTube Video Grid with Thumbnails]
	playerHeight := 5
	middleHeight := m.Height - 3 - playerHeight
	if middleHeight < 8 {
		middleHeight = 8
	}

	sidebarWidth := SidebarWidth
	gridWidth := m.Width - sidebarWidth - 1
	if gridWidth < CardTotalWidth+2 {
		gridWidth = CardTotalWidth + 2
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

	gridHeader := m.ActiveTab.Icon() + " " + m.ActiveTab.String()
	if m.InPlaylists && m.SelectedPlaylistIdx >= 0 && m.SelectedPlaylistIdx < len(m.Playlists) {
		gridHeader = m.Playlists[m.SelectedPlaylistIdx]
	} else if m.SearchQuery != "" {
		gridHeader = "🔍 " + m.SearchQuery
	}

	gridView := RenderGridPanel(&m.Grid, gridHeader, gridWidth, middleHeight, m.Focus == FocusGrid)

	middleSection := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, " ", gridView)

	// 3. Bottom Player Bar (YouTube Miniplayer)
	var statusText string
	if m.PlayerStatus.IsRunning {
		if m.PlayerStatus.Paused {
			statusText = "⏸ Pausado"
		} else {
			statusText = "▶ Reproduciendo"
		}
	} else {
		statusText = "⏹ Inactivo"
	}

	vol := int(m.PlayerStatus.Volume)
	if vol == 0 && m.PlayerStatus.IsRunning {
		vol = 100
	}
	playingTitle := fmt.Sprintf(" Reproductor (mpv IPC • %s • Vol: %d%%) ", statusText, vol)

	// Line 1: Track Title + StatusMessage on the right
	var leftTitle string
	if m.PlayerStatus.Title != "" {
		leftTitle = " ▶ " + m.PlayerStatus.Title
	} else if v, ok := m.Grid.SelectedVideo(); ok {
		leftTitle = " 🎯 Seleccionado: " + v.Title + " (" + v.Channel + ")"
	} else {
		leftTitle = " (Ninguna reproducción activa)"
	}

	innerW := m.Width - 2
	rightStatus := ""
	if m.StatusMessage != "" {
		rightStatus = " │ " + m.StatusMessage + " "
	}
	availLeft := innerW - runewidth.StringWidth(rightStatus)
	if availLeft < 20 {
		availLeft = innerW
		rightStatus = ""
	}
	leftTrunc := runewidth.Truncate(leftTitle, availLeft, "…")
	leftPad := runewidth.FillRight(leftTrunc, availLeft)

	var titleLine string
	if m.PlayerStatus.Title != "" {
		titleLine = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(leftPad) +
			lipgloss.NewStyle().Foreground(colorFocusedPurple).Render(rightStatus)
	} else {
		titleLine = lipgloss.NewStyle().Foreground(colorTextNormal).Render(leftPad) +
			lipgloss.NewStyle().Foreground(colorFocusedPurple).Render(rightStatus)
	}

	// Line 2: Progress timeline
	timePos := player.FormatTime(m.PlayerStatus.TimePos)
	duration := player.FormatTime(m.PlayerStatus.Duration)
	barBudget := m.Width - len(timePos) - len(duration) - 10
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
	keyHintStyle := lipgloss.NewStyle().Foreground(colorTextMuted)
	controlsLine := fmt.Sprintf("  %s  %s  %s  %s  %s  %s  %s  %s   %s",
		btnStyle.Render("[⏮ Ant]"),
		btnStyle.Render("[⏯ Pausa]"),
		btnStyle.Render("[⏭ Sig]"),
		btnStyle.Render("[⏪ -10s]"),
		btnStyle.Render("[⏩ +10s]"),
		btnStyle.Render("[🔉 Vol-]"),
		btnStyle.Render("[🔊 Vol+]"),
		btnStyle.Render("[🗔 Video]"),
		keyHintStyle.Render("Enter: Reproducir • a: Audio • /: Buscar • ?: Ayuda"),
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
		"  " + strings.Repeat("─", 60),
		"  Tab / Shift+Tab   Cambiar de panel (Buscador / Guía / Cuadrícula / Reproductor)",
		"  /                 Enfocar buscador de YouTube directamente",
		"  h / j / k / l     Navegar tarjetas o secciones (también flechas ← ↓ ↑ →)",
		"  Enter             Reproducir video en ventana mpv (o buscar playlist)",
		"  a                 Reproducir solo audio en segundo plano",
		"  v                 Forzar apertura de ventana de video mpv",
		"  Espacio           Pausar / Reanudar reproducción",
		"  [ / ]             Retroceder / Avanzar 10 segundos",
		"  + / -             Subir / Bajar volumen (+5% / -5%)",
		"  n / p             Siguiente / Anterior en cola",
		"  r                 Recargar vista actual",
		"  ?                 Abrir / Cerrar esta ventana de ayuda",
		"  q / Ctrl+C        Cerrar YouTube Terminal",
		"",
		"  🖱 CONTROLES DE MOUSE:",
		"  " + strings.Repeat("─", 60),
		"  • Clic en buscador: activa el cursor para escribir búsqueda",
		"  • Clic en panel izquierdo: cambia de sección o lanza colección",
		"  • Clic en una tarjeta: la selecciona (borde morado #BD93F9)",
		"  • Segundo clic en tarjeta seleccionada: reproduce el video en mpv",
		"  • Rueda del ratón: desplazamiento fluido por las filas de tarjetas",
		"  • Clic en controles inferiores: pausa, salto ±10s y volumen directo",
	}

	modalW := 86
	if m.Width-6 < modalW {
		modalW = m.Width - 6
	}
	box := RenderPanel(" Ayuda & Atajos de YouTube Terminal ", helpText, modalW, len(helpText)+2, true)
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}
