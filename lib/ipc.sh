# lib/ipc.sh — mpv IPC transport: UNIX socket commands, status queries and queue control
# Shellcheck source=/dev/null
#
# Socket path: YT_SOCK="${YT_RUNDIR}/mpv.sock" (assigned by init() once the run dir exists).
# Every call is best-effort: a dead or stale socket never takes the caller down.

_ipc_sock() {
    local sock=${YT_SOCK:-}
    [ -n "$sock" ] || sock=${YT_RUNDIR:+$YT_RUNDIR/mpv.sock}
    printf '%s' "$sock"
}

_ipc_python() { # -> a usable python interpreter (empty + rc 1 when none)
    local py=${PYTHON:-python3}
    if have "$py"; then printf '%s' "$py"; return 0; fi
    for py in python3 python; do
        have "$py" && { printf '%s' "$py"; return 0; }
    done
    return 1
}

mpv_ipc_cmd() { # raw JSON -> JSON response (1.0s timeout, rc 1 when unreachable)
    local json=${1:-} sock py out
    [ -n "$json" ] || return 1
    sock=$(_ipc_sock)
    [ -n "$sock" ] && [ -S "$sock" ] || return 1
    py=$(_ipc_python)
    if [ -n "$py" ]; then
        out=$("$py" - "$sock" "$json" <<'PY' 2>/dev/null
import socket
import sys

sock_path, payload = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(1.0)
buf = b""
try:
    s.connect(sock_path)
    s.sendall(payload.encode("utf-8") + b"\n")
    while b"\n" not in buf:
        chunk = s.recv(65536)
        if not chunk:
            break
        buf += chunk
finally:
    s.close()
sys.stdout.write(buf.decode("utf-8", "replace").strip())
PY
        ) || return 1
    elif have nc; then
        out=$(printf '%s\n' "$json" | nc -U -w 1 "$sock" 2>/dev/null) || return 1
    else
        return 1
    fi
    [ -n "$out" ] || return 1
    printf '%s\n' "$out"
}

mpv_is_running() { # 0 when the socket exists and answers an IPC query
    local sock
    sock=$(_ipc_sock)
    [ -n "$sock" ] && [ -S "$sock" ] || return 1
    mpv_ipc_cmd '{"command": ["get_property", "pause"]}' >/dev/null 2>&1
}

_ipc_json_string() {
    local s=${1//\\/\\\\}
    s=${s//\"/\\\"}
    s=${s//$'\n'/\\n}
    s=${s//$'\t'/\\t}
    s=${s//$'\r'/\\r}
    printf '"%s"' "$s"
}

_ipc_json_command() { # command [args...] -> {"command": [...]} (no python fallback)
    local out="" a
    for a in "$@"; do out="${out:+$out, }$(_ipc_json_string "$a")"; done
    printf '{"command": [%s]}' "$out"
}

mpv_send() { # command [args...] -> JSON response
    local cmd=${1:-} payload py
    [ -n "$cmd" ] || return 1
    shift || true
    py=$(_ipc_python)
    if [ -n "$py" ]; then
        payload=$("$py" -c 'import json, sys
sys.stdout.write(json.dumps({"command": sys.argv[1:]}))' "$cmd" "$@" 2>/dev/null) || return 1
    else
        payload=$(_ipc_json_command "$cmd" "$@")
    fi
    [ -n "$payload" ] || return 1
    mpv_ipc_cmd "$payload"
}

mpv_property() { # property -> scalar value (rc 1 when the property is unavailable)
    local name=${1:-} resp py
    [ -n "$name" ] || return 1
    resp=$(mpv_send get_property "$name") || return 1
    py=$(_ipc_python)
    [ -n "$py" ] || return 1
    printf '%s' "$resp" | "$py" -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(1)
if d.get("error") != "success":
    sys.exit(1)
v = d.get("data")
if v is None:
    print("")
elif isinstance(v, bool):
    print("true" if v else "false")
else:
    print(v)
' 2>/dev/null
}

mpv_get_status() { # -> JSON {title, time_pos, duration, paused, volume, playlist_count}
    local title tpos dur paused vol pl py
    mpv_is_running || return 1
    title=$(mpv_property media-title) || title=""
    tpos=$(mpv_property time-pos) || tpos=""
    dur=$(mpv_property duration) || dur=""
    paused=$(mpv_property pause) || paused=""
    vol=$(mpv_property volume) || vol=""
    pl=$(mpv_property playlist-count) || pl=""
    py=$(_ipc_python)
    [ -n "$py" ] || return 1
    "$py" - "$title" "$tpos" "$dur" "$paused" "$vol" "$pl" <<'PY'
import json
import sys

title, tpos, dur, paused, vol, pl = sys.argv[1:7]


def num(value):
    try:
        f = float(value)
        return int(f) if f.is_integer() else f
    except (TypeError, ValueError):
        return None


def boolean(value):
    return {"true": True, "yes": True, "false": False, "no": False}.get(str(value).lower())


def integer(value):
    try:
        return int(float(value))
    except (TypeError, ValueError):
        return None


print(json.dumps({
    "title": title or None,
    "time_pos": num(tpos),
    "duration": num(dur),
    "paused": boolean(paused),
    "volume": num(vol),
    "playlist_count": integer(pl),
}))
PY
}
