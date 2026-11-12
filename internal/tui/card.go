package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/model"
	"github.com/mattn/go-runewidth"
)

const (
	CardInnerWidth  = 32
	CardTotalWidth  = 34 // Inner + 2 border columns
	CardThumbHeight = 7
	CardTotalHeight = 14 // 7 (thumb) + 1 (badge) + 2 (title) + 1 (channel) + 1 (views) + 2 (borders)
)

var (
	// YouTube Dark Theme
	colorCardBorderNormal   = lipgloss.Color("#2E3346")
	colorCardBorderSelected = lipgloss.Color("#BD93F9") // Vibrant Spotatui Purple Accent
	colorCardDimText        = lipgloss.Color("#9CA3AF")
	colorCardChannelText    = lipgloss.Color("#38BDF8") // YouTube Sky Cyan
	colorCardBadge          = lipgloss.Color("#E5E7EB")
	colorCardLive           = lipgloss.Color("#EF4444")

	cardStyleNormal = lipgloss.NewStyle().
			Width(CardInnerWidth).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorCardBorderNormal).
			Padding(0, 0)

	cardStyleSelected = lipgloss.NewStyle().
				Width(CardInnerWidth).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(colorCardBorderSelected).
				Bold(true).
				Padding(0, 0)

	badgeLiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCardLive)

	badgeDurStyle = lipgloss.NewStyle().
			Foreground(colorCardBadge)

	titleNormalStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#E5E7EB"))

	titleSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF"))

	channelNameStyle = lipgloss.NewStyle().
				Foreground(colorCardChannelText)

	viewsMetaStyle = lipgloss.NewStyle().
			Foreground(colorCardDimText)
)

// RenderCard renders a YouTube Web 3-tier card with strict mathematical height invariance:
// Tier 1: Real Thumbnail (Truecolor subpixel quadrants) - exactly 8 lines
// Tier 2: Dedicated Badge Row - exactly 1 line
// Tier 3: Metadata:
//   - Title (strictly exactly 2 lines)
//   - Channel + Views (strictly 2 lines)
func RenderCard(v model.Video, isSelected bool) string {
	// 1. Thumbnail (strictly CardThumbHeight lines)
	thumbView := RenderThumbnailANSI(v.ID, CardInnerWidth, CardThumbHeight)

	// 2. Badge bar (strictly 1 line)
	var badgeStr string
	if v.IsLive {
		badgeStr = badgeLiveStyle.Render(" ● EN VIVO ")
	} else if v.Duration != "" {
		badgeStr = badgeDurStyle.Render(fmt.Sprintf(" ⏱ %s ", v.Duration))
	} else {
		badgeStr = badgeDurStyle.Render(" ⏱ --:-- ")
	}
	badgeRow := lipgloss.NewStyle().
		Width(CardInnerWidth).
		Align(lipgloss.Right).
		Render(badgeStr)

	// 3. Title (strictly exactly 2 lines)
	cleanTitle := strings.ReplaceAll(strings.ReplaceAll(v.Title, "\n", " "), "\r", " ")
	titlePrefix := " "
	availTitleWidth := CardInnerWidth - 2
	if isSelected {
		titlePrefix = "▶ "
		availTitleWidth = CardInnerWidth - 3
	}
	tLine1, tLine2 := splitTitleStrict(cleanTitle, availTitleWidth)
	tLine1 = titlePrefix + tLine1
	if tLine2 != "" {
		tLine2 = " " + tLine2
	}

	titleSt := titleNormalStyle
	if isSelected {
		titleSt = titleSelectedStyle
	}

	tView1 := titleSt.Render(runewidth.FillRight(tLine1, CardInnerWidth))
	tView2 := titleSt.Render(runewidth.FillRight(tLine2, CardInnerWidth))

	// 4. Channel (strictly exactly 1 line)
	chText := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(v.Channel, "\n", " "), "\r", " "))
	if chText == "" || chText == "NA" {
		chText = "YouTube"
	}
	chText = " 📺 " + chText
	chText = runewidth.Truncate(chText, CardInnerWidth-1, "…")
	chView := channelNameStyle.Render(runewidth.FillRight(chText, CardInnerWidth))

	// 5. Views & metadata (strictly exactly 1 line)
	vText := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(v.Views, "\n", " "), "\r", " "))
	if vText == "" || vText == "NA" {
		vText = "YouTube Video"
	}
	vText = " • " + vText
	vText = runewidth.Truncate(vText, CardInnerWidth-1, "…")
	vView := viewsMetaStyle.Render(runewidth.FillRight(vText, CardInnerWidth))

	// Assemble card body (8 + 1 + 1 + 1 + 1 + 1 = 13 inner lines + 2 border lines = 15 total lines)
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		thumbView,
		badgeRow,
		tView1,
		tView2,
		chView,
		vView,
	)

	if isSelected {
		return cardStyleSelected.Render(body)
	}
	return cardStyleNormal.Render(body)
}

// splitTitleStrict splits a title into exactly two lines of max width maxWidth.
func splitTitleStrict(title string, maxWidth int) (string, string) {
	if maxWidth < 10 {
		maxWidth = 10
	}

	title = strings.TrimSpace(title)
	if runewidth.StringWidth(title) <= maxWidth {
		return title, ""
	}

	words := strings.Fields(title)
	var line1 strings.Builder
	idx := 0

	for _, w := range words {
		test := w
		if line1.Len() > 0 {
			test = " " + w
		}
		if runewidth.StringWidth(line1.String()+test) > maxWidth {
			break
		}
		line1.WriteString(test)
		idx++
	}

	// If single very long word
	if line1.Len() == 0 && len(words) > 0 {
		line1.WriteString(runewidth.Truncate(words[0], maxWidth, ""))
		idx = 1
	}

	remaining := strings.Join(words[idx:], " ")
	line2 := runewidth.Truncate(remaining, maxWidth, "…")

	return line1.String(), line2
}
