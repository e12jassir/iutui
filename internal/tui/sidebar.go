package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/model"
)

const (
	SidebarWidth = 24
)

var (
	sidebarStyle = lipgloss.NewStyle().
			Width(SidebarWidth).
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).
			BorderForeground(lipgloss.Color("#2C2E3B")).
			Padding(1, 1)

	logoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF3333"))

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#FF3333")).
			Padding(0, 1)

	tabNormalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Padding(0, 1)

	tabFocusedUnselectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#00E5FF")).
					Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			PaddingTop(2)
)

// RenderSidebar renders the left navigation panel with tabs and hotkey hints.
func RenderSidebar(activeTab model.Tab, hasFocus bool, height int) string {
	logo := logoStyle.Render("▶ YTX-CLI") + "\n" + lipgloss.NewStyle().Foreground(colorDim).Render("Terminal YouTube")

	tabs := []model.Tab{
		model.TabHome,
		model.TabMusic,
		model.TabSubscriptions,
		model.TabDownloads,
		model.TabHistory,
	}

	var renderedTabs []string
	for _, t := range tabs {
		label := fmt.Sprintf("%s %s", t.Icon(), t.String())
		if t == activeTab {
			renderedTabs = append(renderedTabs, tabActiveStyle.Render(label))
		} else if hasFocus {
			renderedTabs = append(renderedTabs, tabFocusedUnselectedStyle.Render("  "+label))
		} else {
			renderedTabs = append(renderedTabs, tabNormalStyle.Render("  "+label))
		}
	}

	tabSection := strings.Join(renderedTabs, "\n\n")

	helpContent := lipgloss.JoinVertical(
		lipgloss.Left,
		helpStyle.Render("Atajos:"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Tab    Panel"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("h/j/k/l Mover"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Enter  Play"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("/      Buscar"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("q      Salir"),
	)

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		logo,
		"\n",
		tabSection,
		"\n",
		helpContent,
	)

	return sidebarStyle.Height(height - 2).Render(body)
}
