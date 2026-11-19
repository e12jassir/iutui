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

func (c *IPCClient) scheduleMixAppend(mixCtx context.Context, seq uint64, plan resolvedPlayTarget) {
	if (!plan.isMix && !plan.isAutoplay) || plan.seedID == "" || (plan.playlistFetched && len(plan.playlistIDs) == 0) {
		return
	}
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

	if len(playlistIDs) == 0 {
		playlistIDs = c.fetchPlaylistIDs(mixCtx, targetURL, 20)
	}
	if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
		return
	}

	remaining := deduplicateTrackIDs(playlistIDs, seedID)
	if len(remaining) == 0 && isAutoplay {
		c.fallbackMu.RLock()
		fallbackIDs := append([]string(nil), c.autoplayFallbackIDs...)
		c.fallbackMu.RUnlock()
		remaining = deduplicateTrackIDs(fallbackIDs, seedID)
	}
	if len(remaining) == 0 {
		return
	}

	if c.resolver != nil {
		for i := 0; i < len(remaining) && i < 2; i++ {
			c.resolver.Prefetch(mixCtx, model.Video{ID: remaining[i]})
		}
	}

	if !c.isConnected() && !c.tryConnectImmediateCtx(mixCtx, 2*time.Second) {
		return
	}

	for _, id := range remaining {
		if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
			return
		}
		if _, err := c.sendCommand(mixCtx, "loadfile", "https://www.youtube.com/watch?v="+id, "append"); err != nil {
			return
		}
	}
}

func deduplicateTrackIDs(rawIDs []string, excludeID string) []string {
	seen := map[string]struct{}{excludeID: {}}
	var out []string
	for _, id := range rawIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
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

// Enqueue appends v to the active playlist or starts playback if no mpv instance is connected.
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

	if !c.isConnected() {
		_ = c.tryConnectImmediateCtx(ctx, 150*time.Millisecond)
	}
	if c.isConnected() {
		url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", v.ID)
		if v.Title != "" {
			if _, err := c.sendCommand(ctx, "loadfile", url, "append-play", -1, map[string]any{"force-media-title": v.Title}); err == nil {
				return nil
			}
		}
		_, err := c.sendCommand(ctx, "loadfile", url, "append-play")
		return err
	}
	return c.Play(ctx, v, "")
}
