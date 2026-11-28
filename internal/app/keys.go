package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/e12jassir/iutui/internal/model"
)

// GridCols returns the current number of grid columns for the terminal width.
func (s *State) GridCols() int {
	mainW := s.MainInnerWidth()
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
		switch key {
		case "?", "esc":
			s.ShowHelp = false
		case "S":
			s.ShowHelp = false
			s.ShowSettings = true
		}
		return s, nil
	}

	if s.ShowQueue {
		switch key {
		case "esc", "u", "q":
			s.ShowQueue = false
			return s, nil
		case "j", "down":
			if len(s.MPVPlaylist) > 0 && s.QueueIdx < len(s.MPVPlaylist)-1 {
				s.QueueIdx++
				s.syncQueueScroll()
			}
			return s, nil
		case "k", "up":
			if s.QueueIdx > 0 {
				s.QueueIdx--
				s.syncQueueScroll()
			}
			return s, nil
		case "pgdown", "ctrl+d":
			if len(s.MPVPlaylist) > 0 {
				step := max(1, s.queueVisibleCount())
				s.QueueIdx = min(len(s.MPVPlaylist)-1, s.QueueIdx+step)
				s.syncQueueScroll()
			}
			return s, nil
		case "pgup", "ctrl+u":
			if len(s.MPVPlaylist) > 0 {
				step := max(1, s.queueVisibleCount())
				s.QueueIdx = max(0, s.QueueIdx-step)
				s.syncQueueScroll()
			}
			return s, nil
		case "home", "g":
			if len(s.MPVPlaylist) > 0 {
				s.QueueIdx = 0
				s.syncQueueScroll()
			}
			return s, nil
		case "end", "G":
			if len(s.MPVPlaylist) > 0 {
				s.QueueIdx = len(s.MPVPlaylist) - 1
				s.syncQueueScroll()
			}
			return s, nil
		case "enter", " ":
			return s.PlayQueueSelected()
		case "x", "d", "backspace", "delete":
			return s.RemoveQueueSelected()
		case "c":
			return s.ClearQueue()
		}
		return s, nil
	}

	if s.ShowChapters {
		switch key {
		case "esc", "q", "o", "O":
			s.ShowChapters = false
			return s, nil
		case "j", "down":
			if len(s.Chapters) > 0 && s.ChapterIdx < len(s.Chapters)-1 {
				s.ChapterIdx++
			}
			return s, nil
		case "k", "up":
			if s.ChapterIdx > 0 {
				s.ChapterIdx--
			}
			return s, nil
		case "enter", " ":
			if s.ChapterIdx >= 0 && s.ChapterIdx < len(s.Chapters) && s.Deps.Engine != nil {
				ch := s.Chapters[s.ChapterIdx]
				s.ShowChapters = false
				if cp, ok := s.Deps.Engine.(chaptersProvider); ok {
					return s, s.engineCmd(func(ctx context.Context) error {
						return cp.SeekAbsolute(ctx, ch.Time)
					}, i18n.Format(s.Catalog().App.ToastChapterFmt, map[string]string{"title": ch.Title}))
				}
			}
			s.ShowChapters = false
			return s, nil
		}
		return s, nil
	}

	if s.ShowQualityModal {
		switch key {
		case "esc", "f", "F", "q":
			s.ShowQualityModal = false
			return s, nil
		case "j", "down":
			if s.QualityIdx < len(QualityOptions)-1 {
				s.QualityIdx++
			}
			return s, nil
		case "k", "up":
			if s.QualityIdx > 0 {
				s.QualityIdx--
			}
			return s, nil
		case "1", "2", "3", "4", "5":
			idx := int(key[0] - '1')
			return s.ApplyQualityOption(idx)
		case "enter", " ":
			return s.ApplyQualityOption(s.QualityIdx)
		}
		return s, nil
	}

	if s.ShowSettings {
		switch key {
		case "esc", "S", "q":
			s.ShowSettings = false
			return s, nil
		case "?":
			s.ShowSettings = false
			s.ShowHelp = true
			return s, nil
		case "j", "down":
			s.SettingsIdx = (s.SettingsIdx + 1) % SettingsRowCount
			return s, nil
		case "k", "up":
			s.SettingsIdx = (s.SettingsIdx + SettingsRowCount - 1) % SettingsRowCount
			return s, nil
		case "enter", " ", "right", "l":
			return s.AdjustSetting(s.SettingsIdx, 1)
		case "left", "h":
			return s.AdjustSetting(s.SettingsIdx, -1)
		case "f", "F":
			s.ShowSettings = false
			return s.OpenQualityModal(), nil
		case "c":
			return s.CycleQuality()
		case "a", "A":
			return s.ToggleAutoplay()
		case "b", "B":
			return s.ToggleSponsorBlock()
		case "t", "T":
			return s.CycleTheme()
		case "x", "V":
			return s.ToggleVideoMode()
		case "C":
			return s, s.toggleSubtitlesCmd()
		case "P":
			return s, s.togglePiPCmd()
		}
		return s, nil
	}

	if s.Focus == FocusSearch {
		switch key {
		case "esc":
			s.Focus = FocusMain
			s.setSearchDraft("", 0)
			s.SearchSuggestions = nil
			s.SuggestIdx = -1
			if s.suggestCancel != nil {
				s.suggestCancel()
			}
			return s, nil
		case "enter":
			if s.SuggestIdx >= 0 && s.SuggestIdx < len(s.SearchSuggestions) {
				sug := s.SearchSuggestions[s.SuggestIdx]
				s.setSearchDraft(sug, len([]rune(sug)))
			}
			return s.SubmitSearch()
		case "down":
			if len(s.SearchSuggestions) > 0 {
				s.SuggestIdx = (s.SuggestIdx + 1) % len(s.SearchSuggestions)
			}
			return s, nil
		case "up":
			if len(s.SearchSuggestions) > 0 {
				if s.SuggestIdx <= 0 {
					s.SuggestIdx = len(s.SearchSuggestions) - 1
				} else {
					s.SuggestIdx--
				}
			}
			return s, nil
		case "left", "ctrl+b":
			if cur := s.SearchCursorPos(); cur > 0 {
				s.SearchCursor = cur - 1
			}
			return s, nil
		case "right", "ctrl+f":
			if cur := s.SearchCursorPos(); cur < len([]rune(s.SearchDraft)) {
				s.SearchCursor = cur + 1
			}
			return s, nil
		case "home", "ctrl+a":
			s.SearchCursorPos()
			s.SearchCursor = 0
			return s, nil
		case "end", "ctrl+e":
			s.SearchCursor = len([]rune(s.SearchDraft))
			s.searchCursorSynced = s.SearchDraft
			return s, nil
		case "tab":
			if len(s.SearchSuggestions) > 0 {
				if s.SuggestIdx < 0 || s.SuggestIdx >= len(s.SearchSuggestions) {
					s.SuggestIdx = 0
				}
				sug := s.SearchSuggestions[s.SuggestIdx]
				s.setSearchDraft(sug, len([]rune(sug)))
				return s, s.onSearchDraftChanged()
			}
			return s, nil
		case "ctrl+u":
			cur := s.SearchCursorPos()
			r := []rune(s.SearchDraft)
			s.setSearchDraft(string(r[cur:]), 0)
			return s, s.onSearchDraftChanged()
		case "ctrl+k":
			cur := s.SearchCursorPos()
			r := []rune(s.SearchDraft)
			s.setSearchDraft(string(r[:cur]), cur)
			return s, s.onSearchDraftChanged()
		case "ctrl+w", "alt+backspace":
			cur := s.SearchCursorPos()
			r := []rune(s.SearchDraft)
			newPrefix := deleteLastWord(string(r[:cur]))
			suffix := string(r[cur:])
			s.setSearchDraft(newPrefix+suffix, len([]rune(newPrefix)))
			return s, s.onSearchDraftChanged()
		case "backspace", "ctrl+h":
			cur := s.SearchCursorPos()
			r := []rune(s.SearchDraft)
			if cur > 0 {
				next := make([]rune, 0, len(r)-1)
				next = append(next, r[:cur-1]...)
				next = append(next, r[cur:]...)
				s.setSearchDraft(string(next), cur-1)
			}
			return s, s.onSearchDraftChanged()
		case "delete", "ctrl+d":
			cur := s.SearchCursorPos()
			r := []rune(s.SearchDraft)
			if cur < len(r) {
				next := make([]rune, 0, len(r)-1)
				next = append(next, r[:cur]...)
				next = append(next, r[cur+1:]...)
				s.setSearchDraft(string(next), cur)
			} else if cur > 0 {
				next := make([]rune, 0, len(r)-1)
				next = append(next, r[:cur-1]...)
				s.setSearchDraft(string(next), cur-1)
			}
			return s, s.onSearchDraftChanged()
		case " ":
			s.insertSearchText(" ")
			return s, s.onSearchDraftChanged()
		default:
			if msg.Type == tea.KeySpace {
				s.insertSearchText(" ")
				return s, s.onSearchDraftChanged()
			} else if msg.Type == tea.KeyRunes && !msg.Alt {
				ins := sanitizeSearchRunes(msg.Runes)
				if ins == "" {
					return s, nil
				}
				s.insertSearchText(ins)
				return s, s.onSearchDraftChanged()
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
	case "S":
		s.ShowSettings = !s.ShowSettings
		return s, nil
	case "u":
		return s.OpenQueue()
	case "/":
		s.openSearchFocus(s.View == ViewSearch)
		return s, nil
	case "tab":
		if s.Width >= miniGuideMinW {
			if s.Focus == FocusSidebar {
				s.Focus = FocusMain
			} else {
				s.Focus = FocusSidebar
			}
		}
		return s, nil
	case "esc":
		if s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		if s.View == ViewChannel {
			s.View = s.PrevView
			if s.View == ViewChannel {
				s.View = ViewHome
			}
			s.Focus = FocusMain
			return s, nil
		}
		if s.View == ViewSearch {
			s.View = ViewHome
			s.Focus = FocusMain
			return s, nil
		}
		if s.ShowLyrics {
			s.ShowLyrics = false
		}
		return s, nil
	case "j", "down":
		if s.Focus == FocusSidebar {
			return s.cycleSidebarTab(1)
		}
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() + step)
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.activeIdx())
	case "k", "up":
		if s.Focus == FocusSidebar {
			return s.cycleSidebarTab(-1)
		}
		step := 1
		if s.View == ViewHome {
			step = s.GridCols()
		}
		s.SetSelection(s.activeIdx() - step)
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.activeIdx())
	case "right":
		if s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		s.SetSelection(s.activeIdx() + 1)
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.activeIdx())
	case "h", "left":
		if s.Focus == FocusMain && s.Width >= miniGuideMinW && s.activeIdx() == 0 {
			s.Focus = FocusSidebar
			return s, nil
		}
		s.SetSelection(s.activeIdx() - 1)
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), s.activeIdx())
	case "1":
		return s.SwitchTab(model.TabHome)
	case "2":
		return s.SwitchTab(model.TabMusic)
	case "3":
		return s.SwitchTab(model.TabSubscriptions)
	case "4":
		return s.SwitchTab(model.TabHistory)
	case "5":
		return s.SwitchTab(model.TabDownloads)
	case "[":
		s.CycleChip(-1)
		return s, s.maybeFetchLiveChip()
	case "]":
		s.CycleChip(1)
		return s, s.maybeFetchLiveChip()
	case "enter":
		if s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		return s.PlaySelected("auto")
	case "a":
		return s.PlaySelected("audio")
	case "v":
		return s.PlaySelected("video")
	case "x", "V":
		return s.ToggleVideoMode()
	case "C":
		return s, s.toggleSubtitlesCmd()
	case "P":
		return s, s.togglePiPCmd()
	case "m":
		return s.PlaySelected("mix")
	case "w":
		return s.ToggleSaveSelected()
	case "W":
		return s.ToggleLikeSelected()
	case "s":
		return s.ToggleSubscribeSelected()
	case "g", "G":
		return s.OpenSelectedChannel()
	case "q":
		return s.EnqueueSelected()
	case "d":
		return s.DownloadSelected(s.DownloadAudio)
	case "c":
		return s.CycleQuality()
	case "f", "F":
		return s.OpenQualityModal(), nil
	case "o":
		return s.OpenChapters()
	case "O", "ctrl+o":
		return s.OpenSelectedBrowser()
	case "l", "L":
		if key == "l" && s.Focus == FocusSidebar {
			s.Focus = FocusMain
			return s, nil
		}
		return s.ToggleLyrics()
	case "z":
		if s.ShowLyrics {
			return s.AdjustLyricsOffset(-0.5)
		}
	case "Z":
		if s.ShowLyrics {
			return s.AdjustLyricsOffset(0.5)
		}
	case "y":
		if s.ShowLyrics {
			return s.CycleLyricsSource()
		}
		return s.CopySelectedURL()
	case "Y":
		if s.ShowLyrics {
			return s.ResetLyricsOffset()
		}
	case "b", "B":
		return s.ToggleSponsorBlock()
	case "t", "T":
		return s.CycleTheme()
	case "A":
		return s.ToggleAutoplay()
	case " ":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.TogglePause(ctx) }, "")
	case "n":
		return s.NextTrack()
	case "p":
		return s.PrevTrack()
	case "+", "=":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.AddVolume(ctx, 5) }, i18n.Format(s.Catalog().App.ToastVolumeUp, map[string]string{"delta": "5%"}))
	case "-":
		return s, s.engineCmd(func(ctx context.Context) error { return s.Deps.Engine.AddVolume(ctx, -5) }, i18n.Format(s.Catalog().App.ToastVolumeDown, map[string]string{"delta": "5%"}))
	case ".", ">":
		return s.SeekRelative(10)
	case ",", "<":
		return s.SeekRelative(-10)
	case "{":
		return s.AdjustSpeed(-0.25)
	case "}":
		return s.AdjustSpeed(0.25)
	case "0":
		return s.SetPlaybackSpeed(1.0)
	case "R":
		return s.ToggleLoop()
	case "r":
		if s.View == ViewSearch && strings.TrimSpace(s.SearchQuery) != "" {
			return s.RefreshCurrentSearch()
		}
		s.Syncing = true
		return s, tea.Batch(s.loadFeedCmd(s.ActiveTab, true), s.ensureAnimTick())
	}

	return s, nil
}

func (s *State) insertSearchText(ins string) {
	cur := s.SearchCursorPos()
	r := []rune(s.SearchDraft)
	insRunes := []rune(ins)
	next := make([]rune, 0, len(r)+len(insRunes))
	next = append(next, r[:cur]...)
	next = append(next, insRunes...)
	next = append(next, r[cur:]...)
	s.setSearchDraft(string(next), cur+len(insRunes))
}

func (s *State) openSearchFocus(prefill bool) {
	defer s.syncMouseSnapshot()
	s.Focus = FocusSearch
	s.SearchSuggestions = nil
	s.SuggestIdx = -1
	if s.suggestCancel != nil {
		s.suggestCancel()
	}
	if prefill && strings.TrimSpace(s.SearchQuery) != "" {
		s.setSearchDraft(s.SearchQuery, len([]rune(s.SearchQuery)))
	} else {
		s.setSearchDraft("", 0)
	}
}

func (s *State) onSearchDraftChanged() tea.Cmd {
	defer s.syncMouseSnapshot()
	s.SearchSuggestions = nil
	s.SuggestIdx = -1
	if s.suggestCancel != nil {
		s.suggestCancel()
	}
	if strings.TrimSpace(s.SearchDraft) == "" {
		s.suggestSeq++
		return nil
	}
	if s.Deps.SuggestProvider == nil {
		return nil
	}
	s.suggestSeq++
	seq := s.suggestSeq
	draft := s.SearchDraft
	return tea.Tick(suggestDebounce, func(time.Time) tea.Msg {
		return SuggestTickMsg{Seq: seq, Draft: draft}
	})
}

func (s *State) fetchSuggestionsCmd(seq uint64, query string) tea.Cmd {
	if s.Deps.SuggestProvider == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.appCtx, 2*time.Second)
	s.suggestCancel = cancel
	prov := s.Deps.SuggestProvider
	return func() tea.Msg {
		items, err := prov.Suggest(ctx, query)
		return SuggestionsLoadedMsg{Seq: seq, Query: query, Items: items, Err: err}
	}
}

func deleteLastWord(s string) string {
	r := []rune(s)
	for len(r) > 0 && unicode.IsSpace(r[len(r)-1]) {
		r = r[:len(r)-1]
	}
	for len(r) > 0 && !unicode.IsSpace(r[len(r)-1]) {
		r = r[:len(r)-1]
	}
	return string(r)
}

func sanitizeSearchRunes(runes []rune) string {
	if len(runes) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(runes))
	for _, r := range runes {
		switch r {
		case '\r', '\n', '\t':
			b.WriteByte(' ')
		default:
			if r < 32 || r == 127 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isMixTarget(id, mode string) bool {
	return mode == "mix" ||
		strings.HasPrefix(id, "RD") ||
		strings.HasPrefix(id, "PL") ||
		strings.HasPrefix(id, "OLAK5uy_")
}

// SubmitSearch cancels any in-flight search and starts a new one.
func (s *State) SubmitSearch() (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	q := strings.TrimSpace(s.SearchDraft)
	s.SearchSuggestions = nil
	s.SuggestIdx = -1
	if s.suggestCancel != nil {
		s.suggestCancel()
	}
	if q == "" {
		s.Focus = FocusMain
		s.setSearchDraft("", 0)
		return s, nil
	}
	if s.Deps.SearchHistory != nil {
		_ = s.Deps.SearchHistory.Record(q)
	}
	if s.searchCancel != nil {
		s.searchCancel()
	}
	s.searchSeq++
	seq := s.searchSeq

	s.SearchQuery = q
	s.setSearchDraft("", 0)
	s.SearchResults = nil
	s.View = ViewSearch
	s.Focus = FocusMain
	s.SearchPending = true
	s.ResultIdx = 0
	s.ResultScroll = 0

	ctx, cancel := context.WithTimeout(s.appCtx, searchTimeout)
	s.searchCancel = cancel
	catPort := s.Deps.Catalog
	errNoCatalog := s.Catalog().App.ErrCatalogUnconfigured

	searchCmd := func() tea.Msg {
		defer cancel()
		if catPort == nil {
			return SearchDoneMsg{Query: q, Seq: seq, Err: errors.New(errNoCatalog)}
		}
		videos, err := catPort.Search(ctx, q)
		return SearchDoneMsg{Query: q, Seq: seq, Videos: videos, Err: err}
	}
	return s, tea.Batch(searchCmd, s.ensureAnimTick())
}

type searchRefresher interface {
	RefreshSearch(ctx context.Context, query string) ([]model.Video, error)
}

// RefreshCurrentSearch bypasses catalog search cache and re-runs the active search query.
func (s *State) RefreshCurrentSearch() (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	q := strings.TrimSpace(s.SearchQuery)
	if q == "" {
		s.Syncing = true
		return s, tea.Batch(s.loadFeedCmd(s.ActiveTab, true), s.ensureAnimTick())
	}
	if s.searchCancel != nil {
		s.searchCancel()
	}
	s.searchSeq++
	seq := s.searchSeq

	s.SearchResults = nil
	s.View = ViewSearch
	s.Focus = FocusMain
	s.SearchPending = true
	s.ResultIdx = 0
	s.ResultScroll = 0

	ctx, cancel := context.WithTimeout(s.appCtx, searchTimeout)
	s.searchCancel = cancel
	catPort := s.Deps.Catalog
	errNoCatalog := s.Catalog().App.ErrCatalogUnconfigured

	searchCmd := func() tea.Msg {
		defer cancel()
		if catPort == nil {
			return SearchDoneMsg{Query: q, Seq: seq, Err: errors.New(errNoCatalog)}
		}
		var (
			videos []model.Video
			err    error
		)
		if sr, ok := catPort.(searchRefresher); ok {
			videos, err = sr.RefreshSearch(ctx, q)
		} else {
			videos, err = catPort.Search(ctx, q)
		}
		return SearchDoneMsg{Query: q, Seq: seq, Videos: videos, Err: err}
	}
	return s, tea.Batch(searchCmd, s.ensureAnimTick())
}

func (s *State) prefetchTop(videos []model.Video, n int) {
	p, ok := s.Deps.Engine.(streamPrefetcher)
	if !ok {
		return
	}
	for i := 0; i < len(videos) && i < n; i++ {
		p.Prefetch(videos[i])
	}
}

func (s *State) activeIdx() int {
	return s.ActiveListIndex()
}

// SwitchTab changes the active sidebar tab and loads its feed if needed.
func (s *State) SwitchTab(tab model.Tab) (*State, tea.Cmd) {
	defer s.syncMouseSnapshot()
	s.View = ViewHome
	s.PrevView = ViewHome
	s.Focus = FocusMain
	s.ShowChapters = false
	s.ShowQualityModal = false
	s.ActiveTab = tab
	s.SelectedIdx = 0
	s.ScrollOffset = 0
	s.HomeChip = 0
	if tab == model.TabPlaylists {
		var pls []model.Video
		for _, v := range s.Feeds[model.TabHome] {
			if strings.HasPrefix(v.ID, "RD") || strings.Contains(strings.ToLower(v.Title), "mix") || strings.Contains(strings.ToLower(v.Title), "playlist") {
				pls = append(pls, v)
			}
		}
		s.Feeds[model.TabPlaylists] = pls
		return s, s.enrichVisibleMetadataCmd(pls, 0)
	}
	if tab == model.TabWatchLater || tab == model.TabLiked {
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), 0)
	}
	if _, ok := s.Feeds[tab]; ok {
		return s, s.enrichVisibleMetadataCmd(s.VisibleItems(), 0)
	}
	s.Syncing = true
	return s, tea.Batch(s.loadFeedCmd(tab, false), s.ensureAnimTick())
}

func (s *State) cycleSidebarTab(delta int) (*State, tea.Cmd) {
	tabs := []model.Tab{
		model.TabHome,
		model.TabMusic,
		model.TabSubscriptions,
		model.TabHistory,
		model.TabWatchLater,
		model.TabPlaylists,
		model.TabLiked,
		model.TabDownloads,
	}
	cur := 0
	for i, t := range tabs {
		if t == s.ActiveTab {
			cur = i
			break
		}
	}
	next := (cur + delta + len(tabs)) % len(tabs)
	return s.SwitchTab(tabs[next])
}

// CycleChip steps through the Home view's filter chips.
func (s *State) CycleChip(delta int) *State {
	defer s.syncMouseSnapshot()
	if s.View != ViewHome {
		return s
	}
	n := len(HomeChips)
	s.HomeChip = (s.HomeChip + delta + n) % n
	s.SelectedIdx = 0
	s.ScrollOffset = 0
	return s
}

func prependUniqueVideo(list []model.Video, v model.Video) []model.Video {
	out := make([]model.Video, 0, len(list)+1)
	out = append(out, v)
	for _, item := range list {
		if item.ID != v.ID {
			out = append(out, item)
		}
	}
	return out
}

func removeVideoByID(list []model.Video, id string) []model.Video {
	out := make([]model.Video, 0, len(list))
	for _, item := range list {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}
