package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/e12jassir/iutui/internal/model"
)

// FakeStore is an in-memory Store implementation for deterministic tests.
type FakeStore struct {
	mu            sync.Mutex
	SavedVideos   []model.Video
	HistoryVideos []model.Video
	Subs          []string
	Err           error
}

// NewFakeStore returns an empty FakeStore.
func NewFakeStore() *FakeStore {
	return &FakeStore{}
}

// Saved returns a copy of SavedVideos.
func (f *FakeStore) Saved(ctx context.Context) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.SavedVideos), nil
}

// ToggleSaved adds or removes v from SavedVideos and reports whether it is now saved.
func (f *FakeStore) ToggleSaved(ctx context.Context, v model.Video) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return false, f.Err
	}

	for i, item := range f.SavedVideos {
		if item.ID == v.ID {
			f.SavedVideos = append(f.SavedVideos[:i], f.SavedVideos[i+1:]...)
			return false, nil
		}
	}
	f.SavedVideos = append(f.SavedVideos, v)
	return true, nil
}

// History returns a copy of HistoryVideos.
func (f *FakeStore) History(ctx context.Context) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.HistoryVideos), nil
}

// RecordHistory prepends v to HistoryVideos and deduplicates any prior entry.
func (f *FakeStore) RecordHistory(ctx context.Context, v model.Video) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}

	updated := make([]model.Video, 0, len(f.HistoryVideos)+1)
	updated = append(updated, v)
	for _, item := range f.HistoryVideos {
		if item.ID != v.ID {
			updated = append(updated, item)
		}
	}
	f.HistoryVideos = updated
	return nil
}

// Subscriptions returns a copy of Subs.
func (f *FakeStore) Subscriptions(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.Subs), nil
}

// ToggleSubscription adds or removes channelRef from Subs case-insensitively and reports
// whether it is now subscribed.
func (f *FakeStore) ToggleSubscription(ctx context.Context, channelRef string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ref := strings.TrimSpace(channelRef)
	if ref == "" {
		return false, errors.New("channel reference is required")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return false, f.Err
	}

	for i, existing := range f.Subs {
		if strings.EqualFold(strings.TrimSpace(existing), ref) {
			f.Subs = append(f.Subs[:i], f.Subs[i+1:]...)
			return false, nil
		}
	}
	f.Subs = append(f.Subs, ref)
	return true, nil
}
