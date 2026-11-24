package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/e12jassir/iutui/internal/lyrics"
	"github.com/e12jassir/iutui/internal/player"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/e12jassir/iutui/internal/tui/thumbs"
	"github.com/mattn/go-runewidth"
)

func renderQualityOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	_, _, boxW, _, _ := s.QualityModalBounds()
	innerW := max(12, boxW-4)
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(vs.QualityTitle)
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
	rows = append(rows, "", lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(vs.QualityFooter, innerW)))
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderSettingsOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	_, _, boxW, boxH, scroll, visBodyH, layout := s.SettingsModalWindow()
	innerW := max(20, boxW-2)
	rows := s.SettingsRows()
	if len(rows) == 0 {
		return ""
	}
	selIdx := s.SettingsIdx
	if selIdx < 0 || selIdx >= len(rows) {
		selIdx = 0
	}

	titleLeft := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(vs.SettingsHeader)
	scrollInd := ""
	if len(layout) > visBodyH {
		hasAbove := scroll > 0
		hasBelow := scroll+visBodyH < len(layout)
		switch {
		case hasAbove && hasBelow:
			scrollInd = " ⇅"
		case hasAbove:
			scrollInd = " ↑"
		case hasBelow:
			scrollInd = " ↓"
		}
	}
	tagPlain := fmt.Sprintf("%s · %d/%d%s ", s.ThemeName, selIdx+1, len(rows), scrollInd)
	maxTag := max(4, innerW-lipgloss.Width(titleLeft)-1)
	rightTag := lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(tagPlain, maxTag))
	hdrGap := max(1, innerW-lipgloss.Width(titleLeft)-lipgloss.Width(rightTag))
	hdrLine := titleLeft + strings.Repeat(" ", hdrGap) + rightTag

	divider := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(" " + strings.Repeat("─", max(1, innerW-2)))

	renderSettingRow := func(r app.SettingRow) string {
		sel := r.Index == selIdx
		marker := "  "
		if sel {
			marker = "▶ "
		}
		var rightTxt string
		if r.IsBool {
			if r.Enabled {
				rightTxt = vs.SettingsEnabled
			} else {
				rightTxt = vs.SettingsDisabled
			}
		} else if sel {
			rightTxt = "◀ " + r.Value + " ▶"
		} else {
			rightTxt = "  " + r.Value + "  "
		}

		maxRight := max(12, innerW/2)
		rightPlain := truncatePlain(rightTxt, maxRight)
		rightW := runewidth.StringWidth(rightPlain)
		maxLeft := max(8, innerW-rightW-3)

		badge := ""
		if r.Shortcut != "" {
			badge = " [" + r.Shortcut + "]"
		}
		if runewidth.StringWidth(marker+r.Label+badge) > maxLeft {
			badge = ""
		}
		lblText := truncatePlain(r.Label, max(4, maxLeft-runewidth.StringWidth(marker)-runewidth.StringWidth(badge)))
		leftW := runewidth.StringWidth(marker) + runewidth.StringWidth(lblText) + runewidth.StringWidth(badge)
		gapW := max(1, innerW-1-leftW-rightW)

		markerSt := lipgloss.NewStyle().Foreground(pal.text)
		lblSt := lipgloss.NewStyle().Foreground(pal.text)
		badgeSt := lipgloss.NewStyle().Foreground(pal.dim)
		valSt := lipgloss.NewStyle().Foreground(pal.muted)
		bgSt := lipgloss.NewStyle()
		if r.IsBool && r.Enabled {
			valSt = lipgloss.NewStyle().Foreground(pal.ctrlBtn).Bold(true)
		} else if r.IsBool {
			valSt = lipgloss.NewStyle().Foreground(pal.dim)
		}
		if sel {
			markerSt = lipgloss.NewStyle().Foreground(pal.spotAccent).Background(pal.surf).Bold(true)
			lblSt = lipgloss.NewStyle().Foreground(pal.white).Background(pal.surf).Bold(true)
			badgeSt = lipgloss.NewStyle().Foreground(pal.muted).Background(pal.surf)
			valSt = lipgloss.NewStyle().Foreground(pal.spotAccent).Background(pal.surf).Bold(true)
			bgSt = lipgloss.NewStyle().Background(pal.surf)
		}

		row := markerSt.Render(marker) + lblSt.Render(lblText)
		if badge != "" {
			row += badgeSt.Render(badge)
		}
		return row + bgSt.Render(strings.Repeat(" ", gapW)) + valSt.Render(rightPlain) + bgSt.Render(" ")
	}

	content := make([]string, 0, visBodyH+5)
	content = append(content, hdrLine, divider)

	for pos := scroll; pos < scroll+visBodyH; pos++ {
		if pos >= len(layout) {
			content = append(content, "")
			continue
		}
		line := layout[pos]
		switch {
		case line.IsSpacer:
			content = append(content, "")
		case line.IsSection:
			accentSym := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render("◆")
			secTxt := lipgloss.NewStyle().Foreground(pal.dim).Bold(true).Render(line.Section)
			used := 2 + 1 + 1 + runewidth.StringWidth(line.Section) + 1
			rem := max(1, innerW-used-1)
			rule := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(strings.Repeat("─", rem))
			content = append(content, truncateStyledLine("  "+accentSym+" "+secTxt+" "+rule, innerW))
		case line.SettingIdx >= 0 && line.SettingIdx < len(rows):
			content = append(content, renderSettingRow(rows[line.SettingIdx]))
		default:
			content = append(content, "")
		}
	}

	descBar := " " + lipgloss.NewStyle().Foreground(pal.ctrlBtn).Bold(true).Render("ⓘ") + " " +
		lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(rows[selIdx].Desc, max(8, innerW-4)))
	footer := " " + lipgloss.NewStyle().Foreground(pal.dim).Render(
		truncatePlain(vs.SettingsFooter, max(10, innerW-2)),
	)

	content = append(content, divider, descBar, footer)

	panel := renderSpotatuiPanel(vs.SettingsPanelTitle, content, boxW, boxH, true, pal)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, panel)
}

func renderStillWatchingOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(vs.StillWatchingTitle)
	track := strings.TrimSpace(s.PlayerStatus.Title)
	if track == "" {
		track = strings.TrimSpace(s.PlayLoadingTitle)
	}
	rows := []string{
		title,
		"",
		vs.StillWatchingLine1,
		vs.StillWatchingLine2,
	}
	if track != "" {
		rows = append(rows, "", i18n.Format(vs.StillWatchingPaused, map[string]string{"track": truncatePlain(track, 46)}))
	}
	rows = append(rows,
		"",
		vs.StillWatchingContinue,
		vs.StillWatchingKeepPaused,
	)
	boxW := min(62, max(34, width-8))
	trimmed := make([]string, 0, len(rows))
	for _, r := range rows {
		trimmed = append(trimmed, truncateStyledLine(r, boxW-4))
	}
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(trimmed, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

type helpItem struct {
	keys string
	desc string
}

type helpSection struct {
	title string
	items []helpItem
}

func renderHelpColumn(sections []helpSection, colW, keyColW int, withSpacers bool, pal renderPalette) []string {
	var lines []string
	keyStyle := lipgloss.NewStyle().Foreground(pal.ctrlBtn).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(pal.text)
	accentStyle := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	secStyle := lipgloss.NewStyle().Foreground(pal.dim).Bold(true)
	ruleStyle := lipgloss.NewStyle().Foreground(pal.spotBorder)

	for si, sec := range sections {
		if si > 0 && withSpacers {
			lines = append(lines, "")
		}
		used := 1 + 1 + 1 + runewidth.StringWidth(sec.title) + 1
		rem := max(1, colW-used-1)
		hdr := " " + accentStyle.Render("◆") + " " + secStyle.Render(sec.title) + " " + ruleStyle.Render(strings.Repeat("─", rem))
		lines = append(lines, truncateStyledLine(hdr, colW))

		for _, it := range sec.items {
			kw := runewidth.StringWidth(it.keys)
			padK := max(1, keyColW-kw+1)
			descAvail := max(6, colW-1-kw-padK)
			row := " " + keyStyle.Render(it.keys) + strings.Repeat(" ", padK) + descStyle.Render(truncatePlain(it.desc, descAvail))
			lines = append(lines, truncateStyledLine(row, colW))
		}
	}
	return lines
}

func renderHelpOverlay(s *app.State, width, height int, _ theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	leftSections := []helpSection{
		{
			title: vs.HelpSecNav,
			items: []helpItem{
				{keys: "j/k / ↑/↓", desc: vs.HelpNavMove},
				{keys: "h/l / ←/→", desc: vs.HelpNavSide},
				{keys: "1..5 · Tab", desc: vs.HelpNavTabs},
				{keys: "/ · [ / ]", desc: vs.HelpNavSearch},
				{keys: "r · g", desc: vs.HelpNavReload},
			},
		},
		{
			title: vs.HelpSecLib,
			items: []helpItem{
				{keys: "w / W", desc: vs.HelpLibSave},
				{keys: "s · q", desc: vs.HelpLibSub},
				{keys: "u · d", desc: vs.HelpLibQueue},
			},
		},
	}

	rightSections := []helpSection{
		{
			title: vs.HelpSecPlay,
			items: []helpItem{
				{keys: "Enter / a / v", desc: vs.HelpPlayStart},
				{keys: "Space · n / p", desc: vs.HelpPlayPause},
				{keys: ", / . · - / +", desc: vs.HelpPlaySeek},
				{keys: "{ / } / 0 · R", desc: vs.HelpPlaySpeed},
				{keys: "x / V · m", desc: vs.HelpPlayAudio},
				{keys: "o / l · f / c", desc: vs.HelpPlayChapters},
				{keys: "z/Z · y", desc: vs.HelpPlaySync},
			},
		},
		{
			title: vs.HelpSecSys,
			items: []helpItem{
				{keys: "A", desc: vs.HelpSysAutoplay},
				{keys: "b / t", desc: vs.HelpSysTheme},
				{keys: "S · ?", desc: vs.HelpSysSettings},
				{keys: "Esc · Q", desc: vs.HelpSysQuit},
			},
		},
	}

	var boxW int
	wide := width >= 96
	if wide {
		boxW = min(100, width-6)
	} else {
		boxW = min(74, max(36, width-6))
	}
	innerW := max(24, boxW-2)

	hdrLeft := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(vs.HelpHeader)
	verTag := ""
	if s != nil {
		if ver := strings.TrimSpace(s.AppVersion); ver != "" {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			verTag = ver
		}
	}
	hdrLine := hdrLeft
	if verTag != "" {
		hdrRight := lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(verTag+" ", max(2, innerW-lipgloss.Width(hdrLeft)-1)))
		hdrGap := max(1, innerW-lipgloss.Width(hdrLeft)-lipgloss.Width(hdrRight))
		hdrLine = hdrLeft + strings.Repeat(" ", hdrGap) + hdrRight
	}

	divider := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(" " + strings.Repeat("─", max(1, innerW-2)))
	footer := " " + lipgloss.NewStyle().Foreground(pal.dim).Render(
		truncatePlain(vs.HelpFooter, max(10, innerW-2)),
	)

	maxH := max(10, height-2)
	availBody := max(1, maxH-6) // 2 borders + hdr + top div + bottom div + footer

	var bodyLines []string
	if wide {
		leftW := min(41, (innerW-3)*42/100)
		rightW := max(16, innerW-leftW-3)
		leftLines := renderHelpColumn(leftSections, leftW, 10, true, pal)
		rightLines := renderHelpColumn(rightSections, rightW, 13, true, pal)
		nRows := max(len(leftLines), len(rightLines))
		div := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(" │ ")
		for i := 0; i < nRows; i++ {
			l := ""
			if i < len(leftLines) {
				l = leftLines[i]
			}
			r := ""
			if i < len(rightLines) {
				r = rightLines[i]
			}
			padL := max(0, leftW-lipgloss.Width(l))
			bodyLines = append(bodyLines, l+strings.Repeat(" ", padL)+div+r)
		}
	} else {
		allSections := append(append([]helpSection{}, leftSections...), rightSections...)
		bodyLines = renderHelpColumn(allSections, innerW, 13, true, pal)
		if len(bodyLines) > availBody {
			bodyLines = renderHelpColumn(allSections, innerW, 13, false, pal)
		}
	}

	if len(bodyLines) > availBody {
		bodyLines = bodyLines[:availBody]
	}

	content := make([]string, 0, len(bodyLines)+4)
	content = append(content, hdrLine, divider)
	content = append(content, bodyLines...)
	content = append(content, divider, footer)

	boxH := min(maxH, len(content)+2)
	panel := renderSpotatuiPanel(vs.HelpPanelTitle, content, boxW, boxH, true, pal)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, panel)
}

func renderQueueOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	start, end, boxW, _, _, _, _ := s.QueueModalWindow()
	innerW := boxW - 4
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(
		i18n.Format(vs.QueueTitle, map[string]string{"n": strconv.Itoa(len(s.MPVPlaylist))}),
	)
	rows := []string{title, ""}

	if len(s.MPVPlaylist) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(pal.muted).Render(vs.QueueEmpty))
	} else {
		for i := start; i < end; i++ {
			it := s.MPVPlaylist[i]
			name := strings.TrimSpace(it.Title)
			if name == "" {
				name = strings.TrimSpace(it.Filename)
			}
			if name == "" {
				name = i18n.Format(vs.QueueTrackN, map[string]string{"n": strconv.Itoa(it.Index + 1)})
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

	footer := vs.QueueFooter
	rows = append(rows, "", lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(footer, innerW)))
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func renderQueueSidePanel(s *app.State, _ theme.Theme, tr thumbs.Renderer, width, height int, pal renderPalette) string {
	vs := s.Catalog().Views
	innerW := max(12, width-2)
	bodyH := max(1, height-2)
	start, end, _, _, _, _ := s.QueueSideWindow()

	hdrTitleTxt := i18n.Format(vs.QueueSideTitle, map[string]string{"n": strconv.Itoa(len(s.MPVPlaylist))})
	closeTxt := "[✕]"
	maxTitleW := max(6, innerW-len(closeTxt)-2)
	hdrTitle := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(truncatePlain(hdrTitleTxt, maxTitleW))
	closeBtn := lipgloss.NewStyle().Foreground(pal.muted).Bold(true).Render(closeTxt)
	hdrGap := max(1, innerW-lipgloss.Width(hdrTitle)-lipgloss.Width(closeBtn)-1)
	hdrRow := hdrTitle + strings.Repeat(" ", hdrGap) + closeBtn + " "
	divider := lipgloss.NewStyle().Foreground(pal.spotBorder).Render(" " + strings.Repeat("─", max(1, innerW-2)))

	lines := make([]string, 0, bodyH)
	lines = append(lines, hdrRow, divider)

	itemAreaH := max(1, bodyH-3) // header + divider + footer
	if len(s.MPVPlaylist) == 0 {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(vs.QueueSideEmpty, max(4, innerW-2))))
	} else {
		showCover := tr != nil && innerW >= 24
		const coverW = 10
		const coverH = 3

		for i := start; i < end; i++ {
			if len(lines) >= 2+itemAreaH {
				break
			}
			it := s.MPVPlaylist[i]
			vid := it.VideoID
			if vid == "" {
				vid = player.ExtractYouTubeID(it.Filename)
			}
			if showCover && vid != "" {
				tr.Prefetch(vid)
			}

			name := strings.TrimSpace(it.Title)
			if name == "" {
				name = strings.TrimSpace(it.Filename)
			}
			if name == "" {
				name = i18n.Format(vs.QueueTrackN, map[string]string{"n": strconv.Itoa(it.Index + 1)})
			}
			artist := strings.TrimSpace(it.Artist)
			if artist == "" {
				if idx := strings.Index(name, " - "); idx > 0 {
					artist = strings.TrimSpace(name[:idx])
					name = strings.TrimSpace(name[idx+3:])
				} else {
					artist = vs.QueueSideMix
				}
			}

			prefix := fmt.Sprintf("%d. ", i+1)
			if it.Current {
				prefix = "▶ "
			} else if i == s.QueueIdx {
				prefix = "▸ "
			}

			dur := strings.TrimSpace(it.Duration)
			if dur == "" {
				dur = "--:--"
			}
			meta := dur + " · " + vs.QueueSideQueued
			if it.Current {
				meta = dur + " · " + vs.QueueSidePlaying
			}

			selected := i == s.QueueIdx
			titleSt := lipgloss.NewStyle().Foreground(pal.text).Bold(it.Current)
			artistSt := lipgloss.NewStyle().Foreground(pal.muted)
			metaSt := lipgloss.NewStyle().Foreground(pal.dim)
			if it.Current {
				titleSt = lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
				metaSt = lipgloss.NewStyle().Foreground(pal.spotAccent)
			}
			if selected {
				titleSt = lipgloss.NewStyle().Foreground(pal.white).Background(pal.surf).Bold(true)
				artistSt = lipgloss.NewStyle().Foreground(pal.text).Background(pal.surf)
				metaSt = lipgloss.NewStyle().Foreground(pal.spotAccent).Background(pal.surf)
			}

			if showCover && vid != "" {
				infoW := max(8, innerW-coverW-3)
				thumbRaw := tr.Render(vid, coverW, coverH)
				thumbRows := strings.Split(thumbRaw, "\n")
				infoRows := []string{
					titleSt.Render(truncatePlain(prefix+name, infoW)),
					artistSt.Render(truncatePlain(artist, infoW)),
					metaSt.Render(truncatePlain(meta, infoW)),
				}
				for r := 0; r < coverH && len(lines) < 2+itemAreaH; r++ {
					tLine := strings.Repeat(" ", coverW)
					if r < len(thumbRows) && thumbRows[r] != "" {
						tLine = thumbRows[r]
					}
					rowLine := " " + tLine + " " + infoRows[r]
					if selected {
						rem := max(0, innerW-lipgloss.Width(rowLine))
						rowLine += lipgloss.NewStyle().Background(pal.surf).Render(strings.Repeat(" ", rem))
					}
					lines = append(lines, rowLine)
				}
			} else {
				infoW := max(8, innerW-2)
				infoRows := []string{
					titleSt.Render(" " + truncatePlain(prefix+name, infoW)),
					artistSt.Render("   " + truncatePlain(artist, max(4, infoW-2))),
					metaSt.Render("   " + truncatePlain(meta, max(4, infoW-2))),
				}
				for _, ir := range infoRows {
					if len(lines) >= 2+itemAreaH {
						break
					}
					if selected {
						rem := max(0, innerW-lipgloss.Width(ir))
						ir += lipgloss.NewStyle().Background(pal.surf).Render(strings.Repeat(" ", rem))
					}
					lines = append(lines, ir)
				}
			}
			if len(lines) < 2+itemAreaH {
				lines = append(lines, "")
			}
		}
	}

	for len(lines) < bodyH-1 {
		lines = append(lines, "")
	}
	footerLeft := vs.QueueSideFooter
	footerRight := ""
	if len(s.MPVPlaylist) > end-start && end > start {
		footerRight = fmt.Sprintf("%d-%d/%d", start+1, end, len(s.MPVPlaylist))
	}
	availLeft := max(8, innerW-2-lipgloss.Width(footerRight))
	leftRendered := lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(footerLeft, availLeft))
	footer := " " + leftRendered
	if footerRight != "" {
		gap := max(1, innerW-2-lipgloss.Width(leftRendered)-lipgloss.Width(footerRight))
		footer = " " + leftRendered + strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(pal.muted).Render(footerRight)
	}
	lines = append(lines[:bodyH-1], footer)

	return renderSpotatuiPanel(vs.QueueSidePanel, lines, width, height, true, pal)
}

func renderChaptersOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	title := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(vs.ChaptersTitle)
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
	rows = append(rows, "", lipgloss.NewStyle().Foreground(pal.dim).Render(vs.ChaptersFooter))
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func buildLyricsContent(s *app.State, innerW, bodyH int, pal renderPalette) []string {
	vs := s.Catalog().Views
	if s.LyricsLoading {
		return []string{
			" " + lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(loadingSpinner(s.AnimFrame)+" "+vs.LyricsSearching),
			" " + lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(s.LyricsTrack, max(1, innerW-2))),
		}
	}
	if s.LyricsErr != "" {
		return []string{
			" " + lipgloss.NewStyle().Foreground(pal.muted).Render(truncatePlain(s.LyricsErr, max(1, innerW-2))),
			"",
			" " + lipgloss.NewStyle().Foreground(pal.dim).Render(truncatePlain(vs.LyricsCloseHint, max(1, innerW-2))),
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
		effectivePos := s.PlayerStatus.TimePos + s.LyricsOffset
		active := lyrics.ActiveLineIndex(s.Lyrics.Synced, effectivePos)
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
			return append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render(vs.LyricsUnavailable))
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
			return append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render(vs.LyricsUnavailable))
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

	return append(lines, " "+lipgloss.NewStyle().Foreground(pal.muted).Render(vs.LyricsUnavailable))
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
	return renderSpotatuiPanel(s.Catalog().Views.LyricsPanelTitle, content, width, height, true, pal)
}

func renderLyricsOverlay(s *app.State, width, height int, th theme.Theme, pal renderPalette) string {
	vs := s.Catalog().Views
	boxW := min(64, max(34, width-8))
	boxH := max(8, min(height-6, 18))
	innerW := boxW - 4
	content := append([]string{lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true).Render(vs.LyricsPanelTitle)}, buildLyricsContent(s, innerW, boxH-3, pal)...)
	modal := th.Toast.BorderForeground(pal.spotAccent).Width(boxW - 2).Render(strings.Join(content, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
