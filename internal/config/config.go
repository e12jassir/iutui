// Package config loads, validates, and atomically persists ytx configuration.
package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	keyPattern        = regexp.MustCompile(`^YT_[A-Z0-9_]+$`)
	envDefaultPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}$`)
)

// Config holds all YT_* runtime settings for ytx.
type Config struct {
	FeedSize      int
	SearchSize    int
	CacheTTL      int
	CookieTTL     int
	AudioVolume   int
	Quality       int
	Speed         float64
	Autoplay      bool
	MusicMode     string
	DownloadAudio bool
	Prefetch      int
	DefaultMode   string
	Playback      string
	Home          string
	Lang          string
	Preview       string
	ThumbQuality  string
	HideShorts    bool
	Interleave    bool
	Notify        bool
	SponsorBlock  bool
	Theme         string
	Cookies       string
	CookiesFile   string
	Browser       string
	Browsers      []string
	YtdlpBin      string
	YtdlpArgs     []string
	DownloadDir   string
}

// Default returns a Config populated with the default values from lib/config.sh.
func Default() Config {
	return Config{
		FeedSize:      100,
		SearchSize:    40,
		CacheTTL:      900,
		CookieTTL:     21600,
		AudioVolume:   75,
		Quality:       720, // Crisp HD with ~3x-12x lower data usage than 1080p60/4K60.
		Speed:         1.0,
		Autoplay:      true,
		MusicMode:     "bg",
		DownloadAudio: false,
		Prefetch:      24,
		DefaultMode:   "window",
		Playback:      "auto",
		Home:          "auto",
		Lang:          "auto",
		Preview:       "auto",
		ThumbQuality:  "max",
		HideShorts:    true,
		Interleave:    true,
		Notify:        true,
		SponsorBlock:  true,
		Theme:         "youtube-dark",
		Cookies:       "auto",
		CookiesFile:   "",
		Browser:       "",
		Browsers:      []string{"zen", "firefox", "librewolf", "brave", "chromium", "chrome"},
		YtdlpBin:      "yt-dlp",
		YtdlpArgs:     nil,
		DownloadDir:   "",
	}
}

// Load reads KEY=VALUE settings from path, applies environment variable overrides,
// and validates numeric and enum fields, falling back to defaults on invalid values.
// If path is empty or does not exist, Default() with environment overrides is returned.
func Load(path string) (Config, error) {
	raw := make(map[string]string)

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("read config %q: %w", path, err)
			}
		} else {
			parseLines(data, raw)
		}
	}

	// Environment variables always override config file values.
	for _, key := range knownKeys {
		if v, ok := os.LookupEnv(key); ok {
			raw[key] = v
		}
	}

	return buildConfig(raw), nil
}

// SaveVal atomically updates or appends KEY="VAL" in path using a temporary file and rename.
func SaveVal(path, key, val string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("invalid config key %q", key)
	}
	if strings.ContainsAny(val, "\r\n") {
		return fmt.Errorf("invalid config value for %q: contains newline", key)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %q: %w", dir, err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read config %q: %w", path, err)
	}

	var out bytes.Buffer
	done := false
	targetLine := fmt.Sprintf(`%s="%s"`, key, val)

	if len(existing) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(existing))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, key+"=") {
				if !done {
					out.WriteString(targetLine)
					out.WriteByte('\n')
					done = true
				}
				continue
			}
			out.WriteString(line)
			out.WriteByte('\n')
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan config %q: %w", path, err)
		}
	}

	if !done {
		out.WriteString(targetLine)
		out.WriteByte('\n')
	}

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleanup = false
	return nil
}

var knownKeys = []string{
	"YT_FEED_SIZE",
	"YT_SEARCH_SIZE",
	"YT_CACHE_TTL",
	"YT_COOKIE_TTL",
	"YT_AUDIO_VOLUME",
	"YT_QUALITY",
	"YT_SPEED",
	"YT_AUTOPLAY",
	"YT_MUSIC_MODE",
	"YT_DOWNLOAD_AUDIO",
	"YT_PREFETCH",
	"YT_DEFAULT_MODE",
	"YT_PLAYBACK",
	"YT_HOME",
	"YT_LANG",
	"YT_PREVIEW",
	"YT_THUMB_QUALITY",
	"YT_HIDE_SHORTS",
	"YT_INTERLEAVE",
	"YT_NOTIFY",
	"YT_SPONSORBLOCK",
	"YT_THEME",
	"YT_COOKIES",
	"YT_COOKIES_FILE",
	"YT_BROWSER",
	"YT_BROWSERS",
	"YT_YTDLP",
	"YT_YTDLP_ARGS",
	"YT_DOWNLOAD_DIR",
}

func parseLines(data []byte, dst map[string]string) {
	home := resolveHome()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimLeft(scanner.Text(), " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimRight(line[:eq], " \t")
		if !keyPattern.MatchString(key) {
			continue
		}
		val := strings.TrimLeft(line[eq+1:], " \t")
		val = strings.TrimRight(val, "\r")
		val = unquoteValue(val)
		val = expandEnvDefault(val)
		val = expandHome(val, home)
		dst[key] = val
	}
}

func unquoteValue(val string) string {
	switch {
	case strings.HasPrefix(val, `"`):
		rest := val[1:]
		if end := strings.IndexByte(rest, '"'); end >= 0 {
			return rest[:end]
		}
		return rest
	case strings.HasPrefix(val, `'`):
		rest := val[1:]
		if end := strings.IndexByte(rest, '\''); end >= 0 {
			return rest[:end]
		}
		return rest
	default:
		for i := 0; i < len(val); i++ {
			if val[i] == '#' && i > 0 && (val[i-1] == ' ' || val[i-1] == '\t') {
				val = val[:i-1]
				break
			}
		}
		return strings.TrimRight(val, " \t")
	}
}

func expandEnvDefault(val string) string {
	m := envDefaultPattern.FindStringSubmatch(val)
	if m == nil {
		return val
	}
	if v := os.Getenv(m[1]); v != "" {
		return v
	}
	return m[2]
}

func expandHome(val, home string) string {
	if home == "" {
		return val
	}
	if strings.HasPrefix(val, "~") {
		val = home + val[1:]
	}
	val = strings.ReplaceAll(val, "${HOME}", home)
	val = strings.ReplaceAll(val, "$HOME", home)
	return val
}

func resolveHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func buildConfig(raw map[string]string) Config {
	cfg := Default()

	cfg.FeedSize = asNum(raw["YT_FEED_SIZE"], cfg.FeedSize)
	cfg.SearchSize = asNum(raw["YT_SEARCH_SIZE"], cfg.SearchSize)
	cfg.CacheTTL = asNum(raw["YT_CACHE_TTL"], cfg.CacheTTL)
	cfg.CookieTTL = asNum(raw["YT_COOKIE_TTL"], cfg.CookieTTL)
	cfg.AudioVolume = asNum(raw["YT_AUDIO_VOLUME"], cfg.AudioVolume)
	cfg.Quality = asNum(raw["YT_QUALITY"], cfg.Quality)
	cfg.Prefetch = asNum(raw["YT_PREFETCH"], cfg.Prefetch)
	cfg.Speed = asFloatRange(raw["YT_SPEED"], cfg.Speed, 0.5, 2.0)

	cfg.DefaultMode = asEnum(raw["YT_DEFAULT_MODE"], cfg.DefaultMode, "window", "pip", "fs", "audio")
	cfg.MusicMode = asEnum(raw["YT_MUSIC_MODE"], cfg.MusicMode, "bg", "term")
	cfg.Playback = asEnum(raw["YT_PLAYBACK"], cfg.Playback, "pipe", "direct", "auto")
	cfg.Preview = asEnum(raw["YT_PREVIEW"], cfg.Preview, "auto", "kitty", "chafa", "none")
	cfg.ThumbQuality = asEnum(raw["YT_THUMB_QUALITY"], cfg.ThumbQuality, "max", "hq", "mq")
	cfg.Cookies = asEnum(raw["YT_COOKIES"], cfg.Cookies, "auto", "off")
	cfg.Theme = asEnum(strings.ToLower(strings.TrimSpace(raw["YT_THEME"])), cfg.Theme, "youtube-dark", "catppuccin", "tokyonight", "gruvbox", "kanagawa", "terminal")

	cfg.Autoplay = asBool(raw["YT_AUTOPLAY"], cfg.Autoplay)
	cfg.HideShorts = asBool(raw["YT_HIDE_SHORTS"], cfg.HideShorts)
	cfg.Interleave = asBool(raw["YT_INTERLEAVE"], cfg.Interleave)
	cfg.Notify = asBool(raw["YT_NOTIFY"], cfg.Notify)
	cfg.SponsorBlock = asBool(raw["YT_SPONSORBLOCK"], cfg.SponsorBlock)
	cfg.DownloadAudio = asBool(raw["YT_DOWNLOAD_AUDIO"], cfg.DownloadAudio)

	if v := raw["YT_HOME"]; v != "" {
		cfg.Home = v
	}
	cfg.Lang = asEnum(raw["YT_LANG"], cfg.Lang, "auto", "es", "en")
	if v, ok := raw["YT_COOKIES_FILE"]; ok {
		cfg.CookiesFile = v
	}
	if v, ok := raw["YT_BROWSER"]; ok {
		cfg.Browser = v
	}
	if v := raw["YT_BROWSERS"]; v != "" {
		cfg.Browsers = strings.Fields(v)
	}
	if v := raw["YT_YTDLP"]; v != "" {
		cfg.YtdlpBin = v
	}
	if v := raw["YT_YTDLP_ARGS"]; v != "" {
		cfg.YtdlpArgs = strings.Fields(v)
	}
	if v, ok := raw["YT_DOWNLOAD_DIR"]; ok {
		cfg.DownloadDir = v
	}

	return cfg
}

func asNum(val string, def int) int {
	if val == "" {
		return def
	}
	for i := 0; i < len(val); i++ {
		if val[i] < '0' || val[i] > '9' {
			return def
		}
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return n
}

func asEnum(val, def string, allowed ...string) string {
	for _, a := range allowed {
		if val == a {
			return val
		}
	}
	return def
}

func asBool(val string, def bool) bool {
	switch val {
	case "1":
		return true
	case "0":
		return false
	default:
		return def
	}
}

func asFloatRange(val string, def, minVal, maxVal float64) float64 {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return def
	}
	v, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < minVal || v > maxVal {
		return def
	}
	return v
}
