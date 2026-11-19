// Package views renders the Hybrid YouTube Web + Minimalist TUI screens
// from application state, theme styles, and thumbnail renderers.
package views

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
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
	spotPurple    = lipgloss.Color("#BD93F9")
	spotDimBorder = lipgloss.Color("#374151")
	spotDimTitle  = lipgloss.Color("#9CA3AF")
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
		return renderTooSmall(s.Width, s.Height, th, pal)
	}

	topBar := renderTopPanels(s, th, tr, s.Width, pal)

	hasPlayer := s.HasPlayerBar()
	sugH := 0
	var sugBar string
	if s.Focus == app.FocusSearch && strings.TrimSpace(s.SearchDraft) != "" && len(s.SearchSuggestions) > 0 {
		sugCount := min(6, len(s.SearchSuggestions))
		sugH = sugCount + 2
		maxBody := s.Height - 3
		if hasPlayer {
			maxBody -= 5
		}
		if maxBody-sugH < 4 {
			sugH = max(0, maxBody-4)
		}
		if sugH >= 3 {
			sugBar = renderSearchSuggestionsStrip(s, s.Width, sugH, pal)
		} else {
			sugH = 0
		}
	}

	middleH := s.Height - 3 - sugH // top(3) + optional suggestions
	if hasPlayer {
		middleH = s.Height - 8 - sugH // top(3) + player(5) + optional suggestions
	}
	if middleH < 4 {
		middleH = 4
	}

	sideW := sidebarWidthFor(s.Width)
	remW := s.Width - sideW
	lyricsSideW := 0
	if s.ShowLyrics && s.Width >= miniGuideMinW {
		lyricsSideW = min(44, max(28, remW/3))
		if remW-lyricsSideW < 36 {
			lyricsSideW = 0
		}
	}
	mainW := remW - lyricsSideW

	var panes []string
	switch {
	case s.Width >= fullSidebarMinW:
		panes = append(panes, renderSidebarFull(s, th, fullSidebarW, middleH, pal))
	case s.Width >= miniGuideMinW:
		panes = append(panes, renderMiniGuide(s, th, miniGuideW, middleH, pal))
	}
	panes = append(panes, renderMain(s, th, tr, mainW, middleH, pal))
	if lyricsSideW > 0 {
		panes = append(panes, renderLyricsPanel(s, th, lyricsSideW, middleH, pal))
	}
	middle := panes[0]
	if len(panes) > 1 {
		middle = lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	}

	var parts []string
	parts = append(parts, topBar)
	if sugBar != "" {
		parts = append(parts, sugBar)
	}
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
	} else if s.ShowQueue {
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

func renderSearchSuggestionsStrip(s *app.State, width, sugH int, pal renderPalette) string {
	sideW := sidebarWidthFor(width)
	remW := width - sideW
	actionsW := 15
	panelW := max(24, remW-actionsW)
	innerW := max(4, panelW-2)

	maxItems := max(1, min(len(s.SearchSuggestions), sugH-2))
	title := "Sugerencias"

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

	panel := renderSpotatuiPanel(title, rows, panelW, sugH, true, pal)
	if sideW <= 0 {
		return panel
	}
	pad := strings.Repeat(" ", sideW)
	lines := strings.Split(panel, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func renderTooSmall(w, h int, th theme.Theme, pal renderPalette) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	msg1 := "Terminal demasiado pequeña"
	msg2 := fmt.Sprintf("Actual: %dx%d · Mín: %dx%d", w, h, minWidth, minHeight)
	msg3 := "Amplía la ventana"

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

func renderTopPanels(s *app.State, th theme.Theme, tr thumbs.Renderer, width int, pal renderPalette) string {
	sideW := sidebarWidthFor(width)
	remW := width - sideW
	actionsW := 15
	searchPanelW := max(24, remW-actionsW)
	innerW := searchPanelW - 2

	syncColor := pal.muted
	syncLabel := "[r]"
	if s.Syncing || s.SearchPending || s.ChannelPending || s.LyricsLoading {
		syncColor = pal.spotAccent
		syncLabel = "[" + loadingSpinner(s.AnimFrame) + "]"
	}
	syncBtn := lipgloss.NewStyle().Foreground(syncColor).Bold(s.Syncing || s.SearchPending || s.ChannelPending || s.LyricsLoading).Render(syncLabel)
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
			leftTxt = renderSearchInputLine(s.SearchDraft, s.SearchCursorPos(), textAvail, pal)
		}
	} else {
		searchText := "Search"
		textStyle := lipgloss.NewStyle().Foreground(pal.muted)
		if s.SearchQuery != "" {
			searchText = s.SearchQuery
			textStyle = lipgloss.NewStyle().Foreground(pal.white)
		}
		leftTxt = textStyle.Render(" " + truncatePlain(searchText, textAvail))
	}
	gap := max(1, innerW-lipgloss.Width(leftTxt)-lipgloss.Width(syncBtn)-1)
	searchRow := leftTxt + strings.Repeat(" ", gap) + syncBtn + " "
	searchPanel := renderSpotatuiPanel("", []string{searchRow}, searchPanelW, 3, s.Focus == app.FocusSearch, pal)

	actStyle := lipgloss.NewStyle().Foreground(pal.text)
	actionsRow := " " + actStyle.Render("[?]") + " " + actStyle.Render("[S]") + " " + actStyle.Render("[Q]") + " "
	actionsPanel := renderSpotatuiPanel("", []string{actionsRow}, actionsW, 3, s.ShowHelp || s.ShowSettings || s.ShowChapters, pal)

	right := lipgloss.JoinHorizontal(lipgloss.Top, searchPanel, actionsPanel)
	if sideW == 0 {
		return right
	}

	accentBrand := lipgloss.NewStyle().Foreground(pal.red).Bold(true)
	if s.ThemeName == "terminal" {
		accentBrand = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	}
	whiteBrand := lipgloss.NewStyle().Foreground(pal.white).Bold(true)
	dimBrand := lipgloss.NewStyle().Foreground(pal.dim)
	var brandText string
	if sideW >= fullSidebarW {
		brandText = accentBrand.Render("▶") + " " + whiteBrand.Render("iutui")
		if ver := strings.TrimSpace(s.AppVersion); ver != "" {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			brandText += dimBrand.Render(" · " + ver)
		}
	} else {
		brandText = accentBrand.Render("▶") + " " + whiteBrand.Render("iutui")
	}
	brandInnerW := max(4, sideW-2)
	brandRow := lipgloss.PlaceHorizontal(brandInnerW, lipgloss.Center, truncateStyledLine(brandText, brandInnerW))
	brandBox := renderSpotatuiPanel("", []string{brandRow}, sideW, 3, false, pal)

	return lipgloss.JoinHorizontal(lipgloss.Top, brandBox, right)
}

func renderSearchInputLine(draft string, cursor int, avail int, pal renderPalette) string {
	whiteStyle := lipgloss.NewStyle().Foreground(pal.white)
	cursorStyle := lipgloss.NewStyle().Foreground(pal.white).Reverse(true).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(pal.muted)

	if draft == "" {
		if avail >= 10 {
			return " " + whiteStyle.Render("█") + mutedStyle.Render(" "+truncatePlain("Buscar en YouTube...", max(0, avail-3)))
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
		line = " " + th.ChipActive.Render("Canal") + " " + th.Chip.Render(truncatePlain(s.ChannelTitle, max(8, width/3)))
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
	innerW := max(4, width-2)
	sectionStyle := lipgloss.NewStyle().Foreground(pal.dim).Bold(true)
	divider := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(strings.Repeat("─", max(1, innerW-2)))

	var lines []string
	lines = append(lines, sectionStyle.Render(" PRINCIPAL"))
	for _, t := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, th, innerW, pal))
	}

	lines = append(lines, " "+divider)
	lines = append(lines, sectionStyle.Render(" BIBLIOTECA"))
	for _, t := range []model.Tab{model.TabHistory, model.TabWatchLater, model.TabPlaylists, model.TabLiked, model.TabDownloads} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, th, innerW, pal))
	}

	lines = append(lines, " "+divider)
	lines = append(lines, sectionStyle.Render(" SUSCRIPCIONES"))
	if len(s.Subscriptions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(pal.dim).Render("  (sin canales)"))
	} else {
		maxSubs := min(len(s.Subscriptions), 4)
		for i := 0; i < maxSubs; i++ {
			if len(lines) >= height-3 {
				break
			}
			sub := s.Subscriptions[i]
			av := channelAvatar(sub, pal)
			name := truncatePlain(sub, max(1, innerW-5))
			lines = append(lines, " "+av+" "+lipgloss.NewStyle().Foreground(pal.text).Render(name))
		}
		if len(s.Subscriptions) > maxSubs && len(lines) < height-2 {
			lines = append(lines, lipgloss.NewStyle().Foreground(pal.dim).Render(fmt.Sprintf("  +%d canales más", len(s.Subscriptions)-maxSubs)))
		}
	}

	return renderSpotatuiPanel("YouTube", lines, width, height, s.Focus == app.FocusSidebar, pal)
}

func monoTabIcon(t model.Tab) string {
	switch t {
	case model.TabHome:
		return "⌂"
	case model.TabMusic:
		return "♫"
	case model.TabSubscriptions:
		return "▤"
	case model.TabHistory:
		return "↺"
	case model.TabWatchLater:
		return "◷"
	case model.TabPlaylists:
		return "☰"
	case model.TabLiked:
		return "♥"
	case model.TabDownloads:
		return "↓"
	default:
		return "•"
	}
}

func renderSidebarTabLine(t model.Tab, active, focused bool, th theme.Theme, width int, pal renderPalette) string {
	prefix := "  "
	if active {
		prefix = "▶ "
	}
	label := truncatePlain(prefix+monoTabIcon(t)+"  "+t.String(), max(1, width-2))
	style := th.SidebarItem
	if active {
		style = th.SidebarItemActive
		if focused {
			style = style.Foreground(pal.blue)
		}
	}
	return style.Width(width).Render(label)
}

func renderMiniGuide(s *app.State, th theme.Theme, width, height int, pal renderPalette) string {
	innerW := max(4, width-2)
	tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
	lines := make([]string, 0, len(tabs)*3)
	for _, t := range tabs {
		icon := monoTabIcon(t)
		lbl := shortTabLabel(t)
		icStyle := lipgloss.NewStyle().Foreground(pal.muted).Width(innerW).Align(lipgloss.Center)
		lbStyle := lipgloss.NewStyle().Foreground(pal.dim).Width(innerW).Align(lipgloss.Center)
		if t == s.ActiveTab && s.View == app.ViewHome {
			icStyle = icStyle.Foreground(pal.white).Background(pal.surf).Bold(true)
			lbStyle = lbStyle.Foreground(pal.white).Background(pal.surf).Bold(true)
		}
		lines = append(lines, icStyle.Render(icon), lbStyle.Render(lbl), "")
	}
	return renderSpotatuiPanel("Menu", lines, width, height, s.Focus == app.FocusSidebar, pal)
}

func renderMain(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int, pal renderPalette) string {
	innerW := max(10, width-2)
	innerH := max(2, height-2)

	var title string
	switch s.View {
	case app.ViewSearch:
		title = "Búsqueda: " + s.SearchQuery
	case app.ViewChannel:
		title = "Canal: " + s.ChannelTitle
	default:
		title = s.ActiveTab.String()
	}
	if s.Syncing || s.SearchPending || s.ChannelPending {
		title += " · " + loadingSpinner(s.AnimFrame)
	}

	chips := renderChips(s, th, innerW, pal)
	bodyH := max(1, innerH-1)
	var body string
	if s.View == app.ViewSearch || s.View == app.ViewChannel {
		body = renderSearchList(s, th, tr, innerW, bodyH, pal)
	} else {
		body = renderHomeGrid(s, th, tr, innerW, bodyH, pal)
	}
	content := append([]string{chips}, strings.Split(body, "\n")...)
	return renderSpotatuiPanel(title, content, width, height, s.Focus == app.FocusMain, pal)
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
