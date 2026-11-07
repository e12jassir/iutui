# lib/ui.sh — fzf browsing, tab switching, interactive settings and main menu TUIs
# Shellcheck source=/dev/null

home_view() {
    case $YT_HOME in foryou|music|subs|later|history|search) echo "$YT_HOME"; return 0 ;; esac
    if cookies_possible; then echo foryou
    elif [ -s "$HISTORY_FILE" ]; then echo history
    else echo search
    fi
}

needs_sync() { # view query
    case $1 in
        foryou|subs|music) ! is_fresh "$(view_cache_file "$1")" "$YT_CACHE_TTL" ;;
        search)            [ -n "$2" ] && ! is_fresh "$(search_cache_file "$2")" 600 ;;
        *)                 return 1 ;;
    esac
}

initial_rows() { # view query: single render from cache; "syncing" placeholder if refresh is pending
    local view=$1 query=$2 rows tmp
    rows=$(emit_rows "$view" cached "$query")
    if [ -z "$rows" ]; then
        if needs_sync "$view" "$query"; then
            printf 'none\t-\t-\t-\t-\t-\t  \033[90m%s\033[0m\n' "$(t syncing)"
        else
            empty_row "$view"
        fi
        return 0
    fi
    printf '%s\n' "$rows"
    tmp="$YT_RUNDIR/rows.$$"
    if printf '%s\n' "$rows" > "$tmp"; then
        ( prefetch_thumbs "$tmp" ) </dev/null >/dev/null 2>&1 &
        disown 2>/dev/null || true
    fi
}

browse() { # view [query]
    local view=$1 query=${2:-}
    YT_INTERACTIVE=1
    require_deps
    printf '%s\n' "$view" > "$YT_RUNDIR/view"
    [ -n "$query" ] && printf '%s\n' "$query" > "$YT_RUNDIR/query"
    YT_COLS=$(tput cols 2>/dev/null || echo 120); export YT_COLS
    trap cleanup EXIT
    trap 'exit 130' INT TERM

    local sq act list prev P_SYNC='⟳ › ' P_IDLE='  › '
    sq=$(printf '%q' "$SELF"); act="$sq __do"; list="$sq __list"
    prev=$(printf '%q' "$PREVIEW")

    local -a B=() F=(--ansi --delimiter="$TAB" --with-nth=7 --tiebreak=index --layout=reverse
                     --height="$YT_HEIGHT" --border=rounded --border-label-pos=2 --info=inline
                     --prompt="$P_IDLE" --pointer='▌' --marker='●' --scrollbar='│')
    if [ -n "${NO_COLOR:-}" ]; then F+=(--no-color)
    else F+=(--color "${YT_FZF_COLORS:-bg:-1,bg+:-1,gutter:-1,border:238,header:241,info:241,prompt:1,pointer:1,marker:2,hl:4,hl+:4,spinner:1,label:238,query:-1,fg:-1,fg+:15}"); fi
    version_ge "$(fzf_version)" 0.58.0 && F+=(--ghost="$(t ghost)")

    B+=(--bind "enter:execute-silent($act play auto {1} {2} {3} {4} {5})")
    B+=(--bind "alt-m:execute-silent($act mix {1} {3} {4})" --bind "ctrl-x:execute-silent($act mix {1} {3} {4})")
    B+=(--bind "ctrl-p:execute-silent($act play pip {1} {2} {3} {4} {5})")
    B+=(--bind "ctrl-f:execute-silent($act play fs {1} {2} {3} {4} {5})")
    B+=(--bind "ctrl-a:execute-silent($act play audio {1} {2} {3} {4} {5})")
    B+=(--bind "ctrl-y:execute-silent($act copy {1})")
    B+=(--bind "ctrl-w:execute-silent($act later {1} {3} {4} {5} {2})")
    B+=(--bind "ctrl-d:execute-silent($act download {1} {2} {3})")
    B+=(--bind "ctrl-o:execute-silent($act open {1} {2})")
    B+=(--bind "ctrl-/:toggle-preview" --bind "alt-p:toggle-preview")
    B+=(--bind "ctrl-r:change-prompt($P_SYNC)+reload-sync($list @current --refresh)+change-prompt($P_IDLE)")
    B+=(--bind "ctrl-s:change-border-label( $(tab_label search) )+change-prompt($P_SYNC)+reload-sync($list search {q})+change-prompt($P_IDLE)+clear-query")
    local v n=1
    for v in foryou music subs later history; do
        B+=(--bind "alt-$n:change-border-label( $(tab_label "$v") )+change-prompt($P_SYNC)+reload-sync($list $v)+change-prompt($P_IDLE)")
        n=$(( n + 1 ))
    done
    # Stale-while-revalidate: draw the cache instantly, refresh behind it.
    if needs_sync "$view" "$query"; then
        B+=(--bind "start:change-prompt($P_SYNC)+reload-sync($list @current)+change-prompt($P_IDLE)")
    fi

    local -a extra=()
    set -f
    # shellcheck disable=SC2206
    extra=($YT_FZF_OPTS)
    set +f

    initial_rows "$view" "$query" | fzf "${F[@]}" \
        --header="$(t header1)" \
        --border-label=" $(tab_label "$view") " \
        --preview="$prev {1} {3} {4} {5} {6}" \
        --preview-window='right:48%:border-left:wrap' \
        "${B[@]}" "${extra[@]}" >/dev/null
    kitty_clear
    return 0
}

settings_menu() {
    local l_title l_quality l_music l_format l_volume l_preview l_thumbq l_cookies l_cache l_back prompt_s
    l_title=$(t settings_title); l_quality=$(t settings_quality); l_music=$(t settings_music)
    l_format=$(t settings_format); l_volume=$(t settings_volume); l_preview=$(t settings_preview)
    l_thumbq=$(t settings_thumbq); l_cookies=$(t settings_cookies); l_cache=$(t settings_cache)
    l_back=$(t settings_back)
    prompt_s="⚙ $l_title › "
    while :; do
        kitty_clear
        local opt
        opt=$(printf "%s\n" \
            "󰕾 $l_quality: ${YT_QUALITY}p" \
            "󰎆 $l_music: ${YT_MUSIC_MODE} ($(t settings_music_hint))" \
            "󰝚 $l_format: $([ "${YT_DOWNLOAD_AUDIO:-0}" = 1 ] && t settings_fmt_mp3 || t settings_fmt_mp4)" \
            "󰕾 $l_volume: ${YT_AUDIO_VOLUME}%" \
            "󰏘 $l_preview: ${YT_PREVIEW}" \
            "󰏘 $l_thumbq: ${YT_THUMB_QUALITY}" \
            "󰋚 $l_cookies" \
            "󰋚 $l_cache" \
            "󰌑 $l_back" | fzf \
            --color "${YT_FZF_COLORS:-bg:-1,bg+:-1,gutter:-1,border:238,header:241,info:241,prompt:1,pointer:1,marker:2,hl:4,hl+:4,spinner:1,label:238,query:-1,fg:-1,fg+:15}" \
            --pointer="▌" \
            --prompt="$prompt_s" \
            --height=45% \
            --layout=reverse \
            --border=rounded \
            --border-label=" $l_title " \
            --border-label-pos=2)
        [ $? -ne 0 ] || [ -z "$opt" ] && return 0

        case "$opt" in
            *"$l_quality"*)
                local q
                q=$(printf "1080\n720\n480\n%s\n" "$(t settings_quality_max)" | fzf --prompt="$(t settings_quality_prompt)" --height=30% --layout=reverse --border=rounded)
                if [ -n "$q" ]; then
                    q="${q%% *}"
                    set_config_val YT_QUALITY "$q"
                    notify "$l_quality: ${q}p"
                fi
                ;;
            *"$l_music"*)
                local m
                m=$(printf "%s\n%s\n" "$(t settings_music_bg_opt)" "$(t settings_music_term_opt)" | fzf --prompt="$(t settings_music_prompt)" --height=25% --layout=reverse --border=rounded)
                if [ -n "$m" ]; then
                    m="${m%% *}"
                    set_config_val YT_MUSIC_MODE "$m"
                    notify "$l_music: $m"
                fi
                ;;
            *"$l_format"*)
                local d
                d=$(printf "%s\n%s\n" "$(t settings_dl_video_opt)" "$(t settings_dl_audio_opt)" | fzf --prompt="$(t settings_download_prompt)" --height=25% --layout=reverse --border=rounded)
                if [ -n "$d" ]; then
                    d="${d%% *}"
                    set_config_val YT_DOWNLOAD_AUDIO "$d"
                    notify "$l_format: $d"
                fi
                ;;
            *"$l_volume"*)
                local v
                v=$(printf "50\n75\n100\n120\n" | fzf --prompt="$(t settings_volume_prompt)" --height=25% --layout=reverse --border=rounded)
                if [ -n "$v" ]; then
                    set_config_val YT_AUDIO_VOLUME "$v"
                    notify "$l_volume: ${v}%"
                fi
                ;;
            *"$l_preview"*)
                local p
                p=$(printf "auto\nkitty\nchafa\nnone\n" | fzf --prompt="$(t settings_preview_prompt)" --height=25% --layout=reverse --border=rounded)
                if [ -n "$p" ]; then
                    set_config_val YT_PREVIEW "$p"
                    notify "$l_preview: $p"
                fi
                ;;
            *"$l_thumbq"*)
                local tq
                tq=$(printf "%s\n%s\n%s\n" "$(t settings_thumb_mq_opt)" "$(t settings_thumb_hq_opt)" "$(t settings_thumb_max_opt)" | fzf --prompt="$(t settings_thumbq_prompt)" --height=25% --layout=reverse --border=rounded)
                if [ -n "$tq" ]; then
                    tq="${tq%% *}"
                    set_config_val YT_THUMB_QUALITY "$tq"
                    notify "$l_thumbq: $tq"
                fi
                ;;
            *"$l_cookies"*)
                notify "$(t n_refreshing)"
                refresh_cookie_jar && notify "$(t n_cookies_synced)" || notify "$(t n_cookies_none)"
                ;;
            *"$l_cache"*)
                clear_cache
                notify "$(t n_cache_cleared)"
                ;;
            *"$l_back"*)
                return 0
                ;;
        esac
    done
}

build_menu_options() {
    if cookies_possible; then
        printf "󰐌  %s\n󰎆  %s\n󰑈  %s\n" "$(t menu_foryou)" "$(t menu_music)" "$(t menu_subs)"
    else
        printf "󰎆  %s\n" "$(t menu_music)"
    fi
    printf "󰦑  %s\n󰋚  %s\n󰒓  %s\n󰈆  %s\n" \
        "$(t menu_later)" "$(t menu_history)" "$(t menu_settings)" "$(t menu_quit)"
}

main_menu() {
    require_deps
    resolve_lang
    while :; do
        kitty_clear
        local fzf_out
        local -a m_opts=(
            --color "${YT_FZF_COLORS:-bg:-1,bg+:-1,gutter:-1,border:238,header:241,info:241,prompt:1,pointer:1,marker:2,hl:4,hl+:4,spinner:1,label:238,query:-1,fg:-1,fg+:15}"
            --pointer="▌"
            --marker="●"
            --scrollbar="│"
            --disabled
            --print-query
            --prompt="  › "
            --bind="esc:cancel"
            --height=38%
            --layout=reverse
            --border=rounded
            --border-label=" yt "
            --border-label-pos=2
            --info=hidden
        )
        version_ge "$(fzf_version)" 0.58.0 && m_opts+=(--ghost="$(t menu_ghost)")
        fzf_out=$(build_menu_options | fzf "${m_opts[@]}")

        local status=$?
        [ $status -ne 0 ] && return 0

        local query item l_foryou l_music l_subs l_later l_history l_settings l_quit
        query=$(printf "%s\n" "$fzf_out" | sed -n '1p')
        item=$(printf "%s\n" "$fzf_out" | sed -n '2p')
        l_foryou=$(t menu_foryou); l_music=$(t menu_music); l_subs=$(t menu_subs)
        l_later=$(t menu_later); l_history=$(t menu_history)
        l_settings=$(t menu_settings); l_quit=$(t menu_quit)

        if [ -n "$query" ]; then
            browse search "$query"
            kitty_clear
        else
            [ -z "$item" ] && return 0
            case "$item" in
                *"$l_foryou"*)    browse foryou ;;
                *"$l_music"*)     browse music ;;
                *"$l_subs"*)      browse subs ;;
                *"$l_later"*)     browse later ;;
                *"$l_history"*)   browse history ;;
                *"$l_settings"*)  settings_menu ;;
                *"$l_quit"*)      return 0 ;;
            esac
            kitty_clear
        fi
    done
}
