package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	// Spotatui Purple Theme
	colorFocusedPurple = lipgloss.Color("#BD93F9")
	colorDimBorder     = lipgloss.Color("#374151")
	colorDimTitle      = lipgloss.Color("#9CA3AF")
	colorTextNormal    = lipgloss.Color("#E5E7EB")
	colorTextMuted     = lipgloss.Color("#6B7280")
	colorGreen         = lipgloss.Color("#10B981")
	colorRed           = lipgloss.Color("#F43F5E")
	colorCyan          = lipgloss.Color("#38BDF8")

	styleFocusedBorder = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true)
	styleDimBorder     = lipgloss.NewStyle().Foreground(colorDimBorder)

	styleFocusedTitle = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true)
	styleDimTitle     = lipgloss.NewStyle().Foreground(colorDimTitle)
)

// RenderPanel draws a Spotatui-style rounded box with an embedded title in the top border.
// If focused is true, the border and title are highlighted in Spotatui purple (#BD93F9).
func RenderPanel(title string, content []string, width, height int, focused bool) string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}

	borderStyle := styleDimBorder
	titleStyle := styleDimTitle
	if focused {
		borderStyle = styleFocusedBorder
		titleStyle = styleFocusedTitle
	}

	// 1. Top border with embedded title: ╭Title─────────╮
	innerWidth := width - 2
	var topLine strings.Builder
	topLine.WriteString(borderStyle.Render("╭"))

	if title != "" {
		tWidth := runewidth.StringWidth(title)
		if tWidth > innerWidth-1 {
			// Truncate title if it exceeds box width
			title = runewidth.Truncate(title, innerWidth-2, "…")
			tWidth = runewidth.StringWidth(title)
		}
		topLine.WriteString(titleStyle.Render(title))
		remaining := innerWidth - tWidth
		if remaining > 0 {
			topLine.WriteString(borderStyle.Render(strings.Repeat("─", remaining)))
		}
	} else {
		topLine.WriteString(borderStyle.Render(strings.Repeat("─", innerWidth)))
	}
	topLine.WriteString(borderStyle.Render("╮"))

	// 2. Middle content lines: │ content │
	bodyHeight := height - 2
	lines := make([]string, 0, height)
	lines = append(lines, topLine.String())

	for i := 0; i < bodyHeight; i++ {
		var rowContent string
		if i < len(content) {
			rowContent = content[i]
		}

		cWidth := runewidth.StringWidth(stripANSI(rowContent))
		var middle strings.Builder
		middle.WriteString(borderStyle.Render("│"))

		if cWidth > innerWidth {
			middle.WriteString(truncateANSI(rowContent, innerWidth))
		} else {
			middle.WriteString(rowContent)
			padding := innerWidth - cWidth
			if padding > 0 {
				middle.WriteString(strings.Repeat(" ", padding))
			}
		}

		middle.WriteString(borderStyle.Render("│"))
		lines = append(lines, middle.String())
	}

	// 3. Bottom border: ╰────────────╯
	var bottomLine strings.Builder
	bottomLine.WriteString(borderStyle.Render("╰"))
	bottomLine.WriteString(borderStyle.Render(strings.Repeat("─", innerWidth)))
	bottomLine.WriteString(borderStyle.Render("╯"))
	lines = append(lines, bottomLine.String())

	return strings.Join(lines, "\n")
}

// truncateANSI truncates an ANSI-colored string to maxWidth visible columns while preserving SGR sequences and resetting at the end.
func truncateANSI(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	var b strings.Builder
	inEscape := false
	used := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == 'm' {
				inEscape = false
			}
			continue
		}
		rw := runewidth.RuneWidth(r)
		if used+rw > maxWidth {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("\x1b[0m")
	if used < maxWidth {
		b.WriteString(strings.Repeat(" ", maxWidth-used))
	}
	return b.String()
}

// stripANSI removes ANSI escape codes to accurately measure visible string width.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
