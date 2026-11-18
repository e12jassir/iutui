package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFromYT(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, home string, p Paths)
		wantMigrated   bool
		wantSecondRun  bool
		checkPostState func(t *testing.T, home string, p Paths)
	}{
		{
			name: "no legacy directories exist",
			setup: func(t *testing.T, home string, p Paths) {
				// Nothing created.
			},
			wantMigrated:  false,
			wantSecondRun: false,
		},
		{
			name: "migrates all legacy dirs, preserves originals, skips cookies, writes marker",
			setup: func(t *testing.T, home string, p Paths) {
				writeTestFile(t, filepath.Join(home, ".local", "state", "yt", "history.json"), `{"v":1}`)
				writeTestFile(t, filepath.Join(home, ".local", "state", "yt", "sub", "queue.txt"), "track1\n")
				writeTestFile(t, filepath.Join(home, ".local", "state", "yt", "cookies.txt"), "secret-cookie")
				writeTestFile(t, filepath.Join(home, ".cache", "yt_terminal", "thumbs", "abc.jpg"), "imgdata")
				writeTestFile(t, filepath.Join(home, ".cache", "yt_terminal", "yt_cookie.sqlite"), "secret-db")
				writeTestFile(t, filepath.Join(home, ".config", "yt", "config.toml"), "player = 'mpv'\n")
				writeTestFile(t, filepath.Join(home, ".config", "yt", "MyCookies"), "secret")
			},
			wantMigrated:  true,
			wantSecondRun: false,
			checkPostState: func(t *testing.T, home string, p Paths) {
				// Target files copied.
				assertFileContent(t, filepath.Join(p.StateDir, "history.json"), `{"v":1}`)
				assertFileContent(t, filepath.Join(p.StateDir, "sub", "queue.txt"), "track1\n")
				assertFileContent(t, filepath.Join(p.CacheDir, "thumbs", "abc.jpg"), "imgdata")
				assertFileContent(t, filepath.Join(p.ConfigDir, "config.toml"), "player = 'mpv'\n")

				// Cookie files skipped.
				assertNotExists(t, filepath.Join(p.StateDir, "cookies.txt"))
				assertNotExists(t, filepath.Join(p.CacheDir, "yt_cookie.sqlite"))
				assertNotExists(t, filepath.Join(p.ConfigDir, "MyCookies"))

				// Legacy files preserved.
				assertFileContent(t, filepath.Join(home, ".local", "state", "yt", "history.json"), `{"v":1}`)
				assertFileContent(t, filepath.Join(home, ".local", "state", "yt", "cookies.txt"), "secret-cookie")
				assertFileContent(t, filepath.Join(home, ".cache", "yt_terminal", "thumbs", "abc.jpg"), "imgdata")
				assertFileContent(t, filepath.Join(home, ".config", "yt", "config.toml"), "player = 'mpv'\n")

				// .migrated markers created.
				assertExists(t, filepath.Join(p.StateDir, ".migrated"))
				assertExists(t, filepath.Join(p.CacheDir, ".migrated"))
				assertExists(t, filepath.Join(p.ConfigDir, ".migrated"))
			},
		},
		{
			name: "target dir exists without .migrated marker still migrates",
			setup: func(t *testing.T, home string, p Paths) {
				writeTestFile(t, filepath.Join(home, ".config", "yt", "config.toml"), "quality = 'best'\n")
				if err := os.MkdirAll(p.ConfigDir, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantMigrated:  true,
			wantSecondRun: false,
			checkPostState: func(t *testing.T, home string, p Paths) {
				assertFileContent(t, filepath.Join(p.ConfigDir, "config.toml"), "quality = 'best'\n")
				assertExists(t, filepath.Join(p.ConfigDir, ".migrated"))
			},
		},
		{
			name: "target dir already has .migrated marker is skipped",
			setup: func(t *testing.T, home string, p Paths) {
				writeTestFile(t, filepath.Join(home, ".config", "yt", "config.toml"), "old = true\n")
				writeTestFile(t, filepath.Join(p.ConfigDir, ".migrated"), "migrated\n")
			},
			wantMigrated:  false,
			wantSecondRun: false,
			checkPostState: func(t *testing.T, home string, p Paths) {
				assertNotExists(t, filepath.Join(p.ConfigDir, "config.toml"))
			},
		},
		{
			name: "prefers ytx over legacy yt when both exist",
			setup: func(t *testing.T, home string, p Paths) {
				writeTestFile(t, filepath.Join(home, ".config", "yt", "config"), "YT_QUALITY=\"1080\"\n")
				writeTestFile(t, filepath.Join(home, ".config", "ytx", "config"), "YT_QUALITY=\"720\"\n")
				writeTestFile(t, filepath.Join(home, ".config", "ytx", ".migrated"), "migrated\n")
				writeTestFile(t, filepath.Join(home, ".local", "state", "ytx", "history.tsv"), "vidYTX\tNewest\n")
			},
			wantMigrated:  true,
			wantSecondRun: false,
			checkPostState: func(t *testing.T, home string, p Paths) {
				assertFileContent(t, filepath.Join(p.ConfigDir, "config"), "YT_QUALITY=\"720\"\n")
				assertFileContent(t, filepath.Join(p.StateDir, "history.tsv"), "vidYTX\tNewest\n")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg", "config"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "xdg", "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg", "state"))
			t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "xdg", "runtime"))

			p := ResolvePaths()
			tc.setup(t, home, p)

			got, err := MigrateFromYT(p)
			if err != nil {
				t.Fatalf("first MigrateFromYT() error = %v", err)
			}
			if got != tc.wantMigrated {
				t.Fatalf("first MigrateFromYT() = %v, want %v", got, tc.wantMigrated)
			}

			if tc.checkPostState != nil {
				tc.checkPostState(t, home, p)
			}

			gotSecond, err := MigrateFromYT(p)
			if err != nil {
				t.Fatalf("second MigrateFromYT() error = %v", err)
			}
			if gotSecond != tc.wantSecondRun {
				t.Fatalf("second MigrateFromYT() = %v, want %v", gotSecond, tc.wantSecondRun)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(data) != want {
		t.Errorf("file %q = %q, want %q", path, string(data), want)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %q to exist, got err: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %q not to exist, got err: %v", path, err)
	}
}
