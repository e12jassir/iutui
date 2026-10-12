# lib/player.sh — mpv invocation, fast Android stream extraction and audio background/interactive playback
# Shellcheck source=/dev/null

stop_prev_playback() {
    local pid_file="$STATE_DIR/audio.pid"
    if [ -f "$pid_file" ]; then
        local old_pid
        old_pid=$(cat "$pid_file" 2>/dev/null || true)
        if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
            kill "$old_pid" 2>/dev/null || true
        fi
        rm -f "$pid_file"
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

    if [ "$mode" = "audio" ]; then
        vfmt="ba/b"
    elif [ "$YT_QUALITY" = 0 ]; then
        vfmt="bestvideo[height<=1080]+bestaudio/best"
    else
        vfmt="bestvideo[height<=$YT_QUALITY]+bestaudio/best"
    fi

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

    (
        # Fast path: extract direct HD video + audio streams via yt-dlp
        local raw_urls
        raw_urls=$(ytdlp --no-playlist -g --no-warnings -f "$vfmt" -- "$url" 2>/dev/null || true)
        local v_stream a_stream
        v_stream=$(printf '%s\n' "$raw_urls" | sed -n '1p')
        a_stream=$(printf '%s\n' "$raw_urls" | sed -n '2p')

        if [ -n "$v_stream" ]; then
            if [ -n "$a_stream" ]; then
                exec mpv "${mo[@]}" "$v_stream" --audio-file="$a_stream"
            else
                exec mpv "${mo[@]}" "$v_stream"
            fi
        fi

        # Fallback path if direct stream extraction fails
        prepare_cookies fast
        if [ "$mode" = "audio" ]; then
            ytdlp "${COOKIE_ARGS[@]}" -f "ba/b" -o - -- "$url" 2>/dev/null | exec mpv "${mo[@]}" -
        else
            ytdlp "${COOKIE_ARGS[@]}" -f "bestvideo[height<=$YT_QUALITY]+bestaudio/best" --remux-video mkv -o - -- "$url" 2>/dev/null | exec mpv "${mo[@]}" -
        fi
    ) </dev/null >/dev/null 2>&1 &
    local new_pid=$!
    echo "$new_pid" > "$STATE_DIR/audio.pid"
    disown "$new_pid" 2>/dev/null || true
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
    if [ "${#COOKIE_ARGS[@]}" -ge 2 ] && [ "${COOKIE_ARGS[0]}" = "--cookies" ]; then
        mo+=(--ytdl-raw-options-append="cookies=${COOKIE_ARGS[1]}")
    fi

    stop_prev_playback

    if [ "$YT_MUSIC_MODE" = "term" ]; then
        mpv "${mo[@]}" "$mix_url"
        return 0
    fi

    (
        exec mpv "${mo[@]}" --no-terminal "$mix_url"
    ) </dev/null >/dev/null 2>&1 &
    local new_pid=$!
    echo "$new_pid" > "$STATE_DIR/audio.pid"
    disown "$new_pid" 2>/dev/null || true
}
