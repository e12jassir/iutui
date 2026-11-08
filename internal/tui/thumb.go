package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx-cli/internal/data"
)

var (
	thumbCache   = make(map[string]string)
	thumbCacheMu sync.RWMutex
	fetchingMap  = make(map[string]bool)
	fetchingMu   sync.Mutex
)

// GetThumbnailFile returns the path to a cached thumbnail image (preferring HD).
func GetThumbnailFile(id string) string {
	cacheDir, _ := data.DefaultPaths()
	thumbsDir := filepath.Join(cacheDir, "thumbs")
	for _, q := range []string{"maxresdefault", "hqdefault", "mqdefault"} {
		f := filepath.Join(thumbsDir, fmt.Sprintf("%s_%s.jpg", id, q))
		if info, err := os.Stat(f); err == nil && info.Size() > 0 {
			return f
		}
	}
	return ""
}

// FetchThumbnailAsync triggers a background download for missing thumbnails using hqdefault (480x360).
func FetchThumbnailAsync(id string) {
	if len(id) < 5 {
		return
	}
	fetchingMu.Lock()
	if fetchingMap[id] {
		fetchingMu.Unlock()
		return
	}
	fetchingMap[id] = true
	fetchingMu.Unlock()

	go func() {
		cacheDir, _ := data.DefaultPaths()
		thumbsDir := filepath.Join(cacheDir, "thumbs")
		_ = os.MkdirAll(thumbsDir, 0755)
		target := filepath.Join(thumbsDir, fmt.Sprintf("%s_hqdefault.jpg", id))
		url := fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", id)

		cmd := exec.Command("curl", "-sfL", "--max-time", "4", "-o", target, url)
		_ = cmd.Run()

		fetchingMu.Lock()
		delete(fetchingMap, id)
		fetchingMu.Unlock()
	}()
}

// RenderThumbnailANSI converts the JPEG thumbnail into full-bleed truecolor halfblock ANSI art.
func RenderThumbnailANSI(id string, width, height int) string {
	thumbCacheMu.RLock()
	cached, ok := thumbCache[id]
	thumbCacheMu.RUnlock()
	if ok {
		return cached
	}

	file := GetThumbnailFile(id)
	if file == "" {
		FetchThumbnailAsync(id)
		return renderEmptyThumb(width, height)
	}

	sizeArg := fmt.Sprintf("%dx%d", width, height)
	cmd := exec.Command("chafa", "--format=symbols", "--symbols=vhalf", "--stretch", fmt.Sprintf("--size=%s", sizeArg), file)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return renderEmptyThumb(width, height)
	}

	ansiStr := strings.TrimRight(string(out), "\n")

	thumbCacheMu.Lock()
	thumbCache[id] = ansiStr
	thumbCacheMu.Unlock()

	return ansiStr
}

func renderEmptyThumb(width, height int) string {
	var lines []string
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	for i := 0; i < height; i++ {
		if i == height/2 {
			label := "      ▶ [Cargando...]       "
			if len(label) > width {
				label = label[:width]
			}
			padding := width - len(label)
			lines = append(lines, borderStyle.Render(label+strings.Repeat(" ", padding)))
		} else {
			lines = append(lines, strings.Repeat(" ", width))
		}
	}
	return strings.Join(lines, "\n")
}
