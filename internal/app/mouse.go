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
	fullSidebarW    = 22
	miniGuideW      = 8
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

	// 1. Header row (y == 0)
	if y == 0 {
		// Left logo area: return home
		if x < 10 {
			s.Focus = FocusMain
			return s.SwitchTab(model.TabHome)
		}
		// Right action buttons: [⟳] [?] [E]
		if x >= s.Width-12 && x < s.Width-7 {
			s.Focus = FocusMain
			s.Syncing = true
			return s, s.loadFeedCmd(s.ActiveTab, true)
		}
		if x >= s.Width-7 && x < s.Width-3 {
			s.ShowHelp = !s.ShowHelp
			return s, nil
		}

		// Search input + "Buscar" button in the center of header
		searchBtnStart, searchBtnEnd, searchBarStart := s.headerSearchBounds()
		if x >= searchBtnStart && x <= searchBtnEnd {
			if s.SearchDraft == "" && s.SearchQuery != "" {
				s.SearchDraft = s.SearchQuery
			}
			return s.SubmitSearch()
		}
		if x >= searchBarStart && x < searchBtnStart {
			s.Focus = FocusSearch
			return s, nil
		}
		return s, nil
	}

	// Clicking outside header blurs search focus
	if s.Focus == FocusSearch {
		s.Focus = FocusMain
	}

	// 2. Player bar (y == s.Height - 2)
	if y == s.Height-2 && x < 6 {
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
	}

	// Ignore divider / status bar rows
	if y < 2 || y >= s.Height-2 {
		return s, nil
	}

	// 3. Sidebar / Mini-guide area
	mainX := s.mainOriginX()
	if mainX > 0 && x < mainX-1 {
		if s.Width >= fullSidebarMinW {
			switch y {
			case 3:
				return s.SwitchTab(model.TabHome)
			case 4:
				return s.SwitchTab(model.TabMusic)
			case 5:
				return s.SwitchTab(model.TabSubscriptions)
			case 8:
				return s.SwitchTab(model.TabHistory)
			case 9:
				return s.SwitchTab(model.TabDownloads)
			}
			// Clicking a subscribed channel searches that channel
			subIdx := y - 12
			if subIdx >= 0 && subIdx < len(s.Subscriptions) {
				s.SearchDraft = s.Subscriptions[subIdx]
				return s.SubmitSearch()
			}
			return s, nil
		}
		// Mini-guide (3 rows per tab starting at y=2)
		tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
		slot := (y - 2) / 3
		if slot >= 0 && slot < len(tabs) {
			return s.SwitchTab(tabs[slot])
		}
		return s, nil
	}

	if x < mainX {
		return s, nil
	}

	// 4. Chips row (y == 2)
	if y == 2 {
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

	// 5. Main content (y >= 3)
	items := s.VisibleItems()
	if len(items) == 0 {
		return s, nil
	}

	relY := y - 3
	if s.View == ViewSearch {
		bodyH := s.Height - 6
		if bodyH < 1 {
			bodyH = 1
		}
		visibleCount := max(1, bodyH/searchRowH)
		startIdx := s.ResultScroll
		if s.ResultIdx < startIdx {
			startIdx = s.ResultIdx
		}
		if s.ResultIdx >= startIdx+visibleCount {
			startIdx = s.ResultIdx - visibleCount + 1
		}
		idx := startIdx + relY/searchRowH
		if idx >= 0 && idx < len(items) {
			if idx == s.ResultIdx {
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
	bodyH := s.Height - 6
	if bodyH < 1 {
		bodyH = 1
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

func (s *State) mainOriginX() int {
	if s.Width >= fullSidebarMinW {
		return fullSidebarW + 1
	}
	if s.Width >= miniGuideMinW {
		return miniGuideW + 1
	}
	return 0
}

func (s *State) gridCardWidth() int {
	mainW := s.Width - s.mainOriginX()
	cols := s.GridCols()
	if mainW <= 0 || cols <= 0 {
		return cardW
	}
	w := (mainW - (cols-1)*cardGap) / cols
	if w < cardW {
		if mainW < cardW {
			return mainW
		}
		return cardW
	}
	return w
}

func (s *State) headerSearchBounds() (btnStart, btnEnd, barStart int) {
	leftW := 8 // " ▶  ytx"
	if s.Width < miniGuideMinW {
		leftW = 36
	}
	rightW := 12 // "⟳  ?  E "
	searchBtnW := 8
	searchW := min(54, max(14, s.Width-leftW-rightW-searchBtnW-6))
	totalSearchW := searchW + searchBtnW
	padW := max(1, (s.Width-leftW-totalSearchW-rightW)/2)
	barStart = leftW + padW
	btnStart = barStart + searchW
	btnEnd = btnStart + searchBtnW - 1
	return btnStart, btnEnd, barStart
}
