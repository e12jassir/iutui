# lib/feeds.sh — data fetching, formatting pipeline, caching and history/later tracking
# Shellcheck source=/dev/null

RAW_TEMPLATE="%(id)s${TAB}%(title)s${TAB}%(uploader,channel)s${TAB}%(duration_string)s${TAB}%(url)s${TAB}%(view_count)s${TAB}%(live_status)s"

fetch_raw() { # source count
    prepare_cookies
    ytdlp "${COOKIE_ARGS[@]}" --flat-playlist --no-warnings --ignore-errors \
        --print "$RAW_TEMPLATE" --playlist-items "1:$2" -- "$1" 2>/dev/null </dev/null
}

cache_write() { # file ; stdin -> atomic replace (never stores empty data)
    local tmp="$1.tmp.$$"
    if cat > "$tmp" && [ -s "$tmp" ]; then mv -f "$tmp" "$1"; else rm -f "$tmp"; fi
}

view_cache_file() { printf '%s/feed_%s.tsv' "$CACHE_DIR" "$1"; }

view_raw() { # view mode(auto|cached|refresh) -> raw rows (stale-cache fallback on failure)
    local view=$1 mode=$2 src file data count
    file="$(view_cache_file "$view")"
    count="$YT_FEED_SIZE"
    case $view in
        foryou)   src="https://www.youtube.com/" ;;
        subs)     src="https://www.youtube.com/feed/subscriptions" ;;
        music)    src="https://www.youtube.com/playlist?list=LM"; count=100 ;;
        *) return 0 ;;
    esac
    case $mode in
        cached) [ -s "$file" ] && cat "$file"; return 0 ;;
        auto)   if is_fresh "$file" "$YT_CACHE_TTL"; then cat "$file"; return 0; fi ;;
    esac
    data=$(fetch_raw "$src" "$count")
    if [ -n "$data" ]; then
        printf '%s\n' "$data" | cache_write "$file"
        printf '%s\n' "$data"
    elif [ -s "$file" ]; then
        cat "$file"
    fi
    return 0
}

search_cache_file() { printf '%s/search_%s.tsv' "$CACHE_DIR" "$(printf '%s' "$1" | cksum | cut -d' ' -f1)"; }

search_raw() { # query mode
    local q=$1 mode=$2 file data
    [ -n "$q" ] || return 0
    file="$(search_cache_file "$q")"
    case $mode in
        cached) [ -s "$file" ] && cat "$file"; return 0 ;;
        auto)   if is_fresh "$file" 600; then cat "$file"; return 0; fi ;;
    esac
    data=$(fetch_raw "ytsearch${YT_SEARCH_SIZE}:$q" "$YT_SEARCH_SIZE")
    if [ -n "$data" ]; then
        printf '%s\n' "$data" | cache_write "$file"
        printf '%s\n' "$data"
    elif [ -s "$file" ]; then
        cat "$file"
    fi
    return 0
}

list_width() {
    local c="${FZF_COLUMNS:-${YT_COLS:-}}"
    [ -z "$c" ] && c=$(tput cols 2>/dev/null || echo 120)
    case "$c" in ''|*[!0-9]*) c=120 ;; esac
    [ "$c" -lt 60 ] && c=120
    if [ "$c" -ge 90 ]; then echo $(( c * 50 / 100 - 4 )); else echo $(( c - 4 )); fi
}

format_rows() {
    [ -n "$FMT" ] || die "yt-fmt not found (reinstall: make install)"
    "$FMT" --width "$(list_width)" --history "$HISTORY_FILE" --later "$WATCHLATER_FILE" "$@"
}

empty_row() { # view
    local key
    case $1 in
        later)   key=empty_later ;;
        history) key=empty_history ;;
        search)  key=empty_search ;;
        music)   key=empty_music ;;
        *)       key=empty_feed ;;
    esac
    printf 'none\t-\t-\t-\t-\t-\t  \033[90m%s\033[0m\n' "$(t "$key")"
}

emit_rows() { # view mode query
    local view=$1 mode=$2 query=$3
    local -a o=()
    [ "$YT_HIDE_SHORTS" = 1 ] && o+=(--hide-shorts)
    case $view in
        foryou)  [ "$YT_INTERLEAVE" = 1 ] && o+=(--interleave)
                 view_raw foryou "$mode" | format_rows "${o[@]}" ;;
        subs)    view_raw subs "$mode" | format_rows "${o[@]}" ;;
        music)   view_raw music "$mode" | format_rows "${o[@]}" ;;
        later)   format_rows --stored --reverse < "$WATCHLATER_FILE" ;;
        history) format_rows --stored --reverse < "$HISTORY_FILE" ;;
        search)  [ -n "$query" ] && search_raw "$query" "$mode" | format_rows "${o[@]}" ;;
    esac
    return 0
}

prefetch_thumbs() { # rows-file (deleted when done): warm the first N thumbnails in parallel
    local f=$1 q
    if [ "$YT_PREFETCH" -le 0 ] || [ "$YT_PREVIEW" = none ] || ! have curl; then rm -f "$f"; return 0; fi
    case $YT_THUMB_QUALITY in hq) q=hqdefault ;; max) q=maxresdefault ;; *) q=mqdefault ;; esac
    awk -F'\t' -v n="$YT_PREFETCH" 'NR <= n && $1 ~ /^[A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-]+$/ { print $1 }' "$f" |
        xargs -P 8 -I{} sh -c '
            out="$1/$2_$3.jpg"; [ -s "$out" ] && exit 0
            if curl -sfL --max-time 6 -o "$out.part.$$" "https://i.ytimg.com/vi/$2/$3.jpg" && [ -s "$out.part.$$" ]
            then mv -f "$out.part.$$" "$out"; else rm -f "$out.part.$$"; fi' _ "$THUMB_DIR" {} "$q"
    rm -f "$f"
}

emit_view() { # view mode [query]
    local view=$1 mode=$2 query=${3:-} rows tmp
    rows=$(emit_rows "$view" "$mode" "$query")
    [ -n "$rows" ] || rows=$(empty_row "$view")
    printf '%s\n' "$rows"
    tmp="$YT_RUNDIR/rows.$$"
    if printf '%s\n' "$rows" > "$tmp"; then
        ( prefetch_thumbs "$tmp" ) </dev/null >/dev/null 2>&1 &
        disown 2>/dev/null || true
    fi
}

cmd_list() {
    local mode=auto view="" query="" have_q=0 a
    for a in "$@"; do
        case $a in
            --cached)  mode=cached ;;
            --refresh) mode=refresh ;;
            *) if [ -z "$view" ]; then view=$a; else query=$a; have_q=1; fi ;;
        esac
    done
    if [ -z "$view" ] || [ "$view" = "@current" ]; then
        view=$(cat "$YT_RUNDIR/view" 2>/dev/null)
        [ -n "$view" ] || view=$(home_view)
    else
        printf '%s\n' "$view" > "$YT_RUNDIR/view"
    fi
    if [ "$view" = search ]; then
        if [ "$have_q" = 0 ] || [ "$query" = "@query" ] || [ -z "$query" ]; then
            query=$(cat "$YT_RUNDIR/query" 2>/dev/null)
        else
            printf '%s\n' "$query" > "$YT_RUNDIR/query"
        fi
    fi
    emit_view "$view" "$mode" "$query"
}

record_history() { # id title channel dur url
    printf '%s\t%s\t%s\t%s\t%s\tNA\tNA\t%s\n' "$1" "$(sanitize "$2")" "$(sanitize "$3")" \
        "$(sanitize "$4")" "$5" "$(date +%s)" >> "$HISTORY_FILE"
}

toggle_later() { # id title channel dur url
    local id=$1 tmp="$WATCHLATER_FILE.tmp.$$"
    valid_id "$id" || return 0
    if awk -F'\t' -v id="$id" '$1 == id { f = 1 } END { exit !f }' "$WATCHLATER_FILE"; then
        awk -F'\t' -v id="$id" '$1 != id' "$WATCHLATER_FILE" > "$tmp" && mv -f "$tmp" "$WATCHLATER_FILE" || rm -f "$tmp"
        notify "$(t n_later_del)"
    else
        printf '%s\t%s\t%s\t%s\t%s\tNA\tNA\t%s\n' "$id" "$(sanitize "$2")" "$(sanitize "${3:--}")" \
            "$(sanitize "${4:--}")" "$(canonical_url "$id" "${5:--}")" "$(date +%s)" >> "$WATCHLATER_FILE"
        notify "$(t n_later_add): $2"
    fi
}
