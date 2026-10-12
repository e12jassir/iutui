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

initial_rows() { # view query: cache only; "syncing" placeholder if a refresh is about to run
    local view=$1 query=$2 rows
    rows=$(emit_rows "$view" cached "$query")
    if [ -z "$rows" ] && needs_sync "$view" "$query"; then
        printf 'none\t-\t-\t-\t-\t-\t  \033[90m%s\033[0m\n' "$(t syncing)"
    else
        emit_view "$view" cached "$query"
    fi
}

browse() { # view [query]
    local view=$1 query=${2:-}
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

    local default_play="window"
    if [ "$view" = "music" ] || [ "$YT_DEFAULT_MODE" = "audio" ]; then
        default_play="audio"
    fi

    B+=(--bind "enter:execute-silent($act play $default_play {1} {2} {3} {4} {5})")
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
    # shellcheck disable=SC2206
    extra=($YT_FZF_OPTS)

    initial_rows "$view" "$query" | fzf "${F[@]}" \
        --border-label=" $(tab_label "$view") " \
        --preview="$prev {1} {3} {4} {5} {6}" \
        --preview-window='right:48%:border-left:wrap' \
        "${B[@]}" "${extra[@]}" >/dev/null
    kitty_clear
    return 0
}

settings_menu() {
    local prompt_s="⚙ ajustes › "
    while :; do
        kitty_clear
        local opt
        opt=$(printf "%s\n" \
            "󰕾 Calidad de video: ${YT_QUALITY}p" \
            "󰎆 Modo música: ${YT_MUSIC_MODE} (bg=fondo, term=interactivo)" \
            "󰝚 Formato descarga: $([ "${YT_DOWNLOAD_AUDIO:-0}" = 1 ] && echo 'Solo audio MP3' || echo 'Video MP4')" \
            "󰕾 Volumen audio: ${YT_AUDIO_VOLUME}%" \
            "󰏘 Miniaturas preview: ${YT_PREVIEW}" \
            "󰏘 Calidad miniaturas: ${YT_THUMB_QUALITY}" \
            "󰋚 Refrescar cookies ahora" \
            "󰋚 Limpiar caché y miniaturas" \
            "󰌑 Volver" | fzf \
            --color "${YT_FZF_COLORS:-bg:-1,bg+:-1,gutter:-1,border:238,header:241,info:241,prompt:1,pointer:1,marker:2,hl:4,hl+:4,spinner:1,label:238,query:-1,fg:-1,fg+:15}" \
            --pointer="▌" \
            --prompt="$prompt_s" \
            --height=45% \
            --layout=reverse \
            --border=rounded \
            --border-label=" ajustes " \
            --border-label-pos=2)
        [ $? -ne 0 ] || [ -z "$opt" ] && return 0

        case "$opt" in
            *"Calidad de video"*)
                local q
                q=$(printf "1080\n720\n480\n0 (máxima)\n" | fzf --prompt="Resolución › " --height=30% --layout=reverse --border=rounded)
                if [ -n "$q" ]; then
                    q="${q%% *}"
                    set_config_val YT_QUALITY "$q"
                    notify "Calidad de video: ${q}p"
                fi
                ;;
            *"Modo música"*)
                local m
                m=$(printf "bg (en segundo plano / desacoplado)\nterm (en terminal interactiva con teclas mpv)\n" | fzf --prompt="Modo música › " --height=25% --layout=reverse --border=rounded)
                if [ -n "$m" ]; then
                    m="${m%% *}"
                    set_config_val YT_MUSIC_MODE "$m"
                    notify "Modo música: $m"
                fi
                ;;
            *"Formato descarga"*)
                local d
                d=$(printf "0 (video mp4 completo)\n1 (solo audio mp3)\n" | fzf --prompt="Descarga › " --height=25% --layout=reverse --border=rounded)
                if [ -n "$d" ]; then
                    d="${d%% *}"
                    set_config_val YT_DOWNLOAD_AUDIO "$d"
                    notify "Descarga audio: $d"
                fi
                ;;
            *"Volumen audio"*)
                local v
                v=$(printf "50\n75\n100\n120\n" | fzf --prompt="Volumen % › " --height=25% --layout=reverse --border=rounded)
                if [ -n "$v" ]; then
                    set_config_val YT_AUDIO_VOLUME "$v"
                    notify "Volumen: ${v}%"
                fi
                ;;
            *"Miniaturas preview"*)
                local p
                p=$(printf "auto\nkitty\nchafa\nnone\n" | fzf --prompt="Preview › " --height=25% --layout=reverse --border=rounded)
                if [ -n "$p" ]; then
                    set_config_val YT_PREVIEW "$p"
                    notify "Preview: $p"
                fi
                ;;
            *"Calidad miniaturas"*)
                local tq
                tq=$(printf "mq (rápido 16:9)\nhq (alta calidad)\nmax (resolución máxima)\n" | fzf --prompt="Calidad miniaturas › " --height=25% --layout=reverse --border=rounded)
                if [ -n "$tq" ]; then
                    tq="${tq%% *}"
                    set_config_val YT_THUMB_QUALITY "$tq"
                    notify "Calidad miniaturas: $tq"
                fi
                ;;
            *"Refrescar cookies"*)
                notify "Refrescando cookies..."
                refresh_cookie_jar && notify "✅ Cookies sincronizadas" || notify "⚠ No se detectó sesión"
                ;;
            *"Limpiar caché"*)
                clear_cache
                notify "Caché limpiada"
                ;;
            *"Volver"*)
                return 0
                ;;
        esac
    done
}

build_menu_options() {
    if cookies_possible; then
        printf "󰐌  para ti\n󰎆  música\n󰑈  suscripciones\n"
    else
        printf "󰎆  música\n"
    fi
    printf "󰦑  ver después\n󰋚  historial\n󰒓  ajustes\n󰈆  salir\n"
}

main_menu() {
    require_deps
    resolve_lang
    while :; do
        kitty_clear
        local fzf_out
        fzf_out=$(build_menu_options | fzf \
            --color "${YT_FZF_COLORS:-bg:-1,bg+:-1,gutter:-1,border:238,header:241,info:241,prompt:1,pointer:1,marker:2,hl:4,hl+:4,spinner:1,label:238,query:-1,fg:-1,fg+:15}" \
            --pointer="▌" \
            --marker="●" \
            --scrollbar="│" \
            --disabled \
            --print-query \
            --prompt="  › " \
            --ghost="buscar en youtube..." \
            --bind="esc:cancel" \
            --height=38% \
            --layout=reverse \
            --border=rounded \
            --border-label=" yt " \
            --border-label-pos=2 \
            --info=hidden)

        local status=$?
        [ $status -ne 0 ] && return 0

        local query item
        query=$(printf "%s\n" "$fzf_out" | sed -n '1p')
        item=$(printf "%s\n" "$fzf_out" | sed -n '2p')

        if [ -n "$query" ]; then
            browse search "$query"
            kitty_clear
        else
            [ -z "$item" ] && return 0
            case "$item" in
                *"para ti"*)        browse foryou ;;
                *"música"*)         browse music ;;
                *"suscripciones"*)  browse subs ;;
                *"ver después"*)    browse later ;;
                *"historial"*)      browse history ;;
                *"ajustes"*)        settings_menu ;;
                *"salir"*)          return 0 ;;
            esac
            kitty_clear
        fi
    done
}
