# lib/player.sh — mpv invocation, fast Android stream extraction and audio background/interactive playback
# Shellcheck source=/dev/null

# IPC helpers live in lib/ipc.sh; load them when player.sh is sourced on its own.
if ! declare -F mpv_send >/dev/null 2>&1 && [ -n "${LIB_DIR:-}" ] && [ -f "$LIB_DIR/ipc.sh" ]; then
    # shellcheck source=./ipc.sh
    . "$LIB_DIR/ipc.sh"
fi

stop_prev_playback() {
    local pid_file="$STATE_DIR/audio.pid"
    if mpv_is_running && mpv_send quit >/dev/null 2>&1; then
        rm -f "$pid_file" "${YT_SOCK:-}"
        return 0
    fi
    if [ -f "$pid_file" ]; then
        local old_pid
        old_pid=$(cat "$pid_file" 2>/dev/null || true)
        if [ -n "$old_pid" ]; then
            kill -TERM -- -"$old_pid" 2>/dev/null || kill -TERM "$old_pid" 2>/dev/null || true
            pkill -P "$old_pid" 2>/dev/null || true
        fi
        rm -f "$pid_file"
    fi
    [ -n "${YT_SOCK:-}" ] && rm -f "$YT_SOCK" 2>/dev/null || true
}

build_format() { # mode [quality]
    local mode=${1:-window} q=${2:-${YT_QUALITY:-1080}}
    if [ "$mode" = "audio" ]; then
        printf 'ba/b'
    elif [ "$q" = 0 ] || [ -z "$q" ]; then
        printf 'bestvideo+bestaudio/best'
    else
        printf 'bestvideo[height<=%s]+bestaudio/best' "$q"
    fi
}

build_mpv_format() { # mode [quality] -> prefers HLS m3u8 streams first to avoid YouTube SABR 403 on direct HTTPS
    local mode=${1:-window} q=${2:-${YT_QUALITY:-1080}}
    if [ "$mode" = "audio" ]; then
        printf 'ba[protocol*=m3u8]/b[height<=360][protocol*=m3u8]/b[protocol*=m3u8]/ba/b'
    elif [ "$q" = 0 ] || [ -z "$q" ]; then
        printf 'bestvideo[protocol*=m3u8]+bestaudio[protocol*=m3u8]/best[protocol*=m3u8]/bestvideo+bestaudio/best'
    else
        printf 'bestvideo[height<=%s][protocol*=m3u8]+bestaudio[protocol*=m3u8]/best[height<=%s][protocol*=m3u8]/bestvideo[height<=%s]+bestaudio/best' "$q" "$q" "$q"
    fi
}

play_video() { # mode id url title channel dur
    local mode=$1 id=$2 title=$4 url vfmt

    # Resolve auto mode dynamically based on the active tab/view
    if [ "$mode" = "auto" ] || [ -z "$mode" ]; then
        local current_view
        current_view=$(cat "$YT_RUNDIR/view" 2>/dev/null || true)
        if [ "$current_view" = "music" ]; then
            mode="audio"
        else
            mode="${YT_DEFAULT_MODE:-window}"
        fi
    fi

    # If the item is a YouTube Mix playlist, route to continuous mix radio playback
    if [[ $id == RD* ]]; then
        play_mix "$id" "$title" "${5:--}"
        return 0
    fi

    url=$(canonical_url "$id" "$3")
    record_history "$id" "$title" "${5:--}" "${6:--}" "$url"

    # Reuse a live mpv session instead of killing it: the IPC socket swaps the
    # current stream in place so the player keeps its window, volume and queue.
    if mpv_is_running; then
        local cur_yf
        cur_yf=$(mpv_property ytdl-format 2>/dev/null || true)
        if [ -z "$cur_yf" ] || [[ "$cur_yf" == *"m3u8"* ]]; then
            local want_vid="auto"
            [ "$mode" = "audio" ] && want_vid="no"
            mpv_send set_property vid "$want_vid" >/dev/null 2>&1 || true
            if mpv_send loadfile "$url" replace >/dev/null 2>&1; then
                mpv_send set_property force-media-title "$title" >/dev/null 2>&1 || true
                mpv_send set_property pause no >/dev/null 2>&1 || true
                notify "▶ $title"
                return 0
            fi
        fi
    fi

    vfmt=$(build_format "$mode" "$YT_QUALITY")
    local mpv_fmt
    mpv_fmt=$(build_mpv_format "$mode" "$YT_QUALITY")

    # ytdl_hook settings travel with every mpv instance so both native direct
    # playback and later `loadfile ... replace` IPC swaps authenticate and
    # resolve the same format without re-extraction.
    prepare_cookies fast
    local ck_tmp="${COOKIE_TEMP:-}"
    COOKIE_TEMP=""

    local -a mo=(
        --hwdec=auto-safe
        --really-quiet
        --force-media-title="$title"
        --cache-pause=no
        --demuxer-readahead-secs=2
        --ytdl-format="$mpv_fmt"
    )
    if [ "${#COOKIE_ARGS[@]}" -ge 2 ] && [ "${COOKIE_ARGS[0]}" = "--cookies" ]; then
        mo+=(--ytdl-raw-options-append="cookies=${COOKIE_ARGS[1]}")
    else
        mo+=(--ytdl-raw-options-append="extractor-args=youtube:player_client=android,web")
    fi
    case $mode in
        pip)   mo+=(--no-terminal --no-fs --ontop --title="Picture-in-Picture" --geometry=30%x30%-20-20 --autofit=28% --force-window=immediate) ;;
        fs)    mo+=(--no-terminal --fs --title="$title" --force-window=immediate) ;;
        audio)
            local thumb_file="$THUMB_DIR/${id}_mqdefault.jpg"
            [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${id}_maxresdefault.jpg"
            mo+=(--no-video --volume="$YT_AUDIO_VOLUME")
            if [ "$YT_MUSIC_MODE" = "term" ]; then
                stop_prev_playback
                # Foreground interactive player in terminal
                mpv "${mo[@]}" "$url"
                [ -n "$ck_tmp" ] && rm -f "$ck_tmp"
                return 0
            else
                mo+=(--no-terminal)
            fi
            ;;
        *)     mo+=(--no-terminal --no-fs --autofit=65%x65% --title="$title" --force-window=immediate) ;;
    esac

    local thumb_file="$THUMB_DIR/${id}_mqdefault.jpg"
    [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${id}_maxresdefault.jpg"
    if [ -s "$thumb_file" ] && [ "${YT_NOTIFY:-1}" = 1 ] && have notify-send; then
        notify-send --app-name=yt -i "$thumb_file" "▶ $title" "${5:-YouTube}" >/dev/null 2>&1 || notify "▶ $title"
    else
        notify "▶ $title"
    fi

    stop_prev_playback

    [ -n "${YT_SOCK:-}" ] && mo+=(--input-ipc-server="$YT_SOCK" --idle=yes)

    # Native direct playback lets mpv's ytdl_hook talk to YouTube itself, so
    # seeking works in both directions. Pipe mode keeps the authenticated
    # yt-dlp stream as the reliability path; a direct attempt that exits with
    # an error (403, region lock, ...) falls back to that same pipe.
    local direct=0
    case ${YT_PLAYBACK:-auto} in
        direct|auto) direct=1 ;;
    esac

    set -m
    (
        trap '[ -n "$ck_tmp" ] && rm -f "$ck_tmp"' EXIT
        if [ "$direct" = 1 ]; then
            mpv "${mo[@]}" --idle=once "$url" </dev/null
            exit $?
        fi
        if [ "$mode" = "audio" ]; then
            ytdlp "${COOKIE_ARGS[@]}" -f "$vfmt" -o - -- "$url" 2>/dev/null | mpv "${mo[@]}" -
        else
            ytdlp "${COOKIE_ARGS[@]}" -f "$vfmt" --remux-video mkv -o - -- "$url" 2>/dev/null | mpv "${mo[@]}" -
        fi
    ) </dev/null >/dev/null 2>&1 &
    local new_pid=$!
    set +m
    echo "$new_pid" > "$STATE_DIR/audio.pid"
    disown "$new_pid" 2>/dev/null || true
}

enqueue_video() { # id url title channel dur
    local id=${1:-} url title=${3:-}
    valid_id "$id" || return 0
    url=$(canonical_url "$id" "${2:--}")

    if ! mpv_is_running; then
        notify "⚠ mpv is not running"
        return 1
    fi
    if mpv_send loadfile "$url" append-play >/dev/null 2>&1; then
        notify "➕ Encolado: $title"
        return 0
    fi
    notify "⚠ Could not enqueue: $title"
    return 1
}

play_mix() { # id title channel
    local id=$1 title=$2 channel=${3:--}
    valid_id "$id" || return 0
    local clean_id="${id#RD}"
    local mix_url="https://www.youtube.com/watch?v=${clean_id}&list=RD${clean_id}"
    record_history "$id" "$title (Mix)" "$channel" "MIX" "$mix_url"

    local thumb_file="$THUMB_DIR/${clean_id}_maxresdefault.jpg"
    [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${clean_id}_hqdefault.jpg"
    [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${clean_id}_mqdefault.jpg"

    if [ -s "$thumb_file" ] && [ "${YT_NOTIFY:-1}" = 1 ] && have notify-send; then
        notify-send --app-name=yt -i "$thumb_file" "📻 Mix Radio: $title" "$channel" >/dev/null 2>&1 || notify "📻 Mix: $title"
    else
        notify "📻 Mix: $title"
    fi

    local -a mo=(
        --hwdec=auto-safe
        --really-quiet
        --force-media-title="Mix: $title"
        --no-video
        --volume="$YT_AUDIO_VOLUME"
        --cache-pause=no
        --demuxer-readahead-secs=2
        --ytdl-format="ba/b"
    )

    prepare_cookies fast
    local ck_tmp="${COOKIE_TEMP:-}"
    COOKIE_TEMP=""
    if [ "${#COOKIE_ARGS[@]}" -ge 2 ] && [ "${COOKIE_ARGS[0]}" = "--cookies" ]; then
        mo+=(--ytdl-raw-options-append="cookies=${COOKIE_ARGS[1]}")
    fi

    stop_prev_playback

    if [ "$YT_MUSIC_MODE" = "term" ]; then
        mpv "${mo[@]}" "$mix_url"
        [ -n "$ck_tmp" ] && rm -f "$ck_tmp"
        return 0
    fi

    [ -n "${YT_SOCK:-}" ] && mo+=(--input-ipc-server="$YT_SOCK" --idle=yes)

    set -m
    (
        trap '[ -n "$ck_tmp" ] && rm -f "$ck_tmp"' EXIT
        mpv "${mo[@]}" --no-terminal "$mix_url"
    ) </dev/null >/dev/null 2>&1 &
    local new_pid=$!
    set +m
    echo "$new_pid" > "$STATE_DIR/audio.pid"
    disown "$new_pid" 2>/dev/null || true
}
