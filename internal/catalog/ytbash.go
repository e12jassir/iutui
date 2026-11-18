package catalog

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

// BashCatalog implements Catalog by reading cached TSV feeds and falling back
// to invoking the yt bash CLI with __list.
type BashCatalog struct {
	CacheDir string
	StateDir string
	BinPath  string
}

// NewBashCatalog constructs a BashCatalog with explicit directories and binary
// path. Empty values fall back to XDG defaults and PATH/executable resolution.
func NewBashCatalog(cacheDir, stateDir, binPath string) *BashCatalog {
	return &BashCatalog{
		CacheDir: cacheDir,
		StateDir: stateDir,
		BinPath:  binPath,
	}
}

// Feed returns videos for the given tab from cached TSV files when available,
// falling back to `yt __list <view>` with context cancellation support.
func (c *BashCatalog) Feed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cacheDir, stateDir := c.resolveDirs()

	if tab == model.TabDownloads {
		return loadDownloads(stateDir), nil
	}

	if path := c.cachedFeedPath(tab, cacheDir, stateDir); path != "" {
		if videos, err := ParseTSVFile(path); err == nil && len(videos) > 0 {
			return videos, nil
		}
	}

	viewArg, ok := tabViewArg(tab)
	if !ok {
		return nil, nil
	}

	return c.runList(ctx, viewArg)
}

// RefreshFeed bypasses cached TSV files and fetches fresh results from yt.
func (c *BashCatalog) RefreshFeed(ctx context.Context, tab model.Tab) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, stateDir := c.resolveDirs()
	if tab == model.TabDownloads {
		return loadDownloads(stateDir), nil
	}
	viewArg, ok := tabViewArg(tab)
	if !ok {
		return nil, nil
	}
	return c.runList(ctx, viewArg)
}

// Search queries YouTube via `yt __list search <query>` using exec.CommandContext.
func (c *BashCatalog) Search(ctx context.Context, query string) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	return c.runList(ctx, "search", query)
}

func (c *BashCatalog) runList(ctx context.Context, args ...string) ([]model.Video, error) {
	bin := c.BinPath
	if bin == "" {
		bin = resolveYtBin()
	}

	cmdArgs := append([]string{"__list"}, args...)
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	// Put the child in its own process group so cancellation kills yt and its
	// subprocesses (e.g. yt-dlp/curl) rather than leaving them holding stdout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}

	return ParseTSV(bytes.NewReader(out))
}

func (c *BashCatalog) resolveDirs() (cacheDir, stateDir string) {
	cacheDir = c.CacheDir
	if cacheDir == "" {
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache == "" {
			home, _ := os.UserHomeDir()
			xdgCache = filepath.Join(home, ".cache")
		}
		cacheDir = filepath.Join(xdgCache, "yt_terminal")
	}

	stateDir = c.StateDir
	if stateDir == "" {
		xdgState := os.Getenv("XDG_STATE_HOME")
		if xdgState == "" {
			home, _ := os.UserHomeDir()
			xdgState = filepath.Join(home, ".local", "state")
		}
		stateDir = filepath.Join(xdgState, "yt")
	}

	return cacheDir, stateDir
}

func (c *BashCatalog) cachedFeedPath(tab model.Tab, cacheDir, stateDir string) string {
	switch tab {
	case model.TabHome:
		return firstExisting(
			filepath.Join(cacheDir, "feed_foryou.tsv"),
			filepath.Join(cacheDir, "feed_para_ti.tsv"),
		)
	case model.TabMusic:
		return filepath.Join(cacheDir, "feed_music.tsv")
	case model.TabSubscriptions:
		return firstExisting(
			filepath.Join(cacheDir, "feed_subs.tsv"),
			filepath.Join(cacheDir, "feed_suscripciones.tsv"),
		)
	case model.TabHistory:
		return filepath.Join(stateDir, "history.tsv")
	default:
		return ""
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func tabViewArg(tab model.Tab) (string, bool) {
	switch tab {
	case model.TabHome:
		return "foryou", true
	case model.TabMusic:
		return "music", true
	case model.TabSubscriptions:
		return "subs", true
	case model.TabHistory:
		return "history", true
	default:
		return "", false
	}
}

func resolveYtBin() string {
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "yt")
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling
		}
	}
	if info, err := os.Stat("./bin/yt"); err == nil && !info.IsDir() {
		return "./bin/yt"
	}
	if p, err := exec.LookPath("yt"); err == nil {
		return p
	}
	return "yt"
}

// ParseTSVFile opens a TSV file at path and parses its video rows.
func ParseTSVFile(path string) ([]model.Video, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseTSV(f)
}

// ParseTSV reads TSV lines from r and returns parsed Video entries, skipping
// blank lines, comments, and malformed rows.
func ParseTSV(r io.Reader) ([]model.Video, error) {
	var videos []model.Video
	scanner := bufio.NewScanner(r)
	// Allow long lines in feed TSVs.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		v, ok := parseTSVLine(scanner.Text())
		if ok {
			videos = append(videos, v)
		}
	}
	return videos, scanner.Err()
}

func parseTSVLine(raw string) (model.Video, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return model.Video{}, false
	}

	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return model.Video{}, false
	}

	id := strings.TrimSpace(parts[0])

	// Legacy history row: timestamp \t title \t channel \t duration \t id \t url
	if isInvalidID(id) && len(parts) >= 6 && strings.HasPrefix(strings.TrimSpace(parts[5]), "http") {
		legacyID := strings.TrimSpace(parts[4])
		if isInvalidID(legacyID) {
			return model.Video{}, false
		}
		return normalizeVideo(model.Video{
			ID:       legacyID,
			Title:    strings.TrimSpace(parts[1]),
			Channel:  strings.TrimSpace(parts[2]),
			Duration: strings.TrimSpace(parts[3]),
		}), true
	}

	if isInvalidID(id) {
		return model.Video{}, false
	}

	v := model.Video{ID: id}

	// Formatted TSV: id \t url \t title \t channel \t duration [\t views]
	// Raw TSV:       id \t title \t channel \t duration [\t url \t views \t live]
	switch {
	case len(parts) >= 5 && strings.HasPrefix(strings.TrimSpace(parts[1]), "http"):
		v.Title = strings.TrimSpace(parts[2])
		v.Channel = strings.TrimSpace(parts[3])
		v.Duration = strings.TrimSpace(parts[4])
		if len(parts) >= 6 {
			v.Views = strings.TrimSpace(parts[5])
		}
	case len(parts) >= 4:
		v.Title = strings.TrimSpace(parts[1])
		v.Channel = strings.TrimSpace(parts[2])
		v.Duration = strings.TrimSpace(parts[3])
		if len(parts) >= 6 {
			v.Views = strings.TrimSpace(parts[5])
		}
		if len(parts) >= 7 && strings.TrimSpace(parts[6]) == "live" {
			v.IsLive = true
		}
	default:
		return model.Video{}, false
	}

	return normalizeVideo(v), true
}

func isInvalidID(id string) bool {
	return len(id) < 5 || strings.Contains(id, " ")
}

func normalizeVideo(v model.Video) model.Video {
	if v.Channel == "NA" || v.Channel == "-" {
		v.Channel = "YouTube"
	}
	if v.Views == "NA" || v.Views == "-" {
		v.Views = ""
	} else {
		v.Views = formatHumanViews(v.Views)
	}
	if v.Duration == "NA" || v.Duration == "-" {
		v.Duration = ""
	}
	if model.IsPlaylistID(v.ID) {
		v.IsLive = false
		if v.Duration == "" || strings.EqualFold(v.Duration, "live") {
			v.Duration = "MIX"
		}
	}
	return v
}

func formatHumanViews(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var n int64
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return raw
		}
		n = n*10 + int64(ch-'0')
	}
	switch {
	case n >= 1_000_000_000:
		return strings.TrimSuffix(formatFloat1(float64(n)/1e9), ".0") + "B vistas"
	case n >= 1_000_000:
		return strings.TrimSuffix(formatFloat1(float64(n)/1e6), ".0") + "M vistas"
	case n >= 100_000:
		return formatInt(n/1000) + "K vistas"
	case n >= 1_000:
		return strings.TrimSuffix(formatFloat1(float64(n)/1e3), ".0") + "K vistas"
	default:
		return raw + " vistas"
	}
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func formatFloat1(f float64) string {
	tenths := int64(f*10 + 0.5)
	whole := tenths / 10
	rem := tenths % 10
	return formatInt(whole) + "." + formatInt(rem)
}

func loadDownloads(stateDir string) []model.Video {
	dlDir := filepath.Join(stateDir, "downloads")
	entries, err := os.ReadDir(dlDir)
	if err != nil {
		return nil
	}

	var videos []model.Video
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dlDir, e.Name()))
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		if len(lines) >= 2 {
			id := strings.TrimSuffix(e.Name(), ".meta")
			videos = append(videos, model.Video{
				ID:        id,
				Title:     lines[0],
				Channel:   lines[1],
				Duration:  "Descargado",
				Published: "Local",
			})
		}
	}
	return videos
}
