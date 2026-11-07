# lib/core.sh — generic helpers, platform detection, paths, i18n & notifications
# Shellcheck source=/dev/null

have() { command -v "$1" >/dev/null 2>&1; }

die() { printf 'yt: %s\n' "$*" >&2; exit 1; }

find_helper() { # name -> path (next to yt first, then PATH)
    if [ -x "$BIN_DIR/$1" ]; then printf '%s' "$BIN_DIR/$1"
    elif have "$1"; then command -v "$1"
    fi
}

file_mtime() { stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || echo 0; }
file_age()   { echo $(( $(date +%s) - $(file_mtime "$1") )); }
is_fresh()   { [ -s "$1" ] && [ "$(file_age "$1")" -lt "$2" ]; }

valid_id() { [[ ${1:-} =~ ^[A-Za-z0-9_-]{5,}$ ]]; }

sanitize() { local s=$1; s=${s//$'\t'/ }; s=${s//$'\n'/ }; s=${s//$'\r'/ }; printf '%s' "$s"; }

version_ge() { # $1 >= $2 (portable dotted-version comparison, no sort -V)
    local IFS=. i
    local -a a=($1) b=($2)
    for ((i = 0; i < ${#b[@]}; i++)); do
        local va=${a[i]:-0} vb=${b[i]:-0}
        va=${va%%[!0-9]*}; vb=${vb%%[!0-9]*}
        : "${va:=0}" "${vb:=0}"
        ((10#$va > 10#$vb)) && return 0
        ((10#$va < 10#$vb)) && return 1
    done
    return 0
}

log_err() {
    [ -n "${STATE_DIR:-}" ] && [ -d "$STATE_DIR" ] || return 0
    printf '%s [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "${1:-err}" "${2:-}" >> "$STATE_DIR/yt.log" 2>/dev/null || true
}

classify_err() { # stderr text -> bot_check | rate_limit | network_err | "" (also stored in $YT_RUNDIR/last_err)
    local text=${1:-} cls=""
    case $text in
        *"Sign in to confirm"*)                                    cls=bot_check ;;
        *429*|*"Too Many Requests"*)                               cls=rate_limit ;;
        *"Unable to download webpage"*|*"Network is unreachable"*) cls=network_err ;;
    esac
    if [ -n "${YT_RUNDIR:-}" ] && [ -d "$YT_RUNDIR" ]; then
        if [ -n "$cls" ]; then printf '%s\n' "$cls" > "$YT_RUNDIR/last_err" 2>/dev/null || true
        else rm -f "$YT_RUNDIR/last_err"
        fi
    fi
    printf '%s' "$cls"
}

fzf_version() { fzf --version 2>/dev/null | awk '{print $1; exit}'; }

is_kitty_term() {
    [ -n "${KITTY_WINDOW_ID:-}" ] || [ "${TERM:-}" = "xterm-kitty" ] || [ "${TERM_PROGRAM:-}" = "ghostty" ]
}

kitty_clear() {
    if is_kitty_term && [ -t 1 ] && [ -c /dev/tty ] && { : >/dev/tty; } 2>/dev/null; then
        if have kitten; then kitten icat --clear --silent </dev/tty >/dev/tty 2>/dev/null || true
        elif have kitty; then kitty +kitten icat --clear --silent </dev/tty >/dev/tty 2>/dev/null || true
        fi
        printf '\033_Ga=d,d=a\033\\' >/dev/tty 2>/dev/null || true
    fi
}

resolve_lang() {
    case "$YT_LANG" in
        es|en) ;;
        *) case "${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}" in es*) YT_LANG=es ;; *) YT_LANG=en ;; esac ;;
    esac
}

t() {
    if [ "$YT_LANG" = es ]; then
        case $1 in
            tab_foryou) echo "para ti" ;;          tab_subs) echo "suscripciones" ;;
            tab_music) echo "música" ;;            tab_later) echo "guardados" ;;
            tab_history) echo "historial" ;;       tab_search) echo "búsqueda" ;;
            tab_settings) echo "ajustes" ;;
            header1) echo "⏎ reproducir · alt-m mix · ^p mini · ^f pantalla · ^a audio · ^w guardar · ^y copiar · ^d bajar · ^s buscar · alt-1..5 tabs" ;;
            header2) echo "" ;;
            empty_feed) echo "sin resultados — ¿sesión iniciada en YouTube en tu navegador? prueba: yt --doctor" ;;
            empty_music) echo "no se encontraron canciones — inicia sesión para ver tus me gusta de YouTube Music" ;;
            empty_later) echo "no hay videos guardados (^w sobre un video para guardarlo)" ;;
            empty_history) echo "el historial está vacío" ;;
            empty_search) echo "sin resultados (escribe y pulsa ^s para buscar en YouTube)" ;;
            syncing) echo "sincronizando…" ;;  ghost) echo "filtrar..." ;;
            n_later_add) echo "★ Guardado" ;;     n_later_del) echo "✗ Quitado de guardados" ;;
            n_dl_start) echo "📥 Descargando" ;;   n_dl_done) echo "✅ Descargado" ;;
            n_dl_fail) echo "⚠ Falló la descarga" ;;
            n_fallback) echo "↻ Reintentando por tubería" ;;
            menu_foryou) echo "para ti" ;;         menu_music) echo "música" ;;
            menu_subs) echo "suscripciones" ;;     menu_later) echo "ver después" ;;
            menu_history) echo "historial" ;;      menu_settings) echo "ajustes" ;;
            menu_quit) echo "salir" ;;             menu_ghost) echo "buscar en youtube..." ;;
            settings_title) echo "ajustes" ;;
            settings_quality) echo "Calidad de video" ;;   settings_music) echo "Modo música" ;;
            settings_format) echo "Formato descarga" ;;    settings_volume) echo "Volumen audio" ;;
            settings_preview) echo "Miniaturas preview" ;; settings_thumbq) echo "Calidad miniaturas" ;;
            settings_cookies) echo "Refrescar cookies ahora" ;; settings_cache) echo "Limpiar caché y miniaturas" ;;
            settings_back) echo "Volver" ;;
            settings_music_hint) echo "bg=fondo, term=interactivo" ;;
            settings_fmt_mp3) echo "Solo audio MP3" ;;     settings_fmt_mp4) echo "Video MP4" ;;
            settings_quality_prompt) echo "Resolución › " ;;
            settings_music_prompt) echo "Modo música › " ;;
            settings_download_prompt) echo "Descarga › " ;;
            settings_volume_prompt) echo "Volumen % › " ;;
            settings_preview_prompt) echo "Preview › " ;;
            settings_thumbq_prompt) echo "Calidad miniaturas › " ;;
            settings_quality_max) echo "0 (máxima)" ;;
            settings_music_bg_opt) echo "bg (en segundo plano / desacoplado)" ;;
            settings_music_term_opt) echo "term (en terminal interactiva con teclas mpv)" ;;
            settings_dl_video_opt) echo "0 (video mp4 completo)" ;;
            settings_dl_audio_opt) echo "1 (solo audio mp3)" ;;
            settings_thumb_mq_opt) echo "mq (rápido 16:9)" ;;
            settings_thumb_hq_opt) echo "hq (alta calidad)" ;;
            settings_thumb_max_opt) echo "max (resolución máxima)" ;;
            n_refreshing) echo "Refrescando cookies..." ;;
            n_cookies_synced) echo "✅ Cookies sincronizadas" ;;
            n_cookies_none) echo "⚠ No se detectó sesión" ;;
            n_cache_cleared) echo "Caché limpiada" ;;
            empty_bot_check) echo "YouTube pidió iniciar sesión para confirmar que no eres un bot — inicia sesión en tu navegador y prueba: yt --doctor" ;;
            empty_rate_limit) echo "YouTube limitó las solicitudes (429) — espera unos minutos e intenta de nuevo" ;;
            empty_network_err) echo "sin conexión de red — revisa tu internet e intenta de nuevo" ;;
        esac
    else
        case $1 in
            tab_foryou) echo "for you" ;;          tab_subs) echo "subscriptions" ;;
            tab_music) echo "music" ;;             tab_later) echo "saved" ;;
            tab_history) echo "history" ;;         tab_search) echo "search" ;;
            tab_settings) echo "settings" ;;
            header1) echo "⏎ play · alt-m mix · ^p mini · ^f fullscreen · ^a audio · ^w save · ^y copy · ^d download · ^s search · alt-1..5 tabs" ;;
            header2) echo "" ;;
            empty_feed) echo "no results — are you signed in to YouTube in your browser? try: yt --doctor" ;;
            empty_music) echo "no tracks found — sign in to see your Liked Music from YouTube Music" ;;
            empty_later) echo "nothing saved yet (^w on a video saves it)" ;;
            empty_history) echo "history is empty" ;;
            empty_search) echo "no results (type and press ^s to search YouTube)" ;;
            syncing) echo "syncing…" ;;  ghost) echo "filter..." ;;
            n_later_add) echo "★ Saved" ;;         n_later_del) echo "✗ Removed from saved" ;;
            n_dl_start) echo "📥 Downloading" ;;    n_dl_done) echo "✅ Downloaded" ;;
            n_dl_fail) echo "⚠ Download failed" ;;
            n_fallback) echo "↻ Retrying via pipe" ;;
            menu_foryou) echo "for you" ;;         menu_music) echo "music" ;;
            menu_subs) echo "subscriptions" ;;     menu_later) echo "watch later" ;;
            menu_history) echo "history" ;;        menu_settings) echo "settings" ;;
            menu_quit) echo "quit" ;;              menu_ghost) echo "search youtube..." ;;
            settings_title) echo "settings" ;;
            settings_quality) echo "Video quality" ;;      settings_music) echo "Music mode" ;;
            settings_format) echo "Download format" ;;     settings_volume) echo "Audio volume" ;;
            settings_preview) echo "Preview thumbnails" ;; settings_thumbq) echo "Thumbnail quality" ;;
            settings_cookies) echo "Refresh cookies now" ;; settings_cache) echo "Clear cache and thumbnails" ;;
            settings_back) echo "Back" ;;
            settings_music_hint) echo "bg=background, term=interactive" ;;
            settings_fmt_mp3) echo "Audio only MP3" ;;     settings_fmt_mp4) echo "MP4 video" ;;
            settings_quality_prompt) echo "Resolution › " ;;
            settings_music_prompt) echo "Music mode › " ;;
            settings_download_prompt) echo "Download › " ;;
            settings_volume_prompt) echo "Volume % › " ;;
            settings_preview_prompt) echo "Preview › " ;;
            settings_thumbq_prompt) echo "Thumbnail quality › " ;;
            settings_quality_max) echo "0 (max)" ;;
            settings_music_bg_opt) echo "bg (background / detached)" ;;
            settings_music_term_opt) echo "term (interactive terminal with mpv keys)" ;;
            settings_dl_video_opt) echo "0 (full mp4 video)" ;;
            settings_dl_audio_opt) echo "1 (audio only mp3)" ;;
            settings_thumb_mq_opt) echo "mq (fast 16:9)" ;;
            settings_thumb_hq_opt) echo "hq (high quality)" ;;
            settings_thumb_max_opt) echo "max (maximum resolution)" ;;
            n_refreshing) echo "Refreshing cookies..." ;;
            n_cookies_synced) echo "✅ Cookies synced" ;;
            n_cookies_none) echo "⚠ No session detected" ;;
            n_cache_cleared) echo "Cache cleared" ;;
            empty_bot_check) echo "YouTube asked you to sign in to confirm you are not a bot — sign in to your browser and try: yt --doctor" ;;
            empty_rate_limit) echo "YouTube rate-limited the request (429) — wait a few minutes and try again" ;;
            empty_network_err) echo "network unreachable — check your connection and try again" ;;
        esac
    fi
}

tab_label() { t "tab_$1"; }

notify() {
    [ "${YT_NOTIFY:-1}" = 1 ] || return 0
    if have notify-send; then
        notify-send --app-name=yt --urgency=low yt "$1" >/dev/null 2>&1 || true
    elif [ "$OS" = Darwin ] && have osascript; then
        osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "yt"' \
            -e 'end run' "$1" >/dev/null 2>&1 || true
    fi
    return 0
}

copy_text() {
    local s=$1
    if   [ -n "${WAYLAND_DISPLAY:-}" ] && have wl-copy; then printf '%s' "$s" | wl-copy >/dev/null 2>&1
    elif have xclip;   then printf '%s' "$s" | xclip -selection clipboard >/dev/null 2>&1
    elif have xsel;    then printf '%s' "$s" | xsel --clipboard --input >/dev/null 2>&1
    elif have pbcopy;  then printf '%s' "$s" | pbcopy
    elif have clip.exe; then printf '%s' "$s" | clip.exe
    else
        printf '\033]52;c;%s\a' "$(printf '%s' "$s" | base64 | tr -d '\n')" > /dev/tty 2>/dev/null
    fi
}

open_url() {
    local cmd="${YT_BROWSER_CMD:-${BROWSER:-}}"
    if [ -z "$cmd" ] || ! have "$cmd"; then
        if have xdg-open; then cmd=xdg-open; elif have open; then cmd=open; else return 0; fi
    fi
    nohup "$cmd" "$1" >/dev/null 2>&1 &
    disown 2>/dev/null || true
}

canonical_url() { # id url -> a safe https URL (never something that starts with "-")
    local raw_id=$1 raw_url=$2
    if [[ $raw_id == RD* ]]; then
        local stripped="${raw_id#RD}"
        printf 'https://www.youtube.com/watch?v=%s&list=%s' "$stripped" "$raw_id"
        return 0
    fi
    case "$raw_url" in
        *"list=RD"*'&'*v=*)
            printf '%s' "$raw_url" ;;
        *v=*'&'*"list=RD"*)
            printf '%s' "$raw_url" ;;
        *"playlist?list=RD"*)
            local p_id="${raw_url##*list=RD}"
            p_id="${p_id%%&*}"
            printf 'https://www.youtube.com/watch?v=%s&list=RD%s' "$p_id" "$p_id" ;;
        https://*)
            printf '%s' "$raw_url" ;;
        *)
            printf 'https://www.youtube.com/watch?v=%s' "$raw_id" ;;
    esac
}
