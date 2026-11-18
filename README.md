<p align="center">
  <img src="assets/banner.svg" alt="iutui" width="760"/>
</p>

<h3 align="center">Fast, keyboard-first YouTube in your terminal — zero browser bloat.</h3>

<p align="center">
  Native Go YouTube TUI &amp; CLI powered by <code>mpv</code> IPC and <code>yt-dlp</code>, featuring HD Kitty/Ghostty thumbnails, sub-3s cookieless Android playback, continuous Autoplay, SponsorBlock, and live synced lyrics. Browse feeds, follow channels via local RSS without an account, and control playback or background downloads without leaving the keyboard.
</p>

---

## Quick Install

**With Go (`1.22+`)**

```sh
go install github.com/e12jassir/iutui/cmd/iutui@latest
```

**Prebuilt binaries**

Download the tarball for your platform (`linux` or `darwin`, `amd64` or `arm64`) from [GitHub Releases](https://github.com/e12jassir/iutui/releases), extract `iutui` onto your `PATH`, and make it executable.

**Build from source**

```sh
git clone https://github.com/e12jassir/iutui && cd iutui
make install          # installs to ~/.local/bin by default (override with PREFIX=...)
```

*(Alternatively run `./install.sh`, which also seeds `~/.config/iutui/config` if missing.)*

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
| `deno` or `node` | Optional | JS runtime for `yt-dlp` web extractor fallbacks (cookieless Android playback works without one) |
| Kitty / Ghostty or `chafa` | Optional | HD inline thumbnails via Kitty graphics protocol or terminal fallback |
| `wl-copy` / `xclip` / `pbcopy` | Optional | Clipboard support on Wayland, X11, or macOS |

---

## Highlights

- **Sub-3s playback** — Uses a cookieless `player_client=android` fast path paired with async lookahead prefetching so videos start almost instantly.
- **Data Saver & Bounded Cache** — Defaults to `720p` HD; press `[c]` to cycle down to `360p` / `480p` / `1080p` / `MAX`, switch to audio-only (`~55 MB/h`) with `[a]` or `[V]`, and keep `mpv`'s demuxer cache bounded to `24 MiB`.
- **Continuous Autoplay & Mix Radio** — Nonstop queue continuation and YouTube Mix Radio (`[m]`) guarded by an automatic `"Still watching?"` idle pause prompt.
- **Native SponsorBlock (`[B]`)** — Fetches `sponsor` and `selfpromo` segments and skips them seamlessly over `mpv` IPC.
- **Live Synced Lyrics (`[L]`) & Interactive Chapters (`[C]`)** — Time-synced lyrics powered by LRCLIB alongside a jumpable video chapter overlay.
- **Channel Drill-Down (`[g]`) & One-Key Local RSS Subscriptions (`[s]`)** — Open any highlighted video's channel feed or subscribe locally via YouTube Atom feeds with no account required.
- **5 Color Themes (`[t]`) + `NO_COLOR`** — Cycle live between `youtube-dark`, `catppuccin`, `tokyonight`, `gruvbox`, and `kanagawa`, with automatic `NO_COLOR` support.

---

## Keybindings

| Key | Action | Key | Action |
|---|---|---|---|
| `j` / `k` / `h` / `l` / Arrows | Navigate grid, list, or sidebar | `w` | Save / unsave (`★` Watch Later) |
| `Enter` | Play (`auto` mode) | `s` | Subscribe / unsubscribe channel (RSS) |
| `a` / `v` | Play audio-only / video window | `g` | Open highlighted video's channel |
| `V` | Toggle live video/audio mode | `q` | Enqueue video to `mpv` playlist |
| `m` | Play YouTube Mix Radio | `d` | Download video in background |
| `Space` | Pause / resume playback | `C` | Open interactive chapters overlay |
| `n` / `p` | Next / previous item in queue | `L` | Toggle live synced lyrics (LRCLIB) |
| `,` / `.` | Seek `-10s` / `+10s` | `B` | Toggle SponsorBlock skipping |
| `-` / `+` | Volume `-5%` / `+5%` | `t` | Cycle color theme |
| `/` | Focus search bar | `c` | Cycle quality (`720p`/`480p`/`360p`/`1080p`/`MAX`) |
| `[` / `]` | Cycle local filter chips | `A` | Toggle continuous Autoplay |
| `1`..`5` | Switch tabs (Home, Music, Subs, History, Downloads) | `r` | Force refresh current view |
| `S` | Toggle settings modal | `?` | Open help overlay |
| `Esc` | Close overlay / return focus | `Q` | Quit `iutui` |

*(Press `Tab` to move focus between the main content area and the sidebar.)*

---

## CLI Subcommands

| Command | Description |
|---|---|
| `iutui` | Launch the interactive TUI |
| `iutui -q` | Open the fast interactive quick selector |
| `iutui play [-a\|-m\|--pip\|--fs] <id\|url>` | Play a video or URL directly (`-a` audio, `-m` Mix Radio, `--pip`, `--fs`) |
| `iutui status [--json\|--waybar]` | Print live `mpv` playback state (`plain`, `--json`, or `--waybar`) |
| `iutui seek <seconds>` | Seek relative seconds in the active `mpv` instance (e.g. `-10`, `30`) |
| `iutui pause\|next\|prev` | Toggle pause or skip tracks via `mpv` IPC |
| `iutui subscribe <id\|handle\|url>` | Subscribe to a channel via local RSS |
| `iutui channels` | List subscribed channels |
| `iutui queue` | Show background and completed downloads |
| `iutui doctor` | Check dependencies and print resolved XDG & socket paths |

---

## Data Usage Reference

| Quality Preset | Approximate Usage | Notes |
|---|---|---|
| `4K` (`MAX` / `2160p`) | `~15 GB/h` | Maximum resolution when available |
| `720p` HD *(default)* | `~1.2 GB/h` | Balanced clarity and bandwidth |
| `360p` | `~300 MB/h` | Data-saver preset via `[c]` |
| Audio-only (`[a]` / `[V]`) | `~55 MB/h` | Ideal for music, podcasts, and background playback |

---

## XDG Paths & Migration

`iutui` follows the XDG Base Directory specification:

| Purpose | Default Path | Override Env Var |
|---|---|---|
| Configuration | `~/.config/iutui` | `$XDG_CONFIG_HOME/iutui` |
| Cache & thumbnails | `~/.cache/iutui` | `$XDG_CACHE_HOME/iutui` |
| State, RSS subs & downloads | `~/.local/state/iutui` | `$XDG_STATE_HOME/iutui` |
| Runtime `mpv` IPC socket | `$XDG_RUNTIME_DIR/iutui/mpv.sock` | Falls back to `/tmp/iutui-<uid>/mpv.sock` |

**Automatic one-time migration:** On first launch, `iutui` automatically copies your subscriptions, watch-later list, history, download records, and config from legacy `ytx` and `yt` directories (`~/.config/yt`, `~/.cache/yt_terminal`, `~/.local/state/yt`, and `ytx` XDG folders). Legacy files are never moved or deleted, and browser cookie jars are skipped for clean isolation.

---

<details>
<summary><strong>Configuration (<code>~/.config/iutui/config</code>)</strong></summary>

`iutui` reads plain `KEY=value` lines from `~/.config/iutui/config` (never sourced as shell code). Environment variables always take precedence. See [`config.example`](config.example) for the complete annotated template.

Key defaults:

```ini
YT_QUALITY=720
YT_SPONSORBLOCK=1
YT_THEME=youtube-dark
YT_DEFAULT_MODE=window
YT_COOKIES=auto
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
make test    # run bash smoke tests and Go unit tests
make lint    # bash syntax check, shellcheck, and python py_compile
make verify  # run lint + full test suite
```

</details>

MIT licensed.
