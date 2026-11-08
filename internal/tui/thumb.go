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

// GetThumbnailFile returns the path to a cached thumbnail image (preferring 16:9 HD).
func GetThumbnailFile(id string) string {
	cacheDir, _ := data.DefaultPaths()
	thumbsDir := filepath.Join(cacheDir, "thumbs")
	for _, q := range []string{"maxresdefault", "mqdefault", "hqdefault"} {
		f := filepath.Join(thumbsDir, fmt.Sprintf("%s_%s.jpg", id, q))
		if info, err := os.Stat(f); err == nil && info.Size() > 0 {
			return f
		}
	}
	return ""
}

// FetchThumbnailAsync triggers a background download for missing thumbnails using maxresdefault (1280x720)
// and falls back to mqdefault (320x180). Both are true 16:9 and lack the 45px black letterbox bars of hqdefault.
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

		// 1. Try maxresdefault (1280x720 16:9, crystal clear HD)
		targetMax := filepath.Join(thumbsDir, fmt.Sprintf("%s_maxresdefault.jpg", id))
		urlMax := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", id)
		cmdMax := exec.Command("curl", "-sfL", "--max-time", "4", "-o", targetMax, urlMax)
		if err := cmdMax.Run(); err != nil || isFileEmpty(targetMax) {
			_ = os.Remove(targetMax)
			// 2. Fallback to mqdefault (320x180 16:9, clean without black bars)
			targetMq := filepath.Join(thumbsDir, fmt.Sprintf("%s_mqdefault.jpg", id))
			urlMq := fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", id)
			cmdMq := exec.Command("curl", "-sfL", "--max-time", "4", "-o", targetMq, urlMq)
			_ = cmdMq.Run()
		}

		fetchingMu.Lock()
		delete(fetchingMap, id)
		fetchingMu.Unlock()
	}()
}

func isFileEmpty(path string) bool {
	info, err := os.Stat(path)
	return err != nil || info.Size() == 0
}

// RenderThumbnailANSI converts the JPEG thumbnail into full-bleed truecolor quadrant/halfblock ANSI art,
// with mathematically strict line-count clamping to guarantee uniform card height across all states.
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
	cmd := exec.Command("chafa",
		"--format=symbols",
		"--symbols=quad+half",
		"--color-space=din99d",
		"--dither=none",
		"--stretch",
		fmt.Sprintf("--size=%s", sizeArg),
		file,
	)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return renderEmptyThumb(width, height)
	}

	rawLines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	var validLines []string
	for _, l := range rawLines {
		if strings.TrimSpace(l) != "" || len(validLines) < height {
			validLines = append(validLines, l)
		}
	}
	if len(validLines) > height {
		validLines = validLines[:height]
	}
	for len(validLines) < height {
		validLines = append(validLines, strings.Repeat(" ", width))
	}
	ansiStr := strings.Join(validLines, "\n")

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
