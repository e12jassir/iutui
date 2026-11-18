package platform

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const migratedMarker = ".migrated"

type migrationPair struct {
	src string
	dst string
}

// MigrateFromYT performs a one-time copy of user data from prior ytx or legacy
// yt/yt_terminal directories into the resolved iutui XDG directories. Legacy
// files are never moved or deleted, and any file or directory matching *cookie*
// is skipped. When ytx directories exist, they take precedence over older yt ones.
// It returns true if at least one directory was migrated.
func MigrateFromYT(p Paths) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}

	pairs := []migrationPair{
		{src: filepath.Join(home, ".local", "state", "ytx"), dst: p.StateDir},
		{src: filepath.Join(home, ".cache", "ytx"), dst: p.CacheDir},
		{src: filepath.Join(home, ".config", "ytx"), dst: p.ConfigDir},
		{src: filepath.Join(home, ".local", "state", "yt"), dst: p.StateDir},
		{src: filepath.Join(home, ".cache", "yt_terminal"), dst: p.CacheDir},
		{src: filepath.Join(home, ".config", "yt"), dst: p.ConfigDir},
	}

	migrated := false
	for _, pair := range pairs {
		ok, err := migrateDir(pair.src, pair.dst)
		if err != nil {
			return migrated, err
		}
		if ok {
			migrated = true
		}
	}

	return migrated, nil
}

func migrateDir(src, dst string) (bool, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !srcInfo.IsDir() {
		return false, nil
	}

	markerPath := filepath.Join(dst, migratedMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false, err
	}

	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if d.Name() == migratedMarker || isCookieName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		if !d.Type().IsRegular() {
			return nil
		}

		return copyFile(path, targetPath)
	})
	if err != nil {
		return false, err
	}

	if err := os.WriteFile(markerPath, []byte("migrated\n"), 0o644); err != nil {
		return false, err
	}

	return true, nil
}

func isCookieName(name string) bool {
	return strings.Contains(strings.ToLower(name), "cookie")
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}

	return out.Close()
}
