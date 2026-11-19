package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/download"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/e12jassir/iutui/internal/tui/thumbs"
)

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
	sampleCard := renderCard(items[0], false, false, false, "", th, tr, actualCardW, pal)
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
			dlBadge := downloadProgressBadge(s, v.ID)
			card := renderCard(v, selected, saved, playing, dlBadge, th, tr, actualCardW, pal)
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

func renderCard(v model.Video, selected, saved, playing bool, dlBadge string, th theme.Theme, tr thumbs.Renderer, cardW int, pal renderPalette) string {
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
	if dlBadge != "" {
		badgeParts = append(badgeParts, lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(dlBadge))
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

	sampleRow := renderSearchRow(items[0], false, false, false, "", th, tr, width, pal)
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
		dlBadge := downloadProgressBadge(s, v.ID)
		row := renderSearchRow(v, i == curIdx, s.SavedIDs[v.ID], isVideoPlaying(s, v), dlBadge, th, tr, width, pal)
		rows = append(rows, row)
		usedH += lipgloss.Height(row)
	}
	return fitBlock(strings.Join(rows, "\n"), width, height)
}

func renderSearchRow(v model.Video, selected, saved, playing bool, dlBadge string, th theme.Theme, tr thumbs.Renderer, rowW int, pal renderPalette) string {
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
	if dlBadge != "" {
		badges = append(badges, lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(dlBadge))
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

func isVideoPlaying(s *app.State, v model.Video) bool {
	return s.PlayerStatus.IsRunning && s.PlayerStatus.Title != "" && s.PlayerStatus.Title == v.Title
}

func downloadProgressBadge(s *app.State, id string) string {
	if s == nil || len(s.DownloadProgress) == 0 || id == "" {
		return ""
	}
	it, ok := s.DownloadProgress[id]
	if !ok {
		return ""
	}
	switch it.Status {
	case download.StatusPending:
		return "⬇ En cola..."
	case download.StatusDownloading:
		if spd := strings.TrimSpace(it.Speed); spd != "" {
			return fmt.Sprintf("⬇ %.0f%% · %s", it.Progress, spd)
		}
		return fmt.Sprintf("⬇ %.0f%%", it.Progress)
	default:
		return ""
	}
}
