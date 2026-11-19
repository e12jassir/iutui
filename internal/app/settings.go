package app

import (
	"context"
	"fmt"
	"math"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/iutui/internal/tui/theme"
)

// Interactive setting indices in the Settings modal.
const (
	SettingIdxTheme         = 0
	SettingIdxPreview       = 1
	SettingIdxThumbQuality  = 2
	SettingIdxQuality       = 3
	SettingIdxDefaultMode   = 4
	SettingIdxVolume        = 5
	SettingIdxSpeed         = 6
	SettingIdxAutoplay      = 7
	SettingIdxSponsorBlock  = 8
	SettingIdxHideShorts    = 9
	SettingIdxInterleave    = 10
	SettingIdxFeedSize      = 11
	SettingIdxSearchSize    = 12
	SettingIdxCacheTTL      = 13
	SettingIdxDownloadAudio = 14
	SettingIdxCookies       = 15
	SettingsRowCount        = 16
)

var (
	speedPresets      = []float64{0.5, 0.75, 1.0, 1.25, 1.5, 1.75, 2.0}
	feedSizePresets   = []int{30, 50, 75, 100, 150}
	searchSizePresets = []int{20, 30, 40, 60, 80}
	cacheTTLPresets   = []int{300, 900, 1800, 3600, 0}
)

// SettingRow describes a single displayable row in the interactive Settings studio.
type SettingRow struct {
	Index    int
	Section  string
	Label    string
	Value    string
	Desc     string
	Shortcut string
	IsBool   bool
	Enabled  bool
}

// SettingsDisplayLine represents one line in the scrollable Settings modal body.
type SettingsDisplayLine struct {
	IsSpacer   bool
	IsSection  bool
	Section    string
	SettingIdx int // -1 for spacer/section, 0..15 for setting rows
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

	thumbQualVal := "max (Máxima HD)"
	switch normalizeThumbQuality(s.ThumbQuality) {
	case "hq":
		thumbQualVal = "hq (Alta 480p)"
	case "mq":
		thumbQualVal = "mq (Ligera)"
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

	speed := s.PlayerStatus.Speed
	if speed <= 0 {
		speed = 1.0
	}
	speedVal := strconv.FormatFloat(clampSpeed(speed), 'f', -1, 64) + "x"

	feedSize := s.FeedSize
	if feedSize <= 0 {
		feedSize = 100
	}

	searchSize := s.SearchSize
	if searchSize <= 0 {
		searchSize = 40
	}

	cookiesVal := "auto (Zen / Firefox)"
	if s.CookiesMode == "off" {
		cookiesVal = "off (Solo invitado RSS)"
	}

	return []SettingRow{
		{
			Index:    SettingIdxTheme,
			Section:  "APARIENCIA & TERMINAL",
			Label:    "Tema interfaz",
			Value:    themeVal,
			Desc:     "Paleta visual de colores y bordes en toda la TUI",
			Shortcut: "t",
		},
		{
			Index:   SettingIdxPreview,
			Section: "APARIENCIA & TERMINAL",
			Label:   "Miniaturas",
			Value:   previewVal,
			Desc:    "Backend gráfico para portadas (Kitty/Ghostty, Chafa o solo texto)",
		},
		{
			Index:   SettingIdxThumbQuality,
			Section: "APARIENCIA & TERMINAL",
			Label:   "Calidad miniaturas",
			Value:   thumbQualVal,
			Desc:    "Resolución descargada para carátulas (max, hq, mq)",
		},
		{
			Index:    SettingIdxQuality,
			Section:  "REPRODUCCIÓN & CONSUMO",
			Label:    "Resolución máxima",
			Value:    qualVal,
			Desc:     "Tope de resolución al reproducir y precargar streams en mpv",
			Shortcut: "f/c",
		},
		{
			Index:    SettingIdxDefaultMode,
			Section:  "REPRODUCCIÓN & CONSUMO",
			Label:    "Modo reproducción",
			Value:    modeVal,
			Desc:     "Ventana normal, solo audio sin video, PiP flotante o pantalla completa",
			Shortcut: "x",
		},
		{
			Index:    SettingIdxVolume,
			Section:  "REPRODUCCIÓN & CONSUMO",
			Label:    "Volumen mpv",
			Value:    fmt.Sprintf("%d%%", s.AudioVolume),
			Desc:     "Volumen inicial y en vivo de mpv (5% a 100%)",
			Shortcut: "-/+",
		},
		{
			Index:    SettingIdxSpeed,
			Section:  "REPRODUCCIÓN & CONSUMO",
			Label:    "Velocidad mpv",
			Value:    speedVal,
			Desc:     "Velocidad de reproducción aplicada al instante (0.5x a 2x)",
			Shortcut: "{/}/0",
		},
		{
			Index:    SettingIdxAutoplay,
			Section:  "REPRODUCCIÓN & CONSUMO",
			Label:    "Autoplay [A]:",
			Value:    onOff(s.Autoplay),
			Desc:     "Avanza automáticamente al siguiente video al terminar",
			Shortcut: "A",
			IsBool:   true,
			Enabled:  s.Autoplay,
		},
		{
			Index:    SettingIdxSponsorBlock,
			Section:  "REPRODUCCIÓN & CONSUMO",
			Label:    "SponsorBlock",
			Value:    onOff(s.SponsorBlock),
			Desc:     "Omite patrocinadores e intros automáticamente en mpv",
			Shortcut: "b",
			IsBool:   true,
			Enabled:  s.SponsorBlock,
		},
		{
			Index:   SettingIdxHideShorts,
			Section: "CATÁLOGO & FILTROS",
			Label:   "Ocultar Shorts",
			Value:   onOff(s.HideShorts),
			Desc:    "Filtra videos verticales cortos (<= 60s o #shorts) de los feeds",
			IsBool:  true,
			Enabled: s.HideShorts,
		},
		{
			Index:   SettingIdxInterleave,
			Section: "CATÁLOGO & FILTROS",
			Label:   "Mezclar canales",
			Value:   onOff(s.Interleave),
			Desc:    "Intercala canales en Inicio, Música y Suscripciones para variar",
			IsBool:  true,
			Enabled: s.Interleave,
		},
		{
			Index:   SettingIdxFeedSize,
			Section: "CATÁLOGO & FILTROS",
			Label:   "Tamaño del feed",
			Value:   fmt.Sprintf("%d videos", feedSize),
			Desc:    "Cantidad máxima de videos cargados por pestaña de catálogo",
		},
		{
			Index:   SettingIdxSearchSize,
			Section: "CATÁLOGO & FILTROS",
			Label:   "Resultados búsqueda",
			Value:   fmt.Sprintf("%d resultados", searchSize),
			Desc:    "Número máximo de resultados por búsqueda ejecutada",
		},
		{
			Index:   SettingIdxCacheTTL,
			Section: "CATÁLOGO & FILTROS",
			Label:   "Caché del catálogo",
			Value:   formatCacheTTLValue(s.CacheTTL),
			Desc:    "Tiempo de vida de caché local en disco para feeds y búsquedas",
		},
		{
			Index:    SettingIdxDownloadAudio,
			Section:  "DESCARGAS & PRIVACIDAD",
			Label:    "Descarga solo audio",
			Value:    onOff(s.DownloadAudio),
			Desc:     "Al pulsar 'd', descarga únicamente pista de audio (MP3/M4A)",
			Shortcut: "d",
			IsBool:   true,
			Enabled:  s.DownloadAudio,
		},
		{
			Index:   SettingIdxCookies,
			Section: "DESCARGAS & PRIVACIDAD",
			Label:   "Cookies navegador",
			Value:   cookiesVal,
			Desc:    "Usa cookies de Zen/Firefox para feed personalizado o modo invitado",
		},
	}
}

func formatCacheTTLValue(ttl int) string {
	switch ttl {
	case 0:
		return "Desactivada (0s)"
	case 300:
		return "5 min (300s)"
	case 900:
		return "15 min (900s)"
	case 1800:
		return "30 min (1800s)"
	case 3600:
		return "1 hora (3600s)"
	default:
		return fmt.Sprintf("%ds", ttl)
	}
}

// SettingsLayout flattens SettingsRows into section headers, spacers, and setting rows.
func SettingsLayout(rows []SettingRow) []SettingsDisplayLine {
	lines := make([]SettingsDisplayLine, 0, len(rows)+8)
	lastSection := ""
	for _, r := range rows {
		if r.Section != lastSection {
			if lastSection != "" {
				lines = append(lines, SettingsDisplayLine{IsSpacer: true, SettingIdx: -1})
			}
			lines = append(lines, SettingsDisplayLine{IsSection: true, Section: r.Section, SettingIdx: -1})
			lastSection = r.Section
		}
		lines = append(lines, SettingsDisplayLine{SettingIdx: r.Index})
	}
	return lines
}

// SettingsModalWindow computes the responsive modal geometry, scroll window, and display lines.
func (s *State) SettingsModalWindow() (leftX, topY, boxW, boxH, scroll, visBodyH int, lines []SettingsDisplayLine) {
	rows := s.SettingsRows()
	lines = SettingsLayout(rows)
	boxW = min(84, max(48, s.Width-6))
	maxModalH := max(12, s.Height-2)
	visBodyH = min(len(lines), max(5, maxModalH-7))
	boxH = visBodyH + 7

	selLine := 0
	for i, l := range lines {
		if l.SettingIdx == s.SettingsIdx {
			selLine = i
			break
		}
	}

	scroll = 0
	if len(lines) > visBodyH {
		if selLine >= visBodyH {
			scroll = selLine - visBodyH + 1
		}
		maxScroll := max(0, len(lines)-visBodyH)
		if scroll > maxScroll {
			scroll = maxScroll
		}
	}

	leftX = max(0, (s.Width-boxW)/2)
	topY = max(0, (s.Height-boxH)/2)
	return leftX, topY, boxW, boxH, scroll, visBodyH, lines
}

// SettingsModalBounds returns the exact screen placement of the Settings modal.
func (s *State) SettingsModalBounds() (leftX, topY, boxW, boxH int) {
	leftX, topY, boxW, boxH, _, _, _ = s.SettingsModalWindow()
	return leftX, topY, boxW, boxH
}

// SettingsRowScreenY returns the exact screen Y row for setting idx when visible in the scroll window.
func (s *State) SettingsRowScreenY(idx int) (y int, visible bool) {
	_, topY, _, _, scroll, visBodyH, lines := s.SettingsModalWindow()
	for linePos, l := range lines {
		if l.SettingIdx == idx {
			if linePos >= scroll && linePos < scroll+visBodyH {
				return topY + 1 + 2 + (linePos - scroll), true
			}
			return 0, false
		}
	}
	return 0, false
}

func (s *State) settingsModalRowAt(x, y int) (idx int, inside bool) {
	leftX, topY, boxW, boxH, scroll, visBodyH, lines := s.SettingsModalWindow()
	if x < leftX || x >= leftX+boxW || y < topY || y >= topY+boxH {
		return -1, false
	}
	bodyStartY := topY + 1 + 2
	rel := y - bodyStartY
	if rel >= 0 && rel < visBodyH {
		linePos := scroll + rel
		if linePos >= 0 && linePos < len(lines) && lines[linePos].SettingIdx >= 0 {
			return lines[linePos].SettingIdx, true
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
	case SettingIdxTheme:
		palettes := theme.Palettes()
		cur := 0
		for i, name := range palettes {
			if name == s.ThemeName {
				cur = i
				break
			}
		}
		n := len(palettes)
		next := palettes[((cur+delta)%n+n)%n]
		s.ThemeName = next
		return s, tea.Batch(s.toastCmd("🎨 Tema: "+next), s.saveThemeCmd(next))

	case SettingIdxPreview:
		modes := []string{"auto", "kitty", "chafa", "none"}
		cur := 0
		for i, m := range modes {
			if m == s.PreviewMode {
				cur = i
				break
			}
		}
		n := len(modes)
		next := modes[((cur+delta)%n+n)%n]
		s.PreviewMode = next
		return s, tea.Batch(s.toastCmd("🖼 Miniaturas: "+next), s.saveConfigCmd("YT_PREVIEW", next))

	case SettingIdxThumbQuality:
		qualities := []string{"max", "hq", "mq"}
		curNorm := normalizeThumbQuality(s.ThumbQuality)
		cur := 0
		for i, q := range qualities {
			if q == curNorm {
				cur = i
				break
			}
		}
		n := len(qualities)
		next := qualities[((cur+delta)%n+n)%n]
		s.ThumbQuality = next
		return s, tea.Batch(s.toastCmd("🖼 Calidad miniaturas: "+next), s.saveConfigCmd("YT_THUMB_QUALITY", next))

	case SettingIdxQuality:
		cur := 0
		for i, q := range qualityPresets {
			if q == s.Quality {
				cur = i
				break
			}
		}
		n := len(qualityPresets)
		nextQ := qualityPresets[((cur+delta)%n+n)%n]
		lbl := "Máxima (best)"
		if nextQ > 0 {
			lbl = fmt.Sprintf("%dp", nextQ)
		}
		return s.applyPlaybackProfile(nextQ, false, lbl)

	case SettingIdxDefaultMode:
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
		n := len(modes)
		nextMode := modes[((cur+delta)%n+n)%n]
		s.DefaultMode = nextMode
		audioOnly := nextMode == "audio"
		if lms, ok := s.Deps.Engine.(launchModeSetter); ok && s.Deps.Engine != nil {
			s.AudioOnly = audioOnly
			var cmds []tea.Cmd
			modeLabel := "Ventana (video)"
			switch nextMode {
			case "audio":
				modeLabel = "Solo Audio (sin ventana)"
			case "pip":
				modeLabel = "PiP (flotante)"
			case "fs":
				modeLabel = "Pantalla completa"
			}
			cmds = append(cmds, s.toastCmd("🎬 Modo: "+modeLabel))
			selVid, hasSel := s.SelectedVideo()
			prefetcher, hasPrefetch := s.Deps.Engine.(streamPrefetcher)
			m := nextMode
			cmds = append(cmds, func() tea.Msg {
				if err := lms.SetLaunchMode(s.appCtx, m); err != nil {
					if hasSel && hasPrefetch {
						prefetcher.Prefetch(selVid)
					}
					return ActionResultMsg{Err: err}
				}
				if hasSel && hasPrefetch {
					prefetcher.Prefetch(selVid)
				}
				return nil
			})
			if s.Deps.OnAudioOnlyChange != nil {
				fn := s.Deps.OnAudioOnlyChange
				ao := audioOnly
				cmds = append(cmds, func() tea.Msg {
					if err := fn(ao); err != nil {
						return ActionResultMsg{Err: err}
					}
					return nil
				})
			}
			cmds = append(cmds, s.saveConfigCmd("YT_DEFAULT_MODE", nextMode))
			return s, tea.Batch(cmds...)
		}
		_, cmd := s.applyPlaybackProfile(s.Quality, audioOnly, "")
		return s, tea.Batch(cmd, s.saveConfigCmd("YT_DEFAULT_MODE", nextMode))

	case SettingIdxVolume:
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
		if s.Deps.Engine != nil {
			if vs, ok := s.Deps.Engine.(volumeSetter); ok {
				v := nextVol
				cmds = append(cmds, s.engineCmd(func(ctx context.Context) error {
					return vs.SetVolume(ctx, v)
				}, ""))
			} else if nextVol != old && s.PlayerStatus.IsRunning {
				diff := float64(nextVol - old)
				cmds = append(cmds, s.engineCmd(func(ctx context.Context) error {
					return s.Deps.Engine.AddVolume(ctx, diff)
				}, ""))
			}
		}
		cmds = append(cmds, s.toastCmd(fmt.Sprintf("🔊 Volumen: %d%%", nextVol)))
		return s, tea.Batch(cmds...)

	case SettingIdxSpeed:
		curSpeed := s.PlayerStatus.Speed
		if curSpeed <= 0 {
			curSpeed = 1.0
		}
		cur := 0
		bestDiff := math.Abs(speedPresets[0] - curSpeed)
		for i, sp := range speedPresets {
			if d := math.Abs(sp - curSpeed); d < bestDiff {
				bestDiff = d
				cur = i
			}
		}
		n := len(speedPresets)
		nextSpeed := speedPresets[((cur+delta)%n+n)%n]
		return s.SetPlaybackSpeed(nextSpeed)

	case SettingIdxAutoplay:
		return s.ToggleAutoplay()

	case SettingIdxSponsorBlock:
		return s.ToggleSponsorBlock()

	case SettingIdxHideShorts:
		s.HideShorts = !s.HideShorts
		if s.View == ViewHome {
			s.SetSelection(s.SelectedIdx)
		}
		return s, tea.Batch(s.boolToast("Ocultar Shorts", s.HideShorts), s.saveBoolConfigCmd("YT_HIDE_SHORTS", s.HideShorts))

	case SettingIdxInterleave:
		s.Interleave = !s.Interleave
		if s.View == ViewHome {
			s.SetSelection(s.SelectedIdx)
		}
		return s, tea.Batch(s.boolToast("Mezclar canales", s.Interleave), s.saveBoolConfigCmd("YT_INTERLEAVE", s.Interleave))

	case SettingIdxFeedSize:
		cur := closestIntPresetIdx(feedSizePresets, s.FeedSize, 3)
		n := len(feedSizePresets)
		next := feedSizePresets[((cur+delta)%n+n)%n]
		s.FeedSize = next
		return s, tea.Batch(
			s.toastCmd(fmt.Sprintf("📺 Tamaño del feed: %d videos", next)),
			s.saveConfigCmd("YT_FEED_SIZE", strconv.Itoa(next)),
		)

	case SettingIdxSearchSize:
		cur := closestIntPresetIdx(searchSizePresets, s.SearchSize, 2)
		n := len(searchSizePresets)
		next := searchSizePresets[((cur+delta)%n+n)%n]
		s.SearchSize = next
		return s, tea.Batch(
			s.toastCmd(fmt.Sprintf("🔍 Resultados búsqueda: %d", next)),
			s.saveConfigCmd("YT_SEARCH_SIZE", strconv.Itoa(next)),
		)

	case SettingIdxCacheTTL:
		cur := 1 // 900s default
		found := false
		for i, ttl := range cacheTTLPresets {
			if ttl == s.CacheTTL {
				cur = i
				found = true
				break
			}
		}
		if !found && s.CacheTTL > 0 {
			cur = closestIntPresetIdx(cacheTTLPresets[:len(cacheTTLPresets)-1], s.CacheTTL, 1)
		}
		n := len(cacheTTLPresets)
		next := cacheTTLPresets[((cur+delta)%n+n)%n]
		s.CacheTTL = next
		return s, tea.Batch(
			s.toastCmd("💾 Caché del catálogo: "+formatCacheTTLValue(next)),
			s.saveConfigCmd("YT_CACHE_TTL", strconv.Itoa(next)),
		)

	case SettingIdxDownloadAudio:
		s.DownloadAudio = !s.DownloadAudio
		return s, tea.Batch(
			s.boolToast("Descarga solo audio", s.DownloadAudio),
			s.saveBoolConfigCmd("YT_DOWNLOAD_AUDIO", s.DownloadAudio),
		)

	case SettingIdxCookies:
		if s.CookiesMode == "off" {
			s.CookiesMode = "auto"
		} else {
			s.CookiesMode = "off"
		}
		return s, tea.Batch(s.toastCmd("🍪 Cookies: "+s.CookiesMode), s.saveConfigCmd("YT_COOKIES", s.CookiesMode))
	}
	return s, nil
}

func closestIntPresetIdx(presets []int, current, fallback int) int {
	for i, p := range presets {
		if p == current {
			return i
		}
	}
	if current <= 0 {
		return fallback
	}
	best := fallback
	bestDiff := math.MaxInt
	for i, p := range presets {
		d := p - current
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			bestDiff = d
			best = i
		}
	}
	return best
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
