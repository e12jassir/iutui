//go:build !linux

package platform

// MPRISBridge is a no-op stub on non-Linux platforms where D-Bus MPRIS2 is unsupported.
type MPRISBridge struct{}

// NewMPRISBridge returns an inactive no-op MPRISBridge on non-Linux platforms.
func NewMPRISBridge(MPRISCallbacks) *MPRISBridge {
	return &MPRISBridge{}
}

// Active reports whether the MPRIS2 D-Bus bridge is active.
func (b *MPRISBridge) Active() bool {
	return false
}

// UpdateStatus is a no-op on non-Linux platforms.
func (b *MPRISBridge) UpdateStatus(MPRISStatus) {}

// Close is a no-op on non-Linux platforms.
func (b *MPRISBridge) Close() error {
	return nil
}
