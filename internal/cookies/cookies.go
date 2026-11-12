// Package cookies provides Netscape cookie jar filtering, login detection,
// private temporary jar storage, cache cleanup, and Firefox-based profile detection.
package cookies

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	youtubeDomainRe = regexp.MustCompile(`(^|\.)(youtube|youtube-nocookie|googlevideo|ytimg|ggpht)\.com$`)
	googleDomainRe  = regexp.MustCompile(`(^|\.)google\.[a-z.]+$`)
	loginTokenRe    = regexp.MustCompile(`(?:^|\s)(LOGIN_INFO|SAPISID|__Secure-3PAPISID)(?:\s|$)`)
)

// FilterNetscape filters a Netscape cookie jar from r, retaining only comment
// lines and entries whose domain matches Google or YouTube domains.
func FilterNetscape(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, errors.New("cookies: nil reader")
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var out bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		domain, _, _ := strings.Cut(line, "\t")
		domain = strings.TrimPrefix(domain, "#HttpOnly_")
		if isAllowedDomain(domain) {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func isAllowedDomain(domain string) bool {
	return youtubeDomainRe.MatchString(domain) || googleDomainRe.MatchString(domain)
}

// HasLogin reports whether the Netscape cookie jar data contains a Google or
// YouTube authentication token (LOGIN_INFO, SAPISID, or __Secure-3PAPISID).
func HasLogin(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return loginTokenRe.Match(data)
}

// WritePrivateTemp writes data to a new temporary cookie jar in dir with
// strict 0600 permissions and returns its path.
func WritePrivateTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, "ck.*")
	if err != nil {
		return "", err
	}
	name := f.Name()

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Forget removes persisted cookie jars (cookies.txt, cookies.none) and
// temporary cookie jars (ck.*) from cacheDir.
func Forget(cacheDir string) error {
	if cacheDir == "" {
		return errors.New("cookies: empty cache directory")
	}

	var errs []error
	for _, name := range []string{"cookies.txt", "cookies.none"} {
		if err := removeIfExists(filepath.Join(cacheDir, name)); err != nil {
			errs = append(errs, err)
		}
	}

	temps, err := filepath.Glob(filepath.Join(cacheDir, "ck.*"))
	if err != nil {
		errs = append(errs, err)
	}
	for _, p := range temps {
		if err := removeIfExists(p); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DetectFirefoxProfile searches bases in order and returns the first profile
// directory matching *default-release* or *[Dd]efault* that contains cookies.sqlite.
func DetectFirefoxProfile(bases ...string) string {
	patterns := []string{"*default-release*", "*[Dd]efault*"}
	for _, base := range bases {
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		for _, pat := range patterns {
			matches, err := filepath.Glob(filepath.Join(base, pat))
			if err != nil {
				continue
			}
			for _, profileDir := range matches {
				sqlitePath := filepath.Join(profileDir, "cookies.sqlite")
				if fi, err := os.Stat(sqlitePath); err == nil && !fi.IsDir() {
					return profileDir
				}
			}
		}
	}
	return ""
}
