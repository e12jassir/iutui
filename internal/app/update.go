package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/lyrics"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/tui/theme"
)

const (
	searchTimeout           = 45 * time.Second
	channelTimeout          = 20 * time.Second
	lyricsTimeout           = 6 * time.Second
	feedTimeout             = 30 * time.Second
	toastTTL                = 4 * time.Second
	uiTickRate              = 250 * time.Millisecond // 4 Hz cap
	animTickRate            = 90 * time.Millisecond
	stillWatchingTrackLimit = 8
	stillWatchingIdleLimit  = 60 * time.Minute
)

// FeedLoadedMsg arrives when a catalog feed finishes loading.
type FeedLoadedMsg struct {
	Tab    model.Tab
	Videos []model.Video
	Err    error
}

// SearchDoneMsg arrives when an asynchronous search finishes.
type SearchDoneMsg struct {
	Query  string
	Seq    uint64
	Videos []model.Video
	Err    error
}

// SavedLoadedMsg arrives when saved/history/subscriptions finish loading.
type SavedLoadedMsg struct {
	Saved   []model.Video
	History []model.Video
	Subs    []string
}

// ToggledSaveMsg arrives after Store.ToggleSaved completes.
type ToggledSaveMsg struct {
	Video model.Video
	Saved bool
	Err   error
}

// PlayerStatusMsg carries a pushed status update from Engine.Subscribe.
type PlayerStatusMsg struct {
	Status player.Status
}

// ToastExpiredMsg clears the toast if its ID still matches.
type ToastExpiredMsg struct {
	ID int
}

// TickMsg drives bounded UI progress interpolation (4 Hz) when playing.
type TickMsg time.Time

// AnimTickMsg drives smooth spinner and comet loading animations while loading.
type AnimTickMsg time.Time

// PlayStartedMsg reports playback launch completion from Engine.Play.
type PlayStartedMsg struct {
	Err error
}

// ActionResultMsg surfaces async player or action errors/confirmations as a toast.
type ActionResultMsg struct {
	Info string
	Err  error
}

// DownloadDoneMsg arrives when a video download completes or fails.
type DownloadDoneMsg struct {
	Video model.Video
	Err   error
}

// VideoModeToggledMsg reports completion of hot Audio/Video toggling on mpv.
type VideoModeToggledMsg struct {
	VideoOn bool
	Err     error
}

// ChannelLoadedMsg arrives when an asynchronous channel video fetch completes.
type ChannelLoadedMsg struct {
	Channel string
	Seq     uint64
	Videos  []model.Video
	Err     error
}

// SubscriptionToggledMsg arrives when Store toggles a channel subscription.
type SubscriptionToggledMsg struct {
	Channel    string
	Subscribed bool
	Subs       []string
	Err        error
}

// ChaptersLoadedMsg arrives after querying chapter-list from mpv.
type ChaptersLoadedMsg struct {
	Chapters []player.Chapter
	Err      error
}

// LyricsLoadedMsg arrives when a lyrics lookup completes.
type LyricsLoadedMsg struct {
	Seq    uint64
	Track  string
	Lyrics lyrics.TrackLyrics
	Err    error
}

// Init starts initial feed/store loading and subscribes to player status push updates.
func (s *State) Init() tea.Cmd {
	if s.Deps.Catalog != nil {
		s.Syncing = true
	}
	return tea.Batch(
		s.loadFeedCmd(s.ActiveTab, false),
		s.loadStoreCmd(),
		s.waitPlayerStatusCmd(),
		s.ensureAnimTick(),
	)
}

// Update handles Bubble Tea messages and returns updated state + next command.
func (s *State) Update(msg tea.Msg) (*State, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.Width = msg.Width
		s.Height = msg.Height
		s.Ready = true
		return s, nil

	case FeedLoadedMsg:
		s.Syncing = false
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error cargando %s: %v", msg.Tab.String(), msg.Err))
		}
		s.Feeds[msg.Tab] = msg.Videos
		if msg.Tab == s.ActiveTab {
			s.SetSelection(s.SelectedIdx)
			s.prefetchTop(msg.Videos, 4)
		}
		return s, nil

	case SearchDoneMsg:
		if (msg.Seq != 0 && msg.Seq != s.searchSeq) || msg.Query != s.SearchQuery {
			return s, nil // stale search result
		}
		s.SearchPending = false
		if msg.Err != nil {
			if errors.Is(msg.Err, context.Canceled) {
				return s, nil
			}
			return s, s.toastCmd(fmt.Sprintf("Error buscando: %v", msg.Err))
		}
		s.SearchResults = msg.Videos
		s.ResultIdx = 0
		s.ResultScroll = 0
		s.SetSelection(0)
		s.prefetchTop(msg.Videos, 4)
		return s, nil

	case SavedLoadedMsg:
		s.SavedIDs = make(map[string]bool, len(msg.Saved))
		for _, v := range msg.Saved {
			s.SavedIDs[v.ID] = true
		}
		s.HistoryIDs = make(map[string]bool, len(msg.History))
		for _, v := range msg.History {
			s.HistoryIDs[v.ID] = true
		}
		s.Subscriptions = msg.Subs
		if _, ok := s.Feeds[model.TabHistory]; !ok && len(msg.History) > 0 {
			s.Feeds[model.TabHistory] = msg.History
		}
		if _, ok := s.Feeds[model.TabWatchLater]; !ok {
			s.Feeds[model.TabWatchLater] = append([]model.Video(nil), msg.Saved...)
		}
		if _, ok := s.Feeds[model.TabLiked]; !ok {
			s.Feeds[model.TabLiked] = append([]model.Video(nil), msg.Saved...)
		}
		return s, nil

	case ToggledSaveMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error al guardar: %v", msg.Err))
		}
		if msg.Saved {
			s.SavedIDs[msg.Video.ID] = true
			for _, t := range []model.Tab{model.TabWatchLater, model.TabLiked} {
				s.Feeds[t] = prependUniqueVideo(s.Feeds[t], msg.Video)
			}
			s.SetSelection(s.SelectedIdx)
			return s, s.toastCmd("★ Guardado: " + msg.Video.Title)
		}
		delete(s.SavedIDs, msg.Video.ID)
		for _, t := range []model.Tab{model.TabWatchLater, model.TabLiked} {
			s.Feeds[t] = removeVideoByID(s.Feeds[t], msg.Video.ID)
		}
		s.SetSelection(s.SelectedIdx)
		return s, s.toastCmd("Quitado de guardados")

	case ActionResultMsg:
		if msg.Err != nil {
			s.PlayLoading = false
			return s, s.toastCmd(fmt.Sprintf("Error: %v", msg.Err))
		}
		if msg.Info != "" {
			return s, s.toastCmd(msg.Info)
		}
		return s, nil

	case PlayStartedMsg:
		if msg.Err != nil {
			s.PlayLoading = false
			return s, s.toastCmd(fmt.Sprintf("Error: %v", msg.Err))
		}
		return s, nil

	case DownloadDoneMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error descargando: %v", msg.Err))
		}
		delete(s.Feeds, model.TabDownloads)
		var reload tea.Cmd
		if s.ActiveTab == model.TabDownloads {
			reload = s.loadFeedCmd(model.TabDownloads, true)
		}
		return s, tea.Batch(s.toastCmd("✓ Descargado: "+msg.Video.Title), reload)

	case VideoModeToggledMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error alternando video: %v", msg.Err))
		}
		if msg.VideoOn {
			return s, s.toastCmd("🎬 Video activado")
		}
		return s, s.toastCmd("🎧 Solo audio (en caliente)")

	case ChannelLoadedMsg:
		if msg.Seq != 0 && msg.Seq != s.channelSeq {
			return s, nil
		}
		s.ChannelPending = false
		if msg.Err != nil {
			if errors.Is(msg.Err, context.Canceled) {
				return s, nil
			}
			return s, s.toastCmd(fmt.Sprintf("Error cargando canal: %v", msg.Err))
		}
		s.ChannelVideos = msg.Videos
		s.ChannelIdx = 0
		s.ChannelScroll = 0
		s.SetSelection(0)
		s.prefetchTop(msg.Videos, 4)
		return s, nil

	case SubscriptionToggledMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error de suscripción: %v", msg.Err))
		}
		if msg.Subs != nil {
			s.Subscriptions = msg.Subs
		} else {
			s.Subscriptions = toggleLocalSub(s.Subscriptions, msg.Channel, msg.Subscribed)
		}
		delete(s.Feeds, model.TabSubscriptions)
		var reload tea.Cmd
		if s.ActiveTab == model.TabSubscriptions {
			s.Syncing = true
			reload = tea.Batch(s.loadFeedCmd(model.TabSubscriptions, true), s.ensureAnimTick())
		}
		if msg.Subscribed {
			return s, tea.Batch(s.toastCmd("★ Suscrito: "+msg.Channel), reload)
		}
		return s, tea.Batch(s.toastCmd("Suscripción quitada: "+msg.Channel), reload)

	case ChaptersLoadedMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error leyendo capítulos: %v", msg.Err))
		}
		if len(msg.Chapters) == 0 {
			return s, s.toastCmd("Sin capítulos disponibles")
		}
		s.Chapters = msg.Chapters
		s.ChapterIdx = activeChapterIndex(msg.Chapters, s.PlayerStatus.TimePos)
		s.ShowChapters = true
		return s, nil

	case LyricsLoadedMsg:
		if msg.Seq != 0 && msg.Seq != s.lyricsSeq {
			return s, nil
		}
		s.LyricsLoading = false
		s.LyricsTrack = msg.Track
		if msg.Err != nil {
			if errors.Is(msg.Err, context.Canceled) {
				return s, nil
			}
			s.Lyrics = lyrics.TrackLyrics{}
			if errors.Is(msg.Err, lyrics.ErrNotFound) {
				s.LyricsErr = "Sin letra encontrada"
			} else {
				s.LyricsErr = fmt.Sprintf("Error de letra: %v", msg.Err)
			}
			return s, nil
		}
		s.Lyrics = msg.Lyrics
		s.LyricsErr = ""
		return s, nil

	case PlayerStatusMsg:
		wasPlaying := s.PlayerStatus.IsRunning && !s.PlayerStatus.Paused
		s.PlayerStatus = msg.Status
		if msg.Status.IsRunning {
			s.PlayLoading = false
		}
		if msg.Status.IsRunning && msg.Status.Title != "" && msg.Status.Title != s.lastTrackTitle {
			if s.lastTrackTitle != "" && time.Since(s.LastInteractionAt) > 12*time.Second {
				s.AutoplayTracksSinceInput++
			}
			s.lastTrackTitle = msg.Status.Title
		}
		cmds := []tea.Cmd{s.waitPlayerStatusCmd()}
		if s.ShowLyrics && msg.Status.IsRunning && strings.TrimSpace(msg.Status.Title) != "" && msg.Status.Title != s.LyricsTrack {
			v := s.findVideoForTitle(msg.Status.Title)
			if cmd := s.startLyricsFetch(v.ID, msg.Status.Title, v.Channel); cmd != nil {
				cmds = append(cmds, cmd, s.ensureAnimTick())
			}
		}
		if s.Autoplay && !s.ShowStillWatching && msg.Status.IsRunning && !msg.Status.Paused {
			if s.AutoplayTracksSinceInput >= stillWatchingTrackLimit ||
				(s.AutoplayTracksSinceInput >= 1 && time.Since(s.LastInteractionAt) >= stillWatchingIdleLimit) {
				s.ShowStillWatching = true
				if s.Deps.Engine != nil {
					if p, ok := s.Deps.Engine.(pauser); ok {
						cmds = append(cmds, s.engineCmd(p.Pause, ""))
					} else {
						cmds = append(cmds, s.engineCmd(s.Deps.Engine.TogglePause, ""))
					}
				}
			}
		}
		nowPlaying := msg.Status.IsRunning && !msg.Status.Paused
		if !wasPlaying && nowPlaying && !s.ShowStillWatching {
			cmds = append(cmds, s.tickCmd())
		}
		return s, tea.Batch(cmds...)

	case TickMsg:
		if s.PlayerStatus.IsRunning && !s.PlayerStatus.Paused {
			return s, s.tickCmd()
		}
		return s, nil

	case AnimTickMsg:
		if !s.IsAnimating() {
			s.animActive = false
			return s, nil
		}
		s.AnimFrame++
		return s, s.animTickCmd()

	case ToastExpiredMsg:
		if msg.ID == s.ToastID {
			s.Toast = ""
		}
		return s, nil

	case tea.MouseMsg:
		if s.ShowStillWatching {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				return s.resumeStillWatching()
			}
			return s, nil
		}
		if msg.Action == tea.MouseActionPress || msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			s.RegisterUserActivity()
		}
		return s.handleMouse(msg)

	case tea.KeyMsg:
		if s.ShowStillWatching {
			return s.handleStillWatchingKey(msg.String())
		}
		if k := msg.String(); k != "ctrl+c" && k != "Q" {
			s.RegisterUserActivity()
		}
		return s.handleKey(msg)
	}

	return s, nil
}

// GridCols returns the current number of grid columns for the terminal width.
func (s *State) GridCols() int {
	mainW := s.Width - 2
	if s.Width >= 140 {
		mainW -= 26
	} else if s.Width >= 100 {
		mainW -= 10
	}
	cols := (mainW + 2) / 32
	if cols < 1 {
		return 1
	}
	return cols
}

func (s *State) handleKey(msg tea.KeyMsg) (*State, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		s.Close()
		return s, tea.Quit
	}

	if s.ShowHelp {
		if key == "?" || key == "esc" {
			s.ShowHelp = false
		}
		return s, nil
	}

	if s.ShowChapters {
		switch key {
		case "esc", "C", "q":
			s.ShowChapters = false
			return s, nil
		case "j", "down":
			if len(s.Chapters) > 0 && s.ChapterIdx < len(s.Chapters)-1 {
				s.ChapterIdx++
			}
			return s, nil
		case "k", "up":
			if s.ChapterIdx > 0 {
				s.ChapterIdx--
			}
			return s, nil
		case "enter", " ":
			if s.ChapterIdx >= 0 && s.ChapterIdx < len(s.Chapters) && s.Deps.Engine != nil {
				ch := s.Chapters[s.ChapterIdx]
				s.ShowChapters = false
				if cp, ok := s.Deps.Engine.(chaptersProvider); ok {
					return s, s.engineCmd(func(ctx context.Context) error {
						return cp.SeekAbsolute(ctx, ch.Time)
					}, "⏩ Capítulo: "+ch.Title)
				}
			}
			s.ShowChapters = false
			return s, nil
		}
		return s, nil
	}

	if s.ShowSettings {
		switch key {
		case "esc", "S", "q":
			s.ShowSettings = false
			return s, nil
		case "c", "enter", " ", "right", "l":
			return s.CycleQuality()
		case "A":
			return s.ToggleAutoplay()
		case "B":
			return s.ToggleSponsorBlock()
		case "t":
			return s.CycleTheme()
		}
		return s, nil
	}

	if s.Focus == FocusSearch {
		switch key {
		case "esc":
			s.Focus = FocusMain
			s.SearchDraft = ""
			return s, nil
		case "enter":
			return s.SubmitSearch()
		case "ctrl+u":
			s.SearchDraft = ""
			return s, nil
		case "ctrl+w", "alt+backspace":
			s.SearchDraft = deleteLastWord(s.SearchDraft)
			return s, nil
		case "backspace", "ctrl+h", "delete":
			r := []rune(s.SearchDraft)
			if len(r) > 0 {
				s.SearchDraft = string(r[:len(r)-1])
			}
			return s, nil
		case " ":
			s.SearchDraft += " "
			return s, nil
		default:
			if msg.Type == tea.KeySpace {
				s.SearchDraft += " "
			} else if msg.Type == tea.KeyRunes {
				s.SearchDraft += sanitizeSearchRunes(msg.Runes)
			}
			return s, nil
		}
	}

	switch key {
	case "Q":
		s.Close()
		return s, tea.Quit
	case "?":
		s.ShowHelp = true
		return s, nil
	case "S":
		s.ShowSettings = !s.ShowSettings
		return s, nil
	case "/":
		s.Focus = FocusSearch
		s.SearchDraft = ""
		return s, nil
	case "tab":
		if s.Width >= miniGuideMinW {
			if s.Focus == FocusSidebar {
				s.Focus = FocusMain
			} else {
				s.Focus = FocusSidebar
			}
		}
		return s, nil
	case "esc":
		if s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		if s.View == ViewChannel {
			s.View = s.PrevView
			if s.View == ViewChannel {
				s.View = ViewHome
			}
			s.Focus = FocusMain
			return s, nil
		}
		if s.View == ViewSearch {
			s.View = ViewHome
			s.Focus = FocusMain
			return s, nil
		}
		if s.ShowLyrics {
			s.ShowLyrics = false
		}
		return s, nil
	case "j", "down":
		if s.Focus == FocusSidebar {
			return s.cycleSidebarTab(1)
		}
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() + step)
		return s, nil
	case "k", "up":
		if s.Focus == FocusSidebar {
			return s.cycleSidebarTab(-1)
		}
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() - step)
		return s, nil
	case "l", "right":
		if s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		s.SetSelection(s.activeIdx() + 1)
		return s, nil
	case "h", "left":
		if s.Focus == FocusMain && s.Width >= miniGuideMinW && s.activeIdx() == 0 {
			s.Focus = FocusSidebar
			return s, nil
		}
		s.SetSelection(s.activeIdx() - 1)
		return s, nil
	case "1":
		return s.SwitchTab(model.TabHome)
	case "2":
		return s.SwitchTab(model.TabMusic)
	case "3":
		return s.SwitchTab(model.TabSubscriptions)
	case "4":
		return s.SwitchTab(model.TabHistory)
	case "5":
		return s.SwitchTab(model.TabDownloads)
	case "[":
		return s.CycleChip(-1), nil
	case "]":
		return s.CycleChip(1), nil
	case "enter":
		if s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		return s.PlaySelected("auto")
	case "a":
		return s.PlaySelected("audio")
	case "v":
		return s.PlaySelected("video")
	case "V":
		return s.ToggleVideoMode()
	case "m":
		return s.PlaySelected("mix")
	case "w":
		return s.ToggleSaveSelected()
	case "s":
		return s.ToggleSubscribeSelected()
	case "g":
		return s.OpenSelectedChannel()
	case "q":
		return s.EnqueueSelected()
	case "d":
		return s.DownloadSelected(false)
	case "c":
		return s.CycleQuality()
	case "C":
		return s.OpenChapters()
	case "L":
		return s.ToggleLyrics()
	case "B":
		return s.ToggleSponsorBlock()
	case "t":
		return s.CycleTheme()
	case "A":
		return s.ToggleAutoplay()
	case " ":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
	case "n":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Next(ctx) }, "⏭ Siguiente")
	case "p":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Prev(ctx) }, "⏮ Anterior")
	case "+", "=":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.AddVolume(ctx, 5) }, "🔊 Volumen +5%")
	case "-":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.AddVolume(ctx, -5) }, "🔉 Volumen -5%")
	case ".", ">":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Seek(ctx, 10) }, "⏩ +10s")
	case ",", "<":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Seek(ctx, -10) }, "⏪ -10s")
	case "r":
		s.Syncing = true
		return s, tea.Batch(s.loadFeedCmd(s.ActiveTab, true), s.ensureAnimTick())
	}

	return s, nil
}

func (s *State) prefetchTop(videos []model.Video, n int) {
	p, ok := s.Deps.Engine.(streamPrefetcher)
	if !ok {
		return
	}
	for i := 0; i < len(videos) && i < n; i++ {
		p.Prefetch(videos[i])
	}
}

func (s *State) activeIdx() int {
	return s.ActiveListIndex()
}

// SwitchTab changes the active sidebar tab and loads its feed if needed.
func (s *State) SwitchTab(tab model.Tab) (*State, tea.Cmd) {
	s.View = ViewHome
	s.PrevView = ViewHome
	s.ShowChapters = false
	s.ActiveTab = tab
	s.SelectedIdx = 0
	s.ScrollOffset = 0
	s.HomeChip = 0
	if tab == model.TabPlaylists {
		var pls []model.Video
		for _, v := range s.Feeds[model.TabHome] {
			if strings.HasPrefix(v.ID, "RD") || strings.Contains(strings.ToLower(v.Title), "mix") || strings.Contains(strings.ToLower(v.Title), "playlist") {
				pls = append(pls, v)
			}
		}
		s.Feeds[model.TabPlaylists] = pls
		return s, nil
	}
	if tab == model.TabWatchLater || tab == model.TabLiked {
		return s, nil
	}
	if _, ok := s.Feeds[tab]; ok {
		return s, nil
	}
	s.Syncing = true
	return s, tea.Batch(s.loadFeedCmd(tab, false), s.ensureAnimTick())
}

func (s *State) cycleSidebarTab(delta int) (*State, tea.Cmd) {
	tabs := []model.Tab{
		model.TabHome,
		model.TabMusic,
		model.TabSubscriptions,
		model.TabHistory,
		model.TabWatchLater,
		model.TabPlaylists,
		model.TabLiked,
		model.TabDownloads,
	}
	cur := 0
	for i, t := range tabs {
		if t == s.ActiveTab {
			cur = i
			break
		}
	}
	next := (cur + delta + len(tabs)) % len(tabs)
	return s.SwitchTab(tabs[next])
}

func prependUniqueVideo(list []model.Video, v model.Video) []model.Video {
	out := make([]model.Video, 0, len(list)+1)
	out = append(out, v)
	for _, item := range list {
		if item.ID != v.ID {
			out = append(out, item)
		}
	}
	return out
}

func removeVideoByID(list []model.Video, id string) []model.Video {
	out := make([]model.Video, 0, len(list))
	for _, item := range list {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}

// CycleChip steps through the active view's filter chips.
func (s *State) CycleChip(delta int) *State {
	if s.View == ViewSearch {
		n := len(SearchChips)
		s.SearchChip = (s.SearchChip + delta + n) % n
		s.ResultIdx = 0
		return s
	}
	n := len(HomeChips)
	s.HomeChip = (s.HomeChip + delta + n) % n
	s.SelectedIdx = 0
	return s
}

func deleteLastWord(s string) string {
	r := []rune(s)
	for len(r) > 0 && unicode.IsSpace(r[len(r)-1]) {
		r = r[:len(r)-1]
	}
	for len(r) > 0 && !unicode.IsSpace(r[len(r)-1]) {
		r = r[:len(r)-1]
	}
	return string(r)
}

func sanitizeSearchRunes(runes []rune) string {
	if len(runes) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(runes))
	for _, r := range runes {
		switch r {
		case '\r', '\n', '\t':
			b.WriteByte(' ')
		default:
			if r < 32 || r == 127 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isMixTarget(id, mode string) bool {
	return mode == "mix" ||
		strings.HasPrefix(id, "RD") ||
		strings.HasPrefix(id, "PL") ||
		strings.HasPrefix(id, "OLAK5uy_")
}

// SubmitSearch cancels any in-flight search and starts a new one.
func (s *State) SubmitSearch() (*State, tea.Cmd) {
	q := strings.TrimSpace(s.SearchDraft)
	if q == "" {
		s.Focus = FocusMain
		s.SearchDraft = ""
		return s, nil
	}
	if s.searchCancel != nil {
		s.searchCancel()
	}
	s.searchSeq++
	seq := s.searchSeq

	s.SearchQuery = q
	s.SearchDraft = ""
	s.SearchResults = nil
	s.View = ViewSearch
	s.Focus = FocusMain
	s.SearchPending = true
	s.SearchChip = 0
	s.ResultIdx = 0
	s.ResultScroll = 0

	ctx, cancel := context.WithTimeout(s.appCtx, searchTimeout)
	s.searchCancel = cancel

	searchCmd := func() tea.Msg {
		if s.Deps.Catalog == nil {
			return SearchDoneMsg{Query: q, Seq: seq, Err: errors.New("catálogo no configurado")}
		}
		videos, err := s.Deps.Catalog.Search(ctx, q)
		return SearchDoneMsg{Query: q, Seq: seq, Videos: videos, Err: err}
	}
	return s, tea.Batch(searchCmd, s.ensureAnimTick())
}

type autoplayConfigurer interface {
	SetAutoplay(bool)
	SetAutoplayFallback([]string)
}

type pauser interface {
	Pause(ctx context.Context) error
}

func (s *State) upcomingFallbackIDs(limit int) []string {
	items := s.VisibleItems()
	if len(items) <= 1 || limit <= 0 {
		return nil
	}
	cur := s.activeIdx()
	if cur < 0 || cur >= len(items) {
		cur = 0
	}
	curID := items[cur].ID
	ids := make([]string, 0, min(limit, len(items)-1))
	seen := map[string]bool{curID: true}
	for off := 1; off < len(items) && len(ids) < limit; off++ {
		cand := items[(cur+off)%len(items)]
		if cand.IsLive || cand.ID == "" || seen[cand.ID] {
			continue
		}
		seen[cand.ID] = true
		ids = append(ids, cand.ID)
	}
	return ids
}

// ToggleAutoplay flips continuous autoplay mode, configures the engine if supported, and shows a toast.
func (s *State) ToggleAutoplay() (*State, tea.Cmd) {
	s.Autoplay = !s.Autoplay
	if ac, ok := s.Deps.Engine.(autoplayConfigurer); ok {
		ac.SetAutoplay(s.Autoplay)
	}
	msg := "⏹ Autoplay: OFF"
	if s.Autoplay {
		msg = "🔁 Autoplay: ON"
	}
	return s, s.toastCmd(msg)
}

func (s *State) resumeStillWatching() (*State, tea.Cmd) {
	s.ShowStillWatching = false
	s.RegisterUserActivity()
	var resumeCmd tea.Cmd
	if s.Deps.Engine != nil {
		resumeCmd = s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
	}
	return s, tea.Batch(resumeCmd, s.toastCmd("▶ Continuando reproducción"))
}

func (s *State) handleStillWatchingKey(key string) (*State, tea.Cmd) {
	switch key {
	case "ctrl+c":
		s.Close()
		return s, tea.Quit
	case "enter", " ", "y", "s":
		return s.resumeStillWatching()
	case "esc", "q", "n":
		s.ShowStillWatching = false
		s.RegisterUserActivity()
		return s, s.toastCmd("⏸ Reproducción en pausa")
	}
	return s, nil
}

// PlaySelected launches the highlighted video in the player engine and records history.
func (s *State) PlaySelected(mode string) (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Engine == nil {
		return s, nil
	}
	upcomingIDs := s.upcomingFallbackIDs(15)
	s.PlayLoading = true
	s.PlayLoadingTitle = v.Title
	s.RegisterUserActivity()
	s.lastTrackTitle = v.Title

	if ac, ok := s.Deps.Engine.(autoplayConfigurer); ok {
		ac.SetAutoplay(s.Autoplay)
		ac.SetAutoplayFallback(upcomingIDs)
	}

	s.HistoryIDs[v.ID] = true
	if hist, ok := s.Feeds[model.TabHistory]; ok {
		updated := make([]model.Video, 0, len(hist)+1)
		updated = append(updated, v)
		for _, item := range hist {
			if item.ID != v.ID {
				updated = append(updated, item)
			}
		}
		s.Feeds[model.TabHistory] = updated
	}
	toastPrefix := "▶ Abriendo en mpv: "
	if isMixTarget(v.ID, mode) {
		toastPrefix = "📻 Abriendo Mix: "
	}
	toastCmd := s.toastCmd(toastPrefix + v.Title)
	playCmd := func() tea.Msg {
		if err := s.Deps.Engine.Play(s.appCtx, v, mode); err != nil {
			return ActionResultMsg{Err: err}
		}
		if s.Deps.Store != nil {
			_ = s.Deps.Store.RecordHistory(s.appCtx, v)
		}
		return PlayStartedMsg{}
	}
	return s, tea.Batch(toastCmd, playCmd, s.ensureAnimTick())
}

// EnqueueSelected appends the highlighted video to the player queue.
func (s *State) EnqueueSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Engine == nil {
		return s, nil
	}
	s.Queue = append(s.Queue, v)
	toastCmd := s.toastCmd(fmt.Sprintf("En cola (%d): %s", len(s.Queue), v.Title))
	enqCmd := func() tea.Msg {
		if err := s.Deps.Engine.Enqueue(s.appCtx, v); err != nil {
			return ActionResultMsg{Err: err}
		}
		return nil
	}
	return s, tea.Batch(toastCmd, enqCmd)
}

type qualitySetter interface {
	SetQuality(ctx context.Context, quality int)
}

var qualityPresets = []int{0, 1080, 720, 480}

// QualityLabel returns a human-readable badge for the current max vertical resolution.
func (s *State) QualityLabel() string {
	if s.Quality <= 0 {
		return "MAX"
	}
	return fmt.Sprintf("%dp", s.Quality)
}

// CycleQuality rotates through quality presets (MAX -> 1080p -> 720p -> 480p),
// updates the player engine, persists config, and re-prefetches the current item.
func (s *State) CycleQuality() (*State, tea.Cmd) {
	next := qualityPresets[0]
	for i, q := range qualityPresets {
		if q == s.Quality {
			next = qualityPresets[(i+1)%len(qualityPresets)]
			break
		}
	}
	s.Quality = next
	if qs, ok := s.Deps.Engine.(qualitySetter); ok {
		qs.SetQuality(s.appCtx, next)
	}
	if v, ok := s.SelectedVideo(); ok {
		if p, ok := s.Deps.Engine.(streamPrefetcher); ok {
			p.Prefetch(v)
		}
	}
	var saveCmd tea.Cmd
	if s.Deps.OnQualityChange != nil {
		fn := s.Deps.OnQualityChange
		q := next
		saveCmd = func() tea.Msg {
			if err := fn(q); err != nil {
				return ActionResultMsg{Err: err}
			}
			return nil
		}
	}
	label := "Máxima (best)"
	if next > 0 {
		label = fmt.Sprintf("%dp", next)
	}
	return s, tea.Batch(s.toastCmd("🎬 Calidad: "+label), saveCmd)
}

// DownloadSelected queues a background download for the highlighted video.
func (s *State) DownloadSelected(audioOnly bool) (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok {
		return s, nil
	}
	if s.Deps.Downloader == nil {
		return s, s.toastCmd("Descargas no configuradas")
	}
	toastCmd := s.toastCmd("⬇ Descargando: " + v.Title)
	dlCmd := func() tea.Msg {
		err := s.Deps.Downloader.Enqueue(s.appCtx, v, audioOnly)
		return DownloadDoneMsg{Video: v, Err: err}
	}
	return s, tea.Batch(toastCmd, dlCmd)
}

// ToggleSaveSelected persists watch-later state via Store.
func (s *State) ToggleSaveSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Store == nil {
		return s, nil
	}
	return s, func() tea.Msg {
		saved, err := s.Deps.Store.ToggleSaved(s.appCtx, v)
		return ToggledSaveMsg{Video: v, Saved: saved, Err: err}
	}
}

type feedRefresher interface {
	RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error)
}

func (s *State) loadFeedCmd(tab model.Tab, forceRefresh bool) tea.Cmd {
	if s.Deps.Catalog == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.appCtx, feedTimeout)
		defer cancel()
		if forceRefresh {
			if r, ok := s.Deps.Catalog.(feedRefresher); ok {
				videos, err := r.RefreshFeed(ctx, tab)
				return FeedLoadedMsg{Tab: tab, Videos: videos, Err: err}
			}
		}
		videos, err := s.Deps.Catalog.Feed(ctx, tab)
		return FeedLoadedMsg{Tab: tab, Videos: videos, Err: err}
	}
}

func (s *State) loadStoreCmd() tea.Cmd {
	if s.Deps.Store == nil {
		return nil
	}
	return func() tea.Msg {
		saved, _ := s.Deps.Store.Saved(s.appCtx)
		history, _ := s.Deps.Store.History(s.appCtx)
		subs, _ := s.Deps.Store.Subscriptions(s.appCtx)
		return SavedLoadedMsg{Saved: saved, History: history, Subs: subs}
	}
}

func (s *State) waitPlayerStatusCmd() tea.Cmd {
	if s.Deps.Engine == nil {
		return nil
	}
	if s.statusCh == nil {
		s.statusCh = s.Deps.Engine.Subscribe(s.appCtx)
	}
	ch := s.statusCh
	return func() tea.Msg {
		st, ok := <-ch
		if !ok {
			return nil
		}
		return PlayerStatusMsg{Status: st}
	}
}

func (s *State) engineCmd(fn func(context.Context) error, toast string) tea.Cmd {
	if s.Deps.Engine == nil {
		return nil
	}
	var tCmd tea.Cmd
	if toast != "" {
		tCmd = s.toastCmd(toast)
	}
	execCmd := func() tea.Msg {
		if err := fn(s.appCtx); err != nil {
			return ActionResultMsg{Err: err}
		}
		return nil
	}
	return tea.Batch(tCmd, execCmd)
}

func (s *State) toastCmd(msg string) tea.Cmd {
	id := s.setToast(msg)
	return tea.Tick(toastTTL, func(time.Time) tea.Msg {
		return ToastExpiredMsg{ID: id}
	})
}

func (s *State) tickCmd() tea.Cmd {
	return tea.Tick(uiTickRate, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (s *State) animTickCmd() tea.Cmd {
	return tea.Tick(animTickRate, func(t time.Time) tea.Msg {
		return AnimTickMsg(t)
	})
}

func (s *State) ensureAnimTick() tea.Cmd {
	if !s.IsAnimating() || s.animActive {
		return nil
	}
	s.animActive = true
	return s.animTickCmd()
}

type videoModeToggler interface {
	ToggleVideoMode(ctx context.Context) (bool, error)
}

type sponsorBlockController interface {
	SetSponsorBlock(enabled bool)
	SponsorBlockEnabled() bool
}

type chaptersProvider interface {
	Chapters(ctx context.Context) ([]player.Chapter, error)
	SeekAbsolute(ctx context.Context, seconds float64) error
}

type channelCatalog interface {
	ChannelVideos(ctx context.Context, v model.Video, limit int) ([]model.Video, error)
}

type subscriptionToggler interface {
	ToggleSubscription(ctx context.Context, channelRef string) (bool, error)
}

// ToggleVideoMode switches audio-only vs video mode live on the active mpv instance.
func (s *State) ToggleVideoMode() (*State, tea.Cmd) {
	vt, ok := s.Deps.Engine.(videoModeToggler)
	if !ok {
		return s, s.toastCmd("Cambio de video en caliente no disponible")
	}
	return s, func() tea.Msg {
		videoOn, err := vt.ToggleVideoMode(s.appCtx)
		return VideoModeToggledMsg{VideoOn: videoOn, Err: err}
	}
}

// ToggleSponsorBlock flips automatic SponsorBlock segment skipping, configures the engine,
// persists configuration if wired, and displays a confirmation toast.
func (s *State) ToggleSponsorBlock() (*State, tea.Cmd) {
	s.SponsorBlock = !s.SponsorBlock
	if sc, ok := s.Deps.Engine.(sponsorBlockController); ok {
		sc.SetSponsorBlock(s.SponsorBlock)
	}
	var saveCmd tea.Cmd
	if s.Deps.OnSponsorBlockChange != nil {
		fn := s.Deps.OnSponsorBlockChange
		enabled := s.SponsorBlock
		saveCmd = func() tea.Msg {
			if err := fn(enabled); err != nil {
				return ActionResultMsg{Err: err}
			}
			return nil
		}
	}
	msg := "🛡 SponsorBlock: OFF"
	if s.SponsorBlock {
		msg = "🛡 SponsorBlock: ON"
	}
	return s, tea.Batch(s.toastCmd(msg), saveCmd)
}

// CycleTheme rotates through built-in color palettes, persists YT_THEME, and shows a toast.
func (s *State) CycleTheme() (*State, tea.Cmd) {
	next := theme.NextTheme(s.ThemeName)
	s.ThemeName = next
	var saveCmd tea.Cmd
	if s.Deps.OnThemeChange != nil {
		fn := s.Deps.OnThemeChange
		saveCmd = func() tea.Msg {
			if err := fn(next); err != nil {
				return ActionResultMsg{Err: err}
			}
			return nil
		}
	}
	return s, tea.Batch(s.toastCmd("🎨 Tema: "+next), saveCmd)
}

// OpenSelectedChannel navigates into the highlighted video's creator channel view.
func (s *State) OpenSelectedChannel() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok {
		return s, nil
	}
	chName := strings.TrimSpace(v.Channel)
	if chName == "" {
		return s, s.toastCmd("Este video no tiene canal visible")
	}
	cc, ok := s.Deps.Catalog.(channelCatalog)
	if !ok {
		return s, s.toastCmd("Vista de canal no disponible")
	}
	if s.channelCancel != nil {
		s.channelCancel()
	}
	s.channelSeq++
	seq := s.channelSeq

	if s.View != ViewChannel {
		s.PrevView = s.View
	}
	s.View = ViewChannel
	s.Focus = FocusMain
	s.ChannelTitle = chName
	s.ChannelVideos = nil
	s.ChannelIdx = 0
	s.ChannelScroll = 0
	s.ChannelPending = true

	ctx, cancel := context.WithTimeout(s.appCtx, channelTimeout)
	s.channelCancel = cancel

	cmd := func() tea.Msg {
		videos, err := cc.ChannelVideos(ctx, v, 24)
		return ChannelLoadedMsg{Channel: chName, Seq: seq, Videos: videos, Err: err}
	}
	return s, tea.Batch(cmd, s.ensureAnimTick())
}

// ToggleSubscribeSelected subscribes or unsubscribes from the highlighted video's channel.
func (s *State) ToggleSubscribeSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Store == nil {
		return s, nil
	}
	ch := strings.TrimSpace(v.Channel)
	if ch == "" {
		return s, s.toastCmd("Este video no tiene canal para suscribirse")
	}
	st, ok := s.Deps.Store.(subscriptionToggler)
	if !ok {
		return s, s.toastCmd("Suscripciones rápidas no disponibles")
	}
	return s, func() tea.Msg {
		subscribed, err := st.ToggleSubscription(s.appCtx, ch)
		if err != nil {
			return SubscriptionToggledMsg{Channel: ch, Err: err}
		}
		subs, _ := s.Deps.Store.Subscriptions(s.appCtx)
		return SubscriptionToggledMsg{Channel: ch, Subscribed: subscribed, Subs: subs}
	}
}

// OpenChapters queries mpv's active chapter-list and opens the chapters picker modal.
func (s *State) OpenChapters() (*State, tea.Cmd) {
	cp, ok := s.Deps.Engine.(chaptersProvider)
	if !ok {
		return s, s.toastCmd("Sin capítulos disponibles")
	}
	return s, func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.appCtx, 3*time.Second)
		defer cancel()
		chapters, err := cp.Chapters(ctx)
		return ChaptersLoadedMsg{Chapters: chapters, Err: err}
	}
}

// ToggleLyrics toggles the synced lyrics panel and fetches lyrics for the active or selected track.
func (s *State) ToggleLyrics() (*State, tea.Cmd) {
	if s.ShowLyrics {
		s.ShowLyrics = false
		return s, nil
	}
	var videoID, title, channel string
	if s.PlayerStatus.IsRunning && strings.TrimSpace(s.PlayerStatus.Title) != "" {
		title = strings.TrimSpace(s.PlayerStatus.Title)
		if matched := s.findVideoForTitle(title); matched.Title != "" {
			videoID = matched.ID
			channel = matched.Channel
		}
	} else if v, ok := s.SelectedVideo(); ok {
		videoID = v.ID
		title = strings.TrimSpace(v.Title)
		channel = strings.TrimSpace(v.Channel)
	}
	if title == "" {
		return s, s.toastCmd("Selecciona o reproduce un tema primero")
	}
	s.ShowLyrics = true
	if title == s.LyricsTrack && !s.LyricsLoading && (len(s.Lyrics.Synced) > 0 || len(s.Lyrics.Plain) > 0) {
		return s, nil
	}
	cmd := s.startLyricsFetch(videoID, title, channel)
	return s, tea.Batch(cmd, s.ensureAnimTick())
}

func (s *State) startLyricsFetch(videoID, title, channel string) tea.Cmd {
	if strings.TrimSpace(title) == "" {
		return nil
	}
	if s.lyricsCancel != nil {
		s.lyricsCancel()
	}
	s.lyricsSeq++
	seq := s.lyricsSeq
	s.LyricsLoading = true
	s.LyricsTrack = title
	s.LyricsErr = ""
	s.Lyrics = lyrics.TrackLyrics{}

	client := s.Deps.LyricsClient
	if client == nil {
		client = lyrics.NewClient()
		s.Deps.LyricsClient = client
	}
	ctx, cancel := context.WithTimeout(s.appCtx, lyricsTimeout)
	s.lyricsCancel = cancel

	return func() tea.Msg {
		tl, err := client.Fetch(ctx, videoID, title, channel)
		return LyricsLoadedMsg{Seq: seq, Track: title, Lyrics: tl, Err: err}
	}
}

func (s *State) findVideoForTitle(title string) model.Video {
	if title == "" {
		return model.Video{}
	}
	searchLists := [][]model.Video{
		s.Feeds[s.ActiveTab],
		s.SearchResults,
		s.ChannelVideos,
		s.Queue,
	}
	for _, list := range searchLists {
		for _, v := range list {
			if v.Title == title {
				return v
			}
		}
	}
	for _, t := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabWatchLater, model.TabLiked, model.TabDownloads} {
		for _, v := range s.Feeds[t] {
			if v.Title == title {
				return v
			}
		}
	}
	return model.Video{}
}

func toggleLocalSub(subs []string, channel string, add bool) []string {
	out := make([]string, 0, len(subs)+1)
	for _, sub := range subs {
		if strings.EqualFold(strings.TrimSpace(sub), strings.TrimSpace(channel)) {
			continue
		}
		out = append(out, sub)
	}
	if add && strings.TrimSpace(channel) != "" {
		out = append(out, strings.TrimSpace(channel))
	}
	return out
}

func activeChapterIndex(chapters []player.Chapter, pos float64) int {
	idx := 0
	for i, ch := range chapters {
		if ch.Time <= pos+0.5 {
			idx = i
		}
	}
	return idx
}
