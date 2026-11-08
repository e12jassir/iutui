package data

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/e12jassir/ytx-cli/internal/model"
)

// SearchYouTube performs an online or cached search via bin/yt __list search.
func SearchYouTube(query string) ([]model.Video, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	ytBin := "./bin/yt"
	if _, err := os.Stat(ytBin); os.IsNotExist(err) {
		ytBin = "yt"
	}

	cmd := exec.Command(ytBin, "__list", "search", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var videos []model.Video
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			id := parts[0]
			if len(id) < 5 || strings.Contains(id, " ") {
				continue
			}
			title := parts[2]
			channel := parts[3]
			dur := ""
			views := ""
			if len(parts) >= 5 {
				dur = parts[4]
			}
			if len(parts) >= 6 {
				views = parts[5]
			}
			videos = append(videos, model.Video{
				ID:       id,
				Title:    title,
				Channel:  channel,
				Duration: dur,
				Views:    views,
			})
		}
	}
	return videos, nil
}

// DefaultPaths returns cache and state directories.
func DefaultPaths() (cacheDir, stateDir string) {
	xdgCache := os.Getenv("XDG_CACHE_HOME")
	if xdgCache == "" {
		home, _ := os.UserHomeDir()
		xdgCache = filepath.Join(home, ".cache")
	}
	cacheDir = filepath.Join(xdgCache, "yt_terminal")

	xdgState := os.Getenv("XDG_STATE_HOME")
	if xdgState == "" {
		home, _ := os.UserHomeDir()
		xdgState = filepath.Join(home, ".local", "state")
	}
	stateDir = filepath.Join(xdgState, "yt")
	return cacheDir, stateDir
}

// LoadFeed reads real videos from cached TSVs or runs bin/yt __list fallback.
func LoadFeed(tab model.Tab) ([]model.Video, error) {
	cacheDir, stateDir := DefaultPaths()
	var filename string

	switch tab {
	case model.TabHome:
		filename = filepath.Join(cacheDir, "feed_foryou.tsv")
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			filename = filepath.Join(cacheDir, "feed_para_ti.tsv")
		}
	case model.TabMusic:
		filename = filepath.Join(cacheDir, "feed_music.tsv")
	case model.TabSubscriptions:
		filename = filepath.Join(cacheDir, "feed_subs.tsv")
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			filename = filepath.Join(cacheDir, "feed_suscripciones.tsv")
		}
	case model.TabHistory:
		filename = filepath.Join(stateDir, "history.tsv")
	case model.TabDownloads:
		return loadDownloads(stateDir), nil
	}

	if _, err := os.Stat(filename); err == nil {
		videos, err := parseTSV(filename)
		if err == nil && len(videos) > 0 {
			return videos, nil
		}
	}

	// Fallback to calling bin/yt __list
	return fetchViaCLI(tab)
}

func parseTSV(path string) ([]model.Video, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var videos []model.Video
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		id := parts[0]
		// Filter out non-video IDs or malformed tokens
		if len(id) < 5 || strings.Contains(id, " ") {
			continue
		}

		v := model.Video{
			ID: id,
		}

		// Check for raw format: id \t title \t channel \t duration \t url \t views \t live
		// Or formatted format: id \t url \t title \t channel \t duration \t views
		if len(parts) >= 6 && strings.HasPrefix(parts[1], "http") {
			v.Title = parts[2]
			v.Channel = parts[3]
			v.Duration = parts[4]
			v.Views = parts[5]
		} else if len(parts) >= 4 {
			v.Title = parts[1]
			v.Channel = parts[2]
			v.Duration = parts[3]
			if len(parts) >= 6 {
				v.Views = parts[5]
			}
			if len(parts) >= 7 && parts[6] == "live" {
				v.IsLive = true
			}
		}

		if v.Channel == "NA" || v.Channel == "-" {
			v.Channel = "YouTube"
		}
		if v.Views == "NA" || v.Views == "-" {
			v.Views = ""
		}
		if v.Duration == "NA" || v.Duration == "-" {
			v.Duration = ""
		}

		videos = append(videos, v)
	}

	return videos, scanner.Err()
}

func fetchViaCLI(tab model.Tab) ([]model.Video, error) {
	var viewArg string
	switch tab {
	case model.TabHome:
		viewArg = "foryou"
	case model.TabMusic:
		viewArg = "music"
	case model.TabSubscriptions:
		viewArg = "subs"
	default:
		return nil, nil
	}

	cmd := exec.Command("yt", "__list", viewArg)
	out, err := cmd.Output()
	if err != nil {
		// Try ./bin/yt relative to current directory
		cmd = exec.Command("./bin/yt", "__list", viewArg)
		out, err = cmd.Output()
		if err != nil {
			return nil, err
		}
	}

	var videos []model.Video
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			id := parts[0]
			title := parts[2]
			channel := parts[3]
			dur := ""
			views := ""
			if len(parts) >= 5 {
				dur = parts[4]
			}
			if len(parts) >= 6 {
				views = parts[5]
			}
			videos = append(videos, model.Video{
				ID:       id,
				Title:    title,
				Channel:  channel,
				Duration: dur,
				Views:    views,
			})
		}
	}
	return videos, nil
}

func loadDownloads(stateDir string) []model.Video {
	dlDir := filepath.Join(stateDir, "downloads")
	entries, err := os.ReadDir(dlDir)
	if err != nil {
		return nil
	}

	var videos []model.Video
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dlDir, e.Name()))
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		if len(lines) >= 2 {
			id := strings.TrimSuffix(e.Name(), ".meta")
			videos = append(videos, model.Video{
				ID:        id,
				Title:     lines[0],
				Channel:   lines[1],
				Duration:  "Descargado",
				Published: "Local",
			})
		}
	}
	return videos
}
