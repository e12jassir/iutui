// Package views renders the Hybrid YouTube Web + Minimalist TUI screens
// from application state, theme styles, and thumbnail renderers.
package views

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/lyrics"
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
	middleH := s.Height - 3 // top(3)
	if hasPlayer {
		middleH = s.Height - 8 // top(3) + player(5)
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

	var frame string
	if hasPlayer {
		playerBar := renderPlayerPanel(s, th, s.Width, pal)
		frame = lipgloss.JoinVertical(lipgloss.Left, topBar, middle, playerBar)
	} else {
		frame = lipgloss.JoinVertical(lipgloss.Left, topBar, middle)
	}
	if s.ShowStillWatching {
		frame = renderStillWatchingOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowQualityModal {
		frame = renderQualityOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowChapters {
		frame = renderChaptersOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowSettings {
		frame = renderSettingsOverlay(s, s.Width, s.Height, th, pal)
	} else if s.ShowHelp {
		frame = renderHelpOverlay(frame, s.Width, s.Height, th, pal)
	} else if s.ShowLyrics && lyricsSideW == 0 {
		frame = renderLyricsOverlay(s, s.Width, s.Height, th, pal)
	}
	return fitBlock(frame, s.Width, s.Height)
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
		whiteStyle := lipgloss.NewStyle().Foreground(pal.white)
		if s.SearchDraft == "" && s.SearchQuery != "" {
			queryHint := truncatePlain(s.SearchQuery, max(0, textAvail-2))
			leftTxt = whiteStyle.Render(" █ ") + lipgloss.NewStyle().Foreground(pal.muted).Render(queryHint)
		} else {
			leftTxt = whiteStyle.Render(" " + truncatePlain(s.SearchDraft+"█", textAvail))
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
		brandText = accentBrand.Render("▶") + " " + whiteBrand.Render("iutui") + dimBrand.Render(" · tui")
	} else {
		brandText = accentBrand.Render("▶") + " " + whiteBrand.Render("iutui")
	}
	brandInnerW := max(4, sideW-2)
	brandRow := lipgloss.PlaceHorizontal(brandInnerW, lipgloss.Center, truncateStyledLine(brandText, brandInnerW))
	brandBox := renderSpotatuiPanel("", []string{brandRow}, sideW, 3, false, pal)

	return lipgloss.JoinHorizontal(lipgloss.Top, brandBox, right)
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

	items := s.VisibleItems()
	var title string
	switch s.View {
	case app.ViewSearch:
		pos := 0
		if len(items) > 0 {
			pos = s.ResultIdx + 1
		}
		title = fmt.Sprintf("Búsqueda: %s (%d/%d)", s.SearchQuery, pos, len(items))
	case app.ViewChannel:
		pos := 0
		if len(items) > 0 {
			pos = s.ChannelIdx + 1
		}
		title = fmt.Sprintf("Canal: %s (%d/%d)", s.ChannelTitle, pos, len(items))
	default:
		pos := 0
		if len(items) > 0 {
			pos = s.SelectedIdx + 1
		}
		title = fmt.Sprintf("%s (%d/%d)", s.ActiveTab.String(), pos, len(items))
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

func renderHomeGrid(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int, pal renderPalette) string {
	items := s.VisibleItems()
	if len(items) == 0 {
		if s.Syncing {
			return renderLoadingBox("Cargando videos...", s.ActiveTab.String(), s.AnimFrame, width, height, pal)
		}
		if s.HomeChip == 1 && s.LiveLoading[s.ActiveTab] {
			return renderLoadingBox("Buscando directos en vivo...", s.ActiveTab.String(), s.AnimFrame, width, height, pal)
		}
		msg := "No hay videos en esta vista. Presiona '/' para buscar o 'r' para recargar."
		if s.HomeChip == 1 {
			msg = "Sin directos en vivo ahora mismo. Pulsa '/' para buscar o '[' para volver."
		}
		empty := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(msg, max(1, width-4))))
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
	sampleCard := renderCard(items[0], false, false, false, th, tr, actualCardW, pal)
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
			card := renderCard(v, selected, saved, playing, th, tr, actualCardW, pal)
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

func renderCard(v model.Video, selected, saved, playing bool, th theme.Theme, tr thumbs.Renderer, cardW int, pal renderPalette) string {
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
		badgeParts = append(badgeParts, lipgloss.NewStyle().Foreground(pal.yellow).Bold(true).Render("★"))
	}
	if playing {
		badgeParts = append(badgeParts, lipgloss.NewStyle().Foreground(pal.red).Bold(true).Render("▶ sonando"))
	}
	metaRow := truncateStyledLine(strings.Join(badgeParts, " "), innerW)

	// 3. Symmetric 2-line title + clean channel + views (no avatar box).
	titleLines := wrapTwoLines(v.Title, innerW)
	titleStyle := lipgloss.NewStyle().Foreground(pal.text).Bold(true)
	if selected {
		titleStyle = titleStyle.Foreground(pal.white)
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
	ch := lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(chName, innerW))
	vwText := strings.TrimSpace(formatViewsLine(v))
	if vwText == "" {
		vwText = " "
	}
	vw := lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(vwText, innerW))

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

func channelAvatar(name string, pal renderPalette) string {
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
	return lipgloss.NewStyle().Background(bg).Foreground(pal.white).Bold(true).Render(ch + " ")
}

func renderSearchList(s *app.State, th theme.Theme, tr thumbs.Renderer, width, height int, pal renderPalette) string {
	items := s.VisibleItems()
	if len(items) == 0 {
		if s.View == app.ViewChannel {
			if s.ChannelPending {
				return renderLoadingBox("Cargando canal...", s.ChannelTitle, s.AnimFrame, width, height, pal)
			}
			msg := "No hay videos disponibles para este canal. Pulsa Esc para volver."
			empty := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
				lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(msg, max(1, width-4))))
			return fitBlock(empty, width, height)
		}
		if s.SearchPending {
			return renderLoadingBox(fmt.Sprintf("Buscando %q...", s.SearchQuery), "Resultados de YouTube", s.AnimFrame, width, height, pal)
		}
		msg := fmt.Sprintf("Sin resultados para %q.", s.SearchQuery)
		if s.SearchQuery == "" {
			msg = "Presiona '/' para buscar videos en YouTube."
		}
		empty := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(msg, max(1, width-4))))
		return fitBlock(empty, width, height)
	}

	sampleRow := renderSearchRow(items[0], false, false, false, th, tr, width, pal)
	rowH := lipgloss.Height(sampleRow)
	if rowH < 1 {
		rowH = 1
	}
	visibleCount := max(1, height/rowH)

	curIdx := s.ActiveListIndex()
	startIdx := s.ActiveListScroll()
	if curIdx < startIdx {
		startIdx = curIdx
	}
	if curIdx >= startIdx+visibleCount {
		startIdx = curIdx - visibleCount + 1
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
		row := renderSearchRow(v, i == curIdx, s.SavedIDs[v.ID], isVideoPlaying(s, v), th, tr, width, pal)
		rows = append(rows, row)
		usedH += lipgloss.Height(row)
	}
	return fitBlock(strings.Join(rows, "\n"), width, height)
}

func renderSearchRow(v model.Video, selected, saved, playing bool, th theme.Theme, tr thumbs.Renderer, rowW int, pal renderPalette) string {
	innerW := max(10, rowW-4)
	tw := min(searchThumbW, max(12, innerW/3))
	thumbStr := tr.Render(v.ID, tw, searchThumbH)

	infoW := max(8, innerW-tw-2)
	titleStyle := lipgloss.NewStyle().Foreground(pal.text).Bold(true)
	if selected {
		titleStyle = titleStyle.Foreground(pal.white)
	}
	titleLines := wrapTwoLines(v.Title, infoW)
	t1 := titleStyle.Render(titleLines[0])
	t2 := titleStyle.Render(titleLines[1])
	views := lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(formatViewsLine(v), infoW))
	chName := strings.TrimSpace(v.Channel)
	if chName == "" {
		chName = "-"
	}
	chLine := lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(chName, infoW))

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
		badges = append(badges, lipgloss.NewStyle().Foreground(pal.yellow).Bold(true).Render("★ Guardado"))
	}
	if playing {
		badges = append(badges, lipgloss.NewStyle().Foreground(pal.red).Bold(true).Render("▶ sonando"))
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

func renderPlayerPanel(s *app.State, th theme.Theme, width int, pal renderPalette) string {
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
	if !s.PlayerStatus.IsRunning && s.PlayLoading {
		panelTitle = "Reproductor (mpv IPC · " + loadingSpinner(s.AnimFrame) + " Abriendo...)"
	}

	// Row 1: selected/active track on the left, toast (or queue only if >1) on the right.
	rightText := s.Toast
	if rightText == "" {
		queueCount := len(s.Queue)
		if s.PlayerStatus.PlaylistCount > queueCount {
			queueCount = s.PlayerStatus.PlaylistCount
		}
		if queueCount > 1 {
			rightText = fmt.Sprintf("Cola: %d", queueCount)
		}
	}
	rightStyled := ""
	if rightText != "" {
		rightStyled = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(s.Toast != "").Render(" " + truncatePlain(rightText, max(10, innerW/3)) + " ")
	}

	trackLabel := "Sin reproducción activa"
	if s.PlayLoading && s.PlayLoadingTitle != "" {
		trackLabel = loadingSpinner(s.AnimFrame) + " Abriendo en mpv: " + s.PlayLoadingTitle
	} else if s.PlayerStatus.Title != "" {
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
	leftStyled := lipgloss.NewStyle().Foreground(pal.text).Bold(s.PlayerStatus.IsRunning || s.PlayLoading).Render(" " + truncatePlain(trackLabel, availLeft))
	spacer1 := max(0, innerW-lipgloss.Width(leftStyled)-lipgloss.Width(rightStyled))
	row1 := leftStyled + strings.Repeat(" ", spacer1) + rightStyled

	// Row 2: clock + progress bar + duration.
	posStr := formatClock(s.PlayerStatus.TimePos)
	durStr := formatClock(s.PlayerStatus.Duration)
	barW := max(8, innerW-len(posStr)-len(durStr)-6)
	var progBar string
	if !s.PlayerStatus.IsRunning && s.PlayLoading {
		progBar = lipgloss.NewStyle().Foreground(pal.spotBorder).Render(strings.Repeat("─", barW))
	} else {
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
		progBar = lipgloss.NewStyle().Foreground(pal.spotAccent).Render(strings.Repeat("━", filled)) +
			lipgloss.NewStyle().Foreground(pal.spotBorder).Render(strings.Repeat("─", rem))
	}
	row2 := " " + lipgloss.NewStyle().Foreground(pal.muted).Render(posStr) + "  " + progBar + "  " + lipgloss.NewStyle().Foreground(pal.muted).Render(durStr)

	// Row 3: ultra-minimalist transport controls only (zero shortcut/command clutter).
	btnStyle := lipgloss.NewStyle().Foreground(pal.ctrlBtn).Bold(true)
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
	row3 := truncateStyledLine(" "+btns, innerW)

	return renderSpotatuiPanel(panelTitle, []string{row1, row2, row3}, width, 5, s.Focus == app.FocusPlayer, pal)
}

func renderQualityOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	_, _, boxW, _, _ := s.QualityModalBounds()
	innerW := max(12, boxW-4)
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render("🎬 Calidad, Ahorro y Solo Audio")
	rows := []string{title, ""}

	for i, opt := range app.QualityOptions {
		isActive := (!s.AudioOnly && !opt.AudioOnly && s.Quality == opt.Quality) || (s.AudioOnly && opt.AudioOnly)
		marker := "  "
		if i == s.QualityIdx {
			marker = "▶ "
		}
		badge := "○ "
		if isActive {
			badge = "● "
		}
		lineText := fmt.Sprintf("%s%d. %s%s — %s", marker, i+1, badge, opt.Label, opt.Desc)
		lineText = truncatePlain(lineText, innerW)
		if i == s.QualityIdx {
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.white).Background(pal.surf).Bold(true).Width(innerW).Render(lineText))
		} else if isActive {
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(lineText))
		} else {
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.text).Render(lineText))
		}
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain("  [↑/↓] Mover · [Enter/Click] Aplicar · [1-5] · [Esc]", innerW)))
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderSettingsOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	_, _, boxW, boxH := s.SettingsModalBounds()
	innerW := max(20, boxW-2)

	titleLeft := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(" ⚙ Configuración · iutui")
	themeTag := lipgloss.NewStyle().Foreground(pal.muted).Render(s.ThemeName + " ")
	hdrGap := max(1, innerW-lipgloss.Width(titleLeft)-lipgloss.Width(themeTag))
	hdrLine := titleLeft + strings.Repeat(" ", hdrGap) + themeTag

	secStyle := lipgloss.NewStyle().Foreground(pal.dim).Bold(true)
	rows := s.SettingsRows()

	renderSettingRow := func(r app.SettingRow) string {
		sel := r.Index == s.SettingsIdx
		marker := "  "
		if sel {
			marker = "▶ "
		}
		leftTxt := marker + r.Label
		var rightTxt string
		if r.IsBool {
			if r.Enabled {
				rightTxt = "● " + r.Value
			} else {
				rightTxt = "○ " + r.Value
			}
		} else if sel {
			rightTxt = "◀ " + r.Value + " ▶"
		} else {
			rightTxt = "  " + r.Value + "  "
		}

		maxRight := max(12, innerW/2)
		rightPlain := truncatePlain(rightTxt, maxRight)
		maxLeft := max(8, innerW-runewidth.StringWidth(rightPlain)-3)
		leftPlain := truncatePlain(leftTxt, maxLeft)
		gapW := max(1, innerW-1-runewidth.StringWidth(leftPlain)-runewidth.StringWidth(rightPlain))

		lblStyle := lipgloss.NewStyle().Foreground(pal.text)
		valStyle := lipgloss.NewStyle().Foreground(pal.muted)
		if r.IsBool && r.Enabled {
			valStyle = lipgloss.NewStyle().Foreground(pal.ctrlBtn).Bold(true)
		} else if r.IsBool {
			valStyle = lipgloss.NewStyle().Foreground(pal.dim)
		}
		if sel {
			lblStyle = lipgloss.NewStyle().Foreground(pal.white).Bold(true)
			valStyle = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
		}
		return lblStyle.Render(leftPlain) + strings.Repeat(" ", gapW) + valStyle.Render(rightPlain) + " "
	}

	footer := lipgloss.NewStyle().Foreground(pal.dim).Render(
		" " + truncatePlain("~/.config/iutui/config  ·  ↑/↓ Mover · ←/→/Enter/Click Cambiar · Esc", max(10, innerW-2)),
	)

	content := []string{
		hdrLine, // 0
		"",      // 1
		secStyle.Render("  APARIENCIA & TERMINAL"), // 2
		renderSettingRow(rows[0]),                  // 3
		renderSettingRow(rows[1]),                  // 4
		"",                                         // 5
		secStyle.Render("  REPRODUCCIÓN & CONSUMO"), // 6
		renderSettingRow(rows[2]),                   // 7
		renderSettingRow(rows[3]),                   // 8
		renderSettingRow(rows[4]),                   // 9
		renderSettingRow(rows[5]),                   // 10
		renderSettingRow(rows[6]),                   // 11
		"",                                          // 12
		secStyle.Render("  CATÁLOGO & PRIVACIDAD"),  // 13
		renderSettingRow(rows[7]),                   // 14
		renderSettingRow(rows[8]),                   // 15
		renderSettingRow(rows[9]),                   // 16
		"",                                          // 17
		footer,                                      // 18
	}

	panel := renderSpotatuiPanel("Configuración", content, boxW, boxH, true, pal)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, panel)
}

func renderStillWatchingOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render("⏸ ¿Todavía estás viendo?")
	track := strings.TrimSpace(s.PlayerStatus.Title)
	if track == "" {
		track = strings.TrimSpace(s.PlayLoadingTitle)
	}
	rows := []string{
		title,
		"",
		"  La reproducción automática lleva varios videos seguidos",
		"  sin actividad reciente y se pausó automáticamente.",
	}
	if track != "" {
		rows = append(rows, "", "  En pausa: "+truncatePlain(track, 46))
	}
	rows = append(rows,
		"",
		"  [Enter / Space] Continuar viendo",
		"  [Esc / q]       Mantener en pausa",
	)
	boxW := min(62, max(34, width-8))
	trimmed := make([]string, 0, len(rows))
	for _, r := range rows {
		trimmed = append(trimmed, truncateStyledLine(r, boxW-4))
	}
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(trimmed, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderHelpOverlay(_ string, width, height int, th theme.Theme, pal renderPalette) string {
	title := lipgloss.NewStyle().Foreground(pal.white).Bold(true).Render("Atajos de teclado (iutui)")
	rows := []string{
		title,
		"",
		"  j / k / ↑ / ↓   Mover selección (o pasa el puntero)",
		"  Enter / a / v   Reproducir (auto / audio / video)",
		"  x / V · m       Alternar Solo Audio en vivo · Mix Radio",
		"  Space / n / p   Pausar · Siguiente · Anterior",
		"  , / . · - / +   Retroceder/Avanzar 10s · Volumen ±5%",
		"  w / s / g / q   Guardar (★) · Suscribir · Canal · Cola",
		"  d / o / l       Descargar · Capítulos · Letras",
		"  f / c · b / t   Selector Calidad/Audio · SponsorBlock · Tema",
		"  A               Activar/desactivar Autoplay",
		"  [ / ] · Tab     Cambiar chip · Foco sidebar",
		"  1..5 / / / r    Pestañas · Buscar · Recargar",
		"  Esc · ? / Q     Volver/Cerrar · Ayuda · Salir",
	}
	boxW := min(68, max(30, width-8))
	trimmed := make([]string, 0, len(rows))
	for _, r := range rows {
		trimmed = append(trimmed, truncateStyledLine(r, boxW-4))
	}
	modal := th.Toast.Width(boxW - 2).Render(strings.Join(trimmed, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderChaptersOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render("📑 Capítulos del video")
	rows := []string{title, ""}

	start, end, boxW, _, _, _, _ := s.ChaptersModalWindow()
	innerW := boxW - 4
	for i := start; i < end; i++ {
		ch := s.Chapters[i]
		ts := formatClock(ch.Time)
		label := fmt.Sprintf("  %s  %s", ts, ch.Title)
		if i == s.ChapterIdx {
			label = fmt.Sprintf("▶ %s  %s", ts, ch.Title)
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.white).Bold(true).Render(truncatePlain(label, innerW)))
		} else {
			rows = append(rows, lipgloss.NewStyle().Foreground(pal.text).Render(truncatePlain(label, innerW)))
		}
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(pal.dim).Render("  [j/k] Navegar · [Enter/Click] Saltar · [Esc/o] Cerrar"))
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func buildLyricsContent(s *app.State, innerW, bodyH int, pal renderPalette) []string {
	if s.LyricsLoading {
		return []string{
			" " + lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(loadingSpinner(s.AnimFrame)+" Buscando letra..."),
			" " + lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(s.LyricsTrack, max(1, innerW-2))),
		}
	}
	if s.LyricsErr != "" {
		return []string{
			" " + lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(s.LyricsErr, max(1, innerW-2))),
			"",
			" " + lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain("[L] o [Esc] para cerrar", max(1, innerW-2))),
		}
	}
	headerText := strings.TrimSpace(s.Lyrics.TrackName)
	if headerText == "" {
		headerText = strings.TrimSpace(s.LyricsTrack)
	}
	if strings.TrimSpace(s.Lyrics.ArtistName) != "" {
		headerText += " · " + s.Lyrics.ArtistName
	}

	var lines []string
	if headerText != "" {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(truncatePlain(headerText, max(1, innerW-2))))
		lines = append(lines, "")
	}

	availLines := max(1, bodyH-len(lines))
	if len(s.Lyrics.Synced) > 0 {
		active := lyrics.ActiveLineIndex(s.Lyrics.Synced, s.PlayerStatus.TimePos)
		wrapW := max(8, innerW-4)
		activeStyle := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
		pastStyle := lipgloss.NewStyle().Foreground(pal.dim)
		futureStyle := lipgloss.NewStyle().Foreground(pal.text)

		var displayRows []string
		activeStartRow := 0
		for i, l := range s.Lyrics.Synced {
			if i == active {
				activeStartRow = len(displayRows)
			}
			wrapped := wrapLyricLine(l.Text, wrapW)
			for wi, wline := range wrapped {
				switch {
				case i == active:
					prefix := "  "
					if wi == 0 {
						prefix = "▶ "
					}
					displayRows = append(displayRows, " "+activeStyle.Render(prefix+wline))
				case i < active:
					displayRows = append(displayRows, " "+pastStyle.Render("  "+wline))
				default:
					displayRows = append(displayRows, " "+futureStyle.Render("  "+wline))
				}
			}
		}
		if len(displayRows) == 0 {
			return append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render("Sin letra disponible"))
		}
		maxStart := max(0, len(displayRows)-availLines)
		start := activeStartRow - availLines/3 + s.LyricsScroll
		if start < 0 {
			start = 0
		}
		if start > maxStart {
			start = maxStart
		}
		end := min(len(displayRows), start+availLines)
		return append(lines, displayRows[start:end]...)
	}

	if len(s.Lyrics.Plain) > 0 {
		wrapW := max(8, innerW-2)
		plainStyle := lipgloss.NewStyle().Foreground(pal.text)
		var displayRows []string
		for _, l := range s.Lyrics.Plain {
			for _, wline := range wrapLyricLine(l, wrapW) {
				displayRows = append(displayRows, " "+plainStyle.Render(wline))
			}
		}
		if len(displayRows) == 0 {
			return append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render("Sin letra disponible"))
		}
		maxStart := max(0, len(displayRows)-availLines)
		start := s.LyricsScroll
		if s.PlayerStatus.Duration > 0 && maxStart > 0 {
			ratio := s.PlayerStatus.TimePos / s.PlayerStatus.Duration
			if ratio < 0 {
				ratio = 0
			}
			if ratio > 1 {
				ratio = 1
			}
			start = int(ratio*float64(maxStart)) + s.LyricsScroll
		}
		if start < 0 {
			start = 0
		}
		if start > maxStart {
			start = maxStart
		}
		end := min(len(displayRows), start+availLines)
		return append(lines, displayRows[start:end]...)
	}

	return append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render("Sin letra disponible"))
}

func wrapLyricLine(text string, maxW int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxW <= 0 || runewidth.StringWidth(text) <= maxW {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var out []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
	}
	for _, w := range words {
		ww := runewidth.StringWidth(w)
		if cur.Len() > 0 && curW+1+ww <= maxW {
			cur.WriteByte(' ')
			cur.WriteString(w)
			curW += 1 + ww
			continue
		}
		flush()
		rem := w
		for runewidth.StringWidth(rem) > maxW {
			part := runewidth.Truncate(rem, maxW, "")
			if part == "" {
				break
			}
			out = append(out, part)
			rem = strings.TrimPrefix(rem, part)
		}
		if rem != "" {
			cur.WriteString(rem)
			curW = runewidth.StringWidth(rem)
		}
	}
	flush()
	return out
}

func renderLyricsPanel(s *app.State, _ theme.Theme, width, height int, pal renderPalette) string {
	innerW := max(4, width-2)
	bodyH := max(1, height-2)
	content := buildLyricsContent(s, innerW, bodyH, pal)
	return renderSpotatuiPanel("♫ Letras", content, width, height, true, pal)
}

func renderLyricsOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	boxW := min(64, max(34, width-8))
	boxH := max(8, min(height-6, 18))
	innerW := boxW - 4
	content := append([]string{lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render("♫ Letras")}, buildLyricsContent(s, innerW, boxH-3, pal)...)
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(content, "\n"))
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
