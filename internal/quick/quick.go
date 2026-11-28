package quick

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/e12jassir/iutui/internal/model"
)

const defaultMaxVisible = 15

var (
	promptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	countStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(true)
	titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	channelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	emptyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
)

// Model implements tea.Model for the native quick video selector.
type Model struct {
	Videos   []model.Video
	Filtered []model.Video
	Query    string
	Selected int
	Chosen   *model.Video
	Canceled bool
	Width    int
	Height   int
	LangPref string
}

// New creates a new quick selector Model initialized with the given videos.
func New(videos []model.Video) Model {
	return NewWithLang(videos, "auto")
}

// NewWithLang creates a new quick selector Model initialized with videos and language preference.
func NewWithLang(videos []model.Video, langPref string) Model {
	all := make([]model.Video, len(videos))
	copy(all, videos)

	filtered := make([]model.Video, len(videos))
	copy(filtered, videos)

	if strings.TrimSpace(langPref) == "" {
		langPref = "auto"
	}

	return Model{
		Videos:   all,
		Filtered: filtered,
		LangPref: langPref,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model, handling keyboard navigation, live filtering,
// selection, and cancellation.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.Canceled = true
			return m, tea.Quit

		case tea.KeyEnter:
			if len(m.Filtered) > 0 && m.Selected >= 0 && m.Selected < len(m.Filtered) {
				chosen := m.Filtered[m.Selected]
				m.Chosen = &chosen
				return m, tea.Quit
			}
			return m, nil

		case tea.KeyUp, tea.KeyCtrlP:
			if m.Selected > 0 {
				m.Selected--
			}
			return m, nil

		case tea.KeyDown, tea.KeyCtrlN:
			if m.Selected < len(m.Filtered)-1 {
				m.Selected++
			}
			return m, nil

		case tea.KeyBackspace, tea.KeyCtrlH:
			if len(m.Query) > 0 {
				runes := []rune(m.Query)
				m.Query = string(runes[:len(runes)-1])
				m.applyFilter()
			}
			return m, nil

		case tea.KeyCtrlU:
			if m.Query != "" {
				m.Query = ""
				m.applyFilter()
			}
			return m, nil

		case tea.KeySpace:
			if !msg.Alt {
				m.Query += " "
				m.applyFilter()
			}
			return m, nil

		case tea.KeyRunes:
			if !msg.Alt && len(msg.Runes) > 0 {
				m.Query += string(msg.Runes)
				m.applyFilter()
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) applyFilter() {
	m.Filtered = filterVideos(m.Videos, m.Query)
	m.Selected = 0
}

func filterVideos(videos []model.Video, query string) []model.Video {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		out := make([]model.Video, len(videos))
		copy(out, videos)
		return out
	}

	var out []model.Video
	for _, v := range videos {
		haystack := strings.ToLower(v.Title + " " + v.Channel)
		matched := true
		for _, tok := range tokens {
			if !strings.Contains(haystack, tok) {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, v)
		}
	}
	return out
}

// View implements tea.Model, rendering a compact selector prompt, match count,
// and highlighted video list.
func (m Model) View() string {
	var b strings.Builder
	pref := m.LangPref
	if strings.TrimSpace(pref) == "" {
		pref = "auto"
	}
	vs := i18n.For(i18n.Resolve(pref, os.Getenv)).Views

	b.WriteString(promptStyle.Render("> "))
	b.WriteString(m.Query)
	b.WriteString("▌\n")

	b.WriteString(countStyle.Render(fmt.Sprintf("  %d/%d", len(m.Filtered), len(m.Videos))))
	b.WriteByte('\n')

	if len(m.Filtered) == 0 {
		b.WriteString(emptyStyle.Render(vs.QuickNoMatches))
		b.WriteByte('\n')
		return b.String()
	}

	maxRows := defaultMaxVisible
	if m.Height > 3 {
		maxRows = m.Height - 2
	}
	if maxRows < 1 {
		maxRows = 1
	}

	start := 0
	if m.Selected >= maxRows {
		start = m.Selected - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.Filtered) {
		end = len(m.Filtered)
	}

	for i := start; i < end; i++ {
		v := m.Filtered[i]
		line := formatRow(v, m.Width, i == m.Selected, vs)
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

func formatRow(v model.Video, width int, selected bool, vs i18n.ViewStrings) string {
	prefix := "  "
	if selected {
		prefix = "> "
	}

	badge := v.Badge()
	if v.IsLive && vs.QuickBadgeLive != "" {
		badge = vs.QuickBadgeLive
	}
	meta := fmt.Sprintf(" — %s [%s]", v.Channel, badge)
	raw := prefix + v.Title + meta
	if width > 4 {
		raw = runewidth.Truncate(raw, width, "…")
	}

	if selected {
		return selectedStyle.Render(raw)
	}

	// Render title and channel/badge with distinct subtle styling when untruncated,
	// or fall back to truncated line when constrained.
	if width > 4 {
		avail := width - runewidth.StringWidth(prefix)
		if avail <= 0 {
			return prefix
		}
		metaW := runewidth.StringWidth(meta)
		if metaW >= avail {
			return prefix + titleStyle.Render(runewidth.Truncate(v.Title+meta, avail, "…"))
		}
		titleText := runewidth.Truncate(v.Title, avail-metaW, "…")
		return prefix + titleStyle.Render(titleText) + channelStyle.Render(meta)
	}

	return prefix + titleStyle.Render(v.Title) + channelStyle.Render(meta)
}

// Run launches the interactive quick selector and returns the chosen video,
// a boolean indicating whether a selection was made (false if canceled), and any error.
func Run(videos []model.Video) (model.Video, bool, error) {
	return RunWithLang(videos, "auto")
}

// RunWithLang launches the interactive quick selector using langPref ("auto", "es", or "en").
func RunWithLang(videos []model.Video, langPref string) (model.Video, bool, error) {
	p := tea.NewProgram(NewWithLang(videos, langPref), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return model.Video{}, false, err
	}

	fm, ok := finalModel.(Model)
	if !ok || fm.Canceled || fm.Chosen == nil {
		return model.Video{}, false, nil
	}

	return *fm.Chosen, true, nil
}
