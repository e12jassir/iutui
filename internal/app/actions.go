package app

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/download"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/tui/theme"
)

type likedProvider interface {
	Liked(ctx context.Context) ([]model.Video, error)
}

type likedToggler interface {
	ToggleLiked(ctx context.Context, v model.Video) (bool, error)
}

type feedRefresher interface {
	RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error)
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

type subscriptionLabelProvider interface {
	SubscriptionLabels(ctx context.Context) (map[string]string, error)
}

type labeledSubscriptionToggler interface {
	ToggleSubscriptionLabeled(ctx context.Context, channelRef, label string) (bool, error)
}

type subscriptionLabelSetter interface {
	SetSubscriptionLabel(ctx context.Context, channelRef, label string) error
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
	if s.DownloadProgress == nil {
		s.DownloadProgress = make(map[string]download.Item)
	}
	s.DownloadProgress[v.ID] = download.Item{
		Video:     v,
		AudioOnly: audioOnly,
		Status:    download.StatusPending,
	}
	toastCmd := s.toastCmd("⬇ Descargando: " + v.Title)
	dlCmd := func() tea.Msg {
		err := s.Deps.Downloader.Enqueue(s.appCtx, v, audioOnly)
		return DownloadDoneMsg{Video: v, Err: err}
	}
	return s, tea.Batch(toastCmd, dlCmd)
}

func (s *State) activeOrSelectedVideo() (model.Video, bool) {
	if s.CurrentPlaying.ID != "" {
		if s.PlayerStatus.IsRunning {
			liveTitle := strings.TrimSpace(s.PlayerStatus.Title)
			liveID := strings.TrimSpace(s.PlayerStatus.VideoID)
			idMismatch := liveID != "" && liveID != s.CurrentPlaying.ID
			titleMismatch := liveTitle != "" && !strings.EqualFold(strings.TrimSpace(s.CurrentPlaying.Title), liveTitle)
			if idMismatch || titleMismatch {
				v := s.resolvePlayingVideo(liveID, liveTitle)
				if v.Title == "" {
					v.Title = liveTitle
				}
				if v.ID == "" {
					v.ID = liveID
				}
				if v.ID != "" || v.Title != "" {
					return v, true
				}
			}
		}
		return s.CurrentPlaying, true
	}
	return s.SelectedVideo()
}

// ToggleSaveVideo persists watch-later state for v via Store.
func (s *State) ToggleSaveVideo(v model.Video) (*State, tea.Cmd) {
	if s.Deps.Store == nil {
		return s, nil
	}
	return s, func() tea.Msg {
		saved, err := s.Deps.Store.ToggleSaved(s.appCtx, v)
		return ToggledSaveMsg{Video: v, Saved: saved, Err: err}
	}
}

// ToggleSaveSelected persists watch-later state for the highlighted video via Store.
func (s *State) ToggleSaveSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok {
		return s, nil
	}
	return s.ToggleSaveVideo(v)
}

// ToggleSaveActive persists watch-later state for the playing track, falling back to SelectedVideo.
func (s *State) ToggleSaveActive() (*State, tea.Cmd) {
	v, ok := s.activeOrSelectedVideo()
	if !ok {
		return s, nil
	}
	return s.ToggleSaveVideo(v)
}

// ToggleLikeVideo persists liked state for v cleanly separate from Watch Later.
func (s *State) ToggleLikeVideo(v model.Video) (*State, tea.Cmd) {
	if s.Deps.Store == nil {
		return s, nil
	}
	lt, ok := s.Deps.Store.(likedToggler)
	if !ok {
		return s, s.toastCmd("Me gusta no disponible")
	}
	return s, func() tea.Msg {
		liked, err := lt.ToggleLiked(s.appCtx, v)
		return ToggledLikeMsg{Video: v, Liked: liked, Err: err}
	}
}

// ToggleLikeSelected persists liked state for the highlighted video cleanly separate from Watch Later.
func (s *State) ToggleLikeSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok {
		return s, nil
	}
	return s.ToggleLikeVideo(v)
}

// ToggleLikeActive persists liked state for the playing track, falling back to SelectedVideo.
func (s *State) ToggleLikeActive() (*State, tea.Cmd) {
	v, ok := s.activeOrSelectedVideo()
	if !ok {
		return s, nil
	}
	return s.ToggleLikeVideo(v)
}

func (s *State) maybeFetchLiveChip() tea.Cmd {
	if s.View != ViewHome || s.HomeChip != 1 || s.Deps.Catalog == nil || s.Syncing {
		return nil
	}
	if s.ActiveTab != model.TabHome && s.ActiveTab != model.TabMusic {
		return nil
	}
	if len(s.VisibleItems()) > 0 || len(s.Feeds[s.ActiveTab]) == 0 || s.LiveLoading[s.ActiveTab] {
		return nil
	}
	s.LiveLoading[s.ActiveTab] = true
	tab := s.ActiveTab
	query := "en vivo live"
	if tab == model.TabMusic {
		query = "music live radio 24/7"
	}
	liveCmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.appCtx, feedTimeout)
		defer cancel()
		videos, err := s.Deps.Catalog.Search(ctx, query)
		return LiveLoadedMsg{Tab: tab, Videos: videos, Err: err}
	}
	return tea.Batch(liveCmd, s.ensureAnimTick())
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
		var liked []model.Video
		if lp, ok := s.Deps.Store.(likedProvider); ok {
			liked, _ = lp.Liked(s.appCtx)
		}
		history, _ := s.Deps.Store.History(s.appCtx)
		subs, _ := s.Deps.Store.Subscriptions(s.appCtx)
		var subLabels map[string]string
		if lp, ok := s.Deps.Store.(subscriptionLabelProvider); ok {
			subLabels, _ = lp.SubscriptionLabels(s.appCtx)
		}
		return SavedLoadedMsg{Saved: saved, Liked: liked, History: history, Subs: subs, SubLabels: subLabels}
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
	if strings.TrimSpace(v.Channel) == "" && strings.TrimSpace(v.ChannelID) == "" {
		return s, s.toastCmd("Este video no tiene canal visible")
	}
	return s.openChannelVideo(v)
}

func (s *State) openChannelVideo(v model.Video) (*State, tea.Cmd) {
	chName := strings.TrimSpace(v.Channel)
	if chName == "" {
		chName = strings.TrimSpace(v.ChannelID)
	}
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

func resolveVideoChannel(v model.Video) (subRef, label string) {
	chID := strings.TrimSpace(v.ChannelID)
	ch := strings.TrimSpace(v.Channel)
	if strings.EqualFold(ch, "YouTube") {
		ch = ""
	}
	if ch == "" {
		if idx := strings.Index(v.Title, " - "); idx > 0 {
			ch = strings.TrimSpace(v.Title[:idx])
		}
	}
	subRef = chID
	if subRef == "" {
		subRef = ch
	}
	label = ch
	if label == "" {
		label = subRef
	}
	return subRef, label
}

// SubscriptionDisplayName returns the human-readable channel label for a
// stored subscription ref (e.g. resolving a raw "UC..." ID from persisted labels
// or loaded video feeds when available).
func (s *State) SubscriptionDisplayName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if lbl := strings.TrimSpace(s.SubscriptionLabels[ref]); lbl != "" {
		return lbl
	}
	if strings.HasPrefix(ref, "UC") {
		if ch := s.resolveChannelNameByID(ref); ch != "" {
			return ch
		}
	}
	return ref
}

func (s *State) resolveChannelNameByID(channelID string) string {
	match := func(v model.Video) string {
		if !strings.EqualFold(strings.TrimSpace(v.ChannelID), channelID) {
			return ""
		}
		ch := strings.TrimSpace(v.Channel)
		if ch == "" || ch == "-" || strings.EqualFold(ch, "YouTube") {
			return ""
		}
		return ch
	}
	if ch := match(s.CurrentPlaying); ch != "" {
		return ch
	}
	for _, tab := range []model.Tab{model.TabSubscriptions, model.TabHome, model.TabMusic, model.TabHistory, model.TabLiked, model.TabWatchLater} {
		for _, v := range s.Feeds[tab] {
			if ch := match(v); ch != "" {
				return ch
			}
		}
	}
	for _, v := range s.SearchResults {
		if ch := match(v); ch != "" {
			return ch
		}
	}
	for _, v := range s.ChannelVideos {
		if ch := match(v); ch != "" {
			return ch
		}
	}
	return ""
}

func (s *State) hydrateSubscriptionLabelsCmd(videos []model.Video) tea.Cmd {
	if len(s.Subscriptions) == 0 || len(videos) == 0 {
		return nil
	}
	need := make(map[string]string) // lower(ref) -> canonical ref
	for _, sub := range s.Subscriptions {
		sub = strings.TrimSpace(sub)
		if sub == "" || strings.TrimSpace(s.SubscriptionLabels[sub]) != "" {
			continue
		}
		need[strings.ToLower(sub)] = sub
	}
	if len(need) == 0 {
		return nil
	}
	if s.SubscriptionLabels == nil {
		s.SubscriptionLabels = make(map[string]string)
	}
	type labelUpdate struct {
		ref, label string
	}
	var updates []labelUpdate
	for _, v := range videos {
		cid := strings.TrimSpace(v.ChannelID)
		ch := strings.TrimSpace(v.Channel)
		if cid == "" || ch == "" || ch == "-" || strings.EqualFold(ch, "YouTube") || strings.EqualFold(ch, cid) {
			continue
		}
		if ref, ok := need[strings.ToLower(cid)]; ok {
			s.SubscriptionLabels[ref] = ch
			updates = append(updates, labelUpdate{ref: ref, label: ch})
			delete(need, strings.ToLower(cid))
			if len(need) == 0 {
				break
			}
		}
	}
	if len(updates) == 0 {
		return nil
	}
	setter, ok := s.Deps.Store.(subscriptionLabelSetter)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		for _, u := range updates {
			_ = setter.SetSubscriptionLabel(s.appCtx, u.ref, u.label)
		}
		return nil
	}
}

// IsActiveSubscribed reports whether the active or selected video's channel is subscribed.
func (s *State) IsActiveSubscribed() bool {
	v, ok := s.activeOrSelectedVideo()
	if !ok {
		return false
	}
	subRef, label := resolveVideoChannel(v)
	for _, sub := range s.Subscriptions {
		t := strings.TrimSpace(sub)
		if t == "" {
			continue
		}
		lbl := strings.TrimSpace(s.SubscriptionLabels[t])
		if (subRef != "" && strings.EqualFold(t, subRef)) ||
			(label != "" && strings.EqualFold(t, label)) ||
			(label != "" && lbl != "" && strings.EqualFold(lbl, label)) {
			return true
		}
	}
	return false
}

// ToggleSubscribeSelected subscribes or unsubscribes from the highlighted video's channel.
func (s *State) ToggleSubscribeSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok {
		return s, nil
	}
	return s.toggleSubscribeVideo(v)
}

// ToggleSubscribeActive subscribes or unsubscribes from the playing track's channel (fallback: selected).
func (s *State) ToggleSubscribeActive() (*State, tea.Cmd) {
	v, ok := s.activeOrSelectedVideo()
	if !ok {
		return s, nil
	}
	return s.toggleSubscribeVideo(v)
}

func (s *State) toggleSubscribeVideo(v model.Video) (*State, tea.Cmd) {
	if s.Deps.Store == nil {
		return s, nil
	}
	subRef, label := resolveVideoChannel(v)
	if subRef == "" {
		return s, s.toastCmd("Este video no tiene canal para suscribirse")
	}
	directMatch := false
	for _, existing := range s.Subscriptions {
		if strings.EqualFold(strings.TrimSpace(existing), subRef) {
			subRef = strings.TrimSpace(existing)
			directMatch = true
			break
		}
	}
	if !directMatch && label != "" {
		for _, existing := range s.Subscriptions {
			t := strings.TrimSpace(existing)
			if lbl := strings.TrimSpace(s.SubscriptionLabels[t]); lbl != "" && strings.EqualFold(lbl, label) {
				subRef = t
				break
			}
		}
	}

	lst, hasLabeled := s.Deps.Store.(labeledSubscriptionToggler)
	st, hasBasic := s.Deps.Store.(subscriptionToggler)
	if !hasLabeled && !hasBasic {
		return s, s.toastCmd("Suscripciones rápidas no disponibles")
	}
	return s, func() tea.Msg {
		var (
			subscribed bool
			err        error
		)
		if hasLabeled {
			subscribed, err = lst.ToggleSubscriptionLabeled(s.appCtx, subRef, label)
		} else {
			subscribed, err = st.ToggleSubscription(s.appCtx, subRef)
		}
		if err != nil {
			return SubscriptionToggledMsg{SubRef: subRef, Channel: label, Err: err}
		}
		subs, _ := s.Deps.Store.Subscriptions(s.appCtx)
		return SubscriptionToggledMsg{SubRef: subRef, Channel: label, Subscribed: subscribed, Subs: subs}
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
	var videoID, title, channel, durStr string
	if s.PlayLoading && strings.TrimSpace(s.PlayLoadingTitle) != "" {
		title = strings.TrimSpace(s.PlayLoadingTitle)
		if s.CurrentPlaying.ID != "" && strings.TrimSpace(s.CurrentPlaying.Title) == title {
			videoID = s.CurrentPlaying.ID
			channel = strings.TrimSpace(s.CurrentPlaying.Channel)
			durStr = s.CurrentPlaying.Duration
		} else if matched := s.findVideoForTitle(title); matched.Title != "" {
			videoID = matched.ID
			channel = strings.TrimSpace(matched.Channel)
			durStr = matched.Duration
		}
	} else if s.PlayerStatus.IsRunning && strings.TrimSpace(s.PlayerStatus.Title) != "" {
		title = strings.TrimSpace(s.PlayerStatus.Title)
		if s.CurrentPlaying.ID != "" && strings.TrimSpace(s.CurrentPlaying.Title) == title {
			videoID = s.CurrentPlaying.ID
			channel = strings.TrimSpace(s.CurrentPlaying.Channel)
			durStr = s.CurrentPlaying.Duration
		} else if matched := s.findVideoForTitle(title); matched.Title != "" {
			videoID = matched.ID
			channel = strings.TrimSpace(matched.Channel)
			durStr = matched.Duration
		}
	} else if strings.TrimSpace(s.CurrentPlaying.Title) != "" {
		videoID = s.CurrentPlaying.ID
		title = strings.TrimSpace(s.CurrentPlaying.Title)
		channel = strings.TrimSpace(s.CurrentPlaying.Channel)
		durStr = s.CurrentPlaying.Duration
	} else if v, ok := s.SelectedVideo(); ok {
		videoID = v.ID
		title = strings.TrimSpace(v.Title)
		channel = strings.TrimSpace(v.Channel)
		durStr = v.Duration
	}
	if title == "" {
		return s, s.toastCmd("Selecciona o reproduce un tema primero")
	}
	s.ShowQueue = false
	s.ShowLyrics = true
	if title == s.LyricsTrack && !s.LyricsLoading && (len(s.Lyrics.Synced) > 0 || len(s.Lyrics.Plain) > 0) {
		return s, nil
	}
	var dur float64
	if s.PlayerStatus.IsRunning && strings.TrimSpace(s.PlayerStatus.Title) == title {
		dur = s.PlayerStatus.Duration
	}
	if dur <= 0 {
		dur = float64(parseDurationSeconds(durStr))
	}
	cmd := s.startLyricsFetch(videoID, title, channel, dur)
	return s, tea.Batch(cmd, s.ensureAnimTick())
}

func (s *State) startLyricsFetch(videoID, title, channel string, durationSec float64) tea.Cmd {
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
	s.LyricsScroll = 0
	s.Lyrics = lyrics.TrackLyrics{}
	if videoID != s.LyricsVideoID {
		s.LyricsPreferSource = ""
	}
	s.LyricsVideoID = videoID

	client := s.Deps.LyricsClient
	if client == nil {
		client = lyrics.NewClient()
		s.Deps.LyricsClient = client
	}
	if videoID != "" {
		s.LyricsOffset = client.GetOffset(videoID)
	} else {
		s.LyricsOffset = 0
	}
	prefer := s.LyricsPreferSource

	ctx, cancel := context.WithTimeout(s.appCtx, lyricsTimeout)
	s.lyricsCancel = cancel

	return func() tea.Msg {
		tl, err := client.FetchWithOptions(ctx, videoID, title, channel, lyrics.FetchOptions{
			Duration:     durationSec,
			PreferSource: prefer,
			Lang:         "es",
		})
		return LyricsLoadedMsg{Seq: seq, Track: title, Lyrics: tl, Err: err}
	}
}

// AdjustLyricsOffset shifts the active lyric sync offset by delta seconds, persists it, and shows a toast.
func (s *State) AdjustLyricsOffset(delta float64) (*State, tea.Cmd) {
	if !s.ShowLyrics {
		return s, nil
	}
	next := math.Round((s.LyricsOffset+delta)*10) / 10
	if next < -60.0 {
		next = -60.0
	} else if next > 60.0 {
		next = 60.0
	}
	if math.Abs(next) < 0.05 {
		next = 0
	}
	s.LyricsOffset = next
	if s.Deps.LyricsClient != nil && s.LyricsVideoID != "" {
		_ = s.Deps.LyricsClient.SetOffset(s.LyricsVideoID, next)
	}
	msg := "⏱ Sync letra: 0.0s"
	if next != 0 {
		msg = fmt.Sprintf("⏱ Sync letra: %+.1fs", next)
	}
	return s, s.toastCmd(msg)
}

// ResetLyricsOffset resets the active lyric sync offset to 0.0s and clears any persisted offset.
func (s *State) ResetLyricsOffset() (*State, tea.Cmd) {
	if !s.ShowLyrics {
		return s, nil
	}
	s.LyricsOffset = 0
	if s.Deps.LyricsClient != nil && s.LyricsVideoID != "" {
		_ = s.Deps.LyricsClient.SetOffset(s.LyricsVideoID, 0)
	}
	return s, s.toastCmd("⏱ Sync letra: 0.0s")
}

// CycleLyricsSource toggles preferred lyrics source between YouTube captions and LRCLib and refetches.
func (s *State) CycleLyricsSource() (*State, tea.Cmd) {
	if !s.ShowLyrics || strings.TrimSpace(s.LyricsTrack) == "" {
		return s, nil
	}
	var nextSrc, label string
	if strings.HasPrefix(s.Lyrics.Source, "youtube") || s.LyricsPreferSource == lyrics.SourceYouTube {
		nextSrc = lyrics.SourceLRCLib
		label = "📝 Fuente: LRCLib"
	} else {
		nextSrc = lyrics.SourceYouTube
		label = "📝 Fuente: YouTube CC"
	}

	videoID := s.LyricsVideoID
	title := s.LyricsTrack
	channel := ""
	dur := s.PlayerStatus.Duration
	if s.CurrentPlaying.Title == title || (videoID != "" && s.CurrentPlaying.ID == videoID) {
		channel = strings.TrimSpace(s.CurrentPlaying.Channel)
		if dur <= 0 {
			dur = float64(parseDurationSeconds(s.CurrentPlaying.Duration))
		}
	} else {
		v := s.findVideoForTitle(title)
		channel = strings.TrimSpace(v.Channel)
		if dur <= 0 {
			dur = float64(parseDurationSeconds(v.Duration))
		}
		if videoID == "" && v.ID != "" {
			videoID = v.ID
			s.LyricsVideoID = videoID
		}
	}
	s.LyricsPreferSource = nextSrc

	cmd := s.startLyricsFetch(videoID, title, channel, dur)
	return s, tea.Batch(s.toastCmd(label), cmd, s.ensureAnimTick())
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

func (s *State) enrichVisibleMetadataCmd(items []model.Video, centerIdx int) tea.Cmd {
	me, ok := s.Deps.Catalog.(metadataEnricher)
	if !ok || len(items) == 0 {
		return nil
	}
	if centerIdx < 0 {
		centerIdx = 0
	}
	start := max(0, centerIdx-6)
	end := min(len(items), start+24)
	if end-start < min(24, len(items)) && len(items) >= 24 {
		start = max(0, len(items)-24)
		end = len(items)
	}
	if s.metaRequested == nil {
		s.metaRequested = make(map[string]bool)
	}
	var ids []string
	for _, v := range items[start:end] {
		if (v.Views != "" && v.Published != "" && (v.Duration != "" || v.IsLive)) || len(v.ID) != 11 || model.IsPlaylistID(v.ID) {
			continue
		}
		if s.metaRequested[v.ID] {
			continue
		}
		s.metaRequested[v.ID] = true
		ids = append(ids, v.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.appCtx, 10*time.Second)
		defer cancel()
		meta, _ := me.EnrichMetadata(ctx, ids)
		if len(meta) == 0 {
			return nil
		}
		return MetadataEnrichedMsg{Meta: meta}
	}
}

func (s *State) applyEnrichedMetadata(meta map[string]catalog.VideoMeta) {
	if len(meta) == 0 {
		return
	}
	for tab, list := range s.Feeds {
		if applyMetaSlice(list, meta) {
			s.Feeds[tab] = list
		}
	}
	applyMetaSlice(s.SearchResults, meta)
	applyMetaSlice(s.ChannelVideos, meta)
	applyMetaSlice(s.Queue, meta)
	if m, ok := meta[s.CurrentPlaying.ID]; ok {
		if s.CurrentPlaying.Views == "" && m.Views != "" {
			s.CurrentPlaying.Views = m.Views
		}
		if s.CurrentPlaying.Published == "" && m.Published != "" {
			s.CurrentPlaying.Published = m.Published
		}
		if s.CurrentPlaying.Duration == "" && m.Duration != "" {
			s.CurrentPlaying.Duration = m.Duration
		}
		if !s.CurrentPlaying.IsLive && m.IsLive {
			s.CurrentPlaying.IsLive = true
		}
	}
}

func applyMetaSlice(list []model.Video, meta map[string]catalog.VideoMeta) bool {
	changed := false
	for i := range list {
		m, ok := meta[list[i].ID]
		if !ok {
			continue
		}
		if list[i].Views == "" && m.Views != "" {
			list[i].Views = m.Views
			changed = true
		}
		if list[i].Published == "" && m.Published != "" {
			list[i].Published = m.Published
			changed = true
		}
		if list[i].Duration == "" && m.Duration != "" {
			list[i].Duration = m.Duration
			changed = true
		}
		if !list[i].IsLive && m.IsLive {
			list[i].IsLive = true
			changed = true
		}
	}
	return changed
}
