package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/catalog"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/player"
	"github.com/e12jassir/ytx/internal/store"
)

func newTestState() (*State, *catalog.FakeCatalog, *player.FakeEngine, *store.FakeStore) {
	cat := &catalog.FakeCatalog{
		Feeds: map[model.Tab][]model.Video{
			model.TabHome: {
				{ID: "vid01", Title: "Go Concurrency", Channel: "GoDev", Duration: "12:00"},
				{ID: "live1", Title: "News Live", Channel: "World", IsLive: true},
				{ID: "vid02", Title: "Bubble Tea", Channel: "Charm", Duration: "08:30"},
			},
		},
		Results: map[string][]model.Video{
			"golang": {
				{ID: "srch1", Title: "Search Match 1", Channel: "ChanA", Duration: "05:00"},
				{ID: "srch2", Title: "Search Live", Channel: "ChanB", IsLive: true},
			},
		},
	}
	eng := player.NewFakeEngine()
	st := store.NewFakeStore()
	s := NewState(Dependencies{Catalog: cat, Engine: eng, Store: st})
	return s, cat, eng, st
}

func TestStateFeedAndChipFiltering(t *testing.T) {
	s, _, _, _ := newTestState()
	defer s.Close()

	s, _ = s.Update(FeedLoadedMsg{
		Tab: model.TabHome,
		Videos: []model.Video{
			{ID: "vid01", Title: "Regular", IsLive: false},
			{ID: "live1", Title: "Stream", IsLive: true},
		},
	})

	if got := len(s.VisibleItems()); got != 2 {
		t.Fatalf("all chip items = %d, want 2", got)
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if s.HomeChip != 1 {
		t.Fatalf("HomeChip = %d, want 1 (En vivo)", s.HomeChip)
	}
	items := s.VisibleItems()
	if len(items) != 1 || items[0].ID != "live1" {
		t.Fatalf("filtered items = %+v, want only live1", items)
	}
}

func TestStateSearchFlowAndEscReturn(t *testing.T) {
	s, _, _, _ := newTestState()
	defer s.Close()

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if s.Focus != FocusSearch {
		t.Fatalf("Focus = %v, want FocusSearch", s.Focus)
	}

	for _, r := range "golang" {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	var cmd tea.Cmd
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.View != ViewSearch || s.SearchQuery != "golang" {
		t.Fatalf("View=%v Query=%q, want ViewSearch & golang", s.View, s.SearchQuery)
	}
	if cmd == nil {
		t.Fatal("expected search command")
	}
	msg := cmd()
	s, _ = s.Update(msg)
	if len(s.VisibleItems()) != 2 {
		t.Fatalf("search results = %d, want 2", len(s.VisibleItems()))
	}

	// Filter search to En vivo (index 2: Todo=0, Videos=1, En vivo=2)
	s.CycleChip(2)
	if items := s.VisibleItems(); len(items) != 1 || items[0].ID != "srch2" {
		t.Fatalf("search live filter = %+v, want srch2", items)
	}

	// Esc returns to ViewHome
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.View != ViewHome {
		t.Fatalf("View = %v, want ViewHome after Esc", s.View)
	}
}

func TestStatePlayEnqueueAndSave(t *testing.T) {
	s, cat, eng, _ := newTestState()
	defer s.Close()

	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})

	// Save with 'w'
	var cmd tea.Cmd
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	s, _ = s.Update(cmd())
	if !s.SavedIDs["vid01"] {
		t.Fatal("expected vid01 in SavedIDs")
	}

	// Enqueue with 'q'
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if len(s.Queue) != 1 || s.Queue[0].ID != "vid01" {
		t.Fatalf("Queue = %+v, want [vid01]", s.Queue)
	}
	if cmd != nil {
		// Execute batch commands
		_ = cmd()
	}

	// Play with Enter
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.HistoryIDs["vid01"] {
		t.Fatal("expected vid01 in HistoryIDs")
	}
	if s.Toast == "" {
		t.Fatal("expected non-empty toast on play")
	}
	if len(eng.Calls()) == 0 {
		// Run batch sub-commands synchronously to trigger PlayVideo
		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					c()
				}
			}
		}
	}
	if len(eng.Calls()) == 0 {
		t.Fatal("expected FakeEngine to record Play/Enqueue calls")
	}
}

func TestStateToastExpirationRespectsID(t *testing.T) {
	s, _, _, _ := newTestState()
	defer s.Close()

	s.toastCmd("first")
	firstID := s.ToastID
	s.toastCmd("second")

	s, _ = s.Update(ToastExpiredMsg{ID: firstID})
	if s.Toast != "second" {
		t.Fatalf("stale toast expiration overwrote newer toast: got %q", s.Toast)
	}

	s, _ = s.Update(ToastExpiredMsg{ID: s.ToastID})
	if s.Toast != "" {
		t.Fatalf("expected toast cleared, got %q", s.Toast)
	}
}
