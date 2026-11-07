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
        rm -f "$pid_file"
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
    if mpv_is_running && mpv_send loadfile "$url" replace >/dev/null 2>&1; then
        notify "▶ $title"
        return 0
    fi

    vfmt=$(build_format "$mode" "$YT_QUALITY")

    local -a mo=(
        --hwdec=auto-safe
        --really-quiet
        --force-media-title="$title"
        --cache-pause=no
        --demuxer-readahead-secs=2
    )
    case $mode in
        pip)   mo+=(--no-terminal --no-fs --ontop --title="Picture-in-Picture" --geometry=30%x30%-20-20 --autofit=28%) ;;
        fs)    mo+=(--no-terminal --fs --title="$title") ;;
        audio)
            local thumb_file="$THUMB_DIR/${id}_mqdefault.jpg"
            [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${id}_maxresdefault.jpg"
            mo+=(--no-video --volume="$YT_AUDIO_VOLUME")
            if [ "$YT_MUSIC_MODE" = "term" ]; then
                stop_prev_playback
                # Foreground interactive player in terminal
                mpv "${mo[@]}" "$url"
                return 0
            else
                mo+=(--no-terminal)
            fi
            ;;
        *)     mo+=(--no-terminal --no-fs --autofit=65%x65% --title="$title") ;;
    esac

    local thumb_file="$THUMB_DIR/${id}_mqdefault.jpg"
    [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${id}_maxresdefault.jpg"
    if [ -s "$thumb_file" ] && [ "${YT_NOTIFY:-1}" = 1 ] && have notify-send; then
        notify-send --app-name=yt -i "$thumb_file" "▶ $title" "${5:-YouTube}" >/dev/null 2>&1 || notify "▶ $title"
    else
        notify "▶ $title"
    fi

    stop_prev_playback

    prepare_cookies fast
    local ck_tmp="${COOKIE_TEMP:-}"
    COOKIE_TEMP=""

    [ -n "${YT_SOCK:-}" ] && mo+=(--input-ipc-server="$YT_SOCK" --idle=yes)

    set -m
    (
        trap '[ -n "$ck_tmp" ] && rm -f "$ck_tmp"' EXIT
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
