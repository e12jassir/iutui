package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/config"
	"github.com/e12jassir/iutui/internal/cookies"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/store"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/e12jassir/iutui/internal/tui/thumbs"
)

func TestCobraSubcommandsRegistered(t *testing.T) {
	root := newRootCmd()
	want := []string{"play", "status", "seek", "pause", "next", "prev", "subscribe", "channels", "queue", "doctor"}
	for _, name := range want {
		sub, _, err := root.Find([]string{name})
		if err != nil || sub == nil || sub.Name() != name {
			t.Errorf("expected subcommand %q to be registered (err=%v)", name, err)
		}
	}
}

func TestDoctorCommand(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "runtime"))

	cases := []struct {
		name       string
		ytPreview  string
		kittyWinID string
		term       string
		termProg   string
		colorTerm  string
		noColor    string
		wantThumb  string
	}{
		{
			name:       "kitty HD",
			kittyWinID: "1",
			term:       "xterm-kitty",
			wantThumb:  "thumbs   ok (native kitty HD)",
		},
		{
			name:      "native ansi blocks",
			term:      "alacritty",
			colorTerm: "truecolor",
			wantThumb: "thumbs   ok (native ansi blocks)",
		},
		{
			name:      "plain text",
			term:      "dumb",
			wantThumb: "thumbs   text (plain terminal)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("YT_PREVIEW", tc.ytPreview)
			t.Setenv("KITTY_WINDOW_ID", tc.kittyWinID)
			t.Setenv("TERM", tc.term)
			t.Setenv("TERM_PROGRAM", tc.termProg)
			t.Setenv("COLORTERM", tc.colorTerm)
			t.Setenv("NO_COLOR", tc.noColor)

			root := newRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"doctor"})
			if err := root.Execute(); err != nil {
				t.Fatalf("doctor command failed: %v", err)
			}
			got := out.String()
			for _, want := range []string{"config", "cache", "state", "socket", "cookies", tc.wantThumb} {
				if !strings.Contains(got, want) {
					t.Errorf("doctor output missing %q:\n%s", want, got)
				}
			}
			if strings.Contains(got, "missing  chafa") {
				t.Errorf("doctor output should not check for chafa binary:\n%s", got)
			}
		})
	}
}

func TestTeatestSearchFilterAndPlayFlow(t *testing.T) {
	cat := &catalog.FakeCatalog{
		Feeds: map[model.Tab][]model.Video{
			model.TabHome: {
				{ID: "home001", Title: "Home Video", Channel: "HomeChan", Duration: "05:00"},
			},
		},
		Results: map[string][]model.Video{
			"soda": {
				{ID: "soda001", Title: "De Musica Ligera", Channel: "Soda Stereo", Duration: "04:50"},
				{ID: "sodalive", Title: "Soda En Vivo", Channel: "Soda Stereo", IsLive: true},
			},
		},
	}
	eng := player.NewFakeEngine()
	st := store.NewFakeStore()
	state := app.NewState(app.Dependencies{Catalog: cat, Engine: eng, Store: st})

	m := teaModel{
		state:  state,
		theme:  theme.DefaultTheme(),
		thumbs: thumbs.NewTextRenderer(),
	}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 36))

	// Wait for initial home frame
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Home Video"))
	}, teatest.WithDuration(3*time.Second))

	// Search for "soda"
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	tm.Type("soda")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("De Musica Ligera"))
	}, teatest.WithDuration(3*time.Second))

	// Move down to the second search result ("Soda En Vivo") with 'j' (Search hides filter chips)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Soda En Vivo"))
	}, teatest.WithDuration(3*time.Second))

	// Play selected live video with Enter, then Esc back to Home, then Quit with 'Q'
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})

	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

	calls := eng.Calls()
	played := false
	for _, c := range calls {
		if c.Method == "Play" && c.Video.ID == "sodalive" {
			played = true
			break
		}
	}
	if !played {
		t.Fatalf("expected FakeEngine.Play(sodalive), got calls: %+v", calls)
	}
}

func TestResolveCookieJar(t *testing.T) {
	t.Run("disabled cookie modes return empty", func(t *testing.T) {
		for _, mode := range []string{"off", "0", "false", "none", "OFF"} {
			dir := t.TempDir()
			jar := filepath.Join(dir, "cookies.txt")
			if err := os.WriteFile(jar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tLOGIN_INFO\tok\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got := resolveCookieJar(config.Config{Cookies: mode}, platform.Paths{CacheDir: dir})
			if got != "" {
				t.Fatalf("Cookies=%q got %q, want empty", mode, got)
			}
		}
	})

	t.Run("filters CookiesFile and writes private 0600 cache jar", func(t *testing.T) {
		tmp := t.TempDir()
		src := filepath.Join(tmp, "raw-cookies.txt")
		raw := strings.Join([]string{
			"# Netscape HTTP Cookie File",
			".evil.example\tTRUE\t/\tTRUE\t0\tSTEAL\tno",
			".youtube.com\tTRUE\t/\tTRUE\t0\tLOGIN_INFO\tyes",
		}, "\n") + "\n"
		if err := os.WriteFile(src, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		cacheDir := filepath.Join(tmp, "cache")
		got := resolveCookieJar(config.Config{Cookies: "auto", CookiesFile: src}, platform.Paths{CacheDir: cacheDir})
		want := filepath.Join(cacheDir, "cookies.txt")
		if got != want {
			t.Fatalf("resolveCookieJar = %q, want %q", got, want)
		}
		info, err := os.Stat(got)
		if err != nil {
			t.Fatalf("stat jar: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("jar perm = %o, want 0600", info.Mode().Perm())
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "evil.example") || !strings.Contains(string(data), ".youtube.com") {
			t.Fatalf("unexpected filtered cookie jar content:\n%s", string(data))
		}
	})

	t.Run("uses existing non-empty cache cookies.txt", func(t *testing.T) {
		cacheDir := t.TempDir()
		jar := filepath.Join(cacheDir, "cookies.txt")
		if err := os.WriteFile(jar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tabc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := resolveCookieJar(config.Config{Cookies: "auto"}, platform.Paths{CacheDir: cacheDir})
		if got != jar {
			t.Fatalf("resolveCookieJar = %q, want %q", got, jar)
		}
	})

	t.Run("ignores empty cache cookies.txt", func(t *testing.T) {
		cacheDir := t.TempDir()
		jar := filepath.Join(cacheDir, "cookies.txt")
		if err := os.WriteFile(jar, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		got := resolveCookieJar(config.Config{Cookies: "auto"}, platform.Paths{CacheDir: cacheDir})
		if got != "" {
			t.Fatalf("resolveCookieJar = %q, want empty for zero-byte jar", got)
		}
	})

	t.Run("falls back to sibling ytx or yt_terminal cookies.txt when iutui jar missing", func(t *testing.T) {
		base := t.TempDir()
		iutuiCache := filepath.Join(base, "iutui")
		ytxCache := filepath.Join(base, "ytx")
		ytTermCache := filepath.Join(base, "yt_terminal")
		for _, d := range []string{iutuiCache, ytxCache, ytTermCache} {
			if err := os.MkdirAll(d, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		ytxJar := filepath.Join(ytxCache, "cookies.txt")
		if err := os.WriteFile(ytxJar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tytx\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := resolveCookieJar(config.Config{Cookies: "auto"}, platform.Paths{CacheDir: iutuiCache})
		if got != ytxJar {
			t.Fatalf("resolveCookieJar = %q, want ytx sibling %q", got, ytxJar)
		}

		sibJar := filepath.Join(ytTermCache, "cookies.txt")
		if err := os.WriteFile(sibJar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tsibling\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		older := time.Now().Add(-1 * time.Hour)
		_ = os.Chtimes(ytxJar, older, older)
		got = resolveCookieJar(config.Config{Cookies: "auto"}, platform.Paths{CacheDir: iutuiCache})
		if got != sibJar {
			t.Fatalf("resolveCookieJar = %q, want newer sibling %q", got, sibJar)
		}
	})

	t.Run("prefers newer non-empty sibling yt_terminal cookies.txt over older ytx jar", func(t *testing.T) {
		base := t.TempDir()
		ytxCache := filepath.Join(base, "ytx")
		ytTermCache := filepath.Join(base, "yt_terminal")
		if err := os.MkdirAll(ytxCache, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(ytTermCache, 0o700); err != nil {
			t.Fatal(err)
		}
		ytxJar := filepath.Join(ytxCache, "cookies.txt")
		sibJar := filepath.Join(ytTermCache, "cookies.txt")
		if err := os.WriteFile(ytxJar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\told\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sibJar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tnew\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		older := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(ytxJar, older, older); err != nil {
			t.Fatal(err)
		}
		got := resolveCookieJar(config.Config{Cookies: "auto"}, platform.Paths{CacheDir: ytxCache})
		if got != sibJar {
			t.Fatalf("resolveCookieJar = %q, want newer sibling %q", got, sibJar)
		}
	})
}

func TestShouldRefreshCookies(t *testing.T) {
	t.Run("skips refresh when disabled or manual CookiesFile set", func(t *testing.T) {
		dir := t.TempDir()
		if shouldRefreshCookies(config.Config{Cookies: "off"}, dir, "") {
			t.Fatal("expected false when Cookies=off")
		}
		if shouldRefreshCookies(config.Config{Cookies: "auto", CookiesFile: "/tmp/manual.txt"}, dir, "") {
			t.Fatal("expected false when CookiesFile is configured")
		}
	})

	t.Run("probes when jar missing and cookies.none absent or stale, skips when cookies.none fresh", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.Config{Cookies: "auto", CookieTTL: 21600}
		if !shouldRefreshCookies(cfg, dir, "") {
			t.Fatal("expected true on brand-new cache dir with no jar and no cookies.none")
		}

		nonePath := filepath.Join(dir, "cookies.none")
		if err := os.WriteFile(nonePath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if shouldRefreshCookies(cfg, dir, "") {
			t.Fatal("expected false when cookies.none is fresh")
		}

		old := time.Now().Add(-20 * time.Minute)
		if err := os.Chtimes(nonePath, old, old); err != nil {
			t.Fatal(err)
		}
		if !shouldRefreshCookies(cfg, dir, "") {
			t.Fatal("expected true once cookies.none is older than 15m")
		}
	})

	t.Run("refreshes only when existing jar exceeds CookieTTL", func(t *testing.T) {
		dir := t.TempDir()
		jar := filepath.Join(dir, "cookies.txt")
		if err := os.WriteFile(jar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{Cookies: "auto", CookieTTL: 3600}
		if shouldRefreshCookies(cfg, dir, jar) {
			t.Fatal("expected false when jar is fresh")
		}

		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(jar, old, old); err != nil {
			t.Fatal(err)
		}
		if !shouldRefreshCookies(cfg, dir, jar) {
			t.Fatal("expected true when jar is older than CookieTTL")
		}
	})

	t.Run("refreshes immediately when currentJar is a legacy sibling outside cacheDir", func(t *testing.T) {
		cacheDir := t.TempDir()
		siblingDir := t.TempDir()
		siblingJar := filepath.Join(siblingDir, "cookies.txt")
		if err := os.WriteFile(siblingJar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{Cookies: "auto", CookieTTL: 3600}
		if !shouldRefreshCookies(cfg, cacheDir, siblingJar) {
			t.Fatal("expected true when currentJar is a legacy sibling outside cacheDir")
		}
	})

	t.Run("uses cookies.stamp mtime over yt-dlp-mutated cookies.txt mtime", func(t *testing.T) {
		dir := t.TempDir()
		jar := filepath.Join(dir, "cookies.txt")
		if err := os.WriteFile(jar, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stampPath := filepath.Join(dir, cookies.StampFileName)
		if err := os.WriteFile(stampPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(stampPath, old, old); err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{Cookies: "auto", CookieTTL: 3600}
		if !shouldRefreshCookies(cfg, dir, jar) {
			t.Fatal("expected true when cookies.stamp is older than CookieTTL even though cookies.txt is fresh")
		}

		now := time.Now()
		if err := os.Chtimes(stampPath, now, now); err != nil {
			t.Fatal(err)
		}
		if shouldRefreshCookies(cfg, dir, jar) {
			t.Fatal("expected false when cookies.stamp is fresh")
		}
	})
}

func TestResolvePlayTargetAndMixFlag(t *testing.T) {
	tests := []struct {
		name              string
		target            string
		mixFlag           bool
		wantURL           string
		wantAllowPlaylist bool
	}{
		{
			name:              "regular video id",
			target:            "dQw4w9WgXcQ",
			mixFlag:           false,
			wantURL:           "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantAllowPlaylist: false,
		},
		{
			name:              "regular video id with --mix",
			target:            "dQw4w9WgXcQ",
			mixFlag:           true,
			wantURL:           "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ",
			wantAllowPlaylist: true,
		},
		{
			name:              "seeded mix RD id auto-detected",
			target:            "RDdQw4w9WgXcQ",
			mixFlag:           false,
			wantURL:           "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ",
			wantAllowPlaylist: true,
		},
		{
			name:              "playlist PL id auto-detected",
			target:            "PL1234567890",
			mixFlag:           false,
			wantURL:           "https://www.youtube.com/playlist?list=PL1234567890",
			wantAllowPlaylist: true,
		},
		{
			name:              "full URL with list param enables playlist",
			target:            "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ",
			mixFlag:           false,
			wantURL:           "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ",
			wantAllowPlaylist: true,
		},
		{
			name:              "full URL without list stays single video",
			target:            "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			mixFlag:           false,
			wantURL:           "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantAllowPlaylist: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotPlaylist := resolvePlayTarget(tc.target, tc.mixFlag)
			if gotURL != tc.wantURL || gotPlaylist != tc.wantAllowPlaylist {
				t.Fatalf("resolvePlayTarget(%q, %v) = (%q, %v), want (%q, %v)",
					tc.target, tc.mixFlag, gotURL, gotPlaylist, tc.wantURL, tc.wantAllowPlaylist)
			}
		})
	}

	root := newRootCmd()
	playCmd, _, err := root.Find([]string{"play"})
	if err != nil || playCmd == nil {
		t.Fatalf("find play command: %v", err)
	}
	if f := playCmd.Flags().Lookup("mix"); f == nil || f.Shorthand != "m" {
		t.Fatalf("expected play --mix/-m flag, got %+v", f)
	}
}

func TestVersionFlag(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--version failed: %v", err)
	}
	got := strings.TrimSpace(out.String())
	want := "iutui version 0.5.3"
	if got != want {
		t.Fatalf("--version = %q, want %q", got, want)
	}
}

func TestDebugLogFlag(t *testing.T) {
	tmp := t.TempDir()
	stateHome := filepath.Join(tmp, "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "runtime"))

	t.Run("writes JSON slog lines to XDG_STATE_HOME/iutui/debug.log when enabled", func(t *testing.T) {
		root := newRootCmd()
		if flag := root.PersistentFlags().Lookup("debug-log"); flag == nil {
			t.Fatal("expected persistent --debug-log flag on root command")
		}
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"--debug-log", "doctor"})
		if err := root.Execute(); err != nil {
			t.Fatalf("doctor --debug-log failed: %v", err)
		}

		logPath := filepath.Join(stateHome, "iutui", "debug.log")
		info, err := os.Stat(logPath)
		if err != nil {
			t.Fatalf("expected debug.log at %q: %v", logPath, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("debug.log perm = %o, want 0600", info.Mode().Perm())
		}

		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read debug.log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) == 0 || lines[0] == "" {
			t.Fatalf("expected JSON log lines in debug.log, got empty")
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
			t.Fatalf("invalid JSON slog line %q: %v", lines[0], err)
		}
		if entry["msg"] != "iutui starting" {
			t.Errorf("log msg = %v, want %q", entry["msg"], "iutui starting")
		}
		if entry["version"] != "0.5.3" {
			t.Errorf("log version = %v, want %q", entry["version"], "0.5.3")
		}
		wantStateDir := filepath.Join(stateHome, "iutui")
		if entry["state_dir"] != wantStateDir {
			t.Errorf("log state_dir = %v, want %q", entry["state_dir"], wantStateDir)
		}
		if entry["socket_path"] == "" {
			t.Errorf("expected non-empty socket_path in log entry: %+v", entry)
		}

		// PersistentPostRun should have reset default slog to discard and closed debug.log.
		sizeBefore := len(raw)
		slog.Info("post-run discard check")
		rawAfter, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("re-read debug.log: %v", err)
		}
		if len(rawAfter) != sizeBefore {
			t.Fatalf("expected no writes to debug.log after PersistentPostRun, grew from %d to %d bytes", sizeBefore, len(rawAfter))
		}
	})

	t.Run("does not create debug.log when flag is omitted", func(t *testing.T) {
		isolatedState := filepath.Join(t.TempDir(), "state")
		t.Setenv("XDG_STATE_HOME", isolatedState)

		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"doctor"})
		if err := root.Execute(); err != nil {
			t.Fatalf("doctor failed: %v", err)
		}

		logPath := filepath.Join(isolatedState, "iutui", "debug.log")
		if _, err := os.Stat(logPath); !os.IsNotExist(err) {
			t.Fatalf("expected no debug.log when --debug-log is false, got err=%v", err)
		}
	})
}

func TestNativeThumbsSetPreviewMode(t *testing.T) {
	t.Setenv("YT_PREVIEW", "")
	t.Setenv("KITTY_WINDOW_ID", "1")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("COLORTERM", "")
	t.Setenv("NO_COLOR", "")

	tr := newNativeThumbs()
	if got := tr.Backend(); got != thumbs.BackendKitty {
		t.Fatalf("initial auto backend = %v, want %v", got, thumbs.BackendKitty)
	}

	tr.SetPreviewMode("none")
	if got := tr.Backend(); got != thumbs.BackendText {
		t.Fatalf("after SetPreviewMode(\"none\") backend = %v, want %v", got, thumbs.BackendText)
	}
	if rendered := tr.Render("abc123", 14, 5); !strings.Contains(rendered, "abc123") {
		t.Fatalf("expected text placeholder render for BackendText, got:\n%s", rendered)
	}
	if brand := tr.RenderBrand(10, 4); brand != "" {
		t.Fatalf("expected empty RenderBrand when not BackendKitty, got %q", brand)
	}

	tr.SetPreviewMode("chafa")
	if got := tr.Backend(); got != thumbs.BackendChafa {
		t.Fatalf("after SetPreviewMode(\"chafa\") backend = %v, want %v", got, thumbs.BackendChafa)
	}

	tr.SetPreviewMode("auto")
	if got := tr.Backend(); got != thumbs.BackendKitty {
		t.Fatalf("after SetPreviewMode(\"auto\") backend = %v, want %v", got, thumbs.BackendKitty)
	}
}

func TestNativeThumbsEnvAutoDetection(t *testing.T) {
	tests := []struct {
		name      string
		term      string
		colorTerm string
		noColor   string
		want      thumbs.Backend
	}{
		{
			name:      "COLORTERM=truecolor selects ANSI blocks",
			colorTerm: "truecolor",
			want:      thumbs.BackendChafa,
		},
		{
			name: "TERM=alacritty selects ANSI blocks",
			term: "alacritty",
			want: thumbs.BackendChafa,
		},
		{
			name:      "NO_COLOR=1 overrides COLORTERM=truecolor",
			term:      "alacritty",
			colorTerm: "truecolor",
			noColor:   "1",
			want:      thumbs.BackendText,
		},
		{
			name: "TERM=dumb selects text",
			term: "dumb",
			want: thumbs.BackendText,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("YT_PREVIEW", "")
			t.Setenv("KITTY_WINDOW_ID", "")
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("TERM", tc.term)
			t.Setenv("COLORTERM", tc.colorTerm)
			t.Setenv("NO_COLOR", tc.noColor)

			tr := newNativeThumbs()
			if got := tr.Backend(); got != tc.want {
				t.Fatalf("newNativeThumbs().Backend() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBubbleTeaEscapeFilter(t *testing.T) {
	filter := app.NewBubbleTeaEscapeFilter()

	t.Run("drops standalone SGR mouse fragment", func(t *testing.T) {
		got := filter(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<35;117;7M")})
		if got != nil {
			t.Fatalf("expected nil (dropped), got %#v", got)
		}
	})

	t.Run("drops split alt+[ followed by SGR body", func(t *testing.T) {
		if got := filter(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("["), Alt: true}); got != nil {
			t.Fatalf("expected alt+[ to be dropped, got %#v", got)
		}
		if got := filter(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("<35;117;7M")}); got != nil {
			t.Fatalf("expected split SGR body to be dropped, got %#v", got)
		}
	})

	t.Run("passes normal typing and mouse messages untouched", func(t *testing.T) {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}
		if got := filter(nil, key); got == nil {
			t.Fatalf("expected normal typing to pass, got nil")
		} else if km, ok := got.(tea.KeyMsg); !ok || string(km.Runes) != "hello" {
			t.Fatalf("unexpected key message: %#v", got)
		}

		mouse := tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion}
		if got := filter(nil, mouse); got != mouse {
			t.Fatalf("expected MouseMsg untouched, got %#v want %#v", got, mouse)
		}
	})
}

func TestInstallDepsHintAndMissingBins(t *testing.T) {
	t.Run("missingRequiredBins preserves order and returns missing bins", func(t *testing.T) {
		lookPath := func(bin string) (string, error) {
			if bin == "curl" {
				return "/usr/bin/curl", nil
			}
			return "", errors.New("not found")
		}
		got := missingRequiredBins(lookPath, "mpv", "curl", "yt-dlp")
		want := []string{"mpv", "yt-dlp"}
		if len(got) != len(want) {
			t.Fatalf("missingRequiredBins = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("missingRequiredBins[%d] = %q, want %q", i, got[i], want[i])
			}
		}

		allFound := missingRequiredBins(func(bin string) (string, error) {
			return "/usr/bin/" + bin, nil
		}, "mpv", "yt-dlp")
		if len(allFound) != 0 {
			t.Fatalf("expected no missing bins, got %v", allFound)
		}
	})

	t.Run("installDepsHint covers supported platforms and distros", func(t *testing.T) {
		deps := []string{"mpv", "yt-dlp"}
		tests := []struct {
			name      string
			goos      string
			osRelease string
			deps      []string
			want      string
		}{
			{
				name: "empty deps",
				goos: "linux",
				deps: nil,
				want: "",
			},
			{
				name: "darwin",
				goos: "darwin",
				deps: deps,
				want: "brew install mpv yt-dlp",
			},
			{
				name:      "arch cachyos",
				goos:      "linux",
				osRelease: "NAME=\"CachyOS Linux\"\nID=cachyos\nID_LIKE=arch\n",
				deps:      deps,
				want:      "sudo pacman -S --needed mpv yt-dlp",
			},
			{
				name:      "ubuntu debian",
				goos:      "linux",
				osRelease: "ID=ubuntu\nID_LIKE=debian\n",
				deps:      deps,
				want:      "sudo apt install mpv yt-dlp",
			},
			{
				name:      "fedora",
				goos:      "linux",
				osRelease: "ID=fedora\n",
				deps:      deps,
				want:      "sudo dnf install mpv yt-dlp",
			},
			{
				name:      "opensuse",
				goos:      "linux",
				osRelease: "ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n",
				deps:      deps,
				want:      "sudo zypper install mpv yt-dlp",
			},
			{
				name:      "alpine",
				goos:      "linux",
				osRelease: "ID=alpine\n",
				deps:      deps,
				want:      "sudo apk add mpv yt-dlp",
			},
			{
				name:      "unknown linux",
				goos:      "linux",
				osRelease: "ID=nixos\n",
				deps:      deps,
				want:      "install mpv yt-dlp using your package manager",
			},
			{
				name: "other os fallback",
				goos: "windows",
				deps: deps,
				want: "install mpv yt-dlp (see https://mpv.io and https://github.com/yt-dlp/yt-dlp)",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := installDepsHint(tc.goos, tc.osRelease, tc.deps)
				if got != tc.want {
					t.Fatalf("installDepsHint(%q, ..., %v) = %q, want %q", tc.goos, tc.deps, got, tc.want)
				}
			})
		}
	})
}
