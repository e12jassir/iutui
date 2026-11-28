// Package download manages video and audio downloads via yt-dlp and tracks
// queue state both in memory and in on-disk metadata files.
package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
)

// Status represents the lifecycle state of a download item.
type Status string

const (
	StatusPending     Status = "pending"
	StatusDownloading Status = "downloading"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
)

const defaultMaxConcurrent = 2

// Item represents a queued, active, completed, or failed download.
type Item struct {
	Video      model.Video
	AudioOnly  bool
	Status     Status
	OutputPath string
	Err        string
	Progress   float64
	Speed      string
	ETA        string
	FilePath   string
}

// Runner executes yt-dlp (or a test double) with the given context, binary, and arguments.
type Runner func(ctx context.Context, bin string, args ...string) error

// Manager coordinates video and audio downloads and maintains thread-safe queue state.
type Manager struct {
	StateDir      string
	DownloadDir   string
	YtdlpBin      string
	MaxConcurrent int
	Runner        Runner
	OnProgress    func(Item)

	mu        sync.Mutex
	cookieJar string
	quality   int
	extraArgs []string
	items     map[string]*Item
	order     []string
	sem       chan struct{}
	cancels   map[string]context.CancelFunc
}

// SetCookieJar updates the Netscape cookie jar path passed to yt-dlp when valid and non-empty.
func (m *Manager) SetCookieJar(path string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cookieJar = strings.TrimSpace(path)
	m.mu.Unlock()
}

// SetQuality updates the maximum video height for non-audio downloads (0 = no limit).
func (m *Manager) SetQuality(q int) {
	if m == nil {
		return
	}
	if q < 0 {
		q = 0
	}
	m.mu.Lock()
	m.quality = q
	m.mu.Unlock()
}

// SetExtraArgs updates additional CLI flags passed to yt-dlp during downloads.
func (m *Manager) SetExtraArgs(args []string) {
	if m == nil {
		return
	}
	var copied []string
	for _, a := range args {
		if s := strings.TrimSpace(a); s != "" {
			copied = append(copied, s)
		}
	}
	m.mu.Lock()
	m.extraArgs = copied
	m.mu.Unlock()
}

// NewManager creates a Manager configured with the given state directory,
// download destination directory, and yt-dlp binary name (defaulting to "yt-dlp").
func NewManager(stateDir, downloadDir, ytdlpBin string) *Manager {
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	return &Manager{
		StateDir:      stateDir,
		DownloadDir:   downloadDir,
		YtdlpBin:      ytdlpBin,
		MaxConcurrent: defaultMaxConcurrent,
		items:         make(map[string]*Item),
		cancels:       make(map[string]context.CancelFunc),
	}
}

// Enqueue tracks the download in the queue, executes yt-dlp to download the video
// or audio track, and writes StateDir/downloads/<id>.meta atomically only upon success.
func (m *Manager) Enqueue(ctx context.Context, v model.Video, audioOnly bool) error {
	if err := validateVideoID(v.ID); err != nil {
		return err
	}

	dest := m.DownloadDir
	if dest == "" {
		dest = "."
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("download: create download dir: %w", err)
	}

	outputTemplate := filepath.Join(dest, "%(title)s.%(ext)s")

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	m.mu.Lock()
	m.ensureInitLocked()
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
	if prev, ok := m.cancels[v.ID]; ok && prev != nil {
		prev()
	}
	m.cancels[v.ID] = cancel
	sem := m.sem
	bin := m.YtdlpBin
	if bin == "" {
		bin = "yt-dlp"
	}
	runner := m.Runner
	onProg := m.OnProgress
	pendingSnap := *item
	m.mu.Unlock()

	if onProg != nil {
		onProg(pendingSnap)
	}

	defer func() {
		m.mu.Lock()
		if m.items[v.ID] == item {
			delete(m.cancels, v.ID)
		}
		m.mu.Unlock()
	}()

	if err := dlCtx.Err(); err != nil {
		m.finishFailed(v.ID, item, err)
		return err
	}

	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-dlCtx.Done():
		err := dlCtx.Err()
		m.finishFailed(v.ID, item, err)
		return err
	}

	m.mu.Lock()
	item.Status = StatusDownloading
	quality := m.quality
	cookieJar := m.cookieJar
	var extraArgs []string
	if len(m.extraArgs) > 0 {
		extraArgs = make([]string, len(m.extraArgs))
		copy(extraArgs, m.extraArgs)
	}
	dlSnap := *item
	onProg = m.OnProgress
	m.mu.Unlock()
	if onProg != nil {
		onProg(dlSnap)
	}

	args := buildArgs(v.ID, outputTemplate, audioOnly, quality, cookieJar, extraArgs)

	var err error
	if runner != nil {
		err = runner(dlCtx, bin, args...)
	} else {
		err = m.runWithProgress(dlCtx, bin, item, args...)
	}

	if err == nil {
		m.mu.Lock()
		filePath := item.FilePath
		m.mu.Unlock()
		if m.StateDir != "" {
			if metaErr := writeMetaAtomicWithPath(m.downloadsDir(), v, filePath); metaErr != nil {
				err = metaErr
			}
		}
	} else {
		m.cleanInvalidMeta(v.ID)
	}

	m.mu.Lock()
	if m.items[v.ID] == item {
		delete(m.cancels, v.ID)
	}
	if err != nil {
		item.Status = StatusFailed
		item.Err = err.Error()
	} else {
		item.Status = StatusDone
		item.Progress = 100
		item.Err = ""
	}
	finalSnap := *item
	onProg = m.OnProgress
	m.mu.Unlock()

	if onProg != nil {
		onProg(finalSnap)
	}

	return err
}

// Cancel cancels an active or pending download for videoID, returning true if
// a download was active and cancelled.
func (m *Manager) Cancel(videoID string) bool {
	m.mu.Lock()
	cancel, ok := m.cancels[videoID]
	if ok {
		delete(m.cancels, videoID)
	}
	m.mu.Unlock()
	if ok && cancel != nil {
		cancel()
		return true
	}
	return false
}

// LocalFile returns the verified local file path for a completed download if it
// exists on disk and is non-empty, or "" otherwise.
func (m *Manager) LocalFile(videoID string) string {
	if err := validateVideoID(videoID); err != nil {
		return ""
	}

	m.mu.Lock()
	var memPath string
	if it, ok := m.items[videoID]; ok && it != nil {
		memPath = it.FilePath
	}
	m.mu.Unlock()

	if isValidLocalFile(memPath) {
		return memPath
	}

	if dlDir := m.downloadsDir(); dlDir != "" {
		metaPath := filepath.Join(dlDir, videoID+".meta")
		if _, diskPath, err := readMeta(metaPath, videoID); err == nil {
			if isValidLocalFile(diskPath) {
				return diskPath
			}
		}
	}
	return ""
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
		metaPath := filepath.Join(dlDir, entry.Name())
		v, filePath, err := readMeta(metaPath, id)
		if err != nil {
			return nil, err
		}
		if filePath != "" && !isValidLocalFile(filePath) {
			_ = os.Remove(metaPath)
			continue
		}
		diskItems = append(diskItems, Item{
			Video:    v,
			Status:   StatusDone,
			Progress: 100,
			FilePath: filePath,
		})
	}

	sort.Slice(diskItems, func(i, j int) bool {
		return diskItems[i].Video.ID < diskItems[j].Video.ID
	})
	result = append(result, diskItems...)
	return result, nil
}

func (m *Manager) ensureInitLocked() {
	if m.items == nil {
		m.items = make(map[string]*Item)
	}
	if m.cancels == nil {
		m.cancels = make(map[string]context.CancelFunc)
	}
	if m.sem == nil {
		n := m.MaxConcurrent
		if n <= 0 {
			n = defaultMaxConcurrent
		}
		m.sem = make(chan struct{}, n)
	}
}

func (m *Manager) finishFailed(videoID string, item *Item, err error) {
	m.cleanInvalidMeta(videoID)
	m.mu.Lock()
	if m.items[videoID] == item {
		delete(m.cancels, videoID)
	}
	item.Status = StatusFailed
	item.Err = err.Error()
	snap := *item
	onProg := m.OnProgress
	m.mu.Unlock()

	if onProg != nil {
		onProg(snap)
	}
}

func (m *Manager) cleanInvalidMeta(videoID string) {
	dlDir := m.downloadsDir()
	if dlDir == "" || validateVideoID(videoID) != nil {
		return
	}
	metaPath := filepath.Join(dlDir, videoID+".meta")
	_, diskPath, err := readMeta(metaPath, videoID)
	if err != nil {
		return
	}
	if !isValidLocalFile(diskPath) {
		_ = os.Remove(metaPath)
	}
}

func (m *Manager) downloadsDir() string {
	if m.StateDir == "" {
		return ""
	}
	return filepath.Join(m.StateDir, "downloads")
}

func (m *Manager) runWithProgress(ctx context.Context, bin string, item *Item, args ...string) error {
	fullArgs := make([]string, 0, 5+len(args))
	fullArgs = append(fullArgs,
		"--newline",
		"--progress-template", "download:IUTUI_PROG|%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s",
		"--print", "after_move:IUTUI_FILE|%(filepath)s",
	)
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(ctx, bin, fullArgs...)
	platform.SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return platform.KillProcessGroup(cmd.Process.Pid)
	}
	cmd.WaitDelay = 100 * time.Millisecond

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %v", ctx.Err(), err)
		}
		return err
	}

	scanDone := make(chan struct{})
	var errLines []string
	go func() {
		defer close(scanDone)
		defer pr.Close()
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !m.handleOutputLine(item, line) {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					if len(errLines) >= 20 {
						copy(errLines, errLines[1:])
						errLines[len(errLines)-1] = trimmed
					} else {
						errLines = append(errLines, trimmed)
					}
				}
			}
		}
	}()

	waitErr := cmd.Wait()
	_ = pw.Close()
	<-scanDone

	if waitErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %v", ctx.Err(), waitErr)
		}
		if msg := strings.TrimSpace(strings.Join(errLines, "\n")); msg != "" {
			return fmt.Errorf("%w: %s", waitErr, msg)
		}
		return waitErr
	}
	return nil
}

func (m *Manager) handleOutputLine(item *Item, rawLine string) bool {
	line := rawLine
	if idx := strings.LastIndex(line, "\r"); idx >= 0 {
		line = line[idx+1:]
	}
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "IUTUI_PROG|") {
		rest := strings.TrimPrefix(line, "IUTUI_PROG|")
		parts := strings.SplitN(rest, "|", 3)

		var pct float64
		var hasPct bool
		if len(parts) >= 1 {
			pctStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[0]), "%"))
			if parsed, err := strconv.ParseFloat(pctStr, 64); err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
				if parsed < 0 {
					parsed = 0
				} else if parsed > 100 {
					parsed = 100
				}
				pct = parsed
				hasPct = true
			}
		}

		var speed string
		if len(parts) >= 2 {
			s := strings.TrimSpace(parts[1])
			if s != "" && s != "NA" && s != "N/A" {
				speed = s
			}
		}

		var eta string
		if len(parts) >= 3 {
			e := strings.TrimSpace(parts[2])
			if e != "" && e != "NA" && e != "N/A" {
				eta = e
			}
		}

		m.mu.Lock()
		if hasPct {
			item.Progress = pct
		}
		if speed != "" {
			item.Speed = speed
		}
		if eta != "" {
			item.ETA = eta
		}
		snap := *item
		onProg := m.OnProgress
		m.mu.Unlock()
		if onProg != nil {
			onProg(snap)
		}
		return true
	}

	if strings.HasPrefix(line, "IUTUI_FILE|") {
		fp := strings.TrimSpace(strings.TrimPrefix(line, "IUTUI_FILE|"))
		if fp != "" {
			m.mu.Lock()
			item.FilePath = fp
			snap := *item
			onProg := m.OnProgress
			m.mu.Unlock()
			if onProg != nil {
				onProg(snap)
			}
		}
		return true
	}

	return false
}

func validateVideoID(id string) error {
	if id == "" {
		return errors.New("download: video ID is required")
	}
	if strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return fmt.Errorf("download: invalid video ID %q", id)
	}
	return nil
}

func isValidLocalFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() > 0
}

func buildArgs(videoID, outputTemplate string, audioOnly bool, quality int, cookieJar string, extraArgs []string) []string {
	url := "https://www.youtube.com/watch?v=" + videoID
	var args []string
	if audioOnly {
		args = append(args, "-x", "--audio-format", "mp3", "--embed-thumbnail")
	} else if quality > 0 {
		args = append(args, "-f", fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", quality, quality))
	}
	if isValidLocalFile(cookieJar) {
		args = append(args, "--cookies", cookieJar)
	}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}
	args = append(args, "-o", outputTemplate, "--", url)
	return args
}

func writeMetaAtomicWithPath(dir string, v model.Video, filePath string) error {
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

	var content string
	if filePath != "" {
		content = fmt.Sprintf("%s\n%s\n%s\n", sanitizeLine(v.Title), sanitizeLine(v.Channel), sanitizeLine(filePath))
	} else {
		content = fmt.Sprintf("%s\n%s\n", sanitizeLine(v.Title), sanitizeLine(v.Channel))
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download: write temp meta file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download: sync temp meta file: %w", err)
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

func readMeta(path, id string) (model.Video, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Video{}, "", fmt.Errorf("download: read meta file %s: %w", path, err)
	}
	norm := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(norm, "\r\n"), "\n")
	v := model.Video{ID: id}
	if len(lines) >= 1 {
		v.Title = lines[0]
	}
	if len(lines) >= 2 {
		v.Channel = lines[1]
	}
	var filePath string
	if len(lines) >= 3 {
		filePath = strings.TrimSpace(lines[2])
	}
	return v, filePath, nil
}

func sanitizeLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
