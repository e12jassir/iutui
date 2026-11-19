package catalog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
)

// BuildFormat returns a yt-dlp format selector string for the requested
// maximum vertical resolution. Non-positive values select the best available
// video and audio streams.
func BuildFormat(quality int) string {
	if quality <= 0 {
		return "bestvideo+bestaudio/best"
	}
	return fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", quality, quality)
}

// ErrKind categorizes common yt-dlp failure modes from stderr output.
type ErrKind int

const (
	ErrUnknown ErrKind = iota
	ErrAuth
	ErrForbidden
	ErrNetwork
	ErrRateLimit
)

// String returns a human-readable label for the error kind.
func (k ErrKind) String() string {
	switch k {
	case ErrAuth:
		return "auth"
	case ErrForbidden:
		return "forbidden"
	case ErrNetwork:
		return "network"
	case ErrRateLimit:
		return "rate_limit"
	default:
		return "unknown"
	}
}

// ClassifyErr inspects yt-dlp stderr output and maps it to an ErrKind.
func ClassifyErr(stderr string) ErrKind {
	s := strings.ToLower(stderr)
	if s == "" {
		return ErrUnknown
	}

	switch {
	case containsAny(s, "429", "too many requests", "rate limit", "ratelimit"):
		return ErrRateLimit
	case containsAny(s, "403", "forbidden"):
		return ErrForbidden
	case containsAny(s, "401", "sign in", "login", "cookies", "private video", "members-only", "confirm you're not a bot", "confirm you\u2019re not a bot", "authentication"):
		return ErrAuth
	case containsAny(s, "timed out", "timeout", "connection refused", "connection reset", "no such host", "network is unreachable", "temporary failure in name resolution", "unable to download webpage", "urlopen error", "dial tcp", "ssl"):
		return ErrNetwork
	default:
		return ErrUnknown
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// YTDLPError wraps a failed yt-dlp invocation with its classified ErrKind.
type YTDLPError struct {
	Kind   ErrKind
	Stderr string
	Err    error
}

func (e *YTDLPError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("yt-dlp (%s): %v: %s", e.Kind, e.Err, e.Stderr)
	}
	return fmt.Sprintf("yt-dlp (%s): %v", e.Kind, e.Err)
}

func (e *YTDLPError) Unwrap() error {
	return e.Err
}

type searchCacheEntry struct {
	videos    []model.Video
	fetchedAt time.Time
}

// YTDLPCatalog implements Catalog by invoking yt-dlp with exec.CommandContext
// and maintaining a stale-while-revalidate file cache in CacheDir.
type YTDLPCatalog struct {
	BinPath       string
	CacheDir      string
	CacheTTL      time.Duration
	Quality       int
	FeedSize      int
	SearchSize    int
	CookieJarPath string
	Meta          *MetadataEnricher

	searchMu       sync.RWMutex
	searchCache    map[string]searchCacheEntry
	feedMu         sync.Mutex
	feedGeneration map[model.Tab]uint64
	cfgMu          sync.RWMutex
	cacheDisabled  bool

	wg sync.WaitGroup
}

// SetFeedSize updates the playlist item limit used for HTTP feed fetches.
func (c *YTDLPCatalog) SetFeedSize(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.cfgMu.Lock()
	c.FeedSize = n
	c.cfgMu.Unlock()
}

func (c *YTDLPCatalog) feedSize() int {
	if c == nil {
		return 30
	}
	c.cfgMu.RLock()
	n := c.FeedSize
	c.cfgMu.RUnlock()
	if n <= 0 {
		return 30
	}
	return n
}

// SetSearchSize updates the result limit for ytsearch queries and clears cached search results.
func (c *YTDLPCatalog) SetSearchSize(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.cfgMu.Lock()
	c.SearchSize = n
	c.cfgMu.Unlock()

	c.searchMu.Lock()
	c.searchCache = nil
	c.searchMu.Unlock()
}

func (c *YTDLPCatalog) searchSize() int {
	if c == nil {
		return 20
	}
	c.cfgMu.RLock()
	n := c.SearchSize
	c.cfgMu.RUnlock()
	if n <= 0 {
		return 20
	}
	return n
}

// SetCacheTTL updates feed and search cache lifetime. Passing d <= 0 disables caching live.
func (c *YTDLPCatalog) SetCacheTTL(d time.Duration) {
	if c == nil {
		return
	}
	c.cfgMu.Lock()
	if d <= 0 {
		c.CacheTTL = 0
		c.cacheDisabled = true
	} else {
		c.CacheTTL = d
		c.cacheDisabled = false
	}
	c.cfgMu.Unlock()

	if d <= 0 {
		c.searchMu.Lock()
		c.searchCache = nil
		c.searchMu.Unlock()
	}
}

func (c *YTDLPCatalog) cacheTTLConfig() (time.Duration, bool) {
	if c == nil {
		return 0, true
	}
	c.cfgMu.RLock()
	ttl := c.CacheTTL
	disabled := c.cacheDisabled
	c.cfgMu.RUnlock()
	return ttl, disabled
}

// SetCookieJar updates the cookie jar path used for authenticated feed and channel requests.
func (c *YTDLPCatalog) SetCookieJar(path string) {
	if c == nil {
		return
	}
	c.cfgMu.Lock()
	c.CookieJarPath = strings.TrimSpace(path)
	c.cfgMu.Unlock()
}

func (c *YTDLPCatalog) cookieJarPath() string {
	if c == nil {
		return ""
	}
	c.cfgMu.RLock()
	p := c.CookieJarPath
	c.cfgMu.RUnlock()
	return p
}

// SetQuality updates the preferred max video height for yt-dlp format selection.
func (c *YTDLPCatalog) SetQuality(q int) {
	if c == nil || q < 0 {
		return
	}
	c.cfgMu.Lock()
	c.Quality = q
	c.cfgMu.Unlock()
}

func (c *YTDLPCatalog) quality() int {
	if c == nil {
		return 0
	}
	c.cfgMu.RLock()
	q := c.Quality
	c.cfgMu.RUnlock()
	return q
}

// NewYTDLPCatalog constructs a YTDLPCatalog with the given cache directory,
// cache TTL, and yt-dlp binary path. Empty values use sensible defaults.
func NewYTDLPCatalog(cacheDir string, cacheTTL time.Duration, binPath string) *YTDLPCatalog {
	if binPath == "" {
		binPath = "yt-dlp"
	}
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	return &YTDLPCatalog{
		BinPath:  binPath,
		CacheDir: cacheDir,
		CacheTTL: cacheTTL,
		Meta:     NewMetadataEnricher(cacheDir),
	}
}

// Wait blocks until any background stale-while-revalidate refresh completes.
func (c *YTDLPCatalog) Wait() {
	c.wg.Wait()
}

// Feed returns videos for tab, serving from the file cache when available and
// triggering background revalidation when cached entries are older than CacheTTL.
func (c *YTDLPCatalog) Feed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.feedMu.Lock()
	generation := c.feedGeneration[tab]
	c.feedMu.Unlock()

	cacheFile, target, ok := ytdlpTabTarget(tab)
	if !ok {
		return nil, nil
	}

	cachePath := c.cacheFilePath(cacheFile)
	ttl, cacheDisabled := c.cacheTTLConfig()
	if cachePath != "" && !cacheDisabled {
		if videos, modTime, err := readCacheFile(cachePath); err == nil && len(videos) > 0 {
			c.applyCachedMeta(videos)
			if ttl > 0 && time.Since(modTime) <= ttl {
				return videos, nil
			}
			// Stale-while-revalidate: serve cached data immediately and refresh asynchronously.
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if fresh, err := c.fetchTabVideos(bgCtx, tab, target); err == nil && len(fresh) > 0 {
					c.feedMu.Lock()
					if c.feedGeneration[tab] == generation {
						_ = writeCacheFile(cachePath, fresh)
					}
					c.feedMu.Unlock()
				}
			}()
			return videos, nil
		}
	}

	videos, err := c.fetchTabVideos(ctx, tab, target)
	if err != nil {
		return nil, err
	}
	c.applyCachedMeta(videos)
	if cachePath != "" && len(videos) > 0 {
		c.feedMu.Lock()
		if c.feedGeneration[tab] == generation {
			_ = writeCacheFile(cachePath, videos)
		}
		c.feedMu.Unlock()
	}
	return videos, nil
}

// RefreshFeed bypasses cached JSON files, fetches fresh results for tab via
// yt-dlp, writes the updated cache file when non-empty, and returns the videos.
func (c *YTDLPCatalog) RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cacheFile, target, ok := ytdlpTabTarget(tab)
	if !ok {
		return nil, nil
	}
	c.feedMu.Lock()
	if c.feedGeneration == nil {
		c.feedGeneration = make(map[model.Tab]uint64)
	}
	c.feedGeneration[tab]++
	generation := c.feedGeneration[tab]
	c.feedMu.Unlock()

	videos, err := c.fetchTabVideos(ctx, tab, target)
	if err != nil {
		return nil, err
	}
	c.applyCachedMeta(videos)

	cachePath := c.cacheFilePath(cacheFile)
	if cachePath != "" && len(videos) > 0 {
		c.feedMu.Lock()
		if c.feedGeneration[tab] == generation {
			_ = writeCacheFile(cachePath, videos)
		}
		c.feedMu.Unlock()
	}
	return videos, nil
}

// Search queries YouTube via yt-dlp's ytsearch extractor with an in-memory TTL cache.
func (c *YTDLPCatalog) Search(ctx context.Context, query string) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	ttl, cacheDisabled := c.cacheTTLConfig()
	if !cacheDisabled {
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		c.searchMu.RLock()
		if entry, ok := c.searchCache[query]; ok && time.Since(entry.fetchedAt) <= ttl {
			videos := make([]model.Video, len(entry.videos))
			copy(videos, entry.videos)
			c.searchMu.RUnlock()
			c.applyCachedMeta(videos)
			return videos, nil
		}
		c.searchMu.RUnlock()
	}

	searchLimit := c.searchSize()
	videos, err := c.fetchTarget(ctx, fmt.Sprintf("ytsearch%d:%s", searchLimit, query))
	if err != nil {
		return nil, err
	}
	c.applyCachedMeta(videos)

	if !cacheDisabled && len(videos) > 0 {
		now := time.Now()
		c.searchMu.Lock()
		if c.searchCache == nil {
			c.searchCache = make(map[string]searchCacheEntry)
		}
		for k, entry := range c.searchCache {
			if now.Sub(entry.fetchedAt) > ttl {
				delete(c.searchCache, k)
			}
		}
		if len(c.searchCache) >= 64 {
			var oldestKey string
			var oldestTime time.Time
			for k, entry := range c.searchCache {
				if oldestKey == "" || entry.fetchedAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = entry.fetchedAt
				}
			}
			if oldestKey != "" {
				delete(c.searchCache, oldestKey)
			}
		}
		copied := make([]model.Video, len(videos))
		copy(copied, videos)
		c.searchCache[query] = searchCacheEntry{videos: copied, fetchedAt: now}
		c.searchMu.Unlock()
	}

	return videos, nil
}

// RefreshSearch invalidates any cached results for query and executes a fresh lookup.
func (c *YTDLPCatalog) RefreshSearch(ctx context.Context, query string) ([]model.Video, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	c.searchMu.Lock()
	delete(c.searchCache, q)
	c.searchMu.Unlock()
	return c.Search(ctx, q)
}

func (c *YTDLPCatalog) fetchTarget(ctx context.Context, target string) ([]model.Video, error) {
	args := []string{
		"--flat-playlist",
		"--dump-json",
		"--no-warnings",
		"--ignore-errors",
		"--socket-timeout", "8",
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		args = append(args, "--playlist-end", strconv.Itoa(c.feedSize()))
	}
	if jar := c.cookieJarPath(); jar != "" && !strings.HasPrefix(target, "ytsearch") {
		if info, err := os.Stat(jar); err == nil && !info.IsDir() && info.Size() > 0 {
			args = append(args, "--cookies", jar)
		}
	}
	if qual := c.quality(); qual > 0 {
		args = append(args, "-f", BuildFormat(qual))
	}
	args = append(args, target)

	out, err := c.runYTDLP(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseYTDLPOutput(out)
}

func (c *YTDLPCatalog) runYTDLP(ctx context.Context, args ...string) ([]byte, error) {
	bin := c.BinPath
	if bin == "" {
		bin = "yt-dlp"
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	platform.SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return platform.KillProcessGroup(cmd.Process.Pid)
	}
	cmd.WaitDelay = 100 * time.Millisecond

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		errMsg := strings.TrimSpace(stderr.String())
		return nil, &YTDLPError{
			Kind:   ClassifyErr(errMsg),
			Stderr: errMsg,
			Err:    err,
		}
	}

	return stdout.Bytes(), nil
}

func (c *YTDLPCatalog) cacheFilePath(name string) string {
	dir := c.CacheDir
	if dir == "" {
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache == "" {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return ""
			}
			xdgCache = filepath.Join(home, ".cache")
		}
		dir = filepath.Join(xdgCache, "yt_terminal", "ytdlp")
	}
	return filepath.Join(dir, name)
}

const (
	youtubeMusicDiscoveryURL   = "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ"
	youtubeMusicSearchFallback = "ytsearch30:music official video"
)

// fetchTabVideos fetches target for tab and falls back to public discovery
// when logged-out TabHome or TabMusic returns zero videos without error.
func (c *YTDLPCatalog) fetchTabVideos(ctx context.Context, tab model.Tab, target string) ([]model.Video, error) {
	videos, err := c.fetchTarget(ctx, target)
	if err == nil && len(videos) == 0 && ctx.Err() == nil {
		switch tab {
		case model.TabHome:
			return c.fetchTarget(ctx, youtubeMusicDiscoveryURL)
		case model.TabMusic:
			return c.fetchTarget(ctx, youtubeMusicSearchFallback)
		}
	}
	return videos, err
}

func ytdlpTabTarget(tab model.Tab) (cacheFile, target string, ok bool) {
	switch tab {
	case model.TabHome:
		return "feed_home.json", "https://www.youtube.com", true
	case model.TabMusic:
		return "feed_music.json", youtubeMusicDiscoveryURL, true
	case model.TabSubscriptions:
		return "feed_subs.json", "https://www.youtube.com/feed/subscriptions", true
	default:
		return "", "", false
	}
}

func readCacheFile(path string) ([]model.Video, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var videos []model.Video
	if err := json.Unmarshal(data, &videos); err != nil {
		return nil, time.Time{}, err
	}
	return videos, info.ModTime(), nil
}

func writeCacheFile(path string, videos []model.Video) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(videos)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ytdlp-cache-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

type ytdlpJSONEntry struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Channel          string  `json:"channel"`
	ChannelID        string  `json:"channel_id"`
	Uploader         string  `json:"uploader"`
	DurationString   string  `json:"duration_string"`
	Duration         float64 `json:"duration"`
	ViewCount        int64   `json:"view_count"`
	UploadDate       string  `json:"upload_date"`
	Timestamp        int64   `json:"timestamp"`
	ReleaseTimestamp int64   `json:"release_timestamp"`
	LiveStatus       string  `json:"live_status"`
	IsLive           bool    `json:"is_live"`
	Thumbnail        string  `json:"thumbnail"`
}

type ytdlpPlaylist struct {
	Entries []ytdlpJSONEntry `json:"entries"`
}

func parseYTDLPOutput(data []byte) ([]model.Video, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}

	if trimmed[0] == '{' {
		var pl ytdlpPlaylist
		if err := json.Unmarshal(trimmed, &pl); err == nil && len(pl.Entries) > 0 {
			videos := make([]model.Video, 0, len(pl.Entries))
			for _, e := range pl.Entries {
				if v, ok := entryToVideo(e); ok {
					videos = append(videos, v)
				}
			}
			return videos, nil
		}

		var videos []model.Video
		scanner := bufio.NewScanner(bytes.NewReader(trimmed))
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var e ytdlpJSONEntry
			if err := json.Unmarshal(line, &e); err == nil {
				if v, ok := entryToVideo(e); ok {
					videos = append(videos, v)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if len(videos) > 0 {
			return videos, nil
		}
	}

	videos, err := ParseTSV(bytes.NewReader(trimmed))
	if err != nil {
		return nil, err
	}
	return videos, nil
}

func entryToVideo(e ytdlpJSONEntry) (model.Video, bool) {
	id := strings.TrimSpace(e.ID)
	if isInvalidID(id) {
		return model.Video{}, false
	}

	channel := strings.TrimSpace(e.Channel)
	if channel == "" {
		channel = strings.TrimSpace(e.Uploader)
	}
	if channel == "" {
		channel = "YouTube"
	}

	dur := strings.TrimSpace(e.DurationString)
	if dur == "" && e.Duration > 0 {
		dur = formatDurationSeconds(int(e.Duration))
	}

	var views string
	if e.ViewCount > 0 {
		views = strconv.FormatInt(e.ViewCount, 10)
	}

	published := formatEntryDate(e.UploadDate, e.Timestamp, e.ReleaseTimestamp)

	return normalizeVideo(model.Video{
		ID:        id,
		Title:     strings.TrimSpace(e.Title),
		Channel:   channel,
		ChannelID: strings.TrimSpace(e.ChannelID),
		Duration:  dur,
		Views:     views,
		Published: published,
		IsLive:    e.IsLive || e.LiveStatus == "is_live",
		Thumbnail: strings.TrimSpace(e.Thumbnail),
	}), true
}

func formatEntryDate(uploadDate string, ts, releaseTS int64) string {
	d := strings.TrimSpace(uploadDate)
	if len(d) == 8 {
		if _, err := time.Parse("20060102", d); err == nil {
			return d[0:4] + "-" + d[4:6] + "-" + d[6:8]
		}
	}
	unixTS := ts
	if unixTS <= 0 {
		unixTS = releaseTS
	}
	if unixTS > 0 {
		return time.Unix(unixTS, 0).UTC().Format("2006-01-02")
	}
	return ""
}

func formatDurationSeconds(total int) string {
	if total <= 0 {
		return ""
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// applyCachedMeta populates missing Views/Published fields from c.Meta without network I/O.
func (c *YTDLPCatalog) applyCachedMeta(videos []model.Video) {
	if c == nil || c.Meta == nil {
		return
	}
	c.Meta.ApplyCached(videos)
}

// EnrichMetadata fetches missing view counts and publish dates for video IDs.
func (c *YTDLPCatalog) EnrichMetadata(ctx context.Context, ids []string) (map[string]VideoMeta, error) {
	if c == nil || c.Meta == nil {
		return nil, nil
	}
	return c.Meta.Enrich(ctx, ids)
}

// IsErrKind reports whether err is or wraps a YTDLPError of kind k.
func IsErrKind(err error, k ErrKind) bool {
	var ye *YTDLPError
	if errors.As(err, &ye) {
		return ye.Kind == k
	}
	return false
}
