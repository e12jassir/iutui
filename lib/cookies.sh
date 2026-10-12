# lib/cookies.sh — browser detection, cookie jar refresh and isolation
# Shellcheck source=/dev/null

have_any() { local x; for x in "$@"; do case $x in /*) [ -e "$x" ] && return 0 ;; *) have "$x" && return 0 ;; esac; done; return 1; }

firefox_profile_in() { # dirs... -> first profile dir that has cookies.sqlite
    local base p
    for base in "$@"; do
        [ -d "$base" ] || continue
        for p in "$base"/*default-release* "$base"/*[Dd]efault*; do
            [ -f "$p/cookies.sqlite" ] && { printf '%s' "$p"; return 0; }
        done
    done
    return 1
}

browser_specs() { # one --cookies-from-browser spec per line, in priority order
    if [ -n "${YT_BROWSER:-}" ]; then printf '%s\n' "$YT_BROWSER"; return 0; fi
    local b p cfg="${XDG_CONFIG_HOME:-$HOME/.config}" mac="$HOME/Library/Application Support"
    for b in $YT_BROWSERS; do
        case $b in
            zen)
                p=$(firefox_profile_in "$HOME/.zen" "$cfg/zen" "$HOME/.var/app/app.zen_browser.zen/.zen" "$mac/zen/Profiles") \
                    && printf 'firefox:%s\n' "$p" ;;
            firefox)
                p=$(firefox_profile_in "$HOME/.mozilla/firefox" "$cfg/mozilla/firefox" "$HOME/snap/firefox/common/.mozilla/firefox" \
                    "$HOME/.var/app/org.mozilla.firefox/.mozilla/firefox" "$mac/Firefox/Profiles") \
                    && printf 'firefox:%s\n' "$p" ;;
            librewolf)
                p=$(firefox_profile_in "$HOME/.librewolf" "$cfg/librewolf" "$HOME/.var/app/io.gitlab.librewolf-community/.librewolf") \
                    && printf 'firefox:%s\n' "$p" ;;
            brave)    have_any brave-browser brave "/Applications/Brave Browser.app" && echo brave ;;
            chromium) have_any chromium chromium-browser && echo chromium ;;
            chrome)   have_any google-chrome-stable google-chrome "/Applications/Google Chrome.app" && echo chrome ;;
            edge)     have_any microsoft-edge microsoft-edge-stable "/Applications/Microsoft Edge.app" && echo edge ;;
            vivaldi)  have_any vivaldi vivaldi-stable && echo vivaldi ;;
            *)        printf '%s\n' "$b" ;;
        esac
    done
    return 0
}

filter_cookies() { # Netscape jar on stdin -> only Google/YouTube domains on stdout
    awk -F'\t' '
        substr($0, 1, 1) == "#" && substr($0, 1, 10) != "#HttpOnly_" { print; next }
        {
            d = $1; sub(/^#HttpOnly_/, "", d)
            if (d ~ /(^|\.)(youtube|youtube-nocookie|googlevideo|ytimg|ggpht)\.com$/ ||
                d ~ /(^|\.)google\.[a-z.]+$/) print
        }'
}

cookie_has_login() {
    [ -s "$1" ] && grep -qE '[[:space:]](LOGIN_INFO|SAPISID|__Secure-3PAPISID)[[:space:]]' "$1"
}

ytdlp() { "$YTDLP" "${YTDLP_EXTRA[@]}" "$@"; }

refresh_cookie_jar() {
    local spec raw tmp found=""
    while IFS= read -r spec; do
        [ -n "$spec" ] || continue
        raw="$COOKIE_JAR.raw.$$"
        ( umask 077
          ytdlp --cookies-from-browser "$spec" --cookies "$raw" --no-warnings --skip-download \
              --flat-playlist --playlist-items 1 --print id "ytsearch1:youtube" >/dev/null 2>&1 ) </dev/null
        if [ -s "$raw" ]; then
            tmp="$COOKIE_JAR.tmp.$$"
            ( umask 077; filter_cookies < "$raw" > "$tmp" )
            if cookie_has_login "$tmp"; then
                mv -f "$tmp" "$COOKIE_JAR"; chmod 600 "$COOKIE_JAR"
                found=1
            else
                rm -f "$tmp"
            fi
        fi
        rm -f "$raw"
        [ -n "$found" ] && break
    done < <(browser_specs)
    if [ -n "$found" ]; then
        rm -f "$COOKIE_NONE"
        return 0
    fi
    if [ -s "$COOKIE_JAR" ]; then touch "$COOKIE_JAR"; else : > "$COOKIE_NONE"; fi
    return 1
}

COOKIE_ARGS=()
prepare_cookies() { # sets COOKIE_ARGS=(--cookies <private copy>) or ()
    COOKIE_ARGS=()
    [ "$YT_COOKIES" = off ] && return 0
    local src=""
    if [ -n "$YT_COOKIES_FILE" ]; then
        [ -r "$YT_COOKIES_FILE" ] && src="$YT_COOKIES_FILE"
    else
        if [ "${1:-}" != "fast" ]; then
            if ! is_fresh "$COOKIE_JAR" "$YT_COOKIE_TTL"; then
                if [ -s "$COOKIE_JAR" ] || ! is_fresh "$COOKIE_NONE" 900; then
                    refresh_cookie_jar || true
                fi
            fi
        fi
        [ -s "$COOKIE_JAR" ] && src="$COOKIE_JAR"
    fi
    [ -n "$src" ] || return 0
    local copy
    copy="$(mktemp "$CACHE_DIR/ck.XXXXXX")" || return 0
    cp "$src" "$copy" 2>/dev/null && COOKIE_ARGS=(--cookies "$copy") || rm -f "$copy"
    return 0
}

cookies_possible() { # cheap check (no network): could we plausibly be logged in?
    [ "$YT_COOKIES" = off ] && return 1
    [ -n "$YT_COOKIES_FILE" ] && return 0
    [ -s "$COOKIE_JAR" ] && return 0
    is_fresh "$COOKIE_NONE" 900 && return 1
    [ -n "$(browser_specs | head -n 1)" ]
}

forget_cookies() {
    rm -f "$COOKIE_JAR" "$COOKIE_NONE" "$CACHE_DIR"/ck.*
    echo "cookies forgotten (next run re-reads your browser)"
}
