package i18n

// ViewStrings holds user-facing strings for internal/tui/views and internal/quick.
type ViewStrings struct {
	// Quick selector
	QuickNoMatches string

	// Player bar
	PlayerTitle          string
	PlayerOpening        string
	PlayerNothingPlaying string
	PlayerOpeningInMPV   string
	PlayerSelected       string

	// Cards & grids
	LoadingVideos      string
	LoadingLive        string
	EmptyView          string
	EmptyLive          string
	BadgeLive          string
	BadgePlaying       string
	BadgeSaved         string
	LoadingChannel     string
	EmptyChannel       string
	SearchingQuery     string
	YouTubeResults     string
	NoResultsFor       string
	PressSlashToSearch string
	RelToday           string
	RelHoursAgo        string
	RelYesterday       string
	RelDaysAgo         string
	RelWeeksAgo        string
	RelMonthsAgo       string
	RelYearsAgo        string
	DownloadQueued     string
	ViewsSuffix        string
	WatchingSuffix     string

	// Sidebar & mini-guide tabs
	TabHome           string
	TabMusic          string
	TabSubscriptions  string
	TabHistory        string
	TabWatchLater     string
	TabPlaylists      string
	TabLiked          string
	TabDownloads      string
	ShortTabSubs      string
	ShortTabDownloads string
	ShortTabHistory   string

	// Render & navigation
	SuggestionsTitle     string
	TooSmallTitle        string
	TooSmallCurrent      string
	TooSmallHint         string
	SearchPlaceholder    string
	ChipChannel          string
	SidebarMain          string
	SidebarLibrary       string
	SidebarSubscriptions string
	SidebarNoChannels    string
	SidebarMoreSubs      string
	HeaderSearch         string
	HeaderChannel        string

	// Overlays: Quality
	QualityTitle  string
	QualityFooter string

	// Overlays: Settings
	SettingsHeader     string
	SettingsEnabled    string
	SettingsDisabled   string
	SettingsFooter     string
	SettingsPanelTitle string

	// Overlays: Still Watching
	StillWatchingTitle      string
	StillWatchingLine1      string
	StillWatchingLine2      string
	StillWatchingPaused     string
	StillWatchingContinue   string
	StillWatchingKeepPaused string

	// Overlays: Help
	HelpSecNav       string
	HelpNavMove      string
	HelpNavSide      string
	HelpNavTabs      string
	HelpNavSearch    string
	HelpNavReload    string
	HelpSecLib       string
	HelpLibSave      string
	HelpLibSub       string
	HelpLibQueue     string
	HelpSecPlay      string
	HelpPlayStart    string
	HelpPlayPause    string
	HelpPlaySeek     string
	HelpPlaySpeed    string
	HelpPlayAudio    string
	HelpPlayChapters string
	HelpPlaySync     string
	HelpSecSys       string
	HelpSysAutoplay  string
	HelpSysTheme     string
	HelpSysSettings  string
	HelpSysQuit      string
	HelpHeader       string
	HelpFooter       string
	HelpPanelTitle   string

	// Overlays: Queue
	QueueTitle       string
	QueueEmpty       string
	QueueTrackN      string
	QueueFooter      string
	QueueSideTitle   string
	QueueSideEmpty   string
	QueueSideMix     string
	QueueSideQueued  string
	QueueSidePlaying string
	QueueSideFooter  string
	QueueSidePanel   string

	// Overlays: Chapters
	ChaptersTitle  string
	ChaptersFooter string

	// Overlays: Lyrics
	LyricsSearching   string
	LyricsCloseHint   string
	LyricsUnavailable string
	LyricsPanelTitle  string
}

func esViewStrings() ViewStrings {
	return ViewStrings{
		QuickNoMatches: "  Sin coincidencias",

		PlayerTitle:          "Reproductor",
		PlayerOpening:        "Abriendo...",
		PlayerNothingPlaying: "Sin reproducción activa",
		PlayerOpeningInMPV:   "Abriendo en mpv: {title}",
		PlayerSelected:       "Seleccionado: {title}",

		LoadingVideos:      "Cargando videos...",
		LoadingLive:        "Buscando directos en vivo...",
		EmptyView:          "No hay videos en esta vista. Presiona '/' para buscar o 'r' para recargar.",
		EmptyLive:          "Sin directos en vivo ahora mismo. Pulsa '/' para buscar o '[' para volver.",
		BadgeLive:          "● EN VIVO",
		BadgePlaying:       "󰎈 Sonando",
		BadgeSaved:         "󰥔 Guardado",
		LoadingChannel:     "Cargando canal...",
		EmptyChannel:       "No hay videos disponibles para este canal. Pulsa Esc para volver.",
		SearchingQuery:     "Buscando \"{query}\"...",
		YouTubeResults:     "Resultados de YouTube",
		NoResultsFor:       "Sin resultados para \"{query}\".",
		PressSlashToSearch: "Presiona '/' para buscar videos en YouTube.",
		RelToday:           "hoy",
		RelHoursAgo:        "hace {n} h",
		RelYesterday:       "ayer",
		RelDaysAgo:         "hace {n} d",
		RelWeeksAgo:        "hace {n} sem",
		RelMonthsAgo:       "hace {n} m",
		RelYearsAgo:        "hace {n} a",
		DownloadQueued:     "⬇ En cola...",

		SuggestionsTitle:     "Sugerencias",
		TooSmallTitle:        "Terminal demasiado pequeña",
		TooSmallCurrent:      "Actual: {w}x{h} · Mín: {mw}x{mh}",
		TooSmallHint:         "Amplía la ventana",
		SearchPlaceholder:    "Buscar en YouTube...",
		ChipChannel:          "Canal",
		SidebarMain:          " PRINCIPAL",
		SidebarLibrary:       " BIBLIOTECA",
		SidebarSubscriptions: " SUSCRIPCIONES",
		SidebarNoChannels:    "  (sin canales)",
		SidebarMoreSubs:      "  +{n} más",
		HeaderSearch:         "Búsqueda: {query}",
		HeaderChannel:        "Canal: {title}",

		QualityTitle:  "🎬 Calidad, Ahorro y Solo Audio",
		QualityFooter: "  [↑/↓] Mover · [Enter/Click] Aplicar · [1-5] · [Esc]",

		SettingsHeader:     " ⚙ Configuración · iutui",
		SettingsEnabled:    "● ACTIVADO",
		SettingsDisabled:   "○ DESACTIVADO",
		SettingsFooter:     "~/.config/iutui/config  ·  ↑/↓ Mover · ←/→/Enter Cambiar · [?] Atajos · Esc",
		SettingsPanelTitle: "Configuración",

		StillWatchingTitle:      "⏸ ¿Todavía estás viendo?",
		StillWatchingLine1:      "  La reproducción automática lleva varios videos seguidos",
		StillWatchingLine2:      "  sin actividad reciente y se pausó automáticamente.",
		StillWatchingPaused:     "  En pausa: {track}",
		StillWatchingContinue:   "  [Enter / Space] Continuar viendo",
		StillWatchingKeepPaused: "  [Esc / q]       Mantener en pausa",

		HelpSecNav:       "NAVEGACIÓN & VISTAS",
		HelpNavMove:      "Mover selección (o puntero)",
		HelpNavSide:      "Lateral / Sidebar / Tarjeta",
		HelpNavTabs:      "Pestañas · Cambiar foco",
		HelpNavSearch:    "Buscar · Cambiar filtro",
		HelpNavReload:    "Recargar · Abrir canal",
		HelpSecLib:       "BIBLIOTECA & COLA",
		HelpLibSave:      "Guardar · Me gusta",
		HelpLibSub:       "Suscribir · Añadir a cola",
		HelpLibQueue:     "Ver Cola · Descargar",
		HelpSecPlay:      "REPRODUCCIÓN (MPV)",
		HelpPlayStart:    "Reproducir (auto / audio / video)",
		HelpPlayPause:    "Pausa · Siguiente · Anterior",
		HelpPlaySeek:     "Saltar ±10s · Volumen ±5",
		HelpPlaySpeed:    "Velocidad 0.5..2x · Repetir pista",
		HelpPlayAudio:    "Solo audio · Mix Radio",
		HelpPlayChapters: "Capítulos · Letras · Calidad",
		HelpPlaySync:     "Sync letra ±0.5s · Fuente CC",
		HelpSecSys:       "SISTEMA & AJUSTES",
		HelpSysAutoplay:  "Activar/desactivar Autoplay",
		HelpSysTheme:     "SponsorBlock · Cambiar tema",
		HelpSysSettings:  "Configuración · Esta ayuda",
		HelpSysQuit:      "Volver/Cerrar · Salir",
		HelpHeader:       " ⌨ Atajos de teclado · iutui",
		HelpFooter:       "Pulsa [S] Configuración  ·  Click o [?] / [Esc] para cerrar",
		HelpPanelTitle:   "Atajos de teclado",

		QueueTitle:       "♫ Cola de reproducción ({n})",
		QueueEmpty:       "  Cola vacía — añade pistas con 'q'",
		QueueTrackN:      "Pista {n}",
		QueueFooter:      "  [j/k] Navegar · [Enter] Saltar · [x/d] Quitar · [c] Vaciar · [u/Esc] Cerrar",
		QueueSideTitle:   " 󰲸 Cola · Mix ({n})",
		QueueSideEmpty:   "Cola vacía — añade pistas con 'q'",
		QueueSideMix:     "Mix / Cola",
		QueueSideQueued:  "En cola",
		QueueSidePlaying: "Sonando",
		QueueSideFooter:  "↑/↓ · Click/Enter · x Quitar · c Limpiar · Esc",
		QueueSidePanel:   "Cola",

		ChaptersTitle:  "📑 Capítulos del video",
		ChaptersFooter: "  [j/k] Navegar · [Enter/Click] Saltar · [Esc/o] Cerrar",

		LyricsSearching:   "Buscando letra...",
		LyricsCloseHint:   "[L] o [Esc] para cerrar",
		ViewsSuffix:       "vistas",
		WatchingSuffix:    "viendo",
		TabHome:           "Inicio",
		TabMusic:          "Música",
		TabSubscriptions:  "Suscripciones",
		TabHistory:        "Historial",
		TabWatchLater:     "Ver más tarde",
		TabPlaylists:      "Playlists",
		TabLiked:          "Me gusta",
		TabDownloads:      "Descargas",
		ShortTabSubs:      "Subs",
		ShortTabDownloads: "Desc",
		ShortTabHistory:   "Hist",
		LyricsUnavailable: "Sin letra disponible",
		LyricsPanelTitle:  "♫ Letras",
	}
}

func enViewStrings() ViewStrings {
	return ViewStrings{
		QuickNoMatches: "  No matches",

		PlayerTitle:          "Player",
		PlayerOpening:        "Opening...",
		PlayerNothingPlaying: "Nothing playing",
		PlayerOpeningInMPV:   "Opening in mpv: {title}",
		PlayerSelected:       "Selected: {title}",

		LoadingVideos:      "Loading videos...",
		LoadingLive:        "Searching live streams...",
		EmptyView:          "No videos in this view. Press '/' to search or 'r' to reload.",
		EmptyLive:          "No live streams right now. Press '/' to search or '[' to go back.",
		BadgeLive:          "● LIVE",
		BadgePlaying:       "󰎈 Playing",
		BadgeSaved:         "󰥔 Saved",
		LoadingChannel:     "Loading channel...",
		EmptyChannel:       "No videos available for this channel. Press Esc to go back.",
		SearchingQuery:     "Searching \"{query}\"...",
		YouTubeResults:     "YouTube Results",
		NoResultsFor:       "No results for \"{query}\".",
		PressSlashToSearch: "Press '/' to search videos on YouTube.",
		RelToday:           "today",
		RelHoursAgo:        "{n}h ago",
		RelYesterday:       "yesterday",
		RelDaysAgo:         "{n}d ago",
		RelWeeksAgo:        "{n}w ago",
		RelMonthsAgo:       "{n}mo ago",
		RelYearsAgo:        "{n}y ago",
		DownloadQueued:     "⬇ Queued...",

		SuggestionsTitle:     "Suggestions",
		TooSmallTitle:        "Terminal too small",
		TooSmallCurrent:      "Current: {w}x{h} · Min: {mw}x{mh}",
		TooSmallHint:         "Resize the window",
		SearchPlaceholder:    "Search YouTube...",
		ChipChannel:          "Channel",
		SidebarMain:          " MAIN",
		SidebarLibrary:       " LIBRARY",
		SidebarSubscriptions: " SUBSCRIPTIONS",
		SidebarNoChannels:    "  (no channels)",
		SidebarMoreSubs:      "  +{n} more",
		HeaderSearch:         "Search: {query}",
		HeaderChannel:        "Channel: {title}",

		QualityTitle:  "🎬 Quality, Data Saver & Audio Only",
		QualityFooter: "  [↑/↓] Move · [Enter/Click] Apply · [1-5] · [Esc]",

		SettingsHeader:     " ⚙ Settings · iutui",
		SettingsEnabled:    "● ENABLED",
		SettingsDisabled:   "○ DISABLED",
		SettingsFooter:     "~/.config/iutui/config  ·  ↑/↓ Move · ←/→/Enter Change · [?] Shortcuts · Esc",
		SettingsPanelTitle: "Settings",

		StillWatchingTitle:      "⏸ Still watching?",
		StillWatchingLine1:      "  Autoplay has played several videos in a row",
		StillWatchingLine2:      "  without recent activity and paused automatically.",
		StillWatchingPaused:     "  Paused: {track}",
		StillWatchingContinue:   "  [Enter / Space] Continue watching",
		StillWatchingKeepPaused: "  [Esc / q]       Keep paused",

		HelpSecNav:       "NAVIGATION & VIEWS",
		HelpNavMove:      "Move selection (or pointer)",
		HelpNavSide:      "Side / Sidebar / Card",
		HelpNavTabs:      "Tabs · Switch focus",
		HelpNavSearch:    "Search · Switch filter",
		HelpNavReload:    "Reload · Open channel",
		HelpSecLib:       "LIBRARY & QUEUE",
		HelpLibSave:      "Save · Like",
		HelpLibSub:       "Subscribe · Add to queue",
		HelpLibQueue:     "View Queue · Download",
		HelpSecPlay:      "PLAYBACK (MPV)",
		HelpPlayStart:    "Play (auto / audio / video)",
		HelpPlayPause:    "Pause · Next · Previous",
		HelpPlaySeek:     "Seek ±10s · Volume ±5",
		HelpPlaySpeed:    "Speed 0.5..2x · Loop track",
		HelpPlayAudio:    "Audio only · Mix Radio",
		HelpPlayChapters: "Chapters · Lyrics · Quality",
		HelpPlaySync:     "Lyrics sync ±0.5s · CC source",
		HelpSecSys:       "SYSTEM & SETTINGS",
		HelpSysAutoplay:  "Toggle Autoplay",
		HelpSysTheme:     "SponsorBlock · Switch theme",
		HelpSysSettings:  "Settings · This help",
		HelpSysQuit:      "Back/Close · Quit",
		HelpHeader:       " ⌨ Keyboard Shortcuts · iutui",
		HelpFooter:       "Press [S] Settings  ·  Click or [?] / [Esc] to close",
		HelpPanelTitle:   "Keyboard Shortcuts",

		QueueTitle:       "♫ Playback Queue ({n})",
		QueueEmpty:       "  Queue empty — add tracks with 'q'",
		QueueTrackN:      "Track {n}",
		QueueFooter:      "  [j/k] Navigate · [Enter] Jump · [x/d] Remove · [c] Clear · [u/Esc] Close",
		QueueSideTitle:   " 󰲸 Queue · Mix ({n})",
		QueueSideEmpty:   "Queue empty — add tracks with 'q'",
		QueueSideMix:     "Mix / Queue",
		QueueSideQueued:  "Queued",
		QueueSidePlaying: "Playing",
		QueueSideFooter:  "↑/↓ · Click/Enter · x Remove · c Clear · Esc",
		QueueSidePanel:   "Queue",

		ChaptersTitle:  "📑 Video Chapters",
		ChaptersFooter: "  [j/k] Navigate · [Enter/Click] Jump · [Esc/o] Close",

		LyricsSearching:   "Searching lyrics...",
		LyricsCloseHint:   "[L] or [Esc] to close",
		LyricsUnavailable: "No lyrics available",
		LyricsPanelTitle:  "♫ Lyrics",
		ViewsSuffix:       "views",
		WatchingSuffix:    "watching",
		TabHome:           "Home",
		TabMusic:          "Music",
		TabSubscriptions:  "Subscriptions",
		TabHistory:        "History",
		TabWatchLater:     "Watch later",
		TabPlaylists:      "Playlists",
		TabLiked:          "Liked",
		TabDownloads:      "Downloads",
		ShortTabSubs:      "Subs",
		ShortTabDownloads: "Dls",
		ShortTabHistory:   "Hist",
	}
}
