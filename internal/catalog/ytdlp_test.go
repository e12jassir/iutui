package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestBuildFormat(t *testing.T) {
	tests := []struct {
		name    string
		quality int
		want    string
	}{
		{
			name:    "zero quality",
			quality: 0,
			want:    "bestvideo+bestaudio/best",
		},
		{
			name:    "negative quality",
			quality: -1080,
			want:    "bestvideo+bestaudio/best",
		},
		{
			name:    "720p",
			quality: 720,
			want:    "bestvideo[height<=720]+bestaudio/best[height<=720]/best",
		},
		{
			name:    "1080p",
			quality: 1080,
			want:    "bestvideo[height<=1080]+bestaudio/best[height<=1080]/best",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildFormat(tc.quality)
			if got != tc.want {
				t.Fatalf("BuildFormat(%d) = %q, want %q", tc.quality, got, tc.want)
			}
		})
	}
}

func TestClassifyErr(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ErrKind
	}{
		{
			name:   "empty",
			stderr: "",
			want:   ErrUnknown,
		},
		{
			name:   "rate limit 429",
			stderr: "ERROR: HTTP Error 429: Too Many Requests",
			want:   ErrRateLimit,
		},
		{
			name:   "forbidden 403",
			stderr: "ERROR: unable to download video data: HTTP Error 403: Forbidden",
			want:   ErrForbidden,
		},
		{
			name:   "auth sign in",
			stderr: "ERROR: Sign in to confirm you're not a bot. Use --cookies-from-browser",
			want:   ErrAuth,
		},
		{
			name:   "auth private video",
			stderr: "ERROR: Private video. Sign in if you've been granted access",
			want:   ErrAuth,
		},
		{
			name:   "network timeout",
			stderr: "ERROR: Unable to download webpage: <urlopen error timed out>",
			want:   ErrNetwork,
		},
		{
			name:   "network dns",
			stderr: "ERROR: dial tcp: lookup www.youtube.com: no such host",
			want:   ErrNetwork,
		},
		{
			name:   "unknown error",
			stderr: "ERROR: Unsupported URL: https://example.com/nope",
			want:   ErrUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyErr(tc.stderr)
			if got != tc.want {
				t.Fatalf("ClassifyErr(%q) = %v, want %v", tc.stderr, got, tc.want)
			}
		})
	}
}

func writeFakeYTDLP(t *testing.T, dir, script string) string {
	t.Helper()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"+script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return bin
}

func TestYTDLPCatalog_CacheAndFallback(t *testing.T) {
	cachedVideos := []model.Video{
		{ID: "cached123", Title: "Cached Title", Channel: "Cached Chan", Duration: "04:10", Views: "10K vistas"},
	}

	tests := []struct {
		name        string
		setupCache  bool
		staleCache  bool
		binScript   string
		binExitFail bool
		wantErr     bool
		wantFirstID string
		wantRefetch bool
	}{
		{
			name:        "fresh cache hit skips yt-dlp",
			setupCache:  true,
			staleCache:  false,
			binExitFail: true,
			wantFirstID: "cached123",
		},
		{
			name:        "stale cache serves fallback while revalidating",
			setupCache:  true,
			staleCache:  true,
			binScript:   `printf '{"id":"fresh9999","title":"Fresh Video","channel":"Fresh Chan","duration":125,"view_count":5000}\n'`,
			wantFirstID: "cached123",
			wantRefetch: true,
		},
		{
			name:        "stale cache with failing binary still returns fallback",
			setupCache:  true,
			staleCache:  true,
			binScript:   `echo "ERROR: HTTP Error 429: Too Many Requests" >&2; exit 1`,
			wantFirstID: "cached123",
		},
		{
			name:        "cache miss runs yt-dlp and populates cache",
			setupCache:  false,
			binScript:   `printf '{"entries":[{"id":"miss11111","title":"Fetched Video","uploader":"UploaderX","duration_string":"02:05","view_count":1200000}]}\n'`,
			wantFirstID: "miss11111",
			wantRefetch: true,
		},
		{
			name:       "cache miss with failing binary returns classified error",
			setupCache: false,
			binScript:  `echo "ERROR: HTTP Error 403: Forbidden" >&2; exit 1`,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			cacheDir := filepath.Join(tmp, "cache")

			if tc.setupCache {
				if err := os.MkdirAll(cacheDir, 0o755); err != nil {
					t.Fatal(err)
				}
				cachePath := filepath.Join(cacheDir, "feed_home.json")
				data, err := json.Marshal(cachedVideos)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, data, 0o644); err != nil {
					t.Fatal(err)
				}
				if tc.staleCache {
					old := time.Now().Add(-10 * time.Minute)
					if err := os.Chtimes(cachePath, old, old); err != nil {
						t.Fatal(err)
					}
				}
			}

			bin := filepath.Join(tmp, "nonexistent-yt-dlp")
			if !tc.binExitFail {
				bin = writeFakeYTDLP(t, tmp, tc.binScript)
			}

			cat := NewYTDLPCatalog(cacheDir, 2*time.Minute, bin)
			defer cat.Wait()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			videos, err := cat.Feed(ctx, model.TabHome)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !IsErrKind(err, ErrForbidden) {
					t.Errorf("expected ErrForbidden, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(videos) == 0 || videos[0].ID != tc.wantFirstID {
				t.Fatalf("got videos=%+v, want first ID %q", videos, tc.wantFirstID)
			}

			if tc.wantRefetch {
				cat.Wait()
				reloaded, _, err := readCacheFile(filepath.Join(cacheDir, "feed_home.json"))
				if err != nil {
					t.Fatalf("expected cache file to be written/updated: %v", err)
				}
				if len(reloaded) == 0 {
					t.Fatalf("expected non-empty cached videos")
				}
				if tc.staleCache && reloaded[0].ID != "fresh9999" {
					t.Errorf("expected stale cache to be revalidated to fresh9999, got %q", reloaded[0].ID)
				}
			}
		})
	}
}

func TestYTDLPCatalog_SearchTSV(t *testing.T) {
	tmp := t.TempDir()
	bin := writeFakeYTDLP(t, tmp, `printf 'vid12345\tGo Concurrency\tGopherChan\t10:00\thttps://youtu.be/vid12345\t42000\n'`)

	cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), time.Minute, bin)
	defer cat.Wait()

	videos, err := cat.Search(context.Background(), "golang")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("got %d videos, want 1", len(videos))
	}
	if videos[0].ID != "vid12345" || videos[0].Title != "Go Concurrency" || videos[0].Views != "42K vistas" {
		t.Errorf("unexpected video: %+v", videos[0])
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hasArgPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

func readRecordedArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
}

func TestYTDLPCatalog_FetchArgs(t *testing.T) {
	tests := []struct {
		name            string
		invoke          func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error)
		setupCookieJar  func(t *testing.T, dir string) string
		quality         int
		wantTarget      string
		wantPlaylistEnd bool
		wantCookies     bool
		wantFormat      string
	}{
		{
			name: "feed home bounds playlist and sets socket timeout",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
		},
		{
			name: "feed music bounds playlist and sets socket timeout",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabMusic)
			},
			wantTarget:      "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantPlaylistEnd: true,
		},
		{
			name: "feed subscriptions bounds playlist and sets socket timeout",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabSubscriptions)
			},
			wantTarget:      "https://www.youtube.com/feed/subscriptions",
			wantPlaylistEnd: true,
		},
		{
			name: "refresh feed bounds playlist and sets socket timeout",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.RefreshFeed(ctx, model.TabHome)
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
		},
		{
			name: "search sets socket timeout without playlist end",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Search(ctx, "golang")
			},
			wantTarget:      "ytsearch20:golang",
			wantPlaylistEnd: false,
		},
		{
			name: "search with http text does not inject playlist end",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Search(ctx, "https://example.com")
			},
			wantTarget:      "ytsearch20:https://example.com",
			wantPlaylistEnd: false,
		},
		{
			name: "non-empty cookie jar passes --cookies on feed",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			setupCookieJar: func(t *testing.T, dir string) string {
				t.Helper()
				jar := filepath.Join(dir, "cookies.txt")
				if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tabc\n"), 0o600); err != nil {
					t.Fatalf("write cookie jar: %v", err)
				}
				return jar
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
			wantCookies:     true,
		},
		{
			name: "non-empty cookie jar omits --cookies on search",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Search(ctx, "golang")
			},
			setupCookieJar: func(t *testing.T, dir string) string {
				t.Helper()
				jar := filepath.Join(dir, "cookies.txt")
				if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tabc\n"), 0o600); err != nil {
					t.Fatalf("write cookie jar: %v", err)
				}
				return jar
			},
			wantTarget:      "ytsearch20:golang",
			wantPlaylistEnd: false,
			wantCookies:     false,
		},
		{
			name: "empty cookie jar path omits --cookies",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
			wantCookies:     false,
		},
		{
			name: "missing cookie jar file omits --cookies",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			setupCookieJar: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "missing-cookies.txt")
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
			wantCookies:     false,
		},
		{
			name: "zero-byte cookie jar file omits --cookies",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			setupCookieJar: func(t *testing.T, dir string) string {
				t.Helper()
				jar := filepath.Join(dir, "empty-cookies.txt")
				if err := os.WriteFile(jar, nil, 0o600); err != nil {
					t.Fatalf("write empty cookie jar: %v", err)
				}
				return jar
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
			wantCookies:     false,
		},
		{
			name: "directory cookie jar path omits --cookies",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			setupCookieJar: func(t *testing.T, dir string) string {
				t.Helper()
				jarDir := filepath.Join(dir, "cookies-dir")
				if err := os.MkdirAll(jarDir, 0o755); err != nil {
					t.Fatalf("mkdir cookie dir: %v", err)
				}
				return jarDir
			},
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
			wantCookies:     false,
		},
		{
			name: "quality option preserves format selector alongside feed args",
			invoke: func(ctx context.Context, cat *YTDLPCatalog) ([]model.Video, error) {
				return cat.Feed(ctx, model.TabHome)
			},
			quality:         720,
			wantTarget:      "https://www.youtube.com",
			wantPlaylistEnd: true,
			wantFormat:      BuildFormat(720),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			argsFile := filepath.Join(tmp, "args.txt")
			script := fmt.Sprintf(
				"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\nprintf '{\"id\":\"vid12345\",\"title\":\"Test Video\",\"channel\":\"Test Chan\",\"duration\":60}\\n'\n",
				shellQuote(argsFile),
			)
			bin := writeFakeYTDLP(t, tmp, script)

			var jarPath string
			if tc.setupCookieJar != nil {
				jarPath = tc.setupCookieJar(t, tmp)
			}

			cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), time.Minute, bin)
			cat.Quality = tc.quality
			cat.CookieJarPath = jarPath
			defer cat.Wait()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			videos, err := tc.invoke(ctx, cat)
			if err != nil {
				t.Fatalf("invoke error: %v", err)
			}
			if len(videos) != 1 || videos[0].ID != "vid12345" {
				t.Fatalf("got videos = %+v, want vid12345", videos)
			}

			args := readRecordedArgs(t, argsFile)
			for _, baseFlag := range []string{"--flat-playlist", "--dump-json", "--no-warnings", "--ignore-errors"} {
				if !slices.Contains(args, baseFlag) {
					t.Errorf("args %v missing base flag %q", args, baseFlag)
				}
			}
			if !hasArgPair(args, "--socket-timeout", "8") {
				t.Errorf("args %v missing --socket-timeout 8", args)
			}
			if tc.wantPlaylistEnd {
				if !hasArgPair(args, "--playlist-end", "30") {
					t.Errorf("args %v missing --playlist-end 30", args)
				}
			} else if slices.Contains(args, "--playlist-end") {
				t.Errorf("args %v unexpectedly contains --playlist-end", args)
			}
			if tc.wantCookies {
				if !hasArgPair(args, "--cookies", jarPath) {
					t.Errorf("args %v missing --cookies %q", args, jarPath)
				}
			} else if slices.Contains(args, "--cookies") {
				t.Errorf("args %v unexpectedly contains --cookies", args)
			}
			if tc.wantFormat != "" {
				if !hasArgPair(args, "-f", tc.wantFormat) {
					t.Errorf("args %v missing -f %q", args, tc.wantFormat)
				}
			} else if slices.Contains(args, "-f") {
				t.Errorf("args %v unexpectedly contains -f", args)
			}
			if len(args) == 0 || args[len(args)-1] != tc.wantTarget {
				t.Errorf("args %v last arg = %q, want %q", args, lastArg(args), tc.wantTarget)
			}
		})
	}
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}

func TestYTDLPCatalog_TabHomeGuestFallback(t *testing.T) {
	const musicChannelURL = "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ"

	t.Run("Feed TabHome falls back to public music discovery when homepage returns empty output", func(t *testing.T) {
		tmp := t.TempDir()
		targetsFile := filepath.Join(tmp, "targets.txt")
		script := fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
printf '%%s\n' "$target" >> %s
case "$target" in
	"https://www.youtube.com")
		# Logged-out yt-dlp returns 0 items on homepage.
		;;
	%s)
		printf '{"id":"disc11111","title":"Public Music Mix","channel":"YouTube Music","duration":245,"view_count":980000}\n'
		;;
esac
`, shellQuote(targetsFile), shellQuote(musicChannelURL))

		bin := writeFakeYTDLP(t, tmp, script)
		cacheDir := filepath.Join(tmp, "cache")
		cat := NewYTDLPCatalog(cacheDir, 5*time.Minute, bin)
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("Feed(TabHome) error = %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "disc11111" {
			t.Fatalf("Feed(TabHome) = %+v, want disc11111", videos)
		}

		rawTargets, err := os.ReadFile(targetsFile)
		if err != nil {
			t.Fatalf("read targets: %v", err)
		}
		gotTargets := strings.Split(strings.TrimSpace(string(rawTargets)), "\n")
		wantTargets := []string{"https://www.youtube.com", musicChannelURL}
		if !slices.Equal(gotTargets, wantTargets) {
			t.Fatalf("targets invoked = %v, want %v", gotTargets, wantTargets)
		}

		cached, _, err := readCacheFile(filepath.Join(cacheDir, "feed_home.json"))
		if err != nil || len(cached) != 1 || cached[0].ID != "disc11111" {
			t.Fatalf("feed_home.json = %+v (err=%v), want cached disc11111", cached, err)
		}
	})

	t.Run("RefreshFeed TabHome falls back to public music discovery when homepage returns empty entries", func(t *testing.T) {
		tmp := t.TempDir()
		targetsFile := filepath.Join(tmp, "targets.txt")
		script := fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
printf '%%s\n' "$target" >> %s
case "$target" in
	"https://www.youtube.com")
		printf '{"entries":[]}\n'
		;;
	%s)
		printf '{"id":"disc22222","title":"Refreshed Discovery","channel":"Music","duration":190}\n'
		;;
esac
`, shellQuote(targetsFile), shellQuote(musicChannelURL))

		bin := writeFakeYTDLP(t, tmp, script)
		cacheDir := filepath.Join(tmp, "cache")
		cat := NewYTDLPCatalog(cacheDir, 5*time.Minute, bin)
		defer cat.Wait()

		videos, err := cat.RefreshFeed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("RefreshFeed(TabHome) error = %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "disc22222" {
			t.Fatalf("RefreshFeed(TabHome) = %+v, want disc22222", videos)
		}
	})

	t.Run("Feed TabHome preserves ErrForbidden and does not fall back on yt-dlp error", func(t *testing.T) {
		tmp := t.TempDir()
		targetsFile := filepath.Join(tmp, "targets.txt")
		script := fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
printf '%%s\n' "$target" >> %s
if [ "$target" = "https://www.youtube.com" ]; then
	echo "ERROR: HTTP Error 403: Forbidden" >&2
	exit 1
fi
printf '{"id":"shouldnot01","title":"Should Not Reach","channel":"Music","duration":100}\n'
`, shellQuote(targetsFile))

		bin := writeFakeYTDLP(t, tmp, script)
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		defer cat.Wait()

		_, err := cat.Feed(context.Background(), model.TabHome)
		if !IsErrKind(err, ErrForbidden) {
			t.Fatalf("Feed(TabHome) error = %v, want ErrForbidden", err)
		}

		rawTargets, readErr := os.ReadFile(targetsFile)
		if readErr != nil {
			t.Fatalf("read targets: %v", readErr)
		}
		gotTargets := strings.Split(strings.TrimSpace(string(rawTargets)), "\n")
		if !slices.Equal(gotTargets, []string{"https://www.youtube.com"}) {
			t.Fatalf("targets invoked = %v, want only primary target", gotTargets)
		}
	})

	t.Run("non-home tabs do not fall back when empty and fallback errors propagate", func(t *testing.T) {
		tmp := t.TempDir()
		targetsFile := filepath.Join(tmp, "targets.txt")
		script := fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
printf '%%s\n' "$target" >> %s
if [ "$target" = %s ]; then
	echo "ERROR: HTTP Error 429: Too Many Requests" >&2
	exit 1
fi
`, shellQuote(targetsFile), shellQuote(musicChannelURL))

		bin := writeFakeYTDLP(t, tmp, script)
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		defer cat.Wait()

		subs, err := cat.Feed(context.Background(), model.TabSubscriptions)
		if err != nil || len(subs) != 0 {
			t.Fatalf("Feed(TabSubscriptions) = (%+v, %v), want empty nil", subs, err)
		}

		_, err = cat.Feed(context.Background(), model.TabHome)
		if !IsErrKind(err, ErrRateLimit) {
			t.Fatalf("Feed(TabHome) fallback error = %v, want ErrRateLimit", err)
		}
	})

	t.Run("Feed TabHome falls back to home search when both homepage and music discovery return empty output", func(t *testing.T) {
		tmp := t.TempDir()
		targetsFile := filepath.Join(tmp, "targets.txt")
		script := fmt.Sprintf(`
target=""
for a in "$@"; do
	target="$a"
done
printf '%%s\n' "$target" >> %s
case "$target" in
	"https://www.youtube.com"|%s)
		;;
	%s)
		printf '{"id":"trend1111","title":"Trending Fallback Video","channel":"Trending Channel","duration":210,"view_count":500000}\n'
		;;
esac
`, shellQuote(targetsFile), shellQuote(musicChannelURL), shellQuote(youtubeHomeSearchFallback))

		bin := writeFakeYTDLP(t, tmp, script)
		cacheDir := filepath.Join(tmp, "cache")
		cat := NewYTDLPCatalog(cacheDir, 5*time.Minute, bin)
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("Feed(TabHome) error = %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "trend1111" {
			t.Fatalf("Feed(TabHome) = %+v, want trend1111", videos)
		}

		rawTargets, err := os.ReadFile(targetsFile)
		if err != nil {
			t.Fatalf("read targets: %v", err)
		}
		gotTargets := strings.Split(strings.TrimSpace(string(rawTargets)), "\n")
		wantTargets := []string{"https://www.youtube.com", musicChannelURL, youtubeHomeSearchFallback}
		if !slices.Equal(gotTargets, wantTargets) {
			t.Fatalf("targets invoked = %v, want %v", gotTargets, wantTargets)
		}

		cached, _, err := readCacheFile(filepath.Join(cacheDir, "feed_home.json"))
		if err != nil || len(cached) != 1 || cached[0].ID != "trend1111" {
			t.Fatalf("feed_home.json = %+v (err=%v), want cached trend1111", cached, err)
		}
	})
}

func TestYTDLPCatalog_HonorsCustomFeedAndSearchSizes(t *testing.T) {
	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "args.log")
	script := fmt.Sprintf(`
printf '%%s\0' "$@" >> %s
printf '{"id":"vid12345678","title":"Item","channel":"Chan","duration":60}\n'
`, shellQuote(argsFile))

	bin := writeFakeYTDLP(t, tmp, script)
	cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
	cat.FeedSize = 100
	cat.SearchSize = 40
	defer cat.Wait()

	if _, err := cat.Feed(context.Background(), model.TabHome); err != nil {
		t.Fatalf("Feed error = %v", err)
	}
	if _, err := cat.Search(context.Background(), "jazz"); err != nil {
		t.Fatalf("Search error = %v", err)
	}

	args := readRecordedArgs(t, argsFile)
	if !hasArgPair(args, "--playlist-end", "100") {
		t.Fatalf("expected --playlist-end 100 for custom FeedSize, got %v", args)
	}
	if !slices.Contains(args, "ytsearch40:jazz") {
		t.Fatalf("expected ytsearch40:jazz for custom SearchSize, got %v", args)
	}
}

func TestYTDLPCatalog_OnCookiesStale(t *testing.T) {
	writeNonEmptyCookieJar := func(t *testing.T, dir string) string {
		t.Helper()
		jar := filepath.Join(dir, "cookies.txt")
		if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\tabc\n"), 0o600); err != nil {
			t.Fatalf("write cookie jar: %v", err)
		}
		return jar
	}

	t.Run("TabHome empty with non-empty cookie jar calls OnCookiesStale once", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeNonEmptyCookieJar(t, tmp)
		bin := writeFakeYTDLP(t, tmp, `exit 0`)

		var calls atomic.Int32
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.CookieJarPath = jar
		cat.OnCookiesStale = func() {
			calls.Add(1)
		}
		defer cat.Wait()

		if _, err := cat.Feed(context.Background(), model.TabHome); err != nil {
			t.Fatalf("Feed(TabHome) error = %v", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("OnCookiesStale calls = %d, want 1", got)
		}
	})

	t.Run("TabSubscriptions empty with non-empty cookie jar calls OnCookiesStale once", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeNonEmptyCookieJar(t, tmp)
		bin := writeFakeYTDLP(t, tmp, `exit 0`)

		var calls atomic.Int32
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.CookieJarPath = jar
		cat.OnCookiesStale = func() {
			calls.Add(1)
		}
		defer cat.Wait()

		if _, err := cat.Feed(context.Background(), model.TabSubscriptions); err != nil {
			t.Fatalf("Feed(TabSubscriptions) error = %v", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("OnCookiesStale calls = %d, want 1", got)
		}
	})

	t.Run("TabHome empty with empty or missing cookie jar does not call OnCookiesStale", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			jar  string
		}{
			{name: "empty path", jar: ""},
			{name: "missing file", jar: "missing-cookies.txt"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tmp := t.TempDir()
				bin := writeFakeYTDLP(t, tmp, `exit 0`)

				jarPath := tc.jar
				if jarPath != "" {
					jarPath = filepath.Join(tmp, jarPath)
				}

				var calls atomic.Int32
				cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
				cat.CookieJarPath = jarPath
				cat.OnCookiesStale = func() {
					calls.Add(1)
				}
				defer cat.Wait()

				if _, err := cat.Feed(context.Background(), model.TabHome); err != nil {
					t.Fatalf("Feed(TabHome) error = %v", err)
				}
				if got := calls.Load(); got != 0 {
					t.Fatalf("OnCookiesStale calls = %d, want 0", got)
				}
			})
		}
	})

	t.Run("TabHome non-empty does not call OnCookiesStale", func(t *testing.T) {
		tmp := t.TempDir()
		jar := writeNonEmptyCookieJar(t, tmp)
		bin := writeFakeYTDLP(t, tmp, `printf '{"id":"vid12345678","title":"Home Video","channel":"Chan","duration":60}\n'`)

		var calls atomic.Int32
		cat := NewYTDLPCatalog(filepath.Join(tmp, "cache"), 5*time.Minute, bin)
		cat.CookieJarPath = jar
		cat.OnCookiesStale = func() {
			calls.Add(1)
		}
		defer cat.Wait()

		videos, err := cat.Feed(context.Background(), model.TabHome)
		if err != nil {
			t.Fatalf("Feed(TabHome) error = %v", err)
		}
		if len(videos) != 1 {
			t.Fatalf("Feed(TabHome) len = %d, want 1", len(videos))
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("OnCookiesStale calls = %d, want 0", got)
		}
	})
}
