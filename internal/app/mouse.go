package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/mattn/go-runewidth"
)

const (
	fullSidebarMinW = 140
	miniGuideMinW   = 100
	fullSidebarW    = 26
	miniGuideW      = 10
	cardW           = 30
	cardGap         = 2
	searchRowH      = 9
)

func (s *State) handleMouse(msg tea.MouseMsg) (*State, tea.Cmd) {
	switch {
	case msg.Button == tea.MouseButtonWheelDown:
		if s.Focus == FocusSearch && s.SearchDraft == "" {
			s.Focus = FocusMain
			s.SearchSuggestions = nil
			s.SuggestIdx = -1
		}
		if s.ShowQualityModal {
			if s.QualityIdx < len(QualityOptions)-1 {
				s.QualityIdx++
			}
			return s, nil
		}
		if s.ShowQueue {
			if len(s.MPVPlaylist) > 0 && s.QueueIdx < len(s.MPVPlaylist)-1 {
				s.QueueIdx++
			}
			return s, nil
		}
		if s.ShowSettings {
			s.SettingsIdx = (s.SettingsIdx + 1) % SettingsRowCount
			return s, nil
		}
		if s.ShowChapters {
			if len(s.Chapters) > 0 && s.ChapterIdx < len(s.Chapters)-1 {
				s.ChapterIdx++
			}
			return s, nil
		}
		if s.ShowLyrics && (s.LyricsSideWidth() == 0 || msg.X >= s.Width-s.LyricsSideWidth()) {
			s.LyricsScroll += 2
			return s, nil
		}
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() + step)
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.activeIdx())

	case msg.Button == tea.MouseButtonWheelUp:
		if s.Focus == FocusSearch && s.SearchDraft == "" {
			s.Focus = FocusMain
			s.SearchSuggestions = nil
			s.SuggestIdx = -1
		}
		if s.ShowQualityModal {
			if s.QualityIdx > 0 {
				s.QualityIdx--
			}
			return s, nil
		}
		if s.ShowQueue {
			if s.QueueIdx > 0 {
				s.QueueIdx--
			}
			return s, nil
		}
		if s.ShowSettings {
			s.SettingsIdx = (s.SettingsIdx + SettingsRowCount - 1) % SettingsRowCount
			return s, nil
		}
		if s.ShowChapters {
			if s.ChapterIdx > 0 {
				s.ChapterIdx--
			}
			return s, nil
		}
		if s.ShowLyrics && (s.LyricsSideWidth() == 0 || msg.X >= s.Width-s.LyricsSideWidth()) {
			s.LyricsScroll -= 2
			return s, nil
		}
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() - step)
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.activeIdx())

	case msg.Action == tea.MouseActionMotion:
		return s.handleMouseMotion(msg.X, msg.Y), nil

	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		return s.handleLeftClick(msg.X, msg.Y)
	}
	return s, nil
}

func (s *State) handleMouseMotion(x, y int) *State {
	if s.ShowHelp || s.ShowStillWatching {
		return s
	}
	if s.ShowSettings {
		if idx, inside := s.settingsModalRowAt(x, y); inside && idx >= 0 {
			s.SettingsIdx = idx
		}
		return s
	}
	if s.ShowQualityModal {
		if idx, inside := s.qualityModalRowAt(x, y); inside && idx >= 0 {
			s.QualityIdx = idx
		}
		return s
	}
	if s.ShowQueue {
		if idx, inside := s.queueModalRowAt(x, y); inside && idx >= 0 {
			s.QueueIdx = idx
		}
		return s
	}
	if s.ShowChapters {
		if idx, inside := s.chaptersModalRowAt(x, y); inside && idx >= 0 {
			s.ChapterIdx = idx
		}
		return s
	}

	if y >= 4 {
		// Unstick FocusSearch if pointer moves back over the main grid without an active draft.
		if s.Focus == FocusSearch && s.SearchDraft == "" {
			s.Focus = FocusMain
			s.SearchSuggestions = nil
			s.SuggestIdx = -1
		}
	}

	if idx, ok := s.videoIndexAt(x, y, false); ok {
		s.Focus = FocusMain
		s.HoverSelection(idx)
	}
	return s
}

func (s *State) handleLeftClick(x, y int) (*State, tea.Cmd) {
	if s.ShowHelp {
		s.ShowHelp = false
		return s, nil
	}
	if s.ShowQualityModal {
		idx, inside := s.qualityModalRowAt(x, y)
		if !inside {
			s.ShowQualityModal = false
			return s, nil
		}
		if idx >= 0 && idx < len(QualityOptions) {
			return s.ApplyQualityOption(idx)
		}
		return s, nil
	}
	if s.ShowQueue {
		idx, inside := s.queueModalRowAt(x, y)
		if !inside {
			s.ShowQueue = false
			return s, nil
		}
		if idx >= 0 && idx < len(s.MPVPlaylist) {
			s.QueueIdx = idx
			return s.PlayQueueSelected()
		}
		return s, nil
	}
	if s.ShowChapters {
		idx, inside := s.chaptersModalRowAt(x, y)
		if !inside {
			s.ShowChapters = false
			return s, nil
		}
		if idx >= 0 && idx < len(s.Chapters) && s.Deps.Engine != nil {
			ch := s.Chapters[idx]
			s.ChapterIdx = idx
			s.ShowChapters = false
			if cp, ok := s.Deps.Engine.(chaptersProvider); ok {
				return s, s.engineCmd(func(ctx context.Context) error {
					return cp.SeekAbsolute(ctx, ch.Time)
				}, "⏩ Capítulo: "+ch.Title)
			}
		}
		return s, nil
	}
	if s.ShowSettings {
		idx, inside := s.settingsModalRowAt(x, y)
		if !inside {
			s.ShowSettings = false
			return s, nil
		}
		if idx >= 0 && idx < SettingsRowCount {
			s.SettingsIdx = idx
			return s.AdjustSetting(idx, 1)
		}
		return s, nil
	}

	// 1. Top Spotatui panels (y == 0..2)
	if y >= 0 && y <= 2 {
		sideW := s.topBrandWidth()
		if x < sideW {
			s.Focus = FocusMain
			s.View = ViewHome
			return s.SwitchTab(model.TabHome)
		}

		actionsW := 15
		searchPanelW := max(24, (s.Width-sideW)-actionsW)
		if x < sideW+searchPanelW {
			btnStart, btnEnd, _ := s.headerSearchBounds()
			// [r] reload button on far right inside search panel
			if x >= btnStart && x <= btnEnd {
				s.Focus = FocusMain
				s.Syncing = true
				return s, tea.Batch(s.loadFeedCmd(s.ActiveTab, true), s.ensureAnimTick())
			}
			if s.Focus != FocusSearch {
				s.openSearchFocus()
			}
			return s, nil
		}

		// Right actions panel: " [?] [S] [Q] "
		relX := x - (sideW + searchPanelW)
		switch {
		case relX <= 5:
			s.ShowHelp = !s.ShowHelp
			return s, nil
		case relX <= 9:
			s.ShowSettings = !s.ShowSettings
			return s, nil
		default:
			s.Close()
			return s, tea.Quit
		}
	}

	// Clicking outside top search panel blurs search focus
	if s.Focus == FocusSearch {
		s.Focus = FocusMain
		s.SearchSuggestions = nil
		s.SuggestIdx = -1
	}

	// 2. Bottom Player panel (y == s.Height-5 .. s.Height-1) when visible
	if s.HasPlayerBar() && y >= s.Height-5 {
		if y == s.Height-3 && s.PlayerStatus.Duration > 0 {
			posLen := len(formatClockApp(s.PlayerStatus.TimePos))
			durLen := len(formatClockApp(s.PlayerStatus.Duration))
			innerW := max(10, s.Width-2)
			barW := max(8, innerW-posLen-durLen-6)
			barStart := 4 + posLen
			if x >= barStart && x < barStart+barW {
				ratio := float64(x-barStart) / float64(max(1, barW-1))
				if ratio < 0 {
					ratio = 0
				}
				if ratio > 1 {
					ratio = 1
				}
				target := ratio * s.PlayerStatus.Duration
				return s.SeekAbsolutePos(target, "⏩ "+formatClockApp(target))
			}
		}
		if y == s.Height-2 {
			switch {
			case x >= 2 && x <= 5:
				return s.PrevTrack()
			case x >= 7 && x <= 11:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
			case x >= 13 && x <= 16:
				return s.NextTrack()
			case x >= 18 && x <= 23:
				return s.SeekRelative(-10)
			case x >= 25 && x <= 30:
				return s.SeekRelative(10)
			case x >= 32 && x <= 37:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.AddVolume(ctx, -5) }, "Vol -5%")
			case x >= 39 && x <= 44:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.AddVolume(ctx, 5) }, "Vol +5%")
			case x >= 46 && x <= 49:
				return s.ToggleSaveSelected()
			}
		}
		return s, nil
	}

	// Ignore panel top border (y == 3)
	if y < 4 {
		return s, nil
	}

	// 3. Sidebar / Mini-guide panel
	sideW := s.sidebarWidth()
	if sideW > 0 && x < sideW {
		if s.Width >= fullSidebarMinW {
			switch y {
			case 5:
				return s.SwitchTab(model.TabHome)
			case 6:
				return s.SwitchTab(model.TabMusic)
			case 7:
				return s.SwitchTab(model.TabSubscriptions)
			case 10:
				return s.SwitchTab(model.TabHistory)
			case 11:
				return s.SwitchTab(model.TabWatchLater)
			case 12:
				return s.SwitchTab(model.TabPlaylists)
			case 13:
				return s.SwitchTab(model.TabLiked)
			case 14:
				return s.SwitchTab(model.TabDownloads)
			}
			subIdx := y - 17
			if subIdx >= 0 && subIdx < len(s.Subscriptions) && subIdx < 4 {
				s.SearchDraft = s.Subscriptions[subIdx]
				return s.SubmitSearch()
			}
			s.Focus = FocusSidebar
			return s, nil
		}
		tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
		slot := (y - 4) / 3
		if slot >= 0 && slot < len(tabs) {
			return s.SwitchTab(tabs[slot])
		}
		s.Focus = FocusSidebar
		return s, nil
	}

	s.Focus = FocusMain
	mainX := s.mainOriginX()
	mainW := s.MainInnerWidth()
	if x < mainX || x >= mainX+mainW {
		return s, nil
	}

	// 4. Chips row inside Main panel (y == 4)
	if y == 4 {
		chips := HomeChips
		if s.View == ViewSearch {
			chips = SearchChips
		}
		cx := mainX + 1
		for i, c := range chips {
			cw := runewidth.StringWidth(c.Label) + 2 // Padding(0, 1)
			if x >= cx && x < cx+cw {
				if s.View == ViewSearch {
					s.SearchChip = i
					s.ResultIdx = 0
					return s, nil
				}
				s.HomeChip = i
				s.SelectedIdx = 0
				s.ScrollOffset = 0
				return s, s.maybeFetchLiveChip()
			}
			cx += cw + 1
		}
		return s, nil
	}

	// 5. Main content inside panel (y >= 5)
	if idx, ok := s.videoIndexAt(x, y, true); ok {
		if idx == s.ActiveListIndex() {
			return s.PlaySelected("auto")
		}
		s.SetSelection(idx)
	}
	return s, nil
}

func (s *State) videoIndexAt(x, y int, allowPartialBottom bool) (int, bool) {
	if y < 5 {
		return 0, false
	}
	mainX := s.mainOriginX()
	innerW := s.MainInnerWidth()
	if x < mainX || x >= mainX+innerW {
		return 0, false
	}
	items := s.VisibleItems()
	if len(items) == 0 {
		return 0, false
	}

	relY := y - 5
	bodyH := max(1, s.Height-6)
	if s.HasPlayerBar() {
		bodyH = max(1, s.Height-11)
	}
	if relY >= bodyH {
		return 0, false
	}

	if s.View == ViewSearch || s.View == ViewChannel {
		visibleCount := max(1, bodyH/searchRowH)
		rowOffset := relY / searchRowH
		if rowOffset < 0 || (!allowPartialBottom && rowOffset >= visibleCount) {
			return 0, false
		}
		startIdx := max(0, s.ActiveListScroll())
		idx := startIdx + rowOffset
		if idx >= 0 && idx < len(items) {
			return idx, true
		}
		return 0, false
	}

	cols := s.GridCols()
	stepW := s.gridCardWidth() + cardGap
	col := (x - mainX) / stepW
	if col < 0 || col >= cols {
		return 0, false
	}
	cH := s.gridCardHeight()
	visibleRows := max(1, bodyH/cH)
	rowOffset := relY / cH
	if rowOffset < 0 || (!allowPartialBottom && rowOffset >= visibleRows) {
		return 0, false
	}
	startRow := max(0, s.ScrollOffset)
	row := startRow + rowOffset
	idx := row*cols + col
	if idx >= 0 && idx < len(items) {
		return idx, true
	}
	return 0, false
}

// QualityModalBounds returns the screen bounding rectangle (topX, topY, boxW, boxH)
// and visible option range for the Quality/Audio-Only picker modal.
func (s *State) QualityModalBounds() (leftX, topY, boxW, boxH, optionStartY int) {
	boxW = min(68, max(38, s.Width-8))
	// 1 title + 1 spacer + N options + 1 spacer + 1 footer = len(QualityOptions) + 4 lines;
	// th.Toast RoundedBorder adds +2 rows.
	boxH = len(QualityOptions) + 6
	leftX = (s.Width - boxW) / 2
	topY = (s.Height - boxH) / 2
	optionStartY = topY + 3 // border(1) + title(1) + blank(1)
	return leftX, topY, boxW, boxH, optionStartY
}

func (s *State) qualityModalRowAt(x, y int) (idx int, inside bool) {
	leftX, topY, boxW, boxH, optStartY := s.QualityModalBounds()
	if x < leftX || x >= leftX+boxW || y < topY || y >= topY+boxH {
		return -1, false
	}
	optIdx := y - optStartY
	if optIdx >= 0 && optIdx < len(QualityOptions) {
		return optIdx, true
	}
	return -1, true
}

// ChaptersModalWindow computes the visible slice and exact screen placement of Chapters modal.
func (s *State) ChaptersModalWindow() (start, end, boxW, leftX, topY, boxH, optStartY int) {
	maxVisible := max(4, min(len(s.Chapters), s.Height-10))
	start = 0
	if s.ChapterIdx >= maxVisible {
		start = s.ChapterIdx - maxVisible + 1
	}
	end = min(len(s.Chapters), start+maxVisible)
	boxW = min(66, max(34, s.Width-8))
	// title(1) + blank(1) + visible(end-start) + blank(1) + footer(1) + border(2)
	boxH = (end - start) + 6
	leftX = (s.Width - boxW) / 2
	topY = (s.Height - boxH) / 2
	optStartY = topY + 3
	return start, end, boxW, leftX, topY, boxH, optStartY
}

func (s *State) chaptersModalRowAt(x, y int) (idx int, inside bool) {
	start, end, boxW, leftX, topY, boxH, optStartY := s.ChaptersModalWindow()
	if x < leftX || x >= leftX+boxW || y < topY || y >= topY+boxH {
		return -1, false
	}
	rel := y - optStartY
	if rel >= 0 && start+rel < end {
		return start + rel, true
	}
	return -1, true
}

// QueueModalWindow computes the visible slice and exact screen placement of the Live Queue modal.
func (s *State) QueueModalWindow() (start, end, boxW, leftX, topY, boxH, optStartY int) {
	n := len(s.MPVPlaylist)
	maxVisible := max(4, min(n, s.Height-10))
	if n == 0 {
		maxVisible = 1
	}
	start = 0
	if s.QueueIdx >= maxVisible {
		start = s.QueueIdx - maxVisible + 1
	}
	end = min(n, start+maxVisible)
	visRows := max(1, end-start)
	boxW = min(72, max(38, s.Width-8))
	// title(1) + blank(1) + visible(visRows) + blank(1) + footer(1) + border(2)
	boxH = visRows + 6
	leftX = (s.Width - boxW) / 2
	topY = (s.Height - boxH) / 2
	optStartY = topY + 3
	return start, end, boxW, leftX, topY, boxH, optStartY
}

func (s *State) queueModalRowAt(x, y int) (idx int, inside bool) {
	start, end, boxW, leftX, topY, boxH, optStartY := s.QueueModalWindow()
	if x < leftX || x >= leftX+boxW || y < topY || y >= topY+boxH {
		return -1, false
	}
	rel := y - optStartY
	if rel >= 0 && start+rel < end {
		return start + rel, true
	}
	return -1, true
}

func (s *State) sidebarWidth() int {
	if s.Width >= fullSidebarMinW {
		return fullSidebarW
	}
	if s.Width >= miniGuideMinW {
		return miniGuideW
	}
	return 0
}

// LyricsSideWidth returns the width reserved for the side lyrics panel when active.
func (s *State) LyricsSideWidth() int {
	if !s.ShowLyrics || s.Width < miniGuideMinW {
		return 0
	}
	remW := s.Width - s.sidebarWidth()
	lyricsSideW := min(44, max(28, remW/3))
	if remW-lyricsSideW < 36 {
		return 0
	}
	return lyricsSideW
}

// MainInnerWidth returns the inner content width of the main center panel.
func (s *State) MainInnerWidth() int {
	remW := s.Width - s.sidebarWidth() - s.LyricsSideWidth()
	return max(10, remW-2)
}

func (s *State) mainOriginX() int {
	return s.sidebarWidth() + 1 // +1 for left border of Main panel
}

func (s *State) gridCardWidth() int {
	innerMainW := s.MainInnerWidth()
	cols := s.GridCols()
	if innerMainW <= 0 || cols <= 0 {
		return cardW
	}
	w := (innerMainW - (cols-1)*cardGap) / cols
	if w < cardW {
		if innerMainW < cardW {
			return innerMainW
		}
		return cardW
	}
	return w
}

func (s *State) gridCardHeight() int {
	innerW := max(4, s.gridCardWidth()-4)
	thumbH := max(7, min(11, innerW*9/32))
	return thumbH + 7
}

func (s *State) topBrandWidth() int {
	switch {
	case s.Width >= 140:
		return 26
	case s.Width >= 100:
		return 10
	default:
		return 0
	}
}

func (s *State) headerSearchBounds() (btnStart, btnEnd, barStart int) {
	sideW := s.topBrandWidth()
	searchPanelW := max(24, (s.Width-sideW)-15)
	// Inside top search panel: "... [r] │" -> [r] is 3 cols at [sideW+searchPanelW-5, sideW+searchPanelW-2]
	barStart = sideW + 1
	btnStart = sideW + searchPanelW - 5
	btnEnd = sideW + searchPanelW - 2
	return btnStart, btnEnd, barStart
}

func formatClockApp(seconds float64) string {
	if seconds <= 0 {
		return "00:00"
	}
	total := int(seconds)
	h := total / 3600
	m := (total % 3600) / 60
	sec := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}
