package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatTime(t *testing.T) {
	tests := []struct {
		seconds  float64
		expected string
	}{
		{0, "00:00"},
		{45, "00:45"},
		{125, "02:05"},
		{3661, "01:01:01"},
	}

	for _, tt := range tests {
		got := FormatTime(tt.seconds)
		if got != tt.expected {
			t.Errorf("FormatTime(%f) = %s; want %s", tt.seconds, got, tt.expected)
		}
	}
}

func TestProgressBar(t *testing.T) {
	bar := ProgressBar(50, 100, 10)
	if len(bar) == 0 {
		t.Fatalf("expected non-empty progress bar")
	}
	if bar != "━━━━━●────" {
		t.Errorf("unexpected progress bar: %s", bar)
	}
}

func TestFindSocket_PrefersIutuiRuntimeSocket(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("YT_SOCK", "")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	legacySock := filepath.Join(tmpDir, fmt.Sprintf("yt-%d", os.Getuid()), "mpv.sock")
	if err := os.MkdirAll(filepath.Dir(legacySock), 0o700); err != nil {
		t.Fatalf("mkdir legacy runtime dir: %v", err)
	}
	if err := os.WriteFile(legacySock, nil, 0o600); err != nil {
		t.Fatalf("write legacy socket placeholder: %v", err)
	}

	iutuiSock := filepath.Join(tmpDir, "iutui", "mpv.sock")
	if err := os.MkdirAll(filepath.Dir(iutuiSock), 0o700); err != nil {
		t.Fatalf("mkdir iutui runtime dir: %v", err)
	}
	if err := os.WriteFile(iutuiSock, nil, 0o600); err != nil {
		t.Fatalf("write iutui socket placeholder: %v", err)
	}

	if got := FindSocket(); got != iutuiSock {
		t.Fatalf("FindSocket() = %q, want %q", got, iutuiSock)
	}
}

func TestSendCommand_IgnoresInterleavedEventsAndMatchesRequestID(t *testing.T) {
	sockPath := tempSocketPath(t, "standalone.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			Command   []any  `json:"command"`
			RequestID *int64 `json:"request_id"`
		}
		_ = json.Unmarshal(line, &req)
		if req.RequestID == nil {
			return
		}
		// Send interleaved mpv property-change events and a mismatched request_id first.
		_ = json.NewEncoder(conn).Encode(map[string]any{"event": "property-change", "name": "time-pos", "data": 12.4})
		_ = json.NewEncoder(conn).Encode(map[string]any{"request_id": *req.RequestID + 999, "data": "wrong", "error": "success"})
		_ = json.NewEncoder(conn).Encode(map[string]any{"request_id": *req.RequestID, "data": "exact-match", "error": "success"})
	}()

	t.Setenv("YT_SOCK", sockPath)
	res, err := SendCommand("get_property", "media-title")
	if err != nil {
		t.Fatalf("SendCommand() error: %v", err)
	}
	if !strings.Contains(res, "exact-match") {
		t.Fatalf("expected SendCommand() to return matching request_id payload, got %q", res)
	}
}

func TestGetStatus_PreservesLiveSocketOnTimeoutAndRemovesDeadSocket(t *testing.T) {
	t.Run("read timeout on live socket does NOT delete socket file", func(t *testing.T) {
		sockPath := tempSocketPath(t, "live-slow.sock")
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Fatalf("listen unix: %v", err)
		}
		defer ln.Close()

		done := make(chan struct{})
		defer close(done)
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					<-done
				}(conn)
			}
		}()

		t.Setenv("YT_SOCK", sockPath)
		st, err := GetStatus()
		if err != nil {
			t.Fatalf("GetStatus() error: %v", err)
		}
		if st.IsRunning {
			t.Fatalf("expected IsRunning=false on timeout, got %+v", st)
		}
		if _, statErr := os.Stat(sockPath); statErr != nil {
			t.Fatalf("live socket %q was deleted on read timeout: %v", sockPath, statErr)
		}
	})

	t.Run("stale ECONNREFUSED socket file IS removed", func(t *testing.T) {
		sockPath := tempSocketPath(t, "stale.sock")
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Fatalf("listen unix: %v", err)
		}
		// Close via syscall/fd or leave socket file on disk: net.UnixListener removes on Close unless SetUnlinkOnClose(false).
		if uln, ok := ln.(*net.UnixListener); ok {
			uln.SetUnlinkOnClose(false)
		}
		_ = ln.Close()

		if _, err := os.Stat(sockPath); err != nil {
			t.Fatalf("expected stale socket file on disk before GetStatus: %v", err)
		}

		t.Setenv("YT_SOCK", sockPath)
		st, err := GetStatus()
		if err != nil {
			t.Fatalf("GetStatus() error: %v", err)
		}
		if st.IsRunning {
			t.Fatalf("expected IsRunning=false for dead socket")
		}
		if _, statErr := os.Stat(sockPath); !os.IsNotExist(statErr) {
			t.Fatalf("expected dead socket %q to be removed on ECONNREFUSED, statErr=%v", sockPath, statErr)
		}
	})
}
