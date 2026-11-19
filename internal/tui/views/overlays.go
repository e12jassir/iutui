package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/mattn/go-runewidth"
)

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
		"  { / } / 0 · R   Velocidad (0.5x..2x) · Repetir pista",
		"  w / W · s / g   Guardar · Me gusta · Suscribir · Canal",
		"  q / u · d       Añadir a cola · Ver Cola · Descargar",
		"  o / l · f / c   Capítulos · Letras · Calidad/Audio",
		"  A · b / t       Activar/desactivar Autoplay · Sponsor · Tema",
		"  [ / ] · Tab     Cambiar chip · Foco sidebar",
		"  1..5 / / / r    Pestañas · Buscar · Recargar",
		"  Esc · ? / Q     Volver/Cerrar · Ayuda · Salir",
	}
	boxW := min(70, max(30, width-8))
	trimmed := make([]string, 0, len(rows))
	for _, r := range rows {
		trimmed = append(trimmed, truncateStyledLine(r, boxW-4))
	}
	modal := th.Toast.Width(boxW - 2).Render(strings.Join(trimmed, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderQueueOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	start, end, boxW, _, _, _, _ := s.QueueModalWindow()
	innerW := boxW - 4
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(
		fmt.Sprintf("♫ Cola de reproducción (%d)", len(s.MPVPlaylist)),
	)
	rows := []string{title, ""}

	if len(s.MPVPlaylist) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(pal.muted).Render("  Cola vacía — añade pistas con 'q'"))
	} else {
		for i := start; i < end; i++ {
			it := s.MPVPlaylist[i]
			name := strings.TrimSpace(it.Title)
			if name == "" {
				name = strings.TrimSpace(it.Filename)
			}
			if name == "" {
				name = fmt.Sprintf("Pista %d", it.Index+1)
			}
			marker := "  "
			if it.Current {
				marker = "▶ "
			}
			cursor := "  "
			if i == s.QueueIdx {
				cursor = "▸ "
			}
			line := fmt.Sprintf("%s%s%d. %s", cursor, marker, i+1, name)
			st := lipgloss.NewStyle().Foreground(pal.text)
			if i == s.QueueIdx {
				st = lipgloss.NewStyle().Foreground(pal.white).Bold(true)
			} else if it.Current {
				st = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
			}
			rows = append(rows, st.Render(truncatePlain(line, innerW)))
		}
	}

	footer := "  [j/k] Navegar · [Enter] Saltar · [x/d] Quitar · [c] Vaciar · [u/Esc] Cerrar"
	rows = append(rows, "", lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(footer, innerW)))
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(rows, "\n"))
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
