//go:build !linux

package platform

import "context"

type stubSystem struct{}

// NewSystem returns a stub System implementation for unsupported platforms.
func NewSystem() System {
	return &stubSystem{}
}

func (s *stubSystem) OpenURL(context.Context, string) error {
	return ErrUnsupported
}

func (s *stubSystem) CopyText(context.Context, string) error {
	return ErrUnsupported
}

func (s *stubSystem) Notify(context.Context, string, string) error {
	return ErrUnsupported
}
