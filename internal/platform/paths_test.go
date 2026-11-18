package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	uidFallback := fmt.Sprintf("/tmp/iutui-%d", os.Getuid())

	tests := []struct {
		name           string
		xdgConfigHome  string
		xdgCacheHome   string
		xdgStateHome   string
		xdgRuntimeDir  string
		wantConfigSub  string
		wantCacheSub   string
		wantStateSub   string
		wantRuntimeAbs string
	}{
		{
			name:           "defaults when XDG vars unset",
			wantConfigSub:  filepath.Join(".config", "iutui"),
			wantCacheSub:   filepath.Join(".cache", "iutui"),
			wantStateSub:   filepath.Join(".local", "state", "iutui"),
			wantRuntimeAbs: uidFallback,
		},
		{
			name:           "all XDG vars set",
			xdgConfigHome:  "custom-config",
			xdgCacheHome:   "custom-cache",
			xdgStateHome:   "custom-state",
			xdgRuntimeDir:  "custom-runtime",
			wantConfigSub:  filepath.Join("custom-config", "iutui"),
			wantCacheSub:   filepath.Join("custom-cache", "iutui"),
			wantStateSub:   filepath.Join("custom-state", "iutui"),
			wantRuntimeAbs: "", // computed relative to temp dir
		},
		{
			name:           "partial XDG vars set",
			xdgConfigHome:  "only-config",
			xdgRuntimeDir:  "only-runtime",
			wantConfigSub:  filepath.Join("only-config", "iutui"),
			wantCacheSub:   filepath.Join(".cache", "iutui"),
			wantStateSub:   filepath.Join(".local", "state", "iutui"),
			wantRuntimeAbs: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			setOrClear := func(key, rel string) string {
				if rel == "" {
					t.Setenv(key, "")
					return ""
				}
				abs := filepath.Join(home, rel)
				t.Setenv(key, abs)
				return abs
			}

			setOrClear("XDG_CONFIG_HOME", tc.xdgConfigHome)
			setOrClear("XDG_CACHE_HOME", tc.xdgCacheHome)
			setOrClear("XDG_STATE_HOME", tc.xdgStateHome)
			runtimeBase := setOrClear("XDG_RUNTIME_DIR", tc.xdgRuntimeDir)

			got := ResolvePaths()

			wantConfig := filepath.Join(home, tc.wantConfigSub)
			wantCache := filepath.Join(home, tc.wantCacheSub)
			wantState := filepath.Join(home, tc.wantStateSub)
			wantRuntime := tc.wantRuntimeAbs
			if runtimeBase != "" {
				wantRuntime = filepath.Join(runtimeBase, "iutui")
			}
			wantSocket := filepath.Join(wantRuntime, "mpv.sock")

			if got.ConfigDir != wantConfig {
				t.Errorf("ConfigDir = %q, want %q", got.ConfigDir, wantConfig)
			}
			if got.CacheDir != wantCache {
				t.Errorf("CacheDir = %q, want %q", got.CacheDir, wantCache)
			}
			if got.StateDir != wantState {
				t.Errorf("StateDir = %q, want %q", got.StateDir, wantState)
			}
			if got.RuntimeDir != wantRuntime {
				t.Errorf("RuntimeDir = %q, want %q", got.RuntimeDir, wantRuntime)
			}
			if got.SocketPath != wantSocket {
				t.Errorf("SocketPath = %q, want %q", got.SocketPath, wantSocket)
			}
		})
	}
}
