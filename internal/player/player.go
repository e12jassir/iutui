package player

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

// Status represents the current mpv playback state.
type Status struct {
	Title         string  `json:"title"`
	TimePos       float64 `json:"time_pos"`
	Duration      float64 `json:"duration"`
	Paused        bool    `json:"paused"`
	Volume        float64 `json:"volume"`
	PlaylistCount int     `json:"playlist_count"`
	IsRunning     bool    `json:"is_running"`
}

// PlayVideo triggers playback through the existing yt infrastructure.
func PlayVideo(v model.Video, mode string) error {
	if mode == "" {
		mode = "auto"
	}
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", v.ID)

	ytBin := resolveYtBin()

	cmd := exec.Command(ytBin, "__do", "play", mode, v.ID, url, v.Title, v.Channel, v.Duration)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func resolveYtBin() string {
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "yt")
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling
		}
	}
	if info, err := os.Stat("./bin/yt"); err == nil && !info.IsDir() {
		return "./bin/yt"
	}
	if p, err := exec.LookPath("yt"); err == nil {
		return p
	}
	return "yt"
}

// FindSocket locates the active mpv.sock.
func FindSocket() string {
	if sock := os.Getenv("YT_SOCK"); sock != "" {
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

	payload, err := json.Marshal(map[string]any{
		"command": args,
	})
	if err != nil {
		return "", err
	}

	conn.SetDeadline(time.Now().Add(800 * time.Millisecond))
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return "", err
	}

	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}

	return string(buf[:n]), nil
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

// AddVolume changes playback volume by delta (e.g. +5, -5).
func AddVolume(delta float64) error {
	_, err := SendCommand("add", "volume", delta)
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

	// Helper to get property
	getProp := func(prop string) (any, error) {
		res, err := SendCommand("get_property", prop)
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

	st := Status{IsRunning: false}

	// Verify mpv actually responds to an IPC property query before marking IsRunning = true
	paused, err := getProp("pause")
	if err != nil {
		// Stale socket left over by a dead mpv process; remove it so FindSocket stays clean
		_ = os.Remove(sockPath)
		return Status{IsRunning: false}, nil
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
