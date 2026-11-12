package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/player"
)

const (
	searchTimeout = 15 * time.Second
	feedTimeout   = 15 * time.Second
	toastTTL      = 4 * time.Second
	uiTickRate    = 250 * time.Millisecond // 4 Hz cap
)

// FeedLoadedMsg arrives when a catalog feed finishes loading.
type FeedLoadedMsg struct {
	Tab    model.Tab
	Videos []model.Video
	Err    error
}

// SearchDoneMsg arrives when an asynchronous search finishes.
type SearchDoneMsg struct {
	Query  string
	Videos []model.Video
	Err    error
}

// SavedLoadedMsg arrives when saved/history/subscriptions finish loading.
type SavedLoadedMsg struct {
	Saved   []model.Video
	History []model.Video
	Subs    []string
}

// ToggledSaveMsg arrives after Store.ToggleSaved completes.
type ToggledSaveMsg struct {
	Video model.Video
	Saved bool
	Err   error
}

// PlayerStatusMsg carries a pushed status update from Engine.Subscribe.
type PlayerStatusMsg struct {
	Status player.Status
}

// ToastExpiredMsg clears the toast if its ID still matches.
type ToastExpiredMsg struct {
	ID int
}

// TickMsg drives bounded UI progress interpolation (4 Hz) when playing.
type TickMsg time.Time

// Init starts initial feed/store loading and subscribes to player status push updates.
func (s *State) Init() tea.Cmd {
	return tea.Batch(
		s.loadFeedCmd(s.ActiveTab),
		s.loadStoreCmd(),
		s.waitPlayerStatusCmd(),
	)
}

// Update handles Bubble Tea messages and returns updated state + next command.
func (s *State) Update(msg tea.Msg) (*State, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.Width = msg.Width
		s.Height = msg.Height
		s.Ready = true
		return s, nil

	case FeedLoadedMsg:
		s.Syncing = false
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error cargando %s: %v", msg.Tab.String(), msg.Err))
		}
		s.Feeds[msg.Tab] = msg.Videos
		if msg.Tab == s.ActiveTab {
			s.SetSelection(s.SelectedIdx)
		}
		return s, nil

	case SearchDoneMsg:
		if msg.Query != s.SearchQuery {
			return s, nil // stale search result
		}
		s.SearchPending = false
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error buscando: %v", msg.Err))
		}
		s.SearchResults = msg.Videos
		s.ResultIdx = 0
		s.ResultScroll = 0
		return s, nil

	case SavedLoadedMsg:
		s.SavedIDs = make(map[string]bool, len(msg.Saved))
		for _, v := range msg.Saved {
			s.SavedIDs[v.ID] = true
		}
		s.HistoryIDs = make(map[string]bool, len(msg.History))
		for _, v := range msg.History {
			s.HistoryIDs[v.ID] = true
		}
		s.Subscriptions = msg.Subs
		if _, ok := s.Feeds[model.TabHistory]; !ok && len(msg.History) > 0 {
			s.Feeds[model.TabHistory] = msg.History
		}
		return s, nil

	case ToggledSaveMsg:
		if msg.Err != nil {
			return s, s.toastCmd(fmt.Sprintf("Error al guardar: %v", msg.Err))
		}
		if msg.Saved {
			s.SavedIDs[msg.Video.ID] = true
			return s, s.toastCmd("★ Guardado: " + msg.Video.Title)
		}
		delete(s.SavedIDs, msg.Video.ID)
		return s, s.toastCmd("Quitado de guardados")

	case PlayerStatusMsg:
		wasPlaying := s.PlayerStatus.IsRunning && !s.PlayerStatus.Paused
		s.PlayerStatus = msg.Status
		nowPlaying := msg.Status.IsRunning && !msg.Status.Paused
		cmds := []tea.Cmd{s.waitPlayerStatusCmd()}
		if !wasPlaying && nowPlaying {
			cmds = append(cmds, s.tickCmd())
		}
		return s, tea.Batch(cmds...)

	case TickMsg:
		if s.PlayerStatus.IsRunning && !s.PlayerStatus.Paused {
			return s, s.tickCmd()
		}
		return s, nil

	case ToastExpiredMsg:
		if msg.ID == s.ToastID {
			s.Toast = ""
		}
		return s, nil

	case tea.MouseMsg:
		return s.handleMouse(msg)

	case tea.KeyMsg:
		return s.handleKey(msg)
	}

	return s, nil
}

func (s *State) handleMouse(msg tea.MouseMsg) (*State, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() + step)
		return s, nil
	case tea.MouseButtonWheelUp:
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() - step)
		return s, nil
	}
	return s, nil
}

// GridCols returns the current number of grid columns for the terminal width.
func (s *State) GridCols() int {
	mainW := s.Width
	if s.Width >= 140 && s.View == ViewHome {
		mainW -= 23
	} else if s.Width >= 100 {
		mainW -= 9
	}
	cols := (mainW + 2) / 32
	if cols < 1 {
		return 1
	}
	return cols
}

func (s *State) handleKey(msg tea.KeyMsg) (*State, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		s.Close()
		return s, tea.Quit
	}

	if s.ShowHelp {
		if key == "?" || key == "esc" {
			s.ShowHelp = false
		}
		return s, nil
	}

	if s.Focus == FocusSearch {
		switch key {
		case "esc":
			s.Focus = FocusMain
			return s, nil
		case "enter":
			return s.SubmitSearch()
		case "backspace":
			r := []rune(s.SearchDraft)
			if len(r) > 0 {
				s.SearchDraft = string(r[:len(r)-1])
			}
			return s, nil
		default:
			if msg.Type == tea.KeyRunes {
				s.SearchDraft += string(msg.Runes)
			}
			return s, nil
		}
	}

	switch key {
	case "Q":
		s.Close()
		return s, tea.Quit
	case "?":
		s.ShowHelp = true
		return s, nil
	case "/":
		s.Focus = FocusSearch
		s.SearchDraft = s.SearchQuery
		return s, nil
	case "esc":
		if s.View == ViewSearch {
			s.View = ViewHome
			s.Focus = FocusMain
		}
		return s, nil
	case "j", "down":
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() + step)
		return s, nil
	case "k", "up":
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() - step)
		return s, nil
	case "l", "right":
		s.SetSelection(s.activeIdx() + 1)
		return s, nil
	case "h", "left":
		s.SetSelection(s.activeIdx() - 1)
		return s, nil
	case "1":
		return s.SwitchTab(model.TabHome)
	case "2":
		return s.SwitchTab(model.TabMusic)
	case "3":
		return s.SwitchTab(model.TabSubscriptions)
	case "4":
		return s.SwitchTab(model.TabDownloads)
	case "5":
		return s.SwitchTab(model.TabHistory)
	case "[":
		return s.CycleChip(-1), nil
	case "]":
		return s.CycleChip(1), nil
	case "enter":
		return s.PlaySelected("auto")
	case "a":
		return s.PlaySelected("audio")
	case "v":
		return s.PlaySelected("video")
	case "w":
		return s.ToggleSaveSelected()
	case "q":
		return s.EnqueueSelected()
	case " ":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
	case "n":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Next(ctx) }, "⏭ Siguiente")
	case "p":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.Prev(ctx) }, "⏮ Anterior")
	case "r":
		s.Syncing = true
		return s, s.loadFeedCmd(s.ActiveTab)
	}

	return s, nil
}

func (s *State) activeIdx() int {
	if s.View == ViewSearch {
		return s.ResultIdx
	}
	return s.SelectedIdx
}

// SwitchTab changes the active sidebar tab and loads its feed if needed.
func (s *State) SwitchTab(tab model.Tab) (*State, tea.Cmd) {
	s.View = ViewHome
	s.ActiveTab = tab
	s.SelectedIdx = 0
	s.ScrollOffset = 0
	s.HomeChip = 0
	if _, ok := s.Feeds[tab]; ok {
		return s, nil
	}
	s.Syncing = true
	return s, s.loadFeedCmd(tab)
}

// CycleChip steps through the active view's filter chips.
func (s *State) CycleChip(delta int) *State {
	if s.View == ViewSearch {
		n := len(SearchChips)
		s.SearchChip = (s.SearchChip + delta + n) % n
		s.ResultIdx = 0
		return s
	}
	n := len(HomeChips)
	s.HomeChip = (s.HomeChip + delta + n) % n
	s.SelectedIdx = 0
	return s
}

// SubmitSearch cancels any in-flight search and starts a new one.
func (s *State) SubmitSearch() (*State, tea.Cmd) {
	q := strings.TrimSpace(s.SearchDraft)
	if q == "" {
		s.Focus = FocusMain
		return s, nil
	}
	if s.searchCancel != nil {
		s.searchCancel()
	}
	s.SearchQuery = q
	s.View = ViewSearch
	s.Focus = FocusMain
	s.SearchPending = true
	s.SearchChip = 0
	s.ResultIdx = 0

	ctx, cancel := context.WithTimeout(s.appCtx, searchTimeout)
	s.searchCancel = cancel

	return s, func() tea.Msg {
		videos, err := s.Deps.Catalog.Search(ctx, q)
		return SearchDoneMsg{Query: q, Videos: videos, Err: err}
	}
}

// PlaySelected launches the highlighted video in the player engine and records history.
func (s *State) PlaySelected(mode string) (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Engine == nil {
		return s, nil
	}
	s.HistoryIDs[v.ID] = true
	toastCmd := s.toastCmd("▶ Abriendo en mpv: " + v.Title)
	playCmd := func() tea.Msg {
		if err := s.Deps.Engine.Play(s.appCtx, v, mode); err != nil {
			return ToastExpiredMsg{} // noop placeholder; surfaced via status if needed
		}
		if s.Deps.Store != nil {
			_ = s.Deps.Store.RecordHistory(s.appCtx, v)
		}
		return nil
	}
	return s, tea.Batch(toastCmd, playCmd)
}

// EnqueueSelected appends the highlighted video to the player queue.
func (s *State) EnqueueSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Engine == nil {
		return s, nil
	}
	s.Queue = append(s.Queue, v)
	toastCmd := s.toastCmd(fmt.Sprintf("En cola (%d): %s", len(s.Queue), v.Title))
	enqCmd := func() tea.Msg {
		_ = s.Deps.Engine.Enqueue(s.appCtx, v)
		return nil
	}
	return s, tea.Batch(toastCmd, enqCmd)
}

// ToggleSaveSelected persists watch-later state via Store.
func (s *State) ToggleSaveSelected() (*State, tea.Cmd) {
	v, ok := s.SelectedVideo()
	if !ok || s.Deps.Store == nil {
		return s, nil
	}
	return s, func() tea.Msg {
		saved, err := s.Deps.Store.ToggleSaved(s.appCtx, v)
		return ToggledSaveMsg{Video: v, Saved: saved, Err: err}
	}
}

func (s *State) loadFeedCmd(tab model.Tab) tea.Cmd {
	if s.Deps.Catalog == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.appCtx, feedTimeout)
		defer cancel()
		videos, err := s.Deps.Catalog.Feed(ctx, tab)
		return FeedLoadedMsg{Tab: tab, Videos: videos, Err: err}
	}
}

func (s *State) loadStoreCmd() tea.Cmd {
	if s.Deps.Store == nil {
		return nil
	}
	return func() tea.Msg {
		saved, _ := s.Deps.Store.Saved(s.appCtx)
		history, _ := s.Deps.Store.History(s.appCtx)
		subs, _ := s.Deps.Store.Subscriptions(s.appCtx)
		return SavedLoadedMsg{Saved: saved, History: history, Subs: subs}
	}
}

func (s *State) waitPlayerStatusCmd() tea.Cmd {
	if s.Deps.Engine == nil {
		return nil
	}
	ch := s.Deps.Engine.Subscribe(s.appCtx)
	return func() tea.Msg {
		st, ok := <-ch
		if !ok {
			return nil
		}
		return PlayerStatusMsg{Status: st}
	}
}

func (s *State) engineCmd(fn func(context.Context) error, toast string) tea.Cmd {
	if s.Deps.Engine == nil {
		return nil
	}
	var tCmd tea.Cmd
	if toast != "" {
		tCmd = s.toastCmd(toast)
	}
	execCmd := func() tea.Msg {
		_ = fn(s.appCtx)
		return nil
	}
	return tea.Batch(tCmd, execCmd)
}

func (s *State) toastCmd(msg string) tea.Cmd {
	id := s.setToast(msg)
	return tea.Tick(toastTTL, func(time.Time) tea.Msg {
		return ToastExpiredMsg{ID: id}
	})
}

func (s *State) tickCmd() tea.Cmd {
	return tea.Tick(uiTickRate, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
