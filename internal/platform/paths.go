package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths holds resolved XDG base directories and runtime socket path for iutui.
type Paths struct {
	ConfigDir  string
	CacheDir   string
	StateDir   string
	RuntimeDir string
	SocketPath string
}

// ResolvePaths resolves XDG Base Directory paths for iutui, falling back to
// standard user directories when XDG environment variables are unset.
func ResolvePaths() Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}

	configDir := resolveXDGDir("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cacheDir := resolveXDGDir("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	stateDir := resolveXDGDir("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	var runtimeDir string
	if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
		runtimeDir = filepath.Join(xdgRuntime, "iutui")
	} else {
		runtimeDir = fmt.Sprintf("/tmp/iutui-%d", os.Getuid())
	}

	return Paths{
		ConfigDir:  configDir,
		CacheDir:   cacheDir,
		StateDir:   stateDir,
		RuntimeDir: runtimeDir,
		SocketPath: filepath.Join(runtimeDir, "mpv.sock"),
	}
}

func resolveXDGDir(envVar, fallbackBase string) string {
	if base := os.Getenv(envVar); base != "" {
		return filepath.Join(base, "iutui")
	}
	return filepath.Join(fallbackBase, "iutui")
}
