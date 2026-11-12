package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/catalog"
	"github.com/e12jassir/ytx/internal/model"
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
