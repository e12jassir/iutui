# lib/feeds.sh — data fetching, formatting pipeline, caching and history/later tracking
# Shellcheck source=/dev/null

RAW_TEMPLATE="%(id)s${TAB}%(title)s${TAB}%(uploader,channel)s${TAB}%(duration_string)s${TAB}%(url)s${TAB}%(view_count)s${TAB}%(live_status)s"

fetch_raw() { # source count
    local out errfile err cls
    prepare_cookies
    errfile="$YT_RUNDIR/fetch_err.$$"
    out=$(ytdlp "${COOKIE_ARGS[@]}" --flat-playlist --no-warnings --ignore-errors \
        --print "$RAW_TEMPLATE" --playlist-items "1:$2" -- "$1" 2>"$errfile" </dev/null)
    err=$(cat "$errfile" 2>/dev/null)
    rm -f "$errfile"
    [ -n "${COOKIE_TEMP:-}" ] && { rm -f "$COOKIE_TEMP"; COOKIE_TEMP=""; }
    if [ -n "$out" ]; then
        classify_err "" >/dev/null
    else
        cls=$(classify_err "$err")
        log_err "fetch" "no data from $1${cls:+ ($cls)}"
    fi
    printf '%s' "$out"
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
    local key err
    err=$(cat "${YT_RUNDIR:-/nonexistent}/last_err" 2>/dev/null)
    if [ -n "$err" ]; then
        key="empty_$err"
    else
        case $1 in
            later)   key=empty_later ;;
            history) key=empty_history ;;
            search)  key=empty_search ;;
            music)   key=empty_music ;;
            *)       key=empty_feed ;;
        esac
    fi
    printf 'none\t-\t-\t-\t-\t-\t  \033[90m%s\033[0m\n' "$(t "$key")"
}

emit_rows() { # view mode query
    local view=$1 mode=$2 query=$3
    local -a o=()
    [ "$YT_HIDE_SHORTS" = 1 ] && o+=(--hide-shorts)
    case $view in
        foryou)  [ "$YT_INTERLEAVE" = 1 ] && o+=(--interleave)
                 view_raw foryou "$mode" | format_rows "${o[@]}" ;;
        subs)
            if subs_rss_active; then
                if [ -s "${CHANNELS_FILE:-/nonexistent}" ]; then
                    subs_raw "$mode" | format_rows "${o[@]}"
                else
                    printf 'none\t-\t-\t-\t-\t-\t  \033[90m%s\033[0m\n' "$(t empty_subs_rss)"
                fi
            else
                view_raw subs "$mode" | format_rows "${o[@]}"
            fi ;;
        music)   view_raw music "$mode" | format_rows --music "${o[@]}" ;;
        later)   format_rows --stored --reverse < "$WATCHLATER_FILE" ;;
        history) format_rows --stored --reverse < "$HISTORY_FILE" ;;
        search)  [ -n "$query" ] && search_raw "$query" "$mode" | format_rows "${o[@]}" ;;
    esac
    return 0
}

prefetch_thumbs() { # rows-file (deleted when done): warm the first N thumbnails in parallel
    local f=$1 q id
    if [ "$YT_PREFETCH" -le 0 ] || [ "$YT_PREVIEW" = none ] || ! have curl; then rm -f "$f"; return 0; fi
    case $YT_THUMB_QUALITY in hq) q=hqdefault ;; max) q=maxresdefault ;; *) q=mqdefault ;; esac
    local -a missing=()
    while IFS= read -r id; do
        [ -n "$id" ] || continue
        if [ -s "$THUMB_DIR/${id}_${q}.jpg" ]; then continue; fi
        if [ "$q" = "maxresdefault" ] && [ -s "$THUMB_DIR/${id}_hqdefault.jpg" ]; then continue; fi
        missing+=("$id")
    done < <(awk -F'\t' -v n="$YT_PREFETCH" 'NR <= n && $1 ~ /^[A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-][A-Za-z0-9_-]+$/ { print $1 }' "$f")
    rm -f "$f"
    [ "${#missing[@]}" -gt 0 ] || return 0
    printf '%s\n' "${missing[@]}" |
        xargs -P 8 -I{} sh -c '
            out="$1/$2_$3.jpg"; [ -s "$out" ] && exit 0
            if curl -sfL --max-time 6 -o "$out.part.$$" "https://i.ytimg.com/vi/$2/$3.jpg" && [ -s "$out.part.$$" ]; then
                sz=$(stat -c%s "$out.part.$$" 2>/dev/null || stat -f%z "$out.part.$$" 2>/dev/null || echo 0)
                if [ "$3" = "maxresdefault" ] && [ "$sz" -lt 3000 ]; then
                    rm -f "$out.part.$$"
                    curl -sfL --max-time 6 -o "$1/$2_hqdefault.jpg" "https://i.ytimg.com/vi/$2/hqdefault.jpg" 2>/dev/null || true
                else
                    mv -f "$out.part.$$" "$out"
                fi
            else
                rm -f "$out.part.$$"
                [ "$3" = "maxresdefault" ] && curl -sfL --max-time 6 -o "$1/$2_hqdefault.jpg" "https://i.ytimg.com/vi/$2/hqdefault.jpg" 2>/dev/null || true
            fi' _ "$THUMB_DIR" {} "$q"
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

# ── guest RSS subscriptions (no cookies, no yt-dlp) ─────────────────────────

subs_rss_active() { # guest mode uses Atom feeds instead of the authenticated subscriptions page
    [ "${YT_COOKIES:-auto}" = off ] && return 0
    ! cookies_possible
}

normalize_channel_ref() { # id | @handle | channel URL -> UC... or @handle (empty when unusable)
    local raw=${1:-}
    raw="${raw#"${raw%%[![:space:]]*}"}"
    raw="${raw%"${raw##*[![:space:]]}"}"
    [ -n "$raw" ] || return 0
    case $raw in
        *"/channel/"*)
            raw="${raw#*/channel/}"; raw="${raw%%[?/#]*}"
            [[ $raw =~ ^UC[A-Za-z0-9_-]{3,}$ ]] && printf '%s' "$raw" ;;
        *"/@"*)
            raw="@${raw#*"/@"}"; raw="${raw%%[?/#]*}"
            [[ $raw =~ ^@[A-Za-z0-9_.-]{2,}$ ]] && printf '%s' "$raw" ;;
        *"/user/"*)
            raw="${raw#*/user/}"; raw="${raw%%[?/#]*}"
            [[ $raw =~ ^[A-Za-z0-9_.-]{2,}$ ]] && printf '%s' "@$raw" ;;
        *"/c/"*)
            raw="${raw#*/c/}"; raw="${raw%%[?/#]*}"
            [[ $raw =~ ^[A-Za-z0-9_.-]{2,}$ ]] && printf '%s' "@$raw" ;;
        http://*|https://*) : ;;
        UC*) [[ $raw =~ ^UC[A-Za-z0-9_-]{3,}$ ]] && printf '%s' "$raw" ;;
        @*)  [[ $raw =~ ^@[A-Za-z0-9_.-]{2,}$ ]] && printf '%s' "$raw" ;;
        *)   [[ $raw =~ ^[A-Za-z0-9_.-]{3,}$ ]] && printf '@%s' "$raw" ;;
    esac
    return 0
}

channel_feed_url() { # stored ref -> Atom feed URL
    case ${1:-} in
        UC*) printf 'https://www.youtube.com/feeds/videos.xml?channel_id=%s' "$1" ;;
        @*)  printf 'https://www.youtube.com/feeds/videos.xml?user=%s' "${1#@}" ;;
        "")  return 0 ;;
        *)   printf 'https://www.youtube.com/feeds/videos.xml?channel_id=%s' "$1" ;;
    esac
}

subscribe_channel() { # id | @handle | URL
    local raw=${1:-} ref
    [ -n "$raw" ] || { printf 'yt: --subscribe requires a channel id, @handle or URL\n' >&2; return 2; }
    ref=$(normalize_channel_ref "$raw")
    [ -n "$ref" ] || { printf 'yt: invalid channel reference: %s\n' "$raw" >&2; return 2; }
    [ -f "$CHANNELS_FILE" ] || : > "$CHANNELS_FILE"
    if awk -F'\t' -v id="$ref" '$1 == id { f = 1 } END { exit !f }' "$CHANNELS_FILE"; then
        printf 'yt: already subscribed: %s\n' "$ref"
        return 0
    fi
    printf '%s\t%s\n' "$ref" "$(sanitize "$ref")" >> "$CHANNELS_FILE"
    printf 'yt: subscribed: %s\n' "$ref"
}

unsubscribe_channel() { # id | @handle | URL
    local raw=${1:-} ref tmp
    [ -n "$raw" ] || { printf 'yt: --unsubscribe requires a channel id\n' >&2; return 2; }
    ref=$(normalize_channel_ref "$raw")
    [ -n "$ref" ] || ref=$raw
    tmp="$CHANNELS_FILE.tmp.$$"
    if [ -s "$CHANNELS_FILE" ] && awk -F'\t' -v id="$ref" '$1 != id' "$CHANNELS_FILE" > "$tmp"; then
        mv -f "$tmp" "$CHANNELS_FILE"
        printf 'yt: unsubscribed: %s\n' "$ref"
    else
        rm -f "$tmp"
        printf 'yt: not subscribed: %s\n' "$ref"
    fi
    return 0
}

list_channels() {
    if [ ! -s "$CHANNELS_FILE" ]; then
        printf 'no channels subscribed — add one with: yt --subscribe <channel_id>\n'
        return 0
    fi
    awk -F'\t' 'NF { if ($2 == "" || $2 == $1) print $1; else print $1 "  " $2 }' "$CHANNELS_FILE"
}

parse_atom_feed() { # [xml_string|file] [--with-date] -> RAW_TEMPLATE rows
    # YouTube Atom entries carry id, title, author, views and published date but
    # no duration, so duration/live_status stay NA. --with-date appends the ISO
    # timestamp as an 8th column so callers can merge feeds newest-first.
    local src="" with_date=0 a
    for a in "$@"; do
        case $a in
            --with-date) with_date=1 ;;
            *) [ -z "$src" ] && src=$a ;;
        esac
    done
    {
        if [ -z "$src" ]; then cat
        elif [ -f "$src" ]; then cat "$src"
        else printf '%s\n' "$src"
        fi
    } | awk '{ gsub(/></, ">\n<"); print }' | awk -v tab="$TAB" -v with_date="$with_date" '
        function strip(line, name,   s, e) {
            s = index(line, "<" name ">")
            if (!s) return ""
            line = substr(line, s + length(name) + 2)
            e = index(line, "</" name ">")
            if (!e) return ""
            return substr(line, 1, e - 1)
        }
        function attr(line, name,   s, q, rest, e) {
            s = index(line, name "=")
            if (!s) return ""
            s += length(name "=")
            q = substr(line, s, 1)
            if (q != "\"" && q != "\047") return ""
            rest = substr(line, s + 1)
            e = index(rest, q)
            if (!e) return ""
            return substr(rest, 1, e - 1)
        }
        function unesc(s) {
            gsub(/&lt;/, "<", s)
            gsub(/&gt;/, ">", s)
            gsub(/&quot;/, "\"", s)
            gsub(/&apos;/, "\047", s)
            gsub(/&amp;/, "\\&", s)
            gsub(/\t/, " ", s)
            return s
        }
        BEGIN { in_entry = 0 }
        /<entry>/ { in_entry = 1; id = ""; title = ""; uploader = ""; views = ""; published = ""; next }
        in_entry && /<\/entry>/ {
            if (id != "") {
                line = id tab title tab uploader tab "NA" tab "https://www.youtube.com/watch?v=" id tab (views == "" ? "NA" : views) tab "NA"
                if (with_date) line = line tab published
                print line
            }
            in_entry = 0
            next
        }
        in_entry {
            if (id == "" && index($0, "<yt:videoId>")) id = unesc(strip($0, "yt:videoId"))
            if (title == "" && index($0, "<title>")) title = unesc(strip($0, "title"))
            if (uploader == "" && index($0, "<name>")) uploader = unesc(strip($0, "name"))
            if (views == "" && index($0, "media:statistics")) views = attr($0, "views")
            if (published == "" && index($0, "<published>")) published = unesc(strip($0, "published"))
        }
    ' | awk -F'\t' -v OFS='\t' '{
        gsub(/[^A-Za-z0-9_-]/, "", $1)
        if ($1 == "") next
        if ($3 == "") $3 = "NA"
        print
    }'
}

fetch_rss_subs() { # channels file -> merged RAW rows, newest first
    local tmp="${YT_RUNDIR:-${TMPDIR:-/tmp}}/rss.$$" id title feed xml
    [ -s "${CHANNELS_FILE:-/nonexistent}" ] || return 0
    have curl || return 0
    : > "$tmp"
    while IFS="$TAB" read -r id title; do
        [ -n "$id" ] || continue
        feed=$(channel_feed_url "$id")
        [ -n "$feed" ] || continue
        xml=$(curl -fsSL --max-time 10 "$feed" 2>/dev/null) || continue
        [ -n "$xml" ] || continue
        printf '%s' "$xml" | parse_atom_feed --with-date >> "$tmp"
    done < "$CHANNELS_FILE"
    if [ -s "$tmp" ]; then
        LC_ALL=C sort -t "$TAB" -k8,8r "$tmp" | cut -f1-7
    fi
    rm -f "$tmp"
}

subs_raw() { # mode(auto|cached|refresh) -> RAW rows from guest RSS feeds
    local mode=${1:-auto} file data
    file="$(view_cache_file subs)"
    case $mode in
        cached) [ -s "$file" ] && cat "$file"; return 0 ;;
        auto)   if is_fresh "$file" "$YT_CACHE_TTL"; then cat "$file"; return 0; fi ;;
    esac
    data=$(fetch_rss_subs)
    if [ -n "$data" ]; then
        printf '%s\n' "$data" | cache_write "$file"
        printf '%s\n' "$data"
    elif [ -s "$file" ]; then
        cat "$file"
    fi
    return 0
}
