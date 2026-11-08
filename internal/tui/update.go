package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/data"
	"github.com/e12jassir/ytx-cli/internal/model"
	"github.com/e12jassir/ytx-cli/internal/player"
)

// Update handles Bubble Tea messages: resize, IPC ticks, keyboard, and mouse events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Ready = true
		return m, nil

	case TickMsg:
		st, err := player.GetStatus()
		if err == nil {
			m.PlayerStatus = st
		}
		return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return TickMsg(t)
		})

	case SearchResultMsg:
		m.SearchPending = false
		if msg.Err == nil && len(msg.Videos) > 0 {
			m.Table = NewTableState(msg.Videos)
			m.StatusMessage = fmt.Sprintf("Resultados para '%s' (%d videos)", msg.Query, len(msg.Videos))
		} else if msg.Err != nil {
			m.StatusMessage = fmt.Sprintf("Error buscando '%s': %v", msg.Query, msg.Err)
		} else {
			m.StatusMessage = fmt.Sprintf("Sin resultados para '%s'", msg.Query)
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey processes keyboard input based on current active panel focus.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global exit
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// Help modal dismiss
	if m.ShowHelp {
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" {
			m.ShowHelp = false
			return m, nil
		}
		return m, nil
	}

	// Global help toggle
	if msg.String() == "?" && m.Focus != FocusSearch {
		m.ShowHelp = true
		return m, nil
	}

	// Global search jump
	if msg.String() == "/" && m.Focus != FocusSearch {
		m.Focus = FocusSearch
		m.Searching = true
		return m, nil
	}

	// Tab cycle between panels
	if msg.String() == "tab" {
		m.cycleFocus(true)
		return m, nil
	}
	if msg.String() == "shift+tab" {
		m.cycleFocus(false)
		return m, nil
	}

	// Focus-specific routing
	switch m.Focus {
	case FocusSearch:
		return m.updateSearch(msg)
	case FocusSidebar:
		return m.updateSidebar(msg)
	case FocusTable:
		return m.updateTable(msg)
	case FocusPlayer:
		return m.updatePlayer(msg)
	}

	return m, nil
}

func (m *Model) cycleFocus(forward bool) {
	if forward {
		switch m.Focus {
		case FocusSearch:
			m.Focus = FocusSidebar
		case FocusSidebar:
			m.Focus = FocusTable
		case FocusTable:
			m.Focus = FocusPlayer
		case FocusPlayer:
			m.Focus = FocusSearch
		}
	} else {
		switch m.Focus {
		case FocusSearch:
			m.Focus = FocusPlayer
		case FocusPlayer:
			m.Focus = FocusTable
		case FocusTable:
			m.Focus = FocusSidebar
		case FocusSidebar:
			m.Focus = FocusSearch
		}
	}
}

// updateSearch handles text entry in the top Spotatui search box.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.Focus = FocusTable
		m.Searching = false
		return m, nil

	case "enter":
		m.Focus = FocusTable
		m.Searching = false
		if m.SearchQuery != "" {
			m.SearchPending = true
			m.StatusMessage = fmt.Sprintf("Buscando en YouTube: '%s'...", m.SearchQuery)
			q := m.SearchQuery
			return m, func() tea.Msg {
				vids, err := data.SearchYouTube(q)
				return SearchResultMsg{
					Query:  q,
					Videos: vids,
					Err:    err,
				}
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

// updateSidebar navigates Library tabs and Playlists.
func (m Model) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tabs := DefaultLibraryTabs()

	switch msg.String() {
	case "j", "down":
		if !m.InPlaylists {
			if int(m.ActiveTab) < len(tabs)-1 {
				m.ActiveTab++
				m.switchTab(m.ActiveTab)
			} else {
				m.InPlaylists = true
				m.SelectedPlaylistIdx = 0
			}
		} else {
			if m.SelectedPlaylistIdx < len(m.Playlists)-1 {
				m.SelectedPlaylistIdx++
			}
		}

	case "k", "up":
		if m.InPlaylists {
			if m.SelectedPlaylistIdx > 0 {
				m.SelectedPlaylistIdx--
			} else {
				m.InPlaylists = false
				m.ActiveTab = tabs[len(tabs)-1]
				m.switchTab(m.ActiveTab)
			}
		} else {
			if m.ActiveTab > 0 {
				m.ActiveTab--
				m.switchTab(m.ActiveTab)
			}
		}

	case "enter", "l", "right":
		m.Focus = FocusTable

	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// updateTable navigates the tracks table and triggers playback.
func (m Model) updateTable(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visHeight := m.Height - 3 - 5 - 2
	if visHeight < 5 {
		visHeight = 5
	}

	switch msg.String() {
	case "j", "down":
		m.Table.MoveDown(visHeight)

	case "k", "up":
		m.Table.MoveUp(visHeight)

	case "g", "home":
		m.Table.SelectDirect(0, visHeight)

	case "G", "end":
		m.Table.SelectDirect(len(m.Table.Videos)-1, visHeight)

	case "h", "left":
		m.Focus = FocusSidebar

	case "enter":
		if v, ok := m.Table.SelectedVideo(); ok {
			mode := "auto"
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

	case "space":
		_ = player.TogglePause()

	case "[":
		_ = player.Seek(-10)

	case "]":
		_ = player.Seek(10)

	case "+", "=":
		_ = player.AddVolume(5)

	case "-":
		_ = player.AddVolume(-5)

	case "n":
		_ = player.Next()

	case "p":
		_ = player.Prev()

	case "r":
		m.switchTab(m.ActiveTab)

	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// updatePlayer controls playback from the bottom bar focus.
func (m Model) updatePlayer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "space":
		_ = player.TogglePause()
	case "[", "h", "left":
		_ = player.Seek(-10)
	case "]", "l", "right":
		_ = player.Seek(10)
	case "+", "k", "up":
		_ = player.AddVolume(5)
	case "-", "j", "down":
		_ = player.AddVolume(-5)
	case "n":
		_ = player.Next()
	case "p":
		_ = player.Prev()
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// handleMouse implements complete 2D mouse hit-testing for Spotatui panels and buttons.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	visHeight := m.Height - 3 - 5 - 2
	if visHeight < 5 {
		visHeight = 5
	}

	// 1. Mouse wheel scrolling
	if msg.Button == tea.MouseButtonWheelUp {
		if msg.X >= SidebarWidth {
			m.Table.MoveUp(visHeight)
		} else {
			if m.ActiveTab > 0 {
				m.ActiveTab--
				m.switchTab(m.ActiveTab)
			}
		}
		return m, nil
	}

	if msg.Button == tea.MouseButtonWheelDown {
		if msg.X >= SidebarWidth {
			m.Table.MoveDown(visHeight)
		} else {
			tabs := DefaultLibraryTabs()
			if int(m.ActiveTab) < len(tabs)-1 {
				m.ActiveTab++
				m.switchTab(m.ActiveTab)
			}
		}
		return m, nil
	}

	// 2. Left click interaction
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		helpWidth := 14
		settingsWidth := 18
		searchWidth := m.Width - helpWidth - settingsWidth

		// Top Bar (Y < 3)
		if msg.Y < 3 {
			if msg.X < searchWidth {
				m.Focus = FocusSearch
				m.Searching = true
			} else if msg.X >= searchWidth && msg.X < searchWidth+helpWidth {
				m.ShowHelp = !m.ShowHelp
			} else {
				m.ShowSettings = !m.ShowSettings
			}
			return m, nil
		}

		// Middle Section (3 <= Y < m.Height - 5)
		middleBottom := m.Height - 5
		if msg.Y >= 3 && msg.Y < middleBottom {
			if msg.X < SidebarWidth {
				// Left Sidebar clicked
				m.Focus = FocusSidebar
				tabs := DefaultLibraryTabs()
				libHeight := len(tabs) + 2

				// Click inside Library box
				if msg.Y >= 4 && msg.Y < 4+len(tabs) {
					clickedTabIdx := msg.Y - 4
					if clickedTabIdx >= 0 && clickedTabIdx < len(tabs) {
						m.InPlaylists = false
						m.ActiveTab = tabs[clickedTabIdx]
						m.switchTab(m.ActiveTab)
					}
				} else if msg.Y >= 3+libHeight+1 {
					// Click inside Playlists box
					clickedPIdx := msg.Y - (3 + libHeight + 1)
					if clickedPIdx >= 0 && clickedPIdx < len(m.Playlists) {
						m.InPlaylists = true
						m.SelectedPlaylistIdx = clickedPIdx
					}
				}
			} else {
				// Tracks Table clicked
				m.Focus = FocusTable
				// Header is line 0 of table body (Y = 4). First track starts at Y = 5.
				clickedRow := (msg.Y - 5) + m.Table.ScrollOffset
				if clickedRow >= 0 && clickedRow < len(m.Table.Videos) {
					if m.Table.SelectedIdx == clickedRow {
						// Double-click / re-click on selected track: PLAY!
						if v, ok := m.Table.SelectedVideo(); ok {
							mode := "auto"
							if m.ActiveTab == model.TabMusic {
								mode = "audio"
							}
							_ = player.PlayVideo(v, mode)
							m.StatusMessage = fmt.Sprintf("▶ Reproduciendo: %s", v.Title)
						}
					} else {
						m.Table.SelectDirect(clickedRow, visHeight)
					}
				}
			}
			return m, nil
		}

		// Bottom Playing Bar (Y >= m.Height - 5)
		if msg.Y >= middleBottom {
			m.Focus = FocusPlayer
			// Line 3 of player box (Y = m.Height - 2) has clickable control buttons
			if msg.Y == m.Height-2 {
				// Check X coordinate zones:
				// [Prev]   [Play/Pause]   [Next]   [Seek -10]   [Seek +10]   [Vol-]   [Vol+]
				if msg.X >= 2 && msg.X <= 9 {
					_ = player.Prev()
				} else if msg.X >= 11 && msg.X <= 26 {
					_ = player.TogglePause()
				} else if msg.X >= 27 && msg.X <= 36 {
					_ = player.Next()
				} else if msg.X >= 37 && msg.X <= 50 {
					_ = player.Seek(-10)
				} else if msg.X >= 51 && msg.X <= 64 {
					_ = player.Seek(10)
				} else if msg.X >= 65 && msg.X <= 73 {
					_ = player.AddVolume(-5)
				} else if msg.X >= 75 && msg.X <= 84 {
					_ = player.AddVolume(5)
				}
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) switchTab(tab model.Tab) {
	m.ActiveTab = tab
	m.SearchQuery = ""
	videos := m.Feeds[tab]
	if len(videos) == 0 {
		vids, err := data.LoadFeed(tab)
		if err == nil {
			videos = vids
			m.Feeds[tab] = vids
		}
	}
	m.Table = NewTableState(videos)
	m.StatusMessage = fmt.Sprintf("Vista: %s (%d videos)", tab.String(), len(videos))
}
