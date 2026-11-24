package app

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/download"
	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/store"
)

type metadataEnricher interface {
	EnrichMetadata(ctx context.Context, ids []string) (map[string]catalog.VideoMeta, error)
}

// SuggestProvider resolves search autocomplete suggestions for a draft query.
type SuggestProvider interface {
	Suggest(ctx context.Context, query string) ([]string, error)
}

// SearchHistoryStore persists and retrieves recent search queries.
type SearchHistoryStore interface {
	List() ([]string, error)
	Record(query string) error
}

// ResumePositionStore persists playback positions for long-form videos.
type ResumePositionStore interface {
	GetPosition(videoID string) (float64, bool)
	SavePosition(videoID string, position, duration float64) error
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
	SuggestProvider      SuggestProvider
	SearchHistory        SearchHistoryStore
	ResumeStore          ResumePositionStore
	ListeningStats       *store.ListeningStats
	InitialQuality       int
	InitialAudioOnly     bool
	InitialDefaultMode   string
	InitialVolume        int
	InitialPreview       string
	InitialHideShorts    *bool
	InitialInterleave    *bool
	InitialCookies       string
	InitialTheme         string
	InitialLang          string
	InitialSponsorBlock  *bool
	InitialThumbQuality  string
	InitialAutoplay      *bool
	InitialSpeed         float64
	InitialFeedSize      int
	InitialSearchSize    int
	InitialCacheTTL      *int
	InitialDownloadAudio *bool
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

	Feeds              map[model.Tab][]model.Video
	SearchResults      []model.Video
	SearchQuery        string
	SearchDraft        string
	SearchCursor       int
	searchCursorSynced string
	SearchPending      bool
	ChannelTitle       string
	ChannelVideos      []model.Video
	ChannelIdx         int
	ChannelScroll      int
	ChannelPending     bool
	Syncing            bool

	SelectedIdx  int
	ScrollOffset int
	ResultIdx    int
	ResultScroll int

	SavedIDs           map[string]bool
	LikedIDs           map[string]bool
	HistoryIDs         map[string]bool
	Subscriptions      []string
	SubscriptionLabels map[string]string
	Queue              []model.Video
	PlayStack          []model.Video
	CurrentPlaying     model.Video
	skipHistoryPush    bool
	navigatingBack     bool
	lastPlaylistPos    int
	trackMaxPos        float64
	trackDur           float64
	PlayerStatus       player.Status

	Toast   string
	ToastID int

	ShowHelp            bool
	ShowSettings        bool
	SettingsIdx         int
	ShowQualityModal    bool
	QualityIdx          int
	ShowStillWatching   bool
	ShowChapters        bool
	Chapters            []player.Chapter
	ChapterIdx          int
	ShowLyrics          bool
	LyricsLoading       bool
	Lyrics              lyrics.TrackLyrics
	LyricsErr           string
	LyricsTrack         string
	LyricsScroll        int
	LyricsVideoID       string
	LyricsOffset        float64
	LyricsPreferSource  string
	ShowQueue           bool
	QueueIdx            int
	QueueScroll         int
	queueRefreshPending bool
	MPVPlaylist         []player.PlaylistItem
	SearchSuggestions   []string
	SuggestIdx          int
	DownloadProgress    map[string]download.Item

	Autoplay      bool
	SponsorBlock  bool
	AudioOnly     bool
	DefaultMode   string
	AudioVolume   int
	PreviewMode   string
	ThumbQuality  string
	HideShorts    bool
	Interleave    bool
	FeedSize      int
	SearchSize    int
	CacheTTL      int
	DownloadAudio bool
	CookiesMode   string
	ThemeName     string
	LangPref      string
	Lang          string
	Cat           *i18n.Catalog
	Quality       int
	AppVersion    string
	Width         int
	Height        int
	Ready         bool

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
	suggestCancel context.CancelFunc
	suggestSeq    uint64
	channelCancel context.CancelFunc
	channelSeq    uint64
	lyricsCancel  context.CancelFunc
	lyricsSeq     uint64
	statusCh      <-chan player.Status
	escapeGuard   EscapeGuard
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
	thumbQuality := normalizeThumbQuality(deps.InitialThumbQuality)
	autoplay := true
	if deps.InitialAutoplay != nil {
		autoplay = *deps.InitialAutoplay
	}
	speed := 1.0
	if deps.InitialSpeed > 0 {
		speed = clampSpeed(deps.InitialSpeed)
	}
	feedSize := 100
	if deps.InitialFeedSize > 0 {
		feedSize = deps.InitialFeedSize
	}
	searchSize := 40
	if deps.InitialSearchSize > 0 {
		searchSize = deps.InitialSearchSize
	}
	cacheTTL := 900
	if deps.InitialCacheTTL != nil && *deps.InitialCacheTTL >= 0 {
		cacheTTL = *deps.InitialCacheTTL
	}
	downloadAudio := false
	if deps.InitialDownloadAudio != nil {
		downloadAudio = *deps.InitialDownloadAudio
	}
	s := &State{
		Deps:               deps,
		View:               ViewHome,
		PrevView:           ViewHome,
		ActiveTab:          model.TabHome,
		Focus:              FocusMain,
		Autoplay:           autoplay,
		SponsorBlock:       sponsorBlock,
		AudioOnly:          deps.InitialAudioOnly || defMode == "audio",
		DefaultMode:        defMode,
		AudioVolume:        vol,
		PreviewMode:        preview,
		ThumbQuality:       thumbQuality,
		HideShorts:         hideShorts,
		Interleave:         interleave,
		FeedSize:           feedSize,
		SearchSize:         searchSize,
		CacheTTL:           cacheTTL,
		DownloadAudio:      downloadAudio,
		CookiesMode:        cookiesMode,
		ThemeName:          themeName,
		Quality:            deps.InitialQuality,
		AppVersion:         strings.TrimSpace(deps.AppVersion),
		Feeds:              make(map[model.Tab][]model.Video),
		SavedIDs:           make(map[string]bool),
		LikedIDs:           make(map[string]bool),
		HistoryIDs:         make(map[string]bool),
		SubscriptionLabels: make(map[string]string),
		LiveLoading:        make(map[model.Tab]bool),
		DownloadProgress:   make(map[string]download.Item),
		SuggestIdx:         -1,
		metaRequested:      make(map[string]bool),
		lastPlaylistPos:    -1,
		PlayerStatus:       player.Status{Speed: speed, PlaylistPos: -1},
		LastInteractionAt:  time.Now(),
		appCtx:             ctx,
		appCancel:          cancel,
	}
	langPref := strings.TrimSpace(deps.InitialLang)
	if langPref == "" {
		langPref = "auto"
	}
	s.SetLangPref(langPref)
	return s
}

// Catalog returns the active i18n catalog, falling back to Spanish if nil.
func (s *State) Catalog() *i18n.Catalog {
	if s.Cat != nil {
		return s.Cat
	}
	return i18n.For(i18n.LangES)
}

// SetLangPref updates LangPref, resolves Lang from the environment, and refreshes Cat.
func (s *State) SetLangPref(pref string) {
	p := strings.ToLower(strings.TrimSpace(pref))
	if p != "auto" && p != i18n.LangES && p != i18n.LangEN {
		p = "auto"
	}
	s.LangPref = p
	s.Lang = i18n.Resolve(p, os.Getenv)
	s.Cat = i18n.For(s.Lang)
}

func normalizeThumbQuality(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hq":
		return "hq"
	case "mq":
		return "mq"
	default:
		return "max"
	}
}

func clampSpeed(target float64) float64 {
	if target < 0.5 {
		target = 0.5
	}
	if target > 2.0 {
		target = 2.0
	}
	return math.Round(target*100) / 100
}

// SearchCursorPos returns the current rune cursor position within SearchDraft,
// snapping to the end if SearchDraft was modified externally without setSearchDraft.
func (s *State) SearchCursorPos() int {
	runes := []rune(s.SearchDraft)
	if s.searchCursorSynced != s.SearchDraft {
		s.SearchCursor = len(runes)
		s.searchCursorSynced = s.SearchDraft
	}
	if s.SearchCursor < 0 {
		s.SearchCursor = 0
	} else if s.SearchCursor > len(runes) {
		s.SearchCursor = len(runes)
	}
	return s.SearchCursor
}

func (s *State) setSearchDraft(draft string, cursor int) {
	n := len([]rune(draft))
	if cursor < 0 {
		cursor = 0
	} else if cursor > n {
		cursor = n
	}
	s.SearchDraft = draft
	s.SearchCursor = cursor
	s.searchCursorSynced = draft
}

// HasPlayerBar reports whether the bottom player bar should be visible.
// An idle WarmUp mpv process without an active track or playlist stays hidden.
func (s *State) HasPlayerBar() bool {
	if s.PlayLoading {
		return true
	}
	if !s.PlayerStatus.IsRunning {
		return false
	}
	return strings.TrimSpace(s.PlayerStatus.Title) != "" ||
		s.CurrentPlaying.ID != "" ||
		s.PlayerStatus.PlaylistCount > 0
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

// Close releases background contexts, persists any long-video resume position, and stops the player engine.
func (s *State) Close() {
	s.saveCurrentResumePosition()
	if s.searchCancel != nil {
		s.searchCancel()
	}
	if s.suggestCancel != nil {
		s.suggestCancel()
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

func (s *State) saveCurrentResumePosition() {
	if s == nil || s.Deps.ResumeStore == nil || s.CurrentPlaying.ID == "" {
		return
	}
	_ = s.Deps.ResumeStore.SavePosition(s.CurrentPlaying.ID, s.PlayerStatus.TimePos, s.PlayerStatus.Duration)
}

var shortsTagRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])#shorts?(?:[^a-z0-9_]|$)`)

// VisibleItems returns the current list of videos filtered by active catalog filters and chips.
func (s *State) VisibleItems() []model.Video {
	if s.View == ViewSearch {
		return s.SearchResults
	}
	if s.View == ViewChannel {
		return s.ChannelVideos
	}
	items := s.Feeds[s.ActiveTab]
	if s.ActiveTab == model.TabHome || s.ActiveTab == model.TabMusic || s.ActiveTab == model.TabSubscriptions {
		if s.HideShorts {
			filtered := make([]model.Video, 0, len(items))
			for _, v := range items {
				if !isShortVideo(v) {
					filtered = append(filtered, v)
				}
			}
			items = filtered
		}
		if s.ActiveTab == model.TabMusic && s.HomeChip != 1 {
			filtered := make([]model.Video, 0, len(items))
			for _, v := range items {
				if !v.IsLive {
					filtered = append(filtered, v)
				}
			}
			items = filtered
		}
		if s.Interleave {
			items = interleaveByChannel(items)
		}
	}
	if s.ActiveTab == model.TabHome || s.ActiveTab == model.TabMusic {
		return filterByChip(items, HomeChips, s.HomeChip, s)
	}
	return items
}

func isShortVideo(v model.Video) bool {
	if v.IsLive || model.IsPlaylistID(v.ID) {
		return false
	}
	if titleHasShortsTag(v.Title) {
		return true
	}
	sec := parseDurationSeconds(v.Duration)
	return sec > 0 && sec <= 60
}

func titleHasShortsTag(title string) bool {
	if title == "" {
		return false
	}
	return shortsTagRe.MatchString(title)
}

func parseDurationSeconds(dur string) int {
	s := strings.TrimSpace(dur)
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2:
		m, err1 := strconv.Atoi(parts[0])
		sec, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || m < 0 || sec < 0 || sec >= 60 {
			return 0
		}
		return m*60 + sec
	case 3:
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		sec, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil || h < 0 || m < 0 || m >= 60 || sec < 0 || sec >= 60 {
			return 0
		}
		return h*3600 + m*60 + sec
	default:
		return 0
	}
}

func interleaveByChannel(items []model.Video) []model.Video {
	if len(items) <= 2 {
		return items
	}
	type bucket struct {
		items []model.Video
	}
	byKey := make(map[string]int, len(items))
	buckets := make([]bucket, 0, len(items))
	for i, v := range items {
		key := strings.ToLower(strings.TrimSpace(v.Channel))
		if key == "" {
			key = fmt.Sprintf("\x00%d", i)
		}
		idx, ok := byKey[key]
		if !ok {
			idx = len(buckets)
			byKey[key] = idx
			buckets = append(buckets, bucket{})
		}
		buckets[idx].items = append(buckets[idx].items, v)
	}
	if len(buckets) == len(items) {
		return items
	}
	out := make([]model.Video, 0, len(items))
	for round := 0; len(out) < len(items); round++ {
		progressed := false
		for i := range buckets {
			if round < len(buckets[i].items) {
				out = append(out, buckets[i].items[round])
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return out
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
	chrome := 5 // top(3) + header(1) + chips(1)
	if s.View == ViewSearch {
		chrome = 4 // top(3) + header(1), no filter chips bar in Search
	}
	if s.HasPlayerBar() {
		chrome += 5
	}
	return max(1, s.Height-chrome)
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

// SetSelection updates the active cursor clamped to bounds and syncs scroll.
func (s *State) SetSelection(idx int) {
	s.setSelection(idx, true)
}

// SelectVisible updates the active cursor clamped to bounds and prefetches without shifting scroll.
func (s *State) SelectVisible(idx int) {
	s.setSelection(idx, false)
}

func (s *State) setSelection(idx int, syncScroll bool) {
	items := s.VisibleItems()
	if len(items) == 0 {
		switch s.View {
		case ViewSearch:
			s.ResultIdx = 0
			if syncScroll {
				s.ResultScroll = 0
			}
		case ViewChannel:
			s.ChannelIdx = 0
			if syncScroll {
				s.ChannelScroll = 0
			}
		default:
			s.SelectedIdx = 0
			if syncScroll {
				s.ScrollOffset = 0
			}
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
	if syncScroll {
		s.syncScrollToSelection()
	}
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
