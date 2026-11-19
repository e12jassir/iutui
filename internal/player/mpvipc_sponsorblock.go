package player

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const sponsorBlockSkipCooldown = 5 * time.Second

func (c *IPCClient) now() time.Time {
	if c != nil && c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

// SetSponsorBlock enables or disables automatic SponsorBlock segment skipping.
func (c *IPCClient) SetSponsorBlock(enabled bool) {
	if c == nil {
		return
	}
	c.sponsorBlock.Store(enabled)
	if !enabled {
		return
	}

	c.sbMu.Lock()
	videoID := c.sbVideoID
	seq := c.sbSeq
	needsFetch := videoID != "" && !c.sbFetched && c.sbClient != nil && c.isCurrentPlay(seq)
	c.sbMu.Unlock()
	if needsFetch {
		c.spawnSponsorBlockFetch(c.ctx, seq, videoID)
	}
}

// SponsorBlockEnabled reports whether automatic SponsorBlock segment skipping is enabled.
func (c *IPCClient) SponsorBlockEnabled() bool {
	if c == nil {
		return false
	}
	return c.sponsorBlock.Load()
}

func isSingleYouTubeVideoID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) != 11 || model.IsPlaylistID(id) {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !isAlnum && ch != '_' && ch != '-' {
			return false
		}
	}
	return true
}

func (c *IPCClient) clearSponsorBlockState(seq uint64) {
	c.sbMu.Lock()
	c.sbSeq = seq
	c.sbVideoID = ""
	c.sbSegments = nil
	c.sbLastSkipped = nil
	c.sbTimeObserved = false
	c.sbFetched = false
	c.sbMu.Unlock()
}

func (c *IPCClient) initSponsorBlockForPlay(ctx context.Context, seq uint64, rawVideoID string) {
	vid := strings.TrimSpace(rawVideoID)
	if !isSingleYouTubeVideoID(vid) {
		c.clearSponsorBlockState(seq)
		return
	}

	c.sbMu.Lock()
	if !c.isCurrentPlay(seq) {
		c.sbMu.Unlock()
		return
	}
	c.sbSeq = seq
	c.sbVideoID = vid
	c.sbSegments = nil
	c.sbLastSkipped = make(map[string]time.Time)
	c.sbTimeObserved = false
	c.sbFetched = false
	c.sbMu.Unlock()

	if c.SponsorBlockEnabled() && c.sbClient != nil {
		c.spawnSponsorBlockFetch(ctx, seq, vid)
	}
}

func (c *IPCClient) spawnSponsorBlockFetch(ctx context.Context, seq uint64, videoID string) {
	if ctx == nil {
		ctx = c.ctx
	}
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && ctx.Err() == nil && c.isCurrentPlay(seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if canSpawn {
		go c.fetchSponsorBlockSegments(ctx, seq, videoID)
	}
}

func (c *IPCClient) fetchSponsorBlockSegments(ctx context.Context, seq uint64, videoID string) {
	defer c.wg.Done()

	if !c.isCurrentPlay(seq) || ctx.Err() != nil || c.sbClient == nil {
		return
	}

	segs, err := c.sbClient.FetchSegments(ctx, videoID)
	if err != nil || !c.isCurrentPlay(seq) || ctx.Err() != nil {
		return
	}

	c.sbMu.Lock()
	observed := false
	if c.isCurrentPlay(seq) && c.sbSeq == seq && c.sbVideoID == videoID {
		c.sbSegments = segs
		c.sbFetched = true
		observed = c.sbTimeObserved
	}
	c.sbMu.Unlock()

	if observed && c.SponsorBlockEnabled() {
		st := c.Status()
		if st.IsRunning && !st.Paused {
			c.checkAndSkipSponsorSegment(st.TimePos)
		}
	}
}

func (c *IPCClient) markSponsorTimeObserved() {
	c.sbMu.Lock()
	if c.sbVideoID != "" {
		c.sbTimeObserved = true
	}
	c.sbMu.Unlock()
}

func (c *IPCClient) checkAndSkipSponsorSegment(timePos float64) {
	if !c.SponsorBlockEnabled() {
		return
	}

	now := c.now()
	var (
		shouldSkip bool
		targetEnd  float64
		seq        uint64
	)

	c.sbMu.Lock()
	if c.sbVideoID != "" && len(c.sbSegments) > 0 && c.isCurrentPlay(c.sbSeq) {
		for _, seg := range c.sbSegments {
			if timePos >= seg.Start && timePos < seg.End-0.5 {
				key := seg.UUID
				if key == "" {
					key = fmt.Sprintf("%.3f-%.3f", seg.Start, seg.End)
				}
				if last, ok := c.sbLastSkipped[key]; ok && now.Sub(last) < sponsorBlockSkipCooldown {
					continue
				}
				if c.sbLastSkipped == nil {
					c.sbLastSkipped = make(map[string]time.Time)
				}
				c.sbLastSkipped[key] = now
				targetEnd = seg.End
				seq = c.sbSeq
				shouldSkip = true
				break
			}
		}
	}
	c.sbMu.Unlock()

	if !shouldSkip {
		return
	}

	c.spawnSponsorSeek(seq, targetEnd)
}

func (c *IPCClient) spawnSponsorSeek(seq uint64, seconds float64) {
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && c.isCurrentPlay(seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}

	go func() {
		defer c.wg.Done()
		if !c.SponsorBlockEnabled() || !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
			return
		}
		seekCtx, cancel := context.WithTimeout(c.ctx, c.cmdTimeout)
		defer cancel()
		_ = c.SeekAbsolute(seekCtx, seconds)
	}()
}
