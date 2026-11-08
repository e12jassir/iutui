package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/model"
)

const (
	SidebarWidth = 26
)

// SidebarItem represents either a Library tab or a Playlist row.
type SidebarItem struct {
	Title string
	Tab   model.Tab
	IsTab bool
}

// DefaultLibraryTabs returns the fixed tabs in the Library panel.
func DefaultLibraryTabs() []model.Tab {
	return []model.Tab{
		model.TabHome,
		model.TabMusic,
		model.TabSubscriptions,
		model.TabHistory,
		model.TabDownloads,
	}
}

// RenderSidebarSpotatui renders the two stacked Spotatui boxes: ╭Library───╮ and ╭Playlists───╮
func RenderSidebarSpotatui(activeTab model.Tab, focused bool, inPlaylists bool, width, totalHeight int, playlists []string, selectedPlaylistIdx int) string {
	if width < 15 {
		width = 15
	}
	tabs := DefaultLibraryTabs()

	// 1. Library Box
	libHeight := len(tabs) + 2 // 5 items + top/bottom borders = 7
	if libHeight > totalHeight-4 {
		libHeight = totalHeight - 4
		if libHeight < 4 {
			libHeight = 4
		}
	}

	libLines := make([]string, 0, len(tabs))
	for _, t := range tabs {
		isSelected := (t == activeTab)
		prefix := "  "
		if isSelected {
			prefix = "▶ "
		}

		var line string
		label := fmt.Sprintf("%s%s", prefix, t.String())
		if isSelected && focused && !inPlaylists {
			line = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true).Render(label)
		} else if isSelected {
			line = lipgloss.NewStyle().Foreground(colorTextNormal).Bold(true).Render(label)
		} else {
			line = lipgloss.NewStyle().Foreground(colorTextMuted).Render(label)
		}
		libLines = append(libLines, line)
	}

	libPanelFocused := focused && !inPlaylists
	libBox := RenderPanel("Library", libLines, width, libHeight, libPanelFocused)

	// 2. Playlists / Canales Box
	playlistsHeight := totalHeight - libHeight
	if playlistsHeight < 3 {
		playlistsHeight = 3
	}

	pLines := make([]string, 0, playlistsHeight)
	pLines = append(pLines, lipgloss.NewStyle().Foreground(colorDimTitle).Render("  + Add Playlist"))

	if len(playlists) == 0 {
		pLines = append(pLines, lipgloss.NewStyle().Foreground(colorTextMuted).Render("  (Sin canales)"))
	} else {
		for i, p := range playlists {
			prefix := "  "
			isSelected := (i == selectedPlaylistIdx)
			if isSelected && inPlaylists {
				prefix = "▶ "
			}
			label := fmt.Sprintf("%s%s", prefix, p)
			if isSelected && focused && inPlaylists {
				pLines = append(pLines, lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true).Render(label))
			} else {
				pLines = append(pLines, lipgloss.NewStyle().Foreground(colorTextMuted).Render(label))
			}
		}
	}

	playlistsPanelFocused := focused && inPlaylists
	playlistsBox := RenderPanel("Playlists", pLines, width, playlistsHeight, playlistsPanelFocused)

	return lipgloss.JoinVertical(lipgloss.Left, libBox, playlistsBox)
}
