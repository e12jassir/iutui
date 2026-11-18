// Package download manages video and audio downloads via yt-dlp and tracks
// queue state both in memory and in on-disk metadata files.
package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/e12jassir/iutui/internal/model"
)

// Status represents the lifecycle state of a download item.
type Status string

const (
	StatusPending     Status = "pending"
	StatusDownloading Status = "downloading"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
)

// Item represents a queued, active, completed, or failed download.
type Item struct {
	Video      model.Video
	AudioOnly  bool
	Status     Status
	OutputPath string
	Err        string
}

// Runner executes yt-dlp (or a test double) with the given context, binary, and arguments.
type Runner func(ctx context.Context, bin string, args ...string) error

// Manager coordinates video and audio downloads and maintains thread-safe queue state.
type Manager struct {
	StateDir    string
	DownloadDir string
	YtdlpBin    string
	Runner      Runner

	mu    sync.Mutex
	items map[string]*Item
	order []string
}

// NewManager creates a Manager configured with the given state directory,
// download destination directory, and yt-dlp binary name (defaulting to "yt-dlp").
func NewManager(stateDir, downloadDir, ytdlpBin string) *Manager {
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	return &Manager{
		StateDir:    stateDir,
		DownloadDir: downloadDir,
		YtdlpBin:    ytdlpBin,
		Runner:      defaultRunner,
		items:       make(map[string]*Item),
	}
}

// Enqueue writes a .meta file atomically under StateDir/downloads/<id>.meta,
// tracks the download in the queue, and executes yt-dlp to download the video
// or audio track.
func (m *Manager) Enqueue(ctx context.Context, v model.Video, audioOnly bool) error {
	if v.ID == "" {
		return errors.New("download: video ID is required")
	}
	if strings.ContainsAny(v.ID, `/\`) || v.ID == "." || v.ID == ".." {
		return fmt.Errorf("download: invalid video ID %q", v.ID)
	}

	dest := m.DownloadDir
	if dest == "" {
		dest = "."
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("download: create download dir: %w", err)
	}

	outputTemplate := filepath.Join(dest, "%(title)s.%(ext)s")

	if m.StateDir != "" {
		if err := writeMetaAtomic(m.downloadsDir(), v); err != nil {
			return err
		}
	}

	m.mu.Lock()
	item := &Item{
		Video:      v,
		AudioOnly:  audioOnly,
		Status:     StatusPending,
		OutputPath: outputTemplate,
	}
	if _, exists := m.items[v.ID]; !exists {
		m.order = append(m.order, v.ID)
	}
	m.items[v.ID] = item
	item.Status = StatusDownloading
	m.mu.Unlock()

	args := buildArgs(v.ID, outputTemplate, audioOnly)
	bin := m.YtdlpBin
	if bin == "" {
		bin = "yt-dlp"
	}
	runner := m.Runner
	if runner == nil {
		runner = defaultRunner
	}

	err := runner(ctx, bin, args...)

	m.mu.Lock()
	if err != nil {
		item.Status = StatusFailed
		item.Err = err.Error()
	} else {
		item.Status = StatusDone
		item.Err = ""
	}
	m.mu.Unlock()

	return err
}

// List returns all active/tracked queue items merged with any persisted .meta
// files found in StateDir/downloads.
func (m *Manager) List() ([]Item, error) {
	m.mu.Lock()
	result := make([]Item, 0, len(m.order))
	seen := make(map[string]struct{}, len(m.order))
	for _, id := range m.order {
		if it, ok := m.items[id]; ok {
			result = append(result, *it)
			seen[id] = struct{}{}
		}
	}
	m.mu.Unlock()

	dlDir := m.downloadsDir()
	if dlDir == "" {
		return result, nil
	}

	entries, err := os.ReadDir(dlDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return nil, fmt.Errorf("download: read downloads dir: %w", err)
	}

	var diskItems []Item
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".meta")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		v, err := readMeta(filepath.Join(dlDir, entry.Name()), id)
		if err != nil {
			return nil, err
		}
		diskItems = append(diskItems, Item{
			Video:  v,
			Status: StatusDone,
		})
	}

	sort.Slice(diskItems, func(i, j int) bool {
		return diskItems[i].Video.ID < diskItems[j].Video.ID
	})
	result = append(result, diskItems...)
	return result, nil
}

func (m *Manager) downloadsDir() string {
	if m.StateDir == "" {
		return ""
	}
	return filepath.Join(m.StateDir, "downloads")
}

func buildArgs(videoID, outputTemplate string, audioOnly bool) []string {
	url := "https://www.youtube.com/watch?v=" + videoID
	var args []string
	if audioOnly {
		args = append(args, "-x", "--audio-format", "mp3", "--embed-thumbnail")
	}
	args = append(args, "-o", outputTemplate, "--", url)
	return args
}

func writeMetaAtomic(dir string, v model.Video) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("download: create state downloads dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, v.ID+".meta.tmp.*")
	if err != nil {
		return fmt.Errorf("download: create temp meta file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	content := fmt.Sprintf("%s\n%s\n", sanitizeLine(v.Title), sanitizeLine(v.Channel))
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download: write temp meta file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("download: close temp meta file: %w", err)
	}

	metaPath := filepath.Join(dir, v.ID+".meta")
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("download: rename meta file: %w", err)
	}
	cleanup = false
	return nil
}

func readMeta(path, id string) (model.Video, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Video{}, fmt.Errorf("download: read meta file %s: %w", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	v := model.Video{ID: id}
	if len(lines) >= 1 {
		v.Title = lines[0]
	}
	if len(lines) >= 2 {
		v.Channel = lines[1]
	}
	return v, nil
}

func sanitizeLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func defaultRunner(ctx context.Context, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
