package catalog

import (
	"context"
	"slices"
	"strings"

	"github.com/e12jassir/ytx/internal/model"
)

// FakeCatalog is an in-memory Catalog implementation for deterministic tests.
type FakeCatalog struct {
	Feeds   map[model.Tab][]model.Video
	Results map[string][]model.Video
	Err     error
}

// NewFakeCatalog returns an initialized FakeCatalog.
func NewFakeCatalog() *FakeCatalog {
	return &FakeCatalog{
		Feeds:   make(map[model.Tab][]model.Video),
		Results: make(map[string][]model.Video),
	}
}

// Feed returns the configured videos for tab, or Err if set.
func (f *FakeCatalog) Feed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.Feeds[tab]), nil
}

// Search returns deterministic results for query. If Results contains query it
// returns those videos; otherwise if Results is nil it falls back to
// case-insensitive title/channel matching across Feeds.
func (f *FakeCatalog) Search(ctx context.Context, query string) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if f.Results != nil {
		if res, ok := f.Results[query]; ok {
			return slices.Clone(res), nil
		}
		if res, ok := f.Results[q]; ok {
			return slices.Clone(res), nil
		}
		return nil, nil
	}

	lower := strings.ToLower(q)
	var out []model.Video
	for _, list := range f.Feeds {
		for _, v := range list {
			if strings.Contains(strings.ToLower(v.Title), lower) ||
				strings.Contains(strings.ToLower(v.Channel), lower) {
				out = append(out, v)
			}
		}
	}
	return out, nil
}
