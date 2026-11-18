package catalog

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const youtubeFeedBase = "https://www.youtube.com/feeds/videos.xml?channel_id="

// ChannelFeedURL normalizes a YouTube channel ID (e.g. "UC...") or channel URL
// (e.g. "https://www.youtube.com/channel/UC...") into its Atom RSS feed URL.
// It returns an empty string if ref cannot be resolved to a channel ID.
func ChannelFeedURL(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}

	if strings.HasPrefix(ref, "UC") && !strings.ContainsAny(ref, "/?# ") {
		return youtubeFeedBase + ref
	}

	raw := ref
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	if strings.HasSuffix(u.Path, "/feeds/videos.xml") {
		if cid := strings.TrimSpace(u.Query().Get("channel_id")); cid != "" {
			return youtubeFeedBase + cid
		}
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "channel" && strings.TrimSpace(parts[i+1]) != "" {
			return youtubeFeedBase + strings.TrimSpace(parts[i+1])
		}
	}

	return ""
}

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	VideoID   string         `xml:"videoId"`
	Title     string         `xml:"title"`
	Published string         `xml:"published"`
	Author    atomAuthor     `xml:"author"`
	Media     atomMediaGroup `xml:"group"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomMediaGroup struct {
	Community atomMediaCommunity `xml:"community"`
	Thumbnail atomThumbnail      `xml:"thumbnail"`
}

type atomMediaCommunity struct {
	Statistics atomMediaStats `xml:"statistics"`
}

type atomMediaStats struct {
	Views string `xml:"views,attr"`
}

type atomThumbnail struct {
	URL string `xml:"url,attr"`
}

// ParseAtomFeed parses a YouTube Atom XML feed into a slice of model.Video
// sorted newest-first by the published timestamp.
func ParseAtomFeed(xmlData []byte) ([]model.Video, error) {
	if len(strings.TrimSpace(string(xmlData))) == 0 {
		return nil, fmt.Errorf("rss: empty feed data")
	}

	var feed atomFeed
	if err := xml.Unmarshal(xmlData, &feed); err != nil {
		return nil, fmt.Errorf("rss: parse atom feed: %w", err)
	}

	type parsedItem struct {
		video model.Video
		pub   time.Time
		hasT  bool
	}

	items := make([]parsedItem, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		id := strings.TrimSpace(entry.VideoID)
		if id == "" {
			continue
		}

		channel := strings.TrimSpace(entry.Author.Name)
		if channel == "" {
			channel = strings.TrimSpace(feed.Title)
		}
		if channel == "" {
			channel = "YouTube"
		}

		pubStr := strings.TrimSpace(entry.Published)
		pubTime, err := time.Parse(time.RFC3339, pubStr)

		v := normalizeVideo(model.Video{
			ID:        id,
			Title:     strings.TrimSpace(entry.Title),
			Channel:   channel,
			Views:     strings.TrimSpace(entry.Media.Community.Statistics.Views),
			Published: pubStr,
			Thumbnail: strings.TrimSpace(entry.Media.Thumbnail.URL),
		})

		items = append(items, parsedItem{
			video: v,
			pub:   pubTime,
			hasT:  err == nil,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].hasT && items[j].hasT {
			return items[i].pub.After(items[j].pub)
		}
		if items[i].hasT != items[j].hasT {
			return items[i].hasT
		}
		return items[i].video.Published > items[j].video.Published
	})

	videos := make([]model.Video, len(items))
	for i, it := range items {
		videos[i] = it.video
	}
	return videos, nil
}

// FetchRSSFeed retrieves a YouTube Atom feed from feedURL using client and
// parses it into videos. Respects context cancellation and timeouts.
func FetchRSSFeed(ctx context.Context, client *http.Client, feedURL string) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(feedURL) == "" {
		return nil, fmt.Errorf("rss: empty feed URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("rss: build request: %w", err)
	}
	req.Header.Set("Accept", "application/atom+xml, application/xml, text/xml")

	resp, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("rss: fetch %s: %w", feedURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rss: unexpected status %d for %s", resp.StatusCode, feedURL)
	}

	const maxFeedBytes = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("rss: read response: %w", err)
	}

	return ParseAtomFeed(body)
}
