package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/mattn/go-runewidth"
)

const (
	fullSidebarMinW = 140
	miniGuideMinW   = 100
	fullSidebarW    = 26
	miniGuideW      = 10
	cardW           = 30
	cardGap         = 2
	cardH           = 14
	searchRowH      = 9
)

func (s *State) handleMouse(msg tea.MouseMsg) (*State, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() + step)
		return s, nil
	case tea.MouseButtonWheelUp:
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() - step)
		return s, nil
	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			return s.handleLeftClick(msg.X, msg.Y)
		}
	}
	return s, nil
}

func (s *State) handleLeftClick(x, y int) (*State, tea.Cmd) {
	if s.ShowHelp {
		s.ShowHelp = false
		return s, nil
	}
	if s.ShowSettings {
		s.ShowSettings = false
		return s, nil
	}
	if s.ShowChapters {
		s.ShowChapters = false
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
				s.Focus = FocusSearch
				s.SearchDraft = ""
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
	}

	// 2. Bottom Player panel (y == s.Height-5 .. s.Height-1) when visible
	if s.HasPlayerBar() && y >= s.Height-5 {
		if y == s.Height-2 {
			switch {
			case x >= 2 && x <= 5:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Prev(ctx) }, "⏮ Anterior")
			case x >= 7 && x <= 11:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
			case x >= 13 && x <= 16:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Next(ctx) }, "⏭ Siguiente")
			case x >= 18 && x <= 23:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Seek(ctx, -10) }, "-10s")
			case x >= 25 && x <= 30:
				return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Seek(ctx, 10) }, "+10s")
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
		s.Focus = FocusSidebar
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
			return s, nil
		}
		tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
		slot := (y - 4) / 3
		if slot >= 0 && slot < len(tabs) {
			return s.SwitchTab(tabs[slot])
		}
		return s, nil
	}

	s.Focus = FocusMain
	mainX := s.mainOriginX()
	if x < mainX {
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
				} else {
					s.HomeChip = i
					s.SelectedIdx = 0
					s.ScrollOffset = 0
				}
				return s, nil
			}
			cx += cw + 1
		}
		return s, nil
	}

	// 5. Main content inside panel (y >= 5)
	items := s.VisibleItems()
	if len(items) == 0 {
		return s, nil
	}

	relY := y - 5
	bodyH := max(1, s.Height-6)
	if s.HasPlayerBar() {
		bodyH = max(1, s.Height-11)
	}
	if relY >= bodyH {
		return s, nil
	}
	if s.View == ViewSearch || s.View == ViewChannel {
		visibleCount := max(1, bodyH/searchRowH)
		curIdx := s.ActiveListIndex()
		startIdx := s.ActiveListScroll()
		if curIdx < startIdx {
			startIdx = curIdx
		}
		if curIdx >= startIdx+visibleCount {
			startIdx = curIdx - visibleCount + 1
		}
		idx := startIdx + relY/searchRowH
		if idx >= 0 && idx < len(items) {
			if idx == curIdx {
				return s.PlaySelected("auto")
			}
			s.SetSelection(idx)
		}
		return s, nil
	}

	// Home grid
	cols := s.GridCols()
	stepW := s.gridCardWidth() + cardGap
	col := (x - mainX) / stepW
	if col < 0 || col >= cols {
		return s, nil
	}
	visibleRows := max(1, bodyH/cardH)
	selRow := s.SelectedIdx / cols
	startRow := s.ScrollOffset
	if selRow < startRow {
		startRow = selRow
	}
	if selRow >= startRow+visibleRows {
		startRow = selRow - visibleRows + 1
	}
	if startRow < 0 {
		startRow = 0
	}

	row := startRow + relY/cardH
	idx := row*cols + col
	if idx >= 0 && idx < len(items) {
		if idx == s.SelectedIdx {
			return s.PlaySelected("auto")
		}
		s.SetSelection(idx)
	}
	return s, nil
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

func (s *State) mainOriginX() int {
	return s.sidebarWidth() + 1 // +1 for left border of Main panel
}

func (s *State) gridCardWidth() int {
	innerMainW := s.Width - s.sidebarWidth() - 2
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
