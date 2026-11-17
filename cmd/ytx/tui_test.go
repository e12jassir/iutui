package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/catalog"
	"github.com/e12jassir/ytx/internal/config"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/platform"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/store"
	"github.com/e12jassir/ytx/internal/tui/theme"
	"github.com/e12jassir/ytx/internal/tui/thumbs"
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
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"doctor"})
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}
	got := out.String()
	for _, want := range []string{"config", "cache", "state", "socket"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
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

	// Filter with ']' twice (Todo -> Videos -> En vivo)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})

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
