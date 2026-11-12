package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/mattn/go-runewidth"
)

const (
	SidebarWidth = 26
)

// DefaultLibraryTabs returns the fixed tabs in the YouTube navigation drawer.
func DefaultLibraryTabs() []model.Tab {
	return []model.Tab{
		model.TabHome,
		model.TabMusic,
		model.TabSubscriptions,
		model.TabHistory,
		model.TabDownloads,
	}
}

// RenderSidebarYouTube renders YouTube Web's unified navigation drawer in a single solid panel.
func RenderSidebarYouTube(activeTab model.Tab, focused bool, inPlaylists bool, width, totalHeight int, playlists []string, selectedPlaylistIdx int) string {
	if width < 15 {
		width = 15
	}
	innerWidth := width - 2
	maxTextWidth := innerWidth - 2

	var lines []string

	sectionHeaderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Bold(true)
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#2E3346"))
	dividerLine := dividerStyle.Render(strings.Repeat("─", innerWidth))

	// Section 1: PRINCIPAL
	lines = append(lines, sectionHeaderStyle.Render(" PRINCIPAL"))
	mainTabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions}
	for _, t := range mainTabs {
		isSelected := (t == activeTab && !inPlaylists)
		prefix := "  "
		if isSelected {
			prefix = "▶ "
		}
		label := fmt.Sprintf("%s%s %s", prefix, t.Icon(), t.String())
		label = runewidth.Truncate(label, maxTextWidth, "…")

		var rendered string
		if isSelected && focused {
			rendered = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true).Render(label)
		} else if isSelected {
			rendered = lipgloss.NewStyle().Foreground(colorTextNormal).Bold(true).Render(label)
		} else {
			rendered = lipgloss.NewStyle().Foreground(colorTextMuted).Render(label)
		}
		lines = append(lines, " "+rendered)
	}

	lines = append(lines, dividerLine)

	// Section 2: TÚ / BIBLIOTECA
	lines = append(lines, sectionHeaderStyle.Render(" BIBLIOTECA"))
	biblioTabs := []model.Tab{model.TabHistory, model.TabDownloads}
	for _, t := range biblioTabs {
		isSelected := (t == activeTab && !inPlaylists)
		prefix := "  "
		if isSelected {
			prefix = "▶ "
		}
		label := fmt.Sprintf("%s%s %s", prefix, t.Icon(), t.String())
		label = runewidth.Truncate(label, maxTextWidth, "…")

		var rendered string
		if isSelected && focused {
			rendered = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true).Render(label)
		} else if isSelected {
			rendered = lipgloss.NewStyle().Foreground(colorTextNormal).Bold(true).Render(label)
		} else {
			rendered = lipgloss.NewStyle().Foreground(colorTextMuted).Render(label)
		}
		lines = append(lines, " "+rendered)
	}

	lines = append(lines, dividerLine)

	// Section 3: EXPLORAR / COLECCIONES
	lines = append(lines, sectionHeaderStyle.Render(" EXPLORAR MIXES"))

	for i, p := range playlists {
		prefix := "  "
		isSelected := (i == selectedPlaylistIdx && inPlaylists)
		if isSelected {
			prefix = "▶ "
		}
		label := fmt.Sprintf("%s%s", prefix, p)
		label = runewidth.Truncate(label, maxTextWidth, "…")

		var rendered string
		if isSelected && focused {
			rendered = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true).Render(label)
		} else if isSelected {
			rendered = lipgloss.NewStyle().Foreground(colorTextNormal).Bold(true).Render(label)
		} else {
			rendered = lipgloss.NewStyle().Foreground(colorTextMuted).Render(label)
		}
		lines = append(lines, " "+rendered)
	}

	return RenderPanel("YouTube", lines, width, totalHeight, focused)
}
