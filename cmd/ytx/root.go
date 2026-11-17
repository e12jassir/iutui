package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/catalog"
	"github.com/e12jassir/ytx/internal/config"
	"github.com/e12jassir/ytx/internal/cookies"
	"github.com/e12jassir/ytx/internal/download"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/platform"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/quick"
	"github.com/e12jassir/ytx/internal/store"
	"github.com/e12jassir/ytx/internal/tui"
	"github.com/e12jassir/ytx/internal/tui/theme"
	"github.com/e12jassir/ytx/internal/tui/thumbs"
	"github.com/spf13/cobra"
)

var version = "0.3.0-dev"

type nativeThumbs struct {
	backend thumbs.Backend
}

func newNativeThumbs() nativeThumbs {
	b := thumbs.DetectBackend(
		os.Getenv("YT_PREVIEW"),
		os.Getenv("KITTY_WINDOW_ID"),
		os.Getenv("TERM"),
		os.Getenv("TERM_PROGRAM"),
	)
	return nativeThumbs{backend: b}
}

func (n nativeThumbs) Render(id string, width, height int) string {
	if n.backend == thumbs.BackendText {
		return thumbs.NewTextRenderer().Render(id, width, height)
	}
	return tui.RenderThumbnailANSI(id, width, height)
}

func (n nativeThumbs) Prefetch(id string) {
	if n.backend != thumbs.BackendText {
		tui.FetchThumbnailAsync(id)
	}
}

func (n nativeThumbs) Backend() thumbs.Backend {
	return n.backend
}

func (n nativeThumbs) RenderBrand(width, height int) string {
	if n.backend != thumbs.BackendKitty {
		return ""
	}
	return tui.RenderBrandLogo(width, height)
}

func newRootCmd() *cobra.Command {
	var quickMode bool

	root := &cobra.Command{
		Use:     "ytx",
		Short:   "YouTube TUI & CLI powered by mpv IPC and yt-dlp",
		Version: version,
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

func resolveCookieJar(cfg config.Config, paths platform.Paths) string {
	if strings.EqualFold(cfg.Cookies, "off") || cfg.Cookies == "0" || strings.EqualFold(cfg.Cookies, "false") || strings.EqualFold(cfg.Cookies, "none") {
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
		jar := filepath.Join(paths.CacheDir, "cookies.txt")
		if info, err := os.Stat(jar); err == nil && !info.IsDir() && info.Size() > 0 {
			return jar
		}
	}
	return ""
}

func resolvePlayTarget(target string, mixFlag bool) (url string, allowPlaylist bool) {
	trimmed := strings.TrimSpace(target)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed, mixFlag || strings.Contains(trimmed, "list=")
	}
	targetURL, allowPlaylist, _ := model.BuildTargetURL(trimmed, mixFlag)
	return targetURL, allowPlaylist
}

func runTUI(paths platform.Paths) error {
	cfgPath := filepath.Join(paths.ConfigDir, "config")
	cfg, _ := config.Load(cfgPath)
	cookieJar := resolveCookieJar(cfg, paths)
	dlDir := cfg.DownloadDir
	if dlDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dlDir = filepath.Join(home, "Videos", "yt")
		}
	}

	bashCat := catalog.NewBashCatalog(paths.CacheDir, paths.StateDir, "")
	ytdlpCat := catalog.NewYTDLPCatalog(paths.CacheDir, time.Duration(cfg.CacheTTL)*time.Second, cfg.YtdlpBin)
	ytdlpCat.CookieJarPath = cookieJar
	cat := catalog.NewHybridCatalog(bashCat, ytdlpCat)

	st := store.NewTSVStore(paths.StateDir)
	eng := player.NewIPCClient(
		player.WithSocketPath(paths.SocketPath),
		player.WithLaunchDefaults(player.LaunchOpts{
			Mode:          cfg.DefaultMode,
			SocketPath:    paths.SocketPath,
			Quality:       cfg.Quality,
			Volume:        cfg.AudioVolume,
			CookieJarPath: cookieJar,
			YtdlpBin:      cfg.YtdlpBin,
			FastPlayback:  true,
		}),
	)
	defer eng.Close()
	dl := download.NewManager(paths.StateDir, dlDir, cfg.YtdlpBin)

	state := app.NewState(app.Dependencies{
		Catalog:        cat,
		Engine:         eng,
		Store:          st,
		Downloader:     dl,
		InitialQuality: cfg.Quality,
		OnQualityChange: func(q int) error {
			return config.SaveVal(cfgPath, "YT_QUALITY", strconv.Itoa(q))
		},
	})

	p := tea.NewProgram(
		teaModel{
			state:  state,
			theme:  theme.DefaultTheme(),
			thumbs: newNativeThumbs(),
		},
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

func runQuickSelector(paths platform.Paths) error {
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
		Short: "Check system dependencies and ytx paths",
		Run: func(cmd *cobra.Command, args []string) {
			paths := platform.ResolvePaths()
			out := cmd.OutOrStdout()
			for _, bin := range []string{"mpv", "yt-dlp", "chafa"} {
				p, err := exec.LookPath(bin)
				if err != nil {
					fmt.Fprintf(out, "missing  %s\n", bin)
				} else {
					fmt.Fprintf(out, "ok       %s (%s)\n", bin, p)
				}
			}
			fmt.Fprintf(out, "config   %s\n", paths.ConfigDir)
			fmt.Fprintf(out, "cache    %s\n", paths.CacheDir)
			fmt.Fprintf(out, "state    %s\n", paths.StateDir)
			fmt.Fprintf(out, "socket   %s\n", paths.SocketPath)
		},
	}
}
