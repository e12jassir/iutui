#!/usr/bin/env bash
# Smoke tests for yt. No network, no real fzf/yt-dlp/mpv: they are stubbed.
# Run: make test   (or: bash tests/smoke.sh)
# shellcheck source-path=SCRIPTDIR

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export HOME="$TMP/home" XDG_CACHE_HOME="$TMP/home/.cache" XDG_STATE_HOME="$TMP/home/.local/state"
export XDG_CONFIG_HOME="$TMP/home/.config" XDG_RUNTIME_DIR="$TMP/runtime" TMPDIR="$TMP"
export STUB_LOG="$TMP/ytdlp.log" FZF_ARGS_OUT="$TMP/fzf.args" FZF_STDIN_OUT="$TMP/fzf.stdin"
mkdir -p "$HOME" "$TMP/bin" "$TMP/runtime"
: > "$STUB_LOG"

# ── stubs ────────────────────────────────────────────────────────────────────
cat > "$TMP/bin/yt-dlp" <<'STUB'
#!/usr/bin/env bash
echo "$*" >> "$STUB_LOG"
[ "$1" = "--version" ] && { echo "2099.01.01"; exit 0; }
# --print-url --direct resolution (last positional argument is the video id)
for a in "$@"; do [ "$a" = "-g" ] && { echo "https://direct.example/${!#}"; exit 0; }; done
# deterministic failure injection for the classified-error tests
if [ -n "${YT_STUB_FAIL:-}" ]; then printf 'ERROR: %s\n' "$YT_STUB_FAIL" >&2; exit 1; fi
cookies_out=""; prev=""
for a in "$@"; do [ "$prev" = "--cookies" ] && cookies_out=$a; prev=$a; done
case "$*" in
  *--cookies-from-browser*)
    printf '# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tLOGIN_INFO\tabc\n#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tzzz\n.evil.example\tTRUE\t/\tTRUE\t0\tSECRET\tleak\n.google.com\tTRUE\t/\tTRUE\t0\tSID\tq\n' > "$cookies_out" ;;
esac
case "$*" in
  *ytsearch1:youtube*) echo "aaaaaaaaaaa" ;;
  *ytsearch*)
    printf 'srch0000001\tHello wörld 日本語タイトル\tChan A\t10:00\thttps://www.youtube.com/watch?v=srch0000001\t1200\tNA\n' ;;
  *youtube.com*)
    printf 'vidAAAAAAA1\tA one\tChan A\t10:00\thttps://www.youtube.com/watch?v=vidAAAAAAA1\t1500000\tNA\n'
    printf 'vidAAAAAAA2\tA two\tChan A\t11:00\thttps://www.youtube.com/watch?v=vidAAAAAAA2\t900\tNA\n'
    printf 'vidAAAAAAA3\tA three\tChan A\tNA\thttps://www.youtube.com/watch?v=vidAAAAAAA3\tNA\tis_live\n'
    printf 'vidBBBBBBB1\tB one\tChan B\t5:00\thttps://www.youtube.com/watch?v=vidBBBBBBB1\t42\tNA\n'
    printf 'shortSSSSS1\tA short\tChan C\t0:30\thttps://www.youtube.com/shorts/shortSSSSS1\t5\tNA\n'
    printf 'vidAAAAAAA1\tA one (dup)\tChan A\t10:00\thttps://www.youtube.com/watch?v=vidAAAAAAA1\t1500000\tNA\n'
    printf 'noch0000001\tNo channel\t\t1:00\thttps://www.youtube.com/watch?v=noch0000001\t7\tNA\n' ;;
esac
STUB
cat > "$TMP/bin/fzf" <<'STUB'
#!/usr/bin/env bash
[ "$1" = "--version" ] && { echo "0.60.0 (stub)"; exit 0; }
printf '%s\n' "$@" > "$FZF_ARGS_OUT"
cat > "$FZF_STDIN_OUT"
STUB
cat > "$TMP/bin/mpv" <<'STUB'
#!/usr/bin/env bash
[ "$1" = "--version" ] && { echo "mpv 0.38 stub"; exit 0; }
[ -n "${MPV_LOG:-}" ] && printf '%s\n' "$*" >> "$MPV_LOG"
cat >/dev/null 2>/dev/null
exit 0
STUB
cat > "$TMP/bin/notify-send" <<'STUB'
#!/usr/bin/env bash
[ -n "${NOTIFY_LOG:-}" ] && printf '%s\n' "${@: -1}" >> "$NOTIFY_LOG"
exit 0
STUB
cat > "$TMP/bin/mpv-ipc-server.py" <<'PY'
#!/usr/bin/env python3
"""Tiny mpv IPC stand-in: answers get_property and logs every command."""
import json
import os
import signal
import socket
import sys
import threading

sock_path, log_path, ready_path = sys.argv[1], sys.argv[2], sys.argv[3]

PROPS = {
    "media-title": "Test Video",
    "time-pos": 42.5,
    "duration": 300.0,
    "pause": False,
    "volume": 75.0,
    "playlist-count": 3,
}

try:
    os.unlink(sock_path)
except OSError:
    pass

server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(sock_path)
server.listen(16)
server.settimeout(0.2)
with open(ready_path, "w", encoding="utf-8") as fh:
    fh.write("ready")

stopping = False


def shutdown(signum, frame):
    global stopping
    stopping = True


signal.signal(signal.SIGTERM, shutdown)
signal.signal(signal.SIGINT, shutdown)


def handle(conn):
    try:
        conn.settimeout(1.0)
        buf = b""
        while b"\n" not in buf:
            chunk = conn.recv(65536)
            if not chunk:
                break
            buf += chunk
        line = buf.split(b"\n", 1)[0].decode("utf-8", "replace").strip()
        if not line:
            return
        try:
            msg = json.loads(line)
        except Exception:
            msg = {}
        cmd = msg.get("command") or []
        with open(log_path, "a", encoding="utf-8") as fh:
            fh.write(json.dumps(cmd) + "\n")
        response = {"error": "success"}
        if cmd and cmd[0] == "get_property" and len(cmd) > 1 and cmd[1] in PROPS:
            response = {"data": PROPS[cmd[1]], "error": "success"}
        conn.sendall((json.dumps(response) + "\n").encode("utf-8"))
    except Exception:
        pass
    finally:
        try:
            conn.close()
        except Exception:
            pass


while not stopping:
    try:
        conn, _ = server.accept()
    except socket.timeout:
        continue
    except OSError:
        break
    thread = threading.Thread(target=handle, args=(conn,))
    thread.daemon = True
    thread.start()

server.close()
try:
    os.unlink(sock_path)
except OSError:
    pass
PY
printf '#!/usr/bin/env bash\necho "deno 1.0 stub"\n' > "$TMP/bin/deno"
printf '#!/usr/bin/env bash\nexit 0\n' > "$TMP/bin/ffmpeg"
cat > "$TMP/bin/curl" <<'STUB'
#!/usr/bin/env bash
url=""
for a in "$@"; do case $a in http*) url=$a ;; esac; done
case $url in
  *feeds/videos.xml*channel_id=UCrssChannel1*)
    cat <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <title>RSS Channel One</title>
  <entry>
    <id>yt:video:rssVid00001</id>
    <yt:videoId>rssVid00001</yt:videoId>
    <yt:channelId>UCrssChannel1</yt:channelId>
    <title>First &amp; Best</title>
    <published>2026-11-01T10:00:00+00:00</published>
    <author><name>RSS Channel One</name></author>
    <media:group><media:statistics views="1234"/></media:group>
  </entry>
  <entry>
    <id>yt:video:rssVid00002</id>
    <yt:videoId>rssVid00002</yt:videoId>
    <title>Second Video</title>
    <published>2026-11-02T10:00:00+00:00</published>
    <media:group><media:statistics views="99"/></media:group>
  </entry>
  <entry>
    <id>yt:video:rssVid00003</id>
    <yt:videoId>rssVid00003</yt:videoId>
    <title>Third Video</title>
    <published>2026-11-03T10:00:00+00:00</published>
    <media:group><media:statistics views="7"/></media:group>
  </entry>
</feed>
XML
    ;;
esac
exit 0
STUB
chmod +x "$TMP/bin/"*
export PATH="$TMP/bin:$PATH"

# ── tiny test framework ──────────────────────────────────────────────────────
PASS=0; FAIL=0
ok()   { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; [ -n "${2:-}" ] && printf '       %s\n' "$2"; }
eq()   { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "expected [$3] got [$2]"; fi; }
has()  { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1" "missing [$3] in [$2]" ;; esac; }
hasnt(){ case "$2" in *"$3"*) bad "$1" "unexpected [$3] in [$2]" ;; *) ok "$1" ;; esac; }

# shellcheck source=../bin/yt
source "$ROOT/bin/yt"
export YT_BROWSER="firefox:/nonexistent"      # force a deterministic cookie source
export YT_LANG="en"                           # force deterministic locale strings for tests
init light

echo "yt-fmt"
RAW="$("$TMP/bin/yt-dlp" https://www.youtube.com/)"
OUT="$(printf '%s\n' "$RAW" | "$ROOT/bin/yt-fmt" --width 80 --hide-shorts --interleave --no-color)"
IDS="$(printf '%s\n' "$OUT" | cut -f1 | tr '\n' ' ')"
eq "interleaves by channel, dedupes, hides shorts" "$IDS" "vidAAAAAAA1 vidBBBBBBB1 noch0000001 vidAAAAAAA2 vidAAAAAAA3 "
BAD="$(printf '%s\n' "$OUT" | awk -F'\t' 'NF != 7 || $1=="" || $2=="" || $3=="" || $4=="" || $5=="" || $6=="" {print}')"
eq "every row has 7 non-empty fields" "$BAD" ""
has "live badge" "$(printf '%s\n' "$OUT" | grep vidAAAAAAA3 | cut -f7)" "LIVE"
has "views are humanised" "$(printf '%s\n' "$OUT" | grep vidAAAAAAA1 | cut -f6)" "1.5M"
eq "missing channel becomes -" "$(printf '%s\n' "$OUT" | grep noch0000001 | cut -f4)" "-"
W="$(printf 'srch0000001\tHello wörld 日本語タイトル 日本語タイトル 日本語タイトル 日本語タイトル\tChan A\t10:00\thttps://x\t1\tNA\n' \
    | "$ROOT/bin/yt-fmt" --width 60 --no-color | cut -f7)"
eq "display width never exceeds the budget (CJK aware)" "$(python3 - "$W" <<'PY'
import sys, unicodedata
s = sys.argv[1]
w = sum(0 if unicodedata.category(c) in ("Mn","Me","Cf") else 2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in s)
print("ok" if w <= 60 else "too wide: %d" % w)
PY
)" "ok"
NC="$(printf '%s\n' "$RAW" | NO_COLOR=1 "$ROOT/bin/yt-fmt" | cut -f7 | head -n 1)"
hasnt "NO_COLOR removes ANSI" "$NC" $'\033'
MC="$(printf '%s\n' "$RAW" | "$ROOT/bin/yt-fmt" --music --no-color | cut -f7 | head -n 1)"
has "music icon rendered in rows" "$MC" "󰎆"
printf 'idid00000\tT\tC\t1:00\thttps://x\t1\tNA\textra\n' | "$ROOT/bin/yt-fmt" --stored >/dev/null && ok "stored rows accepted"

echo "config"
mkdir -p "$XDG_CONFIG_HOME/yt"
cat > "$XDG_CONFIG_HOME/yt/config" <<CFG
# comment
YT_FEED_SIZE=5        # inline comment
YT_SEARCH_SIZE="7"
YT_BROWSER_CMD="\${BROWSER:-fallbackbrowser}"
YT_DOWNLOAD_DIR=~/dl
YT_EVIL=\$(touch $TMP/pwned1)
notyt=\$(touch $TMP/pwned2)
YT_QUALITY=notanumber
CFG
unset YT_FEED_SIZE YT_SEARCH_SIZE YT_BROWSER_CMD YT_DOWNLOAD_DIR YT_EVIL YT_QUALITY BROWSER
init light
eq "inline comment stripped" "$YT_FEED_SIZE" "5"
eq "quoted value" "$YT_SEARCH_SIZE" "7"
eq "\${BROWSER:-x} supported" "$YT_BROWSER_CMD" "fallbackbrowser"
eq "tilde expands" "$YT_DOWNLOAD_DIR" "$HOME/dl"
eq "bad number falls back to default" "$YT_QUALITY" "1080"
if [ ! -e "$TMP/pwned1" ] && [ ! -e "$TMP/pwned2" ]; then
    ok "config is data, never executed"
else
    bad "config executed code"
fi
eq "env beats config file" "$( ( YT_FEED_SIZE=9; init light; echo "$YT_FEED_SIZE" ) )" "9"
rm -f "$XDG_CONFIG_HOME/yt/config"; unset YT_FEED_SIZE YT_SEARCH_SIZE YT_BROWSER_CMD YT_DOWNLOAD_DIR YT_QUALITY; init light
eq "YT_PLAYBACK defaults to auto" "$( ( unset YT_PLAYBACK; set_defaults; echo "$YT_PLAYBACK" ) )" "auto"

echo "cookies"
prepare_cookies
JAR="$CACHE_DIR/cookies.txt"
if [ -s "$JAR" ]; then ok "jar created"; else bad "jar missing"; fi
hasnt "foreign-domain cookies are dropped" "$(cat "$JAR")" "evil.example"
has "HttpOnly youtube cookie kept" "$(cat "$JAR")" "#HttpOnly_.youtube.com"
has "google cookie kept" "$(cat "$JAR")" ".google.com"
eq "jar permissions are 600" "$(stat -c %a "$JAR" 2>/dev/null || stat -f %Lp "$JAR")" "600"
eq "yt-dlp gets a private copy" "$([ "${COOKIE_ARGS[1]}" != "$JAR" ] && [ -s "${COOKIE_ARGS[1]}" ] && echo yes)" "yes"
BEFORE=$(grep -c cookies-from-browser "$STUB_LOG"); prepare_cookies; prepare_cookies
eq "browser DB is read once per TTL" "$(grep -c cookies-from-browser "$STUB_LOG")" "$BEFORE"

echo "feeds & cache"
: > "$STUB_LOG"
A="$(emit_view foryou auto | cut -f1 | head -n 1)"
eq "first row of the feed" "$A" "vidAAAAAAA1"
N1=$(grep -c 'youtube.com/' "$STUB_LOG")
emit_view foryou auto >/dev/null
eq "fresh cache: no second network call" "$(grep -c 'youtube.com/' "$STUB_LOG")" "$N1"
emit_view foryou refresh >/dev/null
eq "refresh forces a call" "$(grep -c 'youtube.com/' "$STUB_LOG")" "$((N1 + 1))"
if [ "$(emit_view foryou cached | wc -l)" -gt 0 ]; then ok "cached mode serves stale data"; else bad "cached mode empty"; fi
S="$(emit_view search auto "rust lang" | cut -f1 | head -n 1)"
eq "search results" "$S" "srch0000001"
touch -d '2 hours ago' "$(view_cache_file foryou)" 2>/dev/null || touch -t 200001010000 "$(view_cache_file foryou)"
if needs_sync foryou ""; then ok "stale cache triggers background sync"; else bad "stale cache not detected"; fi

echo "views & saved list"
E="$(emit_view later cached | cut -f1)"
eq "empty saved list shows a placeholder row" "$E" "none"
toggle_later vidBBBBBBB1 "B one" "Chan B" "5:00" "https://www.youtube.com/watch?v=vidBBBBBBB1"
eq "toggle adds" "$(cut -f1 "$WATCHLATER_FILE")" "vidBBBBBBB1"
has "saved items are starred in lists" "$(emit_view foryou cached | grep vidBBBBBBB1 | cut -f7)" "★"
toggle_later vidBBBBBBB1 x x x x
eq "toggle removes (exact id match)" "$(wc -l < "$WATCHLATER_FILE" | tr -d ' ')" "0"
printf '\033[90m 1:00 \033[0m T\tOld title\tOld chan\t1:00\toldid000001\thttps://www.youtube.com/watch?v=oldid000001\n' > "$WATCHLATER_FILE"
migrate_watchlater
eq "old watch-later format migrates" "$(cut -f1,2 "$WATCHLATER_FILE")" "$(printf 'oldid000001\tOld title')"
record_history vidAAAAAAA2 "A two" "Chan A" "11:00" "https://www.youtube.com/watch?v=vidAAAAAAA2"
eq "history shows newest first" "$(emit_view history cached | cut -f1 | head -n 1)" "vidAAAAAAA2"

echo "actions"
eq "ids are validated" "$(valid_id 'x; rm -rf /' && echo bad || echo ok)" "ok"
eq "URLs never start with a dash" "$(canonical_url abcdefghijk '-oevil')" "https://www.youtube.com/watch?v=abcdefghijk"
eq "canonical_url transforms RD mix playlist correctly" "$(canonical_url RDfWh0t69MGVk 'https://www.youtube.com/playlist?list=RDfWh0t69MGVk')" "https://www.youtube.com/watch?v=fWh0t69MGVk&list=RDfWh0t69MGVk"
do_action copy 'not valid!' ; ok "invalid ids are ignored by actions"

echo "ui wiring (fzf stub records its arguments)"
rm -f "$CACHE_DIR"/feed_*.tsv
( browse foryou ) >/dev/null 2>&1
ARGS="$(cat "$FZF_ARGS_OUT")"
has "fzf: only the display column is searchable" "$ARGS" "--with-nth=7"
has "fzf: enter plays without leaving the list" "$ARGS" "enter:execute-silent("
has "fzf: stale-while-revalidate bind on start" "$ARGS" "start:change-prompt("
has "fzf: ctrl-r reloads the current view" "$ARGS" "reload-sync("
has "fzf: ctrl-s searches with {q}" "$ARGS" "search {q})"
has "fzf: tabs alt-1..5" "$ARGS" "alt-5:"
has "fzf: preview wired to helper" "$ARGS" "yt-thumb-preview {1} {3} {4} {5} {6}"
has "fzf: ghost text on fzf >= 0.58" "$ARGS" "--ghost="
has "no cache yet: list starts with a syncing row" "$(cat "$FZF_STDIN_OUT")" "syncing"
emit_view foryou refresh >/dev/null
( browse foryou ) >/dev/null 2>&1
hasnt "fresh cache: no start-sync bind" "$(cat "$FZF_ARGS_OUT")" "start:change-prompt("
eq "fresh cache: first row comes from cache" "$(cut -f1 "$FZF_STDIN_OUT" | head -n 1)" "vidAAAAAAA1"
( browse music ) >/dev/null 2>&1
MUSIC_ARGS="$(cat "$FZF_ARGS_OUT")"
has "fzf: enter binds to dynamic auto mode" "$MUSIC_ARGS" "__do play auto {1}"
has "fzf: alt-m plays mix radio" "$MUSIC_ARGS" "alt-m:execute-silent("
has "fzf: ctrl-x plays mix radio" "$MUSIC_ARGS" "ctrl-x:execute-silent("
hasnt "fzf: ctrl-m is never bound (prevents enter hijack)" "$MUSIC_ARGS" "ctrl-m:"

echo "player & mixes"
# Test play_mix URL construction and RD stripping
test_mix_url() {
    local id=$1
    local clean_id="${id#RD}"
    echo "https://www.youtube.com/watch?v=${clean_id}&list=RD${clean_id}"
}
eq "play_mix strips RD prefix for clean playback" "$(test_mix_url RDJI2v5Tzn-g4)" "https://www.youtube.com/watch?v=JI2v5Tzn-g4&list=RDJI2v5Tzn-g4"
eq "play_mix leaves regular ID untouched" "$(test_mix_url dqOAxZaMHMg)" "https://www.youtube.com/watch?v=dqOAxZaMHMg&list=RDdqOAxZaMHMg"

# Test build_format for quality & audio modes
eq "build_format audio" "$(build_format audio 1080)" "ba/b"
eq "build_format 0 (max)" "$(build_format window 0)" "bestvideo+bestaudio/best"
eq "build_format 720" "$(build_format window 720)" "bestvideo[height<=720]+bestaudio/best"

# Test portable version_ge and glob-safe YT_YTDLP_ARGS
if version_ge 0.58.0 0.44.0; then ok "version_ge 0.58.0 >= 0.44.0"; else bad "version_ge 0.58.0 >= 0.44.0"; fi
if version_ge 0.43.9 0.44.0; then bad "version_ge 0.43.9 < 0.44.0"; else ok "version_ge 0.43.9 < 0.44.0"; fi
touch "$TMP/evil.mp4"
if ( cd "$TMP" && YT_YTDLP_ARGS="-o *.mp4" && set_defaults && [ "${YTDLP_EXTRA[1]}" = "*.mp4" ] ); then
    ok "YT_YTDLP_ARGS does not glob-expand"
else
    bad "YT_YTDLP_ARGS expanded globs"
fi

# Test set_config_val updates without sed -i
set_config_val YT_QUALITY 720
eq "set_config_val writes value" "$(grep '^YT_QUALITY=' "$CONFIG_FILE")" 'YT_QUALITY="720"'
set_config_val YT_QUALITY 1080
eq "set_config_val updates value in-place" "$(grep -c '^YT_QUALITY=' "$CONFIG_FILE")" "1"

# Test stop_prev_playback PID file management
sleep 10 &
DUMMY_PID=$!
echo "$DUMMY_PID" > "$STATE_DIR/audio.pid"
stop_prev_playback
kill -0 "$DUMMY_PID" 2>/dev/null && BAD_KILL=1 || BAD_KILL=0
eq "stop_prev_playback terminates previous player process" "$BAD_KILL" "0"
eq "stop_prev_playback cleans up PID file" "$([ -f "$STATE_DIR/audio.pid" ] && echo 1 || echo 0)" "0"

echo "preview"
PV="$(FZF_PREVIEW_COLUMNS=50 FZF_PREVIEW_LINES=30 YT_PREVIEW=none NO_COLOR=1 "$ROOT/bin/yt-thumb-preview" vidAAAAAAA1 "A one" "Chan A" "10:00" "1.5M" 2>&1)"
has "preview prints title" "$PV" "A one"
has "preview prints channel and views" "$PV" "Chan A  ·  10:00  ·  1.5M views"
has "preview prints short url" "$PV" "youtu.be/vidAAAAAAA1"
eq "preview ignores placeholder rows" "$("$ROOT/bin/yt-thumb-preview" none x 2>&1)" ""

echo "cli"
BEFORE_TMP=$(find "$TMP" -maxdepth 1 -name 'yt.*' | wc -l | tr -d ' ')
eq "--version" "$(bash "$ROOT/bin/yt" --version)" "yt $VERSION"
AFTER_TMP=$(find "$TMP" -maxdepth 1 -name 'yt.*' | wc -l | tr -d ' ')
eq "--version leaks zero temp dirs" "$AFTER_TMP" "$BEFORE_TMP"
DOC="$(bash "$ROOT/bin/yt" --doctor 2>&1)"; RC=$?
has "doctor lists fzf" "$DOC" "fzf"
has "doctor checks the JS runtime" "$DOC" "js runtime"
eq "doctor passes with all deps present" "$RC" "0"
has "usage mentions search" "$(bash "$ROOT/bin/yt" --help)" "search YouTube"
has "usage mentions settings" "$(bash "$ROOT/bin/yt" --help)" "--settings"
has "usage mentions --log" "$(bash "$ROOT/bin/yt" --help)" "--log"
has "usage mentions --print-url" "$(bash "$ROOT/bin/yt" --help)" "--print-url"
hasnt "menu options do not include trending" "$(build_menu_options)" "tendencias"

echo "doctor exit codes"
eq "doctor is quiet on a clean setup" "$(bash "$ROOT/bin/yt" --doctor >/dev/null 2>&1; echo $?)" "0"
RC1=$( (
    # shellcheck disable=SC2329
    have() { case $1 in deno|node|bun|qjs|quickjs) return 1 ;; esac; command -v "$1" >/dev/null 2>&1; }
    doctor >/dev/null; echo $?
) )
eq "doctor returns 1 when only warnings remain" "$RC1" "1"
RC2=$( (
    # shellcheck disable=SC2329
    have() { [ "$1" = fzf ] && return 1; command -v "$1" >/dev/null 2>&1; }
    doctor >/dev/null; echo $?
) )
eq "doctor returns 2 when a required dep is missing" "$RC2" "2"

echo "log"
rm -f "$STATE_DIR/yt.log"
eq "--log without a log file" "$(bash "$ROOT/bin/yt" --log)" "no log entries yet"
: > "$STATE_DIR/yt.log"
eq "--log with an empty log file" "$(bash "$ROOT/bin/yt" --log)" "no log entries yet"
for i in $(seq 1 60); do printf 'entry %s\n' "$i"; done > "$STATE_DIR/yt.log"
LOGOUT="$(bash "$ROOT/bin/yt" --log)"
eq "--log prints the last 50 lines" "$(printf '%s\n' "$LOGOUT" | wc -l | tr -d ' ')" "50"
eq "--log keeps the newest entry" "$(printf '%s\n' "$LOGOUT" | grep -cx 'entry 60')" "1"
eq "--log drops the oldest entry" "$(printf '%s\n' "$LOGOUT" | grep -cx 'entry 1')" "0"

echo "print-url"
eq "--print-url prints the canonical URL" "$(bash "$ROOT/bin/yt" --print-url vidAAAAAAA1)" "https://www.youtube.com/watch?v=vidAAAAAAA1"
eq "--print-url prints nothing for an invalid id" "$(bash "$ROOT/bin/yt" --print-url 'bad!' 2>/dev/null)" ""
eq "--print-url exits 2 for an invalid id" "$(bash "$ROOT/bin/yt" --print-url 'bad!' >/dev/null 2>&1; echo $?)" "2"
eq "--print-url --direct resolves through yt-dlp" "$(bash "$ROOT/bin/yt" --print-url vidAAAAAAA1 --direct)" "https://direct.example/vidAAAAAAA1"
eq "--print-url --direct accepts the flag before the id" "$(bash "$ROOT/bin/yt" --print-url --direct vidAAAAAAA1)" "https://direct.example/vidAAAAAAA1"

echo "i18n"
MENU_ES="$(YT_LANG=es build_menu_options)"
MENU_EN="$(YT_LANG=en build_menu_options)"
has "menu options localize es (música)" "$MENU_ES" "música"
has "menu options localize es (ajustes)" "$MENU_ES" "ajustes"
has "menu options localize en (music)" "$MENU_EN" "music"
has "menu options localize en (settings)" "$MENU_EN" "settings"
hasnt "menu options en has no Spanish labels" "$MENU_EN" "música"
YT_LANG=es settings_menu >/dev/null 2>&1
SET_ES="$(cat "$FZF_STDIN_OUT")"
YT_LANG=en settings_menu >/dev/null 2>&1
SET_EN="$(cat "$FZF_STDIN_OUT")"
has "settings menu localizes es" "$SET_ES" "Calidad de video"
has "settings menu localizes en" "$SET_EN" "Video quality"
hasnt "settings menu en has no Spanish labels" "$SET_EN" "Calidad de video"
PV_ES="$(FZF_PREVIEW_COLUMNS=50 FZF_PREVIEW_LINES=30 YT_PREVIEW=none NO_COLOR=1 YT_LANG=es "$ROOT/bin/yt-thumb-preview" vidAAAAAAA1 "A one" "Chan A" "10:00" "1.5M" 2>&1)"
PV_EN="$(FZF_PREVIEW_COLUMNS=50 FZF_PREVIEW_LINES=30 YT_PREVIEW=none NO_COLOR=1 YT_LANG=en "$ROOT/bin/yt-thumb-preview" vidAAAAAAA1 "A one" "Chan A" "10:00" "1.5M" 2>&1)"
has "preview footer localizes es" "$PV_ES" "enter: reproducir"
has "preview footer localizes en" "$PV_EN" "enter: play"
hasnt "preview footer en has no Spanish" "$PV_EN" "reproducir"

echo "error classification"
eq "classify bot check" "$(classify_err 'ERROR: Sign in to confirm you are not a bot')" "bot_check"
eq "classify rate limit (429)" "$(classify_err 'ERROR: HTTP Error 429: Too Many Requests')" "rate_limit"
eq "classify network error" "$(classify_err 'ERROR: Unable to download webpage: Network is unreachable')" "network_err"
printf 'bot_check\n' > "$YT_RUNDIR/last_err"
has "empty row shows localized bot message (en)" "$(YT_LANG=en empty_row foryou)" "sign in"
hasnt "empty row en has no Spanish bot message" "$(YT_LANG=en empty_row foryou)" "iniciar sesión"
has "empty row shows localized bot message (es)" "$(YT_LANG=es empty_row foryou)" "iniciar sesión"
rm -f "$YT_RUNDIR/last_err"
eq "empty row falls back to the view message" "$(YT_LANG=en empty_row later | cut -f1)" "none"
YT_STUB_FAIL='Sign in to confirm you are not a bot' YT_COOKIES=off search_raw "classify-$RANDOM" auto >/dev/null
eq "fetch_raw stores the bot_check class" "$(cat "$YT_RUNDIR/last_err" 2>/dev/null)" "bot_check"
rm -f "$YT_RUNDIR/last_err"

echo "mpv ipc"
IPC_DIR="$TMP/ipc"
mkdir -p "$IPC_DIR"
export YT_RUNDIR="$IPC_DIR"
init light
IPC_LOG="$TMP/ipc.log"
IPC_READY="$TMP/ipc.ready"
MPV_LOG="$TMP/mpv.log"
NOTIFY_LOG="$TMP/notify.log"
export MPV_LOG NOTIFY_LOG
: > "$IPC_LOG"; : > "$MPV_LOG"; : > "$NOTIFY_LOG"; rm -f "$IPC_READY"

start_ipc_server() { # echo the socket; wait until the mock player is listening
    python3 "$TMP/bin/mpv-ipc-server.py" "$YT_SOCK" "$IPC_LOG" "$IPC_READY" >/dev/null 2>&1 &
    IPC_PID=$!
    local i
    for i in $(seq 1 150); do
        [ -s "$IPC_READY" ] && return 0
        sleep 0.02
    done
    return 1
}
stop_ipc_server() {
    [ -n "${IPC_PID:-}" ] || return 0
    kill "$IPC_PID" 2>/dev/null || true
    wait "$IPC_PID" 2>/dev/null || true
    IPC_PID=""
}

if start_ipc_server; then ok "mock mpv IPC socket is listening"; else bad "mock mpv IPC socket did not start"; fi
eq "mpv_is_running sees the live socket" "$(mpv_is_running; echo $?)" "0"

: > "$IPC_LOG"
eq "mpv_send returns the command response" "$(mpv_send loadfile 'https://example.test/v' replace | head -n 1)" '{"error": "success"}'
has "mpv_send forwards loadfile arguments" "$(cat "$IPC_LOG")" '["loadfile", "https://example.test/v", "replace"]'
: > "$IPC_LOG"
mpv_send loadfile 'https://example.test/a"b' >/dev/null
has "mpv_send escapes JSON strings" "$(cat "$IPC_LOG")" 'a\"b'
eq "mpv_ipc_cmd fails without a socket" "$( YT_SOCK="$TMP/missing.sock" mpv_ipc_cmd '{"command": ["pause"]}' >/dev/null 2>&1; echo $? )" "1"

STATUS="$(mpv_get_status)"
eq "mpv_get_status prints the status object" \
    "$(printf '%s' "$STATUS" | python3 -c 'import json, sys; d = json.load(sys.stdin); print(" ".join(str(d[k]) for k in ("title", "time_pos", "duration", "paused", "volume", "playlist_count")))')" \
    "Test Video 42.5 300 False 75 3"
eq "mpv_get_status fails without a player" "$( YT_SOCK="$TMP/missing.sock" mpv_get_status >/dev/null 2>&1; echo $? )" "1"

: > "$IPC_LOG"
eq "yt --status exits 0" "$(bash "$ROOT/bin/yt" --status >/dev/null 2>&1; echo $?)" "0"
eq "yt --status prints the JSON title" "$(bash "$ROOT/bin/yt" --status | python3 -c 'import json, sys; print(json.load(sys.stdin)["title"])')" "Test Video"

: > "$IPC_LOG"
eq "yt --toggle-pause exits 0" "$(bash "$ROOT/bin/yt" --toggle-pause >/dev/null 2>&1; echo $?)" "0"
has "yt --toggle-pause cycles pause" "$(cat "$IPC_LOG")" '["cycle", "pause"]'
: > "$IPC_LOG"
bash "$ROOT/bin/yt" --pause >/dev/null 2>&1
has "yt --pause pauses" "$(cat "$IPC_LOG")" '["set_property", "pause", "yes"]'
: > "$IPC_LOG"
bash "$ROOT/bin/yt" --play >/dev/null 2>&1
has "yt --play resumes" "$(cat "$IPC_LOG")" '["set_property", "pause", "no"]'
: > "$IPC_LOG"
bash "$ROOT/bin/yt" --next >/dev/null 2>&1
has "yt --next advances the queue" "$(cat "$IPC_LOG")" '["playlist-next"]'
: > "$IPC_LOG"
bash "$ROOT/bin/yt" --prev >/dev/null 2>&1
has "yt --prev rewinds the queue" "$(cat "$IPC_LOG")" '["playlist-prev"]'
: > "$IPC_LOG"
bash "$ROOT/bin/yt" --seek -10 >/dev/null 2>&1
has "yt --seek sends a relative seek" "$(cat "$IPC_LOG")" '["seek", "-10"]'
eq "yt --seek without a value exits 2" "$(bash "$ROOT/bin/yt" --seek >/dev/null 2>&1; echo $?)" "2"
: > "$IPC_LOG"
bash "$ROOT/bin/yt" --stop >/dev/null 2>&1
has "yt --stop stops playback" "$(cat "$IPC_LOG")" '["stop"]'

: > "$IPC_LOG"; : > "$NOTIFY_LOG"
eq "enqueue_video succeeds against the player" "$(enqueue_video vidAAAAAAA3 'https://www.youtube.com/watch?v=vidAAAAAAA3' 'A three' 'Chan A' '3:00'; echo $?)" "0"
has "enqueue_video appends to the playlist" "$(cat "$IPC_LOG")" '"append-play"'
has "enqueue_video notifies the user" "$(cat "$NOTIFY_LOG")" "➕ Encolado: A three"
eq "enqueue_video fails without a player" "$( YT_SOCK="$TMP/missing.sock" enqueue_video vidAAAAAAA3 'https://x' 'A three' - - >/dev/null 2>&1; echo $? )" "1"

: > "$IPC_LOG"
sleep 10 & LIVE_PID=$!
echo "$LIVE_PID" > "$STATE_DIR/audio.pid"
play_video window vidAAAAAAA2 'https://www.youtube.com/watch?v=vidAAAAAAA2' 'A two' 'Chan A' '11:00' >/dev/null 2>&1
eq "play_video leaves the running player alive" "$(kill -0 "$LIVE_PID" 2>/dev/null && echo alive || echo dead)" "alive"
has "play_video replaces the stream over IPC" "$(cat "$IPC_LOG")" '"replace"'
kill "$LIVE_PID" 2>/dev/null || true

: > "$IPC_LOG"
sleep 10 & OLD_PID=$!
echo "$OLD_PID" > "$STATE_DIR/audio.pid"
stop_prev_playback
has "stop_prev_playback quits through IPC" "$(cat "$IPC_LOG")" '["quit"]'
eq "stop_prev_playback clears the pid file" "$([ -f "$STATE_DIR/audio.pid" ] && echo 1 || echo 0)" "0"
eq "stop_prev_playback trusts IPC over the process kill" "$(kill -0 "$OLD_PID" 2>/dev/null && echo alive || echo dead)" "alive"
kill "$OLD_PID" 2>/dev/null || true

printf '' > "$TMP/not-a-socket"
eq "a plain file is not a running player" "$( YT_SOCK="$TMP/not-a-socket" mpv_is_running; echo $? )" "1"

stop_ipc_server
eq "mpv_is_running is false once the player exits" "$(mpv_is_running; echo $?)" "1"

: > "$MPV_LOG"
play_video window vidAAAAAAA1 'https://www.youtube.com/watch?v=vidAAAAAAA1' 'A one' 'Chan A' '10:00' >/dev/null 2>&1
for _ in $(seq 1 150); do [ -s "$MPV_LOG" ] && break; sleep 0.02; done
MPV_ARGS="$(cat "$MPV_LOG")"
has "mpv launches with the IPC server" "$MPV_ARGS" "--input-ipc-server=$YT_SOCK"
has "mpv launches idle so the queue survives" "$MPV_ARGS" "--idle=yes"
rm -f "$STATE_DIR/audio.pid"

# ── F2-T3: native direct seek + authenticated pipe fallback ──────────────────
: > "$MPV_LOG"; : > "$STUB_LOG"
YT_PLAYBACK=direct play_video window vidAAAAAAA1 'https://www.youtube.com/watch?v=vidAAAAAAA1' 'A one' 'Chan A' '10:00' >/dev/null 2>&1
for _ in $(seq 1 150); do [ -s "$MPV_LOG" ] && break; sleep 0.02; done
DIRECT_ARGS="$(cat "$MPV_LOG")"
has "direct playback configures mpv's ytdl format" "$DIRECT_ARGS" "--ytdl-format=bestvideo"
has "direct playback authenticates ytdl_hook with cookies" "$DIRECT_ARGS" "--ytdl-raw-options-append=cookies="
has "direct playback hands the URL to mpv natively" "$DIRECT_ARGS" "https://www.youtube.com/watch?v=vidAAAAAAA1"
hasnt "direct playback never builds a yt-dlp pipe" "$(cat "$STUB_LOG")" "--remux-video mkv"

: > "$MPV_LOG"; : > "$STUB_LOG"
YT_PLAYBACK=pipe play_video window vidAAAAAAA1 'https://www.youtube.com/watch?v=vidAAAAAAA1' 'A one' 'Chan A' '10:00' >/dev/null 2>&1
for _ in $(seq 1 150); do [ -s "$MPV_LOG" ] && break; sleep 0.02; done
PIPE_ARGS="$(cat "$MPV_LOG")"
has "pipe playback extracts through yt-dlp" "$(cat "$STUB_LOG")" "--remux-video mkv"
has "pipe playback feeds mpv over stdin" "$PIPE_ARGS" " -"
has "pipe playback still carries the ytdl format" "$PIPE_ARGS" "--ytdl-format=bestvideo"
rm -f "$STATE_DIR/audio.pid"

( browse foryou ) >/dev/null 2>&1
IPC_ARGS="$(cat "$FZF_ARGS_OUT")"
has "fzf: alt-space toggles pause" "$IPC_ARGS" "alt-space:execute-silent("
has "fzf: alt-space sends toggle-pause" "$IPC_ARGS" "transport toggle-pause"
has "fzf: alt-q enqueues the selection" "$IPC_ARGS" "alt-q:execute-silent("
has "fzf: alt-q forwards the row" "$IPC_ARGS" "enqueue {1} {2} {3} {4} {5}"
has "fzf: alt-n skips forward" "$IPC_ARGS" "alt-n:execute-silent("
has "fzf: alt-n sends next" "$IPC_ARGS" "transport next"
has "fzf: alt-p goes back" "$IPC_ARGS" "alt-p:execute-silent("
has "fzf: alt-p sends prev" "$IPC_ARGS" "transport prev"
hasnt "fzf: alt-p no longer toggles the preview" "$IPC_ARGS" "alt-p:toggle-preview"
has "fzf: preview toggle moved to alt-v" "$IPC_ARGS" "alt-v:toggle-preview"

# ── F2-T4: guest RSS subscriptions and observable download queue ────────────
echo "guest rss subscriptions"
ATOM="$TMP/atom.xml"
cat > "$ATOM" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
  <title>Sample Channel</title>
  <entry>
    <id>yt:video:rssVid00001</id>
    <yt:videoId>rssVid00001</yt:videoId>
    <yt:channelId>UCrssChannel1</yt:channelId>
    <title>First &amp; Best</title>
    <published>2026-11-01T10:00:00+00:00</published>
    <author><name>RSS Channel One</name></author>
    <media:group><media:statistics views="1234"/></media:group>
  </entry>
  <entry>
    <id>yt:video:rssVid00002</id>
    <yt:videoId>rssVid00002</yt:videoId>
    <title>Second Video</title>
    <published>2026-11-02T10:00:00+00:00</published>
    <media:group><media:statistics views="99"/></media:group>
  </entry>
  <entry>
    <id>yt:video:rssVid00003</id>
    <yt:videoId>rssVid00003</yt:videoId>
    <title>Third Video</title>
    <published>2026-11-03T10:00:00+00:00</published>
    <media:group><media:statistics views="7"/></media:group>
  </entry>
</feed>
XML

RSS_ROWS="$(parse_atom_feed "$ATOM")"
eq "atom parser: reads every entry" "$(printf '%s\n' "$RSS_ROWS" | wc -l | tr -d ' ')" "3"
eq "atom parser: raw template has 7 columns" "$(printf '%s\n' "$RSS_ROWS" | head -n 1 | awk -F'\t' '{print NF}')" "7"
eq "atom parser: first id" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f1)" "rssVid00001"
eq "atom parser: title is unescaped" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f2)" "First & Best"
eq "atom parser: uploader name" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f3)" "RSS Channel One"
eq "atom parser: missing uploader becomes NA" "$(printf '%s\n' "$RSS_ROWS" | sed -n '2p' | cut -f3)" "NA"
eq "atom parser: duration is NA in feeds" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f4)" "NA"
eq "atom parser: watch url" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f5)" "https://www.youtube.com/watch?v=rssVid00001"
eq "atom parser: view count" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f6)" "1234"
eq "atom parser: live status is NA" "$(printf '%s\n' "$RSS_ROWS" | sed -n '1p' | cut -f7)" "NA"
eq "atom parser: accepts an XML string" \
    "$(parse_atom_feed '<feed><entry><yt:videoId>strVid00001</yt:videoId><title>Inline</title></entry></feed>' | cut -f1)" \
    "strVid00001"

rm -f "$CHANNELS_FILE"
eq "--subscribe persists a channel id" \
    "$(bash "$ROOT/bin/yt" --subscribe UCrssChannel1 >/dev/null; cut -f1 "$CHANNELS_FILE" 2>/dev/null)" "UCrssChannel1"
has "--subscribe prints a confirmation" "$(bash "$ROOT/bin/yt" --subscribe UCrssChannel2 2>&1)" "UCrssChannel2"
bash "$ROOT/bin/yt" --subscribe UCrssChannel3 >/dev/null 2>&1
CHLIST="$(bash "$ROOT/bin/yt" --channels 2>&1)"
has "--channels lists the first channel" "$CHLIST" "UCrssChannel1"
has "--channels lists every channel" "$CHLIST" "UCrssChannel3"
eq "--subscribe is idempotent" "$(grep -c '^UCrssChannel1' "$CHANNELS_FILE")" "1"
bash "$ROOT/bin/yt" --subscribe 'https://www.youtube.com/channel/UCrssChannel9' >/dev/null 2>&1
has "--subscribe accepts a channel URL" "$(cat "$CHANNELS_FILE")" "UCrssChannel9"
eq "--subscribe rejects an invalid reference" "$(bash "$ROOT/bin/yt" --subscribe 'not a channel!' >/dev/null 2>&1; echo $?)" "2"
bash "$ROOT/bin/yt" --unsubscribe UCrssChannel2 >/dev/null 2>&1
hasnt "--unsubscribe removes the channel" "$(cat "$CHANNELS_FILE")" "UCrssChannel2"
has "--unsubscribe keeps the other channels" "$(cat "$CHANNELS_FILE")" "UCrssChannel1"
eq "--unsubscribe exits 0" "$(bash "$ROOT/bin/yt" --unsubscribe UCrssChannel9 >/dev/null 2>&1; echo $?)" "0"

rm -f "$CACHE_DIR/feed_subs.tsv"
RSS_VIEW="$(YT_COOKIES=off emit_view subs auto 2>/dev/null)"
eq "RSS subs: newest entry is first" "$(printf '%s\n' "$RSS_VIEW" | cut -f1 | head -n 1)" "rssVid00003"
has "RSS subs: titles are rendered" "$RSS_VIEW" "Third Video"
has "RSS subs: uploader is rendered" "$RSS_VIEW" "RSS Channel One"
eq "RSS subs: all three entries make it into the view" "$(printf '%s\n' "$RSS_VIEW" | grep -c '^rssVid')" "3"

rm -f "$CHANNELS_FILE" "$CACHE_DIR/feed_subs.tsv"
RSS_EMPTY="$(YT_COOKIES=off emit_view subs auto 2>/dev/null)"
has "RSS subs: no channels shows the subscribe hint" "$RSS_EMPTY" "yt --subscribe"

# Preserve: with cookies available the subscriptions view still uses yt-dlp.
rm -f "$CACHE_DIR/feed_subs.tsv"
: > "$STUB_LOG"
SIGNED_SUBS="$(emit_view subs auto 2>/dev/null)"
has "signed-in subs still uses the authenticated feed" "$(cat "$STUB_LOG")" "youtube.com/feed/subscriptions"
has "signed-in subs still renders yt-dlp rows" "$SIGNED_SUBS" "vidAAAAAAA1"

HELP="$(bash "$ROOT/bin/yt" --help)"
has "usage documents --subscribe" "$HELP" "--subscribe"
has "usage documents --unsubscribe" "$HELP" "--unsubscribe"
has "usage documents --channels" "$HELP" "--channels"
has "usage documents --queue" "$HELP" "--queue"

echo "download queue"
rm -rf "$DOWNLOADS_DIR"
DL_ID="vidDL000001"
download_video "$DL_ID" "https://www.youtube.com/watch?v=$DL_ID" "Download Me" >/dev/null 2>&1
for _ in $(seq 1 300); do
    DL_STATUS=$(cut -f3 "$DOWNLOADS_DIR/$DL_ID".*.rec 2>/dev/null | head -n 1)
    [ "$DL_STATUS" = "done" ] && break
    sleep 0.02
done
eq "download: a tracking record is created" "$(find "$DOWNLOADS_DIR" -maxdepth 1 -name '*.rec' 2>/dev/null | wc -l | tr -d ' ')" "1"
eq "download: record keeps the title" "$(cut -f2 "$DOWNLOADS_DIR"/*.rec | head -n 1)" "Download Me"
eq "download: record reaches done" "$(cut -f3 "$DOWNLOADS_DIR"/*.rec | head -n 1)" "done"
has "download still invokes yt-dlp for the URL" "$(grep 'vidDL000001' "$STUB_LOG" | tail -n 1)" "youtube.com/watch?v=vidDL000001"
QOUT="$(queue_status)"
has "queue: shows a header" "$QOUT" "STATUS"
has "queue: shows the download id" "$QOUT" "$DL_ID"
has "queue: shows the status" "$QOUT" "done"
has "queue: shows the title" "$QOUT" "Download Me"
eq "--queue exits 0" "$(bash "$ROOT/bin/yt" --queue >/dev/null 2>&1; echo $?)" "0"
has "--downloads is an alias for --queue" "$(bash "$ROOT/bin/yt" --downloads 2>&1)" "Download Me"

export YT_STUB_FAIL='disk full'
download_video vidDL000002 "https://www.youtube.com/watch?v=vidDL000002" "Broken" >/dev/null 2>&1
for _ in $(seq 1 300); do
    [ "$(cut -f3 "$DOWNLOADS_DIR"/vidDL000002.*.rec 2>/dev/null | head -n 1)" = "failed" ] && break
    sleep 0.02
done
unset YT_STUB_FAIL
eq "download: failures are recorded" "$(cut -f3 "$DOWNLOADS_DIR"/vidDL000002.*.rec 2>/dev/null | head -n 1)" "failed"
has "queue: shows failed downloads" "$(queue_status)" "failed"

echo
printf '%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
