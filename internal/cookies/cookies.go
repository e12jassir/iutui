// Package cookies provides Netscape cookie jar filtering, login detection,
// private temporary jar storage, cache cleanup, and Firefox-based profile detection.
package cookies

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/platform"
)

// StampFileName is the sentinel filename written alongside cookies.txt to
// track the last successful browser cookie jar refresh.
const StampFileName = "cookies.stamp"

const netscapeCookieHeader = "# Netscape HTTP Cookie File\n"

var (
	youtubeDomainRe = regexp.MustCompile(`(^|\.)(youtube|youtube-nocookie|googlevideo|ytimg|ggpht)\.com$`)
	googleDomainRe  = regexp.MustCompile(`(^|\.)google\.[a-z.]+$`)
	loginTokenRe    = regexp.MustCompile(`(?:^|\s)(LOGIN_INFO|SAPISID|__Secure-3PAPISID)(?:\s|$)`)
)

// FilterNetscape filters a Netscape cookie jar from r, retaining only comment
// lines and entries whose domain matches Google or YouTube domains.
func FilterNetscape(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, errors.New("cookies: nil reader")
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var out bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		domain, _, _ := strings.Cut(line, "\t")
		domain = strings.TrimPrefix(domain, "#HttpOnly_")
		if isAllowedDomain(domain) {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func isAllowedDomain(domain string) bool {
	return youtubeDomainRe.MatchString(domain) || googleDomainRe.MatchString(domain)
}

// HasLogin reports whether the Netscape cookie jar data contains a Google or
// YouTube authentication token (LOGIN_INFO, SAPISID, or __Secure-3PAPISID).
func HasLogin(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return loginTokenRe.Match(data)
}

// WritePrivateTemp writes data to a new temporary cookie jar in dir with
// strict 0600 permissions and returns its path.
func WritePrivateTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, "ck.*")
	if err != nil {
		return "", err
	}
	name := f.Name()

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Forget removes persisted cookie jars (cookies.txt, cookies.none, cookies.stamp)
// and temporary cookie jars (ck.*) from cacheDir.
func Forget(cacheDir string) error {
	if cacheDir == "" {
		return errors.New("cookies: empty cache directory")
	}

	var errs []error
	for _, name := range []string{"cookies.txt", "cookies.none", StampFileName} {
		if err := removeIfExists(filepath.Join(cacheDir, name)); err != nil {
			errs = append(errs, err)
		}
	}

	temps, err := filepath.Glob(filepath.Join(cacheDir, "ck.*"))
	if err != nil {
		errs = append(errs, err)
	}
	for _, p := range temps {
		if err := removeIfExists(p); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DetectFirefoxProfile searches bases in order and returns the first valid
// profile directory containing cookies.sqlite, checking profiles.ini
// ([Install*] Default, then [Profile*] Default=1) before falling back to
// *default-release* and *[Dd]efault* directory globs.
func DetectFirefoxProfile(bases ...string) string {
	patterns := []string{"*default-release*", "*[Dd]efault*"}
	for _, base := range bases {
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		if profileDir := detectProfileFromINI(base); profileDir != "" {
			return profileDir
		}
		for _, pat := range patterns {
			matches, err := filepath.Glob(filepath.Join(base, pat))
			if err != nil {
				continue
			}
			for _, profileDir := range matches {
				if hasCookiesSQLite(profileDir) {
					return profileDir
				}
			}
		}
	}
	return ""
}

func hasCookiesSQLite(profileDir string) bool {
	if profileDir == "" {
		return false
	}
	sqlitePath := filepath.Join(profileDir, "cookies.sqlite")
	fi, err := os.Stat(sqlitePath)
	return err == nil && !fi.IsDir()
}

func detectProfileFromINI(base string) string {
	data, err := os.ReadFile(filepath.Join(base, "profiles.ini"))
	if err != nil {
		return ""
	}

	type iniSection struct {
		name       string
		defaultVal string
		path       string
		isRelative string
	}

	var sections []iniSection
	curIdx := -1

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			sections = append(sections, iniSection{name: name})
			curIdx = len(sections) - 1
			continue
		}
		if curIdx < 0 {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch key {
		case "Default":
			sections[curIdx].defaultVal = val
		case "Path":
			sections[curIdx].path = val
		case "IsRelative":
			sections[curIdx].isRelative = val
		}
	}
	if err := scanner.Err(); err != nil {
		return ""
	}

	// Priority 1: [Install*] Default=<path>
	for _, sec := range sections {
		if !strings.HasPrefix(sec.name, "Install") || sec.defaultVal == "" {
			continue
		}
		profileDir := sec.defaultVal
		if !filepath.IsAbs(profileDir) {
			profileDir = filepath.Join(base, profileDir)
		}
		if hasCookiesSQLite(profileDir) {
			return profileDir
		}
	}

	// Priority 2: [Profile*] Default=1 with Path=<path>
	for _, sec := range sections {
		if !strings.HasPrefix(sec.name, "Profile") || sec.defaultVal != "1" || sec.path == "" {
			continue
		}
		profileDir := sec.path
		if sec.isRelative != "0" && !filepath.IsAbs(profileDir) {
			profileDir = filepath.Join(base, profileDir)
		}
		if hasCookiesSQLite(profileDir) {
			return profileDir
		}
	}

	return ""
}

type profileRootKind uint8

const (
	rootHome profileRootKind = iota
	rootXDG
)

type browserProfileRoot struct {
	kind profileRootKind
	rel  string
}

var (
	defaultBrowsers = []string{"zen", "firefox", "librewolf", "brave", "chromium", "chrome"}

	geckoBrowserRoots = map[string][]browserProfileRoot{
		"zen": {
			{rootHome, ".zen"},
			{rootXDG, "zen"},
			{rootHome, filepath.Join(".var", "app", "app.zen_browser.zen", ".zen")},
			{rootHome, filepath.Join("Library", "Application Support", "zen", "Profiles")},
		},
		"firefox": {
			{rootHome, filepath.Join(".mozilla", "firefox")},
			{rootXDG, filepath.Join("mozilla", "firefox")},
			{rootHome, filepath.Join("snap", "firefox", "common", ".mozilla", "firefox")},
			{rootHome, filepath.Join(".var", "app", "org.mozilla.firefox", ".mozilla", "firefox")},
			{rootHome, filepath.Join("Library", "Application Support", "Firefox", "Profiles")},
		},
		"librewolf": {
			{rootHome, ".librewolf"},
			{rootXDG, "librewolf"},
			{rootHome, filepath.Join(".var", "app", "io.gitlab.librewolf-community", ".librewolf")},
		},
	}

	browserBinaryTargets = map[string][]string{
		"brave":    {"brave-browser", "brave", "/Applications/Brave Browser.app"},
		"chromium": {"chromium", "chromium-browser"},
		"chrome":   {"google-chrome-stable", "google-chrome", "/Applications/Google Chrome.app"},
		"edge":     {"microsoft-edge", "microsoft-edge-stable", "/Applications/Microsoft Edge.app"},
		"vivaldi":  {"vivaldi", "vivaldi-stable"},
	}
)

// BrowserSpecs resolves browser names into yt-dlp --cookies-from-browser specs
// in priority order. When explicitBrowser is non-blank, it returns that trimmed
// spec directly. Profile lookup inspects only homeDir, xdgConfigHome, and lookPath.
func BrowserSpecs(homeDir, xdgConfigHome, explicitBrowser string, browsers []string, lookPath func(string) (string, error)) []string {
	if explicit := strings.TrimSpace(explicitBrowser); explicit != "" {
		return []string{explicit}
	}
	if len(browsers) == 0 {
		browsers = defaultBrowsers
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	home := strings.TrimSpace(homeDir)
	cfg := strings.TrimSpace(xdgConfigHome)
	if cfg == "" && home != "" {
		cfg = filepath.Join(home, ".config")
	}

	var specs []string
	for _, raw := range browsers {
		b := strings.ToLower(strings.TrimSpace(raw))
		if b == "" {
			continue
		}
		if roots, ok := geckoBrowserRoots[b]; ok {
			if prof := detectProfileIn(home, cfg, roots); prof != "" {
				specs = append(specs, "firefox:"+prof)
			}
			continue
		}
		if targets, ok := browserBinaryTargets[b]; ok {
			if hasAnyBrowserTarget(lookPath, targets...) {
				specs = append(specs, b)
			}
			continue
		}
		specs = append(specs, b)
	}
	return specs
}

func detectProfileIn(home, xdg string, roots []browserProfileRoot) string {
	bases := make([]string, 0, len(roots))
	for _, r := range roots {
		switch r.kind {
		case rootHome:
			if home != "" {
				bases = append(bases, filepath.Join(home, r.rel))
			}
		case rootXDG:
			if xdg != "" {
				bases = append(bases, filepath.Join(xdg, r.rel))
			}
		}
	}
	if len(bases) == 0 {
		return ""
	}
	return DetectFirefoxProfile(bases...)
}

func hasAnyBrowserTarget(lookPath func(string) (string, error), targets ...string) bool {
	for _, target := range targets {
		if filepath.IsAbs(target) {
			if _, err := os.Stat(target); err == nil {
				return true
			}
			continue
		}
		if _, err := lookPath(target); err == nil {
			return true
		}
	}
	return false
}

// RefreshJarFromBrowsers probes specs in order using yt-dlp to extract
// authenticated Google/YouTube Netscape cookies, writing the filtered jar
// atomically to cacheDir/cookies.txt (0600) or touching cacheDir/cookies.none
// (0600) if no spec yields login cookies.
func RefreshJarFromBrowsers(ctx context.Context, cacheDir, ytdlpBin string, specs []string) (string, error) {
	if cacheDir == "" || len(specs) == 0 {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	if strings.TrimSpace(ytdlpBin) == "" {
		ytdlpBin = "yt-dlp"
	}

	jarPath := filepath.Join(cacheDir, "cookies.txt")
	nonePath := filepath.Join(cacheDir, "cookies.none")

	for _, rawSpec := range specs {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		spec := strings.TrimSpace(rawSpec)
		if spec == "" {
			continue
		}

		filtered, err := extractBrowserLoginCookies(ctx, cacheDir, ytdlpBin, spec)
		if err != nil {
			return "", err
		}
		if len(filtered) == 0 {
			continue
		}

		if err := writePrivateFileAtomic(cacheDir, jarPath, filtered); err != nil {
			return "", err
		}
		_ = removeIfExists(nonePath)
		_ = touchPrivateFile(filepath.Join(cacheDir, StampFileName))
		return jarPath, nil
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := touchPrivateFile(nonePath); err != nil {
		return "", err
	}
	return "", nil
}

func extractBrowserLoginCookies(ctx context.Context, cacheDir, ytdlpBin, spec string) ([]byte, error) {
	rawFile, err := os.CreateTemp(cacheDir, "cookies.raw.*")
	if err != nil {
		return nil, err
	}
	rawPath := rawFile.Name()
	defer os.Remove(rawPath)

	if err := rawFile.Chmod(0o600); err != nil {
		_ = rawFile.Close()
		return nil, err
	}
	if _, err := rawFile.WriteString(netscapeCookieHeader); err != nil {
		_ = rawFile.Close()
		return nil, err
	}
	if err := rawFile.Close(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(
		ctx,
		ytdlpBin,
		"--cookies-from-browser", spec,
		"--cookies", rawPath,
		"--no-warnings",
		"--skip-download",
		"--flat-playlist",
		"--playlist-items", "1",
		"--print", "id",
		"ytsearch1:youtube",
	)
	// Isolate subprocess group so cancellation kills yt-dlp and any helper children cleanly.
	platform.SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return platform.KillProcessGroup(cmd.Process.Pid)
	}
	cmd.WaitDelay = 100 * time.Millisecond

	_ = cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	rawBytes, err := os.ReadFile(rawPath)
	if err != nil || len(rawBytes) == 0 {
		return nil, nil
	}
	filtered, err := FilterNetscape(bytes.NewReader(rawBytes))
	if err != nil || !HasLogin(filtered) {
		return nil, nil
	}
	return filtered, nil
}

func writePrivateFileAtomic(dir, targetPath string, data []byte) error {
	tmpFile, err := os.CreateTemp(dir, filepath.Base(targetPath)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}
	renamed = true
	return nil
}

func touchPrivateFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
}
