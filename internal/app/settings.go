package app

import (
	"context"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/tui/theme"
)

// SettingsRowCount is the number of interactive entries in the Settings modal.
const SettingsRowCount = 10

// SettingsItemRowOffsets maps each interactive setting index (0..9) to its
// exact line offset inside the rendered Settings overlay box (below top border).
var SettingsItemRowOffsets = [SettingsRowCount]int{3, 4, 7, 8, 9, 10, 11, 14, 15, 16}

// SettingRow describes a single displayable row in the interactive Settings studio.
type SettingRow struct {
	Index    int
	Section  string
	Label    string
	Value    string
	Shortcut string
	IsBool   bool
	Enabled  bool
}

// SettingsRows returns the ordered interactive configuration rows reflecting current state.
func (s *State) SettingsRows() []SettingRow {
	onOff := func(b bool) string {
		if b {
			return "ACTIVADO"
		}
		return "DESACTIVADO"
	}

	themeVal := s.ThemeName
	switch themeVal {
	case "", "youtube-dark":
		themeVal = "youtube-dark"
	case "terminal":
		themeVal = "terminal (Nativo)"
	}

	previewVal := s.PreviewMode
	switch previewVal {
	case "kitty":
		previewVal = "kitty (HD píxeles)"
	case "chafa":
		previewVal = "chafa (Bloques ANSI)"
	case "none":
		previewVal = "none (Solo texto)"
	default:
		previewVal = "auto (Ghostty / Kitty HD)"
	}

	qualVal := "MAX (4K/Best)"
	switch s.Quality {
	case 1080:
		qualVal = "1080p Full HD"
	case 720:
		qualVal = "720p HD"
	case 480:
		qualVal = "480p SD"
	case 360:
		qualVal = "360p Ahorro"
	default:
		if s.Quality > 0 {
			qualVal = fmt.Sprintf("%dp", s.Quality)
		}
	}

	modeVal := "Ventana (video)"
	if s.AudioOnly || s.DefaultMode == "audio" {
		modeVal = "Solo Audio (sin ventana)"
	} else if s.DefaultMode == "pip" {
		modeVal = "PiP (flotante)"
	} else if s.DefaultMode == "fs" {
		modeVal = "Pantalla completa"
	}

	cookiesVal := "auto (Zen / Firefox)"
	if s.CookiesMode == "off" {
		cookiesVal = "off (Solo invitado RSS)"
	}

	return []SettingRow{
		{Index: 0, Section: "APARIENCIA & TERMINAL", Label: "Tema interfaz", Value: themeVal, Shortcut: "t"},
		{Index: 1, Section: "APARIENCIA & TERMINAL", Label: "Miniaturas", Value: previewVal},
		{Index: 2, Section: "REPRODUCCIÓN & CONSUMO", Label: "Resolución máxima", Value: qualVal, Shortcut: "f/c"},
		{Index: 3, Section: "REPRODUCCIÓN & CONSUMO", Label: "Modo reproducción", Value: modeVal, Shortcut: "x"},
		{Index: 4, Section: "REPRODUCCIÓN & CONSUMO", Label: "Volumen mpv", Value: fmt.Sprintf("%d%%", s.AudioVolume), Shortcut: "-/+"},
		{Index: 5, Section: "REPRODUCCIÓN & CONSUMO", Label: "Autoplay [A]:", Value: onOff(s.Autoplay), Shortcut: "A", IsBool: true, Enabled: s.Autoplay},
		{Index: 6, Section: "REPRODUCCIÓN & CONSUMO", Label: "SponsorBlock", Value: onOff(s.SponsorBlock), Shortcut: "b", IsBool: true, Enabled: s.SponsorBlock},
		{Index: 7, Section: "CATÁLOGO & PRIVACIDAD", Label: "Ocultar Shorts", Value: onOff(s.HideShorts), IsBool: true, Enabled: s.HideShorts},
		{Index: 8, Section: "CATÁLOGO & PRIVACIDAD", Label: "Mezclar canales", Value: onOff(s.Interleave), IsBool: true, Enabled: s.Interleave},
		{Index: 9, Section: "CATÁLOGO & PRIVACIDAD", Label: "Cookies navegador", Value: cookiesVal},
	}
}

// SettingsModalBounds returns the exact screen placement of the Settings modal.
func (s *State) SettingsModalBounds() (leftX, topY, boxW, boxH int) {
	boxW = min(76, max(44, s.Width-8))
	// 1 title + 1 spacer + 3 sections * (1 hdr + items) + 2 spacers + 1 footer + 2 borders = 21 lines.
	boxH = 21
	leftX = (s.Width - boxW) / 2
	topY = max(0, (s.Height-boxH)/2)
	return leftX, topY, boxW, boxH
}

func (s *State) settingsModalRowAt(x, y int) (idx int, inside bool) {
	leftX, topY, boxW, boxH := s.SettingsModalBounds()
	if x < leftX || x >= leftX+boxW || y < topY || y >= topY+boxH {
		return -1, false
	}
	relY := y - (topY + 1)
	for i, off := range SettingsItemRowOffsets {
		if relY == off {
			return i, true
		}
	}
	return -1, true
}

// AdjustSetting steps setting idx forward (delta >= 0) or backward (delta < 0),
// applies runtime changes immediately, and persists updated YT_* values.
func (s *State) AdjustSetting(idx, delta int) (*State, tea.Cmd) {
	if delta == 0 {
		delta = 1
	}
	switch idx {
	case 0: // Theme
		palettes := theme.Palettes()
		cur := 0
		for i, name := range palettes {
			if name == s.ThemeName {
				cur = i
				break
			}
		}
		next := palettes[(cur+delta+len(palettes))%len(palettes)]
		s.ThemeName = next
		return s, tea.Batch(s.toastCmd("🎨 Tema: "+next), s.saveThemeCmd(next))

	case 1: // Preview mode
		modes := []string{"auto", "chafa", "none"}
		cur := 0
		for i, m := range modes {
			if m == s.PreviewMode {
				cur = i
				break
			}
		}
		next := modes[(cur+delta+len(modes))%len(modes)]
		s.PreviewMode = next
		return s, tea.Batch(s.toastCmd("🖼 Miniaturas: "+next), s.saveConfigCmd("YT_PREVIEW", next))

	case 2: // Video quality (0, 1080, 720, 480, 360)
		cur := 0
		for i, q := range qualityPresets {
			if q == s.Quality {
				cur = i
				break
			}
		}
		nextQ := qualityPresets[(cur+delta+len(qualityPresets))%len(qualityPresets)]
		lbl := "Máxima (best)"
		if nextQ > 0 {
			lbl = fmt.Sprintf("%dp", nextQ)
		}
		return s.applyPlaybackProfile(nextQ, false, lbl)

	case 3: // Default playback mode (window, audio, pip, fs)
		modes := []string{"window", "audio", "pip", "fs"}
		curMode := s.DefaultMode
		if s.AudioOnly {
			curMode = "audio"
		}
		cur := 0
		for i, m := range modes {
			if m == curMode {
				cur = i
				break
			}
		}
		nextMode := modes[(cur+delta+len(modes))%len(modes)]
		s.DefaultMode = nextMode
		audioOnly := nextMode == "audio"
		_, cmd := s.applyPlaybackProfile(s.Quality, audioOnly, "")
		return s, tea.Batch(cmd, s.saveConfigCmd("YT_DEFAULT_MODE", nextMode))

	case 4: // Initial volume (5..100, step 5)
		old := s.AudioVolume
		step := 5
		if delta < 0 {
			step = -5
		}
		nextVol := old + step
		if nextVol > 100 {
			nextVol = 100
		} else if nextVol < 5 {
			nextVol = 5
		}
		s.AudioVolume = nextVol
		var cmds []tea.Cmd
		cmds = append(cmds, s.saveConfigCmd("YT_AUDIO_VOLUME", strconv.Itoa(nextVol)))
		if nextVol != old && s.Deps.Engine != nil && s.PlayerStatus.IsRunning {
			diff := float64(nextVol - old)
			cmds = append(cmds, s.engineCmd(func(ctx context.Context) error {
				return s.Deps.Engine.AddVolume(ctx, diff)
			}, ""))
		}
		cmds = append(cmds, s.toastCmd(fmt.Sprintf("🔊 Volumen: %d%%", nextVol)))
		return s, tea.Batch(cmds...)

	case 5: // Autoplay
		return s.ToggleAutoplay()

	case 6: // SponsorBlock
		return s.ToggleSponsorBlock()

	case 7: // Hide Shorts
		s.HideShorts = !s.HideShorts
		return s, tea.Batch(s.boolToast("Ocultar Shorts", s.HideShorts), s.saveBoolConfigCmd("YT_HIDE_SHORTS", s.HideShorts))

	case 8: // Interleave
		s.Interleave = !s.Interleave
		return s, tea.Batch(s.boolToast("Mezclar canales", s.Interleave), s.saveBoolConfigCmd("YT_INTERLEAVE", s.Interleave))

	case 9: // Cookies
		if s.CookiesMode == "off" {
			s.CookiesMode = "auto"
		} else {
			s.CookiesMode = "off"
		}
		return s, tea.Batch(s.toastCmd("🍪 Cookies: "+s.CookiesMode), s.saveConfigCmd("YT_COOKIES", s.CookiesMode))
	}
	return s, nil
}

func (s *State) boolToast(label string, enabled bool) tea.Cmd {
	state := "OFF"
	if enabled {
		state = "ON"
	}
	return s.toastCmd(fmt.Sprintf("%s: %s", label, state))
}

func (s *State) saveThemeCmd(name string) tea.Cmd {
	if s.Deps.OnThemeChange == nil {
		return nil
	}
	fn := s.Deps.OnThemeChange
	return func() tea.Msg {
		if err := fn(name); err != nil {
			return ActionResultMsg{Err: err}
		}
		return nil
	}
}

func (s *State) saveConfigCmd(key, val string) tea.Cmd {
	if s.Deps.OnConfigChange == nil {
		return nil
	}
	fn := s.Deps.OnConfigChange
	return func() tea.Msg {
		if err := fn(key, val); err != nil {
			return ActionResultMsg{Err: err}
		}
		return nil
	}
}

func (s *State) saveBoolConfigCmd(key string, enabled bool) tea.Cmd {
	val := "0"
	if enabled {
		val = "1"
	}
	return s.saveConfigCmd(key, val)
}
