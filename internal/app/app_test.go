package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/catalog"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/store"
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
	msg := runBatchForSearch(cmd)
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

func TestMouseInteractions(t *testing.T) {
	t.Run("click search bar focuses search", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 120, 38
		_, _, barStart := s.headerSearchBounds()
		s, _ = s.Update(tea.MouseMsg{
			X:      barStart + 2,
			Y:      1,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if s.Focus != FocusSearch {
			t.Fatalf("expected FocusSearch, got %v", s.Focus)
		}
	})

	t.Run("click reload [r] button triggers sync", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 120, 38
		s.Focus = FocusSearch
		btnStart, _, _ := s.headerSearchBounds()
		var cmd tea.Cmd
		s, cmd = s.Update(tea.MouseMsg{
			X:      btnStart + 1,
			Y:      1,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if !s.Syncing {
			t.Fatal("expected Syncing=true after clicking [r]")
		}
		if cmd == nil {
			t.Fatal("expected non-nil loadFeed cmd")
		}
	})

	t.Run("click chip switches active chip filter", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 120, 38
		// mainX = 11 (miniGuideW=10 + 1 border); first chip "Todo" width=6, second chip starts at 11+1+6+1=19
		s, _ = s.Update(tea.MouseMsg{
			X:      20,
			Y:      4,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if s.HomeChip != 1 {
			t.Fatalf("expected HomeChip 1, got %d", s.HomeChip)
		}
	})

	t.Run("click card selects then second click plays", func(t *testing.T) {
		s, cat, eng, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 120, 38
		s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})
		// mainX = 11; col 1 starts after col 0 width + gap, y >= 5
		col1X := s.mainOriginX() + s.gridCardWidth() + 4
		s, _ = s.Update(tea.MouseMsg{
			X:      col1X,
			Y:      7,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if s.SelectedIdx != 1 {
			t.Fatalf("expected SelectedIdx 1, got %d", s.SelectedIdx)
		}
		if len(eng.Calls()) != 0 {
			t.Fatalf("expected no play on first click, got %v", eng.Calls())
		}

		var cmd tea.Cmd
		s, cmd = s.Update(tea.MouseMsg{
			X:      col1X,
			Y:      7,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if cmd == nil {
			t.Fatal("expected play cmd on second click")
		}
		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					c()
				}
			}
		}
		if len(eng.Calls()) == 0 {
			t.Fatal("expected engine Play call on second click")
		}
	})

	t.Run("click help button toggles and next click dismisses", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 120, 38
		// [?] inside right actions panel [Width-15, Width-10]
		s, _ = s.Update(tea.MouseMsg{
			X:      s.Width - 12,
			Y:      1,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if !s.ShowHelp {
			t.Fatal("expected ShowHelp true")
		}
		s, _ = s.Update(tea.MouseMsg{
			X:      10,
			Y:      10,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		if s.ShowHelp {
			t.Fatal("expected ShowHelp false after dismiss click")
		}
	})

	t.Run("click sidebar or mini-guide switches tab", func(t *testing.T) {
		cases := []struct {
			name   string
			width  int
			height int
			x      int
			y      int
			want   model.Tab
		}{
			{name: "mini-guide music", width: 120, height: 36, x: 4, y: 7, want: model.TabMusic},
			{name: "full sidebar subscriptions", width: 156, height: 45, x: 5, y: 7, want: model.TabSubscriptions},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s, _, _, _ := newTestState()
				s.Width, s.Height = tc.width, tc.height
				s, _ = s.Update(tea.MouseMsg{
					X:      tc.x,
					Y:      tc.y,
					Button: tea.MouseButtonLeft,
					Action: tea.MouseActionPress,
				})
				if s.ActiveTab != tc.want {
					t.Fatalf("expected ActiveTab %v, got %v", tc.want, s.ActiveTab)
				}
			})
		}
	})

	t.Run("space key works inside search input", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Focus = FocusSearch
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pink")})
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("floyd")})
		if s.SearchDraft != "pink floyd" {
			t.Fatalf("expected SearchDraft %q, got %q", "pink floyd", s.SearchDraft)
		}
	})

	t.Run("keys 4 and 5 match sidebar order History and Downloads", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
		if s.ActiveTab != model.TabHistory {
			t.Fatalf("key 4 ActiveTab = %v, want TabHistory", s.ActiveTab)
		}
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
		if s.ActiveTab != model.TabDownloads {
			t.Fatalf("key 5 ActiveTab = %v, want TabDownloads", s.ActiveTab)
		}
	})

	t.Run("Tab focuses sidebar and j/enter navigates tabs", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 140, 36
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyTab})
		if s.Focus != FocusSidebar {
			t.Fatalf("expected FocusSidebar, got %v", s.Focus)
		}
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		if s.ActiveTab != model.TabMusic {
			t.Fatalf("expected TabMusic after j in sidebar, got %v", s.ActiveTab)
		}
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if s.Focus != FocusMain {
			t.Fatalf("expected FocusMain after enter in sidebar, got %v", s.Focus)
		}
	})

	t.Run("ActionResultMsg surfaces engine errors in toast", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s, _ = s.Update(ActionResultMsg{Err: context.DeadlineExceeded})
		if s.Toast == "" {
			t.Fatal("expected error toast")
		}
	})

	t.Run("c key cycles quality presets and invokes OnQualityChange", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		var got []int
		s.Deps.OnQualityChange = func(q int) error {
			got = append(got, q)
			return nil
		}
		var cmd tea.Cmd
		s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
		if s.Quality != 1080 {
			t.Fatalf("expected Quality 1080 after cycling from 0, got %d", s.Quality)
		}
		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					c()
				}
			}
		}
		if len(got) != 1 || got[0] != 1080 {
			t.Fatalf("expected OnQualityChange [1080], got %v", got)
		}
	})
}

func runBatchForSearch(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	resCh := make(chan tea.Msg, len(batch))
	count := 0
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		count++
		go func(c tea.Cmd) {
			resCh <- c()
		}(sub)
	}
	var fallback tea.Msg
	for i := 0; i < count; i++ {
		m := <-resCh
		if sd, ok := m.(SearchDoneMsg); ok {
			return sd
		}
		switch m.(type) {
		case AnimTickMsg, ToastExpiredMsg, TickMsg, nil:
		default:
			if fallback == nil {
				fallback = m
			}
		}
	}
	return fallback
}

func runBatchSync(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
}

type controlledSearchCatalog struct {
	mu      sync.Mutex
	calls   int
	started chan int
	release []chan []model.Video
}

func newControlledSearchCatalog(gates ...chan []model.Video) *controlledSearchCatalog {
	return &controlledSearchCatalog{
		started: make(chan int, len(gates)+4),
		release: gates,
	}
}

func (c *controlledSearchCatalog) Feed(context.Context, model.Tab) ([]model.Video, error) {
	return nil, nil
}

func (c *controlledSearchCatalog) Search(ctx context.Context, _ string) ([]model.Video, error) {
	c.mu.Lock()
	idx := c.calls
	c.calls++
	c.mu.Unlock()

	c.started <- idx
	if idx < len(c.release) {
		select {
		case res := <-c.release[idx]:
			return res, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, ctx.Err()
}

func TestMultiRoundSearchClearsPreviousAndPreventsConcat(t *testing.T) {
	s, cat, _, _ := newTestState()
	defer s.Close()
	s.Width, s.Height = 120, 38
	cat.Results["pink floyd"] = []model.Video{
		{ID: "pf1", Title: "Pink Floyd - Time", Channel: "Pink Floyd", Duration: "07:05"},
	}

	// Round 1 via '/'
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if s.Focus != FocusSearch || s.SearchDraft != "" {
		t.Fatalf("round 1 focus/draft = (%v, %q), want (FocusSearch, \"\")", s.Focus, s.SearchDraft)
	}
	for _, r := range "golang" {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	var cmd tea.Cmd
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected round 1 search cmd")
	}
	s, _ = s.Update(runBatchForSearch(cmd))
	if len(s.SearchResults) != 2 {
		t.Fatalf("round 1 results = %d, want 2", len(s.SearchResults))
	}
	s.SetSelection(1)
	s.ResultScroll = 1
	s.SearchChip = 1

	// Round 2 via '/': draft must start empty so typing does not concatenate onto "golang"
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if s.Focus != FocusSearch {
		t.Fatalf("round 2 Focus = %v, want FocusSearch", s.Focus)
	}
	if s.SearchDraft != "" {
		t.Fatalf("round 2 SearchDraft = %q, want empty string (no concat)", s.SearchDraft)
	}
	if s.SearchQuery != "golang" || len(s.SearchResults) != 2 {
		t.Fatalf("expected prior SearchQuery/SearchResults intact while typing, got query=%q len=%d", s.SearchQuery, len(s.SearchResults))
	}

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pink floyd")})
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.SearchQuery != "pink floyd" {
		t.Fatalf("round 2 SearchQuery = %q, want %q", s.SearchQuery, "pink floyd")
	}
	if s.SearchDraft != "" {
		t.Fatalf("SearchDraft after submit = %q, want empty", s.SearchDraft)
	}
	if len(s.SearchResults) != 0 {
		t.Fatalf("expected previous SearchResults cleared immediately on submit, got %d", len(s.SearchResults))
	}
	if !s.SearchPending || s.ResultIdx != 0 || s.ResultScroll != 0 || s.SearchChip != 0 {
		t.Fatalf("expected pending=true and reset indices, got pending=%v idx=%d scroll=%d chip=%d",
			s.SearchPending, s.ResultIdx, s.ResultScroll, s.SearchChip)
	}
	s, _ = s.Update(runBatchForSearch(cmd))
	if len(s.SearchResults) != 1 || s.SearchResults[0].ID != "pf1" {
		t.Fatalf("round 2 results = %+v, want [pf1]", s.SearchResults)
	}

	// Round 3 via mouse click on search bar when unfocused clears stale draft
	s.SearchDraft = "leftover"
	_, _, barStart := s.headerSearchBounds()
	s, _ = s.Update(tea.MouseMsg{
		X:      barStart + 2,
		Y:      1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if s.Focus != FocusSearch || s.SearchDraft != "" {
		t.Fatalf("mouse focus search = (%v, %q), want (FocusSearch, \"\")", s.Focus, s.SearchDraft)
	}

	// Clicking inside search bar while already focused preserves current draft
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	s, _ = s.Update(tea.MouseMsg{
		X:      barStart + 2,
		Y:      1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if s.SearchDraft != "abc" {
		t.Fatalf("re-clicking focused search bar altered draft to %q, want %q", s.SearchDraft, "abc")
	}
}

func TestStaleAndCanceledSearchIgnoredBySeq(t *testing.T) {
	gate1 := make(chan []model.Video)
	gate2 := make(chan []model.Video, 1)
	cat := newControlledSearchCatalog(gate1, gate2)
	s := NewState(Dependencies{Catalog: cat})
	defer s.Close()

	// Start search 1 ("golang")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("golang")})
	var cmd1 tea.Cmd
	s, cmd1 = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("expected cmd1")
	}
	done1 := make(chan tea.Msg, 1)
	go func() { done1 <- runBatchForSearch(cmd1) }()
	<-cat.started

	// Restart search 2 with the exact same query "golang" while search 1 is in flight
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("golang")})
	var cmd2 tea.Cmd
	s, cmd2 = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd2 == nil {
		t.Fatal("expected cmd2")
	}

	// Search 1 was canceled by SubmitSearch; its SearchDoneMsg must be ignored without clearing SearchPending or setting Toast
	msg1 := <-done1
	s, _ = s.Update(msg1)
	if !s.SearchPending {
		t.Fatal("expected SearchPending to remain true after stale canceled search 1 returned")
	}
	if s.Toast != "" {
		t.Fatalf("expected no error toast on canceled search, got %q", s.Toast)
	}

	// Even if a stale seq=1 message arrives with non-empty results for the same query, it must be ignored
	s, _ = s.Update(SearchDoneMsg{
		Query:  "golang",
		Seq:    1,
		Videos: []model.Video{{ID: "stale", Title: "Stale"}},
	})
	if len(s.SearchResults) != 0 || !s.SearchPending {
		t.Fatalf("stale seq=1 payload leaked into state: results=%+v pending=%v", s.SearchResults, s.SearchPending)
	}

	// Now search 2 completes
	fresh := []model.Video{{ID: "fresh", Title: "Fresh Result"}}
	gate2 <- fresh
	msg2 := runBatchForSearch(cmd2)
	<-cat.started
	s, _ = s.Update(msg2)
	if s.SearchPending {
		t.Fatal("expected SearchPending=false after current search 2 finished")
	}
	if len(s.SearchResults) != 1 || s.SearchResults[0].ID != "fresh" {
		t.Fatalf("expected fresh results, got %+v", s.SearchResults)
	}

	// A context.Canceled error matching current seq (or wrapped) clears SearchPending with no toast
	s.SearchPending = true
	s.Toast = ""
	s, _ = s.Update(SearchDoneMsg{
		Query: "golang",
		Seq:   s.searchSeq,
		Err:   fmt.Errorf("search aborted: %w", context.Canceled),
	})
	if s.SearchPending {
		t.Fatal("expected SearchPending=false on canceled current search")
	}
	if s.Toast != "" {
		t.Fatalf("expected no toast for context.Canceled, got %q", s.Toast)
	}

	// Non-cancellation errors still show toast
	s.SearchPending = true
	s, _ = s.Update(SearchDoneMsg{
		Query: "golang",
		Seq:   s.searchSeq,
		Err:   errors.New("network failure"),
	})
	if s.Toast == "" || !strings.Contains(s.Toast, "Error buscando") {
		t.Fatalf("expected error toast for non-cancel error, got %q", s.Toast)
	}
}

func TestSearchInputEditingShortcutsAndPaste(t *testing.T) {
	s, _, _, _ := newTestState()
	defer s.Close()
	s.SearchQuery = "previous query"
	s.SearchResults = []model.Video{{ID: "keep1", Title: "Keep"}}
	s.Focus = FocusSearch

	// Pasted runes sanitize \r, \n, \t -> ' ' and drop control chars < 32
	s, _ = s.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("soda\nstereo\t\r\x07live"),
		Paste: true,
	})
	if s.SearchDraft != "soda stereo  live" {
		t.Fatalf("after sanitized paste got %q, want %q", s.SearchDraft, "soda stereo  live")
	}

	// delete, ctrl+h, backspace delete last rune cleanly (including multibyte UTF-8)
	s.SearchDraft = "café"
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if s.SearchDraft != "caf" {
		t.Fatalf("after delete got %q, want %q", s.SearchDraft, "caf")
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if s.SearchDraft != "ca" {
		t.Fatalf("after ctrl+h got %q, want %q", s.SearchDraft, "ca")
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if s.SearchDraft != "c" {
		t.Fatalf("after backspace got %q, want %q", s.SearchDraft, "c")
	}

	// ctrl+w and alt+backspace delete last word (trim trailing spaces first, then up to previous space)
	s.SearchDraft = "pink floyd   "
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if s.SearchDraft != "pink " {
		t.Fatalf("after ctrl+w got %q, want %q", s.SearchDraft, "pink ")
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if s.SearchDraft != "" {
		t.Fatalf("after alt+backspace got %q, want %q", s.SearchDraft, "")
	}

	// ctrl+u clears entire draft
	s.SearchDraft = "clear everything"
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if s.SearchDraft != "" {
		t.Fatalf("after ctrl+u got %q, want %q", s.SearchDraft, "")
	}

	// esc blurs search and clears draft while keeping previous SearchQuery & SearchResults
	s.SearchDraft = "aborted"
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.Focus != FocusMain || s.SearchDraft != "" {
		t.Fatalf("after esc got focus=%v draft=%q, want FocusMain & empty", s.Focus, s.SearchDraft)
	}
	if s.SearchQuery != "previous query" || len(s.SearchResults) != 1 {
		t.Fatalf("esc clobbered prior search state: query=%q len=%d", s.SearchQuery, len(s.SearchResults))
	}
}

func TestMixKeyTriggersMixPlayback(t *testing.T) {
	t.Run("m key invokes Engine.Play with mix mode and mix toast", func(t *testing.T) {
		t.Parallel()
		s, cat, eng, _ := newTestState()
		defer s.Close()
		s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})

		var cmd tea.Cmd
		s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
		wantToast := "📻 Abriendo Mix: Go Concurrency"
		if s.Toast != wantToast {
			t.Fatalf("Toast = %q, want %q", s.Toast, wantToast)
		}
		runBatchSync(cmd)

		calls := eng.Calls()
		if len(calls) != 1 || calls[0].Method != "Play" || calls[0].Mode != "mix" || calls[0].Video.ID != "vid01" {
			t.Fatalf("unexpected Engine calls: %+v", calls)
		}
	})

	t.Run("mix and playlist prefixes use Mix toast on Enter", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			id        string
			wantToast string
		}{
			{id: "RDOgeR2oqZGTs", wantToast: "📻 Abriendo Mix: Track"},
			{id: "PLabc123", wantToast: "📻 Abriendo Mix: Track"},
			{id: "OLAK5uy_xyz", wantToast: "📻 Abriendo Mix: Track"},
			{id: "dQw4w9WgXcQ", wantToast: "▶ Abriendo en mpv: Track"},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.id, func(t *testing.T) {
				t.Parallel()
				s, _, _, _ := newTestState()
				defer s.Close()
				s, _ = s.Update(FeedLoadedMsg{
					Tab:    model.TabHome,
					Videos: []model.Video{{ID: tc.id, Title: "Track"}},
				})
				var cmd tea.Cmd
				s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if s.Toast != tc.wantToast {
					t.Fatalf("Toast = %q, want %q", s.Toast, tc.wantToast)
				}
				runBatchSync(cmd)
			})
		}
	})
}

type spyPlayerEngine struct {
	*player.FakeEngine
	mu               sync.Mutex
	prefetched       []string
	autoplayStates   []bool
	autoplayFallback []string
	pauseCalls       int
}

func newSpyPlayerEngine() *spyPlayerEngine {
	return &spyPlayerEngine{FakeEngine: player.NewFakeEngine()}
}

func (e *spyPlayerEngine) Prefetch(v model.Video) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prefetched = append(e.prefetched, v.ID)
}

func (e *spyPlayerEngine) SetAutoplay(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoplayStates = append(e.autoplayStates, enabled)
}

func (e *spyPlayerEngine) SetAutoplayFallback(ids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoplayFallback = append([]string(nil), ids...)
}

func (e *spyPlayerEngine) Pause(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pauseCalls++
	return nil
}

func (e *spyPlayerEngine) snapshot() (prefetched []string, autoplayStates []bool, fallback []string, pauseCalls int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.prefetched...),
		append([]bool(nil), e.autoplayStates...),
		append([]string(nil), e.autoplayFallback...),
		e.pauseCalls
}

func collectNonTickMsgs(cmd tea.Cmd, timeout time.Duration) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 32)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		m := c()
		if batch, ok := m.(tea.BatchMsg); ok {
			var wg sync.WaitGroup
			for _, sub := range batch {
				if sub == nil {
					continue
				}
				wg.Add(1)
				go func(sc tea.Cmd) {
					defer wg.Done()
					run(sc)
				}(sub)
			}
			wg.Wait()
			return
		}
		if m != nil {
			ch <- m
		}
	}
	done := make(chan struct{})
	go func() {
		run(cmd)
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var msgs []tea.Msg
	for {
		select {
		case m := <-ch:
			switch m.(type) {
			case AnimTickMsg, ToastExpiredMsg, TickMsg:
				continue
			}
			msgs = append(msgs, m)
		case <-done:
			for len(ch) > 0 {
				m := <-ch
				switch m.(type) {
				case AnimTickMsg, ToastExpiredMsg, TickMsg:
					continue
				}
				msgs = append(msgs, m)
			}
			return msgs
		case <-timer.C:
			return msgs
		}
	}
}

func TestCollapsiblePlayerBarAndMouseCoords(t *testing.T) {
	s, _, eng, _ := newTestState()
	defer s.Close()
	s.Width, s.Height = 120, 38

	if s.HasPlayerBar() {
		t.Fatal("expected HasPlayerBar() to be false initially")
	}
	if !s.Autoplay {
		t.Fatal("expected Autoplay to default to true")
	}

	var videos []model.Video
	for i := 0; i < 9; i++ {
		videos = append(videos, model.Video{
			ID:       fmt.Sprintf("v%02d", i),
			Title:    fmt.Sprintf("Video %02d", i),
			Channel:  "Chan",
			Duration: "04:00",
		})
	}
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: videos})

	// Click near the bottom (y = s.Height - 3): with collapsed player bar this must hit row 2 card 6, not player controls.
	s, _ = s.Update(tea.MouseMsg{
		X:      s.mainOriginX() + 2,
		Y:      s.Height - 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if s.SelectedIdx != 6 {
		t.Fatalf("expected click at y=%d with collapsed player bar to select idx 6, got %d", s.Height-3, s.SelectedIdx)
	}
	if len(eng.Calls()) != 0 {
		t.Fatalf("expected no engine calls from main grid selection click, got %+v", eng.Calls())
	}

	// PlaySelected enters PlayLoading immediately, expanding the player bar.
	var cmd tea.Cmd
	s, cmd = s.PlaySelected("auto")
	if !s.PlayLoading || s.PlayLoadingTitle != "Video 06" {
		t.Fatalf("expected PlayLoading=true title=%q, got loading=%v title=%q", "Video 06", s.PlayLoading, s.PlayLoadingTitle)
	}
	if !s.HasPlayerBar() {
		t.Fatal("expected HasPlayerBar() == true while PlayLoading")
	}

	// While player bar is visible, clicking bottom transport [▶/⏸] (x=9, y=s.Height-2) must hit player controls.
	_, pauseCmd := s.Update(tea.MouseMsg{
		X:      9,
		Y:      s.Height - 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	_ = collectNonTickMsgs(pauseCmd, 150*time.Millisecond)
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)

	foundTogglePause := false
	for _, c := range eng.Calls() {
		if c.Method == "TogglePause" {
			foundTogglePause = true
			break
		}
	}
	if !foundTogglePause {
		t.Fatalf("expected bottom player click while HasPlayerBar()=true to trigger TogglePause, calls=%+v", eng.Calls())
	}

	// Receiving PlayerStatusMsg{IsRunning: true} clears PlayLoading while keeping player bar visible.
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{IsRunning: true, Title: "Video 06"}})
	if s.PlayLoading {
		t.Fatal("expected PlayerStatusMsg{IsRunning: true} to clear PlayLoading")
	}
	if !s.HasPlayerBar() {
		t.Fatal("expected HasPlayerBar() == true when IsRunning == true")
	}

	// Stopping playback collapses player bar again.
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{IsRunning: false}})
	if s.HasPlayerBar() {
		t.Fatal("expected HasPlayerBar() == false when IsRunning == false and PlayLoading == false")
	}
}

func TestPrefetchLookaheadAndAutoplayFallback(t *testing.T) {
	spy := newSpyPlayerEngine()
	cat := &catalog.FakeCatalog{}
	s := NewState(Dependencies{Catalog: cat, Engine: spy, Store: store.NewFakeStore()})
	defer s.Close()

	var videos []model.Video
	for i := 0; i < 20; i++ {
		v := model.Video{ID: fmt.Sprintf("id%02d", i), Title: fmt.Sprintf("Track %02d", i)}
		if i == 2 {
			v.IsLive = true
		}
		videos = append(videos, v)
	}
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: videos})
	prefetched, _, _, _ := spy.snapshot()
	// FeedLoadedMsg prefetches selection (id00,id01,id02) + prefetchTop(4) (id00..id03), so id03 must be prefetched.
	foundTop4 := false
	for _, id := range prefetched {
		if id == "id03" {
			foundTop4 = true
		}
	}
	if !foundTop4 {
		t.Fatalf("expected FeedLoadedMsg prefetchTop(4) to include id03, prefetched=%v", prefetched)
	}

	spy.mu.Lock()
	spy.prefetched = nil
	spy.mu.Unlock()
	s.SetSelection(5)
	prefetched, _, _, _ = spy.snapshot()
	if len(prefetched) != 3 || prefetched[0] != "id05" || prefetched[1] != "id06" || prefetched[2] != "id07" {
		t.Fatalf("expected SetSelection(5) to prefetch [id05 id06 id07], got %v", prefetched)
	}

	s.SetSelection(0)
	var cmd tea.Cmd
	s, cmd = s.PlaySelected("auto")
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	_, autoplayStates, fallback, _ := spy.snapshot()
	if len(autoplayStates) == 0 || !autoplayStates[len(autoplayStates)-1] {
		t.Fatalf("expected SetAutoplay(true) on PlaySelected, got %v", autoplayStates)
	}
	if len(fallback) != 15 {
		t.Fatalf("expected 15 upcoming non-live fallback IDs, got %d (%v)", len(fallback), fallback)
	}
	for _, id := range fallback {
		if id == "id00" || id == "id02" {
			t.Fatalf("fallback unexpectedly included current or live ID %q: %v", id, fallback)
		}
	}
}

func TestAnimTickLifecycle(t *testing.T) {
	s, cat, _, _ := newTestState()
	defer s.Close()

	// 1. Init() with catalog starts Syncing and AnimTick lifecycle
	initCmd := s.Init()
	if !s.Syncing || !s.animActive || initCmd == nil {
		t.Fatalf("expected Init() with catalog to set Syncing=true, animActive=true, non-nil cmd; got syncing=%v active=%v cmd=%v",
			s.Syncing, s.animActive, initCmd != nil)
	}
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})
	s, cmd := s.Update(AnimTickMsg(time.Now()))
	if cmd != nil || s.animActive {
		t.Fatalf("expected AnimTickMsg to stop after initial FeedLoadedMsg, got cmd=%v active=%v", cmd != nil, s.animActive)
	}

	// 2. Syncing lifecycle via 'r'
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !s.Syncing || cmd == nil {
		t.Fatalf("expected Syncing=true and non-nil cmd on 'r'")
	}
	s, cmd = s.Update(AnimTickMsg(time.Now()))
	if s.AnimFrame != 1 || cmd == nil {
		t.Fatalf("expected AnimFrame=1 and continuing tick cmd while Syncing, got frame=%d cmd=%v", s.AnimFrame, cmd != nil)
	}
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})
	s, cmd = s.Update(AnimTickMsg(time.Now()))
	if cmd != nil {
		t.Fatal("expected AnimTickMsg to stop ticking (cmd == nil) after Syncing finishes")
	}

	// 2. SearchPending lifecycle
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("golang")})
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.SearchPending || cmd == nil {
		t.Fatal("expected SearchPending=true and non-nil cmd on SubmitSearch")
	}
	beforeFrame := s.AnimFrame
	s, cmd = s.Update(AnimTickMsg(time.Now()))
	if s.AnimFrame != beforeFrame+1 || cmd == nil {
		t.Fatalf("expected AnimFrame to advance during SearchPending")
	}
	s, _ = s.Update(SearchDoneMsg{Query: "golang", Seq: s.searchSeq, Videos: cat.Results["golang"]})
	s, cmd = s.Update(AnimTickMsg(time.Now()))
	if cmd != nil {
		t.Fatal("expected AnimTickMsg to stop ticking after SearchDoneMsg")
	}

	// 3. PlayLoading lifecycle
	s.View = ViewHome
	s.SetSelection(0)
	s, cmd = s.PlaySelected("auto")
	if !s.PlayLoading || cmd == nil {
		t.Fatal("expected PlayLoading=true and non-nil cmd on PlaySelected")
	}
	beforeFrame = s.AnimFrame
	s, cmd = s.Update(AnimTickMsg(time.Now()))
	if s.AnimFrame != beforeFrame+1 || cmd == nil {
		t.Fatalf("expected AnimFrame to advance during PlayLoading")
	}
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{IsRunning: true, Title: "Go Concurrency"}})
	s, cmd = s.Update(AnimTickMsg(time.Now()))
	if cmd != nil {
		t.Fatal("expected AnimTickMsg to stop ticking after PlayerStatusMsg{IsRunning: true}")
	}
}

func TestStillWatchingTriggersAfterAutoplayLimitAndResumes(t *testing.T) {
	spy := newSpyPlayerEngine()
	cat := &catalog.FakeCatalog{
		Feeds: map[model.Tab][]model.Video{
			model.TabHome: {{ID: "v0", Title: "Seed Track", Duration: "03:00"}},
		},
	}
	s := NewState(Dependencies{Catalog: cat, Engine: spy, Store: store.NewFakeStore()})
	defer s.Close()
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})

	// Toggle Autoplay via 'A'
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if s.Autoplay || s.Toast != "⏹ Autoplay: OFF" {
		t.Fatalf("expected Autoplay=false toast=%q, got %v %q", "⏹ Autoplay: OFF", s.Autoplay, s.Toast)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if !s.Autoplay || s.Toast != "🔁 Autoplay: ON" {
		t.Fatalf("expected Autoplay=true toast=%q, got %v %q", "🔁 Autoplay: ON", s.Autoplay, s.Toast)
	}

	// Start playing Seed Track, then simulate user inactivity > 12s
	var cmd tea.Cmd
	s, cmd = s.PlaySelected("auto")
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{IsRunning: true, Title: "Seed Track"}})
	s.LastInteractionAt = time.Now().Add(-20 * time.Second)

	for i := 1; i <= 7; i++ {
		s, _ = s.Update(PlayerStatusMsg{Status: player.Status{
			IsRunning: true,
			Paused:    false,
			Title:     fmt.Sprintf("Auto Track %d", i),
		}})
		if s.ShowStillWatching {
			t.Fatalf("ShowStillWatching triggered early on track %d", i)
		}
		if s.AutoplayTracksSinceInput != i {
			t.Fatalf("AutoplayTracksSinceInput = %d, want %d", s.AutoplayTracksSinceInput, i)
		}
	}

	// 8th automatic track transition triggers Still Watching and deterministic Pause
	s, cmd = s.Update(PlayerStatusMsg{Status: player.Status{
		IsRunning: true,
		Paused:    false,
		Title:     "Auto Track 8",
	}})
	if !s.ShowStillWatching {
		t.Fatal("expected ShowStillWatching=true on 8th automatic track transition")
	}
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	_, _, _, pauseCalls := spy.snapshot()
	if pauseCalls != 1 {
		t.Fatalf("expected pauser.Pause called once, got %d", pauseCalls)
	}

	// Pressing Enter dismisses modal, resets counter, resumes playback, and sets toast
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.ShowStillWatching {
		t.Fatal("expected ShowStillWatching=false after Enter")
	}
	if s.AutoplayTracksSinceInput != 0 {
		t.Fatalf("expected AutoplayTracksSinceInput=0 after resume, got %d", s.AutoplayTracksSinceInput)
	}
	if s.Toast != "▶ Continuando reproducción" {
		t.Fatalf("Toast = %q, want %q", s.Toast, "▶ Continuando reproducción")
	}
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	resumed := false
	for _, c := range spy.Calls() {
		if c.Method == "TogglePause" {
			resumed = true
		}
	}
	if !resumed {
		t.Fatalf("expected TogglePause call to resume playback, calls=%+v", spy.Calls())
	}

	// Idle >= 60 minutes with >= 1 autoplay track also triggers Still Watching; Esc keeps paused.
	s.LastInteractionAt = time.Now().Add(-65 * time.Minute)
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{
		IsRunning: true,
		Paused:    false,
		Title:     "Auto Track 9",
	}})
	if !s.ShowStillWatching {
		t.Fatal("expected ShowStillWatching=true after >=60m idle autoplay transition")
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.ShowStillWatching || s.Toast != "⏸ Reproducción en pausa" || s.AutoplayTracksSinceInput != 0 {
		t.Fatalf("unexpected state after Esc on StillWatching: show=%v toast=%q count=%d",
			s.ShowStillWatching, s.Toast, s.AutoplayTracksSinceInput)
	}

	// Title update within 12s grace period after user activity does NOT increment AutoplayTracksSinceInput
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{
		IsRunning: true,
		Paused:    false,
		Title:     "Normalized Title Within Grace",
	}})
	if s.AutoplayTracksSinceInput != 0 {
		t.Fatalf("expected title change within 12s grace not to increment counter, got %d", s.AutoplayTracksSinceInput)
	}

	// Left mouse click dismisses StillWatching and resumes
	s.LastInteractionAt = time.Now().Add(-65 * time.Minute)
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{
		IsRunning: true,
		Paused:    false,
		Title:     "Auto Track 10",
	}})
	if !s.ShowStillWatching {
		t.Fatal("expected ShowStillWatching=true before mouse dismiss")
	}
	s, cmd = s.Update(tea.MouseMsg{X: 10, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if s.ShowStillWatching || s.Toast != "▶ Continuando reproducción" || s.AutoplayTracksSinceInput != 0 {
		t.Fatalf("unexpected state after mouse dismiss: show=%v toast=%q count=%d",
			s.ShowStillWatching, s.Toast, s.AutoplayTracksSinceInput)
	}
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)

	// Toggling 'A' inside ShowSettings flips Autoplay, and with Autoplay=false StillWatching never triggers
	s.ShowSettings = true
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if s.Autoplay || !s.ShowSettings || s.Toast != "⏹ Autoplay: OFF" {
		t.Fatalf("expected Autoplay=false inside settings modal, got autoplay=%v show=%v toast=%q",
			s.Autoplay, s.ShowSettings, s.Toast)
	}
	s.ShowSettings = false
	s.LastInteractionAt = time.Now().Add(-65 * time.Minute)
	for i := 11; i <= 20; i++ {
		s, _ = s.Update(PlayerStatusMsg{Status: player.Status{
			IsRunning: true,
			Paused:    false,
			Title:     fmt.Sprintf("Manual Track %d", i),
		}})
	}
	if s.ShowStillWatching {
		t.Fatal("expected ShowStillWatching to remain false when Autoplay is disabled")
	}
}

type channelSpyCatalog struct {
	*catalog.FakeCatalog
	channelVideos []model.Video
	lastChannel   string
}

func (c *channelSpyCatalog) ChannelVideos(_ context.Context, v model.Video, _ int) ([]model.Video, error) {
	c.lastChannel = v.Channel
	return c.channelVideos, nil
}

func TestV040ShortcutsVideoSponsorThemeChannelSubsChaptersLyrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{
				"trackName":    "De Música Ligera",
				"artistName":   "Soda Stereo",
				"syncedLyrics": "[00:05.00] Ella durmió\n[00:12.00] Al calor de las masas",
			},
		})
	}))
	defer srv.Close()

	lrc := lyrics.NewClient()
	lrc.BaseURL = srv.URL

	cat := &channelSpyCatalog{
		FakeCatalog: &catalog.FakeCatalog{
			Feeds: map[model.Tab][]model.Video{
				model.TabHome: {
					{ID: "vid11111111", Title: "De Música Ligera (Official Video)", Channel: "Soda Stereo", Duration: "04:50"},
					{ID: "vid22222222", Title: "Persiana Americana", Channel: "Soda Stereo", Duration: "05:00"},
				},
			},
		},
		channelVideos: []model.Video{
			{ID: "chan001", Title: "En la Ciudad de la Furia", Channel: "Soda Stereo", Duration: "05:40"},
			{ID: "chan002", Title: "Prófugos", Channel: "Soda Stereo", Duration: "05:20"},
		},
	}
	eng := player.NewFakeEngine()
	eng.SetChapters([]player.Chapter{
		{Title: "Intro", Time: 0},
		{Title: "Estribillo", Time: 62},
	})
	st := store.NewFakeStore()

	var cbMu sync.Mutex
	var savedThemes []string
	var savedSB []bool
	sbInit := true
	s := NewState(Dependencies{
		Catalog:             cat,
		Engine:              eng,
		Store:               st,
		LyricsClient:        lrc,
		InitialSponsorBlock: &sbInit,
		OnThemeChange: func(name string) error {
			cbMu.Lock()
			savedThemes = append(savedThemes, name)
			cbMu.Unlock()
			return nil
		},
		OnSponsorBlockChange: func(enabled bool) error {
			cbMu.Lock()
			savedSB = append(savedSB, enabled)
			cbMu.Unlock()
			return nil
		},
	})
	defer s.Close()
	s.Width = 120
	s.Height = 36
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})

	// 1. Hot Audio/Video toggle via 'V'
	var cmd tea.Cmd
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	for _, msg := range collectNonTickMsgs(cmd, 150*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if !strings.Contains(s.Toast, "Solo audio") {
		t.Fatalf("expected Solo audio toast after V, got %q", s.Toast)
	}
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	for _, msg := range collectNonTickMsgs(cmd, 150*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if !strings.Contains(s.Toast, "Video activado") {
		t.Fatalf("expected Video activado toast after second V, got %q", s.Toast)
	}

	// 2. SponsorBlock toggle via 'B'
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	if s.SponsorBlock || eng.SponsorBlockEnabled() || s.Toast != "🛡 SponsorBlock: OFF" {
		t.Fatalf("expected SponsorBlock OFF, state=%v engine=%v toast=%q", s.SponsorBlock, eng.SponsorBlockEnabled(), s.Toast)
	}
	cbMu.Lock()
	gotSB := slices.Clone(savedSB)
	cbMu.Unlock()
	if !slices.Equal(gotSB, []bool{false}) {
		t.Fatalf("OnSponsorBlockChange = %v, want [false]", gotSB)
	}

	// 3. Theme cycling via 't'
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	cbMu.Lock()
	gotThemes := slices.Clone(savedThemes)
	cbMu.Unlock()
	if s.ThemeName != "catppuccin" || !slices.Equal(gotThemes, []string{"catppuccin"}) {
		t.Fatalf("ThemeName=%q savedThemes=%v, want catppuccin", s.ThemeName, gotThemes)
	}

	// 4. Subscribe toggle via 's'
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	for _, msg := range collectNonTickMsgs(cmd, 150*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if !slices.Equal(s.Subscriptions, []string{"Soda Stereo"}) {
		t.Fatalf("Subscriptions = %v, want [Soda Stereo]", s.Subscriptions)
	}
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	for _, msg := range collectNonTickMsgs(cmd, 150*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if len(s.Subscriptions) != 0 {
		t.Fatalf("expected empty subscriptions after toggle off, got %v", s.Subscriptions)
	}

	// 5. Channel drill-down via 'g' and Esc back to ViewHome
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if s.View != ViewChannel || !s.ChannelPending {
		t.Fatalf("expected ViewChannel & ChannelPending after g, got view=%v pending=%v", s.View, s.ChannelPending)
	}
	for _, msg := range collectNonTickMsgs(cmd, 150*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if s.ChannelPending || len(s.VisibleItems()) != 2 || s.VisibleItems()[0].ID != "chan001" {
		t.Fatalf("unexpected channel view items=%+v pending=%v", s.VisibleItems(), s.ChannelPending)
	}
	if cat.lastChannel != "Soda Stereo" {
		t.Fatalf("expected channel fetch for Soda Stereo, got %q", cat.lastChannel)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.View != ViewHome {
		t.Fatalf("expected Esc from ViewChannel to return to ViewHome, got %v", s.View)
	}

	// 6. Chapters modal via 'C', navigate with 'j', seek with Enter
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	for _, msg := range collectNonTickMsgs(cmd, 150*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if !s.ShowChapters || len(s.Chapters) != 2 {
		t.Fatalf("expected ShowChapters=true with 2 chapters, got show=%v len=%d", s.ShowChapters, len(s.Chapters))
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	if s.ShowChapters || !strings.Contains(s.Toast, "Estribillo") {
		t.Fatalf("expected chapter seek & close, show=%v toast=%q", s.ShowChapters, s.Toast)
	}
	foundSeekAbs := false
	for _, c := range eng.Calls() {
		if c.Method == "SeekAbsolute" && c.Delta == 62 {
			foundSeekAbs = true
		}
	}
	if !foundSeekAbs {
		t.Fatalf("expected FakeEngine SeekAbsolute(62), calls=%+v", eng.Calls())
	}

	// 7. Synced lyrics via 'L'
	s.PlayerStatus = player.Status{IsRunning: true, Title: "De Música Ligera (Official Video)", TimePos: 14}
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if !s.ShowLyrics || !s.LyricsLoading {
		t.Fatalf("expected ShowLyrics=true LyricsLoading=true after L, got show=%v loading=%v", s.ShowLyrics, s.LyricsLoading)
	}
	for _, msg := range collectNonTickMsgs(cmd, 250*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if s.LyricsLoading || len(s.Lyrics.Synced) != 2 || s.LyricsErr != "" {
		t.Fatalf("unexpected lyrics result: loading=%v synced=%v err=%q", s.LyricsLoading, s.Lyrics.Synced, s.LyricsErr)
	}
	if idx := lyrics.ActiveLineIndex(s.Lyrics.Synced, s.PlayerStatus.TimePos); idx != 1 {
		t.Fatalf("ActiveLineIndex at 14s = %d, want 1", idx)
	}
}

func TestApp_MouseHoverQualityModalAndLowercaseKeys(t *testing.T) {
	cat := &catalog.FakeCatalog{
		Feeds: map[model.Tab][]model.Video{
			model.TabHome: {
				{ID: "v0", Title: "Track Zero", Channel: "Ch0", Duration: "03:00"},
				{ID: "v1", Title: "Track One", Channel: "Ch1", Duration: "04:00"},
				{ID: "v2", Title: "Track Two", Channel: "Ch2", Duration: "05:00"},
			},
		},
	}
	eng := player.NewFakeEngine()
	var savedQ []int
	var savedAudio []bool
	var mu sync.Mutex

	s := NewState(Dependencies{
		Catalog: cat,
		Engine:  eng,
		Store:   store.NewFakeStore(),
		OnQualityChange: func(q int) error {
			mu.Lock()
			savedQ = append(savedQ, q)
			mu.Unlock()
			return nil
		},
		OnAudioOnlyChange: func(a bool) error {
			mu.Lock()
			savedAudio = append(savedAudio, a)
			mu.Unlock()
			return nil
		},
	})
	defer s.Close()
	s.Width, s.Height = 120, 38
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})

	// 1. Mouse hover selects card 1 without starting playback, then single click plays it immediately.
	col1X := s.mainOriginX() + s.gridCardWidth() + 4
	s, cmd := s.Update(tea.MouseMsg{
		X:      col1X,
		Y:      7,
		Action: tea.MouseActionMotion,
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd on hover motion, got non-nil")
	}
	if s.SelectedIdx != 1 || len(eng.Calls()) != 0 {
		t.Fatalf("expected hover to select idx 1 without playing, got idx=%d calls=%v", s.SelectedIdx, eng.Calls())
	}
	s, cmd = s.Update(tea.MouseMsg{
		X:      col1X,
		Y:      7,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	_ = collectNonTickMsgs(cmd, 100*time.Millisecond)
	if len(eng.Calls()) == 0 || eng.Calls()[0].Video.ID != "v1" {
		t.Fatalf("expected immediate Play(v1) on single click after hover, got calls=%+v", eng.Calls())
	}

	// 2. 'f' opens ShowQualityModal; selecting Solo Audio sets AudioOnly and SetPlaybackProfile; '360p Ahorro' sets 360.
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if !s.ShowQualityModal {
		t.Fatal("expected ShowQualityModal=true after f key")
	}
	// Press '5' for Solo Audio (index 4).
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	if s.ShowQualityModal || !s.AudioOnly || s.QualityLabel() != "Solo Audio" {
		t.Fatalf("expected Solo Audio applied, modal=%v audio=%v label=%q", s.ShowQualityModal, s.AudioOnly, s.QualityLabel())
	}
	if !eng.AudioOnly() {
		t.Fatal("expected FakeEngine AudioOnly=true via SetPlaybackProfile")
	}
	// Click [Calidad: Solo Audio ▾] or open modal via 'f' and click option 3 ("360p Ahorro").
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	leftX, _, _, _, optStartY := s.QualityModalBounds()
	s, cmd = s.Update(tea.MouseMsg{
		X:      leftX + 4,
		Y:      optStartY + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	if s.Quality != 360 || s.AudioOnly || s.QualityLabel() != "360p Ahorro" {
		t.Fatalf("expected 360p Ahorro applied by click, q=%d audio=%v label=%q", s.Quality, s.AudioOnly, s.QualityLabel())
	}

	mu.Lock()
	if !slices.Equal(savedQ, []int{720, 360}) || !slices.Equal(savedAudio, []bool{true, false}) {
		mu.Unlock()
		t.Fatalf("saved callbacks mismatch: q=%v audio=%v", savedQ, savedAudio)
	}
	mu.Unlock()

	// 3. Lowercase 'b' toggles SponsorBlock, lowercase 'l' toggles Lyrics, and SwitchTab resets FocusMain.
	s.Focus = FocusSidebar
	s, _ = s.SwitchTab(model.TabMusic)
	if s.Focus != FocusMain {
		t.Fatalf("expected SwitchTab to reset Focus=FocusMain, got %v", s.Focus)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if s.SponsorBlock {
		t.Fatal("expected lowercase b to toggle SponsorBlock OFF")
	}
	s.PlayerStatus = player.Status{IsRunning: true, Title: "Track One"}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !s.ShowLyrics {
		t.Fatal("expected lowercase l to toggle ShowLyrics ON")
	}
}

func TestApp_SettingsStudioLiveChipAndStaleTitleGuard(t *testing.T) {
	cat := &catalog.FakeCatalog{
		Feeds: map[model.Tab][]model.Video{
			model.TabHome: {
				{ID: "v1", Title: "Song A", Channel: "Ch1", Duration: "03:00"},
				{ID: "v2", Title: "Song B", Channel: "Ch2", Duration: "04:00"},
			},
		},
		Results: map[string][]model.Video{
			"en vivo live": {
				{ID: "live99", Title: "24/7 Radio Live", Channel: "Radio", IsLive: true},
			},
		},
	}
	eng := player.NewFakeEngine()
	var themes []string
	var mu sync.Mutex

	s := NewState(Dependencies{
		Catalog: cat,
		Engine:  eng,
		Store:   store.NewFakeStore(),
		OnThemeChange: func(name string) error {
			mu.Lock()
			themes = append(themes, name)
			mu.Unlock()
			return nil
		},
	})
	defer s.Close()
	s.Width, s.Height = 120, 38
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: cat.Feeds[model.TabHome]})

	// 1. Switching track keeps new title locked while old track status ticks are still in flight.
	s.PlayerStatus = player.Status{IsRunning: true, Title: "Song A"}
	s.SetSelection(1)
	s, _ = s.PlaySelected("auto")
	if s.PlayerStatus.Title != "Song B" {
		t.Fatalf("expected immediate PlayerStatus.Title=Song B, got %q", s.PlayerStatus.Title)
	}
	s, _ = s.Update(PlayerStatusMsg{Status: player.Status{IsRunning: true, Title: "Song A", TimePos: 42}})
	if s.PlayerStatus.Title != "Song B" || !s.PlayLoading {
		t.Fatalf("stale Song A status overwrote UI during switch: title=%q loading=%v", s.PlayerStatus.Title, s.PlayLoading)
	}
	s, _ = s.Update(PlayStartedMsg{})
	if s.PlayLoading {
		t.Fatal("expected PlayLoading=false after PlayStartedMsg when mpv already running")
	}

	// 2. Clicking "En vivo" chip fetches live streams automatically when feed has no live items.
	s.HomeChip = 0
	// Second chip at y=4: " Todo " (width=6) at x=12..17, " En vivo " (width=9) at x=19..27
	var cmd tea.Cmd
	s, cmd = s.Update(tea.MouseMsg{X: 21, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if s.HomeChip != 1 || cmd == nil {
		t.Fatalf("expected HomeChip=1 and live search cmd, got chip=%d cmd=%v", s.HomeChip, cmd)
	}
	for _, msg := range collectNonTickMsgs(cmd, 200*time.Millisecond) {
		s, _ = s.Update(msg)
	}
	if items := s.VisibleItems(); len(items) != 1 || items[0].ID != "live99" {
		t.Fatalf("expected En vivo chip to load live99, got %+v", items)
	}

	// 3. Settings Studio supports keyboard nav + hover + click + cycling to "terminal" theme.
	s.ShowSettings = true
	s.SettingsIdx = 0
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}) // backward from youtube-dark -> terminal
	_ = collectNonTickMsgs(cmd, 100*time.Millisecond)
	if s.ThemeName != "terminal" {
		t.Fatalf("expected ThemeName=terminal after backward step from youtube-dark, got %q", s.ThemeName)
	}
	leftX, topY, _, _ := s.SettingsModalBounds()
	// Hover row 5 (Autoplay) and click it to toggle false.
	rowY := topY + 1 + SettingsItemRowOffsets[5]
	s, _ = s.Update(tea.MouseMsg{X: leftX + 6, Y: rowY, Action: tea.MouseActionMotion})
	if s.SettingsIdx != 5 {
		t.Fatalf("expected mouse hover to highlight SettingsIdx=5, got %d", s.SettingsIdx)
	}
	s, cmd = s.Update(tea.MouseMsg{X: leftX + 6, Y: rowY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	_ = collectNonTickMsgs(cmd, 100*time.Millisecond)
	if s.Autoplay {
		t.Fatal("expected click on Autoplay row to toggle Autoplay OFF")
	}
}

type profileOrderSpy struct {
	*player.FakeEngine
	mu     sync.Mutex
	events []string
}

func (e *profileOrderSpy) SetPlaybackProfile(ctx context.Context, quality int, audioOnly bool) error {
	e.mu.Lock()
	e.events = append(e.events, fmt.Sprintf("SetPlaybackProfile:%d:%v", quality, audioOnly))
	e.mu.Unlock()
	return e.FakeEngine.SetPlaybackProfile(ctx, quality, audioOnly)
}

func (e *profileOrderSpy) Prefetch(v model.Video) {
	e.mu.Lock()
	e.events = append(e.events, "Prefetch:"+v.ID)
	e.mu.Unlock()
}

func (e *profileOrderSpy) snapshotEvents() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

func TestApp_MouseHoverDoesNotScrollAndSettingsQualityClearsAudioOnly(t *testing.T) {
	videos := make([]model.Video, 12)
	for i := range videos {
		videos[i] = model.Video{
			ID:       fmt.Sprintf("vid%02d", i),
			Title:    fmt.Sprintf("Track %02d", i),
			Channel:  "Channel",
			Duration: "03:30",
		}
	}
	cat := &catalog.FakeCatalog{
		Feeds: map[model.Tab][]model.Video{model.TabHome: videos},
	}
	spy := &profileOrderSpy{FakeEngine: player.NewFakeEngine()}
	s := NewState(Dependencies{
		Catalog:            cat,
		Engine:             spy,
		Store:              store.NewFakeStore(),
		InitialQuality:     1080,
		InitialDefaultMode: "audio",
		InitialAudioOnly:   true,
	})
	defer s.Close()

	// Size 120x38 with no player bar -> bodyH=32, gridCardHeight=15 => 2 full visible rows
	// (y=5..19 and y=20..34), with row 2 partially visible at y=35..36.
	s, _ = s.Update(tea.WindowSizeMsg{Width: 120, Height: 38})
	s, _ = s.Update(FeedLoadedMsg{Tab: model.TabHome, Videos: videos})

	if vis := s.VisibleGridRows(); vis != 2 {
		t.Fatalf("VisibleGridRows = %d, want 2 at 120x38", vis)
	}

	// Hovering over the full visible rows and moving down into the partial bottom row must NEVER scroll.
	col0X := s.mainOriginX() + 2
	col1X := s.mainOriginX() + s.gridCardWidth() + 4
	s, _ = s.Update(tea.MouseMsg{X: col1X, Y: 7, Action: tea.MouseActionMotion})
	if s.SelectedIdx != 1 || s.ScrollOffset != 0 {
		t.Fatalf("after hover row 0 col 1: idx=%d scroll=%d, want (1, 0)", s.SelectedIdx, s.ScrollOffset)
	}
	s, _ = s.Update(tea.MouseMsg{X: col0X, Y: 22, Action: tea.MouseActionMotion})
	if s.SelectedIdx != s.GridCols() || s.ScrollOffset != 0 {
		t.Fatalf("after hover row 1 col 0: idx=%d scroll=%d, want (%d, 0)", s.SelectedIdx, s.ScrollOffset, s.GridCols())
	}
	for y := 34; y <= 37; y++ {
		s, _ = s.Update(tea.MouseMsg{X: col0X, Y: y, Action: tea.MouseActionMotion})
		if s.ScrollOffset != 0 {
			t.Fatalf("mouse motion at y=%d mutated ScrollOffset to %d, want 0", y, s.ScrollOffset)
		}
		if s.SelectedIdx >= 2*s.GridCols() {
			t.Fatalf("mouse motion into partial row y=%d leaked SelectedIdx=%d across viewport boundary", y, s.SelectedIdx)
		}
	}

	// Explicit WheelDown scrolls forward by one grid row, and subsequent hovers do not change ScrollOffset.
	s, _ = s.Update(tea.MouseMsg{X: col0X, Y: 22, Button: tea.MouseButtonWheelDown})
	if s.ScrollOffset != 1 {
		t.Fatalf("ScrollOffset after WheelDown = %d, want 1", s.ScrollOffset)
	}
	s, _ = s.Update(tea.MouseMsg{X: col1X, Y: 7, Action: tea.MouseActionMotion})
	if s.ScrollOffset != 1 || s.SelectedIdx != s.GridCols()+1 {
		t.Fatalf("hover after WheelDown = (idx=%d, scroll=%d), want (idx=%d, scroll=1)",
			s.SelectedIdx, s.ScrollOffset, s.GridCols()+1)
	}

	// Adjusting "Resolución máxima" (row 2) in Settings clears AudioOnly, switches DefaultMode to window,
	// and executes SetPlaybackProfile before Prefetch.
	spy.mu.Lock()
	spy.events = nil
	spy.mu.Unlock()
	s.ShowSettings = true
	s, cmd := s.AdjustSetting(2, 1) // 1080 -> 720
	_ = collectNonTickMsgs(cmd, 150*time.Millisecond)
	if s.AudioOnly || s.DefaultMode != "window" || s.Quality != 720 {
		t.Fatalf("expected AudioOnly=false DefaultMode=window Quality=720, got audio=%v mode=%q q=%d",
			s.AudioOnly, s.DefaultMode, s.Quality)
	}
	events := spy.snapshotEvents()
	wantSet := "SetPlaybackProfile:720:false"
	wantPrefetchPrefix := "Prefetch:"
	setIdx, prefIdx := -1, -1
	for i, ev := range events {
		if ev == wantSet && setIdx < 0 {
			setIdx = i
		}
		if strings.HasPrefix(ev, wantPrefetchPrefix) && prefIdx < 0 {
			prefIdx = i
		}
	}
	if setIdx < 0 || prefIdx <= setIdx {
		t.Fatalf("expected %s before %s*, events=%v", wantSet, wantPrefetchPrefix, events)
	}
}
