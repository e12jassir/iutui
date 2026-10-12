# lib/config.sh — safe config reader, defaults and interactive mutator
# Shellcheck source=/dev/null

load_config() {
    [ -f "$CONFIG_FILE" ] || return 0
    local line key val name def
    while IFS= read -r line || [ -n "$line" ]; do
        line="${line#"${line%%[![:space:]]*}"}"
        case $line in ''|'#'*) continue ;; esac
        case $line in *=*) ;; *) continue ;; esac
        key="${line%%=*}"
        val="${line#*=}"
        key="${key%"${key##*[![:space:]]}"}"
        [[ $key =~ ^YT_[A-Z0-9_]+$ ]] || continue
        val="${val#"${val%%[![:space:]]*}"}"
        case $val in
            \"*) val="${val#\"}"; val="${val%%\"*}" ;;
            \'*) val="${val#\'}"; val="${val%%\'*}" ;;
            *)   val="${val%%[[:space:]]#*}"; val="${val%"${val##*[![:space:]]}"}" ;;
        esac
        # ${VAR:-default}
        if [[ $val =~ ^\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}$ ]]; then
            name="${BASH_REMATCH[1]}"; def="${BASH_REMATCH[2]}"
            val="${!name:-$def}"
        fi
        val="${val/#\~/$HOME}"
        val="${val//\$\{HOME\}/$HOME}"
        val="${val//\$HOME/$HOME}"
        [ -n "${!key+x}" ] && continue
        printf -v "$key" '%s' "$val"
    done < "$CONFIG_FILE"
}

as_num() { local n=$1 d=$2; case "${!n}" in ''|*[!0-9]*) printf -v "$n" '%s' "$d" ;; esac; }
as_enum() { local n=$1 d=$2 v; shift 2; for v in "$@"; do [ "${!n}" = "$v" ] && return 0; done; printf -v "$n" '%s' "$d"; }

set_defaults() {
    : "${YT_FEED_SIZE:=100}" "${YT_SEARCH_SIZE:=40}" "${YT_CACHE_TTL:=900}"
    : "${YT_COOKIE_TTL:=21600}" "${YT_AUDIO_VOLUME:=75}" "${YT_QUALITY:=1080}"
    : "${YT_MUSIC_MODE:=bg}" "${YT_DOWNLOAD_AUDIO:=0}"
    : "${YT_PREFETCH:=24}" "${YT_DEFAULT_MODE:=window}" "${YT_PLAYBACK:=auto}"
    : "${YT_HOME:=auto}" "${YT_LANG:=auto}" "${YT_PREVIEW:=auto}" "${YT_THUMB_QUALITY:=mq}"
    : "${YT_HIDE_SHORTS:=1}" "${YT_INTERLEAVE:=1}" "${YT_NOTIFY:=1}" "${YT_HEIGHT:=95%}"
    : "${YT_COOKIES:=auto}" "${YT_COOKIES_FILE:=}" "${YT_BROWSER:=}"
    : "${YT_BROWSERS:=zen firefox librewolf brave chromium chrome}"
    : "${YT_BROWSER_CMD:=}" "${YT_YTDLP:=yt-dlp}" "${YT_YTDLP_ARGS:=}" "${YT_FZF_OPTS:=}"
    : "${YT_DOWNLOAD_DIR:=}" "${YT_PYTHON:=python3}"
    as_num YT_FEED_SIZE 100; as_num YT_SEARCH_SIZE 40; as_num YT_CACHE_TTL 900
    as_num YT_COOKIE_TTL 21600; as_num YT_AUDIO_VOLUME 75; as_num YT_QUALITY 1080
    as_num YT_PREFETCH 24
    as_enum YT_DEFAULT_MODE window window pip fs audio
    as_enum YT_MUSIC_MODE bg bg term
    as_enum YT_PLAYBACK pipe pipe direct auto
    as_enum YT_PREVIEW auto auto kitty chafa none
    as_enum YT_THUMB_QUALITY max max hq mq
    as_enum YT_HIDE_SHORTS 1 0 1; as_enum YT_INTERLEAVE 1 0 1; as_enum YT_NOTIFY 1 0 1
    as_enum YT_COOKIES auto auto off
    as_enum YT_DOWNLOAD_AUDIO 0 0 1
    YTDLP="$YT_YTDLP"
    PYTHON="$YT_PYTHON"
    # shellcheck disable=SC2206
    YTDLP_EXTRA=($YT_YTDLP_ARGS)
}

set_config_val() {
    local key="$1" val="$2" file="$CONFIG_FILE"
    mkdir -p "$(dirname "$file")"
    [ -f "$file" ] || touch "$file"
    if grep -qE "^[[:space:]]*${key}=" "$file"; then
        sed -i -E "s|^[[:space:]]*(${key}=).*|\1\"${val}\"|" "$file"
    else
        printf '%s="%s"\n' "$key" "$val" >> "$file"
    fi
    printf -v "$key" '%s' "$val"
}
