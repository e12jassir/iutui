package views

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/tui/theme"
)

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
	extra := ""
	if s.PlayerStatus.Speed > 0 && math.Abs(s.PlayerStatus.Speed-1.0) > 0.01 {
		extra += " · " + strconv.FormatFloat(math.Round(s.PlayerStatus.Speed*100)/100, 'f', -1, 64) + "x"
	}
	if s.PlayerStatus.LoopFile {
		extra += " · ↻"
	}
	panelTitle := fmt.Sprintf("Reproductor (mpv IPC · %s %s · Vol: %s%s)", icon, stateLabel, volStr, extra)
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
