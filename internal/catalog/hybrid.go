package catalog

import (
	"context"

	"github.com/e12jassir/ytx/internal/model"
)

// HybridCatalog combines BashCatalog for fast local/cached feeds (Home, Music,
// Subscriptions, History, Downloads) with YTDLPCatalog for direct fast search.
type HybridCatalog struct {
	Feeds  *BashCatalog
	Direct *YTDLPCatalog
}

// NewHybridCatalog creates a HybridCatalog pairing local TSV feeds with direct yt-dlp search.
func NewHybridCatalog(feeds *BashCatalog, direct *YTDLPCatalog) *HybridCatalog {
	return &HybridCatalog{
		Feeds:  feeds,
		Direct: direct,
	}
}

// Feed returns the feed for tab via BashCatalog (local cache first).
func (h *HybridCatalog) Feed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if h.Feeds != nil {
		return h.Feeds.Feed(ctx, tab)
	}
	if h.Direct != nil {
		return h.Direct.Feed(ctx, tab)
	}
	return nil, nil
}

// RefreshFeed bypasses cached TSV files and fetches fresh results for tab.
func (h *HybridCatalog) RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if h.Feeds != nil {
		return h.Feeds.RefreshFeed(ctx, tab)
	}
	if h.Direct != nil {
		return h.Direct.Feed(ctx, tab)
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
