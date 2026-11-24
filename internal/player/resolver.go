package player

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const (
	streamCacheTTL            = 15 * time.Minute
	streamResolveLimit        = 12 * time.Second
	defaultInnerTubePlayerURL = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
	androidVRUserAgent        = "com.google.android.apps.youtube.vr.oculus/1.60.19 (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip"
	iosPlayerUserAgent        = "com.google.ios.youtube/20.10.4 (iPhone16,2; U; CPU iOS 18_3_2 like Mac OS X;)"
)

// ResolvedStream holds pre-resolved direct CDN stream URLs from yt-dlp or InnerTube.
type ResolvedStream struct {
	VideoURL  string
	AudioURL  string
	ExpiresAt time.Time
}

// StreamResolver asynchronously pre-resolves direct stream URLs for selected
// videos so playback starts near-instantly without sacrificing HD quality.
type StreamResolver struct {
	ytdlpBin        string
	cookieJar       string
	quality         int
	audioOnly       bool
	generation      uint64
	enableInnerTube bool
	innerTubeURL    string
	httpClient      *http.Client

	mu         sync.RWMutex
	cache      map[string]ResolvedStream
	audioCache map[string]ResolvedStream
	inFlight   map[string]bool
	inFlightCh map[string]chan struct{}
	sem        chan struct{}
}

// NewStreamResolver constructs a StreamResolver bounded to 4 concurrent background yt-dlp lookups.
// Unauthenticated InnerTube stream resolution is disabled by default because YouTube returns
// LOGIN_REQUIRED for ANDROID_VR and PO-Token-gated HTTP 403 CDN URLs for IOS; callers may
// explicitly opt in via SetInnerTubeEndpoint (e.g. in tests).
func NewStreamResolver(ytdlpBin string, quality int) *StreamResolver {
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	return &StreamResolver{
		ytdlpBin:        ytdlpBin,
		quality:         quality,
		enableInnerTube: false,
		innerTubeURL:    defaultInnerTubePlayerURL,
		httpClient:      &http.Client{Timeout: 5 * time.Second},
		cache:           make(map[string]ResolvedStream),
		audioCache:      make(map[string]ResolvedStream),
		inFlight:        make(map[string]bool),
		inFlightCh:      make(map[string]chan struct{}),
		sem:             make(chan struct{}, 4),
	}
}

// SetInnerTubeEndpoint overrides the InnerTube player endpoint (for tests) and enables InnerTube fast path.
func (r *StreamResolver) SetInnerTubeEndpoint(url string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.innerTubeURL = strings.TrimSpace(url)
	r.enableInnerTube = r.innerTubeURL != ""
	r.mu.Unlock()
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

// SetAudioOnly configures whether background Prefetch resolves audio-only streams.
func (r *StreamResolver) SetAudioOnly(audioOnly bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.audioOnly != audioOnly {
		r.audioOnly = audioOnly
		r.generation++
		r.inFlight = make(map[string]bool)
		r.inFlightCh = make(map[string]chan struct{})
	}
	r.mu.Unlock()
}

// GetCached returns a valid cached stream resolution in 0ms if available.
func (r *StreamResolver) GetCached(videoID string) (ResolvedStream, bool) {
	return r.GetCachedForMode(videoID, false)
}

// GetCachedForMode returns a valid cached stream resolution for audio-only or video mode.
func (r *StreamResolver) GetCachedForMode(videoID string, audioOnly bool) (ResolvedStream, bool) {
	if r == nil || videoID == "" {
		return ResolvedStream{}, false
	}
	r.mu.RLock()
	m := r.cache
	if audioOnly {
		m = r.audioCache
	}
	st, ok := m[videoID]
	r.mu.RUnlock()
	if !ok || time.Now().After(st.ExpiresAt) {
		return ResolvedStream{}, false
	}
	return st, true
}

// Invalidate removes videoID from both video and audio stream caches so a
// rejected or expired direct CDN URL is never reused.
func (r *StreamResolver) Invalidate(videoID string) {
	if r == nil {
		return
	}
	if seed := model.MixSeedVideoID(videoID); seed != "" {
		videoID = seed
	}
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return
	}
	r.mu.Lock()
	delete(r.cache, videoID)
	delete(r.audioCache, videoID)
	r.mu.Unlock()
}

// WaitOrGet returns a cached stream for videoID, or waits up to maxWait for an
// active in-flight Prefetch of the same video (or Mix seed) to complete.
func (r *StreamResolver) WaitOrGet(ctx context.Context, videoID string, maxWait time.Duration) (ResolvedStream, bool) {
	return r.WaitOrGetForMode(ctx, videoID, false, maxWait)
}

// WaitOrGetForMode returns a cached or resolved stream matching audioOnly mode.
// Interactive playback lookups resolve directly on a priority lane without
// waiting behind background Prefetch semaphore slots.
func (r *StreamResolver) WaitOrGetForMode(ctx context.Context, videoID string, audioOnly bool, maxWait time.Duration) (ResolvedStream, bool) {
	if r == nil || videoID == "" {
		return ResolvedStream{}, false
	}
	if seed := model.MixSeedVideoID(videoID); seed != "" {
		videoID = seed
	}
	if st, ok := r.GetCachedForMode(videoID, audioOnly); ok {
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
	inFlightMatchesMode := r.audioOnly == audioOnly
	generation := r.generation
	useInnerTube := r.enableInnerTube
	r.mu.RUnlock()

	if ok && inFlightMatchesMode {
		timer := time.NewTimer(maxWait)
		defer timer.Stop()

		select {
		case <-ch:
			return r.GetCachedForMode(videoID, audioOnly)
		case <-timer.C:
			return ResolvedStream{}, false
		case <-ctx.Done():
			return ResolvedStream{}, false
		}
	}

	if !audioOnly && !useInnerTube {
		return r.GetCached(videoID)
	}

	// Priority lane: interactive Play resolves immediately without blocking on Prefetch sem.
	lookupCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	st, resolved := r.resolveForMode(lookupCtx, videoID, audioOnly)
	if !resolved {
		return ResolvedStream{}, false
	}
	r.mu.Lock()
	if generation == r.generation {
		if audioOnly {
			if r.audioCache == nil {
				r.audioCache = make(map[string]ResolvedStream)
			}
			r.audioCache[videoID] = st
		} else {
			if r.cache == nil {
				r.cache = make(map[string]ResolvedStream)
			}
			r.cache[videoID] = st
		}
	}
	r.mu.Unlock()
	return st, true
}

// Prefetch resolves stream URLs for v in the background if not already cached or in flight.
func (r *StreamResolver) Prefetch(ctx context.Context, v model.Video) {
	if r == nil || v.ID == "" {
		return
	}
	if seed := model.MixSeedVideoID(v.ID); seed != "" {
		v.ID = seed
		v.IsLive = false
	} else if v.IsLive || model.IsPlaylistID(v.ID) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	wantAudio := r.audioOnly
	targetMap := r.cache
	if wantAudio {
		targetMap = r.audioCache
	}
	if cached, ok := targetMap[v.ID]; ok && !time.Now().After(cached.ExpiresAt) {
		r.mu.Unlock()
		return
	}
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

		st, ok := r.resolveForMode(lookupCtx, v.ID, wantAudio)
		if !ok {
			return
		}
		r.mu.Lock()
		if generation == r.generation {
			if wantAudio {
				if r.audioCache == nil {
					r.audioCache = make(map[string]ResolvedStream)
				}
				r.audioCache[v.ID] = st
			} else {
				if r.cache == nil {
					r.cache = make(map[string]ResolvedStream)
				}
				r.cache[v.ID] = st
			}
		}
		r.mu.Unlock()
	}()
}

func (r *StreamResolver) resolve(ctx context.Context, videoID string) (ResolvedStream, bool) {
	return r.resolveForMode(ctx, videoID, false)
}

func (r *StreamResolver) resolveForMode(ctx context.Context, videoID string, audioOnly bool) (ResolvedStream, bool) {
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.RLock()
	q := r.quality
	bin := r.ytdlpBin
	cookieJar := r.cookieJar
	useInnerTube := r.enableInnerTube
	endpoint := r.innerTubeURL
	hc := r.httpClient
	r.mu.RUnlock()

	fmtStr := "bestvideo+bestaudio/best"
	if audioOnly {
		fmtStr = "bestaudio/best"
	} else if q > 0 {
		fmtStr = fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", q, q)
	}

	normalizeResult := func(st ResolvedStream, ok bool) (ResolvedStream, bool) {
		if !ok {
			return ResolvedStream{}, false
		}
		if audioOnly {
			// Ensure an audio-only lookup never exposes VideoURL so callers never treat it as video.
			audioURL := st.AudioURL
			if audioURL == "" {
				audioURL = st.VideoURL
			}
			if audioURL == "" {
				return ResolvedStream{}, false
			}
			return ResolvedStream{
				AudioURL:  audioURL,
				ExpiresAt: st.ExpiresAt,
			}, true
		}
		return st, true
	}

	// Attempt 0: Pure Go InnerTube ANDROID_VR -> IOS (~580ms vs ~9.4s visionos subprocess).
	if useInnerTube && endpoint != "" {
		if st, ok := normalizeResult(resolveViaInnerTube(ctx, hc, endpoint, videoID, audioOnly, q)); ok {
			return st, true
		}
	}

	// Attempt 1: Fast cookieless android yt-dlp fallback.
	if st, ok := normalizeResult(runResolve(ctx, bin, videoID, fmtStr, "")); ok {
		return st, true
	}

	// Attempt 2: Retry with cookies only for restricted/private videos when valid.
	if ctx.Err() == nil && isValidCookieJar(cookieJar) {
		return normalizeResult(runResolve(ctx, bin, videoID, fmtStr, cookieJar))
	}
	return ResolvedStream{}, false
}

type innerTubePlayerResp struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
	} `json:"playabilityStatus"`
	StreamingData struct {
		Formats         []innerTubeFormat `json:"formats"`
		AdaptiveFormats []innerTubeFormat `json:"adaptiveFormats"`
	} `json:"streamingData"`
}

type innerTubeFormat struct {
	URL      string `json:"url"`
	MimeType string `json:"mimeType"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Bitrate  int    `json:"bitrate"`
}

type innerTubeClientSpec struct {
	name      string
	version   string
	userAgent string
	extra     map[string]any
}

var innerTubeStreamClients = []innerTubeClientSpec{
	{
		name:      "ANDROID_VR",
		version:   "1.60.19",
		userAgent: androidVRUserAgent,
		extra: map[string]any{
			"deviceMake":        "Oculus",
			"deviceModel":       "Quest 3",
			"osName":            "Android",
			"osVersion":         "12L",
			"androidSdkVersion": 32,
		},
	},
	{
		name:      "IOS",
		version:   "20.10.4",
		userAgent: iosPlayerUserAgent,
		extra: map[string]any{
			"deviceMake":  "Apple",
			"deviceModel": "iPhone16,2",
			"osName":      "iPhone",
			"osVersion":   "18.3.2.22D82",
		},
	},
}

func resolveViaInnerTube(ctx context.Context, hc *http.Client, endpoint, videoID string, audioOnly bool, maxHeight int) (ResolvedStream, bool) {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	id := ExtractYouTubeID(videoID)
	if id == "" {
		id = strings.TrimSpace(videoID)
	}
	if id == "" {
		return ResolvedStream{}, false
	}

	for _, cl := range innerTubeStreamClients {
		if ctx.Err() != nil {
			return ResolvedStream{}, false
		}
		clientObj := map[string]any{
			"clientName":    cl.name,
			"clientVersion": cl.version,
			"hl":            "en",
			"gl":            "US",
		}
		for k, v := range cl.extra {
			clientObj[k] = v
		}
		payload := map[string]any{
			"context":        map[string]any{"client": clientObj},
			"videoId":        id,
			"contentCheckOk": true,
			"racyCheckOk":    true,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", cl.userAgent)

		resp, err := hc.Do(req)
		if err != nil {
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}

		var parsed innerTubePlayerResp
		if err := json.Unmarshal(raw, &parsed); err != nil {
			continue
		}
		if parsed.PlayabilityStatus.Status != "" && parsed.PlayabilityStatus.Status != "OK" {
			continue
		}
		if st, ok := pickInnerTubeStreams(parsed, audioOnly, maxHeight); ok {
			return st, true
		}
	}
	return ResolvedStream{}, false
}

func pickInnerTubeStreams(resp innerTubePlayerResp, audioOnly bool, maxHeight int) (ResolvedStream, bool) {
	var bestAudio innerTubeFormat
	for _, f := range resp.StreamingData.AdaptiveFormats {
		if f.URL == "" || !strings.HasPrefix(f.MimeType, "audio/") {
			continue
		}
		if f.Bitrate > bestAudio.Bitrate {
			bestAudio = f
		}
	}

	if audioOnly {
		if bestAudio.URL != "" {
			return ResolvedStream{
				AudioURL:  bestAudio.URL,
				ExpiresAt: time.Now().Add(streamCacheTTL),
			}, true
		}
	} else {
		var bestVideo innerTubeFormat
		for _, f := range resp.StreamingData.AdaptiveFormats {
			if f.URL == "" || !strings.HasPrefix(f.MimeType, "video/") {
				continue
			}
			if maxHeight > 0 && f.Height > maxHeight {
				continue
			}
			if f.Height > bestVideo.Height || (f.Height == bestVideo.Height && f.Bitrate > bestVideo.Bitrate) {
				bestVideo = f
			}
		}
		if bestVideo.URL != "" {
			return ResolvedStream{
				VideoURL:  bestVideo.URL,
				AudioURL:  bestAudio.URL,
				ExpiresAt: time.Now().Add(streamCacheTTL),
			}, true
		}
	}

	// Fallback to muxed formats if adaptive streams didn't match.
	var bestMuxed innerTubeFormat
	for _, f := range resp.StreamingData.Formats {
		if f.URL == "" {
			continue
		}
		if !audioOnly && maxHeight > 0 && f.Height > maxHeight {
			continue
		}
		if f.Height > bestMuxed.Height || (f.Height == bestMuxed.Height && f.Bitrate > bestMuxed.Bitrate) {
			bestMuxed = f
		}
	}
	if bestMuxed.URL != "" {
		if audioOnly {
			return ResolvedStream{AudioURL: bestMuxed.URL, ExpiresAt: time.Now().Add(streamCacheTTL)}, true
		}
		return ResolvedStream{VideoURL: bestMuxed.URL, ExpiresAt: time.Now().Add(streamCacheTTL)}, true
	}
	return ResolvedStream{}, false
}

func runResolve(ctx context.Context, bin, videoID, fmtStr, cookieJar string) (ResolvedStream, bool) {
	target := "https://www.youtube.com/watch?v=" + videoID

	args := []string{
		"--no-warnings",
		"--no-playlist",
		"--no-check-certificates",
		"--socket-timeout", "8",
	}
	if cookieJar != "" {
		args = append(args, "--cookies", cookieJar)
	} else {
		args = append(args, "--extractor-args", "youtube:player_client=android")
	}
	args = append(args, "-S", "proto:https", "-f", fmtStr, "-g", target)

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
