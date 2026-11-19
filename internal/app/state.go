package app

import (
	"context"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/store"
)

type metadataEnricher interface {
	EnrichMetadata(ctx context.Context, ids []string) (map[string]catalog.VideoMeta, error)
}

// ViewMode represents the active top-level screen.
type ViewMode int

const (
	ViewHome ViewMode = iota
	ViewSearch
	ViewChannel
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

// Downloader defines the video/audio download port.
type Downloader interface {
	Enqueue(ctx context.Context, v model.Video, audioOnly bool) error
}

// Dependencies holds the injected application ports.
type Dependencies struct {
	Catalog              catalog.Catalog
	Engine               player.Engine
	Store                store.Store
	Downloader           Downloader
	LyricsClient         *lyrics.Client
	InitialQuality       int
	InitialAudioOnly     bool
	InitialDefaultMode   string
	InitialVolume        int
	InitialPreview       string
	InitialHideShorts    *bool
	InitialInterleave    *bool
	InitialCookies       string
	InitialTheme         string
	InitialSponsorBlock  *bool
	AppVersion           string
	OnQualityChange      func(int) error
	OnAudioOnlyChange    func(bool) error
	OnThemeChange        func(string) error
	OnSponsorBlockChange func(bool) error
	OnConfigChange       func(key, val string) error
}

// State holds the pure application state decoupled from terminal rendering.
type State struct {
	Deps Dependencies

	View       ViewMode
	PrevView   ViewMode
	ActiveTab  model.Tab
	Focus      FocusArea
	HomeChip   int
	SearchChip int

	Feeds          map[model.Tab][]model.Video
	SearchResults  []model.Video
	SearchQuery    string
	SearchDraft    string
	SearchPending  bool
	ChannelTitle   string
	ChannelVideos  []model.Video
	ChannelIdx     int
	ChannelScroll  int
	ChannelPending bool
	Syncing        bool

	SelectedIdx  int
	ScrollOffset int
	ResultIdx    int
	ResultScroll int

	SavedIDs        map[string]bool
	LikedIDs        map[string]bool
	HistoryIDs      map[string]bool
	Subscriptions   []string
	Queue           []model.Video
	PlayStack       []model.Video
	CurrentPlaying  model.Video
	skipHistoryPush bool
	PlayerStatus    player.Status

	Toast   string
	ToastID int

	ShowHelp          bool
	ShowSettings      bool
	SettingsIdx       int
	ShowQualityModal  bool
	QualityIdx        int
	ShowStillWatching bool
	ShowChapters      bool
	Chapters          []player.Chapter
	ChapterIdx        int
	ShowLyrics        bool
	LyricsLoading     bool
	Lyrics            lyrics.TrackLyrics
	LyricsErr         string
	LyricsTrack       string
	LyricsScroll      int

	Autoplay     bool
	SponsorBlock bool
	AudioOnly    bool
	DefaultMode  string
	AudioVolume  int
	PreviewMode  string
	HideShorts   bool
	Interleave   bool
	CookiesMode  string
	ThemeName    string
	Quality      int
	AppVersion   string
	Width        int
	Height       int
	Ready        bool

	LiveLoading   map[model.Tab]bool
	metaRequested map[string]bool

	LastInteractionAt        time.Time
	AutoplayTracksSinceInput int
	lastTrackTitle           string

	PlayLoading      bool
	PlayLoadingTitle string
	playInFlight     bool
	AnimFrame        int
	animActive       bool

	searchCancel  context.CancelFunc
	searchSeq     uint64
	channelCancel context.CancelFunc
	channelSeq    uint64
	lyricsCancel  context.CancelFunc
	lyricsSeq     uint64
	statusCh      <-chan player.Status
	appCtx        context.Context
	appCancel     context.CancelFunc
}

// NewState constructs an initialized application State with injected ports.
func NewState(deps Dependencies) *State {
	ctx, cancel := context.WithCancel(context.Background())
	themeName := strings.TrimSpace(deps.InitialTheme)
	if themeName == "" {
		themeName = "youtube-dark"
	}
	sponsorBlock := true
	if deps.InitialSponsorBlock != nil {
		sponsorBlock = *deps.InitialSponsorBlock
	}
	hideShorts := true
	if deps.InitialHideShorts != nil {
		hideShorts = *deps.InitialHideShorts
	}
	interleave := true
	if deps.InitialInterleave != nil {
		interleave = *deps.InitialInterleave
	}
	defMode := strings.TrimSpace(deps.InitialDefaultMode)
	if defMode == "" {
		if deps.InitialAudioOnly {
			defMode = "audio"
		} else {
			defMode = "window"
		}
	}
	vol := deps.InitialVolume
	if vol <= 0 {
		vol = 75
	}
	preview := strings.TrimSpace(deps.InitialPreview)
	if preview == "" {
		preview = "auto"
	}
	cookiesMode := strings.TrimSpace(deps.InitialCookies)
	if cookiesMode == "" {
		cookiesMode = "auto"
	}
	return &State{
		Deps:              deps,
		View:              ViewHome,
		PrevView:          ViewHome,
		ActiveTab:         model.TabHome,
		Focus:             FocusMain,
		Autoplay:          true,
		SponsorBlock:      sponsorBlock,
		AudioOnly:         deps.InitialAudioOnly || defMode == "audio",
		DefaultMode:       defMode,
		AudioVolume:       vol,
		PreviewMode:       preview,
		HideShorts:        hideShorts,
		Interleave:        interleave,
		CookiesMode:       cookiesMode,
		ThemeName:         themeName,
		Quality:           deps.InitialQuality,
		AppVersion:        strings.TrimSpace(deps.AppVersion),
		Feeds:             make(map[model.Tab][]model.Video),
		SavedIDs:          make(map[string]bool),
		LikedIDs:          make(map[string]bool),
		HistoryIDs:        make(map[string]bool),
		LiveLoading:       make(map[model.Tab]bool),
		metaRequested:     make(map[string]bool),
		LastInteractionAt: time.Now(),
		appCtx:            ctx,
		appCancel:         cancel,
	}
}

// HasPlayerBar reports whether the bottom player bar should be visible.
func (s *State) HasPlayerBar() bool {
	return s.PlayerStatus.IsRunning || s.PlayLoading
}

// IsAnimating reports whether any loading/syncing animation is active.
func (s *State) IsAnimating() bool {
	return s.Syncing || s.SearchPending || s.PlayLoading || s.ChannelPending || s.LyricsLoading || s.LiveLoading[s.ActiveTab]
}

// RegisterUserActivity records a user interaction and resets the idle autoplay counter.
func (s *State) RegisterUserActivity() {
	s.LastInteractionAt = time.Now()
	s.AutoplayTracksSinceInput = 0
}

// Close releases background contexts and the player engine.
func (s *State) Close() {
	if s.searchCancel != nil {
		s.searchCancel()
	}
	if s.channelCancel != nil {
		s.channelCancel()
	}
	if s.lyricsCancel != nil {
		s.lyricsCancel()
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
	if s.View == ViewChannel {
		return s.ChannelVideos
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

// ActiveListIndex returns the cursor index for the active view.
func (s *State) ActiveListIndex() int {
	switch s.View {
	case ViewSearch:
		return s.ResultIdx
	case ViewChannel:
		return s.ChannelIdx
	default:
		return s.SelectedIdx
	}
}

// ActiveListScroll returns the scroll offset for the active view.
func (s *State) ActiveListScroll() int {
	switch s.View {
	case ViewSearch:
		return s.ResultScroll
	case ViewChannel:
		return s.ChannelScroll
	default:
		return s.ScrollOffset
	}
}

// SelectedVideo returns the currently highlighted video in the active view.
func (s *State) SelectedVideo() (model.Video, bool) {
	items := s.VisibleItems()
	idx := s.ActiveListIndex()
	if idx < 0 || idx >= len(items) {
		return model.Video{}, false
	}
	return items[idx], true
}

type streamPrefetcher interface {
	Prefetch(v model.Video)
}

// BodyHeight returns the vertical height in rows available for cards/rows in the main panel.
func (s *State) BodyHeight() int {
	if s.Height <= 0 {
		return 0
	}
	if s.HasPlayerBar() {
		return max(1, s.Height-11)
	}
	return max(1, s.Height-6)
}

// VisibleGridRows returns the number of complete card rows visible in the home grid.
func (s *State) VisibleGridRows() int {
	bh := s.BodyHeight()
	if bh <= 0 {
		return 0
	}
	return max(1, bh/s.gridCardHeight())
}

// VisibleListRows returns the number of complete rows visible in search/channel views.
func (s *State) VisibleListRows() int {
	bh := s.BodyHeight()
	if bh <= 0 {
		return 0
	}
	return max(1, bh/searchRowH)
}

func (s *State) syncScrollToSelection() {
	if s.Height <= 0 {
		return
	}
	switch s.View {
	case ViewSearch:
		vis := s.VisibleListRows()
		if vis <= 0 {
			return
		}
		if s.ResultIdx < s.ResultScroll {
			s.ResultScroll = s.ResultIdx
		} else if s.ResultIdx >= s.ResultScroll+vis {
			s.ResultScroll = s.ResultIdx - vis + 1
		}
		if s.ResultScroll < 0 {
			s.ResultScroll = 0
		}
	case ViewChannel:
		vis := s.VisibleListRows()
		if vis <= 0 {
			return
		}
		if s.ChannelIdx < s.ChannelScroll {
			s.ChannelScroll = s.ChannelIdx
		} else if s.ChannelIdx >= s.ChannelScroll+vis {
			s.ChannelScroll = s.ChannelIdx - vis + 1
		}
		if s.ChannelScroll < 0 {
			s.ChannelScroll = 0
		}
	default:
		cols := s.GridCols()
		if cols <= 0 {
			cols = 1
		}
		vis := s.VisibleGridRows()
		if vis <= 0 {
			return
		}
		selRow := s.SelectedIdx / cols
		if selRow < s.ScrollOffset {
			s.ScrollOffset = selRow
		} else if selRow >= s.ScrollOffset+vis {
			s.ScrollOffset = selRow - vis + 1
		}
		if s.ScrollOffset < 0 {
			s.ScrollOffset = 0
		}
	}
}

// SetSelection updates the active cursor clamped to bounds.
func (s *State) SetSelection(idx int) {
	items := s.VisibleItems()
	if len(items) == 0 {
		switch s.View {
		case ViewSearch:
			s.ResultIdx = 0
			s.ResultScroll = 0
		case ViewChannel:
			s.ChannelIdx = 0
			s.ChannelScroll = 0
		default:
			s.SelectedIdx = 0
			s.ScrollOffset = 0
		}
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	switch s.View {
	case ViewSearch:
		s.ResultIdx = idx
	case ViewChannel:
		s.ChannelIdx = idx
	default:
		s.SelectedIdx = idx
	}
	s.syncScrollToSelection()
	if p, ok := s.Deps.Engine.(streamPrefetcher); ok {
		p.Prefetch(items[idx])
		if idx+1 < len(items) {
			p.Prefetch(items[idx+1])
		}
		if idx+2 < len(items) {
			p.Prefetch(items[idx+2])
		}
	}
}

// HoverSelection updates the active cursor from mouse hover without triggering
// stream prefetch storms across items as the cursor glides over the grid/list.
func (s *State) HoverSelection(idx int) bool {
	items := s.VisibleItems()
	if len(items) == 0 {
		return false
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	if idx == s.ActiveListIndex() {
		return false
	}
	switch s.View {
	case ViewSearch:
		s.ResultIdx = idx
	case ViewChannel:
		s.ChannelIdx = idx
	default:
		s.SelectedIdx = idx
	}
	return true
}

func (s *State) setToast(msg string) int {
	s.Toast = strings.TrimSpace(msg)
	s.ToastID++
	return s.ToastID
}
