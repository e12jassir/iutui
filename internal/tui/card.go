package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/model"
)

const (
	CardInnerWidth  = 32
	CardTotalWidth  = 34 // Inner + borders
	CardThumbHeight = 6
	CardTotalHeight = 12
)

var (
	// Transparent / Terminal Native Theme
	colorBorderNormal   = lipgloss.Color("#2E3346")
	colorBorderSelected = lipgloss.Color("#BD93F9") // Spotatui Vibrant Purple
	colorDim            = lipgloss.Color("#6B7280")
	colorAccent         = lipgloss.Color("#38BDF8") // Subtle sky cyan
	colorLiveBadge      = lipgloss.Color("#EF4444")

	cardStyleNormal = lipgloss.NewStyle().
			Width(CardInnerWidth).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderNormal).
			Padding(0, 0)

	cardStyleSelected = lipgloss.NewStyle().
				Width(CardInnerWidth).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderSelected).
				Bold(true).
				Padding(0, 0)

	liveBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLiveBadge)

	durationBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF"))

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

// RenderCard renders a single video card with zero background paints:
// Tier 1: Real Thumbnail (ANSI halfblocks rendered via chafa)
// Tier 2: Badge bar (Duration / Live)
// Tier 3: Metadata (Title, Channel, Views)
func RenderCard(v model.Video, isSelected bool) string {
	// Tier 1: Real Thumbnail
	thumbView := RenderThumbnailANSI(v.ID, CardInnerWidth, CardThumbHeight)

	// Tier 2: Dedicated Badge Row
	var badgeView string
	if v.IsLive {
		badgeView = liveBadgeStyle.Render("● EN VIVO")
	} else if v.Duration != "" {
		badgeView = durationBadgeStyle.Render(fmt.Sprintf("[%s]", v.Duration))
	} else {
		badgeView = durationBadgeStyle.Render("[--:--]")
	}

	badgeRow := lipgloss.NewStyle().
		Width(CardInnerWidth).
		Align(lipgloss.Right).
		Render(badgeView)

	// Tier 3: Title (up to 2 lines, truncated)
	titleText := v.Title
	if isSelected {
		titleText = "▶ " + titleText
	}
	titleView := renderTruncatedTitle(titleText, CardInnerWidth, 2, isSelected)

	// Channel line
	channelView := channelStyle.Render(truncate(v.Channel, CardInnerWidth))

	// Views and date line
	metaText := strings.TrimSpace(v.Views)
	if metaText == "" {
		metaText = "YouTube"
	}
	metaView := metaStyle.Render(truncate(metaText, CardInnerWidth))

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
			test := currentLine
			if test != "" {
				test += " "
			}
			test += w
			if len(test) > maxWidth {
				if len(currentLine) > 0 {
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
