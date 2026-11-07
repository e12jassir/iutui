package model

import "fmt"

// Tab represents the active view category in the sidebar.
type Tab int

const (
	TabHome Tab = iota
	TabMusic
	TabSubscriptions
	TabDownloads
	TabHistory
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
	Duration  string `json:"duration"`
	Views     string `json:"views"`
	Published string `json:"published"`
	IsLive    bool   `json:"is_live"`
	Thumbnail string `json:"thumbnail,omitempty"`
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
