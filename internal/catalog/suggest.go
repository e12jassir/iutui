package catalog

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultSuggestURL     = "https://suggestqueries.google.com/complete/search?client=firefox&ds=yt&q="
	defaultSuggestTimeout = 2 * time.Second
	maxSuggestResults     = 6
	maxSuggestCacheSize   = 64
	maxSuggestBodyBytes   = 1 << 20
)

var defaultSuggestHTTPClient = &http.Client{
	Timeout: defaultSuggestTimeout,
}

type suggestCacheEntry struct {
	key         string
	suggestions []string
}

// SuggestClient fetches YouTube search autocomplete suggestions using the
// public Firefox-format suggest endpoint with a bounded in-memory LRU cache.
type SuggestClient struct {
	BaseURL    string
	HTTPClient *http.Client

	mu    sync.Mutex
	cache map[string]*list.Element
	order *list.List
}

// NewSuggestClient returns a SuggestClient configured with the default YouTube
// suggest endpoint, a 2-second HTTP timeout, and an initialized bounded cache.
func NewSuggestClient() *SuggestClient {
	return &SuggestClient{
		BaseURL:    defaultSuggestURL,
		HTTPClient: &http.Client{Timeout: defaultSuggestTimeout},
		cache:      make(map[string]*list.Element, maxSuggestCacheSize),
		order:      list.New(),
	}
}

// Suggest returns up to 6 deduplicated autocomplete suggestions for query.
func (c *SuggestClient) Suggest(ctx context.Context, query string) ([]string, error) {
	q := strings.TrimSpace(query)
	if c == nil || q == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if cached, ok := c.getCached(q); ok {
		return cached, nil
	}

	if deadline, hasDeadline := ctx.Deadline(); !hasDeadline || time.Until(deadline) > defaultSuggestTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSuggestTimeout)
		defer cancel()
	}

	endpoint := buildSuggestURL(c.BaseURL, q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: build suggest request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = defaultSuggestHTTPClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog: suggest request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("catalog: suggest HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSuggestBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("catalog: read suggest response: %w", err)
	}

	suggestions, err := parseSuggestResponse(q, body)
	if err != nil {
		return nil, err
	}

	c.putCached(q, suggestions)
	return suggestions, nil
}

func (c *SuggestClient) ensureCacheLocked() {
	if c.cache == nil {
		c.cache = make(map[string]*list.Element, maxSuggestCacheSize)
	}
	if c.order == nil {
		c.order = list.New()
	}
}

func (c *SuggestClient) getCached(key string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil || c.order == nil {
		return nil, false
	}
	elem, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(elem)
	entry := elem.Value.(*suggestCacheEntry)
	return cloneStrings(entry.suggestions), true
}

func (c *SuggestClient) putCached(key string, suggestions []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureCacheLocked()
	cloned := cloneStrings(suggestions)

	if elem, ok := c.cache[key]; ok {
		entry := elem.Value.(*suggestCacheEntry)
		entry.suggestions = cloned
		c.order.MoveToFront(elem)
		return
	}

	for c.order.Len() >= maxSuggestCacheSize {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*suggestCacheEntry)
		delete(c.cache, entry.key)
		c.order.Remove(oldest)
	}

	elem := c.order.PushFront(&suggestCacheEntry{
		key:         key,
		suggestions: cloned,
	})
	c.cache[key] = elem
}

func (c *SuggestClient) cacheLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cache)
}

func buildSuggestURL(baseURL, query string) string {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = defaultSuggestURL
	}
	escaped := url.QueryEscape(query)
	if strings.Contains(base, "{query}") {
		return strings.ReplaceAll(base, "{query}", escaped)
	}
	if strings.HasSuffix(base, "=") {
		return base + escaped
	}
	if strings.HasSuffix(base, "?") || strings.HasSuffix(base, "&") {
		return base + "q=" + escaped
	}
	if strings.Contains(base, "?") {
		return base + "&q=" + escaped
	}
	return base + "?q=" + escaped
}

func parseSuggestResponse(rawQuery string, body []byte) ([]string, error) {
	trimmedBody := bytes.TrimPrefix(bytes.TrimSpace(body), []byte("\xef\xbb\xbf"))
	var tuple []any
	if err := json.Unmarshal(trimmedBody, &tuple); err != nil {
		return nil, fmt.Errorf("catalog: decode suggest response: %w", err)
	}
	if len(tuple) < 2 {
		return nil, fmt.Errorf("catalog: unexpected suggest response length %d", len(tuple))
	}
	if tuple[1] == nil {
		return nil, nil
	}

	rawItems, ok := tuple[1].([]any)
	if !ok {
		return nil, fmt.Errorf("catalog: unexpected suggest items type %T", tuple[1])
	}

	seen := make(map[string]struct{}, maxSuggestResults)
	out := make([]string, 0, maxSuggestResults)
	for _, item := range rawItems {
		rawStr, ok := item.(string)
		if !ok {
			continue
		}
		s := strings.TrimSpace(rawStr)
		if s == "" || strings.EqualFold(s, rawQuery) {
			continue
		}
		norm := strings.ToLower(s)
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, s)
		if len(out) >= maxSuggestResults {
			break
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
