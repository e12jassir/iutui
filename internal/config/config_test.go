package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	if cfg.AudioVolume != 75 || cfg.Quality != 720 || cfg.Prefetch != 24 || cfg.Speed != 1.0 {
		t.Fatalf("unexpected media numeric defaults: %+v", cfg)
	}
	if cfg.DefaultMode != "window" {
		t.Fatalf("unexpected mode defaults: %+v", cfg)
	}
	if cfg.Home != "auto" || cfg.Lang != "auto" || cfg.Preview != "auto" || cfg.ThumbQuality != "max" {
		t.Fatalf("unexpected display defaults: %+v", cfg)
	}
	if !cfg.Autoplay || !cfg.HideShorts || !cfg.Interleave || !cfg.Notify || !cfg.SponsorBlock || cfg.DownloadAudio {
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
YT_PREVIEW='kitty' # single quoted with comment
YT_THUMB_QUALITY=hq # unquoted with comment
YT_HOME=custom#hash # hash without leading space is kept
NON_YT_KEY=ignored
not_a_key_value_line
`,
			check: func(t *testing.T, cfg Config) {
				if cfg.DefaultMode != "pip" {
					t.Errorf("DefaultMode = %q, want %q", cfg.DefaultMode, "pip")
				}
				if cfg.Preview != "kitty" {
					t.Errorf("Preview = %q, want %q", cfg.Preview, "kitty")
				}
				if cfg.ThumbQuality != "hq" {
					t.Errorf("ThumbQuality = %q, want %q", cfg.ThumbQuality, "hq")
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
			name:             "terminal native theme valid",
			content:          "YT_THEME=terminal\n",
			wantTheme:        "terminal",
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

func TestLoadAutoplayAndSpeed(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantAutoplay bool
		wantSpeed    float64
	}{
		{
			name:         "valid disabled autoplay and custom speed",
			content:      "YT_AUTOPLAY=0\nYT_SPEED=1.5\n",
			wantAutoplay: false,
			wantSpeed:    1.5,
		},
		{
			name:         "boundary speeds 0.5 and 2.0",
			content:      "YT_AUTOPLAY=1\nYT_SPEED=0.5\n",
			wantAutoplay: true,
			wantSpeed:    0.5,
		},
		{
			name:         "out-of-range speed falls back to 1.0",
			content:      "YT_AUTOPLAY=invalid\nYT_SPEED=3.25\n",
			wantAutoplay: true,
			wantSpeed:    1.0,
		},
		{
			name:         "below-range or non-numeric speed falls back to 1.0",
			content:      "YT_SPEED=fast\n",
			wantAutoplay: true,
			wantSpeed:    1.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearYTEnv(t)
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Autoplay != tc.wantAutoplay {
				t.Errorf("Autoplay = %v, want %v", cfg.Autoplay, tc.wantAutoplay)
			}
			if cfg.Speed != tc.wantSpeed {
				t.Errorf("Speed = %v, want %v", cfg.Speed, tc.wantSpeed)
			}
		})
	}
}

func TestLoadLangEnum(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "auto", want: "auto"},
		{raw: "es", want: "es"},
		{raw: "en", want: "en"},
		{raw: "fr", want: "auto"},
		{raw: "", want: "auto"},
	}
	for _, tc := range tests {
		t.Run("YT_LANG="+tc.raw, func(t *testing.T) {
			clearYTEnv(t)
			path := filepath.Join(t.TempDir(), "config")
			content := ""
			if tc.raw != "" {
				content = "YT_LANG=" + tc.raw + "\n"
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Lang != tc.want {
				t.Errorf("Lang = %q, want %q", cfg.Lang, tc.want)
			}
		})
	}
}

func TestConfigExampleMatchesKnownKeysAndNoLegacyFields(t *testing.T) {
	clearYTEnv(t)

	typ := reflect.TypeOf(Config{})
	for _, legacyField := range []string{"MusicMode", "Playback"} {
		if _, exists := typ.FieldByName(legacyField); exists {
			t.Errorf("legacy bash field %q should be removed from Config", legacyField)
		}
	}
	for _, legacyKey := range []string{"YT_MUSIC_MODE", "YT_PLAYBACK", "YT_HEIGHT", "YT_BROWSER_CMD", "YT_FZF_OPTS"} {
		if slices.Contains(knownKeys, legacyKey) {
			t.Errorf("legacy/ghost key %q should not be in knownKeys", legacyKey)
		}
	}

	examplePath := filepath.Join("..", "..", "config.example")
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read config.example: %v", err)
	}
	content := string(raw)
	for _, ghost := range []string{"YT_HEIGHT", "YT_BROWSER_CMD", "YT_FZF_OPTS", "YT_MUSIC_MODE", "YT_PLAYBACK"} {
		if strings.Contains(content, ghost) {
			t.Errorf("config.example still contains ghost/legacy key %q", ghost)
		}
	}
	for _, required := range []string{"YT_LANG=", "YT_SPEED=", "YT_HOME=", "YT_PREFETCH=", "YT_NOTIFY=", "YT_YTDLP_ARGS="} {
		if !strings.Contains(content, required) {
			t.Errorf("config.example missing documented key %q", required)
		}
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		}
		if eq := strings.IndexByte(trimmed, '='); eq > 0 {
			candKey := strings.TrimSpace(trimmed[:eq])
			if keyPattern.MatchString(candKey) && !slices.Contains(knownKeys, candKey) {
				t.Errorf("config.example references key %q outside knownKeys", candKey)
			}
		}
	}

	cfg, err := Load(examplePath)
	if err != nil {
		t.Fatalf("Load(config.example) error: %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("Load(config.example) = %+v, want Default() %+v", cfg, Default())
	}
}
