package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/model"
	"github.com/e12jassir/ytx-cli/internal/player"
)

// FocusArea denotes which subview currently receives keyboard input.
type FocusArea int

const (
	FocusSearch FocusArea = iota
	FocusSidebar
	FocusTable
	FocusPlayer
)

// SearchResultMsg arrives when an asynchronous YouTube search completes.
type SearchResultMsg struct {
	Query  string
	Videos []model.Video
	Err    error
}

// TickMsg fires periodically to poll mpv playback status.
type TickMsg time.Time

// Model represents the main Bubble Tea application state for ytx-cli.
type Model struct {
	ActiveTab           model.Tab
	Focus               FocusArea
	InPlaylists         bool
	SelectedPlaylistIdx int
	Playlists           []string

	Table         TableState
	Feeds         map[model.Tab][]model.Video
	Width         int
	Height        int
	StatusMessage string
	SearchQuery   string
	Searching     bool
	SearchPending bool
	PlayerStatus  player.Status
	Ready         bool

	ShowHelp     bool
	ShowSettings bool
}

// InitialModel constructs the default Spotatui-style state with initial feeds.
func InitialModel(initialFeeds map[model.Tab][]model.Video) Model {
	homeVideos := initialFeeds[model.TabHome]
	defaultPlaylists := []string{
		"📌 Favoritos",
		"📻 Mix Radio",
		"🎸 Rock Clásico",
		"🎤 Canserbero Colección",
	}

	return Model{
		ActiveTab:           model.TabHome,
		Focus:               FocusTable,
		InPlaylists:         false,
		SelectedPlaylistIdx: 0,
		Playlists:           defaultPlaylists,
		Table:               NewTableState(homeVideos),
		Feeds:               initialFeeds,
		StatusMessage:       "Spotatui TUI lista · Clic con mouse o Tab para cambiar foco · Enter para reproducir",
	}
}

// Init starts the periodic mpv status poller.
func (m Model) Init() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
