package app

import (
	"context"
	"strings"

	"github.com/e12jassir/ytx/internal/catalog"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/store"
)

// ViewMode represents the active top-level screen.
type ViewMode int

const (
	ViewHome ViewMode = iota
	ViewSearch
)

// FocusArea identifies which region currently receives keyboard input.
type FocusArea int

const (
	FocusMain FocusArea = iota
	FocusSearch
	FocusSidebar
	FocusPlayer
)

// Chip defines a local filter pill over loaded videos.
type Chip struct {
	Label string
	Match func(v model.Video, s *State) bool
}

// HomeChips are the filter pills available in the Home/Music grid view.
var HomeChips = []Chip{
	{Label: "Todo", Match: func(model.Video, *State) bool { return true }},
	{Label: "En vivo", Match: func(v model.Video, _ *State) bool { return v.IsLive }},
	{Label: "Guardados", Match: func(v model.Video, s *State) bool { return s.SavedIDs[v.ID] }},
	{Label: "Vistos", Match: func(v model.Video, s *State) bool { return s.HistoryIDs[v.ID] }},
}

// SearchChips are the filter pills available in the Search results list view.
var SearchChips = []Chip{
	{Label: "Todo", Match: func(model.Video, *State) bool { return true }},
	{Label: "Videos", Match: func(v model.Video, _ *State) bool { return !v.IsLive }},
	{Label: "En vivo", Match: func(v model.Video, _ *State) bool { return v.IsLive }},
	{Label: "No vistos", Match: func(v model.Video, s *State) bool { return !s.HistoryIDs[v.ID] }},
}

// Dependencies holds the injected application ports.
type Dependencies struct {
	Catalog catalog.Catalog
	Engine  player.Engine
	Store   store.Store
}

// State holds the pure application state decoupled from terminal rendering.
type State struct {
	Deps Dependencies

	View       ViewMode
	ActiveTab  model.Tab
	Focus      FocusArea
	HomeChip   int
	SearchChip int

	Feeds         map[model.Tab][]model.Video
	SearchResults []model.Video
	SearchQuery   string
	SearchDraft   string
	SearchPending bool
	Syncing       bool

	SelectedIdx  int
	ScrollOffset int
	ResultIdx    int
	ResultScroll int

	SavedIDs      map[string]bool
	HistoryIDs    map[string]bool
	Subscriptions []string
	Queue         []model.Video
	PlayerStatus  player.Status

	Toast   string
	ToastID int

	ShowHelp     bool
	ShowSettings bool
	Width        int
	Height       int
	Ready        bool

	searchCancel context.CancelFunc
	appCtx       context.Context
	appCancel    context.CancelFunc
}

// NewState constructs an initialized application State with injected ports.
func NewState(deps Dependencies) *State {
	ctx, cancel := context.WithCancel(context.Background())
	return &State{
		Deps:       deps,
		View:       ViewHome,
		ActiveTab:  model.TabHome,
		Focus:      FocusMain,
		Feeds:      make(map[model.Tab][]model.Video),
		SavedIDs:   make(map[string]bool),
		HistoryIDs: make(map[string]bool),
		appCtx:     ctx,
		appCancel:  cancel,
	}
}

// Close releases background contexts and the player engine.
func (s *State) Close() {
	if s.searchCancel != nil {
		s.searchCancel()
	}
	if s.appCancel != nil {
		s.appCancel()
	}
	if s.Deps.Engine != nil {
		_ = s.Deps.Engine.Close()
	}
}

// VisibleItems returns the current list of videos filtered by the active chip.
func (s *State) VisibleItems() []model.Video {
	if s.View == ViewSearch {
		return filterByChip(s.SearchResults, SearchChips, s.SearchChip, s)
	}
	raw := s.Feeds[s.ActiveTab]
	if s.ActiveTab == model.TabHome || s.ActiveTab == model.TabMusic {
		return filterByChip(raw, HomeChips, s.HomeChip, s)
	}
	return raw
}

func filterByChip(items []model.Video, chips []Chip, idx int, s *State) []model.Video {
	if idx <= 0 || idx >= len(chips) {
		return items
	}
	fn := chips[idx].Match
	out := make([]model.Video, 0, len(items))
	for _, v := range items {
		if fn(v, s) {
			out = append(out, v)
		}
	}
	return out
}

// SelectedVideo returns the currently highlighted video in the active view.
func (s *State) SelectedVideo() (model.Video, bool) {
	items := s.VisibleItems()
	idx := s.SelectedIdx
	if s.View == ViewSearch {
		idx = s.ResultIdx
	}
	if idx < 0 || idx >= len(items) {
		return model.Video{}, false
	}
	return items[idx], true
}

// SetSelection updates the active cursor clamped to bounds.
func (s *State) SetSelection(idx int) {
	items := s.VisibleItems()
	if len(items) == 0 {
		if s.View == ViewSearch {
			s.ResultIdx = 0
		} else {
			s.SelectedIdx = 0
		}
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	if s.View == ViewSearch {
		s.ResultIdx = idx
	} else {
		s.SelectedIdx = idx
	}
}

func (s *State) setToast(msg string) int {
	s.Toast = strings.TrimSpace(msg)
	s.ToastID++
	return s.ToastID
}
