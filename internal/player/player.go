package player

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/e12jassir/iutui/internal/platform"
)

var standaloneReqID atomic.Int64

// Status represents the current mpv playback state.
type Status struct {
	Title         string  `json:"title"`
	VideoID       string  `json:"video_id,omitempty"`
	TimePos       float64 `json:"time_pos"`
	Duration      float64 `json:"duration"`
	Paused        bool    `json:"paused"`
	Volume        float64 `json:"volume"`
	Speed         float64 `json:"speed,omitempty"`
	LoopFile      bool    `json:"loop_file,omitempty"`
	PlaylistCount int     `json:"playlist_count"`
	PlaylistPos   int     `json:"playlist_pos"`
	IsRunning     bool    `json:"is_running"`
}

// PlaylistItem represents a single entry in mpv's active playlist.
type PlaylistItem struct {
	Index    int    `json:"index"`
	VideoID  string `json:"video_id,omitempty"`
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	Duration string `json:"duration,omitempty"`
	Filename string `json:"filename"`
	Current  bool   `json:"current"`
}

// FindSocket locates the active mpv.sock.
func FindSocket() string {
	if sock := os.Getenv("YT_SOCK"); sock != "" {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
	}

	if sock := platform.ResolvePaths().SocketPath; sock != "" {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
	}

	uid := os.Getuid()
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		s := filepath.Join(xdg, fmt.Sprintf("yt-%d", uid), "mpv.sock")
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	s := fmt.Sprintf("/tmp/yt-%d/mpv.sock", uid)
	if _, err := os.Stat(s); err == nil {
		return s
	}

	// Search in /tmp/yt.*/mpv.sock
	matches, _ := filepath.Glob("/tmp/yt.*/mpv.sock")
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}

	return ""
}

// SendCommand sends a command over UNIX socket to mpv.
func SendCommand(args ...any) (string, error) {
	sockPath := FindSocket()
	if sockPath == "" {
		return "", fmt.Errorf("mpv socket not found")
	}

	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	reader := bufio.NewReaderSize(conn, 64*1024)
	return sendCommandOnConn(conn, reader, args...)
}

func sendCommandOnConn(conn net.Conn, reader *bufio.Reader, args ...any) (string, error) {
	reqID := standaloneReqID.Add(1)
	payload, err := json.Marshal(map[string]any{
		"command":    args,
		"request_id": reqID,
	})
	if err != nil {
		return "", err
	}

	_ = conn.SetDeadline(time.Now().Add(800 * time.Millisecond))
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return "", err
	}

	for {
		line, err := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var env struct {
				Event     string `json:"event"`
				RequestID *int64 `json:"request_id"`
				Error     string `json:"error"`
			}
			if jErr := json.Unmarshal(trimmed, &env); jErr == nil {
				if env.Event != "" {
					// Ignore asynchronous mpv events interleaved on the socket.
					if err != nil {
						return "", err
					}
					continue
				}
				if env.RequestID != nil {
					if *env.RequestID == reqID {
						return string(line), nil
					}
					if err != nil {
						return "", err
					}
					continue
				}
				if env.Error != "" {
					return string(line), nil
				}
			}
		}
		if err != nil {
			return "", err
		}
	}
}

// TogglePause toggles playback pause state in mpv.
func TogglePause() error {
	_, err := SendCommand("cycle", "pause")
	return err
}

// Next skips to next track in queue.
func Next() error {
	_, err := SendCommand("playlist-next")
	return err
}

// Prev rewinds to previous track in queue.
func Prev() error {
	_, err := SendCommand("playlist-prev")
	return err
}

// Seek seeks relative seconds in mpv.
func Seek(seconds int) error {
	_, err := SendCommand("seek", fmt.Sprintf("%d", seconds))
	return err
}

// GetStatus retrieves current playback status from mpv.sock.
func GetStatus() (Status, error) {
	sockPath := FindSocket()
	if sockPath == "" {
		return Status{IsRunning: false}, nil
	}

	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
			// Stale socket left over by a dead mpv process; remove it so FindSocket stays clean.
			_ = os.Remove(sockPath)
		}
		return Status{IsRunning: false, PlaylistPos: -1}, nil
	}
	defer conn.Close()

	reader := bufio.NewReaderSize(conn, 64*1024)

	// Helper to get property over the established connection.
	getProp := func(prop string) (any, error) {
		res, err := sendCommandOnConn(conn, reader, "get_property", prop)
		if err != nil {
			return nil, err
		}
		var d struct {
			Data  any    `json:"data"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(res), &d); err != nil || d.Error != "success" {
			return nil, fmt.Errorf("error getting %s", prop)
		}
		return d.Data, nil
	}

	st := Status{IsRunning: false, PlaylistPos: -1}

	// Verify mpv actually responds to an IPC property query before marking IsRunning = true.
	paused, err := getProp("pause")
	if err != nil {
		return Status{IsRunning: false, PlaylistPos: -1}, nil
	}

	st.IsRunning = true
	if b, ok := paused.(bool); ok {
		st.Paused = b
	}

	if title, err := getProp("media-title"); err == nil && title != nil {
		st.Title = fmt.Sprintf("%v", title)
	}
	if pos, err := getProp("time-pos"); err == nil && pos != nil {
		if f, ok := pos.(float64); ok {
			st.TimePos = f
		}
	}
	if dur, err := getProp("duration"); err == nil && dur != nil {
		if f, ok := dur.(float64); ok {
			st.Duration = f
		}
	}
	if vol, err := getProp("volume"); err == nil && vol != nil {
		if f, ok := vol.(float64); ok {
			st.Volume = f
		}
	}
	if speed, err := getProp("speed"); err == nil && speed != nil {
		if f, ok := toFloat64(speed); ok && f > 0 {
			st.Speed = f
		}
	}
	if loopFile, err := getProp("loop-file"); err == nil && loopFile != nil {
		if b, ok := parseLoopFileValue(loopFile); ok {
			st.LoopFile = b
		}
	}
	if pPos, err := getProp("playlist-pos"); err == nil && pPos != nil {
		if f, ok := toFloat64(pPos); ok {
			st.PlaylistPos = int(f)
		}
	}
	if pathVal, err := getProp("path"); err == nil && pathVal != nil {
		if s, ok := pathVal.(string); ok {
			st.VideoID = ExtractYouTubeID(s)
		}
	}

	return st, nil
}

// FormatTime formats seconds into MM:SS or HH:MM:SS.
func FormatTime(seconds float64) string {
	s := int(seconds)
	if s < 0 {
		s = 0
	}
	hours := s / 3600
	mins := (s % 3600) / 60
	secs := s % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

// ProgressBar generates a graphical terminal progress bar like ━━━●────────.
func ProgressBar(current, total float64, width int) string {
	if total <= 0 || width < 4 {
		return strings.Repeat("─", width)
	}
	ratio := current / total
	if ratio > 1.0 {
		ratio = 1.0
	}
	pos := int(ratio * float64(width))
	if pos >= width {
		pos = width - 1
	}

	var sb strings.Builder
	for i := 0; i < width; i++ {
		if i < pos {
			sb.WriteString("━")
		} else if i == pos {
			sb.WriteString("●")
		} else {
			sb.WriteString("─")
		}
	}
	return sb.String()
}
