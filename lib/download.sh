# lib/download.sh — video/audio download management and observable queue tracking
# Shellcheck source=/dev/null

downloads_dir() { # downloads tracking directory (empty when state is unavailable)
    if [ -n "${DOWNLOADS_DIR:-}" ]; then printf '%s' "$DOWNLOADS_DIR"
    elif [ -n "${STATE_DIR:-}" ]; then printf '%s/downloads' "$STATE_DIR"
    fi
}

download_record_write() { # file id title status pid started_at -> atomic record
    local file=$1 tmp="$1.tmp.$$"
    mkdir -p "${file%/*}"
    if printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$2" "$(sanitize "$3")" "$4" "${5:--}" "$6" "$(date +%s)" > "$tmp"; then
        mv -f "$tmp" "$file" || rm -f "$tmp"
    else
        rm -f "$tmp"
    fi
}

download_record_start() { # id -> record path (sets DOWNLOAD_STARTED)
    local dir
    DOWNLOAD_STARTED=$(date +%s)
    dir="$(downloads_dir)"
    [ -n "$dir" ] || dir="${TMPDIR:-/tmp}/yt-downloads"
    mkdir -p "$dir"
    printf '%s/%s.%s.rec' "$dir" "$1" "$DOWNLOAD_STARTED"
}

download_video() { # id url title
    local dest="${YT_DOWNLOAD_DIR:-${XDG_VIDEOS_DIR:-$HOME/Videos}}" url title=$3
    url=$(canonical_url "$1" "$2")
    mkdir -p "$dest"
    local rec started dlpid
    rec=$(download_record_start "$1")
    started=$DOWNLOAD_STARTED

    download_record_write "$rec" "$1" "$title" downloading "" "$started"
    prepare_cookies
    local ck_tmp="${COOKIE_TEMP:-}"
    COOKIE_TEMP=""
    notify "$(t n_dl_start): $title"
    (
        dlpid=$BASHPID
        download_record_write "$rec" "$1" "$title" downloading "$dlpid" "$started"
        trap '[ -n "$ck_tmp" ] && rm -f "$ck_tmp"' EXIT
        local -a dl_opts=()
        if [ "${YT_DOWNLOAD_AUDIO:-0}" = 1 ]; then
            dest="${YT_DOWNLOAD_DIR:-${XDG_MUSIC_DIR:-$HOME/Music}}"
            mkdir -p "$dest"
            dl_opts=(-x --audio-format mp3 --embed-thumbnail)
        fi
        if ytdlp "${COOKIE_ARGS[@]}" "${dl_opts[@]}" -o "$dest/%(title)s.%(ext)s" -- "$url"; then
            download_record_write "$rec" "$1" "$title" done "$dlpid" "$started"
            notify "$(t n_dl_done): $title"
        else
            download_record_write "$rec" "$1" "$title" failed "$dlpid" "$started"
            log_err "download" "failed for $url"
            notify "$(t n_dl_fail): $title"
        fi
    ) </dev/null >/dev/null 2>&1 &
    disown 2>/dev/null || true
}

queue_status() { # -> formatted list of active, completed and failed downloads
    local dir rec id title status pid started updated t
    local -a recs=()
    dir="$(downloads_dir)"
    if [ -n "$dir" ] && [ -d "$dir" ]; then
        for rec in "$dir"/*.rec; do
            [ -f "$rec" ] && recs+=("$rec")
        done
    fi
    if [ "${#recs[@]}" -eq 0 ]; then
        printf 'no downloads yet\n'
        return 0
    fi
    printf '%-13s %-14s %-17s %s\n' STATUS ID TIME TITLE
    for rec in "${recs[@]}"; do cat "$rec"; done \
        | LC_ALL=C sort -t "$TAB" -k5,5nr \
        | while IFS="$TAB" read -r id title status pid started updated; do
            [ -n "$id" ] || continue
            t=$(fmt_epoch "${started:-0}")
            printf '%-13s %-14s %-17s %s\n' "${status:--}" "$id" "$t" "${title:--}"
        done
    return 0
}
