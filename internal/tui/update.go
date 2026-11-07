package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/model"
)

// Update handles incoming Bubble Tea messages and state transitions.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "tab":
			// Toggle focus between Sidebar and Grid
			if m.Focus == FocusGrid {
				m.Focus = FocusSidebar
			} else {
				m.Focus = FocusGrid
			}
			return m, nil

		case "space":
			if m.PlayingVideo != nil {
				m.IsPlaying = !m.IsPlaying
				if m.IsPlaying {
					m.StatusMessage = fmt.Sprintf("▶ Reanudado: %s", m.PlayingVideo.Title)
				} else {
					m.StatusMessage = fmt.Sprintf("⏸ Pausado: %s", m.PlayingVideo.Title)
				}
			}
			return m, nil
		}

		if m.Focus == FocusSidebar {
			return m.updateSidebar(msg)
		}
		return m.updateGrid(msg)
	}

	return m, nil
}

func (m Model) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.ActiveTab < model.TabHistory {
			m.ActiveTab++
			m.switchTab(m.ActiveTab)
		}
	case "k", "up":
		if m.ActiveTab > model.TabHome {
			m.ActiveTab--
			m.switchTab(m.ActiveTab)
		}
	case "enter", "l", "right":
		m.Focus = FocusGrid
	}
	return m, nil
}

func (m Model) updateGrid(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cols := CalcCols(m.gridWidth())

	switch msg.String() {
	case "h", "left":
		_, col := m.Grid.CurrentCoords(cols)
		if col == 0 && msg.String() == "h" {
			// Jumping to sidebar when pressing 'h' on the first column
			m.Focus = FocusSidebar
			return m, nil
		}
		m.Grid.MoveLeft()

	case "l", "right":
		m.Grid.MoveRight()

	case "k", "up":
		m.Grid.MoveUp(cols)

	case "j", "down":
		m.Grid.MoveDown(cols)

	case "enter":
		if v, ok := m.Grid.SelectedVideo(); ok {
			vidCopy := v
			m.PlayingVideo = &vidCopy
			m.IsPlaying = true
			m.StatusMessage = fmt.Sprintf("▶ Reproduciendo: %s", v.Title)
		}
	}

	return m, nil
}

func (m *Model) switchTab(tab model.Tab) {
	m.ActiveTab = tab
	videos := m.Feeds[tab]
	m.Grid = NewGridState(videos)
	m.StatusMessage = fmt.Sprintf("Vista: %s (%d videos)", tab.String(), len(videos))
}

func (m Model) gridWidth() int {
	w := m.Width - SidebarWidth - 4
	if w < CardTotalWidth {
		return CardTotalWidth
	}
	return w
}

func (m Model) gridHeight() int {
	h := m.Height - 4 // reserve space for header and now playing footer
	if h < CardTotalHeight {
		return CardTotalHeight
	}
	return h
}
