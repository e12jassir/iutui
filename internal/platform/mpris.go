package platform

import (
	"math"
	"strings"
	"time"
)

const (
	mprisTrackPrefix = "/org/mpris/MediaPlayer2/Track/"
	mprisDefaultID   = "current"
)

// MPRISCallbacks handles incoming media-key / D-Bus control requests.
type MPRISCallbacks struct {
	OnPlayPause func()
	OnPlay      func()
	OnPause     func()
	OnNext      func()
	OnPrevious  func()
	OnSeek      func(delta time.Duration) // relative offset in seconds/duration
	OnStop      func()
}

// MPRISStatus represents playback state published over MPRIS2.
type MPRISStatus struct {
	IsRunning bool
	Paused    bool
	Title     string
	Artist    string
	ArtURL    string
	Duration  float64 // seconds
	Position  float64 // seconds
	Volume    float64 // 0..100
	Rate      float64 // 1.0 default
	TrackID   string
}

// MPRISProps holds normalized MPRIS2 Player property values derived from MPRISStatus.
type MPRISProps struct {
	PlaybackStatus string
	Rate           float64
	Volume         float64
	Metadata       map[string]any
	Position       int64 // microseconds
}

// BuildMPRISProps converts an MPRISStatus snapshot into normalized MPRIS2 Player
// property values without requiring a D-Bus connection.
func BuildMPRISProps(st MPRISStatus) MPRISProps {
	status := "Stopped"
	if st.IsRunning {
		if st.Paused {
			status = "Paused"
		} else {
			status = "Playing"
		}
	}

	rate := st.Rate
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
		rate = 1.0
	}

	volume := st.Volume / 100.0
	switch {
	case math.IsNaN(volume) || volume < 0:
		volume = 0
	case math.IsInf(volume, 1) || volume > 1:
		volume = 1
	}

	var position int64
	if st.IsRunning {
		position = secondsToMicroseconds(st.Position)
	}

	meta := make(map[string]any)
	if st.IsRunning {
		meta["mpris:trackid"] = buildMPRISTrackPath(st.TrackID)
		if length := secondsToMicroseconds(st.Duration); length > 0 {
			meta["mpris:length"] = length
		}
		if st.Title != "" {
			meta["xesam:title"] = st.Title
		}
		if st.Artist != "" {
			meta["xesam:artist"] = []string{st.Artist}
		}
		if st.ArtURL != "" {
			meta["mpris:artUrl"] = st.ArtURL
		}
	}

	return MPRISProps{
		PlaybackStatus: status,
		Rate:           rate,
		Volume:         volume,
		Metadata:       meta,
		Position:       position,
	}
}

func secondsToMicroseconds(sec float64) int64 {
	if math.IsNaN(sec) || math.IsInf(sec, 0) || sec <= 0 {
		return 0
	}
	us := math.Round(sec * 1_000_000)
	if us >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(us)
}

func buildMPRISTrackPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return mprisTrackPrefix + mprisDefaultID
	}
	sanitized := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			r == '_' {
			return r
		}
		return '_'
	}, trimmed)
	if sanitized == "" {
		sanitized = mprisDefaultID
	}
	return mprisTrackPrefix + sanitized
}
