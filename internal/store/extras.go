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

	"github.com/e12jassir/iutui/internal/model"
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

// ResumeStore persists playback positions for videos in resume.json capped at MaxEntries.
type ResumeStore struct {
	StateDir   string
	MaxEntries int
	mu         sync.Mutex
	loaded     bool
	cache      map[string]ResumeEntry
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

func (r *ResumeStore) loadLocked(p string) (map[string]ResumeEntry, error) {
	if r.loaded {
		return r.cache, nil
	}
	entries, err := readResumeFile(p)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = make(map[string]ResumeEntry)
	}
	r.cache = entries
	r.loaded = true
	return r.cache, nil
}

// GetPosition returns the saved timestamp for videoID if present and >= 5s.
func (r *ResumeStore) GetPosition(videoID string) (float64, bool) {
	id := strings.TrimSpace(videoID)
	p := r.path()
	if id == "" || p == "" {
		return 0, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.loadLocked(p)
	if err != nil {
		return 0, false
	}
	e, ok := entries[id]
	if !ok || e.Position < 5 {
		return 0, false
	}
	return e.Position, true
}

// ProgressRatio returns the watched fraction (0..1) for videoID, or 0 if not tracked.
func (r *ResumeStore) ProgressRatio(videoID string) float64 {
	id := strings.TrimSpace(videoID)
	p := r.path()
	if id == "" || p == "" {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.loadLocked(p)
	if err != nil {
		return 0
	}
	e, ok := entries[id]
	if !ok || e.Duration <= 0 || e.Position < 5 {
		return 0
	}
	ratio := e.Position / e.Duration
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// SavePosition records position for any video (regardless of duration) when within
// [5s, duration-5s) and under 95% watched; finished or unstarted tracks clear any prior entry.
func (r *ResumeStore) SavePosition(videoID string, position, duration float64) error {
	id := strings.TrimSpace(videoID)
	p := r.path()
	if id == "" || p == "" {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.loadLocked(p)
	if err != nil {
		return err
	}
	if entries == nil {
		entries = make(map[string]ResumeEntry)
		r.cache = entries
	}

	if duration <= 0 || position < 5 || position >= duration-5 || (position/duration) >= 0.95 {
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

const musicStatsFile = "music_stats.json"

// TrackStat stores cumulative local playback engagement for a single video ID.
type TrackStat struct {
	VideoID      string    `json:"video_id"`
	Title        string    `json:"title,omitempty"`
	Artist       string    `json:"artist,omitempty"`
	Duration     string    `json:"duration,omitempty"`
	Plays        int       `json:"plays"`
	Completes    int       `json:"completes"`
	Skips        int       `json:"skips"`
	LastPlayedAt time.Time `json:"last_played_at"`
}

// ListeningStats persists per-track local plays, completions, and fast skips
// inside StateDir/music_stats.json.
type ListeningStats struct {
	StateDir string
	mu       sync.Mutex
}

// NewListeningStats constructs a ListeningStats store rooted at stateDir.
func NewListeningStats(stateDir string) *ListeningStats {
	return &ListeningStats{StateDir: stateDir}
}

func (l *ListeningStats) path() string {
	if l == nil || strings.TrimSpace(l.StateDir) == "" {
		return ""
	}
	return filepath.Join(l.StateDir, musicStatsFile)
}

// Snapshot returns a copy of all locally recorded track stats.
func (l *ListeningStats) Snapshot() map[string]TrackStat {
	p := l.path()
	if p == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m, err := readMusicStatsFile(p)
	if err != nil || len(m) == 0 {
		return nil
	}
	out := make(map[string]TrackStat, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// RecordPlay increments Plays and updates metadata for v.
func (l *ListeningStats) RecordPlay(v model.Video) error {
	return l.mutate(v, func(st *TrackStat) {
		st.Plays++
		st.LastPlayedAt = time.Now().UTC()
	})
}

// RecordComplete increments Completes and updates metadata for v.
func (l *ListeningStats) RecordComplete(v model.Video) error {
	return l.mutate(v, func(st *TrackStat) {
		st.Completes++
		st.LastPlayedAt = time.Now().UTC()
	})
}

// RecordSkip increments Skips and updates metadata for v.
func (l *ListeningStats) RecordSkip(v model.Video) error {
	return l.mutate(v, func(st *TrackStat) {
		st.Skips++
	})
}

func (l *ListeningStats) mutate(v model.Video, fn func(*TrackStat)) error {
	id := strings.TrimSpace(v.ID)
	p := l.path()
	if id == "" || p == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	m, err := readMusicStatsFile(p)
	if err != nil {
		return err
	}
	if m == nil {
		m = make(map[string]TrackStat)
	}
	st := m[id]
	st.VideoID = id
	if strings.TrimSpace(v.Title) != "" {
		st.Title = strings.TrimSpace(v.Title)
	}
	if ch := strings.TrimSpace(v.Channel); ch != "" && !strings.EqualFold(ch, "YouTube") {
		st.Artist = ch
	}
	if d := strings.TrimSpace(v.Duration); d != "" && d != "-" {
		st.Duration = d
	}
	fn(&st)
	m[id] = st
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeBytesAtomic(p, data)
}

func readMusicStatsFile(path string) (map[string]TrackStat, error) {
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
	var m map[string]TrackStat
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil
	}
	return m, nil
}
