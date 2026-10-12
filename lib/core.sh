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

version_ge() { # $1 >= $2 (dotted versions)
    [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n 1)" = "$2" ]
}

fzf_version() { fzf --version 2>/dev/null | awk '{print $1; exit}'; }

is_kitty_term() {
    [ -n "${KITTY_WINDOW_ID:-}" ] || [ "${TERM:-}" = "xterm-kitty" ] || [ "${TERM_PROGRAM:-}" = "ghostty" ]
}

kitty_clear() {
    if is_kitty_term; then
        if [ -c /dev/tty ]; then
            if have kitten; then kitten icat --clear --silent </dev/tty >/dev/tty 2>/dev/null || true
            elif have kitty; then kitty +kitten icat --clear --silent </dev/tty >/dev/tty 2>/dev/null || true
            fi
            printf '\033_Ga=d,d=a\033\\' >/dev/tty 2>/dev/null || true
        elif [ -t 1 ]; then
            if have kitten; then kitten icat --clear --silent 2>/dev/null || true
            elif have kitty; then kitty +kitten icat --clear --silent 2>/dev/null || true
            fi
            printf '\033_Ga=d,d=a\033\\' 2>/dev/null || true
        fi
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
            header1) echo "⏎ reproducir · ^p mini · ^f pantalla · ^a audio · ^w guardar · ^y copiar · ^d bajar · ^s buscar · alt-1..5 tabs" ;;
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
        esac
    else
        case $1 in
            tab_foryou) echo "for you" ;;          tab_subs) echo "subscriptions" ;;
            tab_music) echo "music" ;;             tab_later) echo "saved" ;;
            tab_history) echo "history" ;;         tab_search) echo "search" ;;
            tab_settings) echo "settings" ;;
            header1) echo "⏎ play · ^p mini · ^f fullscreen · ^a audio · ^w save · ^y copy · ^d download · ^s search · alt-1..5 tabs" ;;
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
