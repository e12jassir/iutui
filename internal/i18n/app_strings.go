package i18n

// AppStrings holds user-facing strings for internal/app.
type AppStrings struct {
	// Common / Settings boolean
	On  string
	Off string

	// Settings sections
	SecAppearance string
	SecPlayback   string
	SecCatalog    string
	SecDownloads  string

	// Settings labels
	LblTheme         string
	LblPreview       string
	LblThumbQuality  string
	LblQuality       string
	LblDefaultMode   string
	LblVolume        string
	LblSpeed         string
	LblAutoplay      string
	LblSponsorBlock  string
	LblNotify        string
	LblHideShorts    string
	LblInterleave    string
	LblFeedSize      string
	LblSearchSize    string
	LblCacheTTL      string
	LblDownloadAudio string
	LblCookies       string

	// Settings values
	ValThemeTerminal string
	ValPreviewKitty  string
	ValPreviewChafa  string
	ValPreviewNone   string
	ValPreviewAuto   string
	ValThumbMax      string
	ValThumbHQ       string
	ValThumbMQ       string
	ValQualMax       string
	ValQual360       string
	ValModeWindow    string
	ValModeAudio     string
	ValModePiP       string
	ValModeFS        string
	ValFeedSize      string // {n}
	ValSearchSize    string // {n}
	ValCookiesAuto   string
	ValCookiesOff    string
	ValCacheDisabled string
	ValCache5m       string
	ValCache15m      string
	ValCache30m      string
	ValCache1h       string
	ValQualBestToast string

	// Settings descriptions
	DescTheme         string
	DescPreview       string
	DescThumbQuality  string
	DescQuality       string
	DescDefaultMode   string
	DescVolume        string
	DescSpeed         string
	DescAutoplay      string
	DescSponsorBlock  string
	DescNotify        string
	DescHideShorts    string
	DescInterleave    string
	DescFeedSize      string
	DescSearchSize    string
	DescCacheTTL      string
	DescDownloadAudio string
	DescCookies       string

	// Settings adjustment toasts
	ToastTheme        string // {theme}
	ToastPreview      string // {mode}
	ToastThumbQuality string // {quality}
	ToastMode         string // {mode}
	ToastVolume       string // {vol}
	ToastFeedSize     string // {n}
	ToastSearchSize   string // {n}
	ToastCacheTTL     string // {val}
	ToastCookies      string // {mode}
	ToastBoolFmt      string // {label}, {state}

	// Chips
	ChipAll       string
	ChipLive      string
	ChipSaved     string
	ChipWatched   string
	ChipVideos    string
	ChipUnwatched string

	// Quality modal presets & badges
	QualOptMaxLabel   string
	QualOptMaxDesc    string
	QualOpt1080Label  string
	QualOpt1080Desc   string
	QualOpt720Label   string
	QualOpt720Desc    string
	QualOpt360Label   string
	QualOpt360Desc    string
	QualOptAudioLabel string
	QualOptAudioDesc  string
	BadgeAudioOnly    string
	BadgeQualitySaver string // {q}
	ToastQualityFmt   string // {label}
	ToastAudioModeFmt string // {label}

	// Playback toasts & messages
	ToastAutoplayOn         string
	ToastAutoplayOff        string
	ToastResumePlayback     string
	ToastPausePlayback      string
	ToastOpenMPV            string // {title}
	ToastPlayAudioOnly      string // {title}
	ToastOpenMix            string // {title}
	ToastPlayOffline        string // {title}
	ToastResumingAt         string // {pos}
	ToastNextTrack          string
	ToastPrevTrack          string
	ToastTrackStart         string
	TrackFallbackN          string // {n}
	ToastQueuePlay          string // {title}
	ToastSpeedFmt           string // {speed}
	ToastQueued             string // {count}, {title}
	ToastRemovedFromQueue   string
	ToastQueueCleared       string
	ToastRepeatUnsupported  string
	ToastHotSwapUnavailable string
	ToastSponsorBlockOn     string
	ToastSponsorBlockOff    string
	ToastRepeatOn           string
	ToastRepeatOff          string
	ToastVideoEnabled       string
	ToastAudioOnly          string
	ToastSubtitlesOn        string
	ToastSubtitlesOff       string
	ToastPiPOn              string
	ToastPiPOff             string
	ToastChapterFmt         string // {title}
	ToastVolumeUp           string // {delta}
	ToastVolumeDown         string // {delta}

	// Actions toasts & messages
	ToastLinkCopied            string
	ToastOpeningBrowser        string
	ToastDownloadsUnconfigured string
	ToastDownloading           string // {title}
	ToastLikeUnavailable       string
	ToastNoVisibleChannel      string
	ToastChannelViewUnavail    string
	ToastNoChannelToSub        string
	ToastQuickSubUnavailable   string
	ToastNoChapters            string
	ToastSelectTrackFirst      string
	ToastLyricsSyncReset       string
	ToastLyricsSyncFmt         string // {offset}
	ToastLyricsSourceLRCLib    string
	ToastLyricsSourceYT        string

	// Update toasts & errors
	ErrCatalogUnconfigured string
	ErrLyricsNotFound      string
	ErrLyricsFmt           string // {err}
	ErrLoadTab             string // {tab}, {err}
	ErrSearch              string // {err}
	ErrSave                string // {err}
	ToastSaved             string // {title}
	ToastUnsaved           string
	ErrLike                string // {err}
	ToastLiked             string // {title}
	ToastUnliked           string
	ToastSessionFeed       string
	ToastSessionFound      string
	ErrGeneric             string // {err}
	ErrLiveSearch          string // {err}
	ToastNoLiveNow         string
	ErrDownload            string // {err}
	ToastDownloaded        string // {title}
	ErrToggleVideo         string // {err}
	ErrLoadChannel         string // {err}
	ErrSubscribe           string // {err}
	ToastSubscribed        string // {channel}
	ToastUnsubscribed      string // {channel}
	ErrReadChapters        string // {err}
	ErrRepeat              string // {err}
}

func esAppStrings() AppStrings {
	return AppStrings{
		On:  "ACTIVADO",
		Off: "DESACTIVADO",

		SecAppearance: "APARIENCIA & TERMINAL",
		SecPlayback:   "REPRODUCCIÓN & CONSUMO",
		SecCatalog:    "CATÁLOGO & FILTROS",
		SecDownloads:  "DESCARGAS & PRIVACIDAD",

		LblTheme:         "Tema interfaz",
		LblPreview:       "Miniaturas",
		LblThumbQuality:  "Calidad miniaturas",
		LblQuality:       "Resolución máxima",
		LblDefaultMode:   "Modo reproducción",
		LblVolume:        "Volumen mpv",
		LblSpeed:         "Velocidad mpv",
		LblAutoplay:      "Autoplay",
		LblSponsorBlock:  "SponsorBlock",
		LblNotify:        "Notificaciones",
		LblHideShorts:    "Ocultar Shorts",
		LblInterleave:    "Mezclar canales",
		LblFeedSize:      "Tamaño del feed",
		LblSearchSize:    "Resultados búsqueda",
		LblCacheTTL:      "Caché del catálogo",
		LblDownloadAudio: "Descarga solo audio",
		LblCookies:       "Cookies navegador",

		ValThemeTerminal: "terminal (Nativo)",
		ValPreviewKitty:  "kitty (HD píxeles)",
		ValPreviewChafa:  "chafa (Bloques ANSI nativos)",
		ValPreviewNone:   "none (Solo texto)",
		ValPreviewAuto:   "auto (Kitty HD / Bloques ANSI)",
		ValThumbMax:      "max (Máxima HD)",
		ValThumbHQ:       "hq (Alta 480p)",
		ValThumbMQ:       "mq (Ligera)",
		ValQualMax:       "MAX (4K/Best)",
		ValQual360:       "360p Ahorro",
		ValModeWindow:    "Ventana (video)",
		ValModeAudio:     "Solo Audio (sin ventana)",
		ValModePiP:       "PiP (flotante)",
		ValModeFS:        "Pantalla completa",
		ValFeedSize:      "{n} videos",
		ValSearchSize:    "{n} resultados",
		ValCookiesAuto:   "auto (Zen / Firefox)",
		ValCookiesOff:    "off (Solo invitado RSS)",
		ValCacheDisabled: "Desactivada (0s)",
		ValCache5m:       "5 min (300s)",
		ValCache15m:      "15 min (900s)",
		ValCache30m:      "30 min (1800s)",
		ValCache1h:       "1 hora (3600s)",
		ValQualBestToast: "Máxima (best)",

		DescTheme:         "Paleta visual de colores y bordes en toda la TUI",
		DescPreview:       "Backend gráfico para portadas (Kitty/Ghostty, Chafa o solo texto)",
		DescThumbQuality:  "Resolución descargada para carátulas (max, hq, mq)",
		DescQuality:       "Tope de resolución al reproducir y precargar streams en mpv",
		DescDefaultMode:   "Ventana normal, solo audio sin video, PiP flotante o pantalla completa",
		DescVolume:        "Volumen inicial y en vivo de mpv (5 a 100)",
		DescSpeed:         "Velocidad de reproducción aplicada al instante (0.5x a 2x)",
		DescAutoplay:      "Avanza automáticamente al siguiente video al terminar",
		DescSponsorBlock:  "Omite patrocinadores e intros automáticamente en mpv",
		DescNotify:        "Muestra notificación del sistema al cambiar de pista",
		DescHideShorts:    "Filtra videos verticales cortos (<= 60s o #shorts) de los feeds",
		DescInterleave:    "Intercala canales en Inicio, Música y Suscripciones para variar",
		DescFeedSize:      "Cantidad máxima de videos cargados por pestaña de catálogo",
		DescSearchSize:    "Número máximo de resultados por búsqueda ejecutada",
		DescCacheTTL:      "Tiempo de vida de caché local en disco para feeds y búsquedas",
		DescDownloadAudio: "Al pulsar 'd', descarga únicamente pista de audio (MP3/M4A)",
		DescCookies:       "Usa cookies de Zen/Firefox para feed personalizado o modo invitado",

		ToastTheme:        "🎨 Tema: {theme}",
		ToastPreview:      "🖼 Miniaturas: {mode}",
		ToastThumbQuality: "🖼 Calidad miniaturas: {quality}",
		ToastMode:         "🎬 Modo: {mode}",
		ToastVolume:       "🔊 Volumen: {vol}",
		ToastFeedSize:     "📺 Tamaño del feed: {n} videos",
		ToastSearchSize:   "🔍 Resultados búsqueda: {n}",
		ToastCacheTTL:     "💾 Caché del catálogo: {val}",
		ToastCookies:      "🍪 Cookies: {mode}",
		ToastBoolFmt:      "{label}: {state}",

		ChipAll:       "Todo",
		ChipLive:      "En vivo",
		ChipSaved:     "Guardados",
		ChipWatched:   "Vistos",
		ChipVideos:    "Videos",
		ChipUnwatched: "No vistos",

		QualOptMaxLabel:   "Máxima (4K / Best)",
		QualOptMaxDesc:    "Máxima nitidez disponible (~4-15 GB/h)",
		QualOpt1080Label:  "1080p Full HD",
		QualOpt1080Desc:   "Alta definición (~2.5 GB/h)",
		QualOpt720Label:   "720p HD",
		QualOpt720Desc:    "Equilibrado · recomendado (~1.0 GB/h)",
		QualOpt360Label:   "360p Ahorro",
		QualOpt360Desc:    "Ahorro de datos (~250 MB/h)",
		QualOptAudioLabel: "Solo Audio",
		QualOptAudioDesc:  "Sin ventana de video (~50 MB/h)",
		BadgeAudioOnly:    "Solo Audio",
		BadgeQualitySaver: "{q}p Ahorro",
		ToastQualityFmt:   "🎬 Calidad: {label}",
		ToastAudioModeFmt: "🎧 Modo: {label}",

		ToastAutoplayOn:         "🔁 Autoplay: ACTIVADO",
		ToastAutoplayOff:        "⏹ Autoplay: DESACTIVADO",
		ToastResumePlayback:     "▶ Continuando reproducción",
		ToastPausePlayback:      "⏸ Reproducción en pausa",
		ToastOpenMPV:            "▶ Abriendo en mpv: {title}",
		ToastPlayAudioOnly:      "🎧 Solo audio: {title}",
		ToastOpenMix:            "📻 Abriendo Mix: {title}",
		ToastPlayOffline:        "💾 Sin conexión: {title}",
		ToastResumingAt:         "⏩ Reanudando en {pos}",
		ToastNextTrack:          "⏭ Siguiente",
		ToastPrevTrack:          "⏮ Anterior",
		ToastTrackStart:         "⏮ Inicio de pista",
		TrackFallbackN:          "Pista {n}",
		ToastQueuePlay:          "▶ Cola: {title}",
		ToastSpeedFmt:           "⚡ Velocidad {speed}x",
		ToastQueued:             "En cola ({count}): {title}",
		ToastRemovedFromQueue:   "✕ Quitado de la cola",
		ToastQueueCleared:       "🗑 Cola vaciada",
		ToastRepeatUnsupported:  "Repetir no soportado",
		ToastHotSwapUnavailable: "Cambio de video en caliente no disponible",
		ToastSponsorBlockOn:     "🛡 SponsorBlock: ACTIVADO",
		ToastSponsorBlockOff:    "🛡 SponsorBlock: DESACTIVADO",
		ToastRepeatOn:           "🔁 Repetir: ACTIVADO",
		ToastRepeatOff:          "🔁 Repetir: DESACTIVADO",
		ToastVideoEnabled:       "🎬 Video activado",
		ToastAudioOnly:          "🎧 Solo audio (sin ventana)",
		ToastSubtitlesOn:        "Subtítulos activados (CC)",
		ToastSubtitlesOff:       "Subtítulos desactivados",
		ToastPiPOn:              "Picture-in-Picture activado (siempre arriba)",
		ToastPiPOff:             "Picture-in-Picture desactivado",
		ToastChapterFmt:         "⏩ Capítulo: {title}",
		ToastVolumeUp:           "🔊 Volumen +{delta}",
		ToastVolumeDown:         "🔉 Volumen -{delta}",

		ToastLinkCopied:            "🔗 Enlace copiado",
		ToastOpeningBrowser:        "🌐 Abriendo en navegador",
		ToastDownloadsUnconfigured: "Descargas no configuradas",
		ToastDownloading:           "⬇ Descargando: {title}",
		ToastLikeUnavailable:       "Me gusta no disponible",
		ToastNoVisibleChannel:      "Este video no tiene canal visible",
		ToastChannelViewUnavail:    "Vista de canal no disponible",
		ToastNoChannelToSub:        "Este video no tiene canal para suscribirse",
		ToastQuickSubUnavailable:   "Suscripciones rápidas no disponibles",
		ToastNoChapters:            "Sin capítulos disponibles",
		ToastSelectTrackFirst:      "Selecciona o reproduce un tema primero",
		ToastLyricsSyncReset:       "⏱ Sync letra: 0.0s",
		ToastLyricsSyncFmt:         "⏱ Sync letra: {offset}s",
		ToastLyricsSourceLRCLib:    "📝 Fuente: LRCLib",
		ToastLyricsSourceYT:        "📝 Fuente: YouTube CC",

		ErrCatalogUnconfigured: "catálogo no configurado",
		ErrLyricsNotFound:      "Sin letra encontrada",
		ErrLyricsFmt:           "Error de letra: {err}",
		ErrLoadTab:             "Error cargando {tab}: {err}",
		ErrSearch:              "Error buscando: {err}",
		ErrSave:                "Error al guardar: {err}",
		ToastSaved:             "★ Guardado: {title}",
		ToastUnsaved:           "Quitado de guardados",
		ErrLike:                "Error en Me gusta: {err}",
		ToastLiked:             "♥ Me gusta: {title}",
		ToastUnliked:           "Quitado de Me gusta",
		ToastSessionFeed:       "🍪 Sesión detectada · cargando tu feed...",
		ToastSessionFound:      "🍪 Sesión de navegador detectada",
		ErrGeneric:             "Error: {err}",
		ErrLiveSearch:          "Error buscando directos: {err}",
		ToastNoLiveNow:         "Sin directos en vivo ahora mismo",
		ErrDownload:            "Error descargando: {err}",
		ToastDownloaded:        "✓ Descargado: {title}",
		ErrToggleVideo:         "Error alternando video: {err}",
		ErrLoadChannel:         "Error cargando canal: {err}",
		ErrSubscribe:           "Error de suscripción: {err}",
		ToastSubscribed:        "★ Suscrito: {channel}",
		ToastUnsubscribed:      "Suscripción quitada: {channel}",
		ErrReadChapters:        "Error leyendo capítulos: {err}",
		ErrRepeat:              "Error en repetir: {err}",
	}
}

func enAppStrings() AppStrings {
	return AppStrings{
		On:  "ON",
		Off: "OFF",

		SecAppearance: "APPEARANCE & TERMINAL",
		SecPlayback:   "PLAYBACK & CONSUMPTION",
		SecCatalog:    "CATALOG & FILTERS",
		SecDownloads:  "DOWNLOADS & PRIVACY",

		LblTheme:         "Interface theme",
		LblPreview:       "Thumbnails",
		LblThumbQuality:  "Thumbnail quality",
		LblQuality:       "Max resolution",
		LblDefaultMode:   "Playback mode",
		LblVolume:        "mpv volume",
		LblSpeed:         "mpv speed",
		LblAutoplay:      "Autoplay",
		LblSponsorBlock:  "SponsorBlock",
		LblNotify:        "Notifications",
		LblHideShorts:    "Hide Shorts",
		LblInterleave:    "Mix channels",
		LblFeedSize:      "Feed size",
		LblSearchSize:    "Search results",
		LblCacheTTL:      "Catalog cache",
		LblDownloadAudio: "Audio-only download",
		LblCookies:       "Browser cookies",

		ValThemeTerminal: "terminal (Native)",
		ValPreviewKitty:  "kitty (HD pixels)",
		ValPreviewChafa:  "chafa (Native ANSI blocks)",
		ValPreviewNone:   "none (Text only)",
		ValPreviewAuto:   "auto (Kitty HD / ANSI blocks)",
		ValThumbMax:      "max (Max HD)",
		ValThumbHQ:       "hq (High 480p)",
		ValThumbMQ:       "mq (Light)",
		ValQualMax:       "MAX (4K/Best)",
		ValQual360:       "360p Saver",
		ValModeWindow:    "Window (video)",
		ValModeAudio:     "Audio only (no window)",
		ValModePiP:       "PiP (floating)",
		ValModeFS:        "Fullscreen",
		ValFeedSize:      "{n} videos",
		ValSearchSize:    "{n} results",
		ValCookiesAuto:   "auto (Zen / Firefox)",
		ValCookiesOff:    "off (Guest RSS only)",
		ValCacheDisabled: "Disabled (0s)",
		ValCache5m:       "5 min (300s)",
		ValCache15m:      "15 min (900s)",
		ValCache30m:      "30 min (1800s)",
		ValCache1h:       "1 hour (3600s)",
		ValQualBestToast: "Max (best)",

		DescTheme:         "Visual color palette and borders across the TUI",
		DescPreview:       "Graphics backend for covers (Kitty/Ghostty, Chafa, or text only)",
		DescThumbQuality:  "Downloaded resolution for covers (max, hq, mq)",
		DescQuality:       "Resolution cap when playing and prefetching streams in mpv",
		DescDefaultMode:   "Normal window, audio-only without video, floating PiP, or fullscreen",
		DescVolume:        "Initial and live mpv volume (5 to 100)",
		DescSpeed:         "Playback speed applied immediately (0.5x to 2x)",
		DescAutoplay:      "Automatically advances to the next video when finished",
		DescSponsorBlock:  "Automatically skips sponsors and intros in mpv",
		DescNotify:        "Show desktop notification when track changes",
		DescHideShorts:    "Filters short vertical videos (<= 60s or #shorts) from feeds",
		DescInterleave:    "Interleaves channels in Home, Music, and Subscriptions for variety",
		DescFeedSize:      "Maximum number of videos loaded per catalog tab",
		DescSearchSize:    "Maximum number of results per search query",
		DescCacheTTL:      "Local disk cache lifetime for feeds and searches",
		DescDownloadAudio: "When pressing 'd', downloads audio track only (MP3/M4A)",
		DescCookies:       "Uses Zen/Firefox cookies for personalized feed or guest mode",

		ToastTheme:        "🎨 Theme: {theme}",
		ToastPreview:      "🖼 Thumbnails: {mode}",
		ToastThumbQuality: "🖼 Thumbnail quality: {quality}",
		ToastMode:         "🎬 Mode: {mode}",
		ToastVolume:       "🔊 Volume: {vol}",
		ToastFeedSize:     "📺 Feed size: {n} videos",
		ToastSearchSize:   "🔍 Search results: {n}",
		ToastCacheTTL:     "💾 Catalog cache: {val}",
		ToastCookies:      "🍪 Cookies: {mode}",
		ToastBoolFmt:      "{label}: {state}",

		ChipAll:       "All",
		ChipLive:      "Live",
		ChipSaved:     "Saved",
		ChipWatched:   "Watched",
		ChipVideos:    "Videos",
		ChipUnwatched: "Unwatched",

		QualOptMaxLabel:   "Max (4K / Best)",
		QualOptMaxDesc:    "Highest available sharpness (~4-15 GB/h)",
		QualOpt1080Label:  "1080p Full HD",
		QualOpt1080Desc:   "High definition (~2.5 GB/h)",
		QualOpt720Label:   "720p HD",
		QualOpt720Desc:    "Balanced · recommended (~1.0 GB/h)",
		QualOpt360Label:   "360p Saver",
		QualOpt360Desc:    "Data saver (~250 MB/h)",
		QualOptAudioLabel: "Audio Only",
		QualOptAudioDesc:  "No video window (~50 MB/h)",
		BadgeAudioOnly:    "Audio Only",
		BadgeQualitySaver: "{q}p Saver",
		ToastQualityFmt:   "🎬 Quality: {label}",
		ToastAudioModeFmt: "🎧 Mode: {label}",

		ToastAutoplayOn:         "🔁 Autoplay: ON",
		ToastAutoplayOff:        "⏹ Autoplay: OFF",
		ToastResumePlayback:     "▶ Resuming playback",
		ToastPausePlayback:      "⏸ Playback paused",
		ToastOpenMPV:            "▶ Opening in mpv: {title}",
		ToastPlayAudioOnly:      "🎧 Audio only: {title}",
		ToastOpenMix:            "📻 Opening Mix: {title}",
		ToastPlayOffline:        "💾 Offline: {title}",
		ToastResumingAt:         "⏩ Resuming at {pos}",
		ToastNextTrack:          "⏭ Next",
		ToastPrevTrack:          "⏮ Previous",
		ToastTrackStart:         "⏮ Track start",
		TrackFallbackN:          "Track {n}",
		ToastQueuePlay:          "▶ Queue: {title}",
		ToastSpeedFmt:           "⚡ Speed {speed}x",
		ToastQueued:             "Queued ({count}): {title}",
		ToastRemovedFromQueue:   "✕ Removed from queue",
		ToastQueueCleared:       "🗑 Queue cleared",
		ToastRepeatUnsupported:  "Repeat not supported",
		ToastHotSwapUnavailable: "Hot video switching unavailable",
		ToastSponsorBlockOn:     "🛡 SponsorBlock: ON",
		ToastSponsorBlockOff:    "🛡 SponsorBlock: OFF",
		ToastRepeatOn:           "🔁 Repeat: ON",
		ToastRepeatOff:          "🔁 Repeat: OFF",
		ToastVideoEnabled:       "🎬 Video enabled",
		ToastAudioOnly:          "🎧 Audio only (no window)",
		ToastSubtitlesOn:        "Subtitles enabled (CC)",
		ToastSubtitlesOff:       "Subtitles disabled",
		ToastPiPOn:              "Picture-in-Picture enabled (always on top)",
		ToastPiPOff:             "Picture-in-Picture disabled",
		ToastChapterFmt:         "⏩ Chapter: {title}",
		ToastVolumeUp:           "🔊 Volume +{delta}",
		ToastVolumeDown:         "🔉 Volume -{delta}",

		ToastLinkCopied:            "🔗 Link copied",
		ToastOpeningBrowser:        "🌐 Opening in browser",
		ToastDownloadsUnconfigured: "Downloads not configured",
		ToastDownloading:           "⬇ Downloading: {title}",
		ToastLikeUnavailable:       "Like unavailable",
		ToastNoVisibleChannel:      "This video has no visible channel",
		ToastChannelViewUnavail:    "Channel view unavailable",
		ToastNoChannelToSub:        "This video has no channel to subscribe to",
		ToastQuickSubUnavailable:   "Quick subscriptions unavailable",
		ToastNoChapters:            "No chapters available",
		ToastSelectTrackFirst:      "Select or play a track first",
		ToastLyricsSyncReset:       "⏱ Lyrics sync: 0.0s",
		ToastLyricsSyncFmt:         "⏱ Lyrics sync: {offset}s",
		ToastLyricsSourceLRCLib:    "📝 Source: LRCLib",
		ToastLyricsSourceYT:        "📝 Source: YouTube CC",

		ErrCatalogUnconfigured: "catalog not configured",
		ErrLyricsNotFound:      "No lyrics found",
		ErrLyricsFmt:           "Lyrics error: {err}",
		ErrLoadTab:             "Error loading {tab}: {err}",
		ErrSearch:              "Error searching: {err}",
		ErrSave:                "Error saving: {err}",
		ToastSaved:             "★ Saved: {title}",
		ToastUnsaved:           "Removed from saved",
		ErrLike:                "Error liking: {err}",
		ToastLiked:             "♥ Liked: {title}",
		ToastUnliked:           "Removed from liked",
		ToastSessionFeed:       "🍪 Session detected · loading your feed...",
		ToastSessionFound:      "🍪 Browser session detected",
		ErrGeneric:             "Error: {err}",
		ErrLiveSearch:          "Error searching live streams: {err}",
		ToastNoLiveNow:         "No live streams right now",
		ErrDownload:            "Error downloading: {err}",
		ToastDownloaded:        "✓ Downloaded: {title}",
		ErrToggleVideo:         "Error toggling video: {err}",
		ErrLoadChannel:         "Error loading channel: {err}",
		ErrSubscribe:           "Subscription error: {err}",
		ToastSubscribed:        "★ Subscribed: {channel}",
		ToastUnsubscribed:      "Unsubscribed: {channel}",
		ErrReadChapters:        "Error reading chapters: {err}",
		ErrRepeat:              "Error toggling repeat: {err}",
	}
}
