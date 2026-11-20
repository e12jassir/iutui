package lyrics

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultYouTubePlayerURL = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
	youtubeAndroidUserAgent = "com.google.android.youtube/20.10.38 (Linux; U; Android 14) gzip"
)

type ytCaptionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"`
	Name         struct {
		SimpleText string `json:"simpleText"`
	} `json:"name"`
}

type ytPlayerRequest struct {
	Context ytPlayerContext `json:"context"`
	VideoID string          `json:"videoId"`
}

type ytPlayerContext struct {
	Client ytPlayerClient `json:"client"`
}

type ytPlayerClient struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
	HL            string `json:"hl"`
	GL            string `json:"gl"`
}

type ytPlayerResponse struct {
	VideoDetails struct {
		Title  string `json:"title"`
		Author string `json:"author"`
	} `json:"videoDetails"`
	Captions struct {
		PlayerCaptionsTracklistRenderer struct {
			CaptionTracks []ytCaptionTrack `json:"captionTracks"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
}

type ytJSON3Response struct {
	Events []struct {
		TStartMs int64 `json:"tStartMs"`
		Segs     []struct {
			UTF8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

func isValidYouTubeVideoID(id string) bool {
	if len(id) != 11 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// FetchYouTubeCaptions fetches timed captions from YouTube's InnerTube player API
// and downloads the highest-ranked caption track in json3 (with XML fallback).
func FetchYouTubeCaptions(ctx context.Context, httpClient *http.Client, endpointURL, videoID, prefLang string) (TrackLyrics, error) {
	if !isValidYouTubeVideoID(videoID) {
		return TrackLyrics{}, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TrackLyrics{}, err
	}
	if httpClient == nil {
		httpClient = defaultHTTPClient
	}
	endpoint := strings.TrimSpace(endpointURL)
	if endpoint == "" {
		endpoint = defaultYouTubePlayerURL
	}

	reqBody, err := json.Marshal(ytPlayerRequest{
		Context: ytPlayerContext{
			Client: ytPlayerClient{
				ClientName:    "ANDROID",
				ClientVersion: "20.10.38",
				HL:            "es",
				GL:            "CO",
			},
		},
		VideoID: videoID,
	})
	if err != nil {
		return TrackLyrics{}, fmt.Errorf("lyrics: marshal youtube request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return TrackLyrics{}, fmt.Errorf("lyrics: build youtube request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", youtubeAndroidUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, ctxErr
		}
		return TrackLyrics{}, fmt.Errorf("lyrics: youtube player request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return TrackLyrics{}, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return TrackLyrics{}, fmt.Errorf("lyrics: youtube player unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, ctxErr
		}
		return TrackLyrics{}, fmt.Errorf("lyrics: read youtube player response: %w", err)
	}

	var playerResp ytPlayerResponse
	if err := json.Unmarshal(body, &playerResp); err != nil {
		return TrackLyrics{}, fmt.Errorf("lyrics: decode youtube player response: %w", err)
	}

	tracks := playerResp.Captions.PlayerCaptionsTracklistRenderer.CaptionTracks
	if len(tracks) == 0 {
		return TrackLyrics{}, ErrNotFound
	}

	track, ok := selectCaptionTrack(tracks, prefLang)
	if !ok || strings.TrimSpace(track.BaseURL) == "" {
		return TrackLyrics{}, ErrNotFound
	}

	captionURL, err := url.Parse(strings.TrimSpace(track.BaseURL))
	if err != nil {
		return TrackLyrics{}, fmt.Errorf("lyrics: invalid caption url: %w", err)
	}
	q := captionURL.Query()
	q.Set("fmt", "json3")
	captionURL.RawQuery = q.Encode()

	capReq, err := http.NewRequestWithContext(ctx, http.MethodGet, captionURL.String(), nil)
	if err != nil {
		return TrackLyrics{}, fmt.Errorf("lyrics: build caption request: %w", err)
	}
	capReq.Header.Set("User-Agent", youtubeAndroidUserAgent)

	capResp, err := httpClient.Do(capReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, ctxErr
		}
		return TrackLyrics{}, fmt.Errorf("lyrics: youtube caption request failed: %w", err)
	}
	defer capResp.Body.Close()

	if capResp.StatusCode == http.StatusNotFound {
		return TrackLyrics{}, ErrNotFound
	}
	if capResp.StatusCode != http.StatusOK {
		return TrackLyrics{}, fmt.Errorf("lyrics: youtube caption unexpected status %d", capResp.StatusCode)
	}

	capBody, err := io.ReadAll(io.LimitReader(capResp.Body, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrackLyrics{}, ctxErr
		}
		return TrackLyrics{}, fmt.Errorf("lyrics: read youtube caption response: %w", err)
	}

	lines := ParseYouTubeJSON3(capBody)
	if len(lines) == 0 && bytes.Contains(bytes.TrimSpace(capBody), []byte("<")) {
		lines = ParseYouTubeTimedTextXML(capBody)
	}
	if len(lines) == 0 {
		return TrackLyrics{}, ErrNotFound
	}

	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = l.Text
	}

	source := SourceYouTubeCC
	if strings.EqualFold(strings.TrimSpace(track.Kind), "asr") {
		source = SourceYouTubeASR
	}

	return TrackLyrics{
		Synced:     lines,
		Plain:      plain,
		TrackName:  strings.TrimSpace(playerResp.VideoDetails.Title),
		ArtistName: strings.TrimSpace(playerResp.VideoDetails.Author),
		Source:     source,
		Language:   strings.TrimSpace(track.LanguageCode),
	}, nil
}

func selectCaptionTrack(tracks []ytCaptionTrack, prefLang string) (ytCaptionTrack, bool) {
	pref := strings.TrimSpace(prefLang)
	if pref == "" {
		pref = "es"
	}

	bestScore := -1
	var best ytCaptionTrack
	found := false

	for _, t := range tracks {
		if strings.TrimSpace(t.BaseURL) == "" {
			continue
		}
		score := scoreCaptionTrack(t, pref)
		if !found || score > bestScore {
			best = t
			bestScore = score
			found = true
		}
	}
	return best, found
}

func scoreCaptionTrack(t ytCaptionTrack, prefLang string) int {
	isASR := strings.EqualFold(strings.TrimSpace(t.Kind), "asr")
	matchesPref := captionLangMatches(t.LanguageCode, prefLang)
	matchesEn := captionLangMatches(t.LanguageCode, "en")

	if !isASR {
		if matchesPref {
			return 100
		}
		if matchesEn {
			return 90
		}
		return 80
	}

	if matchesPref {
		return 60
	}
	if matchesEn {
		return 50
	}
	return 40
}

func captionLangMatches(trackLang, targetLang string) bool {
	tl := strings.ToLower(strings.TrimSpace(trackLang))
	pl := strings.ToLower(strings.TrimSpace(targetLang))
	if tl == "" || pl == "" {
		return false
	}
	return tl == pl || strings.HasPrefix(tl, pl+"-")
}

// ParseYouTubeJSON3 parses YouTube's json3 caption payload into normalized,
// chronologically sorted timestamped lines.
func ParseYouTubeJSON3(raw []byte) []Line {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var doc ytJSON3Response
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	lines := make([]Line, 0, len(doc.Events))
	var sb strings.Builder
	for _, ev := range doc.Events {
		if len(ev.Segs) == 0 {
			continue
		}
		sb.Reset()
		for _, seg := range ev.Segs {
			sb.WriteString(seg.UTF8)
		}
		text := collapseSpaces(html.UnescapeString(sb.String()))
		if text == "" {
			continue
		}
		lines = append(lines, Line{
			Time: float64(ev.TStartMs) / 1000.0,
			Text: text,
		})
	}

	return normalizeCaptionLines(lines)
}

// ParseYouTubeTimedTextXML parses both <timedtext format="3"><body><p t="ms">
// and legacy <transcript><text start="sec"> YouTube caption XML documents.
func ParseYouTubeTimedTextXML(raw []byte) []Line {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	dec := xml.NewDecoder(bytes.NewReader(raw))
	var lines []Line
	var buf strings.Builder
	inCue := false
	cueDepth := 0
	cueTime := 0.0

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if !inCue {
				if ts, ok := parseXMLCueStart(t); ok {
					inCue = true
					cueDepth = 1
					cueTime = ts
					buf.Reset()
				}
				continue
			}
			cueDepth++
			if strings.EqualFold(t.Name.Local, "br") {
				buf.WriteByte(' ')
			}

		case xml.CharData:
			if inCue {
				buf.Write(t)
			}

		case xml.EndElement:
			if !inCue {
				continue
			}
			cueDepth--
			if cueDepth == 0 {
				inCue = false
				text := collapseSpaces(html.UnescapeString(buf.String()))
				if text != "" {
					lines = append(lines, Line{
						Time: cueTime,
						Text: text,
					})
				}
			}
		}
	}

	return normalizeCaptionLines(lines)
}

func parseXMLCueStart(el xml.StartElement) (float64, bool) {
	switch strings.ToLower(el.Name.Local) {
	case "p":
		for _, attr := range el.Attr {
			if strings.EqualFold(attr.Name.Local, "t") {
				ms, err := strconv.ParseFloat(strings.TrimSpace(attr.Value), 64)
				if err == nil && !math.IsNaN(ms) && !math.IsInf(ms, 0) {
					return ms / 1000.0, true
				}
			}
		}
	case "text":
		for _, attr := range el.Attr {
			if strings.EqualFold(attr.Name.Local, "start") {
				sec, err := strconv.ParseFloat(strings.TrimSpace(attr.Value), 64)
				if err == nil && !math.IsNaN(sec) && !math.IsInf(sec, 0) {
					return sec, true
				}
			}
		}
	}
	return 0, false
}

func normalizeCaptionLines(lines []Line) []Line {
	if len(lines) == 0 {
		return nil
	}

	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].Time < lines[j].Time
	})

	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		if len(out) > 0 && out[len(out)-1].Text == l.Text {
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
