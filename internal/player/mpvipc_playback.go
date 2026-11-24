package player

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
)

const streamWaitTimeout = 2200 * time.Millisecond

type resolvedPlayTarget struct {
	targetURL           string
	url                 string
	fallbackURL         string
	audioFile           string
	seedID              string
	effectiveMode       string
	playlistIDs         []string
	playlistFetched     bool
	isMix               bool
	isAutoplay          bool
	launchAllowPlaylist bool
}

type playFallbackState struct {
	seq         uint64
	video       model.Video
	plan        resolvedPlayTarget
	fallbackURL string
	armed       bool
}

func (c *IPCClient) armDirectStreamFallback(seq uint64, v model.Video, plan resolvedPlayTarget) {
	c.fallbackPlayMu.Lock()
	defer c.fallbackPlayMu.Unlock()
	if plan.fallbackURL != "" && plan.url != plan.fallbackURL {
		c.playFallback = playFallbackState{
			seq:         seq,
			video:       v,
			plan:        plan,
			fallbackURL: plan.fallbackURL,
			armed:       true,
		}
		return
	}
	c.playFallback = playFallbackState{}
}

func (c *IPCClient) clearDirectStreamFallback(seq uint64) {
	c.fallbackPlayMu.Lock()
	if seq == 0 || c.playFallback.seq == seq {
		c.playFallback = playFallbackState{}
	}
	c.fallbackPlayMu.Unlock()
}

func (c *IPCClient) takeDirectStreamFallback() (playFallbackState, bool) {
	c.fallbackPlayMu.Lock()
	defer c.fallbackPlayMu.Unlock()
	if !c.playFallback.armed || !c.isCurrentPlay(c.playFallback.seq) {
		return playFallbackState{}, false
	}
	fb := c.playFallback
	c.playFallback = playFallbackState{}
	return fb, true
}

func (c *IPCClient) spawnDirectStreamFallback(fb playFallbackState) {
	if c.resolver != nil {
		c.resolver.Invalidate(fb.video.ID)
		if fb.plan.seedID != "" && fb.plan.seedID != fb.video.ID {
			c.resolver.Invalidate(fb.plan.seedID)
		}
	}

	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && c.isCurrentPlay(fb.seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}

	go func() {
		defer c.wg.Done()
		if !c.isCurrentPlay(fb.seq) || c.ctx.Err() != nil {
			return
		}

		c.playMu.Lock()
		if !c.isCurrentPlay(fb.seq) || c.ctx.Err() != nil {
			c.playMu.Unlock()
			return
		}
		prevCancel := c.playCancel
		mixCtx, mixCancel := context.WithCancel(c.ctx)
		c.playCancel = mixCancel
		c.playMu.Unlock()
		if prevCancel != nil {
			prevCancel()
		}

		c.step3Mu.Lock()
		if !c.isCurrentPlay(fb.seq) || c.ctx.Err() != nil || !c.isConnected() {
			c.step3Mu.Unlock()
			return
		}

		cmdCtx, cancel := context.WithTimeout(mixCtx, 2*time.Second)
		_, _ = c.sendCommand(cmdCtx, "set_property", "audio-files", []string{})
		_, _ = c.sendCommand(cmdCtx, "set_property", "force-media-title", "")
		_, _ = c.sendCommand(cmdCtx, "playlist-clear")

		perFileOpts := map[string]any{
			"start": "none",
		}
		if fb.video.Title != "" {
			perFileOpts["force-media-title"] = fb.video.Title
		}
		_, err := c.sendCommand(cmdCtx, "loadfile", fb.fallbackURL, "replace", -1, perFileOpts)
		if err != nil && c.isCurrentPlay(fb.seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(cmdCtx, "loadfile", fb.fallbackURL, "replace")
		}
		if err == nil && c.isCurrentPlay(fb.seq) {
			_, _ = c.sendCommand(cmdCtx, "set_property", "pause", false)
		}
		cancel()
		c.step3Mu.Unlock()

		if err == nil && c.isCurrentPlay(fb.seq) && mixCtx.Err() == nil {
			fbPlan := fb.plan
			fbPlan.url = fb.fallbackURL
			fbPlan.audioFile = ""
			c.scheduleMixAppend(mixCtx, fb.seq, fbPlan)
		}
	}()
}

func (c *IPCClient) hasManagedProc() bool {
	c.procMu.Lock()
	defer c.procMu.Unlock()
	if c.mpvProc == nil {
		return false
	}
	if !platform.ProcessAlive(c.mpvProc.Pid) {
		c.mpvProc = nil
		c.procAudioOnly = false
		return false
	}
	return true
}

func (c *IPCClient) isProcAudioOnly() bool {
	c.procMu.Lock()
	defer c.procMu.Unlock()
	return c.procAudioOnly
}

func (c *IPCClient) killManagedMPV() {
	c.procMu.Lock()
	proc := c.mpvProc
	c.mpvProc = nil
	c.procAudioOnly = false
	c.procMu.Unlock()
	if proc == nil {
		return
	}
	_ = platform.TerminateProcessGroup(proc.Pid)
	_ = proc.Kill()
}

func (c *IPCClient) isCurrentPlay(seq uint64) bool {
	return c.playSeq.Load() == seq
}

// Play starts playback of v using the active socket or launches mpv directly.
// Any active or in-flight playback is immediately interrupted so the latest Play wins.
func (c *IPCClient) Play(ctx context.Context, v model.Video, mode string) error {
	return c.playWithResume(ctx, v, mode, 0, false)
}

// PlayFrom starts playback of v from startPos seconds using the active socket or launches mpv directly.
func (c *IPCClient) PlayFrom(ctx context.Context, v model.Video, mode string, startPos float64) error {
	if startPos < 0 {
		startPos = 0
	}
	return c.playWithResume(ctx, v, mode, startPos, false)
}

func (c *IPCClient) playWithResume(ctx context.Context, v model.Video, mode string, startPos float64, startPaused bool) error {
	if c.ctx.Err() != nil {
		return ErrClientClosed
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	c.launchMu.Lock()
	c.lastVideo = v
	c.lastMode = mode
	c.launchMu.Unlock()

	c.playMu.Lock()
	seq := c.playSeq.Add(1)
	prevCancel := c.playCancel
	mixCtx, mixCancel := context.WithCancel(c.ctx)
	c.playCancel = mixCancel
	c.playMu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}

	c.clearSponsorBlockState(seq)
	c.clearDirectStreamFallback(0)

	waitCtx, waitCancel := context.WithCancel(mixCtx)
	defer waitCancel()
	if ctx != nil && ctx != context.Background() && ctx != context.TODO() {
		stopWatch := context.AfterFunc(ctx, waitCancel)
		defer stopWatch()
	}

	skipReconnect, ok := c.cutActivePlayback(waitCtx, seq)
	if !ok {
		return nil
	}

	if v.Title != "" {
		c.statusMu.Lock()
		c.status.Title = v.Title
		c.status.TimePos = startPos
		if startPos <= 0.5 {
			c.status.Duration = 0
		}
		st := c.status
		c.statusMu.Unlock()
		if st.IsRunning {
			c.broadcast(st)
		}
	}

	plan := c.resolvePlayTarget(waitCtx, v, mode)
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return nil
	}

	if err := c.playOnSingleMPV(ctx, waitCtx, seq, v, plan, skipReconnect, startPos, startPaused); err != nil {
		return err
	}

	c.initSponsorBlockForPlay(mixCtx, seq, v.ID)
	c.scheduleMixAppend(mixCtx, seq, plan)
	return nil
}

func (c *IPCClient) cutActivePlayback(waitCtx context.Context, seq uint64) (skipReconnect bool, ok bool) {
	if c.isConnected() || (c.hasManagedProc() && c.tryConnectImmediateCtx(waitCtx, 200*time.Millisecond)) {
		if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
			return false, false
		}
		if c.fadeDuration > 0 && c.isAudioActivelyPlaying() {
			fseq, fctx := c.beginFade()
			mergedCtx, cancelMerged := c.mergeFadeCtx(fctx, waitCtx)
			c.fadeOutSync(mergedCtx, fseq)
			cancelMerged()
			if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
				return false, false
			}
		}
		conn := c.getConn()
		stopCtx, stopCancel := context.WithTimeout(waitCtx, 500*time.Millisecond)
		_, stopErr := c.sendCommand(stopCtx, "stop")
		stopCancel()
		if stopErr != nil {
			if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
				return false, false
			}
			if conn != nil {
				c.handleDisconnect(conn)
			}
			if c.isConnected() && !c.hasManagedProc() {
				quitCtx, quitCancel := context.WithTimeout(waitCtx, 150*time.Millisecond)
				_, _ = c.sendCommand(quitCtx, "quit")
				quitCancel()
			}
			c.killManagedMPV()
			return true, true
		}
		return false, true
	}
	if c.hasManagedProc() {
		if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
			return false, false
		}
		if c.isConnected() && !c.hasManagedProc() {
			quitCtx, quitCancel := context.WithTimeout(waitCtx, 150*time.Millisecond)
			_, _ = c.sendCommand(quitCtx, "quit")
			quitCancel()
		}
		c.killManagedMPV()
		return true, true
	}
	return false, true
}

func (c *IPCClient) resolvePlayTarget(waitCtx context.Context, v model.Video, mode string) resolvedPlayTarget {
	c.launchMu.RLock()
	audioOnly := c.launchDefaults.AudioOnly
	c.launchMu.RUnlock()

	effectiveMode := mode
	if effectiveMode == "mix" {
		effectiveMode = "auto"
	}
	if audioOnly && (effectiveMode == "" || effectiveMode == "auto") {
		effectiveMode = "audio"
	}
	if mode != "mix" {
		if p := strings.TrimSpace(v.LocalPath); p != "" {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return resolvedPlayTarget{
					targetURL:     p,
					url:           p,
					effectiveMode: effectiveMode,
				}
			}
		}
	}
	targetURL, allowPlaylist, seedID := model.BuildTargetURL(v.ID, mode == "mix")

	plan := resolvedPlayTarget{
		targetURL:     targetURL,
		url:           targetURL,
		fallbackURL:   targetURL,
		seedID:        seedID,
		effectiveMode: effectiveMode,
		isMix:         mode == "mix" || model.IsPlaylistID(v.ID),
	}

	wantAudio := effectiveMode == "audio"
	applyResolved := func(st ResolvedStream) {
		if wantAudio {
			if st.AudioURL != "" {
				plan.url = st.AudioURL
			}
			plan.audioFile = ""
			return
		}
		if st.VideoURL != "" {
			plan.url = st.VideoURL
			plan.audioFile = st.AudioURL
		}
	}

	if !allowPlaylist {
		if mode != "mix" && c.autoplay.Load() && !v.IsLive && v.ID != "" {
			if seed := strings.TrimSpace(v.ID); seed != "" {
				plan.isAutoplay = true
				plan.seedID = seed
				plan.targetURL = "https://www.youtube.com/watch?v=" + seed + "&list=RD" + seed
			}
		}
		if c.resolver != nil {
			if st, ok := c.resolver.WaitOrGetForMode(waitCtx, v.ID, wantAudio, streamWaitTimeout); ok {
				applyResolved(st)
			}
		}
		return plan
	}

	if seedID != "" {
		plan.url = "https://www.youtube.com/watch?v=" + seedID
		plan.fallbackURL = plan.url
		if c.resolver != nil {
			if st, ok := c.resolver.WaitOrGetForMode(waitCtx, seedID, wantAudio, streamWaitTimeout); ok {
				applyResolved(st)
			}
		}
		return plan
	}

	ids := c.fetchPlaylistIDs(waitCtx, targetURL, 25)
	plan.playlistFetched = true
	if len(ids) > 0 {
		plan.seedID = ids[0]
		plan.url = "https://www.youtube.com/watch?v=" + plan.seedID
		plan.fallbackURL = plan.url
		plan.playlistIDs = ids[1:]
		if c.resolver != nil {
			if st, ok := c.resolver.WaitOrGetForMode(waitCtx, plan.seedID, wantAudio, streamWaitTimeout); ok {
				applyResolved(st)
			}
		}
		return plan
	}

	plan.launchAllowPlaylist = true
	return plan
}

func socketExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// CleanupOrphanSocket connects to any leftover mpv socket from an abrupt crash
// (e.g. SIGKILL) and asks it to quit cleanly before WarmUp or Play runs.
func (c *IPCClient) CleanupOrphanSocket() {
	if c == nil || c.ctx.Err() != nil {
		return
	}
	c.launchMu.RLock()
	sock := strings.TrimSpace(c.launchDefaults.SocketPath)
	c.launchMu.RUnlock()
	if sock == "" || !socketExists(sock) {
		return
	}
	if c.isConnected() || c.hasManagedProc() {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 180*time.Millisecond)
	defer cancel()
	if c.tryConnectImmediateCtx(ctx, 120*time.Millisecond) {
		_, _ = c.sendCommand(ctx, "quit")
		c.connMu.Lock()
		conn := c.conn
		c.conn = nil
		c.connMu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
	}
	_ = os.Remove(sock)
}

// WarmUp pre-launches an invisible idle mpv instance in audio-only mode so the
// first Play skips cold mpv process startup (~130ms) and connects immediately.
func (c *IPCClient) WarmUp() {
	if c == nil || c.ctx.Err() != nil {
		return
	}
	c.launchMu.RLock()
	opts := c.launchDefaults
	c.launchMu.RUnlock()

	if !(opts.AudioOnly || opts.Mode == "audio") || strings.TrimSpace(opts.SocketPath) == "" {
		return
	}
	if c.isConnected() || c.hasManagedProc() || socketExists(opts.SocketPath) {
		return
	}

	c.step3Mu.Lock()
	defer c.step3Mu.Unlock()
	if c.ctx.Err() != nil || c.isConnected() || c.hasManagedProc() {
		return
	}

	_ = os.MkdirAll(filepath.Dir(opts.SocketPath), 0o700)
	_ = os.Remove(opts.SocketPath)
	warmOpts := LaunchOpts{
		SocketPath:   opts.SocketPath,
		AudioOnly:    true,
		Mode:         "audio",
		Quality:      opts.Quality,
		Volume:       opts.Volume,
		Speed:        opts.Speed,
		FastPlayback: opts.FastPlayback,
	}
	proc, err := StartMPVProcess("mpv", warmOpts)
	if err != nil {
		return
	}
	c.procMu.Lock()
	c.mpvProc = proc
	c.procAudioOnly = true
	c.procMu.Unlock()
	c.signalWake()
}

func (c *IPCClient) playOnSingleMPV(ctx, waitCtx context.Context, seq uint64, v model.Video, plan resolvedPlayTarget, skipReconnect bool, startPos float64, startPaused bool) error {
	if !skipReconnect && !c.isConnected() {
		if c.hasManagedProc() || socketExists(c.socketFinder()) {
			_ = c.tryConnectImmediateCtx(waitCtx, 150*time.Millisecond)
		}
	}

	c.step3Mu.Lock()
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		c.step3Mu.Unlock()
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		c.step3Mu.Unlock()
		return ctx.Err()
	}

	if startPos <= 0.5 && !startPaused && c.tryPlayViaIPC(waitCtx, seq, v, plan) {
		c.step3Mu.Unlock()
		return nil
	}
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		c.step3Mu.Unlock()
		return nil
	}

	c.launchMu.RLock()
	opts := c.launchDefaults
	c.launchMu.RUnlock()

	if opts.SocketPath == "" {
		c.step3Mu.Unlock()
		opts.VideoID = v.ID
		opts.URL = plan.url
		opts.AudioFile = plan.audioFile
		opts.AllowPlaylist = plan.launchAllowPlaylist
		opts.AudioOnly = plan.effectiveMode == "audio"
		opts.Title = v.Title
		if plan.effectiveMode != "" && plan.effectiveMode != "auto" &&
			!(plan.effectiveMode == "video" && (opts.Mode == "pip" || opts.Mode == "fs")) {
			opts.Mode = plan.effectiveMode
		}
		opts.StartPos = startPos
		opts.StartPaused = startPaused
		opts.IsLive = v.IsLive
		return LaunchMPV("mpv", opts)
	}

	if c.isConnected() && !c.hasManagedProc() {
		quitCtx, quitCancel := context.WithTimeout(waitCtx, 150*time.Millisecond)
		_, _ = c.sendCommand(quitCtx, "quit")
		quitCancel()
	}
	c.killManagedMPV()
	c.procMu.Lock()
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		c.procMu.Unlock()
		c.step3Mu.Unlock()
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(opts.SocketPath), 0o700)
	_ = os.Remove(opts.SocketPath)
	opts.VideoID = v.ID
	opts.Title = v.Title
	opts.URL = plan.url
	opts.AudioFile = plan.audioFile
	opts.AllowPlaylist = plan.launchAllowPlaylist
	opts.IsLive = v.IsLive
	opts.StartPos = startPos
	opts.StartPaused = startPaused
	if plan.effectiveMode != "" && plan.effectiveMode != "auto" &&
		!(plan.effectiveMode == "video" && (opts.Mode == "pip" || opts.Mode == "fs")) {
		opts.Mode = plan.effectiveMode
	}
	switch plan.effectiveMode {
	case "video", "pip", "fs":
		opts.AudioOnly = false
	case "audio":
		opts.AudioOnly = true
	default:
		if !opts.AudioOnly && opts.Mode == "audio" {
			opts.Mode = "window"
		}
	}
	c.armDirectStreamFallback(seq, v, plan)
	proc, err := StartMPVProcess("mpv", opts)
	if err != nil {
		c.clearDirectStreamFallback(seq)
		c.procMu.Unlock()
		c.step3Mu.Unlock()
		return err
	}
	c.mpvProc = proc
	c.procAudioOnly = opts.AudioOnly || opts.Mode == "audio"
	c.procMu.Unlock()
	c.step3Mu.Unlock()
	c.signalWake()
	return nil
}

func (c *IPCClient) tryPlayViaIPC(waitCtx context.Context, seq uint64, v model.Video, plan resolvedPlayTarget) bool {
	if !c.isConnected() {
		return false
	}
	c.launchMu.RLock()
	audioOnly := c.launchDefaults.AudioOnly
	quality := c.launchDefaults.Quality
	c.launchMu.RUnlock()

	wantAudioOnly := false
	wantVid := "auto"
	switch plan.effectiveMode {
	case "audio":
		wantVid = "no"
		wantAudioOnly = true
	case "video", "pip", "fs":
		wantVid = "auto"
		wantAudioOnly = false
	default:
		if audioOnly {
			wantVid = "no"
			wantAudioOnly = true
		}
	}
	// If our managed mpv process was started in a different window mode (e.g.
	// video window vs --no-video --force-window=no), force a clean relaunch so
	// Wayland/Hyprland never leaves a blank window open or fails to open one.
	if c.hasManagedProc() && c.isProcAudioOnly() != wantAudioOnly {
		return false
	}
	_, _ = c.sendCommand(waitCtx, "set_property", "start", "none")
	_, _ = c.sendCommand(waitCtx, "set_property", "vid", wantVid)
	_, _ = c.sendCommand(waitCtx, "set_property", "ytdl-format", formatSelectorForVideo(quality, wantAudioOnly, v.IsLive))
	_, _ = c.sendCommand(waitCtx, "set_property", "audio-files", []string{})
	_, _ = c.sendCommand(waitCtx, "set_property", "force-media-title", "")
	_, _ = c.sendCommand(waitCtx, "playlist-clear")
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return true
	}

	var fseq uint64
	if c.fadeDuration > 0 {
		fseq, _ = c.beginFade()
		c.setGainBestEffort(waitCtx, fadeFloorDB)
		c.pendingFadeIn.Store(true)
	}

	c.armDirectStreamFallback(seq, v, plan)

	var err error
	perFileOpts := map[string]any{
		"start": "none",
	}
	if v.Title != "" {
		perFileOpts["force-media-title"] = v.Title
	}
	if plan.audioFile != "" {
		perFileOpts["audio-file"] = plan.audioFile
		_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace", -1, perFileOpts)
		if err != nil && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace", 0, "audio-file="+plan.audioFile)
		}
		if err != nil && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			c.clearDirectStreamFallback(seq)
			_, err = c.sendCommand(waitCtx, "loadfile", plan.fallbackURL, "replace")
		}
	} else {
		_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace", -1, perFileOpts)
		if err != nil && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace")
		}
		if err != nil && plan.url != plan.fallbackURL && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			c.clearDirectStreamFallback(seq)
			_, err = c.sendCommand(waitCtx, "loadfile", plan.fallbackURL, "replace")
		}
	}

	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return true
	}
	if err != nil {
		c.clearDirectStreamFallback(seq)
		if c.fadeDuration > 0 {
			c.pendingFadeIn.Store(false)
			c.setGainBestEffort(c.ctx, 0.0)
		}
		return false
	}

	c.onNewPlayTrack(v)
	c.statusMu.Lock()
	if v.Title != "" {
		c.status.Title = v.Title
	}
	if vid := ExtractYouTubeID(v.ID); vid != "" {
		c.status.VideoID = vid
	} else {
		c.status.VideoID = model.MixSeedVideoID(v.ID)
	}
	c.status.TimePos = 0
	c.status.Duration = 0
	st := c.status
	c.statusMu.Unlock()
	c.broadcast(st)
	if c.isCurrentPlay(seq) {
		_, _ = c.sendCommand(waitCtx, "set_property", "pause", false)
		if c.fadeDuration > 0 {
			c.scheduleFadeInSafety(fseq, max(200*time.Millisecond, 6*c.fadeDuration))
		}
	}
	return true
}
