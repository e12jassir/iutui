package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/config"
	"github.com/e12jassir/iutui/internal/cookies"
	"github.com/e12jassir/iutui/internal/download"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/quick"
	"github.com/e12jassir/iutui/internal/store"
	"github.com/e12jassir/iutui/internal/tui"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/e12jassir/iutui/internal/tui/thumbs"
	"github.com/spf13/cobra"
)

var version = "0.5.2"

type nativeThumbs struct {
	mu      sync.RWMutex
	backend thumbs.Backend
}

func newNativeThumbs() *nativeThumbs {
	b := thumbs.DetectBackendEnv(
		os.Getenv("YT_PREVIEW"),
		os.Getenv("KITTY_WINDOW_ID"),
		os.Getenv("TERM"),
		os.Getenv("TERM_PROGRAM"),
		os.Getenv("COLORTERM"),
		os.Getenv("NO_COLOR"),
	)
	return &nativeThumbs{backend: b}
}

func (n *nativeThumbs) SetPreviewMode(mode string) {
	next := thumbs.DetectBackendEnv(
		mode,
		os.Getenv("KITTY_WINDOW_ID"),
		os.Getenv("TERM"),
		os.Getenv("TERM_PROGRAM"),
		os.Getenv("COLORTERM"),
		os.Getenv("NO_COLOR"),
	)
	n.mu.Lock()
	n.backend = next
	n.mu.Unlock()
	tui.ClearThumbCache()
}

func (n *nativeThumbs) Render(id string, width, height int) string {
	if n.Backend() == thumbs.BackendText {
		return thumbs.NewTextRenderer().Render(id, width, height)
	}
	return tui.RenderThumbnailANSI(id, width, height)
}

func (n *nativeThumbs) Prefetch(id string) {
	if n.Backend() != thumbs.BackendText {
		tui.FetchThumbnailAsync(id)
	}
}

func (n *nativeThumbs) Backend() thumbs.Backend {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.backend
}

func (n *nativeThumbs) RenderBrand(width, height int) string {
	if n.Backend() != thumbs.BackendKitty {
		return ""
	}
	return tui.RenderBrandLogo(width, height)
}

func newRootCmd() *cobra.Command {
	var (
		quickMode    bool
		debugLog     bool
		debugLogFile *os.File
	)

	closeDebugLog := func() {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		if debugLogFile != nil {
			_ = debugLogFile.Close()
			debugLogFile = nil
		}
	}

	root := &cobra.Command{
		Use:     "iutui",
		Short:   "YouTube TUI & CLI powered by mpv IPC and yt-dlp",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			closeDebugLog()
			if !debugLog {
				return nil
			}
			paths := platform.ResolvePaths()
			if err := os.MkdirAll(paths.StateDir, 0o755); err != nil {
				return err
			}
			logPath := filepath.Join(paths.StateDir, "debug.log")
			f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				return err
			}
			debugLogFile = f
			handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
			slog.SetDefault(slog.New(handler))
			slog.Info("iutui starting",
				"version", version,
				"state_dir", paths.StateDir,
				"socket_path", paths.SocketPath,
			)
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			closeDebugLog()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := platform.ResolvePaths()
			_, _ = platform.MigrateFromYT(paths)

			if quickMode {
				return runQuickSelector(paths)
			}
			return runTUI(paths)
		},
	}
	root.Flags().BoolVarP(&quickMode, "quick", "q", false, "Open fast interactive selector")
	root.PersistentFlags().BoolVar(&debugLog, "debug-log", false, "Write structured debug logs to $XDG_STATE_HOME/iutui/debug.log")

	root.AddCommand(
		newPlayCmd(),
		newStatusCmd(),
		newSeekCmd(),
		newTransportCmd("pause", "Toggle mpv pause", player.TogglePause),
		newTransportCmd("next", "Skip to next item in queue", player.Next),
		newTransportCmd("prev", "Return to previous item in queue", player.Prev),
		newSubscribeCmd(),
		newChannelsCmd(),
		newQueueCmd(),
		newDoctorCmd(),
	)

	return root
}

func isCookiesDisabled(mode string) bool {
	return strings.EqualFold(mode, "off") || mode == "0" || strings.EqualFold(mode, "false") || strings.EqualFold(mode, "none")
}

func resolveCookieJar(cfg config.Config, paths platform.Paths) string {
	if isCookiesDisabled(cfg.Cookies) {
		return ""
	}
	if cfg.CookiesFile != "" {
		if f, err := os.Open(cfg.CookiesFile); err == nil {
			filtered, ferr := cookies.FilterNetscape(f)
			_ = f.Close()
			if ferr == nil && len(filtered) > 0 && paths.CacheDir != "" {
				if err := os.MkdirAll(paths.CacheDir, 0o700); err == nil {
					jar := filepath.Join(paths.CacheDir, "cookies.txt")
					if err := os.WriteFile(jar, filtered, 0o600); err == nil {
						_ = os.Chmod(jar, 0o600)
						return jar
					}
				}
			}
		}
	}
	if paths.CacheDir != "" {
		candidates := []string{
			filepath.Join(paths.CacheDir, "cookies.txt"),
			filepath.Join(filepath.Dir(paths.CacheDir), "ytx", "cookies.txt"),
			filepath.Join(filepath.Dir(paths.CacheDir), "yt_terminal", "cookies.txt"),
		}
		var bestPath string
		var bestMod time.Time
		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() || info.Size() <= 0 {
				continue
			}
			if bestPath == "" || info.ModTime().After(bestMod) {
				bestPath = candidate
				bestMod = info.ModTime()
			}
		}
		if bestPath != "" {
			return bestPath
		}
	}
	return ""
}

func shouldRefreshCookies(cfg config.Config, cacheDir, currentJar string) bool {
	if cacheDir == "" || isCookiesDisabled(cfg.Cookies) || strings.TrimSpace(cfg.CookiesFile) != "" {
		return false
	}
	if info, err := os.Stat(filepath.Join(cacheDir, "cookies.none")); err == nil && time.Since(info.ModTime()) < 15*time.Minute {
		return false
	}
	ownJar := filepath.Join(cacheDir, "cookies.txt")
	if currentJar == "" || filepath.Clean(currentJar) != filepath.Clean(ownJar) {
		return true
	}
	ttl := time.Duration(cfg.CookieTTL) * time.Second
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	info, err := os.Stat(currentJar)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return true
	}
	refTime := info.ModTime()
	if stampInfo, stampErr := os.Stat(filepath.Join(cacheDir, cookies.StampFileName)); stampErr == nil && !stampInfo.IsDir() {
		refTime = stampInfo.ModTime()
	}
	return time.Since(refTime) >= ttl
}

func startCookieBootstrap(ctx context.Context, cfg config.Config, paths platform.Paths, currentJar string, force bool, onReady func(jarPath string)) {
	if paths.CacheDir == "" || isCookiesDisabled(cfg.Cookies) || strings.TrimSpace(cfg.CookiesFile) != "" {
		return
	}
	if force {
		if err := os.Remove(filepath.Join(paths.CacheDir, "cookies.none")); err != nil && !os.IsNotExist(err) {
			slog.Debug("remove stale cookies.none marker", "err", err)
		}
	} else if !shouldRefreshCookies(cfg, paths.CacheDir, currentJar) {
		return
	}
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return
	}
	specs := cookies.BrowserSpecs(homeDir, os.Getenv("XDG_CONFIG_HOME"), cfg.Browser, cfg.Browsers, exec.LookPath)
	if len(specs) == 0 {
		return
	}
	go func() {
		bootCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		jar, err := cookies.RefreshJarFromBrowsers(bootCtx, paths.CacheDir, cfg.YtdlpBin, specs)
		if err != nil {
			slog.Warn("cookie bootstrap failed", "err", err)
			return
		}
		if jar == "" {
			slog.Debug("cookie bootstrap found no logged-in browser cookies", "specs", specs)
			return
		}
		slog.Info("cookie bootstrap ready", "jar", jar)
		if onReady != nil {
			onReady(jar)
		}
	}()
}

func resolvePlayTarget(target string, mixFlag bool) (url string, allowPlaylist bool) {
	trimmed := strings.TrimSpace(target)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed, mixFlag || strings.Contains(trimmed, "list=")
	}
	targetURL, allowPlaylist, _ := model.BuildTargetURL(trimmed, mixFlag)
	return targetURL, allowPlaylist
}

func missingRequiredBins(lookPath func(string) (string, error), bins ...string) []string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var missing []string
	for _, bin := range bins {
		if _, err := lookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	return missing
}

func installDepsHint(goos, osRelease string, deps []string) string {
	if len(deps) == 0 {
		return ""
	}
	pkgList := strings.Join(deps, " ")
	switch goos {
	case "darwin":
		return "brew install " + pkgList
	case "linux":
		var idTokens, idLikeTokens []string
		for _, line := range strings.Split(osRelease, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.Trim(strings.TrimSpace(val), `"'`)
			fields := strings.Fields(strings.ToLower(val))
			if strings.EqualFold(key, "ID") {
				idTokens = append(idTokens, fields...)
			} else if strings.EqualFold(key, "ID_LIKE") {
				idLikeTokens = append(idLikeTokens, fields...)
			}
		}
		for _, tok := range append(idTokens, idLikeTokens...) {
			switch {
			case tok == "arch" || tok == "manjaro" || tok == "endeavouros" || tok == "cachyos" || tok == "artix":
				return "sudo pacman -S --needed " + pkgList
			case tok == "ubuntu" || tok == "debian" || tok == "pop" || tok == "linuxmint" || tok == "zorin" || tok == "elementary":
				return "sudo apt install " + pkgList
			case tok == "fedora" || tok == "rhel" || tok == "centos" || tok == "rocky" || tok == "almalinux":
				return "sudo dnf install " + pkgList
			case tok == "opensuse" || strings.HasPrefix(tok, "opensuse-") || tok == "suse" || tok == "sles":
				return "sudo zypper install " + pkgList
			case tok == "alpine":
				return "sudo apk add " + pkgList
			}
		}
		return "install " + pkgList + " using your package manager"
	default:
		return "install " + pkgList + " (see https://mpv.io and https://github.com/yt-dlp/yt-dlp)"
	}
}

func checkRequiredDeps(bins ...string) error {
	missing := missingRequiredBins(exec.LookPath, bins...)
	if len(missing) == 0 {
		return nil
	}
	raw, _ := os.ReadFile("/etc/os-release")
	hint := installDepsHint(runtime.GOOS, string(raw), missing)
	return fmt.Errorf("missing required dependencies (%s)\nInstall with: %s\nOr run: curl -fsSL https://raw.githubusercontent.com/e12jassir/iutui/main/install.sh | bash", strings.Join(missing, ", "), hint)
}

func runTUI(paths platform.Paths) error {
	if err := checkRequiredDeps("mpv", "yt-dlp"); err != nil {
		return err
	}
	cfgPath := filepath.Join(paths.ConfigDir, "config")
	cfg, _ := config.Load(cfgPath)
	tui.SetThumbQuality(cfg.ThumbQuality)
	cookieJar := resolveCookieJar(cfg, paths)
	bootCtx, bootCancel := context.WithCancel(context.Background())
	defer bootCancel()

	dlDir := cfg.DownloadDir
	if dlDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dlDir = filepath.Join(home, "Videos", "yt")
		}
	}

	bashCat := catalog.NewBashCatalog(paths.CacheDir, paths.StateDir, "")
	ytdlpCat := catalog.NewYTDLPCatalog(paths.CacheDir, time.Duration(cfg.CacheTTL)*time.Second, cfg.YtdlpBin)
	ytdlpCat.FeedSize = cfg.FeedSize
	ytdlpCat.SearchSize = cfg.SearchSize
	catalogCookieJar := cookieJar
	if catalogCookieJar == "" && !isCookiesDisabled(cfg.Cookies) && paths.CacheDir != "" {
		catalogCookieJar = filepath.Join(paths.CacheDir, "cookies.txt")
	}
	ytdlpCat.CookieJarPath = catalogCookieJar
	cat := catalog.NewHybridCatalog(bashCat, ytdlpCat)
	cat.SetDisableCookieAuthSubs(isCookiesDisabled(cfg.Cookies))

	st := store.NewTSVStore(paths.StateDir)
	cat.SubscriptionsProvider = st.Subscriptions
	eng := player.NewIPCClient(
		player.WithSocketPath(paths.SocketPath),
		player.WithLaunchDefaults(player.LaunchOpts{
			Mode:          cfg.DefaultMode,
			SocketPath:    paths.SocketPath,
			Quality:       cfg.Quality,
			Volume:        cfg.AudioVolume,
			Speed:         cfg.Speed,
			CookieJarPath: cookieJar,
			YtdlpBin:      cfg.YtdlpBin,
			FastPlayback:  true,
		}),
	)
	defer eng.Close()
	eng.SetSponsorBlock(cfg.SponsorBlock)
	eng.SetAutoplay(cfg.Autoplay)
	dl := download.NewManager(paths.StateDir, dlDir, cfg.YtdlpBin)
	resumeStore := store.NewResumeStore(paths.StateDir)
	searchHist := store.NewSearchHistory(paths.StateDir)
	suggestClient := catalog.NewSuggestClient()
	thumbRenderer := newNativeThumbs()

	sbEnabled := cfg.SponsorBlock
	hideShorts := cfg.HideShorts
	interleave := cfg.Interleave
	autoplay := cfg.Autoplay
	cacheTTL := cfg.CacheTTL
	dlAudio := cfg.DownloadAudio
	var p *tea.Program
	var programDone atomic.Bool
	var cookiesDisabled atomic.Bool
	cookiesDisabled.Store(isCookiesDisabled(cfg.Cookies))
	notifyCookiesReady := func(jar string, reloadFeed bool) {
		if jar == "" {
			return
		}
		ytdlpCat.SetCookieJar(jar)
		eng.SetCookieJar(jar)
		if reloadFeed && paths.CacheDir != "" {
			for _, name := range []string{"feed_home.json", "feed_music.json", "feed_subs.json"} {
				_ = os.Remove(filepath.Join(paths.CacheDir, name))
			}
		}
		if p != nil && !programDone.Load() {
			p.Send(app.CookiesReadyMsg{JarPath: jar, ReloadFeed: reloadFeed})
		}
	}
	var lastStaleRefresh atomic.Int64
	ytdlpCat.OnCookiesStale = func() {
		if cookiesDisabled.Load() || strings.TrimSpace(cfg.CookiesFile) != "" {
			return
		}
		now := time.Now().Unix()
		prev := lastStaleRefresh.Load()
		if prev != 0 && now-prev < 120 {
			return
		}
		if !lastStaleRefresh.CompareAndSwap(prev, now) {
			return
		}
		slog.Info("authenticated feed returned empty; refreshing browser cookies")
		startCookieBootstrap(bootCtx, cfg, paths, "", true, func(jar string) {
			notifyCookiesReady(jar, true)
		})
	}
	lyricsClient := lyrics.NewClient()
	lyricsClient.EnableYouTubeCaptions(paths.CacheDir)
	state := app.NewState(app.Dependencies{
		Catalog:              cat,
		Engine:               eng,
		Store:                st,
		Downloader:           dl,
		LyricsClient:         lyricsClient,
		SuggestProvider:      suggestClient,
		SearchHistory:        searchHist,
		ResumeStore:          resumeStore,
		InitialQuality:       cfg.Quality,
		InitialAudioOnly:     cfg.DefaultMode == "audio",
		InitialDefaultMode:   cfg.DefaultMode,
		InitialVolume:        cfg.AudioVolume,
		InitialPreview:       cfg.Preview,
		InitialHideShorts:    &hideShorts,
		InitialInterleave:    &interleave,
		InitialCookies:       cfg.Cookies,
		InitialTheme:         cfg.Theme,
		InitialSponsorBlock:  &sbEnabled,
		InitialThumbQuality:  cfg.ThumbQuality,
		InitialAutoplay:      &autoplay,
		InitialSpeed:         cfg.Speed,
		InitialFeedSize:      cfg.FeedSize,
		InitialSearchSize:    cfg.SearchSize,
		InitialCacheTTL:      &cacheTTL,
		InitialDownloadAudio: &dlAudio,
		AppVersion:           version,
		OnQualityChange: func(q int) error {
			ytdlpCat.SetQuality(q)
			return config.SaveVal(cfgPath, "YT_QUALITY", strconv.Itoa(q))
		},
		OnAudioOnlyChange: func(audioOnly bool) error {
			mode := "window"
			if audioOnly {
				mode = "audio"
			}
			return config.SaveVal(cfgPath, "YT_DEFAULT_MODE", mode)
		},
		OnThemeChange: func(name string) error {
			return config.SaveVal(cfgPath, "YT_THEME", name)
		},
		OnSponsorBlockChange: func(enabled bool) error {
			val := "0"
			if enabled {
				val = "1"
			}
			return config.SaveVal(cfgPath, "YT_SPONSORBLOCK", val)
		},
		OnConfigChange: func(key, val string) error {
			if err := config.SaveVal(cfgPath, key, val); err != nil {
				return err
			}
			switch key {
			case "YT_PREVIEW":
				if val == "auto" {
					_ = os.Unsetenv("YT_PREVIEW")
				} else {
					_ = os.Setenv("YT_PREVIEW", val)
				}
				thumbRenderer.SetPreviewMode(val)
			case "YT_THUMB_QUALITY":
				tui.SetThumbQuality(val)
				tui.ClearThumbCache()
			case "YT_FEED_SIZE":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					ytdlpCat.SetFeedSize(n)
				}
			case "YT_SEARCH_SIZE":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					ytdlpCat.SetSearchSize(n)
				}
			case "YT_CACHE_TTL":
				if n, err := strconv.Atoi(val); err == nil && n >= 0 {
					ytdlpCat.SetCacheTTL(time.Duration(n) * time.Second)
				}
			case "YT_COOKIES":
				cfgNext := cfg
				cfgNext.Cookies = val
				if isCookiesDisabled(val) {
					cookiesDisabled.Store(true)
					ytdlpCat.SetCookieJar("")
					cat.SetDisableCookieAuthSubs(true)
					eng.SetCookieJar("")
				} else {
					cookiesDisabled.Store(false)
					nextJar := resolveCookieJar(cfgNext, paths)
					catJar := nextJar
					if catJar == "" && paths.CacheDir != "" {
						catJar = filepath.Join(paths.CacheDir, "cookies.txt")
					}
					ytdlpCat.SetCookieJar(catJar)
					cat.SetDisableCookieAuthSubs(false)
					eng.SetCookieJar(nextJar)
					if nextJar != "" {
						go func() {
							notifyCookiesReady(nextJar, true)
						}()
						startCookieBootstrap(bootCtx, cfgNext, paths, nextJar, false, func(jar string) {
							notifyCookiesReady(jar, true)
						})
					} else {
						startCookieBootstrap(bootCtx, cfgNext, paths, "", true, func(jar string) {
							notifyCookiesReady(jar, true)
						})
					}
				}
			}
			return nil
		},
	})

	initTheme := theme.DefaultTheme()
	if cfg.Theme != "" && !strings.EqualFold(cfg.Theme, "youtube-dark") {
		initTheme = theme.ThemeFromPalette(theme.ByName(cfg.Theme))
	}

	p = tea.NewProgram(
		teaModel{
			state:  state,
			theme:  initTheme,
			thumbs: thumbRenderer,
		},
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
		tea.WithFilter(app.NewBubbleTeaEscapeFilter()),
	)
	startCookieBootstrap(bootCtx, cfg, paths, cookieJar, false, func(jar string) {
		notifyCookiesReady(jar, true)
	})
	dl.OnProgress = func(it download.Item) {
		if !programDone.Load() {
			p.Send(app.DownloadProgressMsg{Item: it})
		}
	}
	defer func() { dl.OnProgress = nil }()

	mprisCtx, mprisCancel := context.WithCancel(context.Background())
	mpris := platform.NewMPRISBridge(platform.MPRISCallbacks{
		OnPlayPause: func() {
			ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
			defer cancel()
			_ = eng.TogglePause(ctx)
		},
		OnPlay: func() {
			if eng.Status().Paused {
				ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
				defer cancel()
				_ = eng.TogglePause(ctx)
			}
		},
		OnPause: func() {
			ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
			defer cancel()
			_ = eng.Pause(ctx)
		},
		OnNext: func() {
			ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
			defer cancel()
			_ = eng.Next(ctx)
		},
		OnPrevious: func() {
			ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
			defer cancel()
			_ = eng.Prev(ctx)
		},
		OnSeek: func(d time.Duration) {
			ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
			defer cancel()
			_ = eng.Seek(ctx, int(d.Seconds()))
		},
		OnStop: func() {
			ctx, cancel := context.WithTimeout(mprisCtx, 2*time.Second)
			defer cancel()
			_ = eng.Pause(ctx)
		},
	})
	defer mpris.Close()
	defer mprisCancel()
	if mpris.Active() {
		slog.Info("mpris bridge status", "active", true)
		mprisCh := eng.Subscribe(mprisCtx)
		go func() {
			for st := range mprisCh {
				mpris.UpdateStatus(platform.MPRISStatus{
					IsRunning: st.IsRunning,
					Paused:    st.Paused,
					Title:     st.Title,
					Duration:  st.Duration,
					Position:  st.TimePos,
					Volume:    st.Volume,
					Rate:      st.Speed,
					TrackID:   st.Title,
				})
			}
		}()
	} else {
		slog.Debug("mpris bridge status", "active", false)
	}

	slog.Debug("tui starting",
		"version", version,
		"theme", cfg.Theme,
		"quality", cfg.Quality,
		"default_mode", cfg.DefaultMode,
		"has_cookies", cookieJar != "",
	)

	tui.SetOnThumbReady(func(id string) {
		if !programDone.Load() {
			p.Send(app.ThumbReadyMsg{ID: id})
		}
	})
	defer tui.SetOnThumbReady(nil)
	defer programDone.Store(true)
	_, err := p.Run()
	programDone.Store(true)
	slog.Info("tui exited", "err", err)
	return err
}

func runQuickSelector(paths platform.Paths) error {
	if err := checkRequiredDeps("mpv", "yt-dlp"); err != nil {
		return err
	}
	cfgPath := filepath.Join(paths.ConfigDir, "config")
	cfg, _ := config.Load(cfgPath)
	cookieJar := resolveCookieJar(cfg, paths)

	bashCat := catalog.NewBashCatalog(paths.CacheDir, paths.StateDir, "")
	ytdlpCat := catalog.NewYTDLPCatalog(paths.CacheDir, time.Duration(cfg.CacheTTL)*time.Second, cfg.YtdlpBin)
	ytdlpCat.CookieJarPath = cookieJar
	cat := catalog.NewHybridCatalog(bashCat, ytdlpCat)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	videos, err := cat.Feed(ctx, model.TabHome)
	if err != nil {
		return err
	}
	chosen, ok, err := quick.Run(videos)
	if err != nil || !ok {
		return err
	}
	targetURL, allowPlaylist, _ := model.BuildTargetURL(chosen.ID, false)
	return player.LaunchMPV("mpv", player.LaunchOpts{
		VideoID:       chosen.ID,
		URL:           targetURL,
		AllowPlaylist: allowPlaylist,
		Mode:          cfg.DefaultMode,
		SocketPath:    paths.SocketPath,
		Quality:       cfg.Quality,
		Volume:        cfg.AudioVolume,
		Speed:         cfg.Speed,
		CookieJarPath: cookieJar,
		YtdlpBin:      cfg.YtdlpBin,
		FastPlayback:  true,
	})
}

func newPlayCmd() *cobra.Command {
	var audioOnly, pip, fs, mixMode bool
	cmd := &cobra.Command{
		Use:   "play <video-id-or-url>",
		Short: "Launch playback in mpv",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkRequiredDeps("mpv"); err != nil {
				return err
			}
			paths := platform.ResolvePaths()
			cfgPath := filepath.Join(paths.ConfigDir, "config")
			cfg, _ := config.Load(cfgPath)
			cookieJar := resolveCookieJar(cfg, paths)

			mode := cfg.DefaultMode
			if audioOnly {
				mode = "audio"
			} else if pip {
				mode = "pip"
			} else if fs {
				mode = "fs"
			}

			targetURL, allowPlaylist := resolvePlayTarget(args[0], mixMode)

			return player.LaunchMPV("mpv", player.LaunchOpts{
				URL:           targetURL,
				AllowPlaylist: allowPlaylist,
				Mode:          mode,
				SocketPath:    paths.SocketPath,
				Quality:       cfg.Quality,
				Volume:        cfg.AudioVolume,
				Speed:         cfg.Speed,
				CookieJarPath: cookieJar,
				YtdlpBin:      cfg.YtdlpBin,
				FastPlayback:  true,
			})
		},
	}
	cmd.Flags().BoolVarP(&audioOnly, "audio", "a", false, "Audio-only playback")
	cmd.Flags().BoolVar(&pip, "pip", false, "Picture-in-picture window")
	cmd.Flags().BoolVar(&fs, "fs", false, "Fullscreen playback")
	cmd.Flags().BoolVarP(&mixMode, "mix", "m", false, "Play YouTube Mix radio queue")
	return cmd
}

func newStatusCmd() *cobra.Command {
	var asJSON, asWaybar bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current mpv playback status",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := player.GetStatus()
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(st)
			}
			if asWaybar {
				text := ""
				cls := "idle"
				if st.IsRunning {
					icon := "▶"
					cls = "playing"
					if st.Paused {
						icon = "⏸"
						cls = "paused"
					}
					text = fmt.Sprintf("%s %s", icon, st.Title)
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"text":  text,
					"class": cls,
				})
			}
			if !st.IsRunning {
				fmt.Fprintln(cmd.OutOrStdout(), "idle")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s (%s / %s)\n", st.Title, player.FormatTime(st.TimePos), player.FormatTime(st.Duration))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	cmd.Flags().BoolVar(&asWaybar, "waybar", false, "Output Waybar JSON")
	return cmd
}

func newSeekCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seek <seconds>",
		Short: "Seek relative seconds in mpv",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid seconds %q: %w", args[0], err)
			}
			return player.Seek(sec)
		},
	}
}

func newTransportCmd(use, short string, fn func() error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fn()
		},
	}
}

func newSubscribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "subscribe <channel-id-or-url>",
		Short: "Subscribe to a YouTube channel via RSS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := platform.ResolvePaths()
			feedURL := catalog.ChannelFeedURL(args[0])
			if feedURL == "" {
				return fmt.Errorf("invalid channel reference: %s", args[0])
			}
			if err := os.MkdirAll(paths.StateDir, 0o755); err != nil {
				return err
			}
			subsPath := filepath.Join(paths.StateDir, "subscriptions.tsv")
			f, err := os.OpenFile(subsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = fmt.Fprintf(f, "%s\n", args[0])
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "subscribed: %s\n", args[0])
			}
			return err
		},
	}
}

func newChannelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "channels",
		Short: "List subscribed channels",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := platform.ResolvePaths()
			st := store.NewTSVStore(paths.StateDir)
			subs, err := st.Subscriptions(context.Background())
			if err != nil {
				return err
			}
			for _, s := range subs {
				fmt.Fprintln(cmd.OutOrStdout(), s)
			}
			return nil
		},
	}
}

func newQueueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Show download queue and saved downloads",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := platform.ResolvePaths()
			cfgPath := filepath.Join(paths.ConfigDir, "config")
			cfg, _ := config.Load(cfgPath)
			mgr := download.NewManager(paths.StateDir, cfg.DownloadDir, cfg.YtdlpBin)
			items, err := mgr.List()
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(empty)")
				return nil
			}
			for _, it := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", it.Video.ID, it.Status, it.Video.Title)
			}
			return nil
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system dependencies and iutui paths",
		Run: func(cmd *cobra.Command, args []string) {
			paths := platform.ResolvePaths()
			out := cmd.OutOrStdout()
			missingReq := []string{}
			for _, bin := range []string{"mpv", "yt-dlp"} {
				p, err := exec.LookPath(bin)
				if err != nil {
					fmt.Fprintf(out, "missing  %s\n", bin)
					missingReq = append(missingReq, bin)
				} else {
					fmt.Fprintf(out, "ok       %s (%s)\n", bin, p)
				}
			}
			if len(missingReq) > 0 {
				osRel, _ := os.ReadFile("/etc/os-release")
				fmt.Fprintf(out, "hint     %s\n", installDepsHint(runtime.GOOS, string(osRel), missingReq))
			}
			cfgPath := filepath.Join(paths.ConfigDir, "config")
			cfg, _ := config.Load(cfgPath)
			previewMode := os.Getenv("YT_PREVIEW")
			if previewMode == "" {
				previewMode = cfg.Preview
			}
			switch thumbs.DetectBackendEnv(
				previewMode,
				os.Getenv("KITTY_WINDOW_ID"),
				os.Getenv("TERM"),
				os.Getenv("TERM_PROGRAM"),
				os.Getenv("COLORTERM"),
				os.Getenv("NO_COLOR"),
			) {
			case thumbs.BackendKitty:
				fmt.Fprintf(out, "thumbs   ok (native kitty HD)\n")
			case thumbs.BackendChafa:
				fmt.Fprintf(out, "thumbs   ok (native ansi blocks)\n")
			default:
				fmt.Fprintf(out, "thumbs   text (plain terminal)\n")
			}
			fmt.Fprintf(out, "config   %s\n", paths.ConfigDir)
			fmt.Fprintf(out, "cache    %s\n", paths.CacheDir)
			fmt.Fprintf(out, "state    %s\n", paths.StateDir)
			fmt.Fprintf(out, "socket   %s\n", paths.SocketPath)

			if isCookiesDisabled(cfg.Cookies) {
				fmt.Fprintf(out, "cookies  off\n")
			} else if jar := resolveCookieJar(cfg, paths); jar != "" {
				fmt.Fprintf(out, "cookies  ok (%s)\n", jar)
			} else {
				homeDir, _ := os.UserHomeDir()
				specs := cookies.BrowserSpecs(homeDir, os.Getenv("XDG_CONFIG_HOME"), cfg.Browser, cfg.Browsers, exec.LookPath)
				fmt.Fprintf(out, "cookies  none (browsers: %s)\n", strings.Join(specs, ", "))
			}
		},
	}
}
