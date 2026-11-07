package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/model"
)

// FocusArea denotes which subview currently receives keyboard input.
type FocusArea int

const (
	FocusGrid FocusArea = iota
	FocusSidebar
)

// Model represents the main Bubble Tea application state for ytx-cli.
type Model struct {
	ActiveTab     model.Tab
	Focus         FocusArea
	Grid          GridState
	Feeds         map[model.Tab][]model.Video
	Width         int
	Height        int
	StatusMessage string
	PlayingVideo  *model.Video
	IsPlaying     bool
	Ready         bool
}

// InitialModel constructs the default state with sample or empty feeds.
func InitialModel(initialFeeds map[model.Tab][]model.Video) Model {
	homeVideos := initialFeeds[model.TabHome]
	return Model{
		ActiveTab:     model.TabHome,
		Focus:         FocusGrid,
		Grid:          NewGridState(homeVideos),
		Feeds:         initialFeeds,
		StatusMessage: "Listo. Usa Tab para cambiar panel, h/j/k/l para navegar.",
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}
