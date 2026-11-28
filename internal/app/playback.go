package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
)

type autoplayConfigurer interface {
	SetAutoplay(bool)
	SetAutoplayFallback([]string)
}

type radioTasteSetter interface {
	SetRadioTaste(player.TasteProfile)
}

type radioSessionFeedback interface {
	RecordFastSkip(model.Video)
	RecordCompletedTrack(model.Video)
}

type pauser interface {
	Pause(ctx context.Context) error
}

type qualitySetter interface {
	SetQuality(ctx context.Context, quality int)
}

type playbackProfileSetter interface {
	SetPlaybackProfile(ctx context.Context, quality int, audioOnly bool) error
}

type localFileProvider interface {
	LocalFile(videoID string) string
}

type speedController interface {
	SetSpeed(ctx context.Context, speed float64) error
}

type loopToggler interface {
	ToggleLoop(ctx context.Context) (bool, error)
}

type playlistController interface {
	Playlist(ctx context.Context) ([]player.PlaylistItem, error)
	PlayPlaylistIndex(ctx context.Context, index int) error
	RemovePlaylistIndex(ctx context.Context, index int) error
	ClearPlaylist(ctx context.Context) error
}

type playFromStarter interface {
	PlayFrom(ctx context.Context, v model.Video, mode string, startPos float64) error
}

type videoModeToggler interface {
	ToggleVideoMode(ctx context.Context) (bool, error)
}

type sponsorBlockController interface {
	SetSponsorBlock(enabled bool)
	SponsorBlockEnabled() bool
}

type volumeSetter interface {
	SetVolume(ctx context.Context, vol int) error
}

type launchModeSetter interface {
	SetLaunchMode(ctx context.Context, mode string) error
}

// QualityOption describes one preset in the interactive Resolution & Audio-Only modal.
type QualityOption struct {
	Label     string
	Desc      string
	Quality   int
	AudioOnly bool
}

// QualityOptions lists the selectable resolution / data-saver / audio-only presets.
var QualityOptions = []QualityOption{
	{Label: "Máxima (4K / Best)", Desc: "Máxima nitidez disponible (~4-15 GB/h)", Quality: 0, AudioOnly: false},
	{Label: "1080p Full HD", Desc: "Alta definición (~2.5 GB/h)", Quality: 1080, AudioOnly: false},
	{Label: "720p HD", Desc: "Equilibrado · recomendado (~1.0 GB/h)", Quality: 720, AudioOnly: false},
	{Label: "360p Ahorro", Desc: "Ahorro de datos (~250 MB/h)", Quality: 360, AudioOnly: false},
	{Label: "Solo Audio", Desc: "Sin ventana de video (~50 MB/h)", Quality: 720, AudioOnly: true},
}

// QualityOptions returns the localized resolution / data-saver / audio-only presets for the active catalog.
func (s *State) QualityOptions() []QualityOption {
	as := s.Catalog().App
	return []QualityOption{
		{Label: as.QualOptMaxLabel, Desc: as.QualOptMaxDesc, Quality: 0, AudioOnly: false},
		{Label: as.QualOpt1080Label, Desc: as.QualOpt1080Desc, Quality: 1080, AudioOnly: false},
		{Label: as.QualOpt720Label, Desc: as.QualOpt720Desc, Quality: 720, AudioOnly: false},
		{Label: as.QualOpt360Label, Desc: as.QualOpt360Desc, Quality: 360, AudioOnly: false},
		{Label: as.QualOptAudioLabel, Desc: as.QualOptAudioDesc, Quality: 720, AudioOnly: true},
	}
}

func (s *State) fireTrackNotification(id, title string) {
	title = strings.TrimSpace(title)
	if seed := model.MixSeedVideoID(id); seed != "" {
		id = seed
	} else {
		id = strings.TrimSpace(id)
	}
	if title == "" {
		return
	}
	s.lastNotifiedID = id
	s.lastNotifiedTitle = title
	if !s.Notify || s.Deps.System == nil {
		return
	}
	sys := s.Deps.System
	go func() {
		_ = sys.Notify("iutui", title)
	}()
}

func (s *State) maybeNotifyStatusTrack(st player.Status) {
	curID := strings.TrimSpace(s.CurrentPlaying.ID)
	if curID == "" {
		curID = strings.TrimSpace(st.VideoID)
	}
	if seed := model.MixSeedVideoID(curID); seed != "" {
		curID = seed
	}
	curTitle := strings.TrimSpace(s.CurrentPlaying.Title)
	if curTitle == "" {
		curTitle = strings.TrimSpace(st.Title)
	}
	if curTitle == "" {
		return
	}
	if curID != "" && s.lastNotifiedID != "" {
		if curID == s.lastNotifiedID {
			return
		}
	} else if curTitle == s.lastNotifiedTitle {
		return
	}
	s.fireTrackNotification(curID, curTitle)
}

var qualityPresets = []int{0, 1080, 720, 480, 360}

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
	as := s.Catalog().App
	msg := as.ToastAutoplayOff
	if s.Autoplay {
		msg = as.ToastAutoplayOn
	}
	return s, tea.Batch(s.toastCmd(msg), s.saveBoolConfigCmd("YT_AUTOPLAY", s.Autoplay))
}

func (s *State) resumeStillWatching() (*State, tea.Cmd) {
	s.ShowStillWatching = false
	s.RegisterUserActivity()
	var resumeCmd tea.Cmd
	if s.Deps.Engine != nil {
		resumeCmd = s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
	}
	return s, tea.Batch(resumeCmd, s.toastCmd(s.Catalog().App.ToastResumePlayback))
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
		return s, s.toastCmd(s.Catalog().App.ToastPausePlayback)
	}
	return s, nil
}

// PlaySelected launches the highlighted video in the player engine and records history.
func (s *State) PlaySelected(mode string) (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Engine == nil {
		return s, nil
	}
	return s.playVideo(v, mode)
}

func (s *State) indexOfVisibleVideo(id string) int {
	if id == "" {
		return -1
	}
	for i, item := range s.VisibleItems() {
		if item.ID == id {
			return i
		}
	}
	return -1
}

func (s *State) playVideo(v model.Video, mode string) (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	if s.Deps.Engine == nil {
		return s, nil
	}
	s.LyricsScroll = 0
	if s.CurrentPlaying.ID != "" && s.CurrentPlaying.ID != v.ID {
		s.saveCurrentResumePosition()
		if !s.skipHistoryPush {
			s.PlayStack = append(s.PlayStack, s.CurrentPlaying)
			if len(s.PlayStack) > 50 {
				s.PlayStack = s.PlayStack[len(s.PlayStack)-50:]
			}
		}
	}
	s.skipHistoryPush = false
	s.navigatingBack = false
	s.lastPlaylistPos = -1
	s.trackMaxPos = 0
	s.trackDur = 0
	s.MPVPlaylist = nil
	s.QueueIdx = 0
	s.QueueScroll = 0
	s.queueRefreshPending = false
	s.CurrentPlaying = v
	if s.Deps.ListeningStats != nil && v.ID != "" {
		rec := v
		stats := s.Deps.ListeningStats
		go func() { _ = stats.RecordPlay(rec) }()
	}

	targetVideo := v
	offlineLocal := false
	if !isMixTarget(v.ID, mode) {
		if lp, ok := s.Deps.Downloader.(localFileProvider); ok {
			if localPath := strings.TrimSpace(lp.LocalFile(v.ID)); localPath != "" {
				targetVideo.LocalPath = localPath
				offlineLocal = true
			}
		}
	}

	var resumePos float64
	if !isMixTarget(v.ID, mode) && s.Deps.ResumeStore != nil {
		if pos, ok := s.Deps.ResumeStore.GetPosition(v.ID); ok && pos >= 5 {
			resumePos = pos
		}
	}

	if !offlineLocal {
		if p, ok := s.Deps.Engine.(streamPrefetcher); ok {
			items := s.VisibleItems()
			idx := s.indexOfVisibleVideo(v.ID)
			if idx < 0 {
				idx = s.activeIdx()
			}
			if idx >= 0 && idx+1 < len(items) {
				p.Prefetch(items[idx+1])
			}
			if idx >= 0 && idx+2 < len(items) {
				p.Prefetch(items[idx+2])
			}
		}
	}

	upcomingIDs := s.upcomingFallbackIDs(15)
	s.playInFlight = true
	s.PlayLoading = true
	s.PlayLoadingTitle = v.Title
	s.PlayerStatus.Title = v.Title
	s.PlayerStatus.VideoID = model.MixSeedVideoID(v.ID)
	s.PlayerStatus.TimePos = resumePos
	s.PlayerStatus.Duration = 0
	s.RegisterUserActivity()
	s.lastTrackTitle = v.Title

	if ac, ok := s.Deps.Engine.(autoplayConfigurer); ok {
		ac.SetAutoplay(s.Autoplay)
		ac.SetAutoplayFallback(upcomingIDs)
	}
	s.syncRadioTaste()

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

	as := s.Catalog().App
	toastMsg := i18n.Format(as.ToastOpenMPV, map[string]string{"title": v.Title})
	if mode == "audio" || (mode == "auto" && s.AudioOnly) {
		toastMsg = i18n.Format(as.ToastPlayAudioOnly, map[string]string{"title": v.Title})
	} else if isMixTarget(v.ID, mode) {
		toastMsg = i18n.Format(as.ToastOpenMix, map[string]string{"title": v.Title})
	} else if offlineLocal {
		toastMsg = i18n.Format(as.ToastPlayOffline, map[string]string{"title": v.Title})
	}
	pf, hasPlayFrom := s.Deps.Engine.(playFromStarter)
	if resumePos > 0 && hasPlayFrom {
		toastMsg = i18n.Format(as.ToastResumingAt, map[string]string{"pos": formatClockApp(resumePos)})
	}
	s.fireTrackNotification(v.ID, v.Title)
	toastCmd := s.toastCmd(toastMsg)
	eng := s.Deps.Engine
	st := s.Deps.Store
	ctx := s.appCtx
	playCmd := func() tea.Msg {
		var err error
		if resumePos > 0 && hasPlayFrom {
			err = pf.PlayFrom(ctx, targetVideo, mode, resumePos)
		} else {
			err = eng.Play(ctx, targetVideo, mode)
		}
		if err != nil {
			return ActionResultMsg{Err: err}
		}
		if st != nil {
			_ = st.RecordHistory(ctx, v)
		}
		return PlayStartedMsg{}
	}
	var lyricsCmd tea.Cmd
	if s.ShowLyrics {
		lyricsCmd = s.startLyricsFetch(v.ID, v.Title, v.Channel, float64(parseDurationSeconds(v.Duration)))
	}
	return s, tea.Batch(toastCmd, playCmd, lyricsCmd, s.ensureAnimTick())
}

// NextTrack advances playback through the active mpv playlist, queued tracks, or visible grid.
func (s *State) NextTrack() (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
	}
	s.recordSkipOrCompleteForOutgoing(s.CurrentPlaying)
	if s.PlayerStatus.IsRunning && s.PlayerStatus.PlaylistCount > 1 &&
		s.PlayerStatus.PlaylistPos >= 0 && s.PlayerStatus.PlaylistPos+1 < s.PlayerStatus.PlaylistCount {
		s.navigatingBack = false
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Next(ctx) }, s.Catalog().App.ToastNextTrack)
	}
	if len(s.Queue) > 0 {
		next := s.Queue[0]
		s.Queue = s.Queue[1:]
		if idx := s.indexOfVisibleVideo(next.ID); idx >= 0 {
			s.SetSelection(idx)
		}
		return s.playVideo(next, "auto")
	}
	if items := s.VisibleItems(); len(items) > 1 {
		nextIdx := (s.activeIdx() + 1) % len(items)
		s.SetSelection(nextIdx)
		return s.PlaySelected("auto")
	}
	return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Next(ctx) }, s.Catalog().App.ToastNextTrack)
}

// PrevTrack returns playback to the previous played track in the active mpv playlist or PlayStack, or restarts the active track.
func (s *State) PrevTrack() (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
	}
	as := s.Catalog().App
	if s.PlayerStatus.IsRunning && s.PlayerStatus.PlaylistCount > 1 && s.PlayerStatus.PlaylistPos > 0 {
		s.navigatingBack = true
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Prev(ctx) }, as.ToastPrevTrack)
	}
	if len(s.PlayStack) > 0 {
		prev := s.PlayStack[len(s.PlayStack)-1]
		s.PlayStack = s.PlayStack[:len(s.PlayStack)-1]
		if idx := s.indexOfVisibleVideo(prev.ID); idx >= 0 {
			s.SetSelection(idx)
		}
		s.skipHistoryPush = true
		return s.playVideo(prev, "auto")
	}
	if s.PlayerStatus.IsRunning || s.CurrentPlaying.ID != "" {
		return s.SeekAbsolutePos(0, as.ToastTrackStart)
	}
	if items := s.VisibleItems(); len(items) > 1 && s.activeIdx() > 0 {
		s.SetSelection(s.activeIdx() - 1)
		s.skipHistoryPush = true
		return s.PlaySelected("auto")
	}
	return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Prev(ctx) }, as.ToastPrevTrack)
}

func (s *State) recordSkipOrCompleteForOutgoing(outgoing model.Video) {
	if outgoing.ID == "" || !s.PlayerStatus.IsRunning {
		return
	}
	pos := max(s.trackMaxPos, s.PlayerStatus.TimePos)
	dur := max(s.trackDur, s.PlayerStatus.Duration)
	isFastSkip := pos >= 3.0 && (pos < 25.0 || (dur > 60 && pos/dur < 0.25))
	isCompleted := pos >= 60.0 || (dur > 30 && pos/dur >= 0.50)

	if isFastSkip {
		if fb, ok := s.Deps.Engine.(radioSessionFeedback); ok {
			fb.RecordFastSkip(outgoing)
		}
		if s.Deps.ListeningStats != nil {
			stats := s.Deps.ListeningStats
			go func() { _ = stats.RecordSkip(outgoing) }()
		}
	} else if isCompleted {
		if fb, ok := s.Deps.Engine.(radioSessionFeedback); ok {
			fb.RecordCompletedTrack(outgoing)
		}
		if s.Deps.ListeningStats != nil {
			stats := s.Deps.ListeningStats
			go func() { _ = stats.RecordComplete(outgoing) }()
		}
	}
}

func (s *State) syncRadioTaste() {
	if s.Deps.Engine == nil {
		return
	}
	if ts, ok := s.Deps.Engine.(radioTasteSetter); ok {
		ts.SetRadioTaste(player.BuildTasteProfile(s.Feeds[model.TabLiked], s.Feeds[model.TabHistory]))
	}
}

func (s *State) resolvePlayingVideo(videoID, title string) model.Video {
	id := player.ExtractYouTubeID(videoID)
	if id == "" {
		id = strings.TrimSpace(videoID)
	}
	if id != "" {
		if s.CurrentPlaying.ID == id {
			v := s.CurrentPlaying
			if title != "" && v.Title == "" {
				v.Title = title
			}
			return v
		}
		for _, v := range s.Queue {
			if v.ID == id {
				return v
			}
		}
		for i := len(s.PlayStack) - 1; i >= 0; i-- {
			if s.PlayStack[i].ID == id {
				return s.PlayStack[i]
			}
		}
		for _, v := range s.VisibleItems() {
			if v.ID == id {
				return v
			}
		}
		for _, feed := range s.Feeds {
			for _, v := range feed {
				if v.ID == id {
					return v
				}
			}
		}
		for _, it := range s.MPVPlaylist {
			itID := it.VideoID
			if itID == "" {
				itID = player.ExtractYouTubeID(it.Filename)
			}
			if itID == id {
				resolvedTitle := strings.TrimSpace(it.Title)
				if resolvedTitle == "" {
					resolvedTitle = strings.TrimSpace(title)
				}
				cleanArtist := strings.TrimSpace(it.Artist)
				if strings.EqualFold(cleanArtist, "YouTube") || strings.EqualFold(cleanArtist, "Mix / Cola") || strings.EqualFold(cleanArtist, "Mix / Queue") {
					cleanArtist = ""
				}
				if resolvedTitle != "" || cleanArtist != "" {
					return model.Video{
						ID:       id,
						Title:    resolvedTitle,
						Channel:  cleanArtist,
						Duration: strings.TrimSpace(it.Duration),
					}
				}
			}
		}
		return model.Video{ID: id, Title: title}
	}
	if title != "" {
		return s.findVideoForTitle(title)
	}
	return model.Video{}
}

// SeekRelative seeks playback relative by deltaSec with optimistic UI timestamp feedback.
func (s *State) SeekRelative(deltaSec int) (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
	}
	next := s.PlayerStatus.TimePos + float64(deltaSec)
	if next < 0 {
		next = 0
	}
	if s.PlayerStatus.Duration > 0 && next > s.PlayerStatus.Duration {
		next = s.PlayerStatus.Duration
	}
	s.PlayerStatus.TimePos = next
	label := "⏩ +10s"
	if deltaSec < 0 {
		label = fmt.Sprintf("⏪ %ds", deltaSec)
	} else if deltaSec != 10 {
		label = fmt.Sprintf("⏩ +%ds", deltaSec)
	}
	return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Seek(ctx, deltaSec) }, label)
}

// SeekAbsolutePos seeks playback to targetSec with optimistic UI timestamp feedback.
func (s *State) SeekAbsolutePos(targetSec float64, toast string) (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
	}
	if targetSec < 0 {
		targetSec = 0
	}
	if s.PlayerStatus.Duration > 0 && targetSec > s.PlayerStatus.Duration {
		targetSec = s.PlayerStatus.Duration
	}
	oldPos := s.PlayerStatus.TimePos
	s.PlayerStatus.TimePos = targetSec
	return s, s.engineCmd(func(ctx context.Context) error {
		if cp, ok := s.Deps.Engine.(chaptersProvider); ok {
			return cp.SeekAbsolute(ctx, targetSec)
		}
		return s.Deps.Engine.Seek(ctx, int(targetSec-oldPos))
	}, toast)
}

// EnqueueSelected appends the highlighted video to the player queue and prefetches its stream.
func (s *State) EnqueueSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Engine == nil {
		return s, nil
	}
	if p, ok := s.Deps.Engine.(streamPrefetcher); ok {
		p.Prefetch(v)
	}
	s.Queue = append(s.Queue, v)
	toastCmd := s.toastCmd(i18n.Format(s.Catalog().App.ToastQueued, map[string]string{"count": strconv.Itoa(len(s.Queue)), "title": v.Title}))
	enqCmd := func() tea.Msg {
		if err := s.Deps.Engine.Enqueue(s.appCtx, v); err != nil {
			return ActionResultMsg{Err: err}
		}
		return nil
	}
	return s, tea.Batch(toastCmd, enqCmd)
}

func (s *State) localQueuePlaylist() []player.PlaylistItem {
	items := make([]player.PlaylistItem, 0, len(s.Queue)+1)
	if s.CurrentPlaying.ID != "" {
		artist := s.CurrentPlaying.Channel
		if strings.EqualFold(artist, "YouTube") {
			artist = ""
		}
		items = append(items, player.PlaylistItem{
			Index:    0,
			VideoID:  player.ExtractYouTubeID(s.CurrentPlaying.ID),
			Title:    s.CurrentPlaying.Title,
			Artist:   artist,
			Duration: s.CurrentPlaying.Duration,
			Filename: s.CurrentPlaying.ID,
			Current:  true,
		})
	}
	for _, v := range s.Queue {
		artist := v.Channel
		if strings.EqualFold(artist, "YouTube") {
			artist = ""
		}
		items = append(items, player.PlaylistItem{
			Index:    len(items),
			VideoID:  player.ExtractYouTubeID(v.ID),
			Title:    v.Title,
			Artist:   artist,
			Duration: v.Duration,
			Filename: v.ID,
		})
	}
	return items
}

// ToggleQueue opens or closes the Queue/Mix drawer cleanly.
func (s *State) ToggleQueue() (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	if s.ShowQueue {
		s.ShowQueue = false
		s.queueRefreshPending = false
		return s, nil
	}
	return s.OpenQueue()
}

// OpenQueue opens the live mpv queue drawer/modal and refreshes playlist entries from Engine.
func (s *State) OpenQueue() (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	s.ShowHelp = false
	s.ShowSettings = false
	s.ShowQualityModal = false
	s.ShowChapters = false
	s.ShowLyrics = false
	s.ShowQueue = true

	if pc, ok := s.Deps.Engine.(playlistController); ok {
		if len(s.MPVPlaylist) == 0 && len(s.Queue) > 0 {
			s.MPVPlaylist = s.localQueuePlaylist()
		}
		if s.QueueIdx >= len(s.MPVPlaylist) {
			s.QueueIdx = max(0, len(s.MPVPlaylist)-1)
		}
		s.syncQueueScroll()
		s.queueRefreshPending = true
		ctx := s.appCtx
		return s, func() tea.Msg {
			items, err := pc.Playlist(ctx)
			return QueueLoadedMsg{Items: items, Err: err}
		}
	}

	s.queueRefreshPending = false
	s.MPVPlaylist = s.localQueuePlaylist()
	if s.QueueIdx >= len(s.MPVPlaylist) {
		s.QueueIdx = max(0, len(s.MPVPlaylist)-1)
	}
	s.syncQueueScroll()
	return s, nil
}

// PlayQueueSelected jumps playback to the currently highlighted Queue overlay item and closes it.
func (s *State) PlayQueueSelected() (*State, tea.Cmd) {
	return s.playQueueAt(s.QueueIdx, true)
}

func (s *State) playQueueAt(queueIdx int, closeAfter bool) (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	if queueIdx < 0 || queueIdx >= len(s.MPVPlaylist) {
		if closeAfter {
			s.ShowQueue = false
		}
		return s, nil
	}
	s.QueueIdx = queueIdx
	for i := range s.MPVPlaylist {
		s.MPVPlaylist[i].Current = (i == queueIdx)
	}
	item := s.MPVPlaylist[queueIdx]
	if closeAfter {
		s.ShowQueue = false
	}
	if pc, ok := s.Deps.Engine.(playlistController); ok && s.PlayerStatus.IsRunning {
		as := s.Catalog().App
		idx := item.Index
		if idx < 0 {
			idx = queueIdx
		}
		title := item.Title
		if title == "" {
			title = i18n.Format(as.TrackFallbackN, map[string]string{"n": strconv.Itoa(idx + 1)})
		}
		vid := item.VideoID
		if vid == "" {
			vid = player.ExtractYouTubeID(item.Filename)
		}
		if vid != "" || title != "" {
			resolved := s.resolvePlayingVideo(vid, item.Title)
			if resolved.ID == "" {
				resolved.ID = vid
			}
			if resolved.Title == "" {
				resolved.Title = title
			}
			if resolved.Channel == "" && item.Artist != "" && !strings.EqualFold(item.Artist, "YouTube") && !strings.EqualFold(item.Artist, "Mix / Cola") && !strings.EqualFold(item.Artist, "Mix / Queue") {
				resolved.Channel = strings.TrimSpace(item.Artist)
			}
			if resolved.Duration == "" {
				resolved.Duration = strings.TrimSpace(item.Duration)
			}
			s.CurrentPlaying = resolved
			s.fireTrackNotification(resolved.ID, resolved.Title)
		}
		return s, s.engineCmd(func(ctx context.Context) error {
			return pc.PlayPlaylistIndex(ctx, idx)
		}, i18n.Format(as.ToastQueuePlay, map[string]string{"title": title}))
	}
	// Fallback when mpv is not running yet: play directly from local s.Queue if matched.
	for i, v := range s.Queue {
		if v.ID == item.Filename || v.Title == item.Title {
			s.Queue = append(s.Queue[:i], s.Queue[i+1:]...)
			return s.playVideo(v, "auto")
		}
	}
	return s, nil
}

// RemoveQueueSelected removes the highlighted track from the Queue overlay and mpv playlist.
func (s *State) RemoveQueueSelected() (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	if s.QueueIdx < 0 || s.QueueIdx >= len(s.MPVPlaylist) {
		return s, nil
	}
	removed := s.MPVPlaylist[s.QueueIdx]
	s.MPVPlaylist = append(s.MPVPlaylist[:s.QueueIdx], s.MPVPlaylist[s.QueueIdx+1:]...)
	if s.QueueIdx >= len(s.MPVPlaylist) && s.QueueIdx > 0 {
		s.QueueIdx--
	}
	s.syncQueueScroll()
	for i, v := range s.Queue {
		if (removed.Filename != "" && v.ID == removed.Filename) || v.Title == removed.Title {
			s.Queue = append(s.Queue[:i], s.Queue[i+1:]...)
			break
		}
	}
	if pc, ok := s.Deps.Engine.(playlistController); ok && s.PlayerStatus.IsRunning {
		idx := removed.Index
		ctx := s.appCtx
		toast := s.toastCmd(s.Catalog().App.ToastRemovedFromQueue)
		rmCmd := func() tea.Msg {
			if err := pc.RemovePlaylistIndex(ctx, idx); err != nil {
				return ActionResultMsg{Err: err}
			}
			items, err := pc.Playlist(ctx)
			return QueueLoadedMsg{Items: items, Err: err}
		}
		return s, tea.Batch(toast, rmCmd)
	}
	return s, s.toastCmd(s.Catalog().App.ToastRemovedFromQueue)
}

// ClearQueue clears queued tracks in state and in the active mpv playlist.
func (s *State) ClearQueue() (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	s.Queue = nil
	s.MPVPlaylist = nil
	s.QueueIdx = 0
	s.QueueScroll = 0
	if pc, ok := s.Deps.Engine.(playlistController); ok && s.PlayerStatus.IsRunning {
		return s, s.engineCmd(func(ctx context.Context) error {
			return pc.ClearPlaylist(ctx)
		}, s.Catalog().App.ToastQueueCleared)
	}
	return s, s.toastCmd(s.Catalog().App.ToastQueueCleared)
}

// AdjustSpeed steps playback speed within [0.5x, 2.0x].
func (s *State) AdjustSpeed(delta float64) (*State, tea.Cmd) {
	cur := s.PlayerStatus.Speed
	if cur <= 0 {
		cur = 1.0
	}
	return s.SetPlaybackSpeed(cur + delta)
}

// SetPlaybackSpeed sets playback speed clamped to [0.5, 2.0].
func (s *State) SetPlaybackSpeed(target float64) (*State, tea.Cmd) {
	target = clampSpeed(target)
	s.PlayerStatus.Speed = target
	speedStr := strconv.FormatFloat(target, 'f', -1, 64)
	label := i18n.Format(s.Catalog().App.ToastSpeedFmt, map[string]string{"speed": speedStr})
	saveCmd := s.saveConfigCmd("YT_SPEED", speedStr)
	if sc, ok := s.Deps.Engine.(speedController); ok && s.Deps.Engine != nil {
		return s, tea.Batch(s.engineCmd(func(ctx context.Context) error {
			return sc.SetSpeed(ctx, target)
		}, label), saveCmd)
	}
	return s, tea.Batch(s.toastCmd(label), saveCmd)
}

// ToggleLoop toggles single-track repeat in the player engine.
func (s *State) ToggleLoop() (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
	}
	lt, ok := s.Deps.Engine.(loopToggler)
	if !ok {
		return s, s.toastCmd(s.Catalog().App.ToastRepeatUnsupported)
	}
	next := !s.PlayerStatus.LoopFile
	s.PlayerStatus.LoopFile = next
	ctx := s.appCtx
	return s, func() tea.Msg {
		enabled, err := lt.ToggleLoop(ctx)
		return LoopToggledMsg{Enabled: enabled, Err: err}
	}
}

// QualityLabel returns a human-readable badge for the current max vertical resolution or audio-only mode.
func (s *State) QualityLabel() string {
	as := s.Catalog().App
	if s.AudioOnly {
		return as.BadgeAudioOnly
	}
	if s.Quality <= 0 {
		return "MAX"
	}
	if s.Quality <= 360 {
		return i18n.Format(as.BadgeQualitySaver, map[string]string{"q": strconv.Itoa(s.Quality)})
	}
	return fmt.Sprintf("%dp", s.Quality)
}

// OpenQualityModal opens the resolution / data-saver / audio-only selector centered on the current preset.
func (s *State) OpenQualityModal() *State {
	defer s.syncMouseSnapshot()
	s.ShowHelp = false
	s.ShowSettings = false
	s.ShowChapters = false
	s.ShowQualityModal = true
	s.QualityIdx = s.currentQualityOptionIndex()
	return s
}

func (s *State) currentQualityOptionIndex() int {
	opts := s.QualityOptions()
	if s.AudioOnly {
		return len(opts) - 1
	}
	for i, opt := range opts {
		if !opt.AudioOnly && opt.Quality == s.Quality {
			return i
		}
	}
	return 2 // 720p HD fallback
}

// ApplyQualityOption applies preset idx from QualityOptions, updates the engine,
// persists configuration, and displays a confirmation toast.
func (s *State) ApplyQualityOption(idx int) (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	opts := s.QualityOptions()
	if idx < 0 || idx >= len(opts) {
		idx = 2
	}
	opt := opts[idx]
	s.ShowQualityModal = false
	return s.applyPlaybackProfile(opt.Quality, opt.AudioOnly, opt.Label)
}

func (s *State) applyPlaybackProfile(quality int, audioOnly bool, label string) (*State, tea.Cmd) {
	s.Quality = quality
	s.AudioOnly = audioOnly
	if audioOnly {
		s.DefaultMode = "audio"
	} else if s.DefaultMode == "audio" || s.DefaultMode == "" {
		s.DefaultMode = "window"
	}

	as := s.Catalog().App
	var cmds []tea.Cmd
	if label == "" {
		if audioOnly {
			label = as.BadgeAudioOnly
		} else if quality <= 0 {
			label = as.ValQualBestToast
		} else {
			label = fmt.Sprintf("%dp", quality)
		}
	}
	toastFmt := as.ToastQualityFmt
	if audioOnly {
		toastFmt = as.ToastAudioModeFmt
	}
	cmds = append(cmds, s.toastCmd(i18n.Format(toastFmt, map[string]string{"label": label})))

	selVid, hasSel := s.SelectedVideo()
	prefetcher, hasPrefetch := s.Deps.Engine.(streamPrefetcher)
	ctx := s.appCtx

	if ps, ok := s.Deps.Engine.(playbackProfileSetter); ok {
		q := quality
		ao := audioOnly
		cmds = append(cmds, func() tea.Msg {
			if err := ps.SetPlaybackProfile(ctx, q, ao); err != nil {
				if hasSel && hasPrefetch {
					prefetcher.Prefetch(selVid)
				}
				return ActionResultMsg{Err: err}
			}
			if hasSel && hasPrefetch {
				prefetcher.Prefetch(selVid)
			}
			return nil
		})
	} else if qs, ok := s.Deps.Engine.(qualitySetter); ok {
		qs.SetQuality(ctx, quality)
		if hasSel && hasPrefetch {
			prefetcher.Prefetch(selVid)
		}
	} else if hasSel && hasPrefetch {
		prefetcher.Prefetch(selVid)
	}

	if s.Deps.OnQualityChange != nil {
		fn := s.Deps.OnQualityChange
		q := quality
		cmds = append(cmds, func() tea.Msg {
			if err := fn(q); err != nil {
				return ActionResultMsg{Err: err}
			}
			return nil
		})
	}
	if s.Deps.OnAudioOnlyChange != nil {
		fn := s.Deps.OnAudioOnlyChange
		ao := audioOnly
		cmds = append(cmds, func() tea.Msg {
			if err := fn(ao); err != nil {
				return ActionResultMsg{Err: err}
			}
			return nil
		})
	}
	return s, tea.Batch(cmds...)
}

// CycleQuality rotates through presets (MAX -> 1080p -> 720p -> 480p -> 360p -> Solo Audio -> MAX),
// updates the player engine, persists config, and re-prefetches the current item.
func (s *State) CycleQuality() (*State, tea.Cmd) {
	as := s.Catalog().App
	if s.AudioOnly {
		return s.applyPlaybackProfile(0, false, as.ValQualBestToast)
	}
	if s.Quality == qualityPresets[len(qualityPresets)-1] {
		return s.applyPlaybackProfile(s.Quality, true, as.ValModeAudio)
	}
	next := qualityPresets[0]
	for i, q := range qualityPresets {
		if q == s.Quality {
			next = qualityPresets[(i+1)%len(qualityPresets)]
			break
		}
	}
	label := as.ValQualBestToast
	if next > 0 {
		label = fmt.Sprintf("%dp", next)
	}
	return s.applyPlaybackProfile(next, false, label)
}

// ToggleVideoMode switches audio-only vs video mode live on the active mpv instance.
func (s *State) ToggleVideoMode() (*State, tea.Cmd) {
	vt, ok := s.Deps.Engine.(videoModeToggler)
	if !ok {
		return s, s.toastCmd(s.Catalog().App.ToastHotSwapUnavailable)
	}
	ctx := s.appCtx
	return s, func() tea.Msg {
		videoOn, err := vt.ToggleVideoMode(ctx)
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
	as := s.Catalog().App
	msg := as.ToastSponsorBlockOff
	if s.SponsorBlock {
		msg = as.ToastSponsorBlockOn
	}
	return s, tea.Batch(s.toastCmd(msg), saveCmd)
}

func (s *State) toggleSubtitlesCmd() tea.Cmd {
	type subtitleCycler interface {
		CycleSubtitles(context.Context) (bool, error)
	}
	cat := s.Catalog()
	sc, ok := s.Deps.Engine.(subtitleCycler)
	if !ok {
		return s.toastCmd(cat.App.ToastHotSwapUnavailable)
	}
	ctx := s.appCtx
	return func() tea.Msg {
		on, err := sc.CycleSubtitles(ctx)
		if err != nil {
			return ActionResultMsg{Err: err}
		}
		if on {
			return ActionResultMsg{Info: cat.App.ToastSubtitlesOn}
		}
		return ActionResultMsg{Info: cat.App.ToastSubtitlesOff}
	}
}

func (s *State) togglePiPCmd() tea.Cmd {
	type pipToggler interface {
		TogglePiP(context.Context) (bool, error)
	}
	cat := s.Catalog()
	pt, ok := s.Deps.Engine.(pipToggler)
	if !ok {
		return s.toastCmd(cat.App.ToastHotSwapUnavailable)
	}
	ctx := s.appCtx
	return func() tea.Msg {
		on, err := pt.TogglePiP(ctx)
		if err != nil {
			return ActionResultMsg{Err: err}
		}
		if on {
			return ActionResultMsg{Info: cat.App.ToastPiPOn}
		}
		return ActionResultMsg{Info: cat.App.ToastPiPOff}
	}
}
