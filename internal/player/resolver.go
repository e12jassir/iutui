package player

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/ytx/internal/model"
)

const (
	streamCacheTTL     = 15 * time.Minute
	streamResolveLimit = 12 * time.Second
)

// ResolvedStream holds pre-resolved direct CDN stream URLs from yt-dlp.
type ResolvedStream struct {
	VideoURL  string
	AudioURL  string
	ExpiresAt time.Time
}

// StreamResolver asynchronously pre-resolves direct stream URLs for selected
// videos so playback starts near-instantly without sacrificing HD quality.
type StreamResolver struct {
	ytdlpBin   string
	cookieJar  string
	quality    int
	generation uint64

	mu         sync.RWMutex
	cache      map[string]ResolvedStream
	inFlight   map[string]bool
	inFlightCh map[string]chan struct{}
	sem        chan struct{}
}

// NewStreamResolver constructs a StreamResolver bounded to 3 concurrent yt-dlp lookups.
func NewStreamResolver(ytdlpBin string, quality int) *StreamResolver {
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	return &StreamResolver{
		ytdlpBin:   ytdlpBin,
		quality:    quality,
		cache:      make(map[string]ResolvedStream),
		inFlight:   make(map[string]bool),
		inFlightCh: make(map[string]chan struct{}),
		sem:        make(chan struct{}, 3),
	}
}

// SetCookieJar sets the Netscape cookie jar path passed to yt-dlp when non-empty and valid.
func (r *StreamResolver) SetCookieJar(path string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cookieJar = path
	r.mu.Unlock()
}

// SetYTDLPBin updates the yt-dlp binary path when non-empty.
func (r *StreamResolver) SetYTDLPBin(bin string) {
	if r == nil || bin == "" {
		return
	}
	r.mu.Lock()
	r.ytdlpBin = bin
	r.mu.Unlock()
}

// SetQuality updates the target max height (0 = best available) and clears stale cache.
func (r *StreamResolver) SetQuality(quality int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.quality = quality
	r.generation++
	r.cache = make(map[string]ResolvedStream)
	r.inFlight = make(map[string]bool)
	r.inFlightCh = make(map[string]chan struct{})
	r.mu.Unlock()
}

// GetCached returns a valid cached stream resolution in 0ms if available.
func (r *StreamResolver) GetCached(videoID string) (ResolvedStream, bool) {
	if r == nil || videoID == "" {
		return ResolvedStream{}, false
	}
	r.mu.RLock()
	st, ok := r.cache[videoID]
	r.mu.RUnlock()
	if !ok || time.Now().After(st.ExpiresAt) {
		return ResolvedStream{}, false
	}
	return st, true
}

// WaitOrGet returns a cached stream for videoID, or waits up to maxWait for an
// active in-flight Prefetch of the same video (or Mix seed) to complete.
func (r *StreamResolver) WaitOrGet(ctx context.Context, videoID string, maxWait time.Duration) (ResolvedStream, bool) {
	if r == nil || videoID == "" {
		return ResolvedStream{}, false
	}
	if seed := model.MixSeedVideoID(videoID); seed != "" {
		videoID = seed
	}
	if st, ok := r.GetCached(videoID); ok {
		return st, true
	}
	if maxWait <= 0 {
		return ResolvedStream{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.RLock()
	ch, ok := r.inFlightCh[videoID]
	r.mu.RUnlock()
	if !ok {
		return r.GetCached(videoID)
	}

	timer := time.NewTimer(maxWait)
	defer timer.Stop()

	select {
	case <-ch:
		return r.GetCached(videoID)
	case <-timer.C:
		return ResolvedStream{}, false
	case <-ctx.Done():
		return ResolvedStream{}, false
	}
}

// Prefetch resolves stream URLs for v in the background if not already cached or in flight.
func (r *StreamResolver) Prefetch(ctx context.Context, v model.Video) {
	if r == nil || v.ID == "" || v.IsLive {
		return
	}
	if seed := model.MixSeedVideoID(v.ID); seed != "" {
		v.ID = seed
	} else if model.IsPlaylistID(v.ID) {
		return
	}
	if _, ok := r.GetCached(v.ID); ok {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	if r.inFlight[v.ID] {
		r.mu.Unlock()
		return
	}
	done := make(chan struct{})
	r.inFlight[v.ID] = true
	r.inFlightCh[v.ID] = done
	generation := r.generation
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			if r.generation == generation {
				delete(r.inFlight, v.ID)
				delete(r.inFlightCh, v.ID)
			}
			r.mu.Unlock()
			close(done)
		}()

		select {
		case r.sem <- struct{}{}:
			defer func() { <-r.sem }()
		case <-ctx.Done():
			return
		}

		lookupCtx, cancel := context.WithTimeout(ctx, streamResolveLimit)
		defer cancel()

		st, ok := r.resolve(lookupCtx, v.ID)
		if !ok {
			return
		}
		r.mu.Lock()
		if generation == r.generation {
			r.cache[v.ID] = st
		}
		r.mu.Unlock()
	}()
}

func (r *StreamResolver) resolve(ctx context.Context, videoID string) (ResolvedStream, bool) {
	r.mu.RLock()
	q := r.quality
	bin := r.ytdlpBin
	cookieJar := r.cookieJar
	r.mu.RUnlock()

	fmtStr := "bestvideo+bestaudio/best"
	if q > 0 {
		fmtStr = fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", q, q)
	}
	target := "https://www.youtube.com/watch?v=" + videoID

	args := []string{
		"--no-warnings",
		"--no-playlist",
		"--no-check-certificates",
		"--socket-timeout", "8",
	}
	if isValidCookieJar(cookieJar) {
		args = append(args, "--cookies", cookieJar)
	} else {
		args = append(args, "--extractor-args", "youtube:player_client=android,web")
	}
	args = append(args, "-f", fmtStr, "-g", target)

	cmd := exec.CommandContext(ctx, bin, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ResolvedStream{}, false
	}

	var urls []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		return ResolvedStream{}, false
	}

	st := ResolvedStream{
		VideoURL:  urls[0],
		ExpiresAt: time.Now().Add(streamCacheTTL),
	}
	if len(urls) > 1 {
		st.AudioURL = urls[1]
	}
	return st, true
}

func isValidCookieJar(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}
