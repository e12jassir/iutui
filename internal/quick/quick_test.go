package quick

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/e12jassir/iutui/internal/model"
)

func sampleVideos() []model.Video {
	return []model.Video{
		{ID: "1", Title: "Comfortably Numb (Live)", Channel: "Pink Floyd", Duration: "09:20"},
		{ID: "2", Title: "De Música Ligera", Channel: "Soda Stereo", Duration: "04:50"},
		{ID: "3", Title: "Time", Channel: "Pink Floyd", Duration: "06:53"},
		{ID: "4", Title: "Go Concurrency Patterns", Channel: "Rob Pike", Duration: "42:10"},
	}
}

func sendText(m Model, s string) Model {
	for _, r := range s {
		var msg tea.KeyMsg
		if r == ' ' {
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestMultiWordFiltering(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantIDs   []string
		backCount int
		ctrlU     bool
	}{
		{
			name:    "empty query returns all",
			query:   "",
			wantIDs: []string{"1", "2", "3", "4"},
		},
		{
			name:    "single word case insensitive title",
			query:   "CONCURRENCY",
			wantIDs: []string{"4"},
		},
		{
			name:    "single word channel",
			query:   "pink",
			wantIDs: []string{"1", "3"},
		},
		{
			name:    "multi word across channel and title",
			query:   "pink live",
			wantIDs: []string{"1"},
		},
		{
			name:    "multi word order independent",
			query:   "time floyd",
			wantIDs: []string{"3"},
		},
		{
			name:    "no matches",
			query:   "nonexistent query",
			wantIDs: nil,
		},
		{
			name:      "backspace restores matches",
			query:     "pink live",
			backCount: 5, // removes " live" -> "pink"
			wantIDs:   []string{"1", "3"},
		},
		{
			name:    "ctrl+u clears query",
			query:   "soda",
			ctrlU:   true,
			wantIDs: []string{"1", "2", "3", "4"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(sampleVideos())
			m = sendText(m, tc.query)

			for i := 0; i < tc.backCount; i++ {
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
				m = next.(Model)
			}
			if tc.ctrlU {
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
				m = next.(Model)
			}

			if len(m.Filtered) != len(tc.wantIDs) {
				t.Fatalf("got %d filtered items, want %d (query=%q)", len(m.Filtered), len(tc.wantIDs), m.Query)
			}
			for i, wantID := range tc.wantIDs {
				if m.Filtered[i].ID != wantID {
					t.Errorf("Filtered[%d].ID = %q, want %q", i, m.Filtered[i].ID, wantID)
				}
			}
			if m.Selected != 0 {
				t.Errorf("Selected = %d, want 0 after filter", m.Selected)
			}
		})
	}
}

func TestNavigationBounds(t *testing.T) {
	tests := []struct {
		name         string
		videos       []model.Video
		keys         []tea.KeyType
		wantSelected int
	}{
		{
			name:         "up at top stays at 0",
			videos:       sampleVideos(),
			keys:         []tea.KeyType{tea.KeyUp, tea.KeyCtrlP},
			wantSelected: 0,
		},
		{
			name:         "down and ctrl+n advance",
			videos:       sampleVideos(),
			keys:         []tea.KeyType{tea.KeyDown, tea.KeyCtrlN},
			wantSelected: 2,
		},
		{
			name:         "down clamps at last item",
			videos:       sampleVideos(),
			keys:         []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown},
			wantSelected: 3,
		},
		{
			name:         "down then up with ctrl+p",
			videos:       sampleVideos(),
			keys:         []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyCtrlP},
			wantSelected: 1,
		},
		{
			name:         "empty list stays at 0",
			videos:       nil,
			keys:         []tea.KeyType{tea.KeyDown, tea.KeyUp},
			wantSelected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.videos)
			for _, k := range tc.keys {
				next, _ := m.Update(tea.KeyMsg{Type: k})
				m = next.(Model)
			}
			if m.Selected != tc.wantSelected {
				t.Errorf("Selected = %d, want %d", m.Selected, tc.wantSelected)
			}
		})
	}
}

func TestSelectionAndCancellation(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		navKeys      []tea.KeyType
		finalKey     tea.KeyType
		wantChosenID string
		wantCanceled bool
		wantQuit     bool
	}{
		{
			name:         "enter selects first item",
			finalKey:     tea.KeyEnter,
			wantChosenID: "1",
			wantQuit:     true,
		},
		{
			name:         "enter selects navigated item after filter",
			query:        "pink",
			navKeys:      []tea.KeyType{tea.KeyDown},
			finalKey:     tea.KeyEnter,
			wantChosenID: "3",
			wantQuit:     true,
		},
		{
			name:         "enter on empty filtered list does not quit",
			query:        "zzz-no-match",
			finalKey:     tea.KeyEnter,
			wantChosenID: "",
			wantQuit:     false,
		},
		{
			name:         "esc cancels and quits",
			finalKey:     tea.KeyEsc,
			wantCanceled: true,
			wantQuit:     true,
		},
		{
			name:         "ctrl+c cancels and quits",
			finalKey:     tea.KeyCtrlC,
			wantCanceled: true,
			wantQuit:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(sampleVideos())
			m = sendText(m, tc.query)
			for _, k := range tc.navKeys {
				next, _ := m.Update(tea.KeyMsg{Type: k})
				m = next.(Model)
			}

			next, cmd := m.Update(tea.KeyMsg{Type: tc.finalKey})
			got := next.(Model)

			if got.Canceled != tc.wantCanceled {
				t.Errorf("Canceled = %v, want %v", got.Canceled, tc.wantCanceled)
			}

			if tc.wantChosenID == "" {
				if got.Chosen != nil {
					t.Errorf("Chosen = %+v, want nil", got.Chosen)
				}
			} else {
				if got.Chosen == nil {
					t.Fatalf("Chosen = nil, want ID %q", tc.wantChosenID)
				}
				if got.Chosen.ID != tc.wantChosenID {
					t.Errorf("Chosen.ID = %q, want %q", got.Chosen.ID, tc.wantChosenID)
				}
			}

			if tc.wantQuit {
				if cmd == nil {
					t.Fatalf("expected tea.Quit cmd, got nil")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Errorf("expected tea.QuitMsg, got %T", cmd())
				}
			} else if cmd != nil {
				t.Errorf("expected nil cmd, got non-nil")
			}
		})
	}
}

func TestView(t *testing.T) {
	m := New(sampleVideos())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = next.(Model)
	m = sendText(m, "pink")

	out := m.View()
	if !strings.Contains(out, "pink") {
		t.Errorf("View() missing query %q:\n%s", "pink", out)
	}
	if !strings.Contains(out, "2/4") {
		t.Errorf("View() missing count %q:\n%s", "2/4", out)
	}
	if !strings.Contains(out, "Comfortably Numb (Live)") {
		t.Errorf("View() missing matched title:\n%s", out)
	}

	m = sendText(m, "nomatch")
	if !strings.Contains(m.View(), "No matches") {
		t.Errorf("View() missing empty state:\n%s", m.View())
	}
}
