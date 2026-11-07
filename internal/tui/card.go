package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/model"
)

const (
	CardInnerWidth  = 34
	CardTotalWidth  = 36 // Inner + borders
	CardThumbHeight = 6
	CardTotalHeight = 13
)

var (
	// Base borders & colors
	colorBorderNormal   = lipgloss.Color("#2C2E3B")
	colorBorderSelected = lipgloss.Color("#FF3333") // YouTube red accent
	colorDim            = lipgloss.Color("#71768E")
	colorAccent         = lipgloss.Color("#00E5FF") // Electric cyan
	colorLiveBadge      = lipgloss.Color("#FF1744")
	colorDurationBadge  = lipgloss.Color("#1E212D")

	cardStyleNormal = lipgloss.NewStyle().
			Width(CardInnerWidth).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderNormal).
			Padding(0, 1)

	cardStyleSelected = lipgloss.NewStyle().
				Width(CardInnerWidth).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(colorBorderSelected).
				Padding(0, 1)

	thumbPlaceholderStyle = lipgloss.NewStyle().
				Width(CardInnerWidth - 2).
				Height(CardThumbHeight).
				Align(lipgloss.Center, lipgloss.Center).
				Background(lipgloss.Color("#14161F")).
				Foreground(colorDim)

	liveBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Background(colorLiveBadge).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	durationBadgeStyle = lipgloss.NewStyle().
				Background(colorDurationBadge).
				Foreground(lipgloss.Color("#D1D5DB")).
				Padding(0, 1)

	titleStyleNormal = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#E5E7EB"))

	titleStyleSelected = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF"))

	channelStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	metaStyle = lipgloss.NewStyle().
			Foreground(colorDim)
)

// RenderCard renders a single video card with a strict 3-tier layout:
// Tier 1: Thumbnail box (placeholder or Kitty graphics canvas)
// Tier 2: Badge bar (Live status or Duration)
// Tier 3: Metadata (Title, Channel, Views & Date)
func RenderCard(v model.Video, isSelected bool) string {
	// Tier 1: Thumbnail viewport placeholder
	thumbContent := fmt.Sprintf("▶ [%s]\n16:9 Thumbnail", v.ID)
	thumbView := thumbPlaceholderStyle.Render(thumbContent)

	// Tier 2: Dedicated Badge Row
	var badgeView string
	if v.IsLive {
		badgeView = liveBadgeStyle.Render("🔴 EN VIVO")
	} else {
		badgeView = durationBadgeStyle.Render(v.Badge())
	}
	badgeRow := lipgloss.NewStyle().
		Width(CardInnerWidth - 2).
		Align(lipgloss.Right).
		Render(badgeView)

	// Tier 3: Title (up to 2 lines, truncated)
	titleText := v.Title
	if isSelected {
		titleText = "▶ " + titleText
	}
	titleView := renderTruncatedTitle(titleText, CardInnerWidth-2, 2, isSelected)

	// Channel line
	channelView := channelStyle.Render(truncate(v.Channel, CardInnerWidth-2))

	// Views and date line
	metaText := strings.TrimSpace(fmt.Sprintf("%s • %s", v.Views, v.Published))
	metaView := metaStyle.Render(truncate(metaText, CardInnerWidth-2))

	// Assemble card body
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		thumbView,
		badgeRow,
		titleView,
		channelView,
		metaView,
	)

	if isSelected {
		return cardStyleSelected.Render(body)
	}
	return cardStyleNormal.Render(body)
}

func renderTruncatedTitle(s string, maxWidth, maxLines int, isSelected bool) string {
	lines := wrapOrTruncate(s, maxWidth, maxLines)
	st := titleStyleNormal
	if isSelected {
		st = titleStyleSelected
	}
	return st.Render(strings.Join(lines, "\n"))
}

func wrapOrTruncate(s string, maxWidth, maxLines int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	currentLine := ""

	for _, w := range words {
		if len(lines) == maxLines-1 {
			// On the final allowed line, fit as many words as possible then append ellipsis
			test := currentLine
			if test != "" {
				test += " "
			}
			test += w
			if len(test) > maxWidth {
				if len(test) > maxWidth-3 && len(currentLine) > 0 {
					currentLine += "..."
				} else {
					currentLine = truncate(test, maxWidth)
				}
				break
			}
			currentLine = test
			continue
		}

		if len(currentLine)+len(w)+1 > maxWidth {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = w
		} else {
			if currentLine != "" {
				currentLine += " "
			}
			currentLine += w
		}
	}

	if currentLine != "" && len(lines) < maxLines {
		lines = append(lines, currentLine)
	}

	// Pad with empty lines if fewer than maxLines for consistent card height
	for len(lines) < maxLines {
		lines = append(lines, "")
	}

	return lines
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
