package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL   = "https://lrclib.net"
	defaultUserAgent = "iutui/0.4"
	defaultTimeout   = 4 * time.Second
	maxResponseBytes = 4 << 20

	SourceAuto       = ""
	SourceLRCLib     = "lrclib"
	SourceYouTube    = "youtube"
	SourceYouTubeCC  = "youtube-cc"
	SourceYouTubeASR = "youtube-asr"
)

// ErrNotFound is returned when LRCLib has no synced or plain lyrics for a track.
var ErrNotFound = errors.New("lyrics: not found")

var defaultHTTPClient = &http.Client{Timeout: defaultTimeout}

// Line represents a single timestamped lyric line.
type Line struct {
	Time float64
	Text string
}

// TrackLyrics contains parsed synced and/or plain lyrics for a track.
type TrackLyrics struct {
	Synced       []Line
	Plain        []string
	TrackName    string
	ArtistName   string
	Source       string
	Duration     float64
	HasAlternate bool
	AltSource    string
	Language     string
}

// FetchOptions configures source preference, target duration matching, and caption language.
type FetchOptions struct {
	Duration     float64
	PreferSource string
	Lang         string
}

type cacheEntry struct {
	lyrics TrackLyrics
	err    error
}

// Client fetches and caches synced and plain lyrics from LRCLib and YouTube captions.
type Client struct {
	BaseURL            string
	HTTP               *http.Client
	UserAgent          string
	YouTubeEndpointURL string
	CacheDir           string

	mu      sync.RWMutex
	cache   map[string]cacheEntry
	ytCache map[string]cacheEntry

	offsetsMu     sync.Mutex
	offsetsLoaded bool
	offsets       map[string]float64
}

// NewClient constructs a Client configured for lrclib.net with a 4s timeout and
// initialized in-memory caches. YouTube captions remain disabled until EnableYouTubeCaptions is called.
func NewClient() *Client {
	return &Client{
		BaseURL:   defaultBaseURL,
		HTTP:      &http.Client{Timeout: defaultTimeout},
		UserAgent: defaultUserAgent,
		cache:     make(map[string]cacheEntry),
		ytCache:   make(map[string]cacheEntry),
		offsets:   make(map[string]float64),
	}
}

// EnableYouTubeCaptions enables pure-Go YouTube InnerTube caption fetching and
// configures cacheDir for persisting per-video sync offsets.
func (c *Client) EnableYouTubeCaptions(cacheDir string) {
	if c == nil {
		return
	}
	c.YouTubeEndpointURL = defaultYouTubePlayerURL
	c.CacheDir = strings.TrimSpace(cacheDir)
}

type lrclibSearchItem struct {
	TrackName    string  `json:"trackName"`
	Name         string  `json:"name"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
}

// Fetch retrieves lyrics for a track from cache or LRCLib's /api/search endpoint.
// Synced lyrics are preferred over plain lyrics. Empty/404 misses are cached to
// avoid redundant network calls.
func (c *Client) Fetch(ctx context.Context, videoID, title, channel string) (TrackLyrics, error) {
	return c.fetchLRCLib(ctx, videoID, title, channel, 0)
}

// FetchWithOptions retrieves lyrics concurrently from LRCLib and YouTube captions
// (when enabled and videoID is valid), ranking candidates by duration and source preference.
func (c *Client) FetchWithOptions(ctx context.Context, videoID, title, channel string, opts FetchOptions) (TrackLyrics, error) {
	if c == nil {
		return TrackLyrics{}, errors.New("lyrics: nil client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TrackLyrics{}, err
	}

	ytEndpoint := strings.TrimSpace(c.YouTubeEndpointURL)
	if ytEndpoint == "" || !isValidYouTubeVideoID(videoID) {
		return c.fetchLRCLib(ctx, videoID, title, channel, opts.Duration)
	}

	switch opts.PreferSource {
	case SourceLRCLib:
		lrc, err := c.fetchLRCLib(ctx, videoID, title, channel, opts.Duration)
		if err != nil {
			return TrackLyrics{}, err
		}
		if c.hasValidYTCache(videoID, opts.Lang) {
			lrc.HasAlternate = true
			lrc.AltSource = SourceYouTube
		}
		return cloneTrackLyrics(lrc), nil

	case SourceYouTube:
		yt, err := c.fetchYouTubeCached(ctx, ytEndpoint, videoID, opts.Lang)
		if err != nil {
			return TrackLyrics{}, err
		}
		if c.hasValidLRCCache(videoID, title, channel, opts.Duration) {
			yt.HasAlternate = true
			yt.AltSource = SourceLRCLib
		}
		return cloneTrackLyrics(yt), nil
	}

	var (
		wg     sync.WaitGroup
		lrc    TrackLyrics
		lrcErr error
		yt     TrackLyrics
		ytErr  error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		lrc, lrcErr = c.fetchLRCLib(ctx, videoID, title, channel, opts.Duration)
	}()
	go func() {
		defer wg.Done()
		yt, ytErr = c.fetchYouTubeCached(ctx, ytEndpoint, videoID, opts.Lang)
	}()
	wg.Wait()

	hasLRC := lrcErr == nil && (len(lrc.Synced) > 0 || len(lrc.Plain) > 0)
	hasLRCSynced := lrcErr == nil && len(lrc.Synced) > 0
	hasYT := ytErr == nil && len(yt.Synced) > 0
	ytManual := hasYT && yt.Source == SourceYouTubeCC

	lrcDurDiff := -1.0
	if opts.Duration > 0 && lrc.Duration > 0 {
		lrcDurDiff = math.Abs(lrc.Duration - opts.Duration)
	}

	var winner TrackLyrics
	switch {
	case hasYT && opts.Duration > 600:
		winner = yt
		if hasLRC {
			winner.HasAlternate = true
			winner.AltSource = SourceLRCLib
		}
	case hasLRCSynced && (lrcDurDiff < 0 || lrcDurDiff <= 6.0):
		winner = lrc
		if hasYT {
			winner.HasAlternate = true
			winner.AltSource = SourceYouTube
		}
	case ytManual:
		winner = yt
		if hasLRC {
			winner.HasAlternate = true
			winner.AltSource = SourceLRCLib
		}
	case hasLRCSynced && lrcDurDiff <= 25.0:
		winner = lrc
		if hasYT {
			winner.HasAlternate = true
			winner.AltSource = SourceYouTube
		}
	case hasYT:
		winner = yt
		if hasLRC {
			winner.HasAlternate = true
			winner.AltSource = SourceLRCLib
		}
	case hasLRC:
		winner = lrc
		if hasYT {
			winner.HasAlternate = true
			winner.AltSource = SourceYouTube
		}
	default:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, ctxErr
		}
		if lrcErr != nil && !errors.Is(lrcErr, ErrNotFound) {
			return TrackLyrics{}, lrcErr
		}
		return TrackLyrics{}, ErrNotFound
	}

	return cloneTrackLyrics(winner), nil
}

func (c *Client) fetchLRCLib(ctx context.Context, videoID, title, channel string, targetDuration float64) (TrackLyrics, error) {
	if c == nil {
		return TrackLyrics{}, errors.New("lyrics: nil client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TrackLyrics{}, err
	}

	query := CleanQuery(title, channel)
	baseKey, ok := buildCacheKey(videoID, query)
	if !ok {
		return TrackLyrics{}, ErrNotFound
	}
	cacheKey := buildDurationCacheKey(baseKey, targetDuration)

	if entry, hit := c.getCached(cacheKey); hit {
		return cloneTrackLyrics(entry.lyrics), entry.err
	}

	if query == "" {
		return TrackLyrics{}, ErrNotFound
	}

	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	ua := strings.TrimSpace(c.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = defaultHTTPClient
	}

	lyrics, found, statusNotFound, err := c.searchOnce(ctx, baseURL, ua, httpClient, query, targetDuration)
	if err != nil {
		return TrackLyrics{}, err
	}
	fb := FallbackQuery(title, channel)
	if (!found || len(lyrics.Synced) == 0) && fb != "" && !strings.EqualFold(fb, query) {
		fbLyrics, fbFound, _, fbErr := c.searchOnce(ctx, baseURL, ua, httpClient, fb, targetDuration)
		if fbErr != nil && !found {
			return TrackLyrics{}, fbErr
		}
		if fbFound && (len(fbLyrics.Synced) > 0 || !found) {
			lyrics = fbLyrics
			found = true
		}
	}
	if !found {
		if core := CoreQuery(title, channel); core != "" && !strings.EqualFold(core, query) && !strings.EqualFold(core, fb) {
			coreLyrics, coreFound, _, coreErr := c.searchOnce(ctx, baseURL, ua, httpClient, core, targetDuration)
			if coreErr != nil {
				return TrackLyrics{}, coreErr
			}
			if coreFound {
				lyrics = coreLyrics
				found = true
			}
		}
	}
	if !found {
		_ = statusNotFound
		c.setCached(cacheKey, cacheEntry{err: ErrNotFound})
		return TrackLyrics{}, ErrNotFound
	}

	c.setCached(cacheKey, cacheEntry{lyrics: cloneTrackLyrics(lyrics)})
	return cloneTrackLyrics(lyrics), nil
}

func (c *Client) fetchYouTubeCached(ctx context.Context, endpoint, videoID, prefLang string) (TrackLyrics, error) {
	normLang := normalizeCaptionLang(prefLang)
	vid := strings.TrimSpace(videoID)
	ytKey := "yt:" + normLang + ":" + vid
	if entry, hit := c.getYTCached(ytKey); hit {
		return cloneTrackLyrics(entry.lyrics), entry.err
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = defaultHTTPClient
	}

	lyrics, err := FetchYouTubeCaptions(ctx, httpClient, endpoint, vid, normLang)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.setYTCached(ytKey, cacheEntry{err: ErrNotFound})
		}
		return TrackLyrics{}, err
	}

	c.setYTCached(ytKey, cacheEntry{lyrics: cloneTrackLyrics(lyrics)})
	return cloneTrackLyrics(lyrics), nil
}

func (c *Client) searchOnce(ctx context.Context, baseURL, ua string, httpClient *http.Client, query string, targetDuration float64) (TrackLyrics, bool, bool, error) {
	searchURL := baseURL + "/api/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return TrackLyrics{}, false, false, fmt.Errorf("lyrics: build request: %w", err)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, false, false, ctxErr
		}
		return TrackLyrics{}, false, false, fmt.Errorf("lyrics: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return TrackLyrics{}, false, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return TrackLyrics{}, false, false, fmt.Errorf("lyrics: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, false, false, ctxErr
		}
		return TrackLyrics{}, false, false, fmt.Errorf("lyrics: read response: %w", err)
	}

	var items []lrclibSearchItem
	if err := json.Unmarshal(body, &items); err != nil {
		return TrackLyrics{}, false, false, fmt.Errorf("lyrics: decode response: %w", err)
	}

	lyrics, found := selectBestLyricsForDuration(items, targetDuration)
	return lyrics, found, false, nil
}

func (c *Client) getCached(key string) (cacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cache == nil {
		return cacheEntry{}, false
	}
	entry, ok := c.cache[key]
	return entry, ok
}

func (c *Client) setCached(key string, entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]cacheEntry)
	}
	c.cache[key] = entry
}

func (c *Client) getYTCached(key string) (cacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ytCache == nil {
		return cacheEntry{}, false
	}
	entry, ok := c.ytCache[key]
	return entry, ok
}

func (c *Client) setYTCached(key string, entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ytCache == nil {
		c.ytCache = make(map[string]cacheEntry)
	}
	c.ytCache[key] = entry
}

func (c *Client) hasValidYTCache(videoID, prefLang string) bool {
	vid := strings.TrimSpace(videoID)
	if vid == "" {
		return false
	}
	normLang := normalizeCaptionLang(prefLang)
	if entry, ok := c.getYTCached("yt:" + normLang + ":" + vid); ok && entry.err == nil && len(entry.lyrics.Synced) > 0 {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	suffix := ":" + vid
	for k, entry := range c.ytCache {
		if strings.HasPrefix(k, "yt:") && strings.HasSuffix(k, suffix) && entry.err == nil && len(entry.lyrics.Synced) > 0 {
			return true
		}
	}
	return false
}

func (c *Client) hasValidLRCCache(videoID, title, channel string, duration float64) bool {
	baseKey, ok := buildCacheKey(videoID, CleanQuery(title, channel))
	if !ok {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cache == nil {
		return false
	}
	isValid := func(e cacheEntry) bool {
		return e.err == nil && (len(e.lyrics.Synced) > 0 || len(e.lyrics.Plain) > 0)
	}
	if duration > 0 {
		if entry, hit := c.cache[buildDurationCacheKey(baseKey, duration)]; hit && isValid(entry) {
			return true
		}
	}
	if entry, hit := c.cache[baseKey]; hit && isValid(entry) {
		return true
	}
	prefix := baseKey + "@dur:"
	for k, entry := range c.cache {
		if strings.HasPrefix(k, prefix) && isValid(entry) {
			return true
		}
	}
	return false
}

func buildCacheKey(videoID, cleanedQuery string) (string, bool) {
	if vid := strings.TrimSpace(videoID); vid != "" {
		return "id:" + vid, true
	}
	if q := strings.ToLower(strings.TrimSpace(cleanedQuery)); q != "" {
		return "q:" + q, true
	}
	return "", false
}

func buildDurationCacheKey(baseKey string, targetDuration float64) string {
	if targetDuration <= 0 {
		return baseKey
	}
	return fmt.Sprintf("%s@dur:%d", baseKey, int(math.Round(targetDuration)))
}

func cloneTrackLyrics(in TrackLyrics) TrackLyrics {
	out := in
	if len(in.Synced) > 0 {
		out.Synced = make([]Line, len(in.Synced))
		copy(out.Synced, in.Synced)
	}
	if len(in.Plain) > 0 {
		out.Plain = make([]string, len(in.Plain))
		copy(out.Plain, in.Plain)
	}
	return out
}

func selectBestLyricsForDuration(items []lrclibSearchItem, targetDuration float64) (TrackLyrics, bool) {
	var bestSynced TrackLyrics
	var bestSyncedDur float64
	hasSynced := false

	for _, item := range items {
		synced := ParseLRC(item.SyncedLyrics)
		if len(synced) == 0 {
			continue
		}
		if !hasSynced || isBetterDurationCandidate(item.Duration, bestSyncedDur, targetDuration) {
			bestSynced = TrackLyrics{
				Synced:     synced,
				Plain:      splitNonEmptyLines(item.PlainLyrics),
				TrackName:  firstNonEmpty(item.TrackName, item.Name),
				ArtistName: strings.TrimSpace(item.ArtistName),
				Source:     SourceLRCLib,
				Duration:   item.Duration,
			}
			bestSyncedDur = item.Duration
			hasSynced = true
			if targetDuration <= 0 {
				return bestSynced, true
			}
		}
	}
	if hasSynced {
		return bestSynced, true
	}

	var bestPlain TrackLyrics
	var bestPlainDur float64
	hasPlain := false

	for _, item := range items {
		plain := splitNonEmptyLines(item.PlainLyrics)
		if len(plain) == 0 {
			continue
		}
		if !hasPlain || isBetterDurationCandidate(item.Duration, bestPlainDur, targetDuration) {
			bestPlain = TrackLyrics{
				Plain:      plain,
				TrackName:  firstNonEmpty(item.TrackName, item.Name),
				ArtistName: strings.TrimSpace(item.ArtistName),
				Source:     SourceLRCLib,
				Duration:   item.Duration,
			}
			bestPlainDur = item.Duration
			hasPlain = true
			if targetDuration <= 0 {
				return bestPlain, true
			}
		}
	}
	if hasPlain {
		return bestPlain, true
	}

	return TrackLyrics{}, false
}

func isBetterDurationCandidate(candDur, bestDur, targetDuration float64) bool {
	if targetDuration <= 0 {
		return false
	}
	if candDur > 0 && bestDur <= 0 {
		return true
	}
	if candDur <= 0 && bestDur > 0 {
		return false
	}
	if candDur <= 0 && bestDur <= 0 {
		return false
	}
	return math.Abs(candDur-targetDuration) < math.Abs(bestDur-targetDuration)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func splitNonEmptyLines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	rawLines := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

var (
	parenNoiseRe      = regexp.MustCompile(`\([^)]*\)`)
	bracketNoiseRe    = regexp.MustCompile(`\[[^\]]*\]`)
	cjkBracketNoiseRe = regexp.MustCompile(`【[^】]*】`)
	noiseKeywordRe    = regexp.MustCompile(`(?i)\b(?:official|oficial|lyrics?|letra|audio|video|videoclip|visualizer|en\s+vivo|concierto|vivo|directo|ac[uú]stico|unplugged|session|sesi[oó]n|versi[oó]n|version|edit(?:ed)?|restored|completo|karaoke|hd|hq|4k|8k|1080p|720p|uhd|mv|m/v|remaster(?:ed)?|explicit|clean|topic|subtitul\w*|sub\.?\s*espa[ñn]ol|featuring)\b|(?i)^(?:live(?:\s+(?:at|in|from|session|version|performance))?)$|(?i)\b(?:ft|feat)(?:\.|\b)`)
	trailingPipeRe    = regexp.MustCompile(`(?i)(?:\s+[\|\-–—]\s*)(?:official(?:\s+(?:music\s+)?(?:video|audio|lyric\s+video|visualizer))?|video\s+oficial|audio\s+oficial|lyrics?|hd|4k|en\s+vivo)\s*$`)
	trailingPipeAnyRe = regexp.MustCompile(`\s*\|.*$`)
	channelSuffixRe   = regexp.MustCompile(`(?i)(?:\s+-\s*topic|\s+(?:oficial|official))$`)
	vevoSuffixRe      = regexp.MustCompile(`(?i)vevo$`)
	featClauseRe      = regexp.MustCompile(`(?i)\b(?:ft\.?|feat\.?|featuring|prod\.?\s+by)\s+[^-–—\(\[\|]+`)
)

// CoreQuery aggressively strips parentheticals, feat./prod. credits, extra hyphenated
// subtitles, and VEVO suffixes for a third-chance LRCLib search when prior queries miss.
func CoreQuery(rawTitle, channel string) string {
	s := strings.TrimSpace(rawTitle)
	if s != "" {
		s = parenNoiseRe.ReplaceAllString(s, " ")
		s = bracketNoiseRe.ReplaceAllString(s, " ")
		s = cjkBracketNoiseRe.ReplaceAllString(s, " ")
		s = trailingPipeAnyRe.ReplaceAllString(s, "")
		s = featClauseRe.ReplaceAllString(s, " ")
		s = stripStandaloneFeatTokens(s)
		s = trimDanglingSeparators(s)
		s = collapseSpaces(s)
		if parts := strings.Split(s, " - "); len(parts) >= 3 {
			s = strings.TrimSpace(parts[0]) + " - " + strings.TrimSpace(parts[1])
		}
	}
	ch := cleanChannelName(channel)
	if ch != "" {
		ch = vevoSuffixRe.ReplaceAllString(ch, "")
		ch = collapseSpaces(ch)
	}
	if s == "" {
		return ch
	}
	if ch == "" || strings.Contains(strings.ToLower(s), strings.ToLower(ch)) || hasArtistSeparator(s) {
		return s
	}
	return ch + " " + s
}

// FallbackQuery strips all parenthetical/bracketed tags and trailing pipe metadata
// for a secondary LRCLib search when the primary cleaned query yields no matches.
func FallbackQuery(rawTitle, channel string) string {
	s := strings.TrimSpace(rawTitle)
	if s != "" {
		s = parenNoiseRe.ReplaceAllString(s, " ")
		s = bracketNoiseRe.ReplaceAllString(s, " ")
		s = cjkBracketNoiseRe.ReplaceAllString(s, " ")
		s = trailingPipeAnyRe.ReplaceAllString(s, "")
		s = stripStandaloneFeatTokens(s)
		s = trimDanglingSeparators(s)
		s = collapseSpaces(s)
	}
	ch := cleanChannelName(channel)
	if s == "" {
		return ch
	}
	if ch == "" || strings.Contains(strings.ToLower(s), strings.ToLower(ch)) || hasArtistSeparator(s) {
		return s
	}
	return ch + " " + s
}

// CleanQuery strips common YouTube title noise (such as "(Official Video)",
// "[Official Audio]", "(Lyrics)", "[HD]", "[4K]", "(En Vivo)", "(Audio Oficial)",
// "ft.", "feat.", and redundant whitespace) and combines the cleaned channel when
// it is not already represented in the title.
func CleanQuery(rawTitle, channel string) string {
	title := cleanTitleText(rawTitle)
	ch := cleanChannelName(channel)

	if title == "" {
		return ch
	}
	if ch == "" {
		return title
	}

	lowerTitle := strings.ToLower(title)
	lowerCh := strings.ToLower(ch)
	if strings.Contains(lowerTitle, lowerCh) || hasArtistSeparator(title) {
		return title
	}
	return ch + " " + title
}

func cleanTitleText(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	stripIfNoise := func(match string) string {
		runes := []rune(match)
		if len(runes) < 2 {
			return match
		}
		inner := strings.TrimSpace(string(runes[1 : len(runes)-1]))
		if inner == "" || noiseKeywordRe.MatchString(inner) {
			return " "
		}
		return match
	}

	s = parenNoiseRe.ReplaceAllStringFunc(s, stripIfNoise)
	s = bracketNoiseRe.ReplaceAllStringFunc(s, stripIfNoise)
	s = cjkBracketNoiseRe.ReplaceAllStringFunc(s, stripIfNoise)
	s = stripStandaloneFeatTokens(s)
	s = trailingPipeRe.ReplaceAllString(s, "")

	s = trimDanglingSeparators(s)
	return collapseSpaces(s)
}

func stripStandaloneFeatTokens(s string) string {
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		lower := strings.ToLower(f)
		if lower == "ft." || lower == "ft" || lower == "feat." || lower == "feat" || lower == "featuring" {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

func cleanChannelName(raw string) string {
	s := collapseSpaces(raw)
	if s == "" {
		return ""
	}
	if strings.EqualFold(s, "YouTube") {
		return ""
	}
	s = channelSuffixRe.ReplaceAllString(s, "")
	s = collapseSpaces(s)
	if strings.EqualFold(s, "YouTube") {
		return ""
	}
	return s
}

func hasArtistSeparator(s string) bool {
	return strings.Contains(s, " - ") || strings.Contains(s, " – ") || strings.Contains(s, " — ")
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func trimDanglingSeparators(s string) string {
	for {
		trimmed := strings.TrimSpace(s)
		next := strings.TrimRight(trimmed, "-–—|/")
		next = strings.TrimLeft(next, "-–—|/")
		next = strings.TrimSpace(next)
		if next == trimmed {
			return next
		}
		s = next
	}
}

// ParseLRC parses LRC lyric text supporting [mm:ss.xx], [mm:ss.xxx], and [m:ss]
// timestamps (including multi-timestamp lines), skips metadata tags like [ti:...],
// and returns lines sorted ascending by Time.
func ParseLRC(raw string) []Line {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	var lines []Line
	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || !strings.HasPrefix(line, "[") {
			continue
		}

		var timestamps []float64
		rest := line
		for strings.HasPrefix(rest, "[") {
			closeIdx := strings.IndexByte(rest, ']')
			if closeIdx <= 1 {
				break
			}
			tag := rest[1:closeIdx]
			ts, ok := parseLRCTimestamp(tag)
			if !ok {
				break
			}
			timestamps = append(timestamps, ts)
			rest = strings.TrimSpace(rest[closeIdx+1:])
		}

		if len(timestamps) == 0 {
			continue
		}

		text := strings.TrimSpace(rest)
		if text == "" {
			continue
		}

		for _, ts := range timestamps {
			lines = append(lines, Line{Time: ts, Text: text})
		}
	}

	if len(lines) == 0 {
		return nil
	}

	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].Time < lines[j].Time
	})
	return lines
}

func parseLRCTimestamp(tag string) (float64, bool) {
	colonIdx := strings.IndexByte(tag, ':')
	if colonIdx <= 0 || colonIdx >= len(tag)-1 {
		return 0, false
	}

	minStr := tag[:colonIdx]
	secStr := tag[colonIdx+1:]

	for i := 0; i < len(minStr); i++ {
		if minStr[i] < '0' || minStr[i] > '9' {
			return 0, false
		}
	}
	if !isValidSecondsToken(secStr) {
		return 0, false
	}

	mins, err := strconv.Atoi(minStr)
	if err != nil || mins < 0 {
		return 0, false
	}
	secs, err := strconv.ParseFloat(secStr, 64)
	if err != nil || math.IsNaN(secs) || secs < 0 || secs >= 60 {
		return 0, false
	}

	return float64(mins)*60 + secs, true
}

func isValidSecondsToken(s string) bool {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return false
	}
	dotSeen := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' && !dotSeen && i < len(s)-1 {
			dotSeen = true
			continue
		}
		return false
	}
	return true
}

// ActiveLineIndex returns the index of the active synced lyric line for posSeconds
// (the last line with Time <= posSeconds), or -1 if lines is empty or posSeconds
// precedes lines[0].Time.
func ActiveLineIndex(lines []Line, posSeconds float64) int {
	if len(lines) == 0 || math.IsNaN(posSeconds) || posSeconds < lines[0].Time {
		return -1
	}

	idx := sort.Search(len(lines), func(i int) bool {
		return lines[i].Time > posSeconds
	})
	return idx - 1
}
