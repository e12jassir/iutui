package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/data"
	"github.com/e12jassir/ytx-cli/internal/model"
)

const (
	CardInnerWidth  = 34
	CardTotalWidth  = 36 // Inner + borders
	CardThumbHeight = 5
	CardTotalHeight = 12
)

var (
	// Theme Colors (Modern Spotatui Dark Palette)
	colorBgCard         = lipgloss.Color("#161822")
	colorBgThumb        = lipgloss.Color("#0E1017")
	colorBorderNormal   = lipgloss.Color("#262938")
	colorBorderSelected = lipgloss.Color("#FF2A55") // Vibrant YouTube Accent
	colorDim            = lipgloss.Color("#6C728A")
	colorAccent         = lipgloss.Color("#00E5FF") // Electric Cyan
	colorLiveBadge      = lipgloss.Color("#FF1744")
	colorDurationBadge  = lipgloss.Color("#1F2330")

	cardStyleNormal = lipgloss.NewStyle().
			Width(CardInnerWidth).
			Background(colorBgCard).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderNormal).
			Padding(0, 1)

	cardStyleSelected = lipgloss.NewStyle().
				Width(CardInnerWidth).
				Background(lipgloss.Color("#1D202F")).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(colorBorderSelected).
				Padding(0, 1)

	thumbPlaceholderStyle = lipgloss.NewStyle().
				Width(CardInnerWidth - 2).
				Height(CardThumbHeight).
				Align(lipgloss.Center, lipgloss.Center).
				Background(colorBgThumb).
				Foreground(colorDim)

	thumbCachedStyle = lipgloss.NewStyle().
				Width(CardInnerWidth - 2).
				Height(CardThumbHeight).
				Align(lipgloss.Center, lipgloss.Center).
				Background(lipgloss.Color("#111528")).
				Foreground(lipgloss.Color("#5B6998"))

	liveBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Background(colorLiveBadge).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	durationBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Background(colorDurationBadge).
				Foreground(lipgloss.Color("#CBD5E1")).
				Padding(0, 1)

	titleStyleNormal = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#E2E8F0"))

	titleStyleSelected = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF"))

	channelStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	metaStyle = lipgloss.NewStyle().
			Foreground(colorDim)
)

// HasCachedThumbnail checks if a thumbnail is already on disk.
func HasCachedThumbnail(id string) bool {
	cacheDir, _ := data.DefaultPaths()
	thumbsDir := filepath.Join(cacheDir, "thumbs")
	for _, q := range []string{"mqdefault", "hqdefault", "maxresdefault"} {
		f := filepath.Join(thumbsDir, fmt.Sprintf("%s_%s.jpg", id, q))
		if info, err := os.Stat(f); err == nil && info.Size() > 0 {
			return true
		}
	}
	return false
}

// RenderCard renders a single video card with a strict 3-tier layout:
// Tier 1: Thumbnail box (placeholder or Kitty graphics canvas)
// Tier 2: Badge bar (Live status or Duration)
// Tier 3: Metadata (Title, Channel, Views & Date)
func RenderCard(v model.Video, isSelected bool) string {
	// Tier 1: Thumbnail viewport
	var thumbView string
	if HasCachedThumbnail(v.ID) {
		thumbView = thumbCachedStyle.Render(fmt.Sprintf("⚡ [THUMB CACHED]\n▶ %s", v.ID))
	} else {
		thumbView = thumbPlaceholderStyle.Render(fmt.Sprintf("▶ [%s]\n16:9 Miniatura", v.ID))
	}

	// Tier 2: Dedicated Badge Row
	var badgeView string
	if v.IsLive {
		badgeView = liveBadgeStyle.Render("🔴 EN VIVO")
	} else if v.Duration != "" {
		badgeView = durationBadgeStyle.Render(v.Duration)
	} else {
		badgeView = durationBadgeStyle.Render("--:--")
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
	metaText := strings.TrimSpace(v.Views)
	if metaText == "" {
		metaText = "YouTube"
	}
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
