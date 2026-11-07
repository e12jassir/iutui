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
	FocusGrid FocusArea = iota
	FocusSidebar
	FocusSearch
)

// TickMsg fires periodically to poll mpv playback status.
type TickMsg time.Time

// Model represents the main Bubble Tea application state for ytx-cli.
type Model struct {
	ActiveTab     model.Tab
	Focus         FocusArea
	Grid          GridState
	Feeds         map[model.Tab][]model.Video
	Width         int
	Height        int
	StatusMessage string
	SearchQuery   string
	Searching     bool
	PlayerStatus  player.Status
	Ready         bool
}

// InitialModel constructs the default state with initial feeds.
func InitialModel(initialFeeds map[model.Tab][]model.Video) Model {
	homeVideos := initialFeeds[model.TabHome]
	return Model{
		ActiveTab:     model.TabHome,
		Focus:         FocusGrid,
		Grid:          NewGridState(homeVideos),
		Feeds:         initialFeeds,
		StatusMessage: "Usa Tab para cambiar de panel · Enter reproducir · Espacio pausa · / buscar",
	}
}

// Init implements tea.Model and starts the background status poller.
func (m Model) Init() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
