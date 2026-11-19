<p align="center">
  <img src="assets/banner.svg" alt="iutui" width="760"/>
</p>

<h3 align="center">Fast, keyboard-first YouTube in your terminal — 100% pure Go, zero browser bloat.</h3>

<p align="center">
  Pure-Go YouTube TUI &amp; CLI (v0.5.0) powered by <code>mpv</code> IPC and <code>yt-dlp</code>, featuring HD Kitty/Ghostty thumbnails, sub-3s cookieless Android playback, Linux MPRIS2 D-Bus media keys, live queue management, search autocomplete, smart auto-resume, SponsorBlock, and live synced lyrics.
</p>

---

## Quick Install

**With Go (`1.24+`)**

```sh
go install github.com/e12jassir/iutui/cmd/iutui@latest
```

**Prebuilt binaries**

Download the archive for your platform (`linux` or `darwin`, `amd64` or `arm64`) from [GitHub Releases](https://github.com/e12jassir/iutui/releases), extract `iutui` onto your `PATH`, and make it executable.

**Build from source**

```sh
git clone https://github.com/e12jassir/iutui && cd iutui
make install          # builds bin/iutui and installs iutui (+ uitui alias) to ~/.local/bin
```

*(Alternatively run `./install.sh`, which also removes legacy Bash helpers if present and seeds `~/.config/iutui/config` when missing.)*

**Verify your setup**

```sh
iutui doctor
```

---

## Requirements

| Dependency | Status | Purpose |
|---|---|---|
| [`mpv`](https://mpv.io/) | Required | Hardware-accelerated video/audio playback and JSON IPC transport |
| [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) | Required | Stream resolution, metadata, search, and background downloads (`yt-dlp -U` to keep current) |
| `deno` / `node` / `bun` | Optional | JS runtime for `yt-dlp` web extractor fallbacks (cookieless Android playback works without one) |
| Kitty / Ghostty or `chafa` | Optional | HD inline thumbnails via Kitty graphics protocol or ANSI terminal fallback |
| `playerctl` / D-Bus (Linux) | Optional | Hardware media keys and desktop media controls via MPRIS2 (`org.mpris.MediaPlayer2.iutui`) |
| `wl-copy` / `xclip` / `pbcopy` | Optional | Clipboard support on Wayland, X11, or macOS |

---

## Highlights (v0.5.0)

- **100% Pure Go Architecture** — Zero legacy Bash scripts or Python helpers. Single static binary built with Bubble Tea, Lip Gloss, `mpv` JSON IPC, native RSS Atom feeds, and structured Go worker pools.
- **Sub-3s Playback & Lookahead Prefetch** — Uses a cookieless `player_client=android` fast path paired with async stream prefetching so videos start almost instantaneously.
- **Linux MPRIS2 D-Bus Media Keys** — Publishes playback metadata, position, rate, and transport controls over `org.mpris.MediaPlayer2.iutui` so desktop widgets, lock screens, `playerctl`, and hardware media keys work seamlessly.
- **Live Queue Overlay (`u`)** — Open the active `mpv` playlist queue with `u`, jump directly to any track (`Enter` / `Space`), remove entries (`x` / `d` / `Backspace`), or clear the queue (`c`), and enqueue tracks anytime with `q`.
- **Search Autocomplete & Recent History (`/`)** — Press `/` to focus search, browse recent queries (`search_history.tsv`) or live YouTube suggestions with `↓` / `↑`, complete into the input with `Tab`, and submit with `Enter`.
- **Playback Speed (`{` / `}` / `0`) & Single-Track Loop (`R`)** — Step speed between `0.5x` and `2.0x` (`{` / `}`), snap back to `1.0x` (`0`), and toggle single-track repeat (`R`).
- **Smart Auto-Resume (`>= 15m`)** — Videos 15 minutes or longer automatically save timestamps (`resume.json`) and resume right where you left off.
- **Live Download Progress & Offline Local Playback** — Background downloads (`d`) display live percentage, speed, and ETA updates; playing an already-downloaded video automatically plays the local file offline.
- **Liked (`W`) vs. Watch Later (`w`)** — Separate curated lists (`liked.tsv` and `watchlater.tsv`), both browsable from the sidebar alongside `Home`, `Music`, `Subscriptions`, `History`, `Playlists`, and `Downloads`.
- **Data Saver, SponsorBlock (`B`) & Synced Lyrics (`L`)** — Quality selector modal (`f`) or quick preset cycle (`c`), live video/audio toggle (`V` / `x`), bounded `24 MiB` demuxer cache, SponsorBlock segment skipping (`B`), LRCLIB synced lyrics (`L`), interactive chapters (`o` / `C`), and `"Still watching?"` idle guard (`A`).
- **6 Color Themes (`t`) + `NO_COLOR`** — Cycle live across `youtube-dark`, `catppuccin`, `tokyonight`, `gruvbox`, `kanagawa`, and `terminal`, with automatic `NO_COLOR` support.
- **Structured Debug Logging (`--debug-log`)** — Write structured JSON `slog` diagnostics to `$XDG_STATE_HOME/iutui/debug.log` (`0600`) for troubleshooting startup, MPRIS2, and TUI lifecycle events.

---

## Keybindings

| Key | Action | Key | Action |
|---|---|---|---|
| `j` / `k` / `h` / `l` / Arrows | Navigate grid, list, or sidebar | `w` | Save / remove in Watch Later |
| `Enter` | Play (`auto` mode) | `W` | Like / unlike video (`liked.tsv`) |
| `a` / `v` | Play audio-only / video window | `s` | Subscribe / unsubscribe channel (RSS) |
| `V` / `x` | Toggle live video/audio mode | `g` / `G` | Open highlighted video's channel |
| `m` | Play YouTube Mix Radio | `q` | Enqueue video to `mpv` playlist |
| `Space` | Pause / resume playback | `u` | Open Live Queue overlay |
| `n` / `p` | Next / previous item in queue | `d` | Download video in background |
| `,` / `.` (`<` / `>`) | Seek `-10s` / `+10s` | `o` / `O` / `C` | Open interactive chapters overlay |
| `-` / `+` (`=`) | Volume `-5%` / `+5%` | `l` / `L` | Toggle live synced lyrics (LRCLIB) |
| `{` / `}` / `0` | Playback speed `-0.25x` / `+0.25x` / `1.0x` | `b` / `B` | Toggle SponsorBlock skipping |
| `R` | Toggle single-track loop | `t` / `T` | Cycle color theme |
| `/` (`Tab` / `↓` / `↑`) | Focus search · cycle/complete suggestions | `f` / `F` | Open Quality & Audio-Only modal |
| `[` / `]` | Cycle local filter chips | `c` | Cycle quality (`MAX`/`1080p`/`720p`/`480p`/`360p`/Audio) |
| `1`..`5` | Quick-switch tabs (Home, Music, Subs, History, Downloads) | `A` | Toggle continuous Autoplay |
| `Tab` | Switch focus between main view and sidebar (all 8 tabs) | `r` | Force refresh current view |
| `S` | Toggle settings modal | `?` | Open help overlay |
| `Esc` | Close overlay / return focus | `Q` | Quit `iutui` |

*(Mouse clicks and scrolling are supported across cards, sidebar tabs, overlays, and the player bar.)*

---

## CLI Subcommands & Flags

| Command | Description |
|---|---|
| `iutui` | Launch the interactive TUI |
| `iutui --debug-log` | Enable JSON `slog` diagnostics in `$XDG_STATE_HOME/iutui/debug.log` (global persistent flag) |
| `iutui --version` | Print the installed version (`iutui version 0.5.0`) |
| `iutui -q` (`--quick`) | Open the fast interactive quick selector |
| `iutui play [-a\|-m\|--pip\|--fs] <id\|url>` | Play a video or URL directly (`-a` audio, `-m` Mix Radio, `--pip`, `--fs`) |
| `iutui status [--json\|--waybar]` | Print live `mpv` playback state (`plain`, `--json`, or `--waybar`) |
| `iutui seek <seconds>` | Seek relative seconds in the active `mpv` instance (e.g. `-10`, `30`) |
| `iutui pause\|next\|prev` | Toggle pause or skip tracks via `mpv` IPC |
| `iutui subscribe <id\|handle\|url>` | Subscribe to a channel via local RSS |
| `iutui channels` | List subscribed channels |
| `iutui queue` | Show background and completed downloads |
| `iutui doctor` | Check dependencies (`mpv`, `yt-dlp`, `chafa`) and print resolved XDG & socket paths |

---

## Data Usage Reference

| Quality Preset | Approximate Usage | Notes |
|---|---|---|
| `4K` (`MAX` / `2160p`) | `~15 GB/h` | Maximum resolution when available |
| `1080p` Full HD | `~2.5 GB/h` | High definition |
| `720p` HD *(default)* | `~1.0–1.2 GB/h` | Balanced clarity and bandwidth |
| `360p` Data Saver | `~250–300 MB/h` | Low-bandwidth preset via `[f]` or `[c]` |
| Audio-only (`[a]` / `[V]` / `[x]`) | `~50–55 MB/h` | Ideal for music, podcasts, and background playback |

---

## XDG Paths & Migration

`iutui` follows the XDG Base Directory specification:

| Purpose | Default Path | Override Env Var |
|---|---|---|
| Configuration (`config`) | `~/.config/iutui` | `$XDG_CONFIG_HOME/iutui` |
| Cache & thumbnails (`feed_*.json`, `video_meta.json`, `cookies.txt`) | `~/.cache/iutui` | `$XDG_CACHE_HOME/iutui` |
| State (`subscriptions.tsv`, `watchlater.tsv`, `liked.tsv`, `history.tsv`, `resume.json`, `search_history.tsv`, `downloads/`, `debug.log`) | `~/.local/state/iutui` | `$XDG_STATE_HOME/iutui` |
| Runtime `mpv` IPC socket | `$XDG_RUNTIME_DIR/iutui/mpv.sock` | Falls back to `/tmp/iutui-<uid>/mpv.sock` |

**Automatic one-time migration:** On first launch, `iutui` automatically copies subscriptions, watch-later lists, history, download records, and config from legacy `ytx` and `yt` directories (`~/.config/yt`, `~/.cache/yt_terminal`, `~/.local/state/yt`, and `ytx` XDG folders). Legacy files are never moved or deleted, and browser cookie jars are skipped for clean isolation.

---

<details>
<summary><strong>Configuration (<code>~/.config/iutui/config</code>)</strong></summary>

`iutui` reads plain `KEY=value` lines from `~/.config/iutui/config` (never executed as shell code). Environment variables always take precedence. See [`config.example`](config.example) for the complete annotated template.

Key defaults:

```ini
YT_QUALITY=720
YT_SPONSORBLOCK=1
YT_THEME=youtube-dark       # youtube-dark | catppuccin | tokyonight | gruvbox | kanagawa | terminal
YT_DEFAULT_MODE=window      # window | pip | fs | audio
YT_COOKIES=auto             # auto | off (guest: search + local RSS subscriptions)
```

</details>

<details>
<summary><strong>Privacy &amp; Cookie Isolation</strong></summary>

- Playback uses the cookieless `player_client=android` fast path by default for single videos.
- When authenticated feeds are enabled (`YT_COOKIES=auto`), browser cookies are read at most once per `YT_COOKIE_TTL` (6 hours by default), filtered strictly to Google/YouTube domains, and written with `0600` permissions.
- Set `YT_COOKIES=off` for pure guest mode (search + local RSS subscriptions + cookieless playback).

</details>

<details>
<summary><strong>Development &amp; Verification</strong></summary>

```sh
make build   # compile pure-Go binary to bin/iutui
make lint    # verify gofmt cleanliness and run go vet ./...
make test    # run go test -race ./...
make verify  # run lint + full race test suite
```

Cross-compile verification:

```sh
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o /dev/null ./cmd/iutui
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o /dev/null ./cmd/iutui
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/iutui
```

</details>

MIT licensed.
