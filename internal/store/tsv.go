package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const (
	watchLaterFile    = "watchlater.tsv"
	likedFile         = "liked.tsv"
	historyFile       = "history.tsv"
	subscriptionsFile = "subscriptions.tsv"

	// MaxHistoryEntries is the maximum number of videos retained in history.tsv.
	MaxHistoryEntries = 500
)

// TSVStore implements Store by atomically reading and writing TSV files
// inside StateDir.
type TSVStore struct {
	StateDir string
	mu       sync.Mutex
}

// NewTSVStore constructs a TSVStore rooted at stateDir. If stateDir is empty,
// it resolves $XDG_STATE_HOME/yt (or ~/.local/state/yt).
func NewTSVStore(stateDir string) *TSVStore {
	if stateDir == "" {
		xdgState := os.Getenv("XDG_STATE_HOME")
		if xdgState == "" {
			home, _ := os.UserHomeDir()
			xdgState = filepath.Join(home, ".local", "state")
		}
		stateDir = filepath.Join(xdgState, "yt")
	}
	return &TSVStore{StateDir: stateDir}
}

// Saved returns all videos currently stored in watchlater.tsv.
func (s *TSVStore) Saved(ctx context.Context) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readVideos(filepath.Join(s.StateDir, watchLaterFile))
}

// ToggleSaved adds v to watchlater.tsv if absent (returning true) or removes it
// if already present (returning false). Writes are atomic.
func (s *TSVStore) ToggleSaved(ctx context.Context, v model.Video) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if strings.TrimSpace(v.ID) == "" {
		return false, errors.New("video ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.StateDir, watchLaterFile)
	existing, err := s.readVideos(path)
	if err != nil {
		return false, err
	}

	found := false
	updated := make([]model.Video, 0, len(existing)+1)
	for _, item := range existing {
		if item.ID == v.ID {
			found = true
			continue
		}
		updated = append(updated, item)
	}

	saved := !found
	if saved {
		updated = append(updated, v)
	}

	if err := writeVideosAtomic(path, updated); err != nil {
		return false, err
	}
	return saved, nil
}

// Liked returns all videos stored in liked.tsv.
func (s *TSVStore) Liked(ctx context.Context) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readVideos(filepath.Join(s.StateDir, likedFile))
}

// ToggleLiked adds v to liked.tsv if absent (returning true) or removes it
// if already present (returning false). Writes are atomic.
func (s *TSVStore) ToggleLiked(ctx context.Context, v model.Video) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if strings.TrimSpace(v.ID) == "" {
		return false, errors.New("video ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.StateDir, likedFile)
	existing, err := s.readVideos(path)
	if err != nil {
		return false, err
	}

	found := false
	updated := make([]model.Video, 0, len(existing)+1)
	for _, item := range existing {
		if item.ID == v.ID {
			found = true
			continue
		}
		updated = append(updated, item)
	}

	liked := !found
	if liked {
		updated = append([]model.Video{v}, updated...)
	}

	if err := writeVideosAtomic(path, updated); err != nil {
		return false, err
	}
	return liked, nil
}

// History returns all videos stored in history.tsv.
func (s *TSVStore) History(ctx context.Context) ([]model.Video, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readVideos(filepath.Join(s.StateDir, historyFile))
}

// RecordHistory prepends v to history.tsv, deduplicates any prior entry
// with the same video ID, and caps the stored entries at MaxHistoryEntries.
// Writes are atomic.
func (s *TSVStore) RecordHistory(ctx context.Context, v model.Video) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(v.ID) == "" {
		return errors.New("video ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.StateDir, historyFile)
	existing, err := s.readVideos(path)
	if err != nil {
		return err
	}

	updated := make([]model.Video, 0, len(existing)+1)
	updated = append(updated, v)
	for _, item := range existing {
		if item.ID == v.ID {
			continue
		}
		updated = append(updated, item)
	}
	if len(updated) > MaxHistoryEntries {
		updated = updated[:MaxHistoryEntries]
	}

	return writeVideosAtomic(path, updated)
}

type subscriptionEntry struct {
	Ref   string
	Label string
}

// Subscriptions returns the channel references listed in subscriptions.tsv.
func (s *TSVStore) Subscriptions(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readSubscriptions(filepath.Join(s.StateDir, subscriptionsFile))
}

// SubscriptionLabels returns optional human-readable display names stored alongside
// channel references in subscriptions.tsv.
func (s *TSVStore) SubscriptionLabels(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readSubscriptionEntries(filepath.Join(s.StateDir, subscriptionsFile))
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	labels := make(map[string]string)
	for _, e := range entries {
		if e.Label != "" {
			labels[e.Ref] = e.Label
		}
	}
	return labels, nil
}

// ToggleSubscription adds channelRef to subscriptions.tsv if absent (returning true)
// or removes matching entries case-insensitively if already present (returning false).
func (s *TSVStore) ToggleSubscription(ctx context.Context, channelRef string) (bool, error) {
	return s.ToggleSubscriptionLabeled(ctx, channelRef, "")
}

// ToggleSubscriptionLabeled adds channelRef with an optional display label (stored in
// column 2 of subscriptions.tsv) if absent, or removes matching entries case-insensitively,
// preserving labels on all other subscriptions.
func (s *TSVStore) ToggleSubscriptionLabeled(ctx context.Context, channelRef, label string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ref := sanitizeField(channelRef)
	if ref == "" {
		return false, errors.New("channel reference is required")
	}
	lbl := sanitizeField(label)
	if strings.EqualFold(lbl, ref) {
		lbl = ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.StateDir, subscriptionsFile)
	existing, err := s.readSubscriptionEntries(path)
	if err != nil {
		return false, err
	}

	found := false
	updated := make([]subscriptionEntry, 0, len(existing)+1)
	for _, sub := range existing {
		if strings.EqualFold(strings.TrimSpace(sub.Ref), ref) {
			found = true
			continue
		}
		updated = append(updated, sub)
	}

	subscribed := !found
	if subscribed {
		updated = append(updated, subscriptionEntry{Ref: ref, Label: lbl})
	}

	if err := writeSubscriptionEntries(path, updated); err != nil {
		return false, err
	}
	return subscribed, nil
}

// SetSubscriptionLabel persists a display label for an existing subscription in
// subscriptions.tsv when the label is non-empty and differs from ref.
func (s *TSVStore) SetSubscriptionLabel(ctx context.Context, channelRef, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref := sanitizeField(channelRef)
	lbl := sanitizeField(label)
	if ref == "" || lbl == "" || strings.EqualFold(lbl, ref) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.StateDir, subscriptionsFile)
	entries, err := s.readSubscriptionEntries(path)
	if err != nil {
		return err
	}
	changed := false
	for i := range entries {
		if strings.EqualFold(strings.TrimSpace(entries[i].Ref), ref) {
			if entries[i].Label != lbl {
				entries[i].Label = lbl
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return writeSubscriptionEntries(path, entries)
}

func writeSubscriptionEntries(path string, entries []subscriptionEntry) error {
	var buf bytes.Buffer
	for _, sub := range entries {
		ref := sanitizeField(sub.Ref)
		lbl := sanitizeField(sub.Label)
		if ref == "" {
			continue
		}
		if lbl != "" && !strings.EqualFold(lbl, ref) {
			fmt.Fprintf(&buf, "%s\t%s\n", ref, lbl)
		} else {
			fmt.Fprintf(&buf, "%s\n", ref)
		}
	}
	return writeBytesAtomic(path, buf.Bytes())
}

func (s *TSVStore) readSubscriptions(path string) ([]string, error) {
	entries, err := s.readSubscriptionEntries(path)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	subs := make([]string, 0, len(entries))
	for _, e := range entries {
		subs = append(subs, e.Ref)
	}
	return subs, nil
}

func (s *TSVStore) readSubscriptionEntries(path string) ([]subscriptionEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var subs []subscriptionEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ref := line
		label := ""
		if idx := strings.IndexByte(line, '\t'); idx >= 0 {
			ref = strings.TrimSpace(line[:idx])
			label = strings.TrimSpace(line[idx+1:])
			if tab2 := strings.IndexByte(label, '\t'); tab2 >= 0 {
				label = strings.TrimSpace(label[:tab2])
			}
		}
		if ref != "" {
			subs = append(subs, subscriptionEntry{Ref: ref, Label: label})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *TSVStore) readVideos(path string) ([]model.Video, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var videos []model.Video
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		v, ok := parseVideoRow(line)
		if ok {
			videos = append(videos, v)
		}
	}
	return videos, scanner.Err()
}

func parseVideoRow(line string) (model.Video, bool) {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return model.Video{}, false
	}

	id := strings.TrimSpace(parts[0])
	if id == "" || strings.Contains(id, " ") {
		return model.Video{}, false
	}

	v := model.Video{ID: id}
	if len(parts) >= 5 && strings.HasPrefix(strings.TrimSpace(parts[1]), "http") {
		v.Title = strings.TrimSpace(parts[2])
		v.Channel = strings.TrimSpace(parts[3])
		v.Duration = strings.TrimSpace(parts[4])
		if len(parts) >= 6 {
			v.Views = normalizeNA(strings.TrimSpace(parts[5]))
		}
	} else {
		v.Title = strings.TrimSpace(parts[1])
		if len(parts) >= 3 {
			v.Channel = normalizeNA(strings.TrimSpace(parts[2]))
		}
		if len(parts) >= 4 {
			v.Duration = normalizeNA(strings.TrimSpace(parts[3]))
		}
		if len(parts) >= 6 {
			v.Views = normalizeNA(strings.TrimSpace(parts[5]))
		}
		if len(parts) >= 7 && strings.TrimSpace(parts[6]) == "live" {
			v.IsLive = true
		}
	}
	return v, true
}

func normalizeNA(s string) string {
	if s == "NA" || s == "-" {
		return ""
	}
	return s
}

func writeVideosAtomic(path string, videos []model.Video) error {
	var buf bytes.Buffer
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for _, v := range videos {
		channel := sanitizeField(v.Channel)
		if channel == "" {
			channel = "-"
		}
		dur := sanitizeField(v.Duration)
		if dur == "" {
			dur = "-"
		}
		views := sanitizeField(v.Views)
		if views == "" {
			views = "NA"
		}
		live := "NA"
		if v.IsLive {
			live = "live"
		}
		url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", sanitizeField(v.ID))
		fmt.Fprintf(&buf, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			sanitizeField(v.ID),
			sanitizeField(v.Title),
			channel,
			dur,
			url,
			views,
			live,
			ts,
		)
	}
	return writeBytesAtomic(path, buf.Bytes())
}

func writeBytesAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*.tsv")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func sanitizeField(s string) string {
	r := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return strings.TrimSpace(r.Replace(s))
}
