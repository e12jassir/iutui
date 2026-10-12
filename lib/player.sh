# lib/player.sh — mpv invocation, fast Android stream extraction and audio background/interactive playback
# Shellcheck source=/dev/null

play_video() { # mode id url title channel dur
    local mode=$1 id=$2 title=$4 url vfmt
    url=$(canonical_url "$id" "$3")
    record_history "$id" "$title" "${5:--}" "${6:--}" "$url"

    if [ "$mode" = "audio" ]; then
        vfmt="ba/b"
    elif [ "$YT_QUALITY" = 0 ]; then
        vfmt="best[height<=1080]/best"
    else
        vfmt="best[height<=$YT_QUALITY]/best"
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

    (
        # Fast path: extract direct stream URL via Android client (bypasses n-sig & web bot checks, ~1.5s)
        # and start mpv with instant zero-probe buffer (~0.05s)
        local stream_url=""
        stream_url=$(ytdlp -g --no-warnings --extractor-args "youtube:player_client=android" -f "$vfmt" -- "$url" 2>/dev/null | head -n 1 || true)
        if [ -n "$stream_url" ]; then
            mpv "${mo[@]}" "$stream_url" && exit 0
        fi

        # Fallback path if android client or format fails (age restricted, live streams, etc.)
        prepare_cookies fast
        if [ "$mode" = "audio" ]; then
            ytdlp "${COOKIE_ARGS[@]}" -f "ba/b" -o - -- "$url" 2>/dev/null | mpv "${mo[@]}" -
        else
            ytdlp "${COOKIE_ARGS[@]}" -f "bestvideo[height<=$YT_QUALITY]+bestaudio/best" --remux-video mkv -o - -- "$url" 2>/dev/null | mpv "${mo[@]}" -
        fi
    ) </dev/null >/dev/null 2>&1 &
    disown 2>/dev/null || true
}

play_mix() { # id title channel
    local id=$1 title=$2 channel=${3:--}
    valid_id "$id" || return 0
    local mix_url="https://www.youtube.com/watch?v=${id}&list=RD${id}"
    record_history "$id" "$title (Mix)" "$channel" "MIX" "$mix_url"

    local thumb_file="$THUMB_DIR/${id}_maxresdefault.jpg"
    [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${id}_hqdefault.jpg"
    [ -s "$thumb_file" ] || thumb_file="$THUMB_DIR/${id}_mqdefault.jpg"

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

    if [ "$YT_MUSIC_MODE" = "term" ]; then
        mpv "${mo[@]}" "$mix_url"
        return 0
    fi

    (
        prepare_cookies fast
        mpv "${mo[@]}" --no-terminal "${COOKIE_ARGS[@]}" "$mix_url"
    ) </dev/null >/dev/null 2>&1 &
    disown 2>/dev/null || true
}
