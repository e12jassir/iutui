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
	"sync"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const (
	youtubeFeedBase         = "https://www.youtube.com/feeds/videos.xml?channel_id="
	maxRSSFetchWorkers      = 6
	maxSubscriptionRSSItems = 100
)

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
	XMLName   xml.Name    `xml:"feed"`
	Title     string      `xml:"title"`
	ChannelID string      `xml:"channelId"`
	Entries   []atomEntry `xml:"entry"`
}

type atomEntry struct {
	VideoID   string         `xml:"videoId"`
	ChannelID string         `xml:"channelId"`
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

	feedChannelID := strings.TrimSpace(feed.ChannelID)
	videos := make([]model.Video, 0, len(feed.Entries))
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

		channelID := strings.TrimSpace(entry.ChannelID)
		if channelID == "" {
			channelID = feedChannelID
		}

		v := normalizeVideo(model.Video{
			ID:        id,
			Title:     strings.TrimSpace(entry.Title),
			Channel:   channel,
			ChannelID: channelID,
			Views:     strings.TrimSpace(entry.Media.Community.Statistics.Views),
			Published: strings.TrimSpace(entry.Published),
			Thumbnail: strings.TrimSpace(entry.Media.Thumbnail.URL),
		})
		videos = append(videos, v)
	}

	sortVideosNewestFirst(videos)
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

// FetchSubscriptionsRSS resolves channelRefs into YouTube Atom feed URLs, fetches them
// concurrently using a bounded worker pool, merges and deduplicates entries by ID,
// and returns up to 100 videos sorted newest-first. Individual feed failures are skipped
// when at least one feed succeeds; if no channelRefs resolve to an RSS URL, an empty
// slice is returned without error.
func FetchSubscriptionsRSS(ctx context.Context, client *http.Client, channelRefs []string) ([]model.Video, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	seenURLs := make(map[string]struct{}, len(channelRefs))
	feedURLs := make([]string, 0, len(channelRefs))
	for _, ref := range channelRefs {
		feedURL := ChannelFeedURL(ref)
		if feedURL == "" {
			continue
		}
		if _, dup := seenURLs[feedURL]; dup {
			continue
		}
		seenURLs[feedURL] = struct{}{}
		feedURLs = append(feedURLs, feedURL)
	}
	if len(feedURLs) == 0 {
		return []model.Video{}, nil
	}

	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	type rssJob struct {
		idx int
		url string
	}
	type rssResult struct {
		videos []model.Video
		err    error
	}

	workers := min(maxRSSFetchWorkers, len(feedURLs))
	jobs := make(chan rssJob, len(feedURLs))
	for i, feedURL := range feedURLs {
		jobs <- rssJob{idx: i, url: feedURL}
	}
	close(jobs)

	results := make([]rssResult, len(feedURLs))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				videos, err := FetchRSSFeed(ctx, client, job.url)
				if err == nil {
					if cid := channelIDFromFeedURL(job.url); cid != "" {
						for j := range videos {
							if videos[j].ChannelID == "" {
								videos[j].ChannelID = cid
							}
						}
					}
				}
				results[job.idx] = rssResult{videos: videos, err: err}
			}
		}()
	}
	wg.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	var (
		successCount int
		firstErr     error
		merged       []model.Video
	)
	for _, res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		successCount++
		merged = append(merged, res.videos...)
	}

	if successCount == 0 {
		return nil, firstErr
	}

	sortVideosNewestFirst(merged)

	deduped := make([]model.Video, 0, min(len(merged), maxSubscriptionRSSItems))
	seenIDs := make(map[string]int, len(merged))
	for _, v := range merged {
		if idx, dup := seenIDs[v.ID]; dup {
			mergeVideoFields(&deduped[idx], v)
			continue
		}
		seenIDs[v.ID] = len(deduped)
		deduped = append(deduped, v)
		if len(deduped) >= maxSubscriptionRSSItems {
			break
		}
	}
	return deduped, nil
}

func mergeVideoFields(dst *model.Video, src model.Video) {
	if dst.Channel == "" && src.Channel != "" {
		dst.Channel = src.Channel
	}
	if dst.ChannelID == "" && src.ChannelID != "" {
		dst.ChannelID = src.ChannelID
	}
	if dst.Duration == "" && src.Duration != "" {
		dst.Duration = src.Duration
	}
	if dst.Views == "" && src.Views != "" {
		dst.Views = src.Views
	}
	if dst.Published == "" && src.Published != "" {
		dst.Published = src.Published
	}
	if dst.Thumbnail == "" && src.Thumbnail != "" {
		dst.Thumbnail = src.Thumbnail
	}
}

func channelIDFromFeedURL(feedURL string) string {
	u, err := url.Parse(feedURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("channel_id"))
}

func parsePublishedTime(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func sortVideosNewestFirst(videos []model.Video) {
	if len(videos) < 2 {
		return
	}
	type indexedTime struct {
		pub  time.Time
		hasT bool
	}
	times := make([]indexedTime, len(videos))
	for i := range videos {
		times[i].pub, times[i].hasT = parsePublishedTime(videos[i].Published)
	}
	order := make([]int, len(videos))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		li, lj := order[i], order[j]
		ti, tj := times[li], times[lj]
		if ti.hasT && tj.hasT {
			return ti.pub.After(tj.pub)
		}
		if ti.hasT != tj.hasT {
			return ti.hasT
		}
		return videos[li].Published > videos[lj].Published
	})
	sorted := make([]model.Video, len(videos))
	for i, idx := range order {
		sorted[i] = videos[idx]
	}
	copy(videos, sorted)
}
