package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
)

type autoplayConfigurer interface {
	SetAutoplay(bool)
	SetAutoplayFallback([]string)
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
	msg := "⏹ Autoplay: OFF"
	if s.Autoplay {
		msg = "🔁 Autoplay: ON"
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
	s.CurrentPlaying = v

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
		if pos, ok := s.Deps.ResumeStore.GetPosition(v.ID); ok && pos >= 30 {
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
	s.PlayerStatus.TimePos = resumePos
	s.PlayerStatus.Duration = 0
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

	toastMsg := "▶ Abriendo en mpv: " + v.Title
	if mode == "audio" || (mode == "auto" && s.AudioOnly) {
		toastMsg = "🎧 Solo audio: " + v.Title
	} else if isMixTarget(v.ID, mode) {
		toastMsg = "📻 Abriendo Mix: " + v.Title
	} else if offlineLocal {
		toastMsg = "💾 Sin conexión: " + v.Title
	}
	pf, hasPlayFrom := s.Deps.Engine.(playFromStarter)
	if resumePos > 0 && hasPlayFrom {
		toastMsg = "⏩ Reanudando en " + formatClockApp(resumePos)
	}
	toastCmd := s.toastCmd(toastMsg)
	playCmd := func() tea.Msg {
		var err error
		if resumePos > 0 && hasPlayFrom {
			err = pf.PlayFrom(s.appCtx, targetVideo, mode, resumePos)
		} else {
			err = s.Deps.Engine.Play(s.appCtx, targetVideo, mode)
		}
		if err != nil {
			return ActionResultMsg{Err: err}
		}
		if s.Deps.Store != nil {
			_ = s.Deps.Store.RecordHistory(s.appCtx, v)
		}
		return PlayStartedMsg{}
	}
	var lyricsCmd tea.Cmd
	if s.ShowLyrics {
		lyricsCmd = s.startLyricsFetch(v.ID, v.Title, v.Channel, float64(parseDurationSeconds(v.Duration)))
	}
	return s, tea.Batch(toastCmd, playCmd, lyricsCmd, s.ensureAnimTick())
}

// NextTrack advances playback to the next queued or visible track, or delegates to mpv playlist-next.
func (s *State) NextTrack() (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
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
	return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Next(ctx) }, "⏭ Siguiente")
}

// PrevTrack returns playback to the previous played or visible track, or restarts the active track.
func (s *State) PrevTrack() (*State, tea.Cmd) {
	if s.Deps.Engine == nil {
		return s, nil
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
	if items := s.VisibleItems(); len(items) > 1 && s.activeIdx() > 0 {
		s.SetSelection(s.activeIdx() - 1)
		s.skipHistoryPush = true
		return s.PlaySelected("auto")
	}
	if s.PlayerStatus.IsRunning {
		return s.SeekAbsolutePos(0, "⏮ Inicio de pista")
	}
	return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Prev(ctx) }, "⏮ Anterior")
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
	toastCmd := s.toastCmd(fmt.Sprintf("En cola (%d): %s", len(s.Queue), v.Title))
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
		items = append(items, player.PlaylistItem{
			Index:    0,
			Title:    s.CurrentPlaying.Title,
			Filename: s.CurrentPlaying.ID,
			Current:  true,
		})
	}
	for _, v := range s.Queue {
		items = append(items, player.PlaylistItem{
			Index:    len(items),
			Title:    v.Title,
			Filename: v.ID,
		})
	}
	return items
}

// OpenQueue opens the live mpv queue modal and refreshes playlist entries from Engine.
func (s *State) OpenQueue() (*State, tea.Cmd) {
	s.ShowHelp = false
	s.ShowSettings = false
	s.ShowQualityModal = false
	s.ShowChapters = false
	s.ShowQueue = true

	if pc, ok := s.Deps.Engine.(playlistController); ok {
		if len(s.MPVPlaylist) == 0 && len(s.Queue) > 0 {
			s.MPVPlaylist = s.localQueuePlaylist()
		}
		if s.QueueIdx >= len(s.MPVPlaylist) {
			s.QueueIdx = max(0, len(s.MPVPlaylist)-1)
		}
		return s, func() tea.Msg {
			items, err := pc.Playlist(s.appCtx)
			return QueueLoadedMsg{Items: items, Err: err}
		}
	}

	s.MPVPlaylist = s.localQueuePlaylist()
	if s.QueueIdx >= len(s.MPVPlaylist) {
		s.QueueIdx = max(0, len(s.MPVPlaylist)-1)
	}
	return s, nil
}

// PlayQueueSelected jumps playback to the currently highlighted Queue overlay item.
func (s *State) PlayQueueSelected() (*State, tea.Cmd) {
	if s.QueueIdx < 0 || s.QueueIdx >= len(s.MPVPlaylist) {
		s.ShowQueue = false
		return s, nil
	}
	item := s.MPVPlaylist[s.QueueIdx]
	s.ShowQueue = false
	if pc, ok := s.Deps.Engine.(playlistController); ok && s.PlayerStatus.IsRunning {
		idx := item.Index
		if idx < 0 {
			idx = s.QueueIdx
		}
		title := item.Title
		if title == "" {
			title = fmt.Sprintf("Pista %d", idx+1)
		}
		return s, s.engineCmd(func(ctx context.Context) error {
			return pc.PlayPlaylistIndex(ctx, idx)
		}, "▶ Cola: "+title)
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
	if s.QueueIdx < 0 || s.QueueIdx >= len(s.MPVPlaylist) {
		return s, nil
	}
	removed := s.MPVPlaylist[s.QueueIdx]
	s.MPVPlaylist = append(s.MPVPlaylist[:s.QueueIdx], s.MPVPlaylist[s.QueueIdx+1:]...)
	if s.QueueIdx >= len(s.MPVPlaylist) && s.QueueIdx > 0 {
		s.QueueIdx--
	}
	for i, v := range s.Queue {
		if (removed.Filename != "" && v.ID == removed.Filename) || v.Title == removed.Title {
			s.Queue = append(s.Queue[:i], s.Queue[i+1:]...)
			break
		}
	}
	if pc, ok := s.Deps.Engine.(playlistController); ok && s.PlayerStatus.IsRunning {
		idx := removed.Index
		toast := s.toastCmd("✕ Quitado de la cola")
		rmCmd := func() tea.Msg {
			if err := pc.RemovePlaylistIndex(s.appCtx, idx); err != nil {
				return ActionResultMsg{Err: err}
			}
			items, err := pc.Playlist(s.appCtx)
			return QueueLoadedMsg{Items: items, Err: err}
		}
		return s, tea.Batch(toast, rmCmd)
	}
	return s, s.toastCmd("✕ Quitado de la cola")
}

// ClearQueue clears queued tracks in state and in the active mpv playlist.
func (s *State) ClearQueue() (*State, tea.Cmd) {
	s.Queue = nil
	s.MPVPlaylist = nil
	s.QueueIdx = 0
	if pc, ok := s.Deps.Engine.(playlistController); ok && s.PlayerStatus.IsRunning {
		return s, s.engineCmd(func(ctx context.Context) error {
			return pc.ClearPlaylist(ctx)
		}, "🗑 Cola vaciada")
	}
	return s, s.toastCmd("🗑 Cola vaciada")
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
	label := fmt.Sprintf("⚡ Velocidad %sx", speedStr)
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
		return s, s.toastCmd("Repetir no soportado")
	}
	next := !s.PlayerStatus.LoopFile
	s.PlayerStatus.LoopFile = next
	return s, func() tea.Msg {
		enabled, err := lt.ToggleLoop(s.appCtx)
		return LoopToggledMsg{Enabled: enabled, Err: err}
	}
}

// QualityLabel returns a human-readable badge for the current max vertical resolution or audio-only mode.
func (s *State) QualityLabel() string {
	if s.AudioOnly {
		return "Solo Audio"
	}
	if s.Quality <= 0 {
		return "MAX"
	}
	if s.Quality <= 360 {
		return fmt.Sprintf("%dp Ahorro", s.Quality)
	}
	return fmt.Sprintf("%dp", s.Quality)
}

// OpenQualityModal opens the resolution / data-saver / audio-only selector centered on the current preset.
func (s *State) OpenQualityModal() *State {
	s.ShowHelp = false
	s.ShowSettings = false
	s.ShowChapters = false
	s.ShowQualityModal = true
	s.QualityIdx = s.currentQualityOptionIndex()
	return s
}

func (s *State) currentQualityOptionIndex() int {
	if s.AudioOnly {
		return len(QualityOptions) - 1
	}
	for i, opt := range QualityOptions {
		if !opt.AudioOnly && opt.Quality == s.Quality {
			return i
		}
	}
	return 2 // 720p HD fallback
}

// ApplyQualityOption applies preset idx from QualityOptions, updates the engine,
// persists configuration, and displays a confirmation toast.
func (s *State) ApplyQualityOption(idx int) (*State, tea.Cmd) {
	if idx < 0 || idx >= len(QualityOptions) {
		idx = 2
	}
	opt := QualityOptions[idx]
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

	var cmds []tea.Cmd
	if label == "" {
		if audioOnly {
			label = "Solo Audio"
		} else if quality <= 0 {
			label = "Máxima (best)"
		} else {
			label = fmt.Sprintf("%dp", quality)
		}
	}
	toastPrefix := "🎬 Calidad: "
	if audioOnly {
		toastPrefix = "🎧 Modo: "
	}
	cmds = append(cmds, s.toastCmd(toastPrefix+label))

	selVid, hasSel := s.SelectedVideo()
	prefetcher, hasPrefetch := s.Deps.Engine.(streamPrefetcher)

	if ps, ok := s.Deps.Engine.(playbackProfileSetter); ok {
		q := quality
		ao := audioOnly
		cmds = append(cmds, func() tea.Msg {
			if err := ps.SetPlaybackProfile(s.appCtx, q, ao); err != nil {
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
		qs.SetQuality(s.appCtx, quality)
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
	if s.AudioOnly {
		return s.applyPlaybackProfile(0, false, "Máxima (best)")
	}
	if s.Quality == qualityPresets[len(qualityPresets)-1] {
		return s.applyPlaybackProfile(s.Quality, true, "Solo Audio (sin ventana)")
	}
	next := qualityPresets[0]
	for i, q := range qualityPresets {
		if q == s.Quality {
			next = qualityPresets[(i+1)%len(qualityPresets)]
			break
		}
	}
	label := "Máxima (best)"
	if next > 0 {
		label = fmt.Sprintf("%dp", next)
	}
	return s.applyPlaybackProfile(next, false, label)
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
