package cookies

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFilterNetscape(t *testing.T) {
	t.Parallel()

	cookieLine := func(domain, name, val string) string {
		return domain + "\tTRUE\t/\tTRUE\t1893456000\t" + name + "\t" + val
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name: "blocks non-google and non-youtube domains",
			input: strings.Join([]string{
				cookieLine("evil.com", "SESSION", "steal"),
				cookieLine("notyoutube.com", "LOGIN_INFO", "spoof"),
				cookieLine("youtube.com.evil.org", "SAPISID", "phish"),
				cookieLine("mygoogle.com", "SID", "fake"),
				cookieLine("#HttpOnly_evil.com", "SECRET", "nope"),
				cookieLine("#HttpOnly_youtube.com.evil.org", "SECRET", "nope"),
			}, "\n") + "\n",
			want: "",
		},
		{
			name: "allows google and youtube domains",
			input: strings.Join([]string{
				cookieLine(".youtube.com", "LOGIN_INFO", "abc"),
				cookieLine("#HttpOnly_.youtube.com", "HSID", "def"),
				cookieLine("googlevideo.com", "PREF", "123"),
				cookieLine("accounts.google.com", "SAPISID", "ghi"),
				cookieLine("google.com.co", "NID", "jkl"),
				cookieLine("www.youtube-nocookie.com", "YSC", "mno"),
				cookieLine("i.ytimg.com", "IMG", "pqr"),
				cookieLine("yt3.ggpht.com", "AV", "stu"),
			}, "\n") + "\n",
			want: strings.Join([]string{
				cookieLine(".youtube.com", "LOGIN_INFO", "abc"),
				cookieLine("#HttpOnly_.youtube.com", "HSID", "def"),
				cookieLine("googlevideo.com", "PREF", "123"),
				cookieLine("accounts.google.com", "SAPISID", "ghi"),
				cookieLine("google.com.co", "NID", "jkl"),
				cookieLine("www.youtube-nocookie.com", "YSC", "mno"),
				cookieLine("i.ytimg.com", "IMG", "pqr"),
				cookieLine("yt3.ggpht.com", "AV", "stu"),
			}, "\n") + "\n",
		},
		{
			name: "preserves comments and drops blank lines and blocked entries",
			input: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				"# https://curl.haxx.se/rfc/cookie_spec.html",
				"",
				cookieLine("evil.com", "A", "1"),
				cookieLine(".youtube.com", "LOGIN_INFO", "ok"),
				cookieLine("notyoutube.com", "B", "2"),
				cookieLine("#HttpOnly_accounts.google.com", "__Secure-3PAPISID", "tok"),
			}, "\n") + "\n",
			want: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				"# https://curl.haxx.se/rfc/cookie_spec.html",
				cookieLine(".youtube.com", "LOGIN_INFO", "ok"),
				cookieLine("#HttpOnly_accounts.google.com", "__Secure-3PAPISID", "tok"),
			}, "\n") + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := FilterNetscape(strings.NewReader(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("FilterNetscape() error = %v, wantErr %v", err, tc.wantErr)
			}
			if string(got) != tc.want {
				t.Fatalf("FilterNetscape() mismatch:\ngot:\n%s\nwant:\n%s", string(got), tc.want)
			}
		})
	}

	t.Run("nil reader returns error", func(t *testing.T) {
		t.Parallel()
		if _, err := FilterNetscape(nil); err == nil {
			t.Fatal("FilterNetscape(nil) = nil error, want non-nil")
		}
	})
}

func TestHasLogin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want bool
	}{
		{
			name: "empty",
			data: "",
			want: false,
		},
		{
			name: "only non-login cookies",
			data: ".youtube.com\tTRUE\t/\tTRUE\t0\tVISITOR_INFO1_LIVE\tabc\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\t123\n",
			want: false,
		},
		{
			name: "contains LOGIN_INFO",
			data: ".youtube.com\tTRUE\t/\tTRUE\t0\tLOGIN_INFO\tsecret\n",
			want: true,
		},
		{
			name: "contains SAPISID",
			data: ".google.com\tTRUE\t/\tTRUE\t0\tSAPISID\tsecret\n",
			want: true,
		},
		{
			name: "contains __Secure-3PAPISID",
			data: ".youtube.com\tTRUE\t/\tTRUE\t0\t__Secure-3PAPISID\tsecret\n",
			want: true,
		},
		{
			name: "rejects substring token names",
			data: ".youtube.com\tTRUE\t/\tTRUE\t0\tMY_LOGIN_INFO_FAKE\tval\n.youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID_ALT\tval\n",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := HasLogin([]byte(tc.data)); got != tc.want {
				t.Fatalf("HasLogin() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWritePrivateTempAndForget(t *testing.T) {
	t.Parallel()

	t.Run("WritePrivateTemp writes 0600 file and Forget cleans up", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		payload := []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tLOGIN_INFO\tok\n")

		tmpPath, err := WritePrivateTemp(dir, payload)
		if err != nil {
			t.Fatalf("WritePrivateTemp() error = %v", err)
		}
		if filepath.Dir(tmpPath) != dir {
			t.Fatalf("WritePrivateTemp() dir = %q, want %q", filepath.Dir(tmpPath), dir)
		}
		if !strings.HasPrefix(filepath.Base(tmpPath), "ck.") {
			t.Fatalf("WritePrivateTemp() base = %q, want ck.* prefix", filepath.Base(tmpPath))
		}

		info, err := os.Stat(tmpPath)
		if err != nil {
			t.Fatalf("os.Stat(%q) error = %v", tmpPath, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("temp file permissions = %o, want 0600", perm)
		}

		gotBytes, err := os.ReadFile(tmpPath)
		if err != nil {
			t.Fatalf("os.ReadFile(%q) error = %v", tmpPath, err)
		}
		if string(gotBytes) != string(payload) {
			t.Fatalf("temp file content = %q, want %q", string(gotBytes), string(payload))
		}

		// Create cookies.txt, cookies.none, and an unrelated file.
		cookiesTxt := filepath.Join(dir, "cookies.txt")
		cookiesNone := filepath.Join(dir, "cookies.none")
		unrelated := filepath.Join(dir, "keep.txt")
		for _, p := range []string{cookiesTxt, cookiesNone, unrelated} {
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatalf("os.WriteFile(%q) error = %v", p, err)
			}
		}

		if err := Forget(dir); err != nil {
			t.Fatalf("Forget() error = %v", err)
		}

		for _, removed := range []string{tmpPath, cookiesTxt, cookiesNone} {
			if _, err := os.Stat(removed); !os.IsNotExist(err) {
				t.Fatalf("expected %q to be removed, stat err = %v", removed, err)
			}
		}
		if _, err := os.Stat(unrelated); err != nil {
			t.Fatalf("expected unrelated file %q to remain, stat err = %v", unrelated, err)
		}
	})

	t.Run("WritePrivateTemp fails for missing dir", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		if _, err := WritePrivateTemp(missing, []byte("x")); err == nil {
			t.Fatal("WritePrivateTemp() = nil error, want non-nil")
		}
	})

	t.Run("Forget on empty dir succeeds and empty path errors", func(t *testing.T) {
		t.Parallel()
		if err := Forget(t.TempDir()); err != nil {
			t.Fatalf("Forget(emptyDir) error = %v", err)
		}
		if err := Forget(""); err == nil {
			t.Fatal("Forget(\"\") = nil error, want non-nil")
		}
	})
}

func TestDetectFirefoxProfile(t *testing.T) {
	t.Parallel()

	mkProfile := func(t *testing.T, base, name string, withSQLite bool) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
		if withSQLite {
			if err := os.WriteFile(filepath.Join(dir, "cookies.sqlite"), []byte("sqlite"), 0o600); err != nil {
				t.Fatalf("WriteFile(cookies.sqlite) error = %v", err)
			}
		}
		return dir
	}

	t.Run("prefers default-release over Default", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		mkProfile(t, base, "aaa.Default", true)
		want := mkProfile(t, base, "zzz.default-release", true)

		if got := DetectFirefoxProfile(base); got != want {
			t.Fatalf("DetectFirefoxProfile() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to Default or default when default-release lacks cookies.sqlite", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		mkProfile(t, base, "abc.default-release", false)
		want := mkProfile(t, base, "xyz.default", true)

		if got := DetectFirefoxProfile(base); got != want {
			t.Fatalf("DetectFirefoxProfile() = %q, want %q", got, want)
		}
	})

	t.Run("searches bases in order and skips missing bases", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "missing")
		base1 := t.TempDir()
		base2 := t.TempDir()
		want := mkProfile(t, base2, "p1.Default", true)

		if got := DetectFirefoxProfile(missing, base1, base2); got != want {
			t.Fatalf("DetectFirefoxProfile() = %q, want %q", got, want)
		}
	})

	t.Run("returns empty string when none found", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		mkProfile(t, base, "other-profile", true)

		if got := DetectFirefoxProfile(base); got != "" {
			t.Fatalf("DetectFirefoxProfile() = %q, want empty", got)
		}
	})
}

func TestBrowserSpecs(t *testing.T) {
	t.Parallel()

	mkProfile := func(t *testing.T, base, name string) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cookies.sqlite"), []byte("sqlite"), 0o600); err != nil {
			t.Fatalf("WriteFile(cookies.sqlite) error = %v", err)
		}
		return dir
	}

	fakeLookPath := func(bins ...string) func(string) (string, error) {
		set := make(map[string]struct{}, len(bins))
		for _, b := range bins {
			set[b] = struct{}{}
		}
		return func(name string) (string, error) {
			if _, ok := set[name]; ok {
				return "/fake/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
	}

	t.Run("explicit browser overrides browser detection", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		mkProfile(t, filepath.Join(home, ".zen"), "abc.default-release")

		got := BrowserSpecs(home, "", "  firefox:custom-prof  ", nil, fakeLookPath("brave", "chrome"))
		want := []string{"firefox:custom-prof"}
		if !slices.Equal(got, want) {
			t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
		}
	})

	t.Run("default browser priority detects gecko profiles and chromium binaries", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		xdg := filepath.Join(home, "xdg-cfg")

		zenProf := mkProfile(t, filepath.Join(home, ".zen"), "z1.default-release")
		ffProf := mkProfile(t, filepath.Join(xdg, "mozilla", "firefox"), "f1.default")
		lwProf := mkProfile(t, filepath.Join(home, ".librewolf"), "l1.default-release")

		got := BrowserSpecs(home, xdg, "", nil, fakeLookPath("brave", "chromium-browser", "google-chrome-stable"))
		want := []string{
			"firefox:" + zenProf,
			"firefox:" + ffProf,
			"firefox:" + lwProf,
			"brave",
			"chromium",
			"chrome",
		}
		if !slices.Equal(got, want) {
			t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
		}
	})

	t.Run("zen searches fallback config, flatpak, and macOS paths in order", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			relBase func(home, xdg string) string
		}{
			{
				name:    "xdg config",
				relBase: func(_, xdg string) string { return filepath.Join(xdg, "zen") },
			},
			{
				name:    "flatpak",
				relBase: func(home, _ string) string { return filepath.Join(home, ".var", "app", "app.zen_browser.zen", ".zen") },
			},
			{
				name: "macOS",
				relBase: func(home, _ string) string {
					return filepath.Join(home, "Library", "Application Support", "zen", "Profiles")
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				home := t.TempDir()
				xdg := filepath.Join(home, "custom-xdg")
				prof := mkProfile(t, tc.relBase(home, xdg), "p1.default-release")

				got := BrowserSpecs(home, xdg, "", []string{"zen"}, fakeLookPath())
				want := []string{"firefox:" + prof}
				if !slices.Equal(got, want) {
					t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
				}
			})
		}
	})

	t.Run("firefox searches snap, flatpak, and macOS paths", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			relBase func(home, xdg string) string
		}{
			{
				name:    "home mozilla",
				relBase: func(home, _ string) string { return filepath.Join(home, ".mozilla", "firefox") },
			},
			{
				name: "snap",
				relBase: func(home, _ string) string {
					return filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox")
				},
			},
			{
				name: "flatpak",
				relBase: func(home, _ string) string {
					return filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox")
				},
			},
			{
				name: "macOS",
				relBase: func(home, _ string) string {
					return filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				home := t.TempDir()
				prof := mkProfile(t, tc.relBase(home, ""), "ff.default-release")

				got := BrowserSpecs(home, "", "", []string{"firefox"}, fakeLookPath())
				want := []string{"firefox:" + prof}
				if !slices.Equal(got, want) {
					t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
				}
			})
		}
	})

	t.Run("librewolf searches xdg and flatpak paths", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			relBase func(home, xdg string) string
		}{
			{
				name:    "xdg config",
				relBase: func(_, xdg string) string { return filepath.Join(xdg, "librewolf") },
			},
			{
				name: "flatpak",
				relBase: func(home, _ string) string {
					return filepath.Join(home, ".var", "app", "io.gitlab.librewolf-community", ".librewolf")
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				home := t.TempDir()
				xdg := filepath.Join(home, "cfg")
				prof := mkProfile(t, tc.relBase(home, xdg), "lw.default")

				got := BrowserSpecs(home, xdg, "", []string{"librewolf"}, fakeLookPath())
				want := []string{"firefox:" + prof}
				if !slices.Equal(got, want) {
					t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
				}
			})
		}
	})

	t.Run("xdgConfigHome defaults to homeDir/.config when empty", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		prof := mkProfile(t, filepath.Join(home, ".config", "zen"), "cfg.default-release")

		got := BrowserSpecs(home, "", "", []string{"zen"}, fakeLookPath())
		want := []string{"firefox:" + prof}
		if !slices.Equal(got, want) {
			t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
		}
	})

	t.Run("supports brave, chromium, chrome, edge, vivaldi, case folding, and custom browsers", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		got := BrowserSpecs(
			home,
			"",
			"",
			[]string{"Brave", "CHROMIUM", "Chrome", "edge", "Vivaldi", "  Opera ", ""},
			fakeLookPath("brave-browser", "chromium", "google-chrome", "microsoft-edge-stable", "vivaldi-stable"),
		)
		want := []string{"brave", "chromium", "chrome", "edge", "vivaldi", "opera"}
		if !slices.Equal(got, want) {
			t.Fatalf("BrowserSpecs() = %v, want %v", got, want)
		}
	})

	t.Run("empty home and xdg returns no profiles", func(t *testing.T) {
		t.Parallel()
		got := BrowserSpecs("", "", "", []string{"zen", "firefox", "librewolf"}, fakeLookPath())
		if len(got) != 0 {
			t.Fatalf("BrowserSpecs() = %v, want empty", got)
		}
	})
}

func TestRefreshJarFromBrowsers(t *testing.T) {
	t.Parallel()

	writeFakeYTDLP := func(t *testing.T, dir, body string) string {
		t.Helper()
		bin := filepath.Join(dir, "yt-dlp")
		if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"+body), 0o755); err != nil {
			t.Fatalf("write fake yt-dlp: %v", err)
		}
		return bin
	}

	t.Run("returns empty immediately when cacheDir or specs are empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		bin := writeFakeYTDLP(t, dir, `exit 99`)

		if jar, err := RefreshJarFromBrowsers(context.Background(), "", bin, []string{"firefox"}); err != nil || jar != "" {
			t.Fatalf("empty cacheDir = (%q, %v), want (\"\", nil)", jar, err)
		}

		cacheDir := filepath.Join(dir, "cache")
		if jar, err := RefreshJarFromBrowsers(context.Background(), cacheDir, bin, nil); err != nil || jar != "" {
			t.Fatalf("empty specs = (%q, %v), want (\"\", nil)", jar, err)
		}
		if _, err := os.Stat(filepath.Join(cacheDir, "cookies.none")); !os.IsNotExist(err) {
			t.Fatalf("expected no cookies.none when specs empty, stat err = %v", err)
		}
	})

	t.Run("writes filtered 0600 cookies.txt, removes cookies.none, and cleans temp files", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		if err := os.MkdirAll(cacheDir, 0o700); err != nil {
			t.Fatal(err)
		}
		nonePath := filepath.Join(cacheDir, "cookies.none")
		if err := os.WriteFile(nonePath, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		argsLog := filepath.Join(tmp, "args.log")
		script := fmt.Sprintf(`
args_log=%q
for a in "$@"; do
	printf '%%s\n' "$a" >> "$args_log"
done
spec=""
raw=""
while [ $# -gt 0 ]; do
	case "$1" in
		--cookies-from-browser) spec="$2"; shift 2 ;;
		--cookies) raw="$2"; shift 2 ;;
		*) shift ;;
	esac
done
cat > "$raw" <<'COOKIES'
# Netscape HTTP Cookie File
evil.com	TRUE	/	TRUE	1893456000	SESSION	secret
.youtube.com	TRUE	/	TRUE	1893456000	LOGIN_INFO	valid-token
COOKIES
`, argsLog)
		bin := writeFakeYTDLP(t, tmp, script)

		jarPath, err := RefreshJarFromBrowsers(context.Background(), cacheDir, bin, []string{"firefox:/prof"})
		if err != nil {
			t.Fatalf("RefreshJarFromBrowsers() error = %v", err)
		}
		wantJar := filepath.Join(cacheDir, "cookies.txt")
		if jarPath != wantJar {
			t.Fatalf("jarPath = %q, want %q", jarPath, wantJar)
		}

		info, err := os.Stat(jarPath)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", jarPath, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("cookies.txt perm = %o, want 0600", perm)
		}

		data, err := os.ReadFile(jarPath)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if strings.Contains(content, "evil.com") {
			t.Fatalf("cookies.txt contains unfiltered domain: %s", content)
		}
		if !strings.Contains(content, "LOGIN_INFO") {
			t.Fatalf("cookies.txt missing LOGIN_INFO: %s", content)
		}
		if _, err := os.Stat(nonePath); !os.IsNotExist(err) {
			t.Fatalf("expected cookies.none to be removed, err = %v", err)
		}

		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "cookies.txt" {
			t.Fatalf("unexpected files left in cacheDir: %+v", entries)
		}

		loggedBytes, err := os.ReadFile(argsLog)
		if err != nil {
			t.Fatal(err)
		}
		gotArgs := strings.Split(strings.TrimSpace(string(loggedBytes)), "\n")
		if len(gotArgs) != 12 {
			t.Fatalf("yt-dlp arg count = %d (%v), want 12", len(gotArgs), gotArgs)
		}
		if gotArgs[0] != "--cookies-from-browser" || gotArgs[1] != "firefox:/prof" || gotArgs[2] != "--cookies" {
			t.Fatalf("unexpected leading args: %v", gotArgs[:4])
		}
		if !strings.HasPrefix(filepath.Base(gotArgs[3]), "cookies.raw.") || filepath.Dir(gotArgs[3]) != cacheDir {
			t.Fatalf("unexpected raw cookie temp path %q", gotArgs[3])
		}
		wantTail := []string{"--no-warnings", "--skip-download", "--flat-playlist", "--playlist-items", "1", "--print", "id", "ytsearch1:youtube"}
		if !slices.Equal(gotArgs[4:], wantTail) {
			t.Fatalf("yt-dlp trailing args = %v, want %v", gotArgs[4:], wantTail)
		}
	})

	t.Run("tries specs in order until one yields login cookies", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		seenFile := filepath.Join(tmp, "seen.txt")

		script := fmt.Sprintf(`
seen=%q
spec=""
raw=""
while [ $# -gt 0 ]; do
	case "$1" in
		--cookies-from-browser) spec="$2"; shift 2 ;;
		--cookies) raw="$2"; shift 2 ;;
		*) shift ;;
	esac
done
printf '%%s\n' "$spec" >> "$seen"
case "$spec" in
	zen-fail)
		exit 1
		;;
	brave)
		printf '.youtube.com\tTRUE\t/\tTRUE\t0\tVISITOR_INFO1_LIVE\tguest\n' > "$raw"
		;;
	chrome)
		printf '.google.com\tTRUE\t/\tTRUE\t0\tSAPISID\tauth-ok\n' > "$raw"
		;;
	never-reached)
		exit 99
		;;
esac
`, seenFile)
		bin := writeFakeYTDLP(t, tmp, script)

		jarPath, err := RefreshJarFromBrowsers(context.Background(), cacheDir, bin, []string{"zen-fail", "brave", "chrome", "never-reached"})
		if err != nil {
			t.Fatalf("RefreshJarFromBrowsers() error = %v", err)
		}
		if jarPath != filepath.Join(cacheDir, "cookies.txt") {
			t.Fatalf("jarPath = %q, want cookies.txt", jarPath)
		}

		seenRaw, err := os.ReadFile(seenFile)
		if err != nil {
			t.Fatal(err)
		}
		seen := strings.Split(strings.TrimSpace(string(seenRaw)), "\n")
		wantSeen := []string{"zen-fail", "brave", "chrome"}
		if !slices.Equal(seen, wantSeen) {
			t.Fatalf("probed specs = %v, want %v", seen, wantSeen)
		}
	})

	t.Run("creates cacheDir 0700 and touches 0600 cookies.none when no spec yields login", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "nested", "cache")
		script := `
raw=""
while [ $# -gt 0 ]; do
	case "$1" in
		--cookies) raw="$2"; shift 2 ;;
		*) shift ;;
	esac
done
printf '.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tguest\n' > "$raw"
`
		bin := writeFakeYTDLP(t, tmp, script)

		jarPath, err := RefreshJarFromBrowsers(context.Background(), cacheDir, bin, []string{"firefox", "brave"})
		if err != nil {
			t.Fatalf("RefreshJarFromBrowsers() error = %v", err)
		}
		if jarPath != "" {
			t.Fatalf("jarPath = %q, want empty", jarPath)
		}

		dirInfo, err := os.Stat(cacheDir)
		if err != nil {
			t.Fatalf("Stat(cacheDir) error = %v", err)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Fatalf("cacheDir perm = %o, want 0700", perm)
		}

		nonePath := filepath.Join(cacheDir, "cookies.none")
		noneInfo, err := os.Stat(nonePath)
		if err != nil {
			t.Fatalf("Stat(cookies.none) error = %v", err)
		}
		if perm := noneInfo.Mode().Perm(); perm != 0o600 {
			t.Fatalf("cookies.none perm = %o, want 0600", perm)
		}
		if _, err := os.Stat(filepath.Join(cacheDir, "cookies.txt")); !os.IsNotExist(err) {
			t.Fatalf("expected no cookies.txt, stat err = %v", err)
		}

		// Verify existing cookies.none mtime gets refreshed on subsequent empty refresh.
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(nonePath, old, old); err != nil {
			t.Fatal(err)
		}
		if _, err := RefreshJarFromBrowsers(context.Background(), cacheDir, bin, []string{"firefox"}); err != nil {
			t.Fatalf("second RefreshJarFromBrowsers() error = %v", err)
		}
		refreshedInfo, err := os.Stat(nonePath)
		if err != nil {
			t.Fatal(err)
		}
		if !refreshedInfo.ModTime().After(old) {
			t.Fatalf("cookies.none mtime = %v, want after %v", refreshedInfo.ModTime(), old)
		}
	})

	t.Run("respects context cancellation without touching cookies.none", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		cacheDir := filepath.Join(tmp, "cache")
		bin := writeFakeYTDLP(t, tmp, `sleep 5`)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		defer cancel()

		jarPath, err := RefreshJarFromBrowsers(ctx, cacheDir, bin, []string{"firefox"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("RefreshJarFromBrowsers() error = %v, want DeadlineExceeded", err)
		}
		if jarPath != "" {
			t.Fatalf("jarPath = %q, want empty", jarPath)
		}
		if _, statErr := os.Stat(filepath.Join(cacheDir, "cookies.none")); !os.IsNotExist(statErr) {
			t.Fatalf("expected no cookies.none on context cancellation, stat err = %v", statErr)
		}
		raws, globErr := filepath.Glob(filepath.Join(cacheDir, "cookies.raw.*"))
		if globErr != nil || len(raws) != 0 {
			t.Fatalf("expected no leftover raw cookie files, got %v (err=%v)", raws, globErr)
		}
	})
}

func TestRefreshJarFromBrowsers_DefaultYTDLPBinary(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "yt-dlp")
	script := `#!/usr/bin/env bash
set -euo pipefail
raw=""
while [ $# -gt 0 ]; do
	case "$1" in
		--cookies) raw="$2"; shift 2 ;;
		*) shift ;;
	esac
done
printf '.youtube.com\tTRUE\t/\tTRUE\t0\t__Secure-3PAPISID\tdefault-bin-ok\n' > "$raw"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cacheDir := filepath.Join(tmp, "cache")
	jarPath, err := RefreshJarFromBrowsers(context.Background(), cacheDir, "", []string{"firefox"})
	if err != nil {
		t.Fatalf("RefreshJarFromBrowsers() error = %v", err)
	}
	if jarPath != filepath.Join(cacheDir, "cookies.txt") {
		t.Fatalf("jarPath = %q, want %q", jarPath, filepath.Join(cacheDir, "cookies.txt"))
	}
}
