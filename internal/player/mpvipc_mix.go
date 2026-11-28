package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
)

// Prefetch asynchronously resolves direct stream URLs for v in the background.
func (c *IPCClient) Prefetch(v model.Video) {
	if c.resolver != nil {
		c.resolver.Prefetch(c.ctx, v)
	}
}

// SetAutoplay enables or disables automatic continuous queueing for single videos.
func (c *IPCClient) SetAutoplay(enabled bool) {
	if c == nil {
		return
	}
	c.autoplay.Store(enabled)
}

// SetAutoplayFallback configures fallback video IDs used by autoplay when
// YouTube does not return an RD Mix playlist for the active video.
func (c *IPCClient) SetAutoplayFallback(ids []string) {
	if c == nil {
		return
	}
	copied := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			copied = append(copied, trimmed)
		}
	}
	c.fallbackMu.Lock()
	c.autoplayFallbackIDs = copied
	c.fallbackMu.Unlock()
}

// SetRadioTaste updates the listener's liked/history taste signals used to
// curate smart radio queues while preserving in-session skip/seed feedback.
func (c *IPCClient) SetRadioTaste(taste TasteProfile) {
	if c == nil {
		return
	}
	c.tasteMu.Lock()
	c.taste = taste
	c.tasteMu.Unlock()
}

func (c *IPCClient) snapshotTasteLocked() (TasteProfile, []string) {
	c.tasteMu.RLock()
	tp := TasteProfile{
		LikedArtists:   c.taste.LikedArtists,
		LikedTracks:    c.taste.LikedTracks,
		HistoryArtists: c.taste.HistoryArtists,
		HistoryTracks:  c.taste.HistoryTracks,
		SkippedArtists: make(map[string]float64, len(c.taste.SkippedArtists)+len(c.sessionSkippedArtists)),
		SkippedTracks:  make(map[string]bool, len(c.taste.SkippedTracks)+len(c.sessionSkippedTracks)),
	}
	for k, v := range c.taste.SkippedArtists {
		tp.SkippedArtists[k] += v
	}
	for k, v := range c.sessionSkippedArtists {
		tp.SkippedArtists[k] += v
	}
	for k, v := range c.taste.SkippedTracks {
		if v {
			tp.SkippedTracks[k] = true
		}
	}
	for k, v := range c.sessionSkippedTracks {
		if v {
			tp.SkippedTracks[k] = true
		}
	}
	var extras []string
	for _, s := range c.completedSeeds {
		if s.ID != "" {
			extras = append(extras, s.ID)
		}
	}
	c.tasteMu.RUnlock()
	return tp, extras
}

// RecordCompletedTrack remembers a track that the listener stayed with so future
// radio extensions can blend up to 3 recent session tracks as a multi-track seed.
func (c *IPCClient) RecordCompletedTrack(v model.Video) {
	if c == nil {
		return
	}
	vid := ExtractYouTubeID(v.ID)
	if vid == "" {
		return
	}
	c.tasteMu.Lock()
	defer c.tasteMu.Unlock()
	next := make([]RadioTrack, 0, 3)
	next = append(next, RadioTrack{ID: vid, Title: v.Title, Artist: v.Channel})
	for _, prev := range c.completedSeeds {
		if prev.ID == vid {
			continue
		}
		next = append(next, prev)
		if len(next) >= 3 {
			break
		}
	}
	c.completedSeeds = next
}

// RecordFastSkip learns an in-session fast skip, demoting the track & artist
// and removing upcoming radio tracks by the same artist (never user-queued tracks).
func (c *IPCClient) RecordFastSkip(v model.Video) {
	if c == nil {
		return
	}
	vid := ExtractYouTubeID(v.ID)
	songKey := canonicalSongKey(v.Title, v.Channel)
	artistKey := primaryArtistKey(v.Channel, v.Title)

	c.tasteMu.Lock()
	if c.sessionSkippedTracks == nil {
		c.sessionSkippedTracks = make(map[string]bool)
	}
	if vid != "" {
		c.sessionSkippedTracks[vid] = true
	}
	if songKey != "" {
		c.sessionSkippedTracks[songKey] = true
	}
	if artistKey != "" {
		if c.sessionSkippedArtists == nil {
			c.sessionSkippedArtists = make(map[string]float64)
		}
		c.sessionSkippedArtists[artistKey] += 1.0
	}
	c.tasteMu.Unlock()

	if artistKey != "" && c.isConnected() && c.ctx.Err() == nil {
		c.spawnPruneSkippedArtist(artistKey)
	}
}

func (c *IPCClient) spawnPruneSkippedArtist(artistKey string) {
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}
	go func() {
		defer c.wg.Done()
		c.pruneSkippedArtistFromPlaylist(artistKey)
	}()
}

func (c *IPCClient) pruneSkippedArtistFromPlaylist(artistKey string) {
	if artistKey == "" || !c.isConnected() {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 1500*time.Millisecond)
	defer cancel()

	items, err := c.Playlist(ctx)
	if err != nil || len(items) == 0 {
		return
	}
	st := c.Status()
	curPos := st.PlaylistPos
	if curPos < 0 {
		for _, it := range items {
			if it.Current {
				curPos = it.Index
				break
			}
		}
	}
	if curPos < 0 {
		curPos = 0
	}

	c.playlistMu.Lock()
	userQ := make(map[string]bool, len(c.userQueuedIDs))
	for _, qid := range c.userQueuedIDs {
		if qid != "" {
			userQ[qid] = true
		}
	}
	c.playlistMu.Unlock()

	var toRemove []int
	for _, it := range items {
		if it.Index <= curPos {
			continue
		}
		vid := c.extractTrackVideoID(it.Filename)
		if vid != "" && userQ[vid] {
			continue
		}
		itemArtist := primaryArtistKey(it.Artist, it.Title)
		if itemArtist == artistKey {
			toRemove = append(toRemove, it.Index)
			if len(toRemove) >= 3 {
				break
			}
		}
	}
	for i := len(toRemove) - 1; i >= 0; i-- {
		if _, err := c.sendCommand(ctx, "playlist-remove", toRemove[i]); err != nil {
			return
		}
	}
}

func (c *IPCClient) cacheRadioTracks(tracks []RadioTrack) {
	if len(tracks) == 0 {
		return
	}
	c.metaMu.Lock()
	if c.trackMeta == nil {
		c.trackMeta = make(map[string]RadioTrack, len(tracks))
	}
	for _, tr := range tracks {
		id := ExtractYouTubeID(tr.ID)
		if id == "" {
			id = strings.TrimSpace(tr.ID)
		}
		if id == "" {
			continue
		}
		tr.ID = id
		if tr.Title != "" || tr.Artist != "" {
			c.trackMeta[id] = tr
		}
	}
	c.metaMu.Unlock()
}

func (c *IPCClient) lookupDisplayTitle(videoID string) (string, bool) {
	if videoID == "" {
		return "", false
	}
	c.metaMu.RLock()
	tr, ok := c.trackMeta[videoID]
	c.metaMu.RUnlock()
	if !ok {
		return "", false
	}
	title := tr.DisplayTitle()
	return title, title != ""
}

func (c *IPCClient) onNewPlayTrack(v model.Video) {
	vid := ExtractYouTubeID(v.ID)
	if vid == "" {
		vid = model.MixSeedVideoID(v.ID)
	}
	if vid != "" && v.Title != "" {
		c.cacheRadioTracks([]RadioTrack{{
			ID:     vid,
			Title:  v.Title,
			Artist: v.Channel,
		}})
	}
	c.resetMixQueueState(vid)
}

func (c *IPCClient) resetMixQueueState(seedID string) {
	c.playlistMu.Lock()
	c.userQueuedIDs = nil
	c.extendingSeq = 0
	if seedID != "" {
		c.appendPlayedHistoryLocked(seedID)
	}
	c.playlistMu.Unlock()
}

func (c *IPCClient) recordPlayedTrackID(videoID string) {
	if videoID == "" {
		return
	}
	c.playlistMu.Lock()
	for i, id := range c.userQueuedIDs {
		if id == videoID {
			c.userQueuedIDs = append(c.userQueuedIDs[:i], c.userQueuedIDs[i+1:]...)
			break
		}
	}
	c.appendPlayedHistoryLocked(videoID)
	c.playlistMu.Unlock()
}

func (c *IPCClient) appendPlayedHistoryLocked(videoID string) {
	if videoID == "" {
		return
	}
	for i, id := range c.playedHistoryIDs {
		if id == videoID {
			c.playedHistoryIDs = append(c.playedHistoryIDs[:i], c.playedHistoryIDs[i+1:]...)
			break
		}
	}
	c.playedHistoryIDs = append(c.playedHistoryIDs, videoID)
	if len(c.playedHistoryIDs) > 100 {
		c.playedHistoryIDs = c.playedHistoryIDs[len(c.playedHistoryIDs)-100:]
	}
}

func (c *IPCClient) scheduleMixAppend(mixCtx context.Context, seq uint64, plan resolvedPlayTarget) {
	if (!plan.isMix && !plan.isAutoplay) || plan.seedID == "" || (plan.playlistFetched && len(plan.playlistIDs) == 0) {
		return
	}
	c.resetMixQueueState(plan.seedID)
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && c.isCurrentPlay(seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if canSpawn {
		go c.appendMixTracks(mixCtx, seq, plan.targetURL, plan.seedID, plan.playlistIDs, plan.isAutoplay)
	}
}

func (c *IPCClient) appendMixTracks(mixCtx context.Context, seq uint64, targetURL, seedID string, playlistIDs []string, isAutoplay bool) {
	defer c.wg.Done()

	if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
		return
	}

	taste, extraSeeds := c.snapshotTasteLocked()

	var candidates []RadioTrack
	if len(playlistIDs) > 0 {
		candidates = idsToRadioTracks(playlistIDs)
	} else {
		if c.radioClient != nil && (isAutoplay || strings.Contains(targetURL, "list=RD")) {
			if fetched, err := c.radioClient.FetchMultiSeedRadio(mixCtx, seedID, extraSeeds, 40); err == nil && len(fetched) > 0 {
				candidates = fetched
			}
		}
		if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
			return
		}
		if len(candidates) == 0 {
			candidates = idsToRadioTracks(c.fetchPlaylistIDs(mixCtx, targetURL, 20))
		}
	}
	if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
		return
	}

	if len(candidates) == 0 && isAutoplay {
		c.fallbackMu.RLock()
		fallbackIDs := append([]string(nil), c.autoplayFallbackIDs...)
		c.fallbackMu.RUnlock()
		candidates = idsToRadioTracks(fallbackIDs)
	}

	c.playlistMu.Lock()
	recentIDs := append([]string(nil), c.playedHistoryIDs...)
	c.playlistMu.Unlock()

	curated := CurateRadioQueue(seedID, candidates, taste, recentIDs, 25)
	if len(curated) == 0 && isAutoplay {
		c.fallbackMu.RLock()
		fallbackIDs := append([]string(nil), c.autoplayFallbackIDs...)
		c.fallbackMu.RUnlock()
		if len(fallbackIDs) > 0 {
			curated = CurateRadioQueue(seedID, idsToRadioTracks(fallbackIDs), taste, recentIDs, 25)
		}
	}
	if len(curated) == 0 {
		return
	}
	c.cacheRadioTracks(curated)

	if c.resolver != nil {
		for i := 0; i < len(curated) && i < 2; i++ {
			c.resolver.Prefetch(mixCtx, model.Video{ID: curated[i].ID, Title: curated[i].DisplayTitle()})
		}
	}

	if !c.isConnected() && !c.tryConnectImmediateCtx(mixCtx, 2*time.Second) {
		return
	}

	for _, tr := range curated {
		if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
			return
		}
		if err := c.appendTrackToMPV(mixCtx, tr, "append"); err != nil {
			return
		}
	}
}

func (c *IPCClient) appendTrackToMPV(ctx context.Context, tr RadioTrack, appendMode string) error {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", tr.ID)
	if title := tr.DisplayTitle(); title != "" {
		if _, err := c.sendCommand(ctx, "loadfile", url, appendMode, -1, map[string]any{"force-media-title": title}); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	_, err := c.sendCommand(ctx, "loadfile", url, appendMode)
	return err
}

const maxResolvedURLMappings = 256

func (c *IPCClient) registerResolvedURL(cdnURL, videoID string) {
	cdnURL = strings.TrimSpace(cdnURL)
	vid := ExtractYouTubeID(videoID)
	if vid == "" {
		vid = strings.TrimSpace(videoID)
	}
	if cdnURL == "" || vid == "" {
		return
	}
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	if c.resolvedURLToID == nil {
		c.resolvedURLToID = make(map[string]string, 32)
	}
	if _, exists := c.resolvedURLToID[cdnURL]; !exists {
		c.resolvedURLOrder = append(c.resolvedURLOrder, cdnURL)
		for len(c.resolvedURLOrder) > maxResolvedURLMappings {
			oldest := c.resolvedURLOrder[0]
			c.resolvedURLOrder = c.resolvedURLOrder[1:]
			delete(c.resolvedURLToID, oldest)
		}
	}
	c.resolvedURLToID[cdnURL] = vid
}

func (c *IPCClient) extractTrackVideoID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if vid := ExtractYouTubeID(trimmed); vid != "" {
		return vid
	}
	c.metaMu.RLock()
	vid := c.resolvedURLToID[trimmed]
	c.metaMu.RUnlock()
	return vid
}

func (c *IPCClient) hasResolvedVideoID(videoID string) bool {
	if videoID == "" {
		return false
	}
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	for _, vid := range c.resolvedURLToID {
		if vid == videoID {
			return true
		}
	}
	return false
}

func (c *IPCClient) wantAudioOnlyPlayback() bool {
	if c.hasManagedProc() {
		return c.isProcAudioOnly()
	}
	c.launchMu.RLock()
	ao := c.launchDefaults.AudioOnly || c.launchDefaults.Mode == "audio"
	c.launchMu.RUnlock()
	return ao
}

func (c *IPCClient) spawnPrefetchUpcomingTracks(pos, count int) {
	if c.resolver == nil || pos < 0 || pos+1 >= count {
		return
	}
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}
	go func() {
		defer c.wg.Done()
		c.prefetchUpcomingTracks(pos, count)
	}()
}

func (c *IPCClient) spawnMaybeExtendRadio(activeVideoID string) {
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}
	go func() {
		defer c.wg.Done()
		c.maybeExtendRadio(activeVideoID)
	}()
}

func (c *IPCClient) hotSwapPlaylistSlotWithStream(ctx context.Context, targetIdx int, vid, fallbackTitle string, st ResolvedStream, wantAudio bool, requireFutureSlot bool) bool {
	var cdnURL, audioFile string
	if wantAudio {
		cdnURL = strings.TrimSpace(st.AudioURL)
		if cdnURL == "" {
			cdnURL = strings.TrimSpace(st.VideoURL)
		}
	} else {
		cdnURL = strings.TrimSpace(st.VideoURL)
		audioFile = strings.TrimSpace(st.AudioURL)
	}
	if cdnURL == "" {
		return false
	}

	c.playlistOpMu.Lock()
	defer c.playlistOpMu.Unlock()

	items, err := c.Playlist(ctx)
	if err != nil || targetIdx < 0 || targetIdx >= len(items) {
		return false
	}
	curPos := c.Status().PlaylistPos
	if curPos < 0 {
		for _, it := range items {
			if it.Current {
				curPos = it.Index
				break
			}
		}
	}
	if requireFutureSlot && curPos >= 0 && targetIdx <= curPos {
		return false
	}
	targetItem := items[targetIdx]
	if ExtractYouTubeID(targetItem.Filename) != vid {
		// Already swapped or playlist changed underneath us.
		return false
	}

	title := ""
	if disp, ok := c.lookupDisplayTitle(vid); ok && disp != "" {
		title = disp
	} else if trimmed := strings.TrimSpace(targetItem.Title); trimmed != "" && !strings.HasPrefix(trimmed, "http") {
		title = trimmed
	} else {
		title = strings.TrimSpace(fallbackTitle)
	}

	c.registerResolvedURL(cdnURL, vid)
	opts := map[string]any{}
	if title != "" {
		opts["force-media-title"] = title
	}
	if audioFile != "" {
		opts["audio-file"] = audioFile
	}

	var appendErr error
	if len(opts) > 0 {
		_, appendErr = c.sendCommand(ctx, "loadfile", cdnURL, "append", -1, opts)
	}
	if len(opts) == 0 || appendErr != nil {
		if ctx.Err() != nil {
			return false
		}
		_, appendErr = c.sendCommand(ctx, "loadfile", cdnURL, "append")
	}
	if appendErr != nil {
		return false
	}

	newLastIdx := len(items)
	if newLastIdx > targetIdx {
		if _, err := c.sendCommand(ctx, "playlist-move", newLastIdx, targetIdx); err != nil {
			_, _ = c.sendCommand(ctx, "playlist-remove", newLastIdx)
			return false
		}
		_, _ = c.sendCommand(ctx, "playlist-remove", targetIdx+1)
	}
	return true
}

func (c *IPCClient) maybeSwapPlaylistTargetWithCachedCDN(ctx context.Context, cmd string, explicitIdx int) {
	if c.resolver == nil || !c.isConnected() {
		return
	}
	swapCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	items, err := c.Playlist(swapCtx)
	if err != nil || len(items) == 0 {
		return
	}

	targetIdx := explicitIdx
	if targetIdx < 0 {
		curPos := c.Status().PlaylistPos
		if curPos < 0 {
			for _, it := range items {
				if it.Current {
					curPos = it.Index
					break
				}
			}
		}
		if curPos < 0 {
			return
		}
		switch cmd {
		case "playlist-next":
			targetIdx = curPos + 1
		case "playlist-prev":
			targetIdx = curPos - 1
		default:
			return
		}
	}
	if targetIdx < 0 || targetIdx >= len(items) {
		return
	}
	vid := ExtractYouTubeID(items[targetIdx].Filename)
	if vid == "" {
		return
	}
	wantAudio := c.wantAudioOnlyPlayback()
	if st, ok := c.resolver.GetCachedForMode(vid, wantAudio); ok {
		c.hotSwapPlaylistSlotWithStream(swapCtx, targetIdx, vid, items[targetIdx].Title, st, wantAudio, false)
	}
}

func (c *IPCClient) prefetchUpcomingTracks(pos, count int) {
	if c.resolver == nil || pos < 0 || pos+1 >= count {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 1500*time.Millisecond)
	items, err := c.Playlist(ctx)
	cancel()
	if err != nil {
		return
	}
	wantAudio := c.wantAudioOnlyPlayback()

	type targetSlot struct {
		idx   int
		vid   string
		title string
	}
	var targets []targetSlot
	for offset := 1; offset <= 2; offset++ {
		idx := pos + offset
		if idx < 0 || idx >= len(items) {
			break
		}
		vid := ExtractYouTubeID(items[idx].Filename)
		if vid == "" {
			continue
		}
		c.resolver.Prefetch(c.ctx, model.Video{ID: vid, Title: items[idx].Title})
		targets = append(targets, targetSlot{idx: idx, vid: vid, title: items[idx].Title})
	}

	for _, t := range targets {
		if c.ctx.Err() != nil || !c.isConnected() {
			return
		}
		waitCtx, waitCancel := context.WithTimeout(c.ctx, streamResolveLimit)
		st, ok := c.resolver.WaitOrGetForMode(waitCtx, t.vid, wantAudio, streamResolveLimit)
		waitCancel()
		if !ok || c.ctx.Err() != nil || !c.isConnected() {
			continue
		}
		swapCtx, swapCancel := context.WithTimeout(c.ctx, 1500*time.Millisecond)
		c.hotSwapPlaylistSlotWithStream(swapCtx, t.idx, t.vid, t.title, st, wantAudio, true)
		swapCancel()
	}
}

func (c *IPCClient) maybeExtendRadio(activeVideoID string) {
	if c.radioClient == nil || !c.autoplay.Load() || !c.isConnected() {
		return
	}
	seq := c.playSeq.Load()
	if seq == 0 {
		return
	}

	c.playlistMu.Lock()
	if c.extendingSeq == seq {
		c.playlistMu.Unlock()
		return
	}
	c.extendingSeq = seq
	recentIDs := append([]string(nil), c.playedHistoryIDs...)
	c.playlistMu.Unlock()

	defer func() {
		c.playlistMu.Lock()
		if c.extendingSeq == seq {
			c.extendingSeq = 0
		}
		c.playlistMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	items, err := c.Playlist(ctx)
	if err == nil {
		for _, it := range items {
			if id := c.extractTrackVideoID(it.Filename); id != "" {
				recentIDs = append(recentIDs, id)
			}
		}
	}

	seedID := c.extractTrackVideoID(activeVideoID)
	if seedID == "" && len(items) > 0 {
		for i := len(items) - 1; i >= 0; i-- {
			if id := c.extractTrackVideoID(items[i].Filename); id != "" {
				seedID = id
				break
			}
		}
	}
	if seedID == "" || !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return
	}

	taste, extraSeeds := c.snapshotTasteLocked()

	candidates, err := c.radioClient.FetchMultiSeedRadio(ctx, seedID, extraSeeds, 40)
	if err != nil || len(candidates) == 0 || !c.isCurrentPlay(seq) {
		return
	}

	curated := CurateRadioQueue(seedID, candidates, taste, recentIDs, 15)
	if len(curated) == 0 || !c.isCurrentPlay(seq) {
		return
	}
	c.cacheRadioTracks(curated)

	for _, tr := range curated {
		if !c.isCurrentPlay(seq) || ctx.Err() != nil {
			return
		}
		if err := c.appendTrackToMPV(ctx, tr, "append"); err != nil {
			return
		}
	}
}

func idsToRadioTracks(ids []string) []RadioTrack {
	if len(ids) == 0 {
		return nil
	}
	out := make([]RadioTrack, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, RadioTrack{ID: id})
	}
	return out
}

func (c *IPCClient) fetchPlaylistIDs(ctx context.Context, targetURL string, limit int) []string {
	if targetURL == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if limit <= 0 {
		limit = 25
	}

	c.launchMu.RLock()
	ytdlpBin := c.launchDefaults.YtdlpBin
	cookieJar := c.launchDefaults.CookieJarPath
	c.launchMu.RUnlock()
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}

	args := []string{
		"--flat-playlist",
		"--yes-playlist",
		"--dump-json",
		"--no-warnings",
		"--ignore-errors",
		"--socket-timeout", "8",
		"--playlist-end", strconv.Itoa(limit),
	}
	if isValidCookieJar(cookieJar) {
		args = append(args, "--cookies", cookieJar)
	}
	args = append(args, targetURL)

	cmd := exec.CommandContext(ctx, ytdlpBin, args...)
	platform.SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return platform.KillProcessGroup(cmd.Process.Pid)
	}
	cmd.WaitDelay = 100 * time.Millisecond

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil && stdout.Len() == 0 {
		return nil
	}

	return parsePlaylistIDs(stdout.Bytes(), limit)
}

func parsePlaylistIDs(data []byte, limit int) []string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}

	var ids []string
	seen := make(map[string]struct{})
	addID := func(raw string) {
		id := strings.TrimSpace(raw)
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if trimmed[0] == '{' {
		var pl struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(trimmed, &pl); err == nil && len(pl.Entries) > 0 {
			for _, e := range pl.Entries {
				addID(e.ID)
				if limit > 0 && len(ids) >= limit {
					break
				}
			}
			return ids
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var entry struct {
			ID   string `json:"id"`
			Type string `json:"_type"`
		}
		if err := json.Unmarshal(line, &entry); err == nil && entry.Type != "playlist" {
			addID(entry.ID)
			if limit > 0 && len(ids) >= limit {
				break
			}
		}
	}
	return ids
}

// Enqueue appends v to the active playlist (inserting right after the current
// track ahead of autoplay radio tracks) or starts playback if no mpv instance
// is connected.
func (c *IPCClient) Enqueue(ctx context.Context, v model.Video) error {
	if c.ctx.Err() != nil {
		return ErrClientClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	vid := ExtractYouTubeID(v.ID)
	if vid == "" {
		vid = strings.TrimSpace(v.ID)
	}
	tr := RadioTrack{
		ID:     vid,
		Title:  v.Title,
		Artist: v.Channel,
	}
	c.cacheRadioTracks([]RadioTrack{tr})

	if !c.isConnected() {
		_ = c.tryConnectImmediateCtx(ctx, 150*time.Millisecond)
	}
	if c.isConnected() {
		if err := c.appendTrackToMPV(ctx, tr, "append-play"); err != nil {
			return err
		}
		st := c.Status()
		if st.PlaylistPos >= 0 && st.PlaylistCount > 1 {
			c.playlistMu.Lock()
			offset := len(c.userQueuedIDs)
			c.userQueuedIDs = append(c.userQueuedIDs, vid)
			c.playlistMu.Unlock()

			if cntResp, err := c.sendCommand(ctx, "get_property", "playlist-count"); err == nil && cntResp.Data != nil {
				if f, ok := toFloat64(cntResp.Data); ok {
					newCount := int(f)
					fromIdx := newCount - 1
					toIdx := st.PlaylistPos + 1 + offset
					if toIdx < st.PlaylistPos+1 {
						toIdx = st.PlaylistPos + 1
					}
					if fromIdx > toIdx {
						_, _ = c.sendCommand(ctx, "playlist-move", fromIdx, toIdx)
					}
				}
			}
		}
		return nil
	}
	return c.Play(ctx, v, "")
}
