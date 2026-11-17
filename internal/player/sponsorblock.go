package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultSponsorBlockURL     = "https://sponsor.ajay.app"
	defaultSponsorBlockTimeout = 3 * time.Second
	sponsorBlockMaxBodyBytes   = 1 << 20 // 1 MiB safety limit
)

var fallbackSponsorHTTPClient = &http.Client{Timeout: defaultSponsorBlockTimeout}

// Segment represents a skippable time range returned by SponsorBlock.
type Segment struct {
	Start    float64
	End      float64
	Category string
	UUID     string
}

// SponsorBlockClient fetches and caches SponsorBlock skip segments per video ID.
type SponsorBlockClient struct {
	BaseURL    string
	HTTPClient *http.Client

	mu    sync.RWMutex
	cache map[string][]Segment
}

// NewSponsorBlockClient constructs a SponsorBlockClient with default endpoint and 3s HTTP timeout.
func NewSponsorBlockClient() *SponsorBlockClient {
	return &SponsorBlockClient{
		BaseURL:    defaultSponsorBlockURL,
		HTTPClient: &http.Client{Timeout: defaultSponsorBlockTimeout},
		cache:      make(map[string][]Segment),
	}
}

type sponsorBlockAPIEntry struct {
	Segment  []float64 `json:"segment"`
	Category string    `json:"category"`
	UUID     string    `json:"UUID"`
}

// FetchSegments returns SponsorBlock skip segments (sponsor, selfpromo) for videoID,
// serving cached hits and caching 404 negative results as an empty slice.
func (c *SponsorBlockClient) FetchSegments(ctx context.Context, videoID string) ([]Segment, error) {
	if c == nil {
		return nil, errors.New("sponsorblock client is nil")
	}
	id := strings.TrimSpace(videoID)
	if id == "" {
		return nil, errors.New("sponsorblock: videoID must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.RLock()
	cached, ok := c.cache[id]
	baseURL := c.BaseURL
	httpClient := c.HTTPClient
	c.mu.RUnlock()
	if ok {
		if len(cached) == 0 {
			return nil, nil
		}
		return slices.Clone(cached), nil
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultSponsorBlockURL
	}
	if httpClient == nil {
		httpClient = fallbackSponsorHTTPClient
	}

	endpoint := baseURL + "/api/skipSegments?videoID=" + url.QueryEscape(id) + "&categories=%5B%22sponsor%22%2C%22selfpromo%22%5D"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("sponsorblock: build request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sponsorblock: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		c.setCache(id, []Segment{})
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sponsorblock: unexpected HTTP %d", resp.StatusCode)
	}

	var entries []sponsorBlockAPIEntry
	dec := json.NewDecoder(io.LimitReader(resp.Body, sponsorBlockMaxBodyBytes))
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("sponsorblock: decode response: %w", err)
	}

	segments := make([]Segment, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Segment) < 2 {
			continue
		}
		start, end := entry.Segment[0], entry.Segment[1]
		if start < 0 || end <= start {
			continue
		}
		segments = append(segments, Segment{
			Start:    start,
			End:      end,
			Category: entry.Category,
			UUID:     entry.UUID,
		})
	}

	c.setCache(id, segments)
	if len(segments) == 0 {
		return nil, nil
	}
	return slices.Clone(segments), nil
}

func (c *SponsorBlockClient) setCache(videoID string, segments []Segment) {
	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string][]Segment)
	}
	c.cache[videoID] = segments
	c.mu.Unlock()
}
