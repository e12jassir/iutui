package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/model"
)

const (
	SidebarWidth = 22
)

var (
	sidebarStyle = lipgloss.NewStyle().
			Width(SidebarWidth).
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).
			BorderForeground(lipgloss.Color("#2E3346")).
			Padding(1, 1)

	logoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF2A55"))

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF2A55"))

	tabNormalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF"))

	tabFocusedUnselectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#38BDF8"))

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			PaddingTop(2)
)

// RenderSidebar renders the left navigation panel with tabs and hotkey hints.
func RenderSidebar(activeTab model.Tab, hasFocus bool, height int) string {
	logo := logoStyle.Render("▶ YTX-CLI") + "\n" + lipgloss.NewStyle().Foreground(colorDim).Render("YouTube Terminal")

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
			renderedTabs = append(renderedTabs, tabActiveStyle.Render("▶ "+label))
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
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Tab    Paneles"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("h/j/k/l Mover"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Enter  Play"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Espacio Pausa"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("/      Buscar"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("r      Recargar"),
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
