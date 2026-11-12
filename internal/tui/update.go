package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/data"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/player"
)

// FeedLoadedMsg arrives when an asynchronous feed load completes.
type FeedLoadedMsg struct {
	Tab    model.Tab
	Videos []model.Video
	Err    error
}

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

	case FeedLoadedMsg:
		if msg.Err == nil && len(msg.Videos) > 0 {
			m.Feeds[msg.Tab] = msg.Videos
			if m.ActiveTab == msg.Tab && !m.InPlaylists && m.SearchQuery == "" {
				m.Grid = NewGridState(msg.Videos)
				m.StatusMessage = fmt.Sprintf("Vista: %s (%d videos)", msg.Tab.String(), len(msg.Videos))
			}
		}
		return m, nil

	case SearchResultMsg:
		m.SearchPending = false
		if msg.Err == nil && len(msg.Videos) > 0 {
			m.Grid = NewGridState(msg.Videos)
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
	case FocusGrid:
		return m.updateGrid(msg)
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
			m.Focus = FocusGrid
		case FocusGrid:
			m.Focus = FocusPlayer
		case FocusPlayer:
			m.Focus = FocusSearch
		}
	} else {
		switch m.Focus {
		case FocusSearch:
			m.Focus = FocusPlayer
		case FocusPlayer:
			m.Focus = FocusGrid
		case FocusGrid:
			m.Focus = FocusSidebar
		case FocusSidebar:
			m.Focus = FocusSearch
		}
	}
}

// updateSearch handles UTF-8 text entry in the top Spotatui search box.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.Focus = FocusGrid
		m.Searching = false
		return m, nil

	case tea.KeyEnter:
		m.Focus = FocusGrid
		m.Searching = false
		q := strings.TrimSpace(m.SearchQuery)
		if q != "" {
			return m.triggerSearch(q)
		}
		return m, nil

	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.SearchQuery) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.SearchQuery)
			m.SearchQuery = m.SearchQuery[:len(m.SearchQuery)-size]
		}
		return m, nil

	case tea.KeySpace:
		m.SearchQuery += " "
		return m, nil

	case tea.KeyRunes:
		m.SearchQuery += string(msg.Runes)
		return m, nil
	}

	// Fallback for synthetic KeyMsg in tests
	s := msg.String()
	if s == "backspace" && len(m.SearchQuery) > 0 {
		_, size := utf8.DecodeLastRuneInString(m.SearchQuery)
		m.SearchQuery = m.SearchQuery[:len(m.SearchQuery)-size]
	} else if len(s) == 1 {
		m.SearchQuery += s
	}
	return m, nil
}

func (m Model) triggerSearch(q string) (tea.Model, tea.Cmd) {
	m.SearchPending = true
	m.StatusMessage = fmt.Sprintf("Buscando en YouTube: '%s'...", q)
	return m, func() tea.Msg {
		vids, err := data.SearchYouTube(q)
		return SearchResultMsg{
			Query:  q,
			Videos: vids,
			Err:    err,
		}
	}
}

// cleanPlaylistQuery strips leading emojis from playlist names so YouTube search is clean.
func cleanPlaylistQuery(name string) string {
	for _, prefix := range []string{"📌 ", "📻 ", "🎸 ", "🎤 "} {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
		}
	}
	if name == "Favoritos" {
		return "Vallenato Clásico Binomio Diomedes"
	}
	if name == "Mix Radio" {
		return "Soda Stereo Pink Floyd The Cure"
	}
	return strings.TrimSpace(name)
}

// updateSidebar navigates Library tabs and Playlists.
func (m Model) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tabs := DefaultLibraryTabs()

	switch msg.String() {
	case "j", "down":
		if !m.InPlaylists {
			if int(m.ActiveTab) < len(tabs)-1 {
				m.ActiveTab++
				return m.switchTabCmd(m.ActiveTab)
			}
			m.InPlaylists = true
			m.SelectedPlaylistIdx = 0
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
				return m.switchTabCmd(m.ActiveTab)
			}
		} else {
			if m.ActiveTab > 0 {
				m.ActiveTab--
				return m.switchTabCmd(m.ActiveTab)
			}
		}

	case "enter", "l", "right":
		m.Focus = FocusGrid
		if m.InPlaylists && m.SelectedPlaylistIdx >= 0 && m.SelectedPlaylistIdx < len(m.Playlists) {
			q := cleanPlaylistQuery(m.Playlists[m.SelectedPlaylistIdx])
			return m.triggerSearch(q)
		}

	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// updateGrid navigates the YouTube cards grid with thumbnails.
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
				m.StatusMessage = fmt.Sprintf("▶ Reproduciendo (%s): %s", mode, v.Title)
			}
		}

	case "a":
		if v, ok := m.Grid.SelectedVideo(); ok {
			_ = player.PlayVideo(v, "audio")
			m.StatusMessage = fmt.Sprintf("🎵 Reproduciendo audio: %s", v.Title)
		}

	case "v":
		if v, ok := m.Grid.SelectedVideo(); ok {
			_ = player.PlayVideo(v, "window")
			m.StatusMessage = fmt.Sprintf("▶ Forzando ventana de video: %s", v.Title)
		}

	case " ", "space":
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
		return m.switchTabCmd(m.ActiveTab)

	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// updatePlayer controls playback from the bottom bar focus.
func (m Model) updatePlayer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "v":
		if v, ok := m.Grid.SelectedVideo(); ok {
			_ = player.PlayVideo(v, "window")
			m.StatusMessage = fmt.Sprintf("▶ Forzando ventana de video: %s", v.Title)
		}

	case "a":
		if v, ok := m.Grid.SelectedVideo(); ok {
			_ = player.PlayVideo(v, "audio")
			m.StatusMessage = fmt.Sprintf("🎵 Reproduciendo audio: %s", v.Title)
		}

	case " ", "space":
		_ = player.TogglePause()
	case "[", "h", "left":
		_ = player.Seek(-10)
	case "]", "l", "right":
		_ = player.Seek(10)
	case "+", "=", "k", "up":
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

// handleMouse implements complete 2D mouse hit-testing for Spotatui panels, cards, and buttons.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	cols := CalcCols(m.gridWidth())

	// 1. Mouse wheel scrolling
	if msg.Button == tea.MouseButtonWheelUp {
		if msg.X >= SidebarWidth {
			m.Grid.MoveUp(cols)
		} else {
			if m.ActiveTab > 0 {
				m.ActiveTab--
				return m.switchTabCmd(m.ActiveTab)
			}
		}
		return m, nil
	}

	if msg.Button == tea.MouseButtonWheelDown {
		if msg.X >= SidebarWidth {
			m.Grid.MoveDown(cols)
		} else {
			tabs := DefaultLibraryTabs()
			if int(m.ActiveTab) < len(tabs)-1 {
				m.ActiveTab++
				return m.switchTabCmd(m.ActiveTab)
			}
		}
		return m, nil
	}

	// 2. Left click interaction
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		if m.ShowHelp {
			m.ShowHelp = false
			return m, nil
		}

		helpWidth := TopHelpWidth
		settingsWidth := TopSettingsWidth
		searchWidth := m.Width - helpWidth - settingsWidth

		// Top Bar (Y < 3)
		if msg.Y < 3 {
			if msg.X < searchWidth {
				m.Focus = FocusSearch
				m.Searching = true
			} else if msg.X >= searchWidth && msg.X < searchWidth+helpWidth {
				m.ShowHelp = !m.ShowHelp
			} else {
				return m, tea.Quit
			}
			return m, nil
		}

		// Middle Section (3 <= Y < m.Height - 5)
		middleBottom := m.Height - 5
		if msg.Y >= 3 && msg.Y < middleBottom {
			if msg.X < SidebarWidth {
				// Left Sidebar (YouTube Drawer) clicked
				// Y=3 is top border, Y=4 is " PRINCIPAL"
				// Y=5: Inicio, Y=6: Música, Y=7: Suscripciones
				// Y=8: divider, Y=9: " BIBLIOTECA"
				// Y=10: Historial, Y=11: Descargas
				// Y=12: divider, Y=13: " EXPLORAR MIXES"
				// Y=14..17: Playlists[0..3]
				m.Focus = FocusSidebar
				switch msg.Y {
				case 5:
					m.InPlaylists = false
					return m.switchTabCmd(model.TabHome)
				case 6:
					m.InPlaylists = false
					return m.switchTabCmd(model.TabMusic)
				case 7:
					m.InPlaylists = false
					return m.switchTabCmd(model.TabSubscriptions)
				case 10:
					m.InPlaylists = false
					return m.switchTabCmd(model.TabHistory)
				case 11:
					m.InPlaylists = false
					return m.switchTabCmd(model.TabDownloads)
				default:
					if msg.Y >= 14 {
						clickedPIdx := msg.Y - 14
						if clickedPIdx >= 0 && clickedPIdx < len(m.Playlists) {
							m.InPlaylists = true
							m.SelectedPlaylistIdx = clickedPIdx
							q := cleanPlaylistQuery(m.Playlists[clickedPIdx])
							return m.triggerSearch(q)
						}
					}
				}
			} else {
				// YouTube Cards Grid clicked (Y=3 is panel top border, so content starts at Y=4)
				m.Focus = FocusGrid
				relX := msg.X - SidebarWidth - 3
				relY := msg.Y - 4
				if relX < 0 {
					relX = 0
				}
				if relY < 0 {
					relY = 0
				}

				cardCol := relX / (CardTotalWidth + 2)
				cardRow := (relY / (CardTotalHeight + 1)) + m.Grid.ScrollRow

				if cardCol >= 0 && cardCol < cols && cardRow >= 0 {
					clickedIdx := cardRow*cols + cardCol
					if clickedIdx >= 0 && clickedIdx < len(m.Grid.Videos) {
						if m.Grid.SelectedIdx == clickedIdx {
							// Second click on selected video card: PLAY!
							mode := "window"
							if m.ActiveTab == model.TabMusic {
								mode = "audio"
							}
							_ = player.PlayVideo(m.Grid.Videos[clickedIdx], mode)
							m.StatusMessage = fmt.Sprintf("▶ Lanzando mpv (%s): %s", mode, m.Grid.Videos[clickedIdx].Title)
						} else {
							m.Grid.SelectedIdx = clickedIdx
						}
					}
				}
			}
			return m, nil
		}

		// Bottom Playing Bar (Y >= m.Height - 5)
		if msg.Y >= middleBottom {
			m.Focus = FocusPlayer
			if msg.Y == m.Height-2 {
				// Controls line: "  [⏮ Ant]  [⏯ Pausa]  [⏭ Sig]  [⏪ -10s]  [⏩ +10s]  [🔉 Vol-]  [🔊 Vol+]  [🗔 Video]"
				x := msg.X
				switch {
				case x >= 2 && x <= 10:
					_ = player.Prev()
				case x >= 11 && x <= 22:
					_ = player.TogglePause()
				case x >= 23 && x <= 32:
					_ = player.Next()
				case x >= 33 && x <= 44:
					_ = player.Seek(-10)
				case x >= 45 && x <= 56:
					_ = player.Seek(10)
				case x >= 57 && x <= 68:
					_ = player.AddVolume(-5)
				case x >= 69 && x <= 80:
					_ = player.AddVolume(5)
				case x >= 81 && x <= 93:
					if v, ok := m.Grid.SelectedVideo(); ok {
						_ = player.PlayVideo(v, "window")
						m.StatusMessage = fmt.Sprintf("▶ Abriendo ventana mpv: %s", v.Title)
					}
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
	m.Grid = NewGridState(videos)
	m.StatusMessage = fmt.Sprintf("Vista: %s (%d videos)", tab.String(), len(videos))
}

func (m Model) switchTabCmd(tab model.Tab) (tea.Model, tea.Cmd) {
	m.ActiveTab = tab
	m.SearchQuery = ""
	if videos, ok := m.Feeds[tab]; ok && len(videos) > 0 {
		m.Grid = NewGridState(videos)
		m.StatusMessage = fmt.Sprintf("Vista: %s (%d videos)", tab.String(), len(videos))
		return m, nil
	}
	m.StatusMessage = fmt.Sprintf("Cargando %s...", tab.String())
	return m, func() tea.Msg {
		vids, err := data.LoadFeed(tab)
		return FeedLoadedMsg{
			Tab:    tab,
			Videos: vids,
			Err:    err,
		}
	}
}

func (m Model) gridWidth() int {
	w := m.Width - SidebarWidth - 1
	if w < CardTotalWidth+2 {
		return CardTotalWidth + 2
	}
	return w
}
