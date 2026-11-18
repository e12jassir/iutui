package store

import (
	"context"

	"github.com/e12jassir/iutui/internal/model"
)

// Store persists user watch-later, playback history, and subscription state.
type Store interface {
	Saved(ctx context.Context) ([]model.Video, error)
	ToggleSaved(ctx context.Context, v model.Video) (bool, error)
	History(ctx context.Context) ([]model.Video, error)
	RecordHistory(ctx context.Context, v model.Video) error
	Subscriptions(ctx context.Context) ([]string, error)
}
