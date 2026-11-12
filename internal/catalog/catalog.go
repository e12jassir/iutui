package catalog

import (
	"context"

	"github.com/e12jassir/ytx/internal/model"
)

// Catalog provides access to video feeds and search results.
type Catalog interface {
	Feed(ctx context.Context, tab model.Tab) ([]model.Video, error)
	Search(ctx context.Context, query string) ([]model.Video, error)
}
