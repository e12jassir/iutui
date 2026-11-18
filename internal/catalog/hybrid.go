package catalog

import (
	"context"

	"github.com/e12jassir/iutui/internal/model"
)

// HybridCatalog prioritizes native Go YTDLPCatalog for remote feeds and search
// (with stale-while-revalidate JSON caching) and falls back to BashCatalog for
// local-only tabs (History, Downloads), legacy TSV caches, or yt-dlp errors.
type HybridCatalog struct {
	Feeds  *BashCatalog
	Direct *YTDLPCatalog
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
// first and falls back to BashCatalog on error, empty result, or local-only tabs.
func (h *HybridCatalog) Feed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var directErr error
	_, _, directSupported := ytdlpTabTarget(tab)
	if directSupported && h.Direct != nil {
		videos, err := h.Direct.Feed(ctx, tab)
		if err == nil && len(videos) > 0 {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		directErr = err
	}

	if h.Feeds != nil {
		videos, err := h.Feeds.Feed(ctx, tab)
		if err == nil {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if directErr != nil {
			return nil, directErr
		}
		return nil, err
	}

	if directErr != nil {
		return nil, directErr
	}
	return nil, nil
}

// RefreshFeed bypasses caches and fetches fresh results via YTDLPCatalog first,
// falling back to BashCatalog on error, empty result, or local-only tabs.
func (h *HybridCatalog) RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var directErr error
	_, _, directSupported := ytdlpTabTarget(tab)
	if directSupported && h.Direct != nil {
		videos, err := h.Direct.RefreshFeed(ctx, tab)
		if err == nil && len(videos) > 0 {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		directErr = err
	}

	if h.Feeds != nil {
		videos, err := h.Feeds.RefreshFeed(ctx, tab)
		if err == nil {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if directErr != nil {
			return nil, directErr
		}
		return nil, err
	}

	if directErr != nil {
		return nil, directErr
	}
	return nil, nil
}

// Search queries YouTube directly via yt-dlp (~2s) and falls back to BashCatalog on error.
func (h *HybridCatalog) Search(ctx context.Context, query string) ([]model.Video, error) {
	if h.Direct != nil {
		videos, err := h.Direct.Search(ctx, query)
		if err == nil {
			return videos, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if h.Feeds != nil {
		return h.Feeds.Search(ctx, query)
	}
	return nil, nil
}
