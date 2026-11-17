package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

func TestMouseInteractions(t *testing.T) {
	t.Run("click search bar focuses search", func(t *testing.T) {
		s, _, _, _ := newTestState()
		defer s.Close()
		s.Width, s.Height = 120, 36
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
		s.Width, s.Height = 120, 36
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
		s.Width, s.Height = 120, 36
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
		s.Width, s.Height = 120, 36
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
		s.Width, s.Height = 120, 36
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
	s.Width, s.Height = 120, 36
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
	s, _ = s.Update(cmd())
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
	s, _ = s.Update(cmd())
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
	go func() { done1 <- cmd1() }()
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
	msg2 := cmd2()
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
