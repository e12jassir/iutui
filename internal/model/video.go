package model

import (
	"fmt"
	"strings"
)

// Tab represents the active view category in the sidebar.
type Tab int

const (
	TabHome Tab = iota
	TabMusic
	TabSubscriptions
	TabDownloads
	TabHistory
	TabWatchLater
	TabLiked
	TabPlaylists
)

func (t Tab) String() string {
	switch t {
	case TabHome:
		return "Inicio"
	case TabMusic:
		return "Música"
	case TabSubscriptions:
		return "Suscripciones"
	case TabDownloads:
		return "Descargas"
	case TabHistory:
		return "Historial"
	case TabWatchLater:
		return "Ver más tarde"
	case TabLiked:
		return "Me gusta"
	case TabPlaylists:
		return "Playlists"
	default:
		return "Desconocido"
	}
}

// Icon returns the glyph representing the tab.
func (t Tab) Icon() string {
	switch t {
	case TabHome:
		return "🏠"
	case TabMusic:
		return "🎵"
	case TabSubscriptions:
		return "🔔"
	case TabDownloads:
		return "📥"
	case TabHistory:
		return "🕒"
	default:
		return "•"
	}
}

// Video represents a single YouTube video item in the feed.
type Video struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	ChannelID string `json:"channel_id,omitempty"`
	Duration  string `json:"duration"`
	Views     string `json:"views"`
	Published string `json:"published"`
	IsLive    bool   `json:"is_live"`
	Thumbnail string `json:"thumbnail,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
}

// Badge returns the visual badge text (e.g. "[🔴 EN VIVO]" or "[05:18]").
func (v Video) Badge() string {
	if v.IsLive {
		return "🔴 EN VIVO"
	}
	if v.Duration != "" {
		return v.Duration
	}
	return "--:--"
}

func (v Video) String() string {
	return fmt.Sprintf("[%s] %s (%s)", v.ID, v.Title, v.Channel)
}

var playlistPrefixes = []string{"PL", "OLAK5uy_", "UU", "FL", "LP"}

// IsMixID reports whether id looks like a YouTube Mix identifier (e.g. RD...).
func IsMixID(id string) bool {
	trimmed := strings.TrimSpace(id)
	return strings.HasPrefix(trimmed, "RD") && len(trimmed) > 2
}

// IsPlaylistID reports whether id represents a YouTube playlist or Mix ID.
func IsPlaylistID(id string) bool {
	trimmed := strings.TrimSpace(id)
	if IsMixID(trimmed) {
		return true
	}
	for _, prefix := range playlistPrefixes {
		if strings.HasPrefix(trimmed, prefix) && len(trimmed) > len(prefix) {
			return true
		}
	}
	return false
}

// MixSeedVideoID extracts the 11-character seed video ID from an RD<videoID> Mix ID.
func MixSeedVideoID(id string) string {
	trimmed := strings.TrimSpace(id)
	if strings.HasPrefix(trimmed, "RD") && len(trimmed) == 13 {
		return trimmed[2:]
	}
	return ""
}

// BuildTargetURL builds the YouTube playback target URL, whether playlists are
// allowed, and the seed video ID (empty for non-seeded playlists).
func BuildTargetURL(id string, asMix bool) (url string, allowPlaylist bool, seedID string) {
	trimmed := strings.TrimSpace(id)
	if seed := MixSeedVideoID(trimmed); seed != "" {
		return "https://www.youtube.com/watch?v=" + seed + "&list=" + trimmed, true, seed
	}
	if IsPlaylistID(trimmed) {
		return "https://www.youtube.com/playlist?list=" + trimmed, true, ""
	}
	if asMix && trimmed != "" {
		return "https://www.youtube.com/watch?v=" + trimmed + "&list=RD" + trimmed, true, trimmed
	}
	return "https://www.youtube.com/watch?v=" + trimmed, false, trimmed
}
