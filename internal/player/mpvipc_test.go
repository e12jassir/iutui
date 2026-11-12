package player

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/e12jassir/ytx/internal/model"
)

var (
	_ Engine = (*IPCClient)(nil)
	_ Engine = (*FakeEngine)(nil)
)

type testMPVServer struct {
	sockPath string
	listener net.Listener

	mu      sync.Mutex
	conns   []net.Conn
	onCmd   func(conn net.Conn, cmd []any, reqID int64)
	accepts chan net.Conn
	closed  bool
}

func startTestMPVServer(t *testing.T, onCmd func(conn net.Conn, cmd []any, reqID int64)) *testMPVServer {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "mpv.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	s := &testMPVServer{
		sockPath: sockPath,
		listener: ln,
		onCmd:    onCmd,
		accepts:  make(chan net.Conn, 8),
	}

	t.Cleanup(func() {
		s.Close()
	})

	go s.acceptLoop()
	return s
}

func (s *testMPVServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns = append(s.conns, conn)
		s.mu.Unlock()

		select {
		case s.accepts <- conn:
		default:
		}

		go s.serveConn(conn)
	}
}

func (s *testMPVServer) serveConn(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var req struct {
				Command   []any `json:"command"`
				RequestID int64 `json:"request_id"`
			}
			if jErr := json.Unmarshal(line, &req); jErr == nil {
				if s.onCmd != nil {
					s.onCmd(conn, req.Command, req.RequestID)
				} else {
					_ = s.SendJSON(conn, map[string]any{
						"error":      "success",
						"request_id": req.RequestID,
					})
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *testMPVServer) SendJSON(conn net.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = conn.Write(append(b, '\n'))
	return err
}

func (s *testMPVServer) CloseConnections() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *testMPVServer) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.listener.Close()
	s.CloseConnections()
}

func TestIPCClient_RequestIDCorrelationWithInterleavedEvents(t *testing.T) {
	var (
		mu       sync.Mutex
		observed []string
	)

	srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
		if len(cmd) == 0 {
			return
		}
		name, _ := cmd[0].(string)
		if name == "observe_property" && len(cmd) >= 3 {
			if prop, ok := cmd[2].(string); ok {
				mu.Lock()
				observed = append(observed, prop)
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
			return
		}

		// Interleave unsolicited events before replying to the command.
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"event": "property-change",
			"name":  "time-pos",
			"data":  float64(reqID) * 1.5,
		})

		switch name {
		case "playlist-next":
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "property unavailable",
				"request_id": reqID,
			})
		default:
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		}
	})

	client := NewIPCClient(WithSocketPath(srv.sockPath), WithCommandTimeout(2*time.Second))
	defer client.Close()

	// Wait briefly for observe_property messages to be processed by the server.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(observed)
		mu.Unlock()
		if n == len(observedProperties) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	if len(observed) != len(observedProperties) {
		mu.Unlock()
		t.Fatalf("expected %d observed properties, got %d (%v)", len(observedProperties), len(observed), observed)
	}
	mu.Unlock()

	tests := []struct {
		name    string
		call    func(context.Context) error
		wantErr bool
	}{
		{name: "toggle pause succeeds", call: client.TogglePause, wantErr: false},
		{name: "seek succeeds", call: func(ctx context.Context) error { return client.Seek(ctx, 10) }, wantErr: false},
		{name: "add volume succeeds", call: func(ctx context.Context) error { return client.AddVolume(ctx, 5) }, wantErr: false},
		{name: "next propagates mpv error", call: client.Next, wantErr: true},
		{name: "prev succeeds", call: client.Prev, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestIPCClient_LongJSONLinesOver16KB(t *testing.T) {
	longTitle := strings.Repeat("A", 20*1024) // >16KB, well past old 8KB buffer

	srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
		// Emit a >16KB property-change event and a >16KB command response.
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"event": "property-change",
			"name":  "media-title",
			"data":  longTitle,
		})
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"data":       longTitle,
			"error":      "success",
			"request_id": reqID,
		})
	})

	client := NewIPCClient(WithSocketPath(srv.sockPath))
	defer client.Close()

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := client.Subscribe(subCtx)

	if err := client.TogglePause(context.Background()); err != nil {
		t.Fatalf("TogglePause failed on >16KB response: %v", err)
	}

	var got Status
	timeout := time.After(2 * time.Second)
	for got.Title != longTitle {
		select {
		case st, ok := <-ch:
			if !ok {
				t.Fatalf("subscription closed unexpectedly")
			}
			got = st
		case <-timeout:
			t.Fatalf("timed out waiting for >16KB media-title; last len=%d", len(got.Title))
		}
	}
}

func TestIPCClient_ObservePropertyUpdatesAndEndFile(t *testing.T) {
	srv := startTestMPVServer(t, nil)
	client := NewIPCClient(WithSocketPath(srv.sockPath))
	defer client.Close()

	var serverConn net.Conn
	select {
	case serverConn = <-srv.accepts:
	case <-time.After(time.Second):
		t.Fatalf("server did not accept client connection")
	}

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := client.Subscribe(subCtx)

	events := []map[string]any{
		{"event": "property-change", "name": "media-title", "data": "Pink Floyd - Time"},
		{"event": "property-change", "name": "duration", "data": 413.5},
		{"event": "property-change", "name": "time-pos", "data": 42.0},
		{"event": "property-change", "name": "pause", "data": true},
		{"event": "property-change", "name": "volume", "data": 85.0},
		{"event": "property-change", "name": "playlist-count", "data": 3},
	}

	for _, ev := range events {
		if err := srv.SendJSON(serverConn, ev); err != nil {
			t.Fatalf("send event: %v", err)
		}
	}

	var latest Status
	deadline := time.After(2 * time.Second)
	for latest.PlaylistCount != 3 || latest.Title != "Pink Floyd - Time" || !latest.Paused || latest.Volume != 85.0 {
		select {
		case st := <-ch:
			latest = st
		case <-deadline:
			t.Fatalf("timed out waiting for status updates; last=%+v", latest)
		}
	}

	if !latest.IsRunning || latest.Duration != 413.5 || latest.TimePos != 42.0 {
		t.Fatalf("unexpected status fields: %+v", latest)
	}

	// Send end-file and verify TimePos resets to 0.
	if err := srv.SendJSON(serverConn, map[string]any{"event": "end-file", "reason": "eof"}); err != nil {
		t.Fatalf("send end-file: %v", err)
	}

	deadline = time.After(2 * time.Second)
	for latest.TimePos != 0 {
		select {
		case st := <-ch:
			latest = st
		case <-deadline:
			t.Fatalf("timed out waiting for end-file TimePos reset; last=%+v", latest)
		}
	}
}

func TestIPCClient_DisconnectResetsRunningAndReconnects(t *testing.T) {
	srv := startTestMPVServer(t, nil)
	client := NewIPCClient(
		WithSocketPath(srv.sockPath),
		WithBackoff(20*time.Millisecond, 100*time.Millisecond),
	)
	defer client.Close()

	select {
	case conn := <-srv.accepts:
		if err := srv.SendJSON(conn, map[string]any{
			"event": "property-change",
			"name":  "media-title",
			"data":  "Track 1",
		}); err != nil {
			t.Fatalf("send title: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("initial accept timed out")
	}

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := client.Subscribe(subCtx)

	// Wait until Track 1 is reflected in Status.
	deadline := time.After(time.Second)
	for client.Status().Title != "Track 1" {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("timed out waiting for Track 1 status")
		}
	}

	// Force disconnect from server side.
	srv.CloseConnections()

	// Expect IsRunning: false on disconnect.
	seenReset := false
	deadline = time.After(2 * time.Second)
	for !seenReset {
		if !client.Status().IsRunning {
			seenReset = true
			break
		}
		select {
		case st := <-ch:
			if !st.IsRunning {
				seenReset = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for IsRunning=false after disconnect; status=%+v", client.Status())
		}
	}

	// Expect automatic reconnect via exponential backoff.
	select {
	case <-srv.accepts:
	case <-time.After(2 * time.Second):
		t.Fatalf("client did not reconnect after disconnect")
	}

	deadline = time.After(time.Second)
	for !client.Status().IsRunning {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("expected IsRunning=true after reconnect")
		}
	}
}

func TestFakeEngine(t *testing.T) {
	fake := NewFakeEngine()
	defer fake.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := fake.Subscribe(ctx)

	v := model.Video{ID: "abc123", Title: "Demo"}
	if err := fake.Play(ctx, v, "audio"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := fake.TogglePause(ctx); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	if err := fake.Seek(ctx, 15); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if err := fake.AddVolume(ctx, 10); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}

	st := fake.Status()
	if !st.IsRunning || st.Title != "Demo" || !st.Paused || st.TimePos != 15 || st.Volume != 10 {
		t.Fatalf("unexpected FakeEngine status: %+v", st)
	}
	if len(fake.Calls()) != 4 {
		t.Fatalf("expected 4 calls, got %d (%s)", len(fake.Calls()), fmt.Sprintf("%+v", fake.Calls()))
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("expected status update on Subscribe channel")
	}
}
