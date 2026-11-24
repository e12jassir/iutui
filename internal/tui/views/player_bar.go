package views

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/iutui/internal/app"
	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/tui/theme"
	"github.com/mattn/go-runewidth"
)

func renderPlayerPanel(s *app.State, _ theme.Theme, width int, pal renderPalette) string {
	innerW := max(10, width-2)
	layout := s.PlayerBarLayout()

	extra := ""
	if s.PlayerStatus.Speed > 0 && math.Abs(s.PlayerStatus.Speed-1.0) > 0.01 {
		extra += " · " + strconv.FormatFloat(math.Round(s.PlayerStatus.Speed*100)/100, 'f', -1, 64) + "x"
	}
	if s.PlayerStatus.LoopFile {
		extra += " · 󰑖"
	}
	panelTitle := "Reproductor" + extra
	if !s.PlayerStatus.IsRunning && s.PlayLoading {
		panelTitle = "Reproductor · " + loadingSpinner(s.AnimFrame) + " Abriendo..."
	}

	// Row 1: active track cleanly on left, toast or clickable Queue badge on right.
	rightText := s.Toast
	rightColor := pal.spotAccent
	rightBold := s.Toast != ""
	if rightText == "" && layout.HasQueue {
		rightText = layout.QueueText
		if s.ShowQueue {
			rightColor = pal.spotAccent
			rightBold = true
		} else {
			rightColor = pal.muted
			rightBold = false
		}
	}
	rightStyled := ""
	if rightText != "" {
		rightStyled = lipgloss.NewStyle().Foreground(rightColor).Bold(rightBold).Render(" " + truncatePlain(rightText, max(10, innerW/3)) + " ")
	}

	trackLabel := "Sin reproducción activa"
	if s.PlayLoading && s.PlayLoadingTitle != "" {
		trackLabel = loadingSpinner(s.AnimFrame) + " Abriendo en mpv: " + s.PlayLoadingTitle
	} else if strings.TrimSpace(s.PlayerStatus.Title) != "" {
		title := strings.TrimSpace(s.PlayerStatus.Title)
		trackLabel = "󰎈 " + title
		if ch := findChannelForActive(s, title); ch != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(ch)) {
			trackLabel += " · " + ch
		}
	} else if v, ok := s.SelectedVideo(); ok {
		trackLabel = "Seleccionado: " + v.Title
		if ch := cleanChannelName(v.Channel); ch != "" && !strings.Contains(strings.ToLower(v.Title), strings.ToLower(ch)) {
			trackLabel += " · " + ch
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
		if filled > 0 && filled < barW {
			progBar = lipgloss.NewStyle().Foreground(pal.spotAccent).Render(strings.Repeat("━", filled-1)+"●") +
				lipgloss.NewStyle().Foreground(pal.spotBorder).Render(strings.Repeat("─", barW-filled))
		} else {
			progBar = lipgloss.NewStyle().Foreground(pal.spotAccent).Render(strings.Repeat("━", filled)) +
				lipgloss.NewStyle().Foreground(pal.spotBorder).Render(strings.Repeat("─", barW-filled))
		}
	}
	row2 := " " + lipgloss.NewStyle().Foreground(pal.muted).Render(posStr) + "  " + progBar + "  " + lipgloss.NewStyle().Foreground(pal.muted).Render(durStr)

	// Row 3: Nerd Font Studio 3-zone transport aligned identically to layout hitboxes.
	activeID := s.CurrentPlaying.ID
	if activeID == "" {
		if v, ok := s.SelectedVideo(); ok {
			activeID = v.ID
		}
	}
	btnStyle := lipgloss.NewStyle().Foreground(pal.ctrlBtn).Bold(true)
	accentBtnStyle := lipgloss.NewStyle().Foreground(pal.spotAccent).Bold(true)
	spotifyGreenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#1DB954")).Bold(true)

	likeGlyph := "󰋕"
	likeStyle := btnStyle
	if activeID != "" && s.LikedIDs[activeID] {
		likeGlyph = "󰋑"
		likeStyle = spotifyGreenStyle
	}
	saveStyle := btnStyle
	if activeID != "" && s.SavedIDs[activeID] {
		saveStyle = accentBtnStyle
	}
	subLabel := "󰂚 Sub"
	subStyle := btnStyle
	if s.IsActiveSubscribed() {
		subLabel = "󰂞 Sub"
		subStyle = spotifyGreenStyle
	}
	leftZone := " " + likeStyle.Render(likeGlyph) + "  " + saveStyle.Render("󰥔") + "  " + subStyle.Render(subLabel)

	playGlyph := "󰐊"
	if s.PlayerStatus.IsRunning && !s.PlayerStatus.Paused {
		playGlyph = "󰏤"
	}
	centerZone := btnStyle.Render("󰒮") + "  " + btnStyle.Render("󰑟") + "  " +
		lipgloss.NewStyle().Foreground(pal.white).Bold(true).Render(playGlyph) + "  " +
		btnStyle.Render("󰈑") + "  " + btnStyle.Render("󰒭")

	leftW := runewidth.StringWidth(" " + likeGlyph + "  󰥔  " + subLabel)
	padCenter := max(1, layout.CenterStartCol-leftW)
	row3Plain := leftZone + strings.Repeat(" ", padCenter) + centerZone

	if layout.ShowVol {
		curW := leftW + padCenter + 13
		padRight := max(1, layout.RightStartCol-curW)
		rightZone := btnStyle.Render("󰖀") + "  " + lipgloss.NewStyle().Foreground(pal.muted).Render(layout.VolStr) + "  " + btnStyle.Render("󰕾") + " "
		row3Plain += strings.Repeat(" ", padRight) + rightZone
	}
	row3 := truncateStyledLine(row3Plain, innerW)

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

func cleanChannelName(ch string) string {
	ch = strings.TrimSpace(ch)
	if ch == "" || ch == "-" || strings.EqualFold(ch, "YouTube") {
		return ""
	}
	return ch
}

// findChannelForActive resolves the active track's channel in deterministic order
// (never iterating maps randomly and never returning generic "YouTube").
func findChannelForActive(s *app.State, title string) string {
	if ch := cleanChannelName(s.CurrentPlaying.Channel); ch != "" && (s.CurrentPlaying.Title == title || s.PlayerStatus.VideoID == s.CurrentPlaying.ID) {
		return ch
	}
	for _, v := range s.Queue {
		if v.Title == title {
			if ch := cleanChannelName(v.Channel); ch != "" {
				return ch
			}
		}
	}
	for _, v := range s.SearchResults {
		if v.Title == title {
			if ch := cleanChannelName(v.Channel); ch != "" {
				return ch
			}
		}
	}
	tabOrder := []model.Tab{
		model.TabMusic,
		model.TabHome,
		model.TabLiked,
		model.TabHistory,
		model.TabWatchLater,
		model.TabSubscriptions,
		model.TabDownloads,
	}
	for _, tab := range tabOrder {
		for _, v := range s.Feeds[tab] {
			if v.Title == title {
				if ch := cleanChannelName(v.Channel); ch != "" {
					return ch
				}
			}
		}
	}
	return ""
}
