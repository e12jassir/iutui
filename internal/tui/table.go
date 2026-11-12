package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/mattn/go-runewidth"
)

// TableState manages the list of videos, selection, and viewport scrolling.
type TableState struct {
	Videos       []model.Video
	SelectedIdx  int
	ScrollOffset int
}

// NewTableState constructs a TableState initialized to index 0.
func NewTableState(videos []model.Video) TableState {
	return TableState{
		Videos:       videos,
		SelectedIdx:  0,
		ScrollOffset: 0,
	}
}

// SelectedVideo returns the currently selected video if valid.
func (ts *TableState) SelectedVideo() (model.Video, bool) {
	if len(ts.Videos) == 0 || ts.SelectedIdx < 0 || ts.SelectedIdx >= len(ts.Videos) {
		return model.Video{}, false
	}
	return ts.Videos[ts.SelectedIdx], true
}

// MoveDown advances the cursor by 1 row.
func (ts *TableState) MoveDown(visibleHeight int) {
	if len(ts.Videos) == 0 {
		return
	}
	if ts.SelectedIdx < len(ts.Videos)-1 {
		ts.SelectedIdx++
		ts.adjustScroll(visibleHeight)
	}
}

// MoveUp retreats the cursor by 1 row.
func (ts *TableState) MoveUp(visibleHeight int) {
	if len(ts.Videos) == 0 {
		return
	}
	if ts.SelectedIdx > 0 {
		ts.SelectedIdx--
		ts.adjustScroll(visibleHeight)
	}
}

// SelectDirect sets the selected index directly (used by mouse clicks).
func (ts *TableState) SelectDirect(idx int, visibleHeight int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ts.Videos) {
		idx = len(ts.Videos) - 1
	}
	if idx >= 0 {
		ts.SelectedIdx = idx
		ts.adjustScroll(visibleHeight)
	}
}

// adjustScroll ensures the selected item stays inside the visible window.
func (ts *TableState) adjustScroll(visibleHeight int) {
	if visibleHeight <= 0 {
		visibleHeight = 10
	}
	if ts.SelectedIdx < ts.ScrollOffset {
		ts.ScrollOffset = ts.SelectedIdx
	}
	if ts.SelectedIdx >= ts.ScrollOffset+visibleHeight {
		ts.ScrollOffset = ts.SelectedIdx - visibleHeight + 1
	}
	if ts.ScrollOffset < 0 {
		ts.ScrollOffset = 0
	}
}

// RenderTableContent formats the tabular lines of tracks/videos.
func RenderTableContent(ts *TableState, width, height int, focused bool, nowPlayingID string) []string {
	if width < 20 {
		width = 20
	}

	// Calculate column budget:
	// Format: [Prefix 3] [Title] [Artist/Channel] [Length 8]
	avail := width
	lengthWidth := 8
	prefixWidth := 3

	remain := avail - lengthWidth - prefixWidth - 4
	if remain < 15 {
		remain = 15
	}
	titleWidth := int(float64(remain) * 0.58)
	channelWidth := remain - titleWidth
	if channelWidth < 8 {
		channelWidth = 8
	}

	lines := make([]string, 0, height)

	// 1. Column header line
	headerPrefix := "   "
	headerTitle := runewidth.FillRight("Title", titleWidth)
	headerChannel := runewidth.FillRight("Artist / Channel", channelWidth)
	headerLength := runewidth.FillLeft("Length", lengthWidth)

	headerLine := lipgloss.NewStyle().Foreground(colorDimTitle).Bold(true).Render(
		fmt.Sprintf("%s%s  %s  %s", headerPrefix, headerTitle, headerChannel, headerLength),
	)
	lines = append(lines, headerLine)

	// 2. Empty state
	if len(ts.Videos) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorTextMuted).Render("   (No hay videos disponibles)"))
		return lines
	}

	// 3. Render visible rows
	visibleRows := height - 1
	if visibleRows < 1 {
		visibleRows = 1
	}

	ts.adjustScroll(visibleRows)

	start := ts.ScrollOffset
	end := start + visibleRows
	if end > len(ts.Videos) {
		end = len(ts.Videos)
	}

	for i := start; i < end; i++ {
		v := ts.Videos[i]
		isSelected := (i == ts.SelectedIdx)
		isPlaying := (v.ID != "" && v.ID == nowPlayingID)

		// Prefix icon
		prefix := "   "
		if isPlaying {
			prefix = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("▶  ")
		} else if isSelected {
			prefix = lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true).Render("▶  ")
		}

		// Truncate fields
		tStr := runewidth.Truncate(v.Title, titleWidth, "…")
		tStr = runewidth.FillRight(tStr, titleWidth)

		cStr := runewidth.Truncate(v.Channel, channelWidth, "…")
		cStr = runewidth.FillRight(cStr, channelWidth)

		dur := v.Duration
		if dur == "" {
			dur = "--:--"
		}
		dStr := runewidth.Truncate(dur, lengthWidth, "")
		dStr = runewidth.FillLeft(dStr, lengthWidth)

		// Row styling
		var rowStr string
		if isSelected && focused {
			// Spotatui Purple highlighted row
			selectedStyle := lipgloss.NewStyle().Foreground(colorFocusedPurple).Bold(true)
			channelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
			durStyle := lipgloss.NewStyle().Foreground(colorFocusedPurple)
			rowStr = fmt.Sprintf("%s%s  %s  %s",
				prefix,
				selectedStyle.Render(tStr),
				channelStyle.Render(cStr),
				durStyle.Render(dStr),
			)
		} else if isSelected && !focused {
			// Dim selected row when panel lost focus
			selDimStyle := lipgloss.NewStyle().Foreground(colorTextNormal).Bold(true)
			rowStr = fmt.Sprintf("%s%s  %s  %s",
				prefix,
				selDimStyle.Render(tStr),
				lipgloss.NewStyle().Foreground(colorTextMuted).Render(cStr),
				lipgloss.NewStyle().Foreground(colorTextMuted).Render(dStr),
			)
		} else {
			// Regular row
			rowStr = fmt.Sprintf("%s%s  %s  %s",
				prefix,
				lipgloss.NewStyle().Foreground(colorTextNormal).Render(tStr),
				lipgloss.NewStyle().Foreground(colorTextMuted).Render(cStr),
				lipgloss.NewStyle().Foreground(colorTextMuted).Render(dStr),
			)
		}

		lines = append(lines, rowStr)
	}

	return lines
}
