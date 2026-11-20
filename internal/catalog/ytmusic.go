package catalog

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const (
	defaultYTMusicBrowseURL     = "https://music.youtube.com/youtubei/v1/browse?prettyPrint=false"
	defaultYTMusicOrigin        = "https://music.youtube.com"
	defaultYTMusicClientVersion = "1.20260401.01.00"
	defaultYTMusicUserAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
	maxYTMusicResponseBytes     = 16 << 20
)

var (
	ytMusicDurationRe      = regexp.MustCompile(`^\d{1,2}:\d{2}(:\d{2})?$`)
	errYTMusicLimitReached = errors.New("ytmusic item limit reached")
)

func parseYouTubeCookies(jarPath string) (cookieHeader, sapisid string, ok bool) {
	raw, err := os.ReadFile(jarPath)
	if err != nil {
		return "", "", false
	}

	nowUnix := time.Now().Unix()
	values := make(map[string]string)
	var order []string

	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			continue
		}

		domain := strings.ToLower(strings.TrimSpace(fields[0]))
		if domain != "youtube.com" && !strings.HasSuffix(domain, ".youtube.com") {
			continue
		}

		if exp, parseErr := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64); parseErr == nil && exp > 0 && exp < nowUnix {
			continue
		}

		name := strings.TrimSpace(fields[5])
		if name == "" {
			continue
		}
		value := fields[6]

		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}

	if len(order) == 0 {
		return "", "", false
	}

	pairs := make([]string, 0, len(order))
	for _, name := range order {
		pairs = append(pairs, name+"="+values[name])
	}
	cookieHeader = strings.Join(pairs, "; ")

	sapisid = values["SAPISID"]
	if sapisid == "" {
		sapisid = values["__Secure-3PAPISID"]
	}

	return cookieHeader, sapisid, sapisid != "" && cookieHeader != ""
}

func buildSAPISIDHash(ts int64, sapisid, origin string) string {
	digest := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", ts, sapisid, origin)))
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, digest)
}

func (c *YTDLPCatalog) fetchYTMusicHome(ctx context.Context) (videos []model.Video, loggedOut bool, err error) {
	jar := c.cookieJarPath()
	if jar == "" {
		return nil, false, nil
	}

	cookieHeader, sapisid, ok := parseYouTubeCookies(jar)
	if !ok {
		return nil, false, nil
	}

	c.cfgMu.RLock()
	endpoint := c.musicEndpoint
	client := c.musicHTTPClient
	c.cfgMu.RUnlock()

	if endpoint == "" {
		endpoint = defaultYTMusicBrowseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}

	payload := []byte(`{"context":{"client":{"clientName":"WEB_REMIX","clientVersion":"` + defaultYTMusicClientVersion + `"}},"browseId":"FEmusic_home"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultYTMusicUserAgent)
	req.Header.Set("Origin", defaultYTMusicOrigin)
	req.Header.Set("Referer", defaultYTMusicOrigin+"/")
	req.Header.Set("X-Origin", defaultYTMusicOrigin)
	req.Header.Set("X-Goog-AuthUser", "0")
	req.Header.Set("X-YouTube-Client-Name", "67")
	req.Header.Set("X-YouTube-Client-Version", defaultYTMusicClientVersion)
	req.Header.Set("Authorization", buildSAPISIDHash(time.Now().Unix(), sapisid, defaultYTMusicOrigin))
	req.Header.Set("Cookie", cookieHeader)

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("ytmusic browse HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxYTMusicResponseBytes))
	if err != nil {
		return nil, false, err
	}

	return parseYTMusicBrowse(body, c.feedSize())
}

func parseYTMusicBrowse(data []byte, limit int) ([]model.Video, bool, error) {
	var env struct {
		ResponseContext struct {
			MainAppWebResponseContext struct {
				LoggedOut bool `json:"loggedOut"`
			} `json:"mainAppWebResponseContext"`
		} `json:"responseContext"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, err
	}
	if env.ResponseContext.MainAppWebResponseContext.LoggedOut {
		return nil, true, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	seen := make(map[string]struct{})
	var videos []model.Video

	err := walkYTMusicDecoder(dec, seen, &videos, limit)
	if err != nil && !errors.Is(err, errYTMusicLimitReached) {
		return nil, false, err
	}
	return videos, false, nil
}

func walkYTMusicDecoder(dec *json.Decoder, seen map[string]struct{}, videos *[]model.Video, limit int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("unexpected JSON key token %T", keyTok)
			}

			switch key {
			case "musicResponsiveListItemRenderer":
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return err
				}
				if limit <= 0 || len(*videos) < limit {
					if v, ok := parseResponsiveMusicItem(raw); ok {
						if _, dup := seen[v.ID]; !dup {
							seen[v.ID] = struct{}{}
							*videos = append(*videos, v)
						}
					}
				}
			case "musicTwoRowItemRenderer":
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return err
				}
				if limit <= 0 || len(*videos) < limit {
					if v, ok := parseTwoRowMusicItem(raw); ok {
						if _, dup := seen[v.ID]; !dup {
							seen[v.ID] = struct{}{}
							*videos = append(*videos, v)
						}
					}
				}
			case "responseContext":
				var skip json.RawMessage
				if err := dec.Decode(&skip); err != nil {
					return err
				}
			default:
				if err := walkYTMusicDecoder(dec, seen, videos, limit); err != nil {
					return err
				}
			}

			if limit > 0 && len(*videos) >= limit {
				return errYTMusicLimitReached
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	case '[':
		for dec.More() {
			if err := walkYTMusicDecoder(dec, seen, videos, limit); err != nil {
				return err
			}
			if limit > 0 && len(*videos) >= limit {
				return errYTMusicLimitReached
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	}

	return nil
}

type ytMusicRun struct {
	Text               string `json:"text"`
	NavigationEndpoint struct {
		WatchEndpoint struct {
			VideoID string `json:"videoId"`
		} `json:"watchEndpoint"`
		BrowseEndpoint struct {
			BrowseID string `json:"browseId"`
		} `json:"browseEndpoint"`
	} `json:"navigationEndpoint"`
}

type ytMusicColumn struct {
	MusicResponsiveListItemFlexColumnRenderer struct {
		Text struct {
			Runs []ytMusicRun `json:"runs"`
		} `json:"text"`
	} `json:"musicResponsiveListItemFlexColumnRenderer"`
	MusicResponsiveListItemFixedColumnRenderer struct {
		Text struct {
			Runs []ytMusicRun `json:"runs"`
		} `json:"text"`
	} `json:"musicResponsiveListItemFixedColumnRenderer"`
}

type ytMusicThumbnailList struct {
	MusicThumbnailRenderer struct {
		Thumbnail struct {
			Thumbnails []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"thumbnails"`
		} `json:"thumbnail"`
	} `json:"musicThumbnailRenderer"`
}

type ytMusicOverlay struct {
	MusicItemThumbnailOverlayRenderer struct {
		Content struct {
			MusicPlayButtonRenderer struct {
				PlayNavigationEndpoint struct {
					WatchEndpoint struct {
						VideoID string `json:"videoId"`
					} `json:"watchEndpoint"`
				} `json:"playNavigationEndpoint"`
			} `json:"musicPlayButtonRenderer"`
		} `json:"content"`
	} `json:"musicItemThumbnailOverlayRenderer"`
}

type ytMusicResponsiveListItem struct {
	PlaylistItemData struct {
		VideoID string `json:"videoId"`
	} `json:"playlistItemData"`
	Overlay      ytMusicOverlay       `json:"overlay"`
	FlexColumns  []ytMusicColumn      `json:"flexColumns"`
	FixedColumns []ytMusicColumn      `json:"fixedColumns"`
	Thumbnail    ytMusicThumbnailList `json:"thumbnail"`
}

type ytMusicTwoRowItem struct {
	Title struct {
		Runs []ytMusicRun `json:"runs"`
	} `json:"title"`
	Subtitle struct {
		Runs []ytMusicRun `json:"runs"`
	} `json:"subtitle"`
	NavigationEndpoint struct {
		WatchEndpoint struct {
			VideoID string `json:"videoId"`
		} `json:"watchEndpoint"`
	} `json:"navigationEndpoint"`
	ThumbnailOverlay  ytMusicOverlay       `json:"thumbnailOverlay"`
	ThumbnailRenderer ytMusicThumbnailList `json:"thumbnailRenderer"`
}

func parseResponsiveMusicItem(raw json.RawMessage) (model.Video, bool) {
	var item ytMusicResponsiveListItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return model.Video{}, false
	}

	videoID := strings.TrimSpace(item.PlaylistItemData.VideoID)
	if videoID == "" {
		videoID = strings.TrimSpace(item.Overlay.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer.PlayNavigationEndpoint.WatchEndpoint.VideoID)
	}
	if videoID == "" && len(item.FlexColumns) > 0 {
		for _, run := range item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer.Text.Runs {
			if id := strings.TrimSpace(run.NavigationEndpoint.WatchEndpoint.VideoID); id != "" {
				videoID = id
				break
			}
		}
	}
	if videoID == "" {
		return model.Video{}, false
	}

	var title string
	if len(item.FlexColumns) > 0 {
		title, _ = collectRunsTextAndChannelID(item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer.Text.Runs)
	}

	var colTexts []string
	var channelID string
	for _, col := range item.FlexColumns[min(1, len(item.FlexColumns)):] {
		txt, cid := collectRunsTextAndChannelID(col.MusicResponsiveListItemFlexColumnRenderer.Text.Runs)
		if txt != "" {
			colTexts = append(colTexts, txt)
		}
		if channelID == "" && cid != "" {
			channelID = cid
		}
	}
	for _, col := range item.FixedColumns {
		txt, cid := collectRunsTextAndChannelID(col.MusicResponsiveListItemFixedColumnRenderer.Text.Runs)
		if txt != "" {
			colTexts = append(colTexts, txt)
		}
		if channelID == "" && cid != "" {
			channelID = cid
		}
	}

	channel, views, duration := parseYTMusicMetadata(colTexts)
	if channel == "" {
		channel = "YouTube"
	}

	return normalizeVideo(model.Video{
		ID:        videoID,
		Title:     title,
		Channel:   channel,
		ChannelID: channelID,
		Duration:  duration,
		Views:     views,
		Thumbnail: pickBestYTMusicThumbnail(item.Thumbnail, videoID),
	}), true
}

func parseTwoRowMusicItem(raw json.RawMessage) (model.Video, bool) {
	var item ytMusicTwoRowItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return model.Video{}, false
	}

	videoID := strings.TrimSpace(item.NavigationEndpoint.WatchEndpoint.VideoID)
	if videoID == "" {
		videoID = strings.TrimSpace(item.ThumbnailOverlay.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer.PlayNavigationEndpoint.WatchEndpoint.VideoID)
	}
	if videoID == "" {
		return model.Video{}, false
	}

	title, _ := collectRunsTextAndChannelID(item.Title.Runs)
	subText, channelID := collectRunsTextAndChannelID(item.Subtitle.Runs)

	var colTexts []string
	if subText != "" {
		colTexts = []string{subText}
	}
	channel, views, duration := parseYTMusicMetadata(colTexts)
	if channel == "" {
		channel = "YouTube"
	}

	return normalizeVideo(model.Video{
		ID:        videoID,
		Title:     title,
		Channel:   channel,
		ChannelID: channelID,
		Duration:  duration,
		Views:     views,
		Thumbnail: pickBestYTMusicThumbnail(item.ThumbnailRenderer, videoID),
	}), true
}

func collectRunsTextAndChannelID(runs []ytMusicRun) (text string, channelID string) {
	if len(runs) == 0 {
		return "", ""
	}
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.Text)
		if channelID == "" {
			bid := strings.TrimSpace(run.NavigationEndpoint.BrowseEndpoint.BrowseID)
			if strings.HasPrefix(bid, "UC") {
				channelID = bid
			}
		}
	}
	return strings.TrimSpace(b.String()), channelID
}

func parseYTMusicMetadata(columns []string) (channel, views, duration string) {
	for _, col := range columns {
		segments := splitYTMusicSegments(col)
		for _, seg := range segments {
			if isYTMusicNoiseLabel(seg) {
				continue
			}
			if ytMusicDurationRe.MatchString(seg) {
				if duration == "" {
					duration = seg
				}
				continue
			}
			if isYTMusicViewsSegment(seg) {
				if views == "" {
					views = seg
				}
				continue
			}
			if channel == "" {
				channel = seg
			}
		}
	}
	return channel, views, duration
}

func splitYTMusicSegments(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '•' || r == '·'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func isYTMusicNoiseLabel(seg string) bool {
	switch strings.ToLower(strings.TrimSpace(seg)) {
	case "song", "video", "canción", "cancion", "vídeo", "single", "album", "álbum", "ep":
		return true
	default:
		return false
	}
}

func isYTMusicViewsSegment(seg string) bool {
	lower := strings.ToLower(seg)
	return containsAny(lower, "plays", "reproducciones", "views", "vistas", "visualizaciones")
}

func pickBestYTMusicThumbnail(thumbs ytMusicThumbnailList, videoID string) string {
	bestURL := ""
	bestWidth := -1
	for _, th := range thumbs.MusicThumbnailRenderer.Thumbnail.Thumbnails {
		u := strings.TrimSpace(th.URL)
		if u == "" {
			continue
		}
		if th.Width > bestWidth || (th.Width == bestWidth && bestURL == "") {
			bestWidth = th.Width
			bestURL = u
		}
	}
	if bestURL != "" {
		return bestURL
	}
	return "https://i.ytimg.com/vi/" + videoID + "/hqdefault.jpg"
}
