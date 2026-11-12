// Package views renders the Hybrid YouTube Web + Minimalist TUI screens
// from application state, theme styles, and thumbnail renderers.
package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx/internal/app"
	"github.com/e12jassir/ytx/internal/model"
	"github.com/e12jassir/ytx/internal/tui/theme"
	"github.com/e12jassir/ytx/internal/tui/thumbs"
	"github.com/mattn/go-runewidth"
)

const (
	minWidth        = 80
	minHeight       = 24
	fullSidebarMinW = 140
	miniGuideMinW   = 100
	fullSidebarW    = 22
	miniGuideW      = 8
	cardWidth       = 30
	cardGap         = 2
	homeThumbH      = 7
	searchThumbW    = 26
	searchThumbH    = 6
)

// Render produces the full terminal UI frame for the given application state,
// theme, and thumbnail renderer, strictly bounded to s.Width x s.Height.
func Render(s *app.State, th theme.Theme, tr thumbs.Renderer) string {
	if s == nil {
		return ""
	}
	if tr == nil {
		tr = thumbs.NewTextRenderer()
	}
	if s.Width < minWidth || s.Height < minHeight {
		return renderTooSmall(s.Width, s.Height, th)
	}

	header := renderHeader(s, th, s.Width)
	playerBar := renderPlayerBar(s, th, s.Width)
	statusBar := renderStatusBar(s, th, s.Width)

	middleH := s.Height - 5 // header(2) + player(2) + status(1)
	if middleH < 1 {
		middleH = 1
	}

	var middle string
	switch {
	case s.Width >= fullSidebarMinW && s.View == app.ViewHome:
		side := renderSidebarFull(s, th, fullSidebarW, middleH)
		mainW := s.Width - fullSidebarW - 1
		main := renderMain(s, th, tr, mainW, middleH)
		middle = lipgloss.JoinHorizontal(lipgloss.Top, side, " ", main)
	case s.Width >= miniGuideMinW:
		side := renderMiniGuide(s, th, miniGuideW, middleH)
		mainW := s.Width - miniGuideW - 1
		main := renderMain(s, th, tr, mainW, middleH)
		middle = lipgloss.JoinHorizontal(lipgloss.Top, side, " ", main)
	default:
		// Width < 100: no sidebar (compact tabs/nav rendered in header).
		middle = renderMain(s, th, tr, s.Width, middleH)
	}

	frame := lipgloss.JoinVertical(lipgloss.Left, header, middle, playerBar, statusBar)
	if s.ShowHelp {
		frame = renderHelpOverlay(frame, s.Width, s.Height, th)
	}
	return fitBlock(frame, s.Width, s.Height)
}

func renderTooSmall(w, h int, th theme.Theme) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	msg1 := "Terminal demasiado pequeña"
	msg2 := fmt.Sprintf("Actual: %dx%d · Mín: %dx%d", w, h, minWidth, minHeight)
	msg3 := "Amplía la ventana"

	boxStyle := th.Toast.Align(lipgloss.Center)
	content := strings.Join([]string{
		lipgloss.NewStyle().Bold(true).Foreground(theme.Red).Render(truncatePlain(msg1, max(1, w-4))),
		lipgloss.NewStyle().Foreground(theme.Text).Render(truncatePlain(msg2, max(1, w-4))),
		lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(msg3, max(1, w-4))),
	}, "\n")
	box := boxStyle.Render(content)
	placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
	return fitBlock(placed, w, h)
}

func renderHeader(s *app.State, th theme.Theme, width int) string {
	logo := " " + th.HeaderLogo.Render("▶") + " " + lipgloss.NewStyle().Foreground(theme.White).Bold(true).Render("ytx")

	var navStr string
	if width < miniGuideMinW {
		tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabDownloads, model.TabHistory}
		parts := make([]string, 0, len(tabs))
		for _, t := range tabs {
			label := shortTabLabel(t)
			if t == s.ActiveTab && s.View == app.ViewHome {
				parts = append(parts, lipgloss.NewStyle().Foreground(theme.White).Bold(true).Underline(true).Render(label))
			} else {
				parts = append(parts, lipgloss.NewStyle().Foreground(theme.Muted).Render(label))
			}
		}
		navStr = "  " + strings.Join(parts, " ")
	}

	syncBadge := lipgloss.NewStyle().Background(theme.Surf).Foreground(theme.Text).Padding(0, 1).Render("⟳")
	if s.Syncing || s.SearchPending {
		syncBadge = lipgloss.NewStyle().Background(theme.Surf).Foreground(theme.Blue).Bold(true).Padding(0, 1).Render("⟳")
	}
	helpBtn := lipgloss.NewStyle().Background(theme.Surf).Foreground(theme.Muted).Padding(0, 1).Render("?")

	leftBlock := logo + navStr
	rightBlock := syncBadge + " " + helpBtn + " "

	leftW := lipgloss.Width(leftBlock)
	rightW := lipgloss.Width(rightBlock)
	searchW := min(64, max(18, width-leftW-rightW-6))

	var searchText string
	if s.Focus == app.FocusSearch {
		searchText = "⌕ " + s.SearchDraft + "█"
	} else if s.SearchQuery != "" {
		searchText = "⌕ " + s.SearchQuery
	} else {
		searchText = "⌕ Buscar en YouTube   /"
	}
	searchText = truncatePlain(searchText, max(4, searchW-2))

	searchStyle := th.SearchBox
	if s.Focus == app.FocusSearch {
		searchStyle = th.SearchBoxFocused
	}
	searchBar := searchStyle.Width(searchW).Render(searchText)

	padW := max(1, (width-leftW-lipgloss.Width(searchBar)-rightW)/2)
	rightPad := max(1, width-leftW-padW-lipgloss.Width(searchBar)-rightW)
	topLine := leftBlock + strings.Repeat(" ", padW) + searchBar + strings.Repeat(" ", rightPad) + rightBlock
	ruleLine := lipgloss.NewStyle().Foreground(theme.Rule).Render(strings.Repeat("─", width))
	return fitBlock(topLine+"\n"+ruleLine, width, 2)
}

func renderChips(s *app.State, th theme.Theme, width int) string {
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
	line := " " + strings.Join(parts, " ")
	return fitBlock(line, width, 1)
}

func renderSidebarFull(s *app.State, th theme.Theme, width, height int) string {
	sectionStyle := lipgloss.NewStyle().Foreground(theme.Dim).Bold(true)
	divider := lipgloss.NewStyle().Foreground(theme.Rule).Render(strings.Repeat("─", max(1, width-2)))

	var lines []string
	lines = append(lines, sectionStyle.Render(" PRINCIPAL"))
	for _, t := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, th, width))
	}

	lines = append(lines, " "+divider)
	lines = append(lines, sectionStyle.Render(" TÚ"))
	for _, t := range []model.Tab{model.TabHistory, model.TabDownloads} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, th, width))
	}

	lines = append(lines, " "+divider)
	lines = append(lines, sectionStyle.Render(" SUSCRIPCIONES"))
	if len(s.Subscriptions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Dim).Render("  (sin canales)"))
	} else {
		for _, sub := range s.Subscriptions {
			if len(lines) >= height {
				break
			}
			av := channelAvatar(sub)
			name := truncatePlain(sub, max(1, width-5))
			lines = append(lines, " "+av+" "+lipgloss.NewStyle().Foreground(theme.Text).Render(name))
		}
	}

	return fitBlock(strings.Join(lines, "\n"), width, height)
}

func renderSidebarTabLine(t model.Tab, active, focused bool, th theme.Theme, width int) string {
	prefix := "  "
	if active {
		prefix = "▶ "
	}
	label := truncatePlain(prefix+t.Icon()+" "+t.String(), max(1, width-2))
	style := th.SidebarItem
	if active {
		style = th.SidebarItemActive
		if focused {
			style = style.Foreground(theme.Blue)
		}
	}
	return style.Width(width).Render(label)
}

func renderMiniGuide(s *app.State, th theme.Theme, width, height int) string {
	tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
	lines := make([]string, 0, len(tabs)*2)
	for _, t := range tabs {
		label := truncatePlain(t.Icon()+" "+shortTabLabel(t), max(1, width-2))
		style := th.SidebarItem
		if t == s.ActiveTab && s.View == app.ViewHome {
			style = th.SidebarItemActive
		}
		lines = append(lines, style.Width(width).Render(label))
	}
	return fitBlock(strings.Join(lines, "\n"), width, height)
}

func renderVerticalSep(height int) string {
	sepStyle := lipgloss.NewStyle().Foreground(theme.Rule)
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		lines[i] = sepStyle.Render("│")
	}
	return strings.Join(lines, "\n")
}

func renderMain(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int) string {
	chips := renderChips(s, th, width)
	bodyH := max(1, height-1)
	var body string
	if s.View == app.ViewSearch {
		body = renderSearchList(s, th, tr, width, bodyH)
	} else {
		body = renderHomeGrid(s, th, tr, width, bodyH)
	}
	return fitBlock(chips+"\n"+body, width, height)
}

func renderHomeGrid(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int) string {
	items := s.VisibleItems()
	if len(items) == 0 {
		msg := "No hay videos en esta vista. Presiona '/' para buscar o 'r' para recargar."
		if s.Syncing {
			msg = "⟳ Cargando videos..."
		}
		empty := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(msg, max(1, width-4))))
		return fitBlock(empty, width, height)
	}

	cols := max(1, (width+cardGap)/(cardWidth+cardGap))
	actualCardW := min(cardWidth, width)

	totalRows := (len(items) + cols - 1) / cols
	selRow := 0
	if s.SelectedIdx >= 0 && s.SelectedIdx < len(items) {
		selRow = s.SelectedIdx / cols
	}

	// Render a sample card to know exact row height.
	sampleCard := renderCard(items[0], false, false, false, th, tr, actualCardW)
	cardH := lipgloss.Height(sampleCard)
	if cardH < 1 {
		cardH = 1
	}

	visibleRows := max(1, height/cardH)
	startRow := s.ScrollOffset
	if selRow < startRow {
		startRow = selRow
	}
	if selRow >= startRow+visibleRows {
		startRow = selRow - visibleRows + 1
	}
	if startRow < 0 {
		startRow = 0
	}
	if startRow >= totalRows {
		startRow = max(0, totalRows-1)
	}

	gapStr := strings.Repeat(" ", cardGap)
	var gridRows []string
	renderedH := 0
	for r := startRow; r < totalRows && renderedH < height; r++ {
		var rowCards []string
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx >= len(items) {
				break
			}
			v := items[idx]
			tr.Prefetch(v.ID)
			selected := idx == s.SelectedIdx
			saved := s.SavedIDs[v.ID]
			playing := isVideoPlaying(s, v)
			card := renderCard(v, selected, saved, playing, th, tr, actualCardW)
			if c > 0 {
				rowCards = append(rowCards, gapStr)
			}
			rowCards = append(rowCards, card)
		}
		joinedRow := lipgloss.JoinHorizontal(lipgloss.Top, rowCards...)
		gridRows = append(gridRows, joinedRow)
		renderedH += lipgloss.Height(joinedRow)
	}

	return fitBlock(strings.Join(gridRows, "\n"), width, height)
}

func renderCard(v model.Video, selected, saved, playing bool, th theme.Theme, tr thumbs.Renderer, cardW int) string {
	innerW := max(4, cardW-4) // border(2) + padding(2)

	// 1. Thumbnail block (isolated on its own rows, never overlapped).
	thumbStr := tr.Render(v.ID, innerW, homeThumbH)

	// 2. Meta badges row IMMEDIATELY BELOW the thumbnail.
	var badgeParts []string
	if v.IsLive {
		badgeParts = append(badgeParts, th.BadgeLive.Render("● EN VIVO"))
	} else {
		dur := v.Duration
		if dur == "" {
			dur = "--:--"
		}
		badgeParts = append(badgeParts, th.BadgeDuration.Render(dur))
	}
	if saved {
		badgeParts = append(badgeParts, lipgloss.NewStyle().Foreground(theme.Yellow).Bold(true).Render("★"))
	}
	if playing {
		badgeParts = append(badgeParts, lipgloss.NewStyle().Foreground(theme.Red).Bold(true).Render("▶ sonando"))
	}
	metaRow := truncateStyledLine(strings.Join(badgeParts, " "), innerW)

	// 3. Avatar + 2-line title + channel + views (YouTube Web card layout).
	titleLines := wrapTwoLines(v.Title, max(4, innerW-3))
	titleStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)
	if selected {
		titleStyle = titleStyle.Foreground(theme.White)
	}
	av := channelAvatar(v.Channel)
	t1 := av + " " + titleStyle.Render(titleLines[0])
	t2 := "   " + titleStyle.Render(titleLines[1])

	// 4. Channel & views.
	ch := "   " + lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(v.Channel, max(4, innerW-3)))
	vw := "   " + lipgloss.NewStyle().Foreground(theme.Dim).Render(truncatePlain(formatViewsLine(v), max(4, innerW-3)))

	body := strings.Join([]string{thumbStr, metaRow, t1, t2, ch, vw}, "\n")
	style := th.Card
	if selected {
		style = th.CardSelected
	}
	return style.Width(max(1, cardW-2)).Render(body)
}

var avatarColors = []lipgloss.Color{
	lipgloss.Color("#c0392b"),
	lipgloss.Color("#2980b9"),
	lipgloss.Color("#27ae60"),
	lipgloss.Color("#8e44ad"),
	lipgloss.Color("#d35400"),
	lipgloss.Color("#16a085"),
}

func channelAvatar(name string) string {
	r := []rune(strings.TrimSpace(name))
	ch := "?"
	var sum int
	if len(r) > 0 {
		ch = strings.ToUpper(string(r[0]))
		for _, c := range r {
			sum += int(c)
		}
	}
	bg := avatarColors[sum%len(avatarColors)]
	return lipgloss.NewStyle().Background(bg).Foreground(theme.White).Bold(true).Render(ch + " ")
}

func renderSearchList(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int) string {
	items := s.VisibleItems()
	if len(items) == 0 {
		msg := fmt.Sprintf("Sin resultados para %q.", s.SearchQuery)
		if s.SearchPending {
			msg = fmt.Sprintf("⟳ Buscando %q...", s.SearchQuery)
		} else if s.SearchQuery == "" {
			msg = "Presiona '/' para buscar videos en YouTube."
		}
		empty := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(msg, max(1, width-4))))
		return fitBlock(empty, width, height)
	}

	sampleRow := renderSearchRow(items[0], false, false, false, th, tr, width)
	rowH := lipgloss.Height(sampleRow)
	if rowH < 1 {
		rowH = 1
	}
	visibleCount := max(1, height/rowH)

	startIdx := s.ResultScroll
	if s.ResultIdx < startIdx {
		startIdx = s.ResultIdx
	}
	if s.ResultIdx >= startIdx+visibleCount {
		startIdx = s.ResultIdx - visibleCount + 1
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(items) {
		startIdx = max(0, len(items)-1)
	}

	var rows []string
	usedH := 0
	for i := startIdx; i < len(items) && usedH < height; i++ {
		v := items[i]
		tr.Prefetch(v.ID)
		row := renderSearchRow(v, i == s.ResultIdx, s.SavedIDs[v.ID], isVideoPlaying(s, v), th, tr, width)
		rows = append(rows, row)
		usedH += lipgloss.Height(row)
	}
	return fitBlock(strings.Join(rows, "\n"), width, height)
}

func renderSearchRow(v model.Video, selected, saved, playing bool, th theme.Theme, tr thumbs.Renderer, rowW int) string {
	innerW := max(10, rowW-4)
	tw := min(searchThumbW, max(8, innerW/3))
	thumbStr := tr.Render(v.ID, tw, searchThumbH)

	infoW := max(8, innerW-tw-2)
	titleStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)
	if selected {
		titleStyle = titleStyle.Foreground(theme.White)
	}
	title := titleStyle.Render(truncatePlain(v.Title, infoW))
	channel := lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(v.Channel, infoW))
	views := lipgloss.NewStyle().Foreground(theme.Dim).Render(truncatePlain(formatViewsLine(v), infoW))

	var badges []string
	if v.IsLive {
		badges = append(badges, th.BadgeLive.Render("● EN VIVO"))
	} else {
		dur := v.Duration
		if dur == "" {
			dur = "--:--"
		}
		badges = append(badges, th.BadgeDuration.Render(dur))
	}
	if saved {
		badges = append(badges, lipgloss.NewStyle().Foreground(theme.Yellow).Bold(true).Render("★ Guardado"))
	}
	if playing {
		badges = append(badges, lipgloss.NewStyle().Foreground(theme.Red).Bold(true).Render("▶ sonando"))
	}
	badgeLine := truncateStyledLine(strings.Join(badges, " "), infoW)

	rightCol := strings.Join([]string{title, channel, views, badgeLine}, "\n")
	joined := lipgloss.JoinHorizontal(lipgloss.Top, thumbStr, "  ", rightCol)

	style := th.Card
	if selected {
		style = th.CardSelected
	}
	return style.Width(max(1, rowW-2)).Render(joined)
}

func renderPlayerBar(s *app.State, th theme.Theme, width int) string {
	// Line 1: full-width red/track progress bar.
	ratio := 0.0
	if s.PlayerStatus.IsRunning && s.PlayerStatus.Duration > 0 {
		ratio = s.PlayerStatus.TimePos / s.PlayerStatus.Duration
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	remaining := width - filled
	progLine := lipgloss.NewStyle().Foreground(theme.Red).Render(strings.Repeat("━", filled)) +
		lipgloss.NewStyle().Foreground(theme.Track).Render(strings.Repeat("─", remaining))

	// Line 2: play/pause icon, title · channel, pos / dur, queue count.
	icon := "⏹"
	if s.PlayerStatus.IsRunning {
		if s.PlayerStatus.Paused {
			icon = "⏸"
		} else {
			icon = "▶"
		}
	}

	posStr := formatClock(s.PlayerStatus.TimePos)
	durStr := formatClock(s.PlayerStatus.Duration)
	queueCount := len(s.Queue)
	if s.PlayerStatus.PlaylistCount > queueCount {
		queueCount = s.PlayerStatus.PlaylistCount
	}
	rightMeta := fmt.Sprintf(" %s / %s  ·  Cola: %d ", posStr, durStr, queueCount)
	rightStyled := lipgloss.NewStyle().Foreground(theme.Muted).Render(rightMeta)

	availLeft := max(10, width-lipgloss.Width(rightStyled)-1)
	trackLabel := "Sin reproducción activa"
	if s.PlayerStatus.Title != "" {
		trackLabel = s.PlayerStatus.Title
		if ch := findChannelForTitle(s, s.PlayerStatus.Title); ch != "" {
			trackLabel += " · " + ch
		}
	}
	leftPlain := truncatePlain(fmt.Sprintf(" %s %s", icon, trackLabel), availLeft)
	leftStyled := lipgloss.NewStyle().Foreground(theme.Text).Bold(s.PlayerStatus.IsRunning).Render(leftPlain)

	spacerW := max(0, width-lipgloss.Width(leftStyled)-lipgloss.Width(rightStyled))
	infoLine := leftStyled + strings.Repeat(" ", spacerW) + rightStyled

	return fitBlock(progLine+"\n"+infoLine, width, 2)
}

func renderStatusBar(s *app.State, th theme.Theme, width int) string {
	modeLabel := strings.ToUpper(s.ActiveTab.String())
	if s.Focus == app.FocusSearch {
		modeLabel = "BUSCAR"
	} else if s.View == app.ViewSearch {
		modeLabel = "BÚSQUEDA"
	}
	badge := lipgloss.NewStyle().
		Background(theme.Red).
		Foreground(theme.White).
		Bold(true).
		Padding(0, 1).
		Render(modeLabel)

	rightText := s.Toast
	if rightText == "" {
		items := s.VisibleItems()
		rightText = fmt.Sprintf("%d videos", len(items))
	}
	rightStyled := th.StatusBadge.Render(truncatePlain(rightText, max(10, width/3)))

	availHintW := max(0, width-lipgloss.Width(badge)-lipgloss.Width(rightStyled)-2)
	hints := truncatePlain(" j/k nav · Enter play · a audio · w ★ · q cola · / buscar · ? ayuda", availHintW)
	hintStyled := lipgloss.NewStyle().Foreground(theme.Dim).Render(hints)

	spacerW := max(0, width-lipgloss.Width(badge)-lipgloss.Width(hintStyled)-lipgloss.Width(rightStyled))
	line := badge + hintStyled + strings.Repeat(" ", spacerW) + rightStyled
	return fitBlock(line, width, 1)
}

func renderHelpOverlay(_ string, width, height int, th theme.Theme) string {
	title := lipgloss.NewStyle().Foreground(theme.White).Bold(true).Render("Atajos de teclado (ytx)")
	rows := []string{
		title,
		"",
		"  j / k / ↑ / ↓   Mover selección",
		"  Enter / a / v   Reproducir (auto / audio / video)",
		"  Space / n / p   Pausar · Siguiente · Anterior",
		"  w / q           Guardar (★) · Agregar a cola",
		"  [ / ]           Cambiar filtro (chip)",
		"  /               Buscar en YouTube",
		"  r               Recargar feed actual",
		"  Esc             Volver / Cerrar modal",
		"  ? / Q           Ayuda / Salir",
	}
	boxW := min(62, max(30, width-8))
	trimmed := make([]string, 0, len(rows))
	for _, r := range rows {
		trimmed = append(trimmed, truncateStyledLine(r, boxW-4))
	}
	modal := th.Toast.Width(boxW - 2).Render(strings.Join(trimmed, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
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

func truncateStyledLine(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxW).Render(s)
}

func formatViewsLine(v model.Video) string {
	switch {
	case v.Views != "" && v.Published != "":
		return v.Views + " · " + v.Published
	case v.Views != "":
		return v.Views
	case v.Published != "":
		return v.Published
	default:
		return ""
	}
}

func formatClock(seconds float64) string {
	if seconds <= 0 {
		return "00:00"
	}
	total := int(seconds)
	h := total / 3600
	m := (total % 3600) / 60
	sec := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
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

func isVideoPlaying(s *app.State, v model.Video) bool {
	return s.PlayerStatus.IsRunning && s.PlayerStatus.Title != "" && s.PlayerStatus.Title == v.Title
}

func findChannelForTitle(s *app.State, title string) string {
	for _, list := range s.Feeds {
		for _, v := range list {
			if v.Title == title {
				return v.Channel
			}
		}
	}
	for _, v := range s.SearchResults {
		if v.Title == title {
			return v.Channel
		}
	}
	for _, v := range s.Queue {
		if v.Title == title {
			return v.Channel
		}
	}
	return ""
}
