package catalog

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/e12jassir/iutui/internal/model"
)

// HybridCatalog prioritizes native Go YTDLPCatalog for remote feeds and search
// (with stale-while-revalidate JSON caching) and falls back to BashCatalog for
// local-only tabs (History, Downloads), legacy TSV caches, or yt-dlp errors.
// When SubscriptionsProvider is set, TabSubscriptions falls back to (or runs in
// guest mode via) concurrent YouTube Atom RSS feeds without requiring cookies.
type HybridCatalog struct {
	Feeds                 *BashCatalog
	Direct                *YTDLPCatalog
	SubscriptionsProvider func(context.Context) ([]string, error)
	RSSClient             *http.Client
	DisableCookieAuthSubs bool

	cfgMu sync.RWMutex
}

// SetDisableCookieAuthSubs updates whether cookie-authenticated subscriptions
// fetching is bypassed in favor of guest Atom RSS feeds.
func (h *HybridCatalog) SetDisableCookieAuthSubs(disable bool) {
	if h == nil {
		return
	}
	h.cfgMu.Lock()
	h.DisableCookieAuthSubs = disable
	h.cfgMu.Unlock()
}

func (h *HybridCatalog) disableCookieAuthSubs() bool {
	if h == nil {
		return false
	}
	h.cfgMu.RLock()
	d := h.DisableCookieAuthSubs
	h.cfgMu.RUnlock()
	return d
}

// NewHybridCatalog creates a HybridCatalog pairing native Go yt-dlp catalog access
// with BashCatalog fallback for local tabs and legacy TSV caches.
func NewHybridCatalog(feeds *BashCatalog, direct *YTDLPCatalog) *HybridCatalog {
	return &HybridCatalog{
		Feeds:  feeds,
		Direct: direct,
	}
}

// Feed loads remote tabs (Home, Music, Subscriptions) via native Go YTDLPCatalog
// first and falls back to guest RSS subscriptions (when SubscriptionsProvider is
// configured) or BashCatalog on error, empty result, or local-only tabs.
func (h *HybridCatalog) Feed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	return h.loadFeed(ctx, tab, false)
}

// RefreshFeed bypasses caches and fetches fresh results via YTDLPCatalog first,
// falling back to guest RSS subscriptions (when SubscriptionsProvider is configured)
// or BashCatalog on error, empty result, or local-only tabs.
func (h *HybridCatalog) RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	return h.loadFeed(ctx, tab, true)
}

func (h *HybridCatalog) loadFeed(ctx context.Context, tab model.Tab, refresh bool) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h == nil {
		return nil, nil
	}

	if tab == model.TabSubscriptions && h.SubscriptionsProvider != nil {
		return h.subscriptionsFeed(ctx, refresh)
	}

	var directErr error
	_, _, directSupported := ytdlpTabTarget(tab)
	if directSupported && h.Direct != nil {
		videos, err := h.directFeed(ctx, tab, refresh)
		if err == nil && len(videos) > 0 {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		directErr = err
	}

	return h.fallbackBashFeed(ctx, tab, refresh, directErr)
}

func (h *HybridCatalog) subscriptionsFeed(ctx context.Context, refresh bool) ([]model.Video, error) {
	var directErr error
	if !h.disableCookieAuthSubs() && h.Direct != nil && hasValidCookieFile(h.Direct.cookieJarPath()) {
		videos, err := h.directFeed(ctx, model.TabSubscriptions, refresh)
		if err == nil && len(videos) > 0 {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		directErr = err
	}

	refs, subErr := h.SubscriptionsProvider(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if subErr == nil {
		videos, rssErr := FetchSubscriptionsRSS(ctx, h.RSSClient, refs)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if rssErr == nil && len(videos) > 0 {
			if h.Direct != nil {
				h.Direct.applyCachedMeta(videos)
			}
			return videos, nil
		}
		if rssErr != nil {
			subErr = rssErr
		}
	}

	primaryErr := subErr
	if primaryErr == nil {
		primaryErr = directErr
	}
	videos, err := h.fallbackBashFeed(ctx, model.TabSubscriptions, refresh, primaryErr)
	if err != nil && primaryErr == nil && ctx.Err() == nil {
		// SubscriptionsProvider + RSS succeeded with zero items; avoid surfacing missing legacy bash binary errors.
		return []model.Video{}, nil
	}
	return videos, err
}

func (h *HybridCatalog) directFeed(ctx context.Context, tab model.Tab, refresh bool) ([]model.Video, error) {
	if refresh {
		return h.Direct.RefreshFeed(ctx, tab)
	}
	return h.Direct.Feed(ctx, tab)
}

func (h *HybridCatalog) fallbackBashFeed(ctx context.Context, tab model.Tab, refresh bool, primaryErr error) ([]model.Video, error) {
	if h.Feeds != nil {
		var (
			videos []model.Video
			err    error
		)
		if refresh {
			videos, err = h.Feeds.RefreshFeed(ctx, tab)
		} else {
			videos, err = h.Feeds.Feed(ctx, tab)
		}
		if err == nil {
			if h.Direct != nil {
				h.Direct.applyCachedMeta(videos)
			}
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if primaryErr != nil {
			return nil, primaryErr
		}
		return nil, err
	}

	if primaryErr != nil {
		return nil, primaryErr
	}
	return nil, nil
}

func hasValidCookieFile(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Search queries YouTube directly via yt-dlp (~2s) and falls back to BashCatalog only if it yields results.
func (h *HybridCatalog) Search(ctx context.Context, query string) ([]model.Video, error) {
	var directErr error
	if h.Direct != nil {
		videos, err := h.Direct.Search(ctx, query)
		if err == nil {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		directErr = err
	}
	if h.Feeds != nil {
		videos, err := h.Feeds.Search(ctx, query)
		if err == nil && len(videos) > 0 {
			if h.Direct != nil {
				h.Direct.applyCachedMeta(videos)
			}
			return videos, nil
		}
		if directErr != nil {
			return nil, directErr
		}
		return videos, err
	}
	return nil, directErr
}

// RefreshSearch bypasses Direct's search cache and performs a fresh lookup.
func (h *HybridCatalog) RefreshSearch(ctx context.Context, query string) ([]model.Video, error) {
	if h.Direct != nil {
		videos, err := h.Direct.RefreshSearch(ctx, query)
		if err == nil {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return h.Search(ctx, query)
}

// EnrichMetadata delegates per-video view/date enrichment to the underlying Direct catalog.
func (h *HybridCatalog) EnrichMetadata(ctx context.Context, ids []string) (map[string]VideoMeta, error) {
	if h == nil || h.Direct == nil {
		return nil, nil
	}
	return h.Direct.EnrichMetadata(ctx, ids)
}
