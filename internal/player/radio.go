package player

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
)

const (
	defaultYTMusicBaseURL = "https://music.youtube.com"
	defaultYouTubeBaseURL = "https://www.youtube.com"

	musicVideoTypeATV = "MUSIC_VIDEO_TYPE_ATV"
	musicVideoTypeOMV = "MUSIC_VIDEO_TYPE_OMV"
	musicVideoTypeUGC = "MUSIC_VIDEO_TYPE_UGC"
)

// RadioTrack represents a candidate track returned by YouTube Music or YouTube radio.
type RadioTrack struct {
	ID             string
	Title          string
	Artist         string
	VideoType      string
	DurationSec    int
	ConsensusCount int
}

// DisplayTitle formats the track as "Artist - Title" when Artist is available
// and not already included in Title.
func (t RadioTrack) DisplayTitle() string {
	title := strings.TrimSpace(t.Title)
	artist := strings.TrimSpace(t.Artist)
	if title == "" {
		return artist
	}
	if artist == "" {
		return title
	}
	lowerTitle := strings.ToLower(title)
	lowerArtist := strings.ToLower(artist)
	if strings.Contains(lowerTitle, lowerArtist) || strings.Contains(title, " - ") {
		return title
	}
	return artist + " - " + title
}

// TasteProfile summarizes the listener's liked, historical, and in-session
// engagement signals for radio re-ranking.
type TasteProfile struct {
	LikedArtists   map[string]float64
	LikedTracks    map[string]bool
	HistoryArtists map[string]float64
	HistoryTracks  map[string]bool
	SkippedArtists map[string]float64
	SkippedTracks  map[string]bool
	CompletedSeeds []RadioTrack
}

// BuildTasteProfile aggregates artist and track signals from the user's liked
// and recently played lists.
func BuildTasteProfile(liked, history []model.Video) TasteProfile {
	tp := TasteProfile{
		LikedArtists:   make(map[string]float64),
		LikedTracks:    make(map[string]bool),
		HistoryArtists: make(map[string]float64),
		HistoryTracks:  make(map[string]bool),
		SkippedArtists: make(map[string]float64),
		SkippedTracks:  make(map[string]bool),
	}
	for _, v := range liked {
		if k := canonicalSongKey(v.Title, v.Channel); k != "" {
			tp.LikedTracks[k] = true
		}
		if v.ID != "" {
			tp.LikedTracks[v.ID] = true
		}
		if ak := primaryArtistKey(v.Channel, v.Title); ak != "" {
			tp.LikedArtists[ak] += 1.0
		}
	}
	for i, v := range history {
		if v.ID != "" {
			tp.HistoryTracks[v.ID] = true
		}
		if k := canonicalSongKey(v.Title, v.Channel); k != "" {
			tp.HistoryTracks[k] = true
		}
		if ak := primaryArtistKey(v.Channel, v.Title); ak != "" {
			// Mild recency weight so repeated plays signal artist affinity without dominating likes.
			weight := 0.6
			if i < 25 {
				weight = 1.0
			}
			tp.HistoryArtists[ak] += weight
		}
	}
	return tp
}

var ytIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ExtractYouTubeID extracts an 11-character YouTube video ID from a URL or raw ID.
func ExtractYouTubeID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if ytIDPattern.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("v"); ytIDPattern.MatchString(v) {
		return v
	}
	host := strings.ToLower(u.Host)
	path := strings.Trim(u.Path, "/")
	if strings.Contains(host, "youtu.be") && ytIDPattern.MatchString(path) {
		return path
	}
	parts := strings.Split(path, "/")
	for i := 0; i+1 < len(parts); i++ {
		if (parts[i] == "shorts" || parts[i] == "embed" || parts[i] == "live" || parts[i] == "v") &&
			ytIDPattern.MatchString(parts[i+1]) {
			return parts[i+1]
		}
	}
	return ""
}

var (
	parenNoiseRe  = regexp.MustCompile(`(?i)\(([^)]*(official|video|audio|lyric|letra|hd|4k|hq|visualizer|remaster|live|en vivo|explicit|clean|version|vers[ií]on|feat\.?|ft\.?)[^)]*)\)`)
	brackNoiseRe  = regexp.MustCompile(`(?i)\[([^\]]*(official|video|audio|lyric|letra|hd|4k|hq|visualizer|remaster|live|en vivo|explicit|clean|version|vers[ií]on|feat\.?|ft\.?)[^\]]*)\]`)
	featSuffixRe  = regexp.MustCompile(`(?i)\b(feat\.?|ft\.?|con|with)\b.*$`)
	multiSpaceRe  = regexp.MustCompile(`\s+`)
	artistSplitRe = regexp.MustCompile(`(?i)\s*(?:,|&|\bx\b|\bfeat\.?\b|\bft\.?\b|\band\b|\by\b)\s*`)
)

func stripAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		case 'ñ':
			r = 'n'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizeAlphaNum(s string) string {
	s = stripAccents(strings.ToLower(strings.TrimSpace(s)))
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func canonicalSongKey(title, artist string) string {
	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		return ""
	}
	// Strip "Artist - " prefix when artist is known or when title has clear dash separation.
	if artist != "" {
		lowerT := strings.ToLower(cleanTitle)
		lowerA := strings.ToLower(strings.TrimSpace(artist))
		if strings.HasPrefix(lowerT, lowerA+" - ") {
			cleanTitle = cleanTitle[len(artist)+3:]
		}
	} else if idx := strings.Index(cleanTitle, " - "); idx > 0 {
		artist = cleanTitle[:idx]
		cleanTitle = cleanTitle[idx+3:]
	}
	cleanTitle = parenNoiseRe.ReplaceAllString(cleanTitle, " ")
	cleanTitle = brackNoiseRe.ReplaceAllString(cleanTitle, " ")
	cleanTitle = featSuffixRe.ReplaceAllString(cleanTitle, "")
	cleanTitle = multiSpaceRe.ReplaceAllString(cleanTitle, " ")

	normTitle := normalizeAlphaNum(cleanTitle)
	normArtist := primaryArtistKey(artist, title)
	if normTitle == "" {
		return ""
	}
	if normArtist == "" {
		return normTitle
	}
	return normArtist + "::" + normTitle
}

func primaryArtistKey(artist, title string) string {
	a := strings.TrimSpace(artist)
	if a == "" {
		if idx := strings.Index(title, " - "); idx > 0 {
			a = strings.TrimSpace(title[:idx])
		}
	}
	if a == "" {
		return ""
	}
	a = strings.TrimSuffix(a, " - Topic")
	a = strings.TrimSuffix(a, "VEVO")
	parts := artistSplitRe.Split(a, 2)
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		a = parts[0]
	}
	return normalizeAlphaNum(a)
}

func parseDurationSeconds(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	parts := strings.Split(text, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func isJunkTitle(title string) bool {
	lower := strings.ToLower(title)
	junkTokens := []string{
		"1 hour", "2 hours", "3 hours", "10 hours", "full album",
		"álbum completo", "album completo", "compilation", "recopilación",
		"sped up", "slowed + reverb", "slowed & reverb", "slowed and reverb",
		"nightcore", "8d audio", "bass boosted", "reaction", "tutorial",
		"karaoke", "mega mix", "megamix",
	}
	for _, tok := range junkTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

// CurateRadioQueue filters, scores, deduplicates, and diversifies candidate
// tracks to produce a cohesive radio queue after seedID.
func CurateRadioQueue(seedID string, candidates []RadioTrack, taste TasteProfile, recentIDs []string, limit int) []RadioTrack {
	if len(candidates) == 0 || limit <= 0 {
		return nil
	}

	// Legacy fallback: when candidates only contain IDs (no Title or Artist),
	// preserve input order while skipping seedID and deduplicating IDs.
	hasMetadata := false
	for _, c := range candidates {
		if strings.TrimSpace(c.Title) != "" || strings.TrimSpace(c.Artist) != "" {
			hasMetadata = true
			break
		}
	}
	if !hasMetadata {
		seen := make(map[string]bool, len(candidates)+1)
		if seedID != "" {
			seen[seedID] = true
		}
		out := make([]RadioTrack, 0, minInt(limit, len(candidates)))
		for _, c := range candidates {
			if c.ID == "" || seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			out = append(out, c)
			if len(out) >= limit {
				break
			}
		}
		return out
	}

	recentSet := make(map[string]bool, len(recentIDs)+1)
	if seedID != "" {
		recentSet[seedID] = true
	}
	for _, id := range recentIDs {
		if id != "" {
			recentSet[id] = true
		}
	}

	type scoredTrack struct {
		track     RadioTrack
		score     float64
		songKey   string
		artistKey string
	}

	bestBySong := make(map[string]scoredTrack, len(candidates))
	bestOrder := make([]string, 0, len(candidates))
	seenIDs := make(map[string]bool, len(candidates))

	for idx, cand := range candidates {
		if cand.ID == "" || seenIDs[cand.ID] {
			continue
		}
		seenIDs[cand.ID] = true

		if cand.ID == seedID || recentSet[cand.ID] {
			continue
		}
		isMusicTrack := cand.VideoType == musicVideoTypeATV ||
			cand.VideoType == musicVideoTypeOMV ||
			cand.VideoType == "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC" ||
			cand.VideoType == musicVideoTypeUGC
		if isMusicTrack && cand.DurationSec > 0 && (cand.DurationSec < 75 || cand.DurationSec > 720) {
			continue
		}
		if isJunkTitle(cand.Title) {
			continue
		}

		songKey := canonicalSongKey(cand.Title, cand.Artist)
		if songKey == "" {
			songKey = "id::" + cand.ID
		}
		if taste.SkippedTracks[cand.ID] || taste.SkippedTracks[songKey] {
			continue
		}
		artistKey := primaryArtistKey(cand.Artist, cand.Title)

		// Relevance decay across original curated radio rank.
		score := 100.0 - float64(idx)*1.1

		// Multi-seed session consensus boost when multiple completed tracks suggest this song.
		if cand.ConsensusCount > 1 {
			score += float64(cand.ConsensusCount-1) * 26.0
		}

		// Audio purity bias (official track/video strongly preferred over UGC uploads).
		switch cand.VideoType {
		case musicVideoTypeATV:
			score += 36
		case musicVideoTypeOMV:
			score += 24
		case "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC":
			score += 20
		case musicVideoTypeUGC:
			score -= 18
		}

		// Sweet-spot song duration.
		if cand.DurationSec >= 120 && cand.DurationSec <= 360 {
			score += 8
		}

		// Personal taste affinity & live skip penalties.
		if artistKey != "" {
			if w := taste.LikedArtists[artistKey]; w > 0 {
				score += minFloat(28, 16+w*3)
			}
			if w := taste.HistoryArtists[artistKey]; w > 0 {
				score += minFloat(16, 6+w*2)
			}
			if sk := taste.SkippedArtists[artistKey]; sk > 0 {
				score -= 42.0 * sk
			}
		}
		if taste.LikedTracks[songKey] || taste.LikedTracks[cand.ID] {
			score += 12
		}
		if taste.HistoryTracks[songKey] || taste.HistoryTracks[cand.ID] {
			// Slight anti-fatigue nudge when a track already appeared in older history.
			score -= 8
		}

		st := scoredTrack{
			track:     cand,
			score:     score,
			songKey:   songKey,
			artistKey: artistKey,
		}
		if prev, exists := bestBySong[songKey]; exists {
			if st.score > prev.score {
				bestBySong[songKey] = st
			}
		} else {
			bestBySong[songKey] = st
			bestOrder = append(bestOrder, songKey)
		}
	}

	pool := make([]scoredTrack, 0, len(bestOrder))
	for _, k := range bestOrder {
		pool = append(pool, bestBySong[k])
	}
	// Stable initial sort so ties preserve natural radio relevance order.
	sort.SliceStable(pool, func(i, j int) bool {
		return pool[i].score > pool[j].score
	})

	const artistCooldown = 3
	out := make([]RadioTrack, 0, minInt(limit, len(pool)))
	used := make([]bool, len(pool))
	recentArtists := make([]string, 0, limit)

	for len(out) < limit && len(out) < len(pool) {
		bestIdx := -1
		bestAdjusted := -1e9
		for i, cand := range pool {
			if used[i] {
				continue
			}
			adj := cand.score
			if cand.artistKey != "" {
				for back := 1; back <= artistCooldown && back <= len(recentArtists); back++ {
					if recentArtists[len(recentArtists)-back] == cand.artistKey {
						// Strong penalty within sliding window so consecutive same-artist tracks are spread out.
						adj -= float64(55 - (back-1)*12)
						break
					}
				}
			}
			if adj > bestAdjusted {
				bestAdjusted = adj
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		chosen := pool[bestIdx]
		out = append(out, chosen.track)
		recentArtists = append(recentArtists, chosen.artistKey)
	}

	return out
}

// RadioClientOption configures a RadioClient.
type RadioClientOption func(*RadioClient)

// WithRadioHTTPClient overrides the HTTP client used by RadioClient.
func WithRadioHTTPClient(hc *http.Client) RadioClientOption {
	return func(c *RadioClient) {
		if hc != nil {
			c.http = hc
		}
	}
}

// WithRadioBaseURL overrides the YouTube Music InnerTube base URL (useful for tests).
func WithRadioBaseURL(baseURL string) RadioClientOption {
	return func(c *RadioClient) {
		if strings.TrimSpace(baseURL) != "" {
			c.ytmBaseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithRadioYouTubeBaseURL overrides the standard YouTube fallback base URL (useful for tests).
func WithRadioYouTubeBaseURL(baseURL string) RadioClientOption {
	return func(c *RadioClient) {
		if strings.TrimSpace(baseURL) != "" {
			c.ytBaseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithRadioCookiesPath sets a Netscape cookies.txt path for SAPISIDHASH auth.
func WithRadioCookiesPath(path string) RadioClientOption {
	return func(c *RadioClient) {
		c.SetCookiesPath(path)
	}
}

// RadioClient queries YouTube Music's InnerTube /youtubei/v1/next (RDAMVM<id>)
// endpoint in pure Go, falling back to standard YouTube RD<id> when a seed
// video is non-music UGC or Music rejects the seed.
type RadioClient struct {
	http        *http.Client
	ytmBaseURL  string
	ytBaseURL   string
	mu          sync.RWMutex
	cookiesPath string
}

// NewRadioClient creates a configured RadioClient.
func NewRadioClient(opts ...RadioClientOption) *RadioClient {
	c := &RadioClient{
		http:       &http.Client{Timeout: 8 * time.Second},
		ytmBaseURL: defaultYTMusicBaseURL,
		ytBaseURL:  defaultYouTubeBaseURL,
	}
	if cacheDir := platform.ResolvePaths().CacheDir; cacheDir != "" {
		c.cookiesPath = filepath.Join(cacheDir, "cookies.txt")
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetCookiesPath updates the Netscape cookies.txt path for SAPISIDHASH auth.
func (c *RadioClient) SetCookiesPath(path string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cookiesPath = strings.TrimSpace(path)
	c.mu.Unlock()
}

func (c *RadioClient) getCookiesPath() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookiesPath
}

// FetchRadio fetches radio tracks seeded from seedID. It queries YouTube Music
// WEB_REMIX (RDAMVM<seedID>) first, applies a non-music UGC gate, and falls
// back to standard YouTube WEB (RD<seedID>) when appropriate.
func (c *RadioClient) FetchRadio(ctx context.Context, seedID string, limit int) ([]RadioTrack, error) {
	seedID = ExtractYouTubeID(seedID)
	if seedID == "" {
		return nil, fmt.Errorf("invalid seed video id")
	}
	if limit <= 0 {
		limit = 35
	}

	tracks, err := c.fetchYTMusicNext(ctx, seedID)
	rejectedYTM := err == nil && len(tracks) > 0 && isNonMusicUGCRadio(seedID, tracks)
	if err == nil && len(tracks) > 0 && !rejectedYTM {
		if len(tracks) > limit {
			tracks = tracks[:limit]
		}
		return tracks, nil
	}

	fallback, fbErr := c.fetchYouTubeNext(ctx, seedID)
	if fbErr == nil && len(fallback) > 0 {
		if len(fallback) > limit {
			fallback = fallback[:limit]
		}
		return fallback, nil
	}
	if rejectedYTM {
		return nil, fbErr
	}
	if err != nil {
		return nil, err
	}
	return nil, fbErr
}

func videoTypeRank(vt string) int {
	switch vt {
	case musicVideoTypeATV:
		return 3
	case musicVideoTypeOMV:
		return 2
	case "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC":
		return 2
	case "":
		return 1
	default:
		return 0
	}
}

// FetchMultiSeedRadio blends primarySeedID's radio with up to two recent
// completed tracks concurrently, awarding ConsensusCount to tracks corroborated
// by multiple session seeds.
func (c *RadioClient) FetchMultiSeedRadio(ctx context.Context, primarySeedID string, extraSeedIDs []string, limit int) ([]RadioTrack, error) {
	primary := ExtractYouTubeID(primarySeedID)
	if primary == "" {
		return nil, fmt.Errorf("invalid seed video id")
	}
	if limit <= 0 {
		limit = 40
	}

	var extras []string
	seenSeeds := map[string]bool{primary: true}
	for _, raw := range extraSeedIDs {
		id := ExtractYouTubeID(raw)
		if id == "" || seenSeeds[id] {
			continue
		}
		seenSeeds[id] = true
		extras = append(extras, id)
		if len(extras) >= 2 {
			break
		}
	}

	if len(extras) == 0 {
		tracks, err := c.FetchRadio(ctx, primary, limit)
		for i := range tracks {
			if tracks[i].ConsensusCount <= 0 {
				tracks[i].ConsensusCount = 1
			}
		}
		return tracks, err
	}

	type seedRes struct {
		tracks []RadioTrack
		err    error
	}

	primaryCh := make(chan seedRes, 1)
	go func() {
		tr, err := c.FetchRadio(ctx, primary, limit)
		primaryCh <- seedRes{tracks: tr, err: err}
	}()

	extraCtx, cancelExtras := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancelExtras()
	extraChs := make([]chan []RadioTrack, len(extras))
	for i, sid := range extras {
		ch := make(chan []RadioTrack, 1)
		extraChs[i] = ch
		go func(id string, out chan<- []RadioTrack) {
			tr, err := c.FetchRadio(extraCtx, id, 25)
			if err != nil {
				out <- nil
				return
			}
			out <- tr
		}(sid, ch)
	}

	pRes := <-primaryCh
	if pRes.err != nil && len(pRes.tracks) == 0 {
		return nil, pRes.err
	}

	type mergedItem struct {
		track RadioTrack
		seeds map[string]bool
	}
	merged := make(map[string]*mergedItem)
	var order []string
	addBatch := func(seed string, batch []RadioTrack) {
		for _, tr := range batch {
			if tr.ID == "" {
				continue
			}
			key := canonicalSongKey(tr.Title, tr.Artist)
			if key == "" {
				key = "id::" + tr.ID
			}
			if cur, exists := merged[key]; exists {
				cur.seeds[seed] = true
				if videoTypeRank(tr.VideoType) > videoTypeRank(cur.track.VideoType) {
					cur.track = tr
				}
			} else {
				merged[key] = &mergedItem{
					track: tr,
					seeds: map[string]bool{seed: true},
				}
				order = append(order, key)
			}
		}
	}

	addBatch(primary, pRes.tracks)
	primaryIsMusic := false
	for _, tr := range pRes.tracks {
		if tr.VideoType == musicVideoTypeATV || tr.VideoType == musicVideoTypeOMV || tr.VideoType == "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC" {
			primaryIsMusic = true
			break
		}
	}
	if primaryIsMusic {
		for i, ch := range extraChs {
			addBatch(extras[i], <-ch)
		}
	} else {
		cancelExtras()
		for _, ch := range extraChs {
			<-ch
		}
	}

	out := make([]RadioTrack, 0, len(order))
	for _, k := range order {
		item := merged[k]
		tr := item.track
		tr.ConsensusCount = len(item.seeds)
		out = append(out, tr)
	}
	return out, nil
}

// isNonMusicUGCRadio triggers the fallback gate whenever YouTube Music either
// tags the seed video itself as UGC (filling the rest with a generic default
// trending playlist), omits the seed video altogether, or returns zero
// official ATV/OMV music tracks.
func isNonMusicUGCRadio(seedID string, tracks []RadioTrack) bool {
	if len(tracks) == 0 {
		return true
	}
	seedFound := false
	seedIsUGC := false
	hasCuratedMusic := false
	for _, tr := range tracks {
		if seedID != "" && tr.ID == seedID {
			seedFound = true
			if tr.VideoType == musicVideoTypeUGC {
				seedIsUGC = true
			}
		}
		if tr.VideoType == musicVideoTypeATV || tr.VideoType == musicVideoTypeOMV || tr.VideoType == "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC" {
			hasCuratedMusic = true
		}
	}
	if seedIsUGC {
		return true
	}
	if seedID != "" && !seedFound {
		return true
	}
	return !hasCuratedMusic
}

func (c *RadioClient) fetchYTMusicNext(ctx context.Context, seedID string) ([]RadioTrack, error) {
	payload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB_REMIX",
				"clientVersion": "1.20240918.01.00",
				"hl":            "es",
				"gl":            "CO",
			},
		},
		"enablePersistentPlaylistPanel": true,
		"isAudioOnly":                   true,
		"videoId":                       seedID,
		"playlistId":                    "RDAMVM" + seedID,
	}

	cookieHeader, sapisid := loadYouTubeCookies(c.getCookiesPath())
	if cookieHeader != "" && sapisid != "" {
		raw, status, err := c.doNextPost(ctx, c.ytmBaseURL, payload, cookieHeader, sapisid, c.ytmBaseURL)
		if err == nil && status >= 200 && status < 300 {
			if tracks := parseInnerTubeNextTracks(raw); len(tracks) > 0 {
				return tracks, nil
			}
		}
	}

	raw, status, err := c.doNextPost(ctx, c.ytmBaseURL, payload, "", "", c.ytmBaseURL)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("ytmusic next HTTP %d", status)
	}
	return parseInnerTubeNextTracks(raw), nil
}

func (c *RadioClient) fetchYouTubeNext(ctx context.Context, seedID string) ([]RadioTrack, error) {
	rdPayload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20250501.00.00",
				"hl":            "es",
				"gl":            "CO",
			},
		},
		"videoId":    seedID,
		"playlistId": "RD" + seedID,
	}

	raw, status, err := c.doNextPost(ctx, c.ytBaseURL, rdPayload, "", "", c.ytBaseURL)
	if err == nil && status >= 200 && status < 300 {
		if tracks := parseInnerTubeNextTracks(raw); len(tracks) > 0 {
			return tracks, nil
		}
	}

	// Non-music videos often return no RD<id> playlist; query watch-next related videos directly.
	watchPayload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20250501.00.00",
				"hl":            "es",
				"gl":            "CO",
			},
		},
		"videoId": seedID,
	}
	raw2, status2, err2 := c.doNextPost(ctx, c.ytBaseURL, watchPayload, "", "", c.ytBaseURL)
	if err2 != nil {
		if err != nil {
			return nil, err
		}
		return nil, err2
	}
	if status2 < 200 || status2 >= 300 {
		return nil, fmt.Errorf("youtube next HTTP %d", status2)
	}
	return parseInnerTubeNextTracks(raw2), nil
}

func (c *RadioClient) doNextPost(ctx context.Context, baseURL string, payload map[string]any, cookieHeader, sapisid, origin string) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	endpoint := baseURL + "/youtubei/v1/next?prettyPrint=false"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36")
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
	if cookieHeader != "" && sapisid != "" {
		req.Header.Set("Cookie", cookieHeader)
		req.Header.Set("Authorization", buildSAPISIDHash(time.Now().Unix(), sapisid, origin))
		req.Header.Set("X-Origin", origin)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return raw, resp.StatusCode, err
}

func loadYouTubeCookies(jarPath string) (cookieHeader, sapisid string) {
	if strings.TrimSpace(jarPath) == "" {
		return "", ""
	}
	data, err := os.ReadFile(jarPath)
	if err != nil {
		return "", ""
	}
	var pairs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := strings.ToLower(fields[0])
		if !strings.Contains(domain, "youtube.com") && !strings.Contains(domain, "google.com") {
			continue
		}
		name := fields[5]
		val := fields[6]
		pairs = append(pairs, name+"="+val)
		if name == "SAPISID" || name == "__Secure-3PAPISID" {
			if sapisid == "" || name == "SAPISID" {
				sapisid = val
			}
		}
	}
	return strings.Join(pairs, "; "), sapisid
}

func buildSAPISIDHash(ts int64, sapisid, origin string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", ts, sapisid, origin)))
	return fmt.Sprintf("SAPISIDHASH %d_%s", ts, hex.EncodeToString(sum[:]))
}

type nextRunText struct {
	Runs []struct {
		Text string `json:"text"`
	} `json:"runs"`
	SimpleText string `json:"simpleText"`
}

func (t nextRunText) String() string {
	if t.SimpleText != "" {
		return strings.TrimSpace(t.SimpleText)
	}
	var b strings.Builder
	for _, r := range t.Runs {
		b.WriteString(r.Text)
	}
	return strings.TrimSpace(b.String())
}

type playlistPanelVideoRendererJSON struct {
	VideoID            string      `json:"videoId"`
	Title              nextRunText `json:"title"`
	ShortBylineText    nextRunText `json:"shortBylineText"`
	LongBylineText     nextRunText `json:"longBylineText"`
	LengthText         nextRunText `json:"lengthText"`
	NavigationEndpoint struct {
		WatchEndpoint struct {
			VideoID                            string `json:"videoId"`
			WatchEndpointMusicSupportedConfigs struct {
				WatchEndpointMusicConfig struct {
					MusicVideoType string `json:"musicVideoType"`
				} `json:"watchEndpointMusicConfig"`
			} `json:"watchEndpointMusicSupportedConfigs"`
		} `json:"watchEndpoint"`
	} `json:"navigationEndpoint"`
}

type lockupViewModelJSON struct {
	ContentID    string `json:"contentId"`
	ContentType  string `json:"contentType"`
	ContentImage struct {
		ThumbnailViewModel struct {
			Overlays []struct {
				ThumbnailBottomOverlayViewModel struct {
					Badges []struct {
						ThumbnailBadgeViewModel struct {
							Text       string `json:"text"`
							BadgeStyle string `json:"badgeStyle"`
						} `json:"thumbnailBadgeViewModel"`
					} `json:"badges"`
				} `json:"thumbnailBottomOverlayViewModel"`
			} `json:"overlays"`
		} `json:"thumbnailViewModel"`
	} `json:"contentImage"`
	Metadata struct {
		LockupMetadataViewModel struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
			Metadata struct {
				ContentMetadataViewModel struct {
					MetadataRows []struct {
						MetadataParts []struct {
							Text struct {
								Content string `json:"content"`
							} `json:"text"`
						} `json:"metadataParts"`
					} `json:"metadataRows"`
				} `json:"contentMetadataViewModel"`
			} `json:"metadata"`
		} `json:"lockupMetadataViewModel"`
	} `json:"metadata"`
}

type compactVideoRendererJSON struct {
	VideoID         string      `json:"videoId"`
	Title           nextRunText `json:"title"`
	ShortBylineText nextRunText `json:"shortBylineText"`
	LongBylineText  nextRunText `json:"longBylineText"`
	LengthText      nextRunText `json:"lengthText"`
}

func parseInnerTubeNextTracks(raw []byte) []RadioTrack {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var playlistTracks []RadioTrack
	var relatedTracks []RadioTrack
	var walk func(node any)
	walk = func(node any) {
		switch x := node.(type) {
		case map[string]any:
			if rawRenderer, ok := x["playlistPanelVideoRenderer"]; ok {
				if data, err := json.Marshal(rawRenderer); err == nil {
					var r playlistPanelVideoRendererJSON
					if json.Unmarshal(data, &r) == nil {
						id := strings.TrimSpace(r.VideoID)
						if id == "" {
							id = strings.TrimSpace(r.NavigationEndpoint.WatchEndpoint.VideoID)
						}
						if id != "" && !seen[id] {
							seen[id] = true
							artist := firstBylineArtist(r.LongBylineText, r.ShortBylineText)
							playlistTracks = append(playlistTracks, RadioTrack{
								ID:          id,
								Title:       r.Title.String(),
								Artist:      artist,
								VideoType:   r.NavigationEndpoint.WatchEndpoint.WatchEndpointMusicSupportedConfigs.WatchEndpointMusicConfig.MusicVideoType,
								DurationSec: parseDurationSeconds(r.LengthText.String()),
							})
						}
					}
				}
			}
			if rawLockup, ok := x["lockupViewModel"]; ok {
				if data, err := json.Marshal(rawLockup); err == nil {
					var lv lockupViewModelJSON
					if json.Unmarshal(data, &lv) == nil {
						id := strings.TrimSpace(lv.ContentID)
						if ytIDPattern.MatchString(id) &&
							(lv.ContentType == "" || lv.ContentType == "LOCKUP_CONTENT_TYPE_VIDEO") &&
							!seen[id] {
							isLive := false
							durSec := 0
							for _, ov := range lv.ContentImage.ThumbnailViewModel.Overlays {
								for _, b := range ov.ThumbnailBottomOverlayViewModel.Badges {
									bm := b.ThumbnailBadgeViewModel
									if bm.BadgeStyle == "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE" || strings.EqualFold(strings.TrimSpace(bm.Text), "LIVE") || strings.EqualFold(strings.TrimSpace(bm.Text), "EN DIRECTO") {
										isLive = true
										break
									}
									if durSec == 0 && bm.Text != "" {
										durSec = parseDurationSeconds(bm.Text)
									}
								}
								if isLive {
									break
								}
							}
							title := strings.TrimSpace(lv.Metadata.LockupMetadataViewModel.Title.Content)
							if !isLive && title != "" {
								seen[id] = true
								artist := ""
								rows := lv.Metadata.LockupMetadataViewModel.Metadata.ContentMetadataViewModel.MetadataRows
								if len(rows) > 0 && len(rows[0].MetadataParts) > 0 {
									artist = strings.TrimSpace(rows[0].MetadataParts[0].Text.Content)
								}
								relatedTracks = append(relatedTracks, RadioTrack{
									ID:          id,
									Title:       title,
									Artist:      artist,
									DurationSec: durSec,
								})
							}
						}
					}
				}
			}
			if rawCompact, ok := x["compactVideoRenderer"]; ok {
				if data, err := json.Marshal(rawCompact); err == nil {
					var cv compactVideoRendererJSON
					if json.Unmarshal(data, &cv) == nil {
						id := strings.TrimSpace(cv.VideoID)
						title := cv.Title.String()
						durStr := cv.LengthText.String()
						if ytIDPattern.MatchString(id) && title != "" && durStr != "" && !seen[id] {
							seen[id] = true
							relatedTracks = append(relatedTracks, RadioTrack{
								ID:          id,
								Title:       title,
								Artist:      firstBylineArtist(cv.LongBylineText, cv.ShortBylineText),
								DurationSec: parseDurationSeconds(durStr),
							})
						}
					}
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(root)
	if len(playlistTracks) > 0 {
		return playlistTracks
	}
	return relatedTracks
}

func firstBylineArtist(longText, shortText nextRunText) string {
	// LongBylineText on YT Music looks like: "Soda Stereo • Canción Animal • 1990"
	if len(longText.Runs) > 0 {
		if first := strings.TrimSpace(longText.Runs[0].Text); first != "" && first != "•" {
			return first
		}
	}
	raw := shortText.String()
	if raw == "" {
		raw = longText.String()
	}
	if idx := strings.Index(raw, "•"); idx > 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	return raw
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
