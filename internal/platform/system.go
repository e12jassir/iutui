package platform

import (
	"context"
	"errors"
)

// ErrUnsupported indicates that a system integration is not supported on the current OS.
var ErrUnsupported = errors.ErrUnsupported

// System defines OS-level desktop integration operations.
type System interface {
	OpenURL(ctx context.Context, url string) error
	CopyText(ctx context.Context, text string) error
	Notify(ctx context.Context, title, body string) error
}
