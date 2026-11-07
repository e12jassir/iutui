# lib/download.sh — video and audio-only download management
# Shellcheck source=/dev/null

download_video() { # id url title
    local dest="${YT_DOWNLOAD_DIR:-${XDG_VIDEOS_DIR:-$HOME/Videos}}" url title=$3
    url=$(canonical_url "$1" "$2")
    mkdir -p "$dest"
    prepare_cookies
    local ck_tmp="${COOKIE_TEMP:-}"
    COOKIE_TEMP=""
    notify "$(t n_dl_start): $title"
    (
        trap '[ -n "$ck_tmp" ] && rm -f "$ck_tmp"' EXIT
        local -a dl_opts=()
        if [ "${YT_DOWNLOAD_AUDIO:-0}" = 1 ]; then
            dest="${YT_DOWNLOAD_DIR:-${XDG_MUSIC_DIR:-$HOME/Music}}"
            mkdir -p "$dest"
            dl_opts=(-x --audio-format mp3 --embed-thumbnail)
        fi
        if ytdlp "${COOKIE_ARGS[@]}" "${dl_opts[@]}" -o "$dest/%(title)s.%(ext)s" -- "$url"; then
            notify "$(t n_dl_done): $title"
        else
            log_err "download" "failed for $url"
            notify "$(t n_dl_fail): $title"
        fi
    ) </dev/null >/dev/null 2>&1 &
    disown 2>/dev/null || true
}
