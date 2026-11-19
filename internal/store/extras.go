package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	resumeFile        = "resume.json"
	searchHistoryFile = "search_history.tsv"
	defaultMaxResume  = 200
	defaultMaxSearch  = 20
	// LongVideoThresholdSeconds is the minimum duration (15 minutes) eligible for auto-resume.
	LongVideoThresholdSeconds = 900.0
)

// ResumeEntry stores the last playback timestamp for a long-form video.
type ResumeEntry struct {
	Position  float64   `json:"position"`
	Duration  float64   `json:"duration"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ResumeStore persists playback positions for long videos in resume.json capped at MaxEntries.
type ResumeStore struct {
	StateDir   string
	MaxEntries int
	mu         sync.Mutex
}

// NewResumeStore constructs a ResumeStore rooted at stateDir (default cap 200 entries).
func NewResumeStore(stateDir string) *ResumeStore {
	return &ResumeStore{
		StateDir:   stateDir,
		MaxEntries: defaultMaxResume,
	}
}

func (r *ResumeStore) path() string {
	if r == nil || strings.TrimSpace(r.StateDir) == "" {
		return ""
	}
	return filepath.Join(r.StateDir, resumeFile)
}

func (r *ResumeStore) maxEntries() int {
	if r != nil && r.MaxEntries > 0 {
		return r.MaxEntries
	}
	return defaultMaxResume
}

// GetPosition returns the saved timestamp for videoID if present and positive.
func (r *ResumeStore) GetPosition(videoID string) (float64, bool) {
	id := strings.TrimSpace(videoID)
	p := r.path()
	if id == "" || p == "" {
		return 0, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := readResumeFile(p)
	if err != nil {
		return 0, false
	}
	e, ok := entries[id]
	if !ok || e.Position < 30 {
		return 0, false
	}
	return e.Position, true
}

// SavePosition records position for long videos (>= 15 min) when within
// [30s, duration-30s); short videos or finished tracks remove any prior entry.
func (r *ResumeStore) SavePosition(videoID string, position, duration float64) error {
	id := strings.TrimSpace(videoID)
	p := r.path()
	if id == "" || p == "" {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := readResumeFile(p)
	if err != nil {
		return err
	}
	if entries == nil {
		entries = make(map[string]ResumeEntry)
	}

	if duration < LongVideoThresholdSeconds || position < 30 || position >= duration-30 {
		if _, exists := entries[id]; !exists {
			return nil
		}
		delete(entries, id)
	} else {
		entries[id] = ResumeEntry{
			Position:  position,
			Duration:  duration,
			UpdatedAt: time.Now().UTC(),
		}
		pruneResumeEntries(entries, r.maxEntries())
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return writeBytesAtomic(p, data)
}

func readResumeFile(path string) (map[string]ResumeEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var entries map[string]ResumeEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil
	}
	return entries, nil
}

func pruneResumeEntries(entries map[string]ResumeEntry, maxEntries int) {
	if maxEntries <= 0 || len(entries) <= maxEntries {
		return
	}
	type pair struct {
		id string
		at time.Time
	}
	items := make([]pair, 0, len(entries))
	for id, e := range entries {
		items = append(items, pair{id: id, at: e.UpdatedAt})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].at.After(items[j].at)
	})
	for _, old := range items[maxEntries:] {
		delete(entries, old.id)
	}
}

// SearchHistory persists recent search queries newest-first in search_history.tsv.
type SearchHistory struct {
	StateDir   string
	MaxEntries int
	mu         sync.Mutex
}

// NewSearchHistory constructs a SearchHistory rooted at stateDir (default cap 20).
func NewSearchHistory(stateDir string) *SearchHistory {
	return &SearchHistory{
		StateDir:   stateDir,
		MaxEntries: defaultMaxSearch,
	}
}

func (h *SearchHistory) path() string {
	if h == nil || strings.TrimSpace(h.StateDir) == "" {
		return ""
	}
	return filepath.Join(h.StateDir, searchHistoryFile)
}

func (h *SearchHistory) maxEntries() int {
	if h != nil && h.MaxEntries > 0 {
		return h.MaxEntries
	}
	return defaultMaxSearch
}

// List returns stored search queries ordered newest-first.
func (h *SearchHistory) List() ([]string, error) {
	p := h.path()
	if p == "" {
		return nil, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return readSearchHistoryFile(p)
}

// Record prepends query to search_history.tsv, deduplicating case-insensitively and capping size.
func (h *SearchHistory) Record(query string) error {
	q := sanitizeField(query)
	p := h.path()
	if q == "" || p == "" {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	existing, err := readSearchHistoryFile(p)
	if err != nil {
		return err
	}

	limit := h.maxEntries()
	updated := make([]string, 0, limit)
	updated = append(updated, q)
	for _, prev := range existing {
		if strings.EqualFold(prev, q) {
			continue
		}
		updated = append(updated, prev)
		if len(updated) >= limit {
			break
		}
	}

	var buf bytes.Buffer
	for _, item := range updated {
		buf.WriteString(item)
		buf.WriteByte('\n')
	}
	return writeBytesAtomic(p, buf.Bytes())
}

func readSearchHistoryFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := sanitizeField(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out, scanner.Err()
}
