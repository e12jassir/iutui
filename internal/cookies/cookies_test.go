package cookies

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			name:  "blocks non-google and non-youtube domains",
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
