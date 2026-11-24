package app

import (
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const escapeSequenceWindow = 150 * time.Millisecond

var (
	sgrMouseAnyRe          = regexp.MustCompile(`(?:\[)?<\d+;\d+;\d+[Mm]`)
	sgrBodyCompleteRe      = regexp.MustCompile(`^<\d+;\d+;\d+[Mm]`)
	sgrBodyPartialRe       = regexp.MustCompile(`^<[0-9;]*$`)
	sgrBracketPartialRe    = regexp.MustCompile(`^\[<[0-9;]*$`)
	sgrTailCompleteRe      = regexp.MustCompile(`^[0-9;]*[Mm]`)
	sgrTailPartialRe       = regexp.MustCompile(`^[0-9;]+$`)
	csiReplyAfterBracketRe = regexp.MustCompile(`^(?:[IO]|\?[0-9;]*[A-Za-z]|[0-9]+;[0-9]+R|M[\x20-\xff]{3})$`)
	csiReplyStandaloneRe   = regexp.MustCompile(`^\[(?:\?[0-9;]+[A-Za-z]|[0-9]+;[0-9]+R)$`)
)

// EscapeGuard drops or strips leaked terminal mouse (SGR 1006), CSI/DA/focus, and
// Kitty APC escape fragments that Bubble Tea's fixed-buffer input reader can split
// under rapid motion traffic.
type EscapeGuard struct {
	nowFn      func() time.Time
	armedUntil time.Time
	inSGRUntil time.Time
	inAPCUntil time.Time
}

func (g *EscapeGuard) now() time.Time {
	if g != nil && g.nowFn != nil {
		return g.nowFn()
	}
	return time.Now()
}

func (g *EscapeGuard) resetPending() {
	g.armedUntil = time.Time{}
	g.inSGRUntil = time.Time{}
	g.inAPCUntil = time.Time{}
}

// FilterKey inspects a Bubble Tea KeyMsg, dropping pure leaked escape fragments and
// stripping embedded SGR mouse packets. Returns (filteredMsg, keep).
func (g *EscapeGuard) FilterKey(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if msg.Type != tea.KeyRunes || msg.Paste {
		g.resetPending()
		return msg, true
	}

	raw := string(msg.Runes)
	if raw == "" {
		g.resetPending()
		return msg, true
	}

	now := g.now()

	// Bubble Tea parses split "\x1b" + "[" as Alt+"[", leaving the rest of the
	// CSI/SGR sequence to arrive in subsequent KeyRunes events.
	if msg.Alt && raw == "[" {
		g.armedUntil = now.Add(escapeSequenceWindow)
		g.inSGRUntil = time.Time{}
		g.inAPCUntil = time.Time{}
		return msg, false
	}
	if msg.Alt && raw == "_" {
		g.inAPCUntil = now.Add(escapeSequenceWindow)
		g.armedUntil = time.Time{}
		g.inSGRUntil = time.Time{}
		return msg, false
	}

	if now.Before(g.inAPCUntil) {
		if strings.HasPrefix(raw, "G") || strings.Contains(raw, "OK") {
			g.inAPCUntil = time.Time{}
			return msg, false
		}
		g.inAPCUntil = time.Time{}
	}

	if now.Before(g.inSGRUntil) {
		if loc := sgrTailCompleteRe.FindStringIndex(raw); loc != nil && loc[0] == 0 {
			g.inSGRUntil = time.Time{}
			raw = raw[loc[1]:]
			if raw == "" {
				return msg, false
			}
			msg.Runes = []rune(raw)
		} else if sgrTailPartialRe.MatchString(raw) && len(raw) <= 12 {
			g.inSGRUntil = now.Add(escapeSequenceWindow)
			return msg, false
		} else {
			g.inSGRUntil = time.Time{}
		}
	}

	if now.Before(g.armedUntil) {
		g.armedUntil = time.Time{}
		switch {
		case sgrBodyCompleteRe.MatchString(raw):
			cleaned := sgrMouseAnyRe.ReplaceAllString(raw, "")
			if cleaned == "" {
				return msg, false
			}
			raw = cleaned
			msg.Runes = []rune(raw)
		case sgrBodyPartialRe.MatchString(raw):
			g.inSGRUntil = now.Add(escapeSequenceWindow)
			return msg, false
		case csiReplyAfterBracketRe.MatchString(raw):
			return msg, false
		}
	}

	if len(msg.Runes) >= 2 && sgrBracketPartialRe.MatchString(raw) {
		g.inSGRUntil = now.Add(escapeSequenceWindow)
		return msg, false
	}
	if csiReplyStandaloneRe.MatchString(raw) {
		return msg, false
	}

	if cleaned := sgrMouseAnyRe.ReplaceAllString(raw, ""); cleaned != raw {
		if cleaned == "" {
			return msg, false
		}
		msg.Runes = []rune(cleaned)
	}

	return msg, true
}

// NewBubbleTeaEscapeFilter returns a tea.WithFilter hook that drops leaked SGR
// mouse, CSI/DA, and Kitty APC key fragments before they reach Model.Update.
func NewBubbleTeaEscapeFilter() func(tea.Model, tea.Msg) tea.Msg {
	return NewBubbleTeaProgramFilter(nil)
}

// NewBubbleTeaProgramFilter returns a tea.WithFilter hook that drops leaked escape
// key fragments and filters out redundant MouseActionMotion events that would not
// change hover selection, avoiding unnecessary View/TTY redraws on fast pointer sweeps.
func NewBubbleTeaProgramFilter(s *State) func(tea.Model, tea.Msg) tea.Msg {
	var (
		mu    sync.Mutex
		guard EscapeGuard
	)
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		switch m := msg.(type) {
		case tea.KeyMsg:
			mu.Lock()
			filtered, keep := guard.FilterKey(m)
			mu.Unlock()
			if !keep {
				return nil
			}
			return filtered
		case tea.MouseMsg:
			if m.Action == tea.MouseActionMotion && s != nil && !s.WouldMouseMotionChange(m.X, m.Y) {
				return nil
			}
			return msg
		default:
			return msg
		}
	}
}
