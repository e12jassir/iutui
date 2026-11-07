package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/data"
	"github.com/e12jassir/ytx-cli/internal/model"
	"github.com/e12jassir/ytx-cli/internal/player"
)

// Update handles incoming Bubble Tea messages and state transitions.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Ready = true
		return m, nil

	case TickMsg:
		// Poll mpv IPC status
		st, err := player.GetStatus()
		if err == nil {
			m.PlayerStatus = st
		}
		// Schedule next status tick
		return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return TickMsg(t)
		})

	case tea.KeyMsg:
		// Global quit
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.Focus == FocusSearch {
			return m.updateSearch(msg)
		}

		switch msg.String() {
		case "q":
			return m, tea.Quit

		case "/":
			m.Focus = FocusSearch
			m.Searching = true
			m.SearchQuery = ""
			return m, nil

		case "tab":
			if m.Focus == FocusGrid {
				m.Focus = FocusSidebar
			} else {
				m.Focus = FocusGrid
			}
			return m, nil

		case "space":
			_ = player.TogglePause()
			return m, nil

		case "[":
			_ = player.Seek(-10)
			return m, nil

		case "]":
			_ = player.Seek(10)
			return m, nil

		case "r":
			// Reload feed
			vids, err := data.LoadFeed(m.ActiveTab)
			if err == nil && len(vids) > 0 {
				m.Feeds[m.ActiveTab] = vids
				m.Grid = NewGridState(vids)
				m.StatusMessage = fmt.Sprintf("↻ Recargados %d videos de %s", len(vids), m.ActiveTab.String())
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

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.Focus = FocusGrid
		m.Searching = false
		return m, nil

	case "enter":
		m.Focus = FocusGrid
		m.Searching = false
		if m.SearchQuery != "" {
			m.StatusMessage = fmt.Sprintf("Buscando: %s...", m.SearchQuery)
			// Filter current feed or search
			var matched []model.Video
			for _, v := range m.Feeds[m.ActiveTab] {
				q := m.SearchQuery
				if containsIgnoreCase(v.Title, q) || containsIgnoreCase(v.Channel, q) {
					matched = append(matched, v)
				}
			}
			if len(matched) > 0 {
				m.Grid = NewGridState(matched)
				m.StatusMessage = fmt.Sprintf("Resultados para '%s' (%d videos)", m.SearchQuery, len(matched))
			} else {
				m.StatusMessage = fmt.Sprintf("Sin coincidencias locales para '%s'", m.SearchQuery)
			}
		}
		return m, nil

	case "backspace":
		if len(m.SearchQuery) > 0 {
			m.SearchQuery = m.SearchQuery[:len(m.SearchQuery)-1]
		}
		return m, nil

	default:
		if len(msg.String()) == 1 {
			m.SearchQuery += msg.String()
		}
		return m, nil
	}
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(substr) == 0 ||
			(len(s) > 0 && len(substr) > 0 &&
				(stringContains(stringsToLower(s), stringsToLower(substr)))))
}

func stringsToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
			mode := "window"
			if m.ActiveTab == model.TabMusic {
				mode = "audio"
			}
			err := player.PlayVideo(v, mode)
			if err != nil {
				m.StatusMessage = fmt.Sprintf("Error iniciando reproductor: %v", err)
			} else {
				m.StatusMessage = fmt.Sprintf("▶ Lanzando mpv: %s", v.Title)
			}
		}
	}

	return m, nil
}

func (m *Model) switchTab(tab model.Tab) {
	m.ActiveTab = tab
	videos := m.Feeds[tab]
	if len(videos) == 0 {
		vids, err := data.LoadFeed(tab)
		if err == nil {
			videos = vids
			m.Feeds[tab] = vids
		}
	}
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
