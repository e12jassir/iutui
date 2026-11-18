package player

import (
	"context"

	"github.com/e12jassir/iutui/internal/model"
)

// Engine defines the playback control and status observation interface for a media player.
type Engine interface {
	Play(ctx context.Context, v model.Video, mode string) error
	Enqueue(ctx context.Context, v model.Video) error
	TogglePause(ctx context.Context) error
	Next(ctx context.Context) error
	Prev(ctx context.Context) error
	Seek(ctx context.Context, seconds int) error
	AddVolume(ctx context.Context, delta float64) error
	Status() Status
	Subscribe(ctx context.Context) <-chan Status
	Close() error
}
