package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/store"
)

// LocalMusicMinTracks is the minimum number of unique local music tracks
// required before TabMusic switches from cold-start YT Music discovery to the
// local-first personalized music feed.
const LocalMusicMinTracks = 5

const (
	localMusicCacheFile  = "feed_music_local.json"
	defaultLocalMusicURL = "https://music.youtube.com"
)

// LocalMusicData aggregates the user's local iutui library and engagement stats.
type LocalMusicData struct {
	Liked   []model.Video
	History []model.Video
	Stats   map[string]store.TrackStat
}

type localRadioCandidate struct {
	video       model.Video
	songKey     string
	artistKey   string
	videoType   string
	durationSec int
	rank        int
}

var (
	lmParenNoiseRe  = regexp.MustCompile(`(?i)\(([^)]*(official|video|audio|lyric|letra|hd|4k|hq|visualizer|remaster|live|en vivo|explicit|clean|version|vers[ií]on|feat\.?|ft\.?)[^)]*)\)`)
	lmBrackNoiseRe  = regexp.MustCompile(`(?i)\[([^\]]*(official|video|audio|lyric|letra|hd|4k|hq|visualizer|remaster|live|en vivo|explicit|clean|version|vers[ií]on|feat\.?|ft\.?)[^\]]*)\]`)
	lmFeatSuffixRe  = regexp.MustCompile(`(?i)\b(feat\.?|ft\.?|con|with)\b.*$`)
	lmMultiSpaceRe  = regexp.MustCompile(`\s+`)
	lmArtistSplitRe = regexp.MustCompile(`(?i)\s*(?:,|&|\bx\b|\bfeat\.?\b|\bft\.?\b|\band\b|\by\b)\s*`)
)

// BuildLocalMusicFeed builds a personalized TabMusic feed strictly from local
// iutui listening signals when the user has >= LocalMusicMinTracks unique local
// music tracks. Returns handled=false for cold-start users (< 5 tracks).
func BuildLocalMusicFeed(
	ctx context.Context,
	data LocalMusicData,
	cacheDir string,
	cacheTTL time.Duration,
	refresh bool,
	feedSize int,
	hc *http.Client,
	baseURL string,
) (videos []model.Video, handled bool, err error) {
	if feedSize <= 0 {
		feedSize = 30
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultLocalMusicURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if hc == nil {
		hc = &http.Client{Timeout: 6 * time.Second}
	}

	eligible := collectEligibleLocalMusic(data.Liked, data.History, data.Stats)
	if len(eligible) < LocalMusicMinTracks {
		return nil, false, nil
	}

	var cachePath string
	if strings.TrimSpace(cacheDir) != "" {
		cachePath = filepath.Join(cacheDir, localMusicCacheFile)
	}
	if !refresh && cachePath != "" && cacheTTL > 0 {
		if cached, modAt, readErr := readCacheFile(cachePath); readErr == nil && len(cached) > 0 && time.Since(modAt) < cacheTTL {
			return cached, true, nil
		}
	}

	seeds := pickLocalSeeds(eligible, data.Liked, data.Stats, 5)
	if len(seeds) == 0 {
		return nil, false, nil
	}

	// Fetch cookieless YouTube Music WEB_REMIX next radios for up to 5 local seeds in parallel.
	fetchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	type batch struct {
		seedID string
		tracks []localRadioCandidate
	}
	ch := make(chan batch, len(seeds))
	var wg sync.WaitGroup
	for _, seed := range seeds {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			items, _ := fetchCookielessYTMusicRadio(fetchCtx, hc, baseURL, sid)
			ch <- batch{seedID: sid, tracks: items}
		}(seed.ID)
	}
	wg.Wait()
	close(ch)

	seedSet := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		seedSet[s.ID] = true
	}

	merged := make(map[string]*localRadioCandidate)
	mergedOrder := make([]string, 0, 128)
	seedHits := make(map[string]map[string]bool)

	for b := range ch {
		for idx, cand := range b.tracks {
			if cand.video.ID == "" || seedSet[cand.video.ID] || isLocalJunkTitle(cand.video.Title) {
				continue
			}
			if cand.durationSec > 0 && (cand.durationSec < 75 || cand.durationSec > 720) {
				continue
			}
			sk := lmCanonicalSongKey(cand.video.Title, cand.video.Channel)
			if sk == "" {
				sk = "id::" + cand.video.ID
			}
			cand.songKey = sk
			cand.artistKey = lmPrimaryArtistKey(cand.video.Channel, cand.video.Title)
			cand.rank = idx

			if seedHits[sk] == nil {
				seedHits[sk] = make(map[string]bool)
			}
			seedHits[sk][b.seedID] = true

			if prev, exists := merged[sk]; exists {
				if lmVideoTypeRank(cand.videoType) > lmVideoTypeRank(prev.videoType) {
					prev.video = cand.video
					prev.videoType = cand.videoType
					prev.durationSec = cand.durationSec
				}
				if idx < prev.rank {
					prev.rank = idx
				}
			} else {
				copyCand := cand
				merged[sk] = &copyCand
				mergedOrder = append(mergedOrder, sk)
			}
		}
	}

	if len(mergedOrder) == 0 {
		// Offline/network fallback: return clean local music favorites rather than stale global YT Music.
		localOut := make([]model.Video, 0, min(feedSize, len(eligible)))
		for _, v := range eligible {
			localOut = append(localOut, v)
			if len(localOut) >= feedSize {
				break
			}
		}
		if len(localOut) > 0 {
			return localOut, true, nil
		}
		return nil, false, nil
	}

	likedArtistW, histArtistW, likedSongSet := buildLocalAffinity(data.Liked, data.History, data.Stats)

	type scored struct {
		video     model.Video
		artistKey string
		songKey   string
		score     float64
	}
	discoveries := make([]scored, 0, len(mergedOrder))
	for _, sk := range mergedOrder {
		c := merged[sk]
		if st, ok := data.Stats[c.video.ID]; ok && st.Skips > st.Completes+1 {
			continue
		}
		consensus := len(seedHits[sk])
		score := 100.0 - float64(c.rank)*1.0 + float64(max(0, consensus-1))*25.0
		switch c.videoType {
		case "MUSIC_VIDEO_TYPE_ATV":
			score += 36
		case "MUSIC_VIDEO_TYPE_OMV", "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC":
			score += 24
		case "MUSIC_VIDEO_TYPE_UGC":
			score -= 18
		}
		if c.durationSec >= 120 && c.durationSec <= 360 {
			score += 8
		}
		if c.artistKey != "" {
			if w := likedArtistW[c.artistKey]; w > 0 {
				score += minFloatCat(28, 14+w*3)
			}
			if w := histArtistW[c.artistKey]; w > 0 {
				score += minFloatCat(18, 6+w*2)
			}
		}
		if likedSongSet[sk] {
			score += 10
		}
		if st, ok := data.Stats[c.video.ID]; ok {
			score += float64(st.Completes) * 4.0
			score -= float64(st.Skips) * 35.0
		}
		discoveries = append(discoveries, scored{
			video:     c.video,
			artistKey: c.artistKey,
			songKey:   sk,
			score:     score,
		})
	}
	sort.SliceStable(discoveries, func(i, j int) bool {
		return discoveries[i].score > discoveries[j].score
	})

	// Mix ~75% fresh discoveries + ~25% proven local favorites with artist spacing.
	favQuota := max(2, feedSize/4)
	discQuota := max(1, feedSize-favQuota)

	pickedSongs := make(map[string]bool)
	pickedIDs := make(map[string]bool)
	artistCounts := make(map[string]int)
	var recentArtists []string

	pickNextDiscovery := func() (model.Video, bool) {
		bestIdx := -1
		bestAdj := -1e9
		for i, d := range discoveries {
			if pickedIDs[d.video.ID] || pickedSongs[d.songKey] {
				continue
			}
			if d.artistKey != "" && artistCounts[d.artistKey] >= 2 {
				continue
			}
			adj := d.score
			if d.artistKey != "" {
				for back := 1; back <= 3 && back <= len(recentArtists); back++ {
					if recentArtists[len(recentArtists)-back] == d.artistKey {
						adj -= float64(55 - (back-1)*12)
						break
					}
				}
			}
			if adj > bestAdj {
				bestAdj = adj
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			return model.Video{}, false
		}
		d := discoveries[bestIdx]
		pickedIDs[d.video.ID] = true
		pickedSongs[d.songKey] = true
		if d.artistKey != "" {
			artistCounts[d.artistKey]++
			recentArtists = append(recentArtists, d.artistKey)
		}
		return d.video, true
	}

	var discList []model.Video
	for len(discList) < discQuota {
		v, ok := pickNextDiscovery()
		if !ok {
			break
		}
		discList = append(discList, v)
	}

	var favList []model.Video
	favPool := append(append([]model.Video(nil), data.Liked...), eligible...)
	for _, v := range favPool {
		if len(favList) >= favQuota {
			break
		}
		if !isMusicEligibleVideo(v, data.Stats) {
			continue
		}
		sk := lmCanonicalSongKey(v.Title, v.Channel)
		if pickedIDs[v.ID] || (sk != "" && pickedSongs[sk]) {
			continue
		}
		if st, ok := data.Stats[v.ID]; ok && st.Skips > st.Completes+1 {
			continue
		}
		ak := lmPrimaryArtistKey(v.Channel, v.Title)
		if ak != "" && artistCounts[ak] >= 2 {
			continue
		}
		pickedIDs[v.ID] = true
		if sk != "" {
			pickedSongs[sk] = true
		}
		if ak != "" {
			artistCounts[ak]++
		}
		favList = append(favList, v)
	}

	// Interleave: every 4th slot highlights a proven local favorite among fresh discoveries.
	out := make([]model.Video, 0, feedSize)
	di, fi := 0, 0
	for len(out) < feedSize && (di < len(discList) || fi < len(favList)) {
		if (len(out)%4 == 3 && fi < len(favList)) || di >= len(discList) {
			out = append(out, favList[fi])
			fi++
		} else {
			out = append(out, discList[di])
			di++
		}
	}

	if cachePath != "" && cacheTTL > 0 && len(out) > 0 {
		_ = writeCacheFile(cachePath, out)
	}
	return out, true, nil
}

func collectEligibleLocalMusic(liked, history []model.Video, stats map[string]store.TrackStat) []model.Video {
	seenID := make(map[string]bool)
	seenSong := make(map[string]bool)
	var out []model.Video
	addList := func(list []model.Video, forceInclude bool) {
		for _, v := range list {
			id := strings.TrimSpace(v.ID)
			if id == "" || len(id) != 11 || seenID[id] {
				continue
			}
			if !forceInclude && !isMusicEligibleVideo(v, stats) {
				continue
			}
			sk := lmCanonicalSongKey(v.Title, v.Channel)
			if sk != "" && seenSong[sk] {
				continue
			}
			seenID[id] = true
			if sk != "" {
				seenSong[sk] = true
			}
			out = append(out, v)
		}
	}
	addList(liked, true)
	addList(history, false)
	return out
}

func isMusicEligibleVideo(v model.Video, stats map[string]store.TrackStat) bool {
	if v.IsLive || len(strings.TrimSpace(v.ID)) != 11 {
		return false
	}
	if isLocalJunkTitle(v.Title) {
		return false
	}
	dur := lmParseDurationSeconds(v.Duration)
	if dur > 0 && (dur < 75 || dur > 720) {
		return false
	}
	lower := strings.ToLower(v.Title)
	nonMusicHints := []string{"podcast", "gameplay", "review", "unboxing", "mod de ", "trailer", "capítulo", "episodio", "noticias", "news", "stream highlights"}
	for _, h := range nonMusicHints {
		if strings.Contains(lower, h) {
			return false
		}
	}
	if st, ok := stats[v.ID]; ok && st.Skips > st.Completes+2 {
		return false
	}
	return strings.TrimSpace(v.Title) != ""
}

func pickLocalSeeds(eligible, liked []model.Video, stats map[string]store.TrackStat, maxSeeds int) []model.Video {
	likedIDs := make(map[string]bool, len(liked))
	for _, v := range liked {
		likedIDs[v.ID] = true
	}
	type seedCandidate struct {
		video     model.Video
		artistKey string
		score     float64
	}
	cands := make([]seedCandidate, 0, len(eligible))
	for i, v := range eligible {
		st := stats[v.ID]
		if st.Skips > st.Completes+1 {
			continue
		}
		score := float64(max(0, 60-i))
		if likedIDs[v.ID] {
			score += 35
		}
		score += float64(st.Completes) * 18.0
		score += float64(st.Plays) * 4.0
		score -= float64(st.Skips) * 30.0
		cands = append(cands, seedCandidate{
			video:     v,
			artistKey: lmPrimaryArtistKey(v.Channel, v.Title),
			score:     score,
		})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].score > cands[j].score
	})

	usedArtists := make(map[string]bool)
	seeds := make([]model.Video, 0, maxSeeds)
	for _, c := range cands {
		if c.artistKey != "" && usedArtists[c.artistKey] {
			continue
		}
		if c.artistKey != "" {
			usedArtists[c.artistKey] = true
		}
		seeds = append(seeds, c.video)
		if len(seeds) >= maxSeeds {
			return seeds
		}
	}
	for _, c := range cands {
		already := false
		for _, s := range seeds {
			if s.ID == c.video.ID {
				already = true
				break
			}
		}
		if !already {
			seeds = append(seeds, c.video)
			if len(seeds) >= maxSeeds {
				break
			}
		}
	}
	return seeds
}

func buildLocalAffinity(liked, history []model.Video, stats map[string]store.TrackStat) (map[string]float64, map[string]float64, map[string]bool) {
	likedW := make(map[string]float64)
	histW := make(map[string]float64)
	likedSongs := make(map[string]bool)
	for _, v := range liked {
		if ak := lmPrimaryArtistKey(v.Channel, v.Title); ak != "" {
			likedW[ak] += 1.0
		}
		if sk := lmCanonicalSongKey(v.Title, v.Channel); sk != "" {
			likedSongs[sk] = true
		}
	}
	for i, v := range history {
		if i >= 120 {
			break
		}
		if !isMusicEligibleVideo(v, stats) {
			continue
		}
		if ak := lmPrimaryArtistKey(v.Channel, v.Title); ak != "" {
			w := 0.5
			if i < 30 {
				w = 1.0
			}
			if st, ok := stats[v.ID]; ok {
				if st.Completes > 0 {
					w += minFloatCat(2.0, float64(st.Completes)*0.5)
				}
				if st.Skips > st.Completes {
					w -= float64(st.Skips - st.Completes)
				}
			}
			if w > 0 {
				histW[ak] += w
			}
		}
	}
	return likedW, histW, likedSongs
}

func fetchCookielessYTMusicRadio(ctx context.Context, hc *http.Client, baseURL, seedID string) ([]localRadioCandidate, error) {
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
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/youtubei/v1/next?prettyPrint=false", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", baseURL+"/")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ytmusic local next HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseLocalRadioCandidates(raw), nil
}

type lmRunText struct {
	Runs []struct {
		Text string `json:"text"`
	} `json:"runs"`
	SimpleText string `json:"simpleText"`
}

func (t lmRunText) String() string {
	if t.SimpleText != "" {
		return strings.TrimSpace(t.SimpleText)
	}
	var b strings.Builder
	for _, r := range t.Runs {
		b.WriteString(r.Text)
	}
	return strings.TrimSpace(b.String())
}

func parseLocalRadioCandidates(raw []byte) []localRadioCandidate {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	type rendererJSON struct {
		VideoID            string    `json:"videoId"`
		Title              lmRunText `json:"title"`
		ShortBylineText    lmRunText `json:"shortBylineText"`
		LongBylineText     lmRunText `json:"longBylineText"`
		LengthText         lmRunText `json:"lengthText"`
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

	seen := make(map[string]bool)
	var out []localRadioCandidate
	var walk func(node any)
	walk = func(node any) {
		switch x := node.(type) {
		case map[string]any:
			if rr, ok := x["playlistPanelVideoRenderer"]; ok {
				if data, err := json.Marshal(rr); err == nil {
					var r rendererJSON
					if json.Unmarshal(data, &r) == nil {
						id := strings.TrimSpace(r.VideoID)
						if id == "" {
							id = strings.TrimSpace(r.NavigationEndpoint.WatchEndpoint.VideoID)
						}
						if id != "" && !seen[id] {
							seen[id] = true
							artist := ""
							if len(r.LongBylineText.Runs) > 0 {
								if first := strings.TrimSpace(r.LongBylineText.Runs[0].Text); first != "" && first != "•" {
									artist = first
								}
							}
							if artist == "" {
								artist = r.ShortBylineText.String()
								if idx := strings.Index(artist, "•"); idx > 0 {
									artist = strings.TrimSpace(artist[:idx])
								}
							}
							durStr := r.LengthText.String()
							out = append(out, localRadioCandidate{
								video: model.Video{
									ID:        id,
									Title:     r.Title.String(),
									Channel:   artist,
									Duration:  durStr,
									Thumbnail: fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", id),
								},
								videoType:   r.NavigationEndpoint.WatchEndpoint.WatchEndpointMusicSupportedConfigs.WatchEndpointMusicConfig.MusicVideoType,
								durationSec: lmParseDurationSeconds(durStr),
							})
						}
					}
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, it := range x {
				walk(it)
			}
		}
	}
	walk(root)
	return out
}

func isLocalJunkTitle(title string) bool {
	lower := strings.ToLower(title)
	for _, tok := range []string{
		"1 hour", "2 hours", "3 hours", "10 hours", "full album",
		"álbum completo", "album completo", "compilation", "recopilación",
		"sped up", "slowed + reverb", "slowed & reverb", "slowed and reverb",
		"nightcore", "8d audio", "bass boosted", "reaction", "tutorial",
		"karaoke", "mega mix", "megamix",
	} {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func lmVideoTypeRank(vt string) int {
	switch vt {
	case "MUSIC_VIDEO_TYPE_ATV":
		return 3
	case "MUSIC_VIDEO_TYPE_OMV", "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC":
		return 2
	case "":
		return 1
	default:
		return 0
	}
}

func lmParseDurationSeconds(text string) int {
	text = strings.TrimSpace(text)
	if text == "" || text == "-" || strings.EqualFold(text, "MIX") {
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

func lmStripAccents(s string) string {
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

func lmNormalizeAlphaNum(s string) string {
	s = lmStripAccents(strings.ToLower(strings.TrimSpace(s)))
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

func lmPrimaryArtistKey(artist, title string) string {
	a := strings.TrimSpace(artist)
	if strings.EqualFold(a, "YouTube") {
		a = ""
	}
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
	parts := lmArtistSplitRe.Split(a, 2)
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		a = parts[0]
	}
	return lmNormalizeAlphaNum(a)
}

func lmCanonicalSongKey(title, artist string) string {
	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(artist), "YouTube") {
		artist = ""
	}
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
	cleanTitle = lmParenNoiseRe.ReplaceAllString(cleanTitle, " ")
	cleanTitle = lmBrackNoiseRe.ReplaceAllString(cleanTitle, " ")
	cleanTitle = lmFeatSuffixRe.ReplaceAllString(cleanTitle, "")
	cleanTitle = lmMultiSpaceRe.ReplaceAllString(cleanTitle, " ")

	normTitle := lmNormalizeAlphaNum(cleanTitle)
	normArtist := lmPrimaryArtistKey(artist, title)
	if normTitle == "" {
		return ""
	}
	if normArtist == "" {
		return normTitle
	}
	return normArtist + "::" + normTitle
}

func minFloatCat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
