package lyrics

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const offsetsFileName = "lyrics_offsets.json"

// GetOffset returns the persisted sync offset in seconds for videoID, or 0 if unset.
func (c *Client) GetOffset(videoID string) float64 {
	if c == nil {
		return 0
	}
	vid := strings.TrimSpace(videoID)
	if vid == "" {
		return 0
	}

	c.offsetsMu.Lock()
	defer c.offsetsMu.Unlock()

	c.loadOffsetsLocked()
	return c.offsets[vid]
}

// SetOffset rounds offsetSec to 0.1s, clamps it to [-60, 60], removes entries
// near zero (< 0.05s), and atomically persists the map when CacheDir is set.
func (c *Client) SetOffset(videoID string, offsetSec float64) error {
	if c == nil {
		return errors.New("lyrics: nil client")
	}
	vid := strings.TrimSpace(videoID)
	if vid == "" {
		return nil
	}
	if math.IsNaN(offsetSec) {
		return errors.New("lyrics: invalid offset")
	}

	if offsetSec < -60 {
		offsetSec = -60
	} else if offsetSec > 60 {
		offsetSec = 60
	}
	v := math.Round(offsetSec*10) / 10

	c.offsetsMu.Lock()
	defer c.offsetsMu.Unlock()

	c.loadOffsetsLocked()
	if math.Abs(v) < 0.05 {
		delete(c.offsets, vid)
	} else {
		c.offsets[vid] = v
	}

	dir := strings.TrimSpace(c.CacheDir)
	if dir == "" {
		return nil
	}
	return persistOffsetsAtomic(dir, c.offsets)
}

func (c *Client) loadOffsetsLocked() {
	if c.offsets == nil {
		c.offsets = make(map[string]float64)
	}
	if c.offsetsLoaded {
		return
	}

	dir := strings.TrimSpace(c.CacheDir)
	if dir == "" {
		return
	}

	c.offsetsLoaded = true
	data, err := os.ReadFile(filepath.Join(dir, offsetsFileName))
	if err != nil {
		return
	}

	var stored map[string]float64
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}
	for k, v := range stored {
		if _, exists := c.offsets[k]; !exists {
			c.offsets[k] = v
		}
	}
}

func persistOffsetsAtomic(dir string, offsets map[string]float64) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(offsets, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(dir, ".lyrics-offsets-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, offsetsFileName)); err != nil {
		return err
	}

	cleanup = false
	return nil
}
