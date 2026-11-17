package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func clearYTEnv(t *testing.T) {
	t.Helper()
	for _, k := range knownKeys {
		if _, ok := os.LookupEnv(k); ok {
			t.Setenv(k, "")
			_ = os.Unsetenv(k)
		}
	}
}

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.FeedSize != 100 || cfg.SearchSize != 40 || cfg.CacheTTL != 900 || cfg.CookieTTL != 21600 {
		t.Fatalf("unexpected numeric defaults: %+v", cfg)
	}
	if cfg.AudioVolume != 75 || cfg.Quality != 1080 || cfg.Prefetch != 24 {
		t.Fatalf("unexpected media numeric defaults: %+v", cfg)
	}
	if cfg.MusicMode != "bg" || cfg.DefaultMode != "window" || cfg.Playback != "auto" {
		t.Fatalf("unexpected mode defaults: %+v", cfg)
	}
	if cfg.Home != "auto" || cfg.Lang != "auto" || cfg.Preview != "auto" || cfg.ThumbQuality != "max" {
		t.Fatalf("unexpected display defaults: %+v", cfg)
	}
	if !cfg.HideShorts || !cfg.Interleave || !cfg.Notify || !cfg.SponsorBlock || cfg.DownloadAudio {
		t.Fatalf("unexpected bool defaults: %+v", cfg)
	}
	if cfg.Theme != "youtube-dark" {
		t.Fatalf("Theme = %q, want %q", cfg.Theme, "youtube-dark")
	}
	if cfg.Cookies != "auto" || cfg.YtdlpBin != "yt-dlp" {
		t.Fatalf("unexpected tool defaults: %+v", cfg)
	}
	wantBrowsers := []string{"zen", "firefox", "librewolf", "brave", "chromium", "chrome"}
	if !reflect.DeepEqual(cfg.Browsers, wantBrowsers) {
		t.Fatalf("Browsers = %v, want %v", cfg.Browsers, wantBrowsers)
	}
}

func TestLoadMissingAndErrors(t *testing.T) {
	clearYTEnv(t)

	t.Run("missing file returns defaults", func(t *testing.T) {
		cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.conf"))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !reflect.DeepEqual(cfg, Default()) {
			t.Fatalf("Load() = %+v, want %+v", cfg, Default())
		}
	})

	t.Run("empty path returns defaults", func(t *testing.T) {
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !reflect.DeepEqual(cfg, Default()) {
			t.Fatalf("Load() = %+v, want %+v", cfg, Default())
		}
	})

	t.Run("directory path returns error", func(t *testing.T) {
		if _, err := Load(t.TempDir()); err == nil {
			t.Fatal("Load(dir) expected error, got nil")
		}
	})
}

func TestLoadParsing(t *testing.T) {
	tests := []struct {
		name    string
		content string
		home    string
		env     map[string]string
		check   func(t *testing.T, cfg Config)
	}{
		{
			name: "quotes and inline comments",
			content: `
# leading comment
  YT_DEFAULT_MODE="pip" # double quoted with comment
YT_MUSIC_MODE='term' # single quoted with comment
YT_PLAYBACK=direct # unquoted with comment
YT_HOME=custom#hash # hash without leading space is kept
NON_YT_KEY=ignored
not_a_key_value_line
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.DefaultMode != "pip" {
					t.Errorf("DefaultMode = %q, want %q", cfg.DefaultMode, "pip")
				}
				if cfg.MusicMode != "term" {
					t.Errorf("MusicMode = %q, want %q", cfg.MusicMode, "term")
				}
				if cfg.Playback != "direct" {
					t.Errorf("Playback = %q, want %q", cfg.Playback, "direct")
				}
				if cfg.Home != "custom#hash" {
					t.Errorf("Home = %q, want %q", cfg.Home, "custom#hash")
				}
			},
		},
		{
			name: "tilde and HOME expansion",
			home: "/tmp/testhome",
			content: `
YT_COOKIES_FILE="~/cookies/yt.txt"
YT_DOWNLOAD_DIR="$HOME/Videos/${HOME}/sub"
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.CookiesFile != "/tmp/testhome/cookies/yt.txt" {
					t.Errorf("CookiesFile = %q, want /tmp/testhome/cookies/yt.txt", cfg.CookiesFile)
				}
				if cfg.DownloadDir != "/tmp/testhome/Videos//tmp/testhome/sub" {
					t.Errorf("DownloadDir = %q, want /tmp/testhome/Videos//tmp/testhome/sub", cfg.DownloadDir)
				}
			},
		},
		{
			name: "var default expansion",
			env:  map[string]string{"CUSTOM_BROWSER": "zen"},
			content: `
YT_BROWSER="${CUSTOM_BROWSER:-firefox}"
YT_LANG="${UNSET_LANG_VAR:-es}"
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.Browser != "zen" {
					t.Errorf("Browser = %q, want zen", cfg.Browser)
				}
				if cfg.Lang != "es" {
					t.Errorf("Lang = %q, want es", cfg.Lang)
				}
			},
		},
		{
			name: "env var overrides config file",
			env: map[string]string{
				"YT_QUALITY":     "720",
				"YT_HIDE_SHORTS": "0",
				"YT_YTDLP_ARGS":  "--no-playlist --geo-bypass",
			},
			content: `
YT_QUALITY=2160
YT_HIDE_SHORTS=1
YT_YTDLP_ARGS="--ignore-errors"
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.Quality != 720 {
					t.Errorf("Quality = %d, want 720", cfg.Quality)
				}
				if cfg.HideShorts {
					t.Errorf("HideShorts = true, want false")
				}
				wantArgs := []string{"--no-playlist", "--geo-bypass"}
				if !reflect.DeepEqual(cfg.YtdlpArgs, wantArgs) {
					t.Errorf("YtdlpArgs = %v, want %v", cfg.YtdlpArgs, wantArgs)
				}
			},
		},
		{
			name: "invalid numeric values fall back to defaults",
			content: `
YT_FEED_SIZE=abc
YT_SEARCH_SIZE=-10
YT_CACHE_TTL=12.5
YT_COOKIE_TTL=
YT_AUDIO_VOLUME=85
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.FeedSize != 100 {
					t.Errorf("FeedSize = %d, want 100", cfg.FeedSize)
				}
				if cfg.SearchSize != 40 {
					t.Errorf("SearchSize = %d, want 40", cfg.SearchSize)
				}
				if cfg.CacheTTL != 900 {
					t.Errorf("CacheTTL = %d, want 900", cfg.CacheTTL)
				}
				if cfg.CookieTTL != 21600 {
					t.Errorf("CookieTTL = %d, want 21600", cfg.CookieTTL)
				}
				if cfg.AudioVolume != 85 {
					t.Errorf("AudioVolume = %d, want 85", cfg.AudioVolume)
				}
			},
		},
		{
			name: "invalid enum and bool values fall back to defaults",
			content: `
YT_DEFAULT_MODE=invalid
YT_MUSIC_MODE=nope
YT_PLAYBACK=stream
YT_PREVIEW=sixel
YT_THUMB_QUALITY=ultra
YT_COOKIES=always
YT_HIDE_SHORTS=yes
YT_INTERLEAVE=2
YT_NOTIFY=true
YT_DOWNLOAD_AUDIO=1
YT_BROWSERS="firefox brave"
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.DefaultMode != "window" {
					t.Errorf("DefaultMode = %q, want window", cfg.DefaultMode)
				}
				if cfg.MusicMode != "bg" {
					t.Errorf("MusicMode = %q, want bg", cfg.MusicMode)
				}
				if cfg.Playback != "auto" {
					t.Errorf("Playback = %q, want auto", cfg.Playback)
				}
				if cfg.Preview != "auto" {
					t.Errorf("Preview = %q, want auto", cfg.Preview)
				}
				if cfg.ThumbQuality != "max" {
					t.Errorf("ThumbQuality = %q, want max", cfg.ThumbQuality)
				}
				if cfg.Cookies != "auto" {
					t.Errorf("Cookies = %q, want auto", cfg.Cookies)
				}
				if !cfg.HideShorts || !cfg.Interleave || !cfg.Notify {
					t.Errorf("expected HideShorts/Interleave/Notify to fall back to true, got %v/%v/%v", cfg.HideShorts, cfg.Interleave, cfg.Notify)
				}
				if !cfg.DownloadAudio {
					t.Errorf("DownloadAudio = false, want true")
				}
				wantBrowsers := []string{"firefox", "brave"}
				if !reflect.DeepEqual(cfg.Browsers, wantBrowsers) {
					t.Errorf("Browsers = %v, want %v", cfg.Browsers, wantBrowsers)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearYTEnv(t)
			if tc.home != "" {
				t.Setenv("HOME", tc.home)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			tc.check(t, cfg)
		})
	}
}

func TestSaveVal(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		key     string
		val     string
		want    string
		wantErr bool
	}{
		{
			name:    "creates file and parent directories",
			initial: "",
			key:     "YT_QUALITY",
			val:     "720",
			want:    "YT_QUALITY=\"720\"\n",
		},
		{
			name:    "appends new key preserving existing lines",
			initial: "# comment\nYT_FEED_SIZE=\"50\"\n",
			key:     "YT_QUALITY",
			val:     "1080",
			want:    "# comment\nYT_FEED_SIZE=\"50\"\nYT_QUALITY=\"1080\"\n",
		},
		{
			name:    "updates existing key and removes duplicates",
			initial: "# header\n  YT_QUALITY=480\nYT_LANG=\"en\"\nYT_QUALITY=\"720\"\n",
			key:     "YT_QUALITY",
			val:     "1080",
			want:    "# header\nYT_QUALITY=\"1080\"\nYT_LANG=\"en\"\n",
		},
		{
			name:    "rejects invalid key",
			key:     "BAD_KEY",
			val:     "1",
			wantErr: true,
		},
		{
			name:    "rejects newline in value",
			key:     "YT_LANG",
			val:     "en\nYT_HOME=evil",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "nested")
			path := filepath.Join(dir, "config")
			if tc.initial != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(path, []byte(tc.initial), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}

			err := SaveVal(path, tc.key, tc.val)
			if tc.wantErr {
				if err == nil {
					t.Fatal("SaveVal() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("SaveVal() error = %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("file content =\n%q\nwant\n%q", string(got), tc.want)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "config" {
				t.Errorf("unexpected leftover files in %s: %v", dir, entries)
			}
		})
	}
}

func TestLoadThemeAndSponsorBlock(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		env              map[string]string
		wantTheme        string
		wantSponsorBlock bool
	}{
		{
			name:             "valid normalized theme and disabled sponsorblock",
			content:          "YT_THEME=\"  Catppuccin  \"\nYT_SPONSORBLOCK=0\n",
			wantTheme:        "catppuccin",
			wantSponsorBlock: false,
		},
		{
			name:             "mixed case tokyonight and enabled sponsorblock",
			content:          "YT_THEME=TokyoNight\nYT_SPONSORBLOCK=1\n",
			wantTheme:        "tokyonight",
			wantSponsorBlock: true,
		},
		{
			name:             "gruvbox and kanagawa supported",
			content:          "YT_THEME=GRUVBOX\n",
			wantTheme:        "gruvbox",
			wantSponsorBlock: true,
		},
		{
			name:             "kanagawa valid",
			content:          "YT_THEME=kanagawa\n",
			wantTheme:        "kanagawa",
			wantSponsorBlock: true,
		},
		{
			name:             "invalid theme and invalid bool fall back to defaults",
			content:          "YT_THEME=solarized\nYT_SPONSORBLOCK=true\n",
			wantTheme:        "youtube-dark",
			wantSponsorBlock: true,
		},
		{
			name:             "env overrides config file",
			content:          "YT_THEME=gruvbox\nYT_SPONSORBLOCK=1\n",
			env:              map[string]string{"YT_THEME": "  KANAGAWA ", "YT_SPONSORBLOCK": "0"},
			wantTheme:        "kanagawa",
			wantSponsorBlock: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearYTEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Theme != tc.wantTheme {
				t.Errorf("Theme = %q, want %q", cfg.Theme, tc.wantTheme)
			}
			if cfg.SponsorBlock != tc.wantSponsorBlock {
				t.Errorf("SponsorBlock = %v, want %v", cfg.SponsorBlock, tc.wantSponsorBlock)
			}
		})
	}
}
