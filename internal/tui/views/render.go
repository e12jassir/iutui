// Package views renders the Hybrid YouTube Web + Minimalist TUI screens
// from application state, theme styles, and thumbnail renderers.
package views

import (
	"fmt"
	"strings"
	"time"

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
)

func renderSpotatuiPanel(title string, content []string, width, height int, focused bool) string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}
	borderStyle := lipgloss.NewStyle().Foreground(spotDimBorder)
	titleStyle := lipgloss.NewStyle().Foreground(spotDimTitle)
	if focused {
		borderStyle = lipgloss.NewStyle().Foreground(spotPurple).Bold(true)
		titleStyle = lipgloss.NewStyle().Foreground(spotPurple).Bold(true)
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
	if s.Width < minWidth || s.Height < minHeight {
		return renderTooSmall(s.Width, s.Height, th)
	}

	topBar := renderTopPanels(s, th, tr, s.Width)
	playerBar := renderPlayerPanel(s, th, s.Width)

	middleH := s.Height - 8 // top(3) + player(5)
	if middleH < 4 {
		middleH = 4
	}

	var middle string
	switch {
	case s.Width >= fullSidebarMinW:
		side := renderSidebarFull(s, th, fullSidebarW, middleH)
		mainW := s.Width - fullSidebarW
		main := renderMain(s, th, tr, mainW, middleH)
		middle = lipgloss.JoinHorizontal(lipgloss.Top, side, main)
	case s.Width >= miniGuideMinW:
		side := renderMiniGuide(s, th, miniGuideW, middleH)
		mainW := s.Width - miniGuideW
		main := renderMain(s, th, tr, mainW, middleH)
		middle = lipgloss.JoinHorizontal(lipgloss.Top, side, main)
	default:
		middle = renderMain(s, th, tr, s.Width, middleH)
	}

	frame := lipgloss.JoinVertical(lipgloss.Left, topBar, middle, playerBar)
	if s.ShowSettings {
		frame = renderSettingsOverlay(s, s.Width, s.Height, th)
	} else if s.ShowHelp {
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

func renderTopPanels(s *app.State, th theme.Theme, tr thumbs.Renderer, width int) string {
	sideW := sidebarWidthFor(width)
	remW := width - sideW
	actionsW := 15
	searchPanelW := max(24, remW-actionsW)
	innerW := searchPanelW - 2

	var searchText string
	if s.Focus == app.FocusSearch {
		searchText = s.SearchDraft + "█"
	} else if s.SearchQuery != "" {
		searchText = s.SearchQuery
	} else {
		searchText = "Search"
	}

	syncColor := theme.Muted
	if s.Syncing || s.SearchPending {
		syncColor = theme.Blue
	}
	syncBtn := lipgloss.NewStyle().Foreground(syncColor).Render("[r]")
	textAvail := max(4, innerW-lipgloss.Width(syncBtn)-2)
	textStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	if s.Focus == app.FocusSearch || s.SearchQuery != "" {
		textStyle = lipgloss.NewStyle().Foreground(theme.White)
	}
	leftTxt := textStyle.Render(" " + truncatePlain(searchText, textAvail))
	gap := max(1, innerW-lipgloss.Width(leftTxt)-lipgloss.Width(syncBtn)-1)
	searchRow := leftTxt + strings.Repeat(" ", gap) + syncBtn + " "
	searchPanel := renderSpotatuiPanel("", []string{searchRow}, searchPanelW, 3, s.Focus == app.FocusSearch)

	actStyle := lipgloss.NewStyle().Foreground(theme.Text)
	actionsRow := " " + actStyle.Render("[?]") + " " + actStyle.Render("[S]") + " " + actStyle.Render("[Q]") + " "
	actionsPanel := renderSpotatuiPanel("", []string{actionsRow}, actionsW, 3, s.ShowHelp || s.ShowSettings)

	right := lipgloss.JoinHorizontal(lipgloss.Top, searchPanel, actionsPanel)
	if sideW == 0 {
		return right
	}

	var brand string
	red := lipgloss.NewStyle().Foreground(theme.Red).Bold(true)
	white := lipgloss.NewStyle().Foreground(theme.White).Bold(true)
	if sideW >= fullSidebarW {
		if br, ok := tr.(thumbs.BrandRenderer); ok {
			brand = br.RenderBrand(sideW, 3)
		}
		if brand == "" {
			brand = red.Render("▶") + white.Render("  ytx")
		}
	} else {
		brand = red.Render("▶") + white.Render(" ytx")
	}
	brandBox := lipgloss.NewStyle().
		Width(sideW).
		Height(3).
		Align(lipgloss.Center, lipgloss.Center).
		Render(brand)

	return lipgloss.JoinHorizontal(lipgloss.Top, brandBox, right)
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
	innerW := max(4, width-2)
	sectionStyle := lipgloss.NewStyle().Foreground(theme.Dim).Bold(true)
	divider := lipgloss.NewStyle().Foreground(spotDimBorder).Render(strings.Repeat("─", max(1, innerW-2)))

	var lines []string
	lines = append(lines, sectionStyle.Render(" PRINCIPAL"))
	for _, t := range []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, th, innerW))
	}

	lines = append(lines, " "+divider)
	lines = append(lines, sectionStyle.Render(" BIBLIOTECA"))
	for _, t := range []model.Tab{model.TabHistory, model.TabWatchLater, model.TabPlaylists, model.TabLiked, model.TabDownloads} {
		lines = append(lines, renderSidebarTabLine(t, t == s.ActiveTab, s.Focus == app.FocusSidebar, th, innerW))
	}

	lines = append(lines, " "+divider)
	lines = append(lines, sectionStyle.Render(" SUSCRIPCIONES"))
	if len(s.Subscriptions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Dim).Render("  (sin canales)"))
	} else {
		maxSubs := min(len(s.Subscriptions), 4)
		for i := 0; i < maxSubs; i++ {
			if len(lines) >= height-3 {
				break
			}
			sub := s.Subscriptions[i]
			av := channelAvatar(sub)
			name := truncatePlain(sub, max(1, innerW-5))
			lines = append(lines, " "+av+" "+lipgloss.NewStyle().Foreground(theme.Text).Render(name))
		}
		if len(s.Subscriptions) > maxSubs && len(lines) < height-2 {
			lines = append(lines, lipgloss.NewStyle().Foreground(theme.Dim).Render(fmt.Sprintf("  +%d canales más", len(s.Subscriptions)-maxSubs)))
		}
	}

	return renderSpotatuiPanel("YouTube", lines, width, height, s.Focus == app.FocusSidebar)
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

func renderSidebarTabLine(t model.Tab, active, focused bool, th theme.Theme, width int) string {
	prefix := "  "
	if active {
		prefix = "▶ "
	}
	label := truncatePlain(prefix+monoTabIcon(t)+"  "+t.String(), max(1, width-2))
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
	innerW := max(4, width-2)
	tabs := []model.Tab{model.TabHome, model.TabMusic, model.TabSubscriptions, model.TabHistory, model.TabDownloads}
	lines := make([]string, 0, len(tabs)*3)
	for _, t := range tabs {
		icon := monoTabIcon(t)
		lbl := shortTabLabel(t)
		icStyle := lipgloss.NewStyle().Foreground(theme.Muted).Width(innerW).Align(lipgloss.Center)
		lbStyle := lipgloss.NewStyle().Foreground(theme.Dim).Width(innerW).Align(lipgloss.Center)
		if t == s.ActiveTab && s.View == app.ViewHome {
			icStyle = icStyle.Foreground(theme.White).Background(theme.Surf).Bold(true)
			lbStyle = lbStyle.Foreground(theme.White).Background(theme.Surf).Bold(true)
		}
		lines = append(lines, icStyle.Render(icon), lbStyle.Render(lbl), "")
	}
	return renderSpotatuiPanel("Menu", lines, width, height, s.Focus == app.FocusSidebar)
}

func renderMain(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int) string {
	innerW := max(10, width-2)
	innerH := max(2, height-2)

	items := s.VisibleItems()
	var title string
	if s.View == app.ViewSearch {
		pos := 0
		if len(items) > 0 {
			pos = s.ResultIdx + 1
		}
		title = fmt.Sprintf("Búsqueda: %s (%d/%d)", s.SearchQuery, pos, len(items))
	} else {
		pos := 0
		if len(items) > 0 {
			pos = s.SelectedIdx + 1
		}
		title = fmt.Sprintf("%s (%d/%d)", s.ActiveTab.String(), pos, len(items))
	}
	if s.Syncing || s.SearchPending {
		title += " · ⟳"
	}

	chips := renderChips(s, th, innerW)
	bodyH := max(1, innerH-1)
	var body string
	if s.View == app.ViewSearch {
		body = renderSearchList(s, th, tr, innerW, bodyH)
	} else {
		body = renderHomeGrid(s, th, tr, innerW, bodyH)
	}
	content := append([]string{chips}, strings.Split(body, "\n")...)
	return renderSpotatuiPanel(title, content, width, height, s.Focus == app.FocusMain)
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
	actualCardW := (width - (cols-1)*cardGap) / cols
	if actualCardW < cardWidth {
		actualCardW = min(cardWidth, width)
	}

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
	thumbH := max(homeThumbH, min(11, innerW*9/32))

	// 1. Thumbnail block (isolated on its own rows, never overlapped).
	thumbStr := tr.Render(v.ID, innerW, thumbH)

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

	// 3. Symmetric 2-line title + clean channel + views (no avatar box).
	titleLines := wrapTwoLines(v.Title, innerW)
	titleStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)
	if selected {
		titleStyle = titleStyle.Foreground(theme.White)
	}
	line1 := titleLines[0]
	if line1 == "" {
		line1 = " "
	}
	line2 := titleLines[1]
	if line2 == "" {
		line2 = " "
	}
	t1 := titleStyle.Render(line1)
	t2 := titleStyle.Render(line2)

	// 4. Channel & views.
	chName := strings.TrimSpace(v.Channel)
	if chName == "" {
		chName = "-"
	}
	ch := lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(chName, innerW))
	vwText := strings.TrimSpace(formatViewsLine(v))
	if vwText == "" {
		vwText = " "
	}
	vw := lipgloss.NewStyle().Foreground(theme.Dim).Render(truncatePlain(vwText, innerW))

	body := strings.Join([]string{thumbStr, metaRow, t1, t2, ch, vw}, "\n")
	style := th.Card
	if selected {
		style = th.CardSelected
	}
	return style.Width(max(1, cardW-2)).Height(thumbH + 5).Render(body)
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
	tw := min(searchThumbW, max(12, innerW/3))
	thumbStr := tr.Render(v.ID, tw, searchThumbH)

	infoW := max(8, innerW-tw-2)
	titleStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)
	if selected {
		titleStyle = titleStyle.Foreground(theme.White)
	}
	titleLines := wrapTwoLines(v.Title, infoW)
	t1 := titleStyle.Render(titleLines[0])
	t2 := titleStyle.Render(titleLines[1])
	views := lipgloss.NewStyle().Foreground(theme.Dim).Render(truncatePlain(formatViewsLine(v), infoW))
	chName := strings.TrimSpace(v.Channel)
	if chName == "" {
		chName = "-"
	}
	chLine := lipgloss.NewStyle().Foreground(theme.Muted).Render(truncatePlain(chName, infoW))

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

	rightCol := strings.Join([]string{t1, t2, views, chLine, badgeLine}, "\n")
	joined := lipgloss.JoinHorizontal(lipgloss.Top, thumbStr, "  ", rightCol)

	style := th.Card
	if selected {
		style = th.CardSelected
	}
	return style.Width(max(1, rowW-2)).Render(joined)
}

func renderPlayerPanel(s *app.State, th theme.Theme, width int) string {
	innerW := max(10, width-2)

	stateLabel := "Detenido"
	icon := "⏹"
	if s.PlayerStatus.IsRunning {
		if s.PlayerStatus.Paused {
			stateLabel = "En pausa"
			icon = "⏸"
		} else {
			stateLabel = "Reproduciendo"
			icon = "▶"
		}
	}
	volStr := fmt.Sprintf("%.0f%%", s.PlayerStatus.Volume)
	if s.PlayerStatus.Volume <= 0 {
		volStr = "75%"
	}
	panelTitle := fmt.Sprintf("Reproductor (mpv IPC · %s %s · Vol: %s)", icon, stateLabel, volStr)

	// Row 1: selected/active track on the left, toast or queue count on the right.
	rightText := s.Toast
	if rightText == "" {
		queueCount := len(s.Queue)
		if s.PlayerStatus.PlaylistCount > queueCount {
			queueCount = s.PlayerStatus.PlaylistCount
		}
		rightText = fmt.Sprintf("%s · Cola: %d", s.QualityLabel(), queueCount)
	}
	rightStyled := lipgloss.NewStyle().Foreground(spotPurple).Bold(s.Toast != "").Render(" " + truncatePlain(rightText, max(10, innerW/3)) + " ")

	trackLabel := "Sin reproducción activa"
	if s.PlayerStatus.Title != "" {
		trackLabel = icon + " " + s.PlayerStatus.Title
		if ch := findChannelForTitle(s, s.PlayerStatus.Title); ch != "" {
			trackLabel += " · " + ch
		}
	} else if v, ok := s.SelectedVideo(); ok {
		trackLabel = "Seleccionado: " + v.Title
		if v.Channel != "" {
			trackLabel += " (" + v.Channel + ")"
		}
	}
	availLeft := max(8, innerW-lipgloss.Width(rightStyled)-1)
	leftStyled := lipgloss.NewStyle().Foreground(theme.Text).Bold(s.PlayerStatus.IsRunning).Render(" " + truncatePlain(trackLabel, availLeft))
	spacer1 := max(0, innerW-lipgloss.Width(leftStyled)-lipgloss.Width(rightStyled))
	row1 := leftStyled + strings.Repeat(" ", spacer1) + rightStyled

	// Row 2: clock + progress bar + duration.
	posStr := formatClock(s.PlayerStatus.TimePos)
	durStr := formatClock(s.PlayerStatus.Duration)
	barW := max(8, innerW-len(posStr)-len(durStr)-6)
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
	filled := int(ratio * float64(barW))
	if filled > barW {
		filled = barW
	}
	rem := barW - filled
	progBar := lipgloss.NewStyle().Foreground(spotPurple).Render(strings.Repeat("━", filled)) +
		lipgloss.NewStyle().Foreground(spotDimBorder).Render(strings.Repeat("─", rem))
	row2 := " " + lipgloss.NewStyle().Foreground(theme.Muted).Render(posStr) + "  " + progBar + "  " + lipgloss.NewStyle().Foreground(theme.Muted).Render(durStr)

	// Row 3: minimalist transport controls + key hints (no heavy emojis).
	btnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true)
	btns := strings.Join([]string{
		btnStyle.Render("[|<]"),
		btnStyle.Render("[▶/⏸]"),
		btnStyle.Render("[>|]"),
		btnStyle.Render("[-10s]"),
		btnStyle.Render("[+10s]"),
		btnStyle.Render("[Vol-]"),
		btnStyle.Render("[Vol+]"),
		btnStyle.Render("[★]"),
	}, " ")
	hints := lipgloss.NewStyle().Foreground(theme.Dim).Render("   Enter: Play · a: Audio · /: Buscar · S: Config")
	row3 := " " + truncateStyledLine(btns+hints, max(10, innerW-2))

	return renderSpotatuiPanel(panelTitle, []string{row1, row2, row3}, width, 5, s.Focus == app.FocusPlayer)
}

func renderSettingsOverlay(s *app.State, width, height int, th theme.Theme) string {
	title := lipgloss.NewStyle().Foreground(spotPurple).Bold(true).Render("Configuración (ytx)")
	qLabel := "Máxima (best)"
	if s.Quality > 0 {
		qLabel = fmt.Sprintf("%dp", s.Quality)
	}
	rows := []string{
		title,
		"",
		fmt.Sprintf("  Calidad máxima [c]:   %s", qLabel),
		"  Config archivo:       ~/.config/yt/config",
		"  Carpeta descargas:    ~/Videos/yt",
		"  Reproductor:          mpv IPC + prefetch HD",
		"",
		"  [c / Enter] Cambiar calidad   [Esc / S] Cerrar",
	}
	boxW := min(60, max(32, width-8))
	trimmed := make([]string, 0, len(rows))
	for _, r := range rows {
		trimmed = append(trimmed, truncateStyledLine(r, boxW-4))
	}
	modal := th.Toast.Width(boxW - 2).Render(strings.Join(trimmed, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderHelpOverlay(_ string, width, height int, th theme.Theme) string {
	title := lipgloss.NewStyle().Foreground(theme.White).Bold(true).Render("Atajos de teclado (ytx)")
	rows := []string{
		title,
		"",
		"  j / k / ↑ / ↓   Mover selección",
		"  Enter / a / v   Reproducir (auto / audio / video)",
		"  Space / n / p   Pausar · Siguiente · Anterior",
		"  , / . · - / +   Retroceder/Avanzar 10s · Volumen ±5%",
		"  w / q / d       Guardar (★) · Cola · Descargar",
		"  c               Cambiar calidad (MAX/1080/720/480)",
		"  [ / ] · Tab     Cambiar chip · Foco sidebar",
		"  1..5 / / / r    Pestañas · Buscar · Recargar",
		"  Esc · ? / Q     Volver/Cerrar · Ayuda · Salir",
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
	views := strings.TrimSpace(v.Views)
	pub := formatRelativeDate(strings.TrimSpace(v.Published))
	switch {
	case views != "" && pub != "":
		return views + " · " + pub
	case views != "":
		return views
	case pub != "":
		return pub
	default:
		return ""
	}
}

func formatRelativeDate(raw string) string {
	if raw == "" {
		return ""
	}
	layouts := []string{time.RFC3339, "2006-01-02", "20060102"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			days := int(time.Since(t).Hours() / 24)
			switch {
			case days <= 0:
				hours := int(time.Since(t).Hours())
				if hours <= 1 {
					return "hoy"
				}
				return fmt.Sprintf("hace %d h", hours)
			case days == 1:
				return "ayer"
			case days < 7:
				return fmt.Sprintf("hace %d d", days)
			case days < 30:
				return fmt.Sprintf("hace %d sem", days/7)
			case days < 365:
				return fmt.Sprintf("hace %d m", days/30)
			default:
				return fmt.Sprintf("hace %d a", days/365)
			}
		}
	}
	return raw
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
