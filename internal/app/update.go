package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/download"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
)

const (
	searchTimeout           = 45 * time.Second
	channelTimeout          = 20 * time.Second
	lyricsTimeout           = 6 * time.Second
	feedTimeout             = 30 * time.Second
	uiTickRate              = 250 * time.Millisecond // 4 Hz cap
	animTickRate            = 90 * time.Millisecond
	stillWatchingTrackLimit = 8
	stillWatchingIdleLimit  = 60 * time.Minute
)

var (
	toastTTL        = 4 * time.Second
	suggestDebounce = 180 * time.Millisecond
)

// FeedLoadedMsg arrives when a catalog feed finishes loading.
type FeedLoadedMsg struct {
	Tab    model.Tab
	Videos []model.Video
	Err    error
}

// LiveLoadedMsg arrives when background live stream lookup for the En vivo chip finishes.
type LiveLoadedMsg struct {
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

// SavedLoadedMsg arrives when saved/liked/history/subscriptions finish loading.
type SavedLoadedMsg struct {
	Saved   []model.Video
	Liked   []model.Video
	History []model.Video
	Subs    []string
}

// ToggledSaveMsg arrives after Store.ToggleSaved completes.
type ToggledSaveMsg struct {
	Video model.Video
	Saved bool
	Err   error
}

// ToggledLikeMsg arrives after Store.ToggleLiked completes.
type ToggledLikeMsg struct {
	Video model.Video
	Liked bool
	Err   error
}

// ThumbReadyMsg notifies the TUI that an async thumbnail finished downloading or uploading.
type ThumbReadyMsg struct {
	ID string
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

// MetadataEnrichedMsg delivers asynchronously enriched views and publish dates.
type MetadataEnrichedMsg struct {
	Meta map[string]catalog.VideoMeta
}

// SuggestTickMsg triggers a debounced autocomplete query when the user pauses typing.
type SuggestTickMsg struct {
	Seq   uint64
	Draft string
}

// SuggestionsLoadedMsg delivers search autocomplete results for the active search draft.
type SuggestionsLoadedMsg struct {
	Seq   uint64
	Query string
	Items []string
	Err   error
}

// QueueLoadedMsg delivers the live mpv playlist items for the Queue overlay.
type QueueLoadedMsg struct {
	Items []player.PlaylistItem
	Err   error
}

// LoopToggledMsg reports completion of toggling single-track repeat in the player engine.
type LoopToggledMsg struct {
	Enabled bool
	Err     error
}

// DownloadProgressMsg delivers incremental progress updates for active downloads.
type DownloadProgressMsg struct {
	Item download.Item
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
		s.syncScrollToSelection()
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
			return s, s.enrichVisibleMetadataCmd(msg.Videos, s.SelectedIdx)
		}
		return s, s.enrichVisibleMetadataCmd(msg.Videos, 0)

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
		return s, s.enrichVisibleMetadataCmd(msg.Videos, 0)

	case SavedLoadedMsg:
		s.SavedIDs = make(map[string]bool, len(msg.Saved))
		for _, v := range msg.Saved {
			s.SavedIDs[v.ID] = true
		}
		s.LikedIDs = make(map[string]bool, len(msg.Liked))
		for _, v := range msg.Liked {
			s.LikedIDs[v.ID] = true
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
			s.Feeds[model.TabLiked] = append([]model.Video(nil), msg.Liked...)
		}
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.SelectedIdx)

	case ToggledSaveMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error al guardar: %v", msg.Err))
		}
		if msg.Saved {
			s.SavedIDs[msg.Video.ID] = true
			s.Feeds[model.TabWatchLater] = prependUniqueVideo(s.Feeds[model.TabWatchLater], msg.Video)
			s.SetSelection(s.SelectedIdx)
			return s, s.toastCmd("★ Guardado: " + msg.Video.Title)
		}
		delete(s.SavedIDs, msg.Video.ID)
		s.Feeds[model.TabWatchLater] = removeVideoByID(s.Feeds[model.TabWatchLater], msg.Video.ID)
		s.SetSelection(s.SelectedIdx)
		return s, s.toastCmd("Quitado de guardados")

	case ToggledLikeMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error en Me gusta: %v", msg.Err))
		}
		if msg.Liked {
			s.LikedIDs[msg.Video.ID] = true
			s.Feeds[model.TabLiked] = prependUniqueVideo(s.Feeds[model.TabLiked], msg.Video)
			s.SetSelection(s.SelectedIdx)
			return s, s.toastCmd("♥ Me gusta: " + msg.Video.Title)
		}
		delete(s.LikedIDs, msg.Video.ID)
		s.Feeds[model.TabLiked] = removeVideoByID(s.Feeds[model.TabLiked], msg.Video.ID)
		s.SetSelection(s.SelectedIdx)
		return s, s.toastCmd("Quitado de Me gusta")

	case ThumbReadyMsg:
		return s, nil

	case ActionResultMsg:
		if msg.Err != nil {
			s.PlayLoading = false
			return s, s.toastCmd(fmt.Sprintf("Error: %v", msg.Err))
		}
		if msg.Info != "" {
			return s, s.toastCmd(msg.Info)
		}
		return s, nil

	case LiveLoadedMsg:
		delete(s.LiveLoading, msg.Tab)
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error buscando directos: %v", msg.Err))
		}
		if len(msg.Videos) == 0 {
			return s, s.toastCmd("Sin directos en vivo ahora mismo")
		}
		existing := s.Feeds[msg.Tab]
		seen := make(map[string]bool, len(existing))
		merged := make([]model.Video, 0, len(existing)+len(msg.Videos))
		for _, v := range existing {
			seen[v.ID] = true
			merged = append(merged, v)
		}
		for _, v := range msg.Videos {
			v.IsLive = true
			if v.Duration == "" {
				v.Duration = "LIVE"
			}
			if !seen[v.ID] {
				seen[v.ID] = true
				merged = append(merged, v)
			}
		}
		s.Feeds[msg.Tab] = merged
		if msg.Tab == s.ActiveTab && len(s.VisibleItems()) > 0 {
			s.SetSelection(0)
			s.prefetchTop(s.VisibleItems(), 4)
			return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), 0)
		}
		return s, s.enrichVisibleMetadataCmd(s.Feeds[msg.Tab], 0)

	case PlayStartedMsg:
		s.playInFlight = false
		if msg.Err != nil {
			s.PlayLoading = false
			return s, s.toastCmd(fmt.Sprintf("Error: %v", msg.Err))
		}
		if s.PlayerStatus.IsRunning {
			s.PlayLoading = false
		}
		return s, nil

	case DownloadDoneMsg:
		delete(s.DownloadProgress, msg.Video.ID)
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error descargando: %v", msg.Err))
		}
		delete(s.Feeds, model.TabDownloads)
		var reload tea.Cmd
		if s.ActiveTab == model.TabDownloads {
			reload = s.loadFeedCmd(model.TabDownloads, true)
		}
		return s, tea.Batch(s.toastCmd("✓ Descargado: "+msg.Video.Title), reload)

	case DownloadProgressMsg:
		if s.DownloadProgress == nil {
			s.DownloadProgress = make(map[string]download.Item)
		}
		if msg.Item.Video.ID != "" {
			s.DownloadProgress[msg.Item.Video.ID] = msg.Item
		}
		return s, nil

	case VideoModeToggledMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error alternando video: %v", msg.Err))
		}
		s.AudioOnly = !msg.VideoOn
		if s.AudioOnly {
			s.DefaultMode = "audio"
		} else if s.DefaultMode == "audio" || s.DefaultMode == "" {
			s.DefaultMode = "window"
		}
		var saveCmd tea.Cmd
		if s.Deps.OnAudioOnlyChange != nil {
			fn := s.Deps.OnAudioOnlyChange
			audioOnly := s.AudioOnly
			saveCmd = func() tea.Msg {
				if err := fn(audioOnly); err != nil {
					return ActionResultMsg{Err: err}
				}
				return nil
			}
		}
		if msg.VideoOn {
			return s, tea.Batch(s.toastCmd("🎬 Video activado"), saveCmd)
		}
		return s, tea.Batch(s.toastCmd("🎧 Solo audio (sin ventana)"), saveCmd)

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
		return s, s.enrichVisibleMetadataCmd(msg.Videos, 0)

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

	case MetadataEnrichedMsg:
		s.applyEnrichedMetadata(msg.Meta)
		return s, nil

	case SuggestTickMsg:
		if s.Focus != FocusSearch || msg.Seq != s.suggestSeq || msg.Draft != s.SearchDraft {
			return s, nil
		}
		return s, s.fetchSuggestionsCmd(msg.Seq, msg.Draft)

	case SuggestionsLoadedMsg:
		if s.Focus != FocusSearch || msg.Seq != s.suggestSeq || msg.Query != s.SearchDraft {
			return s, nil
		}
		if msg.Err == nil {
			s.SearchSuggestions = msg.Items
			s.SuggestIdx = -1
		}
		return s, nil

	case QueueLoadedMsg:
		if msg.Err == nil && len(msg.Items) > 0 {
			s.MPVPlaylist = msg.Items
		} else if len(s.Queue) > 0 {
			s.MPVPlaylist = s.localQueuePlaylist()
		}
		if s.QueueIdx >= len(s.MPVPlaylist) {
			s.QueueIdx = max(0, len(s.MPVPlaylist)-1)
		}
		return s, nil

	case LoopToggledMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error en repetir: %v", msg.Err))
		}
		s.PlayerStatus.LoopFile = msg.Enabled
		if msg.Enabled {
			return s, s.toastCmd("🔁 Repetir: ACTIVADO")
		}
		return s, s.toastCmd("🔁 Repetir: DESACTIVADO")

	case PlayerStatusMsg:
		wasPlaying := s.PlayerStatus.IsRunning && !s.PlayerStatus.Paused
		prevRunning := s.PlayerStatus.IsRunning
		prevPos := s.PlayerStatus.TimePos
		prevDur := s.PlayerStatus.Duration
		if prevRunning && !msg.Status.IsRunning && !s.playInFlight && s.CurrentPlaying.ID != "" && s.Deps.ResumeStore != nil {
			_ = s.Deps.ResumeStore.SavePosition(s.CurrentPlaying.ID, prevPos, prevDur)
		}
		isStaleTitle := s.playInFlight && s.PlayLoadingTitle != "" && msg.Status.Title != "" && msg.Status.Title != s.PlayLoadingTitle
		if isStaleTitle {
			// Avoid flashing the outgoing track title/position while switching to the new video.
			msg.Status.Title = s.PlayLoadingTitle
			msg.Status.TimePos = 0
		}
		s.PlayerStatus = msg.Status
		if !isStaleTitle && msg.Status.IsRunning && (!s.playInFlight || msg.Status.Title == "" || msg.Status.Title == s.PlayLoadingTitle) {
			s.PlayLoading = false
			s.playInFlight = false
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
