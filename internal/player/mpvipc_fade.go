package player

import (
	"context"
	"time"
)

const (
	fadeFloorDB = -42.0
	fadeSteps   = 9
)

func (c *IPCClient) fadeInDuration() time.Duration {
	if c.fadeDuration <= 0 {
		return 0
	}
	return c.fadeDuration + c.fadeDuration/4
}

func (c *IPCClient) isAudioActivelyPlaying() bool {
	st := c.Status()
	return st.IsRunning && !st.Paused && (st.TimePos > 0 || st.Duration > 0)
}

func (c *IPCClient) beginFade() (uint64, context.Context) {
	c.fadeMu.Lock()
	if c.fadeCancel != nil {
		c.fadeCancel()
	}
	c.fadingIn = false
	seq := c.fadeSeq.Add(1)
	fctx, fcancel := context.WithCancel(c.ctx)
	c.fadeCancel = fcancel
	c.fadeMu.Unlock()
	return seq, fctx
}

func (c *IPCClient) cancelFade() {
	c.fadeMu.Lock()
	if c.fadeCancel != nil {
		c.fadeCancel()
		c.fadeCancel = nil
	}
	c.fadingIn = false
	c.fadeSeq.Add(1)
	c.fadeMu.Unlock()
}

func (c *IPCClient) resetFadeState() {
	c.pendingFadeIn.Store(false)
	c.cancelFade()
	c.fadeMu.Lock()
	c.fadeGain = 0
	c.endFadeTrack = ""
	c.fadeMu.Unlock()
}

func (c *IPCClient) mergeFadeCtx(fctx, callerCtx context.Context) (context.Context, context.CancelFunc) {
	if callerCtx == nil || callerCtx == context.Background() || callerCtx == context.TODO() {
		return fctx, func() {}
	}
	mergedCtx, cancelMerged := context.WithCancel(fctx)
	stopWatch := context.AfterFunc(callerCtx, cancelMerged)
	return mergedCtx, func() {
		stopWatch()
		cancelMerged()
	}
}

func (c *IPCClient) setGainBestEffort(ctx context.Context, db float64) bool {
	if !c.isConnected() {
		return false
	}
	if ctx == nil {
		ctx = c.ctx
	}
	gctx, cancel := context.WithTimeout(ctx, 120*time.Millisecond)
	defer cancel()
	if _, err := c.sendCommand(gctx, "set_property", "volume-gain", db); err == nil {
		c.fadeMu.Lock()
		c.fadeGain = db
		c.fadeMu.Unlock()
		return true
	}
	return false
}

func (c *IPCClient) fadeRampSync(ctx context.Context, seq uint64, fromDB, toDB float64, dur time.Duration) {
	if dur <= 0 || !c.isConnected() {
		return
	}
	if ctx == nil {
		ctx = c.ctx
	}
	stepDur := dur / fadeSteps
	if stepDur < 2*time.Millisecond {
		stepDur = 2 * time.Millisecond
	}
	for i := 1; i <= fadeSteps; i++ {
		if c.fadeSeq.Load() != seq || c.ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(stepDur)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if c.fadeSeq.Load() != seq || c.ctx.Err() != nil {
			return
		}
		db := fromDB + (toDB-fromDB)*(float64(i)/float64(fadeSteps))
		c.setGainBestEffort(ctx, db)
	}
}

func (c *IPCClient) fadeOutSync(ctx context.Context, seq uint64) {
	c.fadeMu.Lock()
	startDB := c.fadeGain
	c.fadeMu.Unlock()
	if startDB <= fadeFloorDB {
		return
	}
	c.fadeRampSync(ctx, seq, startDB, fadeFloorDB, c.fadeDuration)
}

func (c *IPCClient) spawnFadeIn(fctx context.Context, seq uint64, dur time.Duration) {
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
		c.fadeMu.Lock()
		if c.fadeSeq.Load() != seq {
			c.fadeMu.Unlock()
			return
		}
		c.fadingIn = true
		startDB := c.fadeGain
		if startDB > -1.0 {
			startDB = fadeFloorDB
		}
		c.fadeMu.Unlock()

		c.fadeRampSync(fctx, seq, startDB, 0.0, dur)

		if c.fadeSeq.Load() == seq && c.ctx.Err() == nil && c.isConnected() {
			c.fadeMu.Lock()
			gain := c.fadeGain
			c.fadeMu.Unlock()
			if gain < -0.1 {
				c.setGainBestEffort(fctx, 0.0)
			}
		}

		c.fadeMu.Lock()
		if c.fadeSeq.Load() == seq {
			c.fadingIn = false
		}
		c.fadeMu.Unlock()
	}()
}

func (c *IPCClient) spawnEndOfTrackFade(fctx context.Context, seq uint64, startDB float64, dur time.Duration) {
	if dur <= 0 || startDB <= -36.0 {
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
		c.fadeRampSync(fctx, seq, startDB, -36.0, dur)
	}()
}

func (c *IPCClient) scheduleFadeInSafety(seq uint64, wait time.Duration) {
	if wait <= 0 {
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
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
		}
		if c.fadeSeq.Load() == seq && c.pendingFadeIn.Swap(false) {
			fseq, fctx := c.beginFade()
			c.spawnFadeIn(fctx, fseq, c.fadeInDuration())
		}
	}()
}
