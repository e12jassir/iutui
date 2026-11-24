// Package views renders the Hybrid YouTube Web + Minimalist TUI screens
// from application state, theme styles, and thumbnail renderers.
package views

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/e12jassir/iutui/internal/tui/thumbs"
	"github.com/mattn/go-runewidth"
)

const (
	minWidth        = 80
	minHeight       = 24
	fullSidebarMinW = 140
	miniGuideMinW   = 100
	fullSidebarW    = 26
	miniGuideW      = 10
	cardWidth       = 30
	cardGap         = 2
	homeThumbH      = 7
	searchThumbW    = 34
	searchThumbH    = 7
)

var (
	spotPurple    = lipgloss.Color("#FF4D6D")
	spotDimBorder = lipgloss.Color("#2A2F3D")
	spotDimTitle  = lipgloss.Color("#8B93A7")
	brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

type renderPalette struct {
	custom     bool
	theme      theme.Palette
	spotAccent lipgloss.Color
	spotBorder lipgloss.Color
	spotTitle  lipgloss.Color
	text       lipgloss.Color
	white      lipgloss.Color
	muted      lipgloss.Color
	dim        lipgloss.Color
	red        lipgloss.Color
	blue       lipgloss.Color
	yellow     lipgloss.Color
	surf       lipgloss.Color
	ctrlBtn    lipgloss.Color
}

func resolveRenderPalette(themeName string) renderPalette {
	norm := strings.ToLower(strings.TrimSpace(themeName))
	if os.Getenv("NO_COLOR") == "" && (norm == "" || norm == "youtube-dark") {
		return renderPalette{
			custom:     false,
			spotAccent: spotPurple,
			spotBorder: spotDimBorder,
			spotTitle:  spotDimTitle,
			text:       theme.Text,
			white:      theme.White,
			muted:      theme.Muted,
			dim:        theme.Dim,
			red:        theme.Red,
			blue:       theme.Blue,
			yellow:     theme.Yellow,
			surf:       theme.Surf,
			ctrlBtn:    lipgloss.Color("#38BDF8"),
		}
	}
	tp := theme.ByName(themeName)
	return renderPalette{
		custom:     true,
		theme:      tp,
		spotAccent: tp.Accent,
		spotBorder: tp.Border,
		spotTitle:  tp.Muted,
		text:       tp.Text,
		white:      tp.White,
		muted:      tp.Muted,
		dim:        tp.Dim,
		red:        tp.Red,
		blue:       tp.Blue,
		yellow:     tp.Yellow,
		surf:       tp.Surf,
		ctrlBtn:    tp.Secondary,
	}
}

func loadingSpinner(frame int) string {
	if frame < 0 {
		frame = -frame
	}
	return brailleFrames[frame%len(brailleFrames)]
}

func renderLoadingBox(title, subtitle string, frame, width, height int, pal renderPalette) string {
	if width < 8 || height < 3 {
		return fitBlock("", width, height)
	}
	spinner := loadingSpinner(frame)
	header := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(
		spinner + " " + truncatePlain(title, max(1, width-6)),
	)
	lines := []string{header}
	if strings.TrimSpace(subtitle) != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(subtitle, max(1, width-6))))
	}
	placed := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
	return fitBlock(placed, width, height)
}

func renderSpotatuiPanel(title string, content []string, width, height int, focused bool, pal renderPalette) string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}
	borderStyle := lipgloss.NewStyle().Foreground(pal.spotBorder)
	titleStyle := lipgloss.NewStyle().Foreground(pal.spotTitle)
	if focused {
		borderStyle = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
		titleStyle = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	}

	innerW := width - 2
	var top strings.Builder
	top.WriteString(borderStyle.Render("╭"))
	if title != "" {
		t := truncatePlain(title, max(1, innerW-1))
		tw := runewidth.StringWidth(t)
		top.WriteString(titleStyle.Render(t))
		if rem := innerW - tw; rem > 0 {
			top.WriteString(borderStyle.Render(strings.Repeat("─", rem)))
		}
	} else {
		top.WriteString(borderStyle.Render(strings.Repeat("─", innerW)))
	}
	top.WriteString(borderStyle.Render("╮"))

	bodyH := height - 2
	lines := make([]string, 0, height)
	lines = append(lines, top.String())
	for i := 0; i < bodyH; i++ {
		row := ""
		if i < len(content) {
			row = content[i]
		}
		if lipgloss.Width(row) > innerW {
			row = truncateStyledLine(row, innerW)
		}
		pad := max(0, innerW-lipgloss.Width(row))
		lines = append(lines, borderStyle.Render("│")+row+strings.Repeat(" ", pad)+borderStyle.Render("│"))
	}
	lines = append(lines, borderStyle.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(lines, "\n")
}

// Render produces the full terminal UI frame for the given application state,
// theme, and thumbnail renderer, strictly bounded to s.Width x s.Height.
func Render(s *app.State, th theme.Theme, tr thumbs.Renderer) string {
	if s == nil {
		return ""
	}
	if tr == nil {
		tr = thumbs.NewTextRenderer()
	}
	pal := resolveRenderPalette(s.ThemeName)
	if pal.custom {
		th = theme.ThemeFromPalette(pal.theme)
	}
	if s.Width < minWidth || s.Height < minHeight {
		return renderTooSmall(s.Width, s.Height, th, pal, s.Catalog().Views)
	}

	topBar := renderTopPanels(s, th, tr, s.Width, pal)

	hasPlayer := s.HasPlayerBar()
	middleH := s.Height - 3 // top(3)
	if hasPlayer {
		middleH = s.Height - 8 // top(3) + player(5)
	}
	if middleH < 4 {
		middleH = 4
	}

	sideW := sidebarWidthFor(s.Width)
	remW := s.Width - sideW
	lyricsSideW := s.LyricsSideWidth()
	queueSideW := s.QueueSideWidth()
	mainW := remW - lyricsSideW - queueSideW

	mainPane := renderMain(s, th, tr, mainW, middleH, pal)
	if s.Focus == app.FocusSearch && strings.TrimSpace(s.SearchDraft) != "" && len(s.SearchSuggestions) > 0 {
		sugCount := min(6, len(s.SearchSuggestions))
		sugH := sugCount + 2
		if middleH-sugH < 4 {
			sugH = max(0, middleH-4)
		}
		if sugH >= 3 {
			sugPanel := renderSearchSuggestionsPanel(s, mainW, sugH, pal)
			mainPane = overlayTopLeftBlock(mainPane, sugPanel, mainW, middleH)
		}
	}

	var panes []string
	switch {
	case s.Width >= fullSidebarMinW:
		panes = append(panes, renderSidebarFull(s, th, fullSidebarW, middleH, pal))
	case s.Width >= miniGuideMinW:
		panes = append(panes, renderMiniGuide(s, th, miniGuideW, middleH, pal))
	}
	panes = append(panes, mainPane)
	if lyricsSideW > 0 {
		panes = append(panes, renderLyricsPanel(s, th, lyricsSideW, middleH, pal))
	} else if queueSideW > 0 {
		panes = append(panes, renderQueueSidePanel(s, th, tr, queueSideW, middleH, pal))
	}
	middle := panes[0]
	if len(panes) > 1 {
		middle = lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	}

	var parts []string
	parts = append(parts, topBar)
	parts = append(parts, middle)
	if hasPlayer {
		playerBar := renderPlayerPanel(s, th, s.Width, pal)
		parts = append(parts, playerBar)
	}
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if s.ShowStillWatching {
		frame = renderStillWatchingOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowQualityModal {
		frame = renderQualityOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowQueue && queueSideW == 0 {
		frame = renderQueueOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowChapters {
		frame = renderChaptersOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowSettings {
		frame = renderSettingsOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowHelp {
		frame = renderHelpOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowLyrics && lyricsSideW == 0 {
		frame = renderLyricsOverlay(s, s.Width, s.Height, th, pal)
	}
	return fitBlock(frame, s.Width, s.Height)
}

func renderSearchSuggestionsPanel(s *app.State, panelW, sugH int, pal renderPalette) string {
	innerW := max(4, panelW-2)

	maxItems := max(1, min(len(s.SearchSuggestions), sugH-2))
	title := s.Catalog().Views.SuggestionsTitle

	rows := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		sug := s.SearchSuggestions[i]
		if i == s.SuggestIdx {
			row := "▸ " + sug
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(truncatePlain(row, innerW)))
		} else {
			row := "  " + sug
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.text).Render(truncatePlain(row, innerW)))
		}
	}

	return renderSpotatuiPanel(title, rows, panelW, sugH, true, pal)
}

func overlayTopLeftBlock(base, overlay string, width, height int) string {
	baseLines := strings.Split(padBlock(base, width, height), "\n")
	overLines := strings.Split(overlay, "\n")
	for i := 0; i < len(overLines) && i < len(baseLines); i++ {
		line := overLines[i]
		w := lipgloss.Width(line)
		if w > width {
			baseLines[i] = truncateStyledLine(line, width)
			continue
		}
		rightSuffix := dropLeftColumns(baseLines[i], w)
		composed := line + rightSuffix
		if cw := lipgloss.Width(composed); cw < width {
			composed += strings.Repeat(" ", width-cw)
		} else if cw > width {
			composed = truncateStyledLine(composed, width)
		}
		baseLines[i] = composed
	}
	return strings.Join(baseLines, "\n")
}

// dropLeftColumns skips the first cols display cells of an ANSI-styled line while
// tracking active SGR attributes so the remaining right-hand content keeps its
// exact colors and alignment untouched.
func dropLeftColumns(s string, cols int) string {
	if cols <= 0 {
		return s
	}
	var activeSGR strings.Builder
	skipped := 0
	i := 0
	n := len(s)
	for i < n {
		if s[i] == '\x1b' {
			if i+1 < n && s[i+1] == '[' {
				j := i + 2
				for j < n && !(s[j] >= 0x40 && s[j] <= 0x7E) {
					j++
				}
				if j < n {
					seq := s[i : j+1]
					if s[j] == 'm' {
						if seq == "\x1b[0m" || seq == "\x1b[m" {
							activeSGR.Reset()
						} else {
							activeSGR.WriteString(seq)
						}
					}
					i = j + 1
					continue
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := runewidth.RuneWidth(r)
		if r == 0x10EEEE && rw == 0 {
			rw = 1
		}
		if rw == 0 {
			if skipped < cols {
				i += size
				continue
			}
			// Combining mark immediately after the last skipped cell still belongs to that skipped cell.
			if skipped == cols {
				i += size
				continue
			}
		}
		if skipped >= cols {
			if activeSGR.Len() > 0 {
				return "\x1b[0m" + activeSGR.String() + s[i:]
			}
			return "\x1b[0m" + s[i:]
		}
		skipped += rw
		i += size
	}
	return ""
}

func renderTooSmall(w, h int, th theme.Theme, pal renderPalette, vs i18n.ViewStrings) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	msg1 := vs.TooSmallTitle
	msg2 := i18n.Format(vs.TooSmallCurrent, map[string]string{
		"w":  strconv.Itoa(w),
		"h":  strconv.Itoa(h),
		"mw": strconv.Itoa(minWidth),
		"mh": strconv.Itoa(minHeight),
	})
	msg3 := vs.TooSmallHint

	boxStyle := th.Toast.Align(lipgloss.Center)
	content := strings.Join([]string{
		lipgloss.NewStyle().Bold(true).Foreground(pal.red).Render(truncatePlain(msg1, max(1, w-4))),
		lipgloss.NewStyle().Foreground(pal.text).Render(truncatePlain(msg2, max(1, w-4))),
		lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(msg3, max(1, w-4))),
	}, "\n")
	box := boxStyle.Render(content)
	placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
	return fitBlock(placed, w, h)
}

func sidebarWidthFor(totalW int) int {
	switch {
	case totalW >= fullSidebarMinW:
		return fullSidebarW
	case totalW >= miniGuideMinW:
		return miniGuideW
	default:
		return 0
	}
}

func renderTopPanels(s *app.State, _ theme.Theme, _ thumbs.Renderer, width int, pal renderPalette) string {
	sideW := sidebarWidthFor(width)
	remW := width - sideW
	actionsW := 15
	searchPanelW := max(24, remW-actionsW)
	innerW := searchPanelW - 2

	isLoading := s.Syncing || s.SearchPending || s.ChannelPending || s.LyricsLoading || s.LiveLoading[s.ActiveTab]
	syncColor := pal.muted
	syncIcon := "󰑐"
	if isLoading {
		syncColor = pal.spotAccent
		syncIcon = loadingSpinner(s.AnimFrame)
	}
	syncBtn := lipgloss.NewStyle().Foreground(syncColor).Bold(isLoading).Render(" " + syncIcon + " ")
	textAvail := max(4, innerW-lipgloss.Width(syncBtn)-2)

	var leftTxt string
	if s.Focus == app.FocusSearch {
		if s.SearchDraft == "" && s.SearchQuery != "" {
			whiteStyle := lipgloss.NewStyle().Foreground(pal.white)
			mutedStyle := lipgloss.NewStyle().Foreground(pal.muted)
			if textAvail >= 4 {
				leftTxt = " " + whiteStyle.Render("█") + " " + mutedStyle.Render(truncatePlain(s.SearchQuery, max(0, textAvail-3)))
			} else {
				leftTxt = " " + whiteStyle.Render("█")
			}
		} else {
			leftTxt = renderSearchInputLine(s.SearchDraft, s.SearchCursorPos(), textAvail, pal, s.Catalog().Views)
		}
	} else {
		searchText := "󰍉 " + s.Catalog().Views.SearchPlaceholder
		textStyle := lipgloss.NewStyle().Foreground(pal.muted)
		if s.SearchQuery != "" {
			searchText = "󰍉 " + s.SearchQuery
			textStyle = lipgloss.NewStyle().Foreground(pal.white)
		}
		leftTxt = textStyle.Render(" " + truncatePlain(searchText, max(1, textAvail-1)))
	}
	gap := max(1, innerW-lipgloss.Width(leftTxt)-lipgloss.Width(syncBtn)-1)
	searchRow := leftTxt + strings.Repeat(" ", gap) + syncBtn + " "
	searchPanel := renderSpotatuiPanel("", []string{searchRow}, searchPanelW, 3, s.Focus == app.FocusSearch, pal)

	actStyle := lipgloss.NewStyle().Foreground(pal.muted)
	if s.ShowHelp {
		actStyle = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	}
	setStyle := lipgloss.NewStyle().Foreground(pal.muted)
	if s.ShowSettings {
		setStyle = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	}
	quitStyle := lipgloss.NewStyle().Foreground(pal.muted)
	actionsRow := "  " + actStyle.Render("󰋗") + "   " + setStyle.Render("󰒓") + "   " + quitStyle.Render("󰐥") + "  "
	actionsPanel := renderSpotatuiPanel("", []string{actionsRow}, actionsW, 3, s.ShowHelp || s.ShowSettings || s.ShowChapters, pal)

	right := lipgloss.JoinHorizontal(lipgloss.Top, searchPanel, actionsPanel)
	if sideW <= 0 {
		return fitBlock(right, width, 3)
	}

	accentBrand := lipgloss.NewStyle().Foreground(pal.red).Bold(true)
	if s.ThemeName == "terminal" {
		accentBrand = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	}
	whiteBrand := lipgloss.NewStyle().Foreground(pal.white).Bold(true)
	dimBrand := lipgloss.NewStyle().Foreground(pal.dim)
	brandText := accentBrand.Render("▶") + " " + whiteBrand.Render("iutui")
	if sideW >= fullSidebarW {
		if ver := strings.TrimSpace(s.AppVersion); ver != "" {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			brandText += dimBrand.Render(" · " + ver)
		}
	}
	brandInner := max(2, sideW-2)
	brandCentered := lipgloss.PlaceHorizontal(brandInner, lipgloss.Center, truncateStyledLine(brandText, brandInner))
	brandPanel := renderSpotatuiPanel("", []string{brandCentered}, sideW, 3, false, pal)

	return fitBlock(lipgloss.JoinHorizontal(lipgloss.Top, brandPanel, right), width, 3)
}

func renderSearchInputLine(draft string, cursor int, avail int, pal renderPalette, vs i18n.ViewStrings) string {
	whiteStyle := lipgloss.NewStyle().Foreground(pal.white)
	cursorStyle := lipgloss.NewStyle().Foreground(pal.white).Reverse(true).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(pal.muted)

	if draft == "" {
		if avail >= 10 {
			return " " + whiteStyle.Render("█") + mutedStyle.Render(" "+truncatePlain(vs.SearchPlaceholder, max(0, avail-3)))
		}
		return " " + whiteStyle.Render("█")
	}

	runes := []rune(draft)
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(runes) {
		cursor = len(runes)
	}

	budget := max(1, avail-1)
	widths := make([]int, len(runes))
	totalW := 0
	for i, r := range runes {
		w := runewidth.RuneWidth(r)
		if w <= 0 {
			w = 1
		}
		widths[i] = w
		totalW += w
	}

	if cursor == len(runes) {
		if totalW+1 <= budget {
			return " " + whiteStyle.Render(draft+"█")
		}
		rem := max(0, budget-2)
		used := 0
		start := len(runes)
		for i := len(runes) - 1; i >= 0; i-- {
			if used+widths[i] > rem {
				break
			}
			used += widths[i]
			start = i
		}
		return " " + whiteStyle.Render("…"+string(runes[start:])+"█")
	}

	if totalW <= budget {
		var b strings.Builder
		b.WriteByte(' ')
		if cursor > 0 {
			b.WriteString(whiteStyle.Render(string(runes[:cursor])))
		}
		b.WriteString(cursorStyle.Render(string(runes[cursor])))
		if cursor+1 < len(runes) {
			b.WriteString(whiteStyle.Render(string(runes[cursor+1:])))
		}
		return b.String()
	}

	windowWidth := func(st, en int) int {
		w := 0
		if st > 0 {
			w++
		}
		for i := st; i < en; i++ {
			w += widths[i]
		}
		if en < len(runes) {
			w++
		}
		return w
	}

	start, end := cursor, cursor+1
	preferLeft := true
	for {
		grew := false
		if preferLeft {
			if start > 0 && windowWidth(start-1, end) <= budget {
				start--
				grew = true
			} else if end < len(runes) && windowWidth(start, end+1) <= budget {
				end++
				grew = true
			}
		} else {
			if end < len(runes) && windowWidth(start, end+1) <= budget {
				end++
				grew = true
			} else if start > 0 && windowWidth(start-1, end) <= budget {
				start--
				grew = true
			}
		}
		if !grew {
			break
		}
		preferLeft = !preferLeft
	}

	var prefix strings.Builder
	if start > 0 {
		prefix.WriteRune('…')
	}
	prefix.WriteString(string(runes[start:cursor]))

	var suffix strings.Builder
	suffix.WriteString(string(runes[cursor+1 : end]))
	if end < len(runes) {
		suffix.WriteRune('…')
	}

	var outB strings.Builder
	outB.WriteByte(' ')
	if prefix.Len() > 0 {
		outB.WriteString(whiteStyle.Render(prefix.String()))
	}
	outB.WriteString(cursorStyle.Render(string(runes[cursor])))
	if suffix.Len() > 0 {
		outB.WriteString(whiteStyle.Render(suffix.String()))
	}
	out := outB.String()
	if avail > 0 && lipgloss.Width(out) > avail {
		return truncateStyledLine(out, avail)
	}
	return out
}

func renderChips(s *app.State, th theme.Theme, width int, pal renderPalette) string {
	var line string
	if s.View == app.ViewChannel {
		line = " " + th.ChipActive.Render(s.Catalog().Views.ChipChannel) + " " + th.Chip.Render(truncatePlain(s.ChannelTitle, max(8, width/3)))
	} else {
		chips := app.HomeChips
		active := s.HomeChip
		if s.View == app.ViewSearch {
			chips = app.SearchChips
			active = s.SearchChip
		}

		parts := make([]string, 0, len(chips))
		for i, c := range chips {
			if i == active {
				parts = append(parts, th.ChipActive.Render(c.Label))
			} else {
				parts = append(parts, th.Chip.Render(c.Label))
			}
		}
		line = " " + strings.Join(parts, " ")
	}
	if !s.HasPlayerBar() && s.Toast != "" {
		leftW := lipgloss.Width(line)
		avail := width - leftW - 2
		if avail >= 8 {
			toastTxt := truncatePlain(s.Toast, avail)
			toastStyled := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(toastTxt)
			spacer := max(1, width-leftW-lipgloss.Width(toastStyled)-1)
			line = line + strings.Repeat(" ", spacer) + toastStyled
		}
	}
	return fitBlock(line, width, 1)
}

func renderSidebarFull(s *app.State, th theme.Theme, width, height int, pal renderPalette) string {
	vs := s.Catalog().Views
	innerW := max(4, width-2)
	sectionStyle := lipgloss.NewStyle().Foreground(pal.dim).Bold(true)
	divider := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(" " + strings.Repeat("─", max(1, innerW-2)))

	// Row 0 (y=3) is the rounded panel top border; PRINCIPAL sits at y=4 -> tabs at y=5..7.
	var lines []string
	lines = append(lines, sectionStyle.Render(vs.SidebarMain))
	for _, t := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, "", th, innerW, pal))
	}

	lines = append(lines, divider)
	lines = append(lines, sectionStyle.Render(vs.SidebarLibrary))
	for _, t := range []model.Tab{model.TabHistory, model.TabWatchLater, model.TabPlaylists, model.TabLiked, model.TabDownloads} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, "", th, innerW, pal))
	}

	lines = append(lines, divider)
	lines = append(lines, sectionStyle.Render(vs.SidebarSubscriptions))
	if len(s.Subscriptions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(pal.dim).Render(vs.SidebarNoChannels))
	} else {
		maxSubs := min(len(s.Subscriptions), 4)
		for i := 0; i < maxSubs; i++ {
			if len(lines) >= height-3 {
				break
			}
			sub := s.Subscriptions[i]
			displayName := s.SubscriptionDisplayName(sub)
			name := truncatePlain(displayName, max(1, innerW-6))
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(pal.muted).Render("󰀄")+"  "+lipgloss.NewStyle().Foreground(pal.text).Render(name))
		}
		if len(s.Subscriptions) > maxSubs && len(lines) < height-2 {
			lines = append(lines, lipgloss.NewStyle().Foreground(pal.dim).Render(i18n.Format(vs.SidebarMoreSubs, map[string]string{"n": strconv.Itoa(len(s.Subscriptions) - maxSubs)})))
		}
	}

	return renderSpotatuiPanel("", lines, width, height, s.Focus == app.FocusSidebar, pal)
}

func monoTabIcon(t model.Tab) string {
	switch t {
	case model.TabHome:
		return "󰋜"
	case model.TabMusic:
		return "󰝚"
	case model.TabSubscriptions:
		return "󰗃"
	case model.TabHistory:
		return "󰋚"
	case model.TabWatchLater:
		return "󰥔"
	case model.TabPlaylists:
		return "󰲸"
	case model.TabLiked:
		return "󰋑"
	case model.TabDownloads:
		return "󰇚"
	default:
		return "•"
	}
}

func renderSidebarTabLine(t model.Tab, active, focused bool, badge string, th theme.Theme, width int, pal renderPalette) string {
	if active {
		rail := lipgloss.NewStyle().Foreground(pal.spotAccent).Background(pal.surf).Bold(true).Render("▎")
		lblCol := pal.white
		if focused {
			lblCol = pal.spotAccent
		}
		textSt := lipgloss.NewStyle().Foreground(lblCol).Background(pal.surf).Bold(true)
		badgeSt := lipgloss.NewStyle().Foreground(pal.muted).Background(pal.surf)
		maxLabel := max(1, width-2)
		if badge != "" && width > 14 {
			bw := runewidth.StringWidth(badge) + 1
			maxLabel = max(1, width-2-bw)
		} else {
			badge = ""
		}
		lbl := truncatePlain(" "+monoTabIcon(t)+"  "+t.String(), maxLabel)
		row := rail + textSt.Render(lbl)
		used := 1 + runewidth.StringWidth(lbl)
		if badge != "" {
			pad := max(1, width-used-runewidth.StringWidth(badge)-1)
			row += textSt.Render(strings.Repeat(" ", pad)) + badgeSt.Render(badge+" ")
			return row
		}
		if pad := max(0, width-used); pad > 0 {
			row += textSt.Render(strings.Repeat(" ", pad))
		}
		return row
	}
	if badge != "" && width > 14 {
		bw := runewidth.StringWidth(badge) + 1
		lbl := truncatePlain("  "+monoTabIcon(t)+"  "+t.String(), max(1, width-bw-2))
		pad := max(1, width-runewidth.StringWidth(lbl)-runewidth.StringWidth(badge)-1)
		return lipgloss.NewStyle().Foreground(pal.muted).Render(lbl) +
			strings.Repeat(" ", pad) +
			lipgloss.NewStyle().Foreground(pal.dim).Render(badge+" ")
	}
	lbl := truncatePlain("  "+monoTabIcon(t)+"  "+t.String(), max(1, width-1))
	pad := max(0, width-runewidth.StringWidth(lbl))
	return lipgloss.NewStyle().Foreground(pal.muted).Render(lbl) + strings.Repeat(" ", pad)
}

func renderMiniGuide(s *app.State, _ theme.Theme, width, height int, pal renderPalette) string {
	innerW := max(4, width-2)

	tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
	lines := make([]string, 0, len(tabs)*3)
	// Row 0 (y=3) is the rounded panel top border so slot 0 starts at y=4 (exact mouse.go slot := (y-4)/3).
	for _, t := range tabs {
		icon := monoTabIcon(t)
		lbl := shortTabLabel(t)
		icStyle := lipgloss.NewStyle().Foreground(pal.muted).Width(innerW).Align(lipgloss.Center)
		lbStyle := lipgloss.NewStyle().Foreground(pal.dim).Width(innerW).Align(lipgloss.Center)
		if t == s.ActiveTab && s.View == app.ViewHome {
			icStyle = icStyle.Foreground(pal.spotAccent).Background(pal.surf).Bold(true)
			lbStyle = lbStyle.Foreground(pal.white).Background(pal.surf).Bold(true)
		}
		lines = append(lines, icStyle.Render(icon), lbStyle.Render(lbl), "")
	}
	return renderSpotatuiPanel("", lines, width, height, s.Focus == app.FocusSidebar, pal)
}

func renderMain(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int, pal renderPalette) string {
	innerW := max(10, width-2)
	showChips := s.View != app.ViewSearch
	bodyH := max(1, height-2)
	if !showChips {
		bodyH = max(1, height-1)
	}

	headerRow := ""
	if s.View == app.ViewSearch || s.View == app.ViewChannel {
		vs := s.Catalog().Views
		headerIcon := "󰍉"
		title := i18n.Format(vs.HeaderSearch, map[string]string{"query": s.SearchQuery})
		if s.View == app.ViewChannel {
			headerIcon = "󰗃"
			title = i18n.Format(vs.HeaderChannel, map[string]string{"title": s.ChannelTitle})
		}
		headerRow = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(" "+headerIcon) + " " +
			lipgloss.NewStyle().Foreground(pal.white).Bold(true).Render(truncatePlain(title, max(4, innerW-6)))
	}
	if !showChips && !s.HasPlayerBar() && s.Toast != "" {
		leftW := lipgloss.Width(headerRow)
		avail := innerW - leftW - 2
		if avail >= 8 {
			toastTxt := truncatePlain(s.Toast, avail)
			toastStyled := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(toastTxt)
			spacer := max(1, innerW-leftW-lipgloss.Width(toastStyled)-1)
			headerRow = headerRow + strings.Repeat(" ", spacer) + toastStyled
		}
	}
	if pad := max(0, innerW-lipgloss.Width(headerRow)); pad > 0 {
		headerRow += strings.Repeat(" ", pad)
	}

	var body string
	if s.View == app.ViewSearch || s.View == app.ViewChannel {
		body = renderSearchList(s, th, tr, innerW, bodyH, pal)
	} else {
		body = renderHomeGrid(s, th, tr, innerW, bodyH, pal)
	}

	bodyLines := strings.Split(body, "\n")
	allLines := make([]string, 0, height)
	allLines = append(allLines, " "+truncateStyledLine(headerRow, innerW)+" ")
	if showChips {
		chips := renderChips(s, th, innerW, pal)
		allLines = append(allLines, " "+truncateStyledLine(chips, innerW)+" ")
	}
	for i := 0; i < bodyH; i++ {
		row := ""
		if i < len(bodyLines) {
			row = bodyLines[i]
		}
		if lipgloss.Width(row) > innerW {
			row = truncateStyledLine(row, innerW)
		}
		pad := max(0, innerW-lipgloss.Width(row))
		allLines = append(allLines, " "+row+strings.Repeat(" ", pad)+" ")
	}
	return fitBlock(strings.Join(allLines, "\n"), width, height)
}

func fitBlock(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	clamp := lipgloss.NewStyle().MaxWidth(width)
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = clamp.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func wrapTwoLines(text string, width int) [2]string {
	if width <= 0 {
		return [2]string{"", ""}
	}
	text = strings.TrimSpace(text)
	if runewidth.StringWidth(text) <= width {
		return [2]string{text, ""}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return [2]string{"", ""}
	}

	var line1 strings.Builder
	idx := 0
	for idx < len(words) {
		w := words[idx]
		cand := w
		if line1.Len() > 0 {
			cand = line1.String() + " " + w
		}
		if runewidth.StringWidth(cand) <= width {
			if line1.Len() > 0 {
				line1.WriteByte(' ')
			}
			line1.WriteString(w)
			idx++
		} else {
			if line1.Len() == 0 {
				// Single word longer than width: hard-split by runes.
				line1.WriteString(runewidth.Truncate(w, width, ""))
				rest := strings.TrimPrefix(w, line1.String())
				words[idx] = rest
			}
			break
		}
	}

	rem := strings.Join(words[idx:], " ")
	line2 := truncatePlain(rem, width)
	return [2]string{line1.String(), line2}
}

func truncatePlain(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return "…"
	}
	return runewidth.Truncate(s, maxW, "…")
}

func tailPlain(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return "…"
	}
	runes := []rune(s)
	avail := maxW - 1
	used := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if used+rw > avail {
			break
		}
		used += rw
		start = i
	}
	return "…" + string(runes[start:])
}

func truncateStyledLine(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxW).Render(s)
}

func shortTabLabel(t model.Tab) string {
	switch t {
	case model.TabHome:
		return "Inicio"
	case model.TabMusic:
		return "Música"
	case model.TabSubscriptions:
		return "Subs"
	case model.TabDownloads:
		return "Desc"
	case model.TabHistory:
		return "Hist"
	default:
		return t.String()
	}
}

func padBlock(s string, width, height int) string {
	clamped := fitBlock(s, width, height)
	if height <= 0 {
		return clamped
	}
	lines := strings.Split(clamped, "\n")
	blank := strings.Repeat(" ", max(0, width))
	for len(lines) < height {
		lines = append(lines, blank)
	}
	return strings.Join(lines[:height], "\n")
}
