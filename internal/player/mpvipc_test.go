package player

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
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

func writeStubBin(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir stub bin dir: %v", err)
	}
	binPath := filepath.Join(dir, name)
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(binPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return binPath
}

func TestIPCClient_ConcurrentSetQualityPrefetchPlay(t *testing.T) {
	t.Run("disconnected fallback synchronizes launchDefaults across SetQuality, Prefetch, and Play", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		finalArgsFile := filepath.Join(tmpDir, "final-mpv-args.txt")

		writeStubBin(t, binDir, "yt-dlp", "printf 'https://cdn.example.com/video.mp4\\nhttps://cdn.example.com/audio.m4a\\n'")
		writeStubBin(t, binDir, "mpv", fmt.Sprintf(`for arg in "$@"; do
  if [ "$arg" = "--force-media-title=final-verify-title" ]; then
    printf '%%s\n' "$@" > %q
    break
  fi
done
exit 0`, finalArgsFile))

		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		sockPath := filepath.Join(tmpDir, "run", "mpv.sock")
		client := NewIPCClient(
			WithSocketPath(sockPath),
			WithBackoff(50*time.Millisecond, 100*time.Millisecond),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   sockPath,
				Mode:         "window",
				Quality:      1080,
				FastPlayback: true,
			}),
		)
		defer client.Close()

		const workers = 8
		const iterations = 15
		qualities := []int{0, 1080, 720, 480}
		videos := []model.Video{
			{ID: "vid-a", Title: "Video A"},
			{ID: "vid-b", Title: "Video B"},
			{ID: "vid-c", Title: "Video C"},
			{ID: "vid-d", Title: "Video D"},
		}

		start := make(chan struct{})
		errCh := make(chan error, workers*iterations)
		var wg sync.WaitGroup

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				<-start
				for i := 0; i < iterations; i++ {
					v := videos[(workerID+i)%len(videos)]
					switch (workerID + i) % 3 {
					case 0:
						client.SetQuality(context.Background(), qualities[(workerID+i)%len(qualities)])
					case 1:
						client.Prefetch(v)
					case 2:
						if err := client.Play(context.Background(), v, "window"); err != nil {
							errCh <- err
						}
					}
				}
			}(w)
		}

		close(start)
		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("concurrent Play failed: %v", err)
		}

		client.SetQuality(context.Background(), 480)
		if err := client.Play(context.Background(), model.Video{ID: "final-id", Title: "final-verify-title"}, "window"); err != nil {
			t.Fatalf("final Play failed: %v", err)
		}

		deadline := time.Now().Add(2 * time.Second)
		var rawArgs []byte
		var readErr error
		for time.Now().Before(deadline) {
			rawArgs, readErr = os.ReadFile(finalArgsFile)
			if readErr == nil && len(rawArgs) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if readErr != nil || len(rawArgs) == 0 {
			t.Fatalf("timed out waiting for final fake mpv args: %v", readErr)
		}

		args := strings.Split(strings.TrimSuffix(string(rawArgs), "\n"), "\n")
		wantFmt := "--ytdl-format=bestvideo[height<=480]+bestaudio/best[height<=480]/best"
		if !slices.Contains(args, wantFmt) {
			t.Fatalf("final mpv args %v missing %q", args, wantFmt)
		}
	})

	t.Run("connected IPC handles concurrent SetQuality, Prefetch, and Play", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		writeStubBin(t, binDir, "yt-dlp", "printf 'https://cdn.example.com/video.mp4\\nhttps://cdn.example.com/audio.m4a\\n'")
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		srv := startTestMPVServer(t, nil)
		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      1080,
				FastPlayback: true,
			}),
		)
		defer client.Close()

		const workers = 6
		const iterations = 10
		qualities := []int{0, 1080, 720, 480}
		videos := []model.Video{
			{ID: "conn-a", Title: "Conn A"},
			{ID: "conn-b", Title: "Conn B"},
			{ID: "conn-c", Title: "Conn C"},
		}

		start := make(chan struct{})
		errCh := make(chan error, workers*iterations)
		var wg sync.WaitGroup

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				<-start
				for i := 0; i < iterations; i++ {
					v := videos[(workerID+i)%len(videos)]
					switch (workerID + i) % 3 {
					case 0:
						client.SetQuality(context.Background(), qualities[(workerID+i)%len(qualities)])
					case 1:
						client.Prefetch(v)
					case 2:
						if err := client.Play(context.Background(), v, "window"); err != nil {
							errCh <- err
						}
					}
				}
			}(w)
		}

		close(start)
		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("concurrent connected Play failed: %v", err)
		}
	})
}

func TestIPCClient_PlayStopsPreviousImmediatelyAndLatestWins(t *testing.T) {
	t.Run("sends stop before loading replacement video and falls back cleanly on stale stream", func(t *testing.T) {
		var (
			mu   sync.Mutex
			cmds [][]string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			parts := make([]string, len(cmd))
			for i, item := range cmd {
				parts[i] = fmt.Sprint(item)
			}
			mu.Lock()
			cmds = append(cmds, parts)
			mu.Unlock()

			// Simulate stale cached CDN URL rejection when audio-file option is passed.
			if len(cmd) >= 5 && fmt.Sprint(cmd[0]) == "loadfile" {
				hasAudioOpt := strings.HasPrefix(fmt.Sprint(cmd[4]), "audio-file=")
				if m, ok := cmd[4].(map[string]any); ok {
					if _, exists := m["audio-file"]; exists {
						hasAudioOpt = true
					}
				}
				if hasAudioOpt {
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"error":      "loading failed",
						"request_id": reqID,
					})
					return
				}
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
			}),
		)
		defer client.Close()

		// Seed a cached split stream that mpv will reject so fallback is also exercised.
		client.resolver.mu.Lock()
		client.resolver.cache["second12345"] = ResolvedStream{
			VideoURL:  "https://cdn.example.com/stale-video.mp4",
			AudioURL:  "https://cdn.example.com/stale-audio.m4a",
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		client.resolver.mu.Unlock()

		if err := client.Play(context.Background(), model.Video{ID: "first123456", Title: "First"}, "window"); err != nil {
			t.Fatalf("first Play failed: %v", err)
		}
		if err := client.Play(context.Background(), model.Video{ID: "second12345", Title: "Second"}, "window"); err != nil {
			t.Fatalf("second Play failed: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		stopIdx := -1
		secondFallbackIdx := -1
		for i, c := range cmds {
			if len(c) >= 1 && c[0] == "stop" && i > 0 {
				stopIdx = i
			}
			if len(c) >= 3 && c[0] == "loadfile" && c[1] == "https://www.youtube.com/watch?v=second12345" && c[2] == "replace" {
				secondFallbackIdx = i
			}
		}
		if stopIdx < 0 {
			t.Fatalf("expected stop command before second video, cmds=%v", cmds)
		}
		if secondFallbackIdx <= stopIdx {
			t.Fatalf("expected fallback loadfile for second12345 after stop (stopIdx=%d, loadIdx=%d), cmds=%v", stopIdx, secondFallbackIdx, cmds)
		}
	})

	t.Run("latest Play cancels superseded in-flight Play and wins", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		startedSlow := filepath.Join(tmpDir, "slow-started")
		gateSlow := filepath.Join(tmpDir, "slow-gate")

		ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
for last; do :; done
case "$last" in
  *slow1234567*)
    touch %q
    while [ ! -f %q ]; do sleep 0.01; done
    printf 'https://cdn.example.com/slow.mp4\n'
    ;;
  *)
    printf 'https://cdn.example.com/fast.mp4\n'
    ;;
esac
`, startedSlow, gateSlow))

		var (
			mu    sync.Mutex
			loads []string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 2 && fmt.Sprint(cmd[0]) == "loadfile" {
				mu.Lock()
				loads = append(loads, fmt.Sprint(cmd[1]))
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
				YtdlpBin:     ytdlp,
			}),
		)
		defer client.Close()

		// Start Prefetch for slow video so Play enters WaitOrGet.
		client.Prefetch(model.Video{ID: "slow1234567", Title: "Slow"})
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(startedSlow); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if _, err := os.Stat(startedSlow); err != nil {
			t.Fatal("slow prefetch did not start in time")
		}

		slowDone := make(chan error, 1)
		go func() {
			slowDone <- client.Play(context.Background(), model.Video{ID: "slow1234567", Title: "Slow"}, "window")
		}()

		// Give slow Play a moment to enter WaitOrGet, then supersede with fast Play.
		time.Sleep(30 * time.Millisecond)
		if err := client.Play(context.Background(), model.Video{ID: "fast7654321", Title: "Fast"}, "window"); err != nil {
			t.Fatalf("fast Play failed: %v", err)
		}

		// Release slow lookup gate and wait for slow Play to return cleanly.
		if err := os.WriteFile(gateSlow, []byte("go"), 0o600); err != nil {
			t.Fatalf("write gate: %v", err)
		}
		select {
		case err := <-slowDone:
			if err != nil {
				t.Fatalf("superseded slow Play returned error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("slow Play did not unblock when superseded")
		}

		mu.Lock()
		defer mu.Unlock()
		if len(loads) == 0 || loads[len(loads)-1] != "https://www.youtube.com/watch?v=fast7654321" {
			t.Fatalf("expected fast video to be the final loaded URL, got loads=%v", loads)
		}
		for _, url := range loads {
			if strings.Contains(url, "slow") {
				t.Fatalf("superseded slow Play should not load after/during fast Play, got loads=%v", loads)
			}
		}
	})

	t.Run("unresponsive stop triggers forced disconnect and replaces broken player", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		fallbackArgsFile := filepath.Join(tmpDir, "fallback-mpv.args")

		writeStubBin(t, binDir, "mpv", fmt.Sprintf(`printf '%%s\n' "$@" > %q`, fallbackArgsFile))
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		var srv *testMPVServer
		srv = startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) > 0 && fmt.Sprint(cmd[0]) == "stop" {
				// Stop accepting new sockets and hang on stop so 500ms timeout fires.
				_ = srv.listener.Close()
				return
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithBackoff(2*time.Second, 5*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath: srv.sockPath,
				Mode:       "window",
				Quality:    720,
			}),
		)
		defer client.Close()

		if !client.isConnected() {
			t.Fatal("expected initial IPC connection to test server")
		}

		if err := client.Play(context.Background(), model.Video{ID: "recover1234", Title: "Recover"}, "window"); err != nil {
			t.Fatalf("Play after hung stop failed: %v", err)
		}

		deadline := time.Now().Add(2 * time.Second)
		var b []byte
		var err error
		for time.Now().Before(deadline) {
			b, err = os.ReadFile(fallbackArgsFile)
			if err == nil && len(b) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil || !strings.Contains(string(b), "https://www.youtube.com/watch?v=recover1234") {
			t.Fatalf("expected fallback mpv launch after hung stop, err=%v args=%q", err, string(b))
		}
	})
}

func TestIPCClient_SingleMPVInstanceNoOverlap(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	pidsFile := filepath.Join(tmpDir, "pids.log")

	writeStubBin(t, binDir, "mpv", fmt.Sprintf(`
echo "$$" >> %q
sleep 30 &
wait $!
`, pidsFile))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	readPIDs := func() []int {
		b, err := os.ReadFile(pidsFile)
		if err != nil {
			return nil
		}
		var out []int
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			pid, err := strconv.Atoi(line)
			if err == nil && pid > 0 {
				out = append(out, pid)
			}
		}
		return out
	}

	t.Cleanup(func() {
		for _, pid := range readPIDs() {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	sockPath := filepath.Join(tmpDir, "run", "mpv.sock")
	client := NewIPCClient(
		WithSocketPath(sockPath),
		WithBackoff(200*time.Millisecond, 500*time.Millisecond),
		WithLaunchDefaults(LaunchOpts{
			SocketPath: sockPath,
			Mode:       "window",
			Quality:    720,
		}),
	)

	pidAlive := func(pid int) bool {
		err := syscall.Kill(pid, 0)
		return err == nil
	}

	if err := client.Play(context.Background(), model.Video{ID: "vid11111111", Title: "First"}, "window"); err != nil {
		t.Fatalf("first Play failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(readPIDs()) < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	pids := readPIDs()
	if len(pids) != 1 {
		_ = client.Close()
		t.Fatalf("expected 1 launched mpv pid, got %v", pids)
	}
	pid1 := pids[0]
	if !pidAlive(pid1) {
		_ = client.Close()
		t.Fatalf("expected first mpv pid %d to be alive", pid1)
	}

	// Second Play while first mpv has no IPC socket must kill pid1 before launching pid2.
	if err := client.Play(context.Background(), model.Video{ID: "vid22222222", Title: "Second"}, "window"); err != nil {
		_ = client.Close()
		t.Fatalf("second Play failed: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for len(readPIDs()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	pids = readPIDs()
	if len(pids) != 2 {
		_ = client.Close()
		t.Fatalf("expected 2 total mpv launches, got %v", pids)
	}
	pid2 := pids[1]

	// Wait briefly for kernel process reaping of pid1.
	deadline = time.Now().Add(2 * time.Second)
	for pidAlive(pid1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pidAlive(pid1) {
		_ = client.Close()
		t.Fatalf("first mpv pid %d remained alive after second Play launched pid %d", pid1, pid2)
	}
	if !pidAlive(pid2) {
		_ = client.Close()
		t.Fatalf("second mpv pid %d should be alive", pid2)
	}

	// Close must terminate the active managed mpv process.
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for pidAlive(pid2) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pidAlive(pid2) {
		_ = syscall.Kill(pid2, syscall.SIGKILL)
		t.Fatalf("second mpv pid %d remained alive after Close()", pid2)
	}
}

func TestIPCClient_PlayMixStartsSeedAndAppendsTracks(t *testing.T) {
	t.Run("RD<11char> Mix ID starts seed video immediately and appends deduplicated Mix tracks", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		ytdlpArgsFile := filepath.Join(tmpDir, "ytdlp-args.log")

		ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
printf '%%s\n' "$*" >> %q
printf '{"id":"dQw4w9WgXcQ"}\n{"id":"track000002"}\n{"id":"track000003"}\n{"id":"track000002"}\n'
`, ytdlpArgsFile))

		var (
			mu    sync.Mutex
			loads [][]string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "loadfile" {
				mu.Lock()
				loads = append(loads, []string{fmt.Sprint(cmd[1]), fmt.Sprint(cmd[2])})
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
				YtdlpBin:     ytdlp,
			}),
		)
		defer client.Close()

		if err := client.Play(context.Background(), model.Video{ID: "RDdQw4w9WgXcQ", Title: "Mix Radio"}, "window"); err != nil {
			t.Fatalf("Play RD mix failed: %v", err)
		}

		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(loads)
			mu.Unlock()
			if n >= 3 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Give brief time to ensure no extra duplicates arrive.
		time.Sleep(30 * time.Millisecond)

		mu.Lock()
		gotLoads := slices.Clone(loads)
		mu.Unlock()

		wantLoads := [][]string{
			{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "replace"},
			{"https://www.youtube.com/watch?v=track000002", "append"},
			{"https://www.youtube.com/watch?v=track000003", "append"},
		}
		if len(gotLoads) != len(wantLoads) {
			t.Fatalf("got loads=%v, want %v", gotLoads, wantLoads)
		}
		for i := range wantLoads {
			if !slices.Equal(gotLoads[i], wantLoads[i]) {
				t.Errorf("load[%d] = %v, want %v", i, gotLoads[i], wantLoads[i])
			}
		}

		rawArgs, err := os.ReadFile(ytdlpArgsFile)
		if err != nil {
			t.Fatalf("read yt-dlp args: %v", err)
		}
		if !strings.Contains(string(rawArgs), "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ") {
			t.Errorf("expected RD Mix target URL in yt-dlp invocation, got:\n%s", string(rawArgs))
		}
	})

	t.Run("mode == mix on regular video survives caller ctx cancel and appends RD tracks", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		ytdlpArgsFile := filepath.Join(tmpDir, "ytdlp-mix-args.log")

		ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
printf '%%s\n' "$*" >> %q
sleep 0.05
printf '{"id":"seedVideo11"}\n{"id":"mixNext0001"}\n{"id":"mixNext0002"}\n'
`, ytdlpArgsFile))

		var (
			mu    sync.Mutex
			loads [][]string
			vids  []string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "loadfile" {
				mu.Lock()
				loads = append(loads, []string{fmt.Sprint(cmd[1]), fmt.Sprint(cmd[2])})
				mu.Unlock()
			}
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "set_property" && fmt.Sprint(cmd[1]) == "vid" {
				mu.Lock()
				vids = append(vids, fmt.Sprint(cmd[2]))
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
				YtdlpBin:     ytdlp,
			}),
		)
		defer client.Close()

		callerCtx, cancel := context.WithCancel(context.Background())
		if err := client.Play(callerCtx, model.Video{ID: "seedVideo11", Title: "Seed Track"}, "mix"); err != nil {
			cancel()
			t.Fatalf("Play mode=mix failed: %v", err)
		}
		// Immediately cancel the caller's Play context; async Mix queuing must keep running.
		cancel()

		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(loads)
			mu.Unlock()
			if n >= 3 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		mu.Lock()
		gotLoads := slices.Clone(loads)
		gotVids := slices.Clone(vids)
		mu.Unlock()

		wantLoads := [][]string{
			{"https://www.youtube.com/watch?v=seedVideo11", "replace"},
			{"https://www.youtube.com/watch?v=mixNext0001", "append"},
			{"https://www.youtube.com/watch?v=mixNext0002", "append"},
		}
		if len(gotLoads) != len(wantLoads) {
			t.Fatalf("got loads=%v, want %v", gotLoads, wantLoads)
		}
		for i := range wantLoads {
			if !slices.Equal(gotLoads[i], wantLoads[i]) {
				t.Errorf("load[%d] = %v, want %v", i, gotLoads[i], wantLoads[i])
			}
		}
		if !slices.Contains(gotVids, "auto") {
			t.Errorf("expected mode=mix to map vid property to auto, got %v", gotVids)
		}

		rawArgs, err := os.ReadFile(ytdlpArgsFile)
		if err != nil {
			t.Fatalf("read yt-dlp args: %v", err)
		}
		if !strings.Contains(string(rawArgs), "https://www.youtube.com/watch?v=seedVideo11&list=RDseedVideo11") {
			t.Errorf("expected RD seed Mix URL in yt-dlp invocation, got:\n%s", string(rawArgs))
		}
	})

	t.Run("non-seeded PL playlist starts first entry immediately and appends remaining entries", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")

		ytdlp := writeStubBin(t, binDir, "yt-dlp", `
printf '{"id":"plTrack0001"}\n{"id":"plTrack0002"}\n{"id":"plTrack0003"}\n'
`)

		var (
			mu    sync.Mutex
			loads [][]string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "loadfile" {
				mu.Lock()
				loads = append(loads, []string{fmt.Sprint(cmd[1]), fmt.Sprint(cmd[2])})
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
				YtdlpBin:     ytdlp,
			}),
		)
		defer client.Close()

		if err := client.Play(context.Background(), model.Video{ID: "PLplaylist123", Title: "Playlist"}, "window"); err != nil {
			t.Fatalf("Play PL playlist failed: %v", err)
		}

		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(loads)
			mu.Unlock()
			if n >= 3 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		mu.Lock()
		gotLoads := slices.Clone(loads)
		mu.Unlock()

		wantLoads := [][]string{
			{"https://www.youtube.com/watch?v=plTrack0001", "replace"},
			{"https://www.youtube.com/watch?v=plTrack0002", "append"},
			{"https://www.youtube.com/watch?v=plTrack0003", "append"},
		}
		if len(gotLoads) != len(wantLoads) {
			t.Fatalf("got loads=%v, want %v", gotLoads, wantLoads)
		}
		for i := range wantLoads {
			if !slices.Equal(gotLoads[i], wantLoads[i]) {
				t.Errorf("load[%d] = %v, want %v", i, gotLoads[i], wantLoads[i])
			}
		}
	})
}

func TestIPCClient_PlayClearsStickyAudioFilesBeforeLoadfile(t *testing.T) {
	var (
		mu   sync.Mutex
		cmds [][]any
	)
	srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
		cp := make([]any, len(cmd))
		copy(cp, cmd)
		mu.Lock()
		cmds = append(cmds, cp)
		mu.Unlock()
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"error":      "success",
			"request_id": reqID,
		})
	})

	client := NewIPCClient(
		WithSocketPath(srv.sockPath),
		WithCommandTimeout(2*time.Second),
		WithLaunchDefaults(LaunchOpts{
			SocketPath:   srv.sockPath,
			Mode:         "window",
			Quality:      720,
			FastPlayback: true,
		}),
	)
	defer client.Close()

	client.resolver.mu.Lock()
	client.resolver.cache["split123456"] = ResolvedStream{
		VideoURL:  "https://cdn.example.com/v1.mp4",
		AudioURL:  "https://cdn.example.com/a1.m4a",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	client.resolver.mu.Unlock()

	if err := client.Play(context.Background(), model.Video{ID: "split123456", Title: "First Split"}, "window"); err != nil {
		t.Fatalf("first Play failed: %v", err)
	}
	if err := client.Play(context.Background(), model.Video{ID: "plain123456", Title: "Second Plain"}, "window"); err != nil {
		t.Fatalf("second Play failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	clearCount := 0
	var firstLoad, secondLoad []any
	firstClearIdx, firstLoadIdx := -1, -1
	secondClearIdx, secondLoadIdx := -1, -1
	for i, c := range cmds {
		if len(c) >= 3 && fmt.Sprint(c[0]) == "set_property" && fmt.Sprint(c[1]) == "audio-files" {
			arr, ok := c[2].([]any)
			if !ok || len(arr) != 0 {
				t.Fatalf("expected set_property audio-files [], got %#v", c[2])
			}
			clearCount++
			if firstClearIdx < 0 {
				firstClearIdx = i
			} else if secondClearIdx < 0 {
				secondClearIdx = i
			}
		}
		if len(c) >= 3 && fmt.Sprint(c[0]) == "loadfile" {
			if firstLoad == nil {
				firstLoad = c
				firstLoadIdx = i
			} else if secondLoad == nil {
				secondLoad = c
				secondLoadIdx = i
			}
		}
	}

	if clearCount < 2 {
		t.Fatalf("expected audio-files [] reset before both loadfile calls (count=%d), cmds=%v", clearCount, cmds)
	}
	if firstClearIdx < 0 || firstLoadIdx <= firstClearIdx {
		t.Fatalf("expected audio-files clear before first loadfile (clear=%d, load=%d)", firstClearIdx, firstLoadIdx)
	}
	if secondClearIdx <= firstLoadIdx || secondLoadIdx <= secondClearIdx {
		t.Fatalf("expected audio-files clear before second loadfile (clear2=%d, load2=%d)", secondClearIdx, secondLoadIdx)
	}
	if len(firstLoad) < 5 {
		t.Fatalf("expected split loadfile with per-file audio-file option map, got %v", firstLoad)
	}
	optsMap, ok := firstLoad[4].(map[string]any)
	if !ok || fmt.Sprint(optsMap["audio-file"]) != "https://cdn.example.com/a1.m4a" {
		t.Fatalf("expected loadfile option map with audio-file, got %#v", firstLoad[4])
	}
	if len(secondLoad) < 3 || fmt.Sprint(secondLoad[1]) != "https://www.youtube.com/watch?v=plain123456" {
		t.Fatalf("expected second loadfile for plain123456, got %v", secondLoad)
	}
}

func TestIPCClient_AutoplayQueuesRDTracksAndFallsBackToFeed(t *testing.T) {
	t.Run("default Autoplay=true plays single video immediately and appends deduplicated RD tracks", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		ytdlpArgsFile := filepath.Join(tmpDir, "ytdlp-autoplay.log")

		ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
printf '%%s\n' "$*" >> %q
case "$*" in
  *--flat-playlist*)
    printf '{"id":"autoSeed111"}\n{"id":"autoNext001"}\n{"id":"autoNext002"}\n{"id":"autoNext001"}\n'
    ;;
  *)
    printf 'https://cdn.example.com/prefetched-next.mp4\n'
    ;;
esac
`, ytdlpArgsFile))

		var (
			mu    sync.Mutex
			loads [][]string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "loadfile" {
				mu.Lock()
				loads = append(loads, []string{fmt.Sprint(cmd[1]), fmt.Sprint(cmd[2])})
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
				YtdlpBin:     ytdlp,
			}),
		)
		defer client.Close()

		if err := client.Play(context.Background(), model.Video{ID: "autoSeed111", Title: "Autoplay Seed"}, "window"); err != nil {
			t.Fatalf("Play single video failed: %v", err)
		}

		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(loads)
			mu.Unlock()
			if n >= 3 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)

		mu.Lock()
		gotLoads := slices.Clone(loads)
		mu.Unlock()

		wantLoads := [][]string{
			{"https://www.youtube.com/watch?v=autoSeed111", "replace"},
			{"https://www.youtube.com/watch?v=autoNext001", "append"},
			{"https://www.youtube.com/watch?v=autoNext002", "append"},
		}
		if len(gotLoads) != len(wantLoads) {
			t.Fatalf("got loads=%v, want %v", gotLoads, wantLoads)
		}
		for i := range wantLoads {
			if !slices.Equal(gotLoads[i], wantLoads[i]) {
				t.Errorf("load[%d] = %v, want %v", i, gotLoads[i], wantLoads[i])
			}
		}

		rawArgs, err := os.ReadFile(ytdlpArgsFile)
		if err != nil {
			t.Fatalf("read yt-dlp args: %v", err)
		}
		if !strings.Contains(string(rawArgs), "https://www.youtube.com/watch?v=autoSeed111&list=RDautoSeed111") {
			t.Errorf("expected RD autoplay target URL in yt-dlp args, got:\n%s", string(rawArgs))
		}

		// Confirm next track (remaining[0]) was prefetched.
		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if st, ok := client.resolver.GetCached("autoNext001"); ok && st.VideoURL == "https://cdn.example.com/prefetched-next.mp4" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("expected remaining[0] (autoNext001) to be prefetched by autoplay")
	})

	t.Run("falls back to SetAutoplayFallback IDs when RD mix is empty", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			flatReply string
		}{
			{
				name:      "yt-dlp returns empty output",
				flatReply: "exit 1",
			},
			{
				name:      "yt-dlp returns only seed ID",
				flatReply: `printf '{"id":"seedNoMix01"}\n'`,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				binDir := filepath.Join(tmpDir, "bin")

				ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
case "$*" in
  *--flat-playlist*)
    %s
    ;;
  *)
    printf 'https://cdn.example.com/fallback-prefetched.mp4\n'
    ;;
esac
`, tc.flatReply))

				var (
					mu    sync.Mutex
					loads [][]string
				)
				srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
					if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "loadfile" {
						mu.Lock()
						loads = append(loads, []string{fmt.Sprint(cmd[1]), fmt.Sprint(cmd[2])})
						mu.Unlock()
					}
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"error":      "success",
						"request_id": reqID,
					})
				})

				client := NewIPCClient(
					WithSocketPath(srv.sockPath),
					WithCommandTimeout(2*time.Second),
					WithLaunchDefaults(LaunchOpts{
						SocketPath:   srv.sockPath,
						Mode:         "window",
						Quality:      720,
						FastPlayback: true,
						YtdlpBin:     ytdlp,
					}),
				)
				defer client.Close()

				fallback := []string{"", "  ", "seedNoMix01", "feedTrack01", "feedTrack01", "  feedTrack02  "}
				client.SetAutoplayFallback(fallback)
				// Mutate caller slice to verify defensive copy.
				fallback[3] = "tampered999"

				if err := client.Play(context.Background(), model.Video{ID: "seedNoMix01", Title: "No Mix Video"}, "window"); err != nil {
					t.Fatalf("Play failed: %v", err)
				}

				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					mu.Lock()
					n := len(loads)
					mu.Unlock()
					if n >= 3 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				time.Sleep(30 * time.Millisecond)

				mu.Lock()
				gotLoads := slices.Clone(loads)
				mu.Unlock()

				wantLoads := [][]string{
					{"https://www.youtube.com/watch?v=seedNoMix01", "replace"},
					{"https://www.youtube.com/watch?v=feedTrack01", "append"},
					{"https://www.youtube.com/watch?v=feedTrack02", "append"},
				}
				if len(gotLoads) != len(wantLoads) {
					t.Fatalf("got loads=%v, want %v", gotLoads, wantLoads)
				}
				for i := range wantLoads {
					if !slices.Equal(gotLoads[i], wantLoads[i]) {
						t.Errorf("load[%d] = %v, want %v", i, gotLoads[i], wantLoads[i])
					}
				}
			})
		}
	})

	t.Run("SetAutoplay(false) and live streams disable automatic queuing on single videos", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		flatCallsFile := filepath.Join(tmpDir, "flat-calls.log")

		ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
case "$*" in
  *--flat-playlist*)
    printf 'called\n' >> %q
    printf '{"id":"shouldNotQueue1"}\n'
    ;;
  *)
    printf 'https://cdn.example.com/single.mp4\n'
    ;;
esac
`, flatCallsFile))

		var (
			mu    sync.Mutex
			loads [][]string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "loadfile" {
				mu.Lock()
				loads = append(loads, []string{fmt.Sprint(cmd[1]), fmt.Sprint(cmd[2])})
				mu.Unlock()
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath:   srv.sockPath,
				Mode:         "window",
				Quality:      720,
				FastPlayback: true,
				YtdlpBin:     ytdlp,
			}),
		)
		defer client.Close()

		client.SetAutoplayFallback([]string{"fallback001"})
		client.SetAutoplay(false)

		if err := client.Play(context.Background(), model.Video{ID: "manualVid01", Title: "Manual"}, "window"); err != nil {
			t.Fatalf("Play manualVid01 failed: %v", err)
		}

		// Re-enable autoplay, but play a live stream: live streams must also skip autoplay queuing.
		client.SetAutoplay(true)
		if err := client.Play(context.Background(), model.Video{ID: "liveStream1", Title: "Live", IsLive: true}, "window"); err != nil {
			t.Fatalf("Play liveStream1 failed: %v", err)
		}

		time.Sleep(80 * time.Millisecond)

		mu.Lock()
		gotLoads := slices.Clone(loads)
		mu.Unlock()

		wantLoads := [][]string{
			{"https://www.youtube.com/watch?v=manualVid01", "replace"},
			{"https://www.youtube.com/watch?v=liveStream1", "replace"},
		}
		if len(gotLoads) != len(wantLoads) {
			t.Fatalf("got loads=%v, want %v", gotLoads, wantLoads)
		}
		for i := range wantLoads {
			if !slices.Equal(gotLoads[i], wantLoads[i]) {
				t.Errorf("load[%d] = %v, want %v", i, gotLoads[i], wantLoads[i])
			}
		}
		if _, err := os.Stat(flatCallsFile); !os.IsNotExist(err) {
			t.Fatalf("expected no --flat-playlist calls when autoplay disabled or live, stat err=%v", err)
		}
	})

	t.Run("Pause sends set_property pause true and propagates IPC errors", func(t *testing.T) {
		var (
			mu   sync.Mutex
			cmds [][]any
		)
		var failNext atomicBoolStub
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			cp := make([]any, len(cmd))
			copy(cp, cmd)
			mu.Lock()
			cmds = append(cmds, cp)
			mu.Unlock()

			if failNext.get() && len(cmd) >= 2 && fmt.Sprint(cmd[0]) == "set_property" && fmt.Sprint(cmd[1]) == "pause" {
				_ = json.NewEncoder(conn).Encode(map[string]any{
					"error":      "property unavailable",
					"request_id": reqID,
				})
				return
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(WithSocketPath(srv.sockPath), WithCommandTimeout(2*time.Second))
		defer client.Close()

		if err := client.Pause(context.Background()); err != nil {
			t.Fatalf("Pause() unexpected error: %v", err)
		}

		failNext.set(true)
		if err := client.Pause(context.Background()); err == nil {
			t.Fatal("expected Pause() to return error on mpv IPC failure")
		}

		mu.Lock()
		defer mu.Unlock()

		found := false
		for _, c := range cmds {
			if len(c) == 3 && fmt.Sprint(c[0]) == "set_property" && fmt.Sprint(c[1]) == "pause" {
				if paused, ok := c[2].(bool); ok && paused {
					found = true
					break
				}
			}
		}
		if !found {
			t.Fatalf("expected set_property pause true command, got cmds=%v", cmds)
		}
	})
}

type atomicBoolStub struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBoolStub) set(v bool) {
	a.mu.Lock()
	a.v = v
	a.mu.Unlock()
}

func (a *atomicBoolStub) get() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

func TestIPCClient_WaitOrGetCoalescesUpTo2200ms(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	startedFile := filepath.Join(tmpDir, "started")

	ytdlp := writeStubBin(t, binDir, "yt-dlp", fmt.Sprintf(`
case "$*" in
  *--flat-playlist*)
    exit 1
    ;;
  *)
    touch %q
    sleep 1.15
    printf 'https://cdn.example.com/coalesced-2200.mp4\n'
    ;;
esac
`, startedFile))

	var (
		mu    sync.Mutex
		loads []string
	)
	srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
		if len(cmd) >= 2 && fmt.Sprint(cmd[0]) == "loadfile" {
			mu.Lock()
			loads = append(loads, fmt.Sprint(cmd[1]))
			mu.Unlock()
		}
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"error":      "success",
			"request_id": reqID,
		})
	})

	client := NewIPCClient(
		WithSocketPath(srv.sockPath),
		WithCommandTimeout(2*time.Second),
		WithLaunchDefaults(LaunchOpts{
			SocketPath:   srv.sockPath,
			Mode:         "window",
			Quality:      720,
			FastPlayback: true,
			YtdlpBin:     ytdlp,
		}),
	)
	defer client.Close()

	client.Prefetch(model.Video{ID: "warm2200vid", Title: "Warm 2200"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(startedFile); err != nil {
		t.Fatal("prefetch did not start in time")
	}

	if err := client.Play(context.Background(), model.Video{ID: "warm2200vid", Title: "Warm 2200"}, "window"); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	mu.Lock()
	gotLoads := slices.Clone(loads)
	mu.Unlock()
	if len(gotLoads) != 1 || gotLoads[0] != "https://cdn.example.com/coalesced-2200.mp4" {
		t.Fatalf("expected WaitOrGet 2200ms coalescing to load direct CDN URL, got loads=%v", gotLoads)
	}
}

func TestIPCClient_SponsorBlockAutoSkipAndCooldown(t *testing.T) {
	t.Run("fetches segments for 11-char video, auto-skips when playing, respects pause, toggle, and 5s cooldown", func(t *testing.T) {
		var sbRequested []string
		var sbMu sync.Mutex
		sbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			vid := r.URL.Query().Get("videoID")
			sbMu.Lock()
			sbRequested = append(sbRequested, vid)
			sbMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"segment":[10.0,30.0],"category":"sponsor","UUID":"seg-uuid-1"},
				{"segment":[50.0,70.0],"category":"selfpromo","UUID":"seg-uuid-2"}
			]`))
		}))
		defer sbSrv.Close()

		var (
			cmdMu sync.Mutex
			seeks []float64
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 3 && fmt.Sprint(cmd[0]) == "seek" && fmt.Sprint(cmd[2]) == "absolute" {
				if pos, ok := toFloat64(cmd[1]); ok {
					cmdMu.Lock()
					seeks = append(seeks, pos)
					cmdMu.Unlock()
				}
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		sbClient := NewSponsorBlockClient()
		sbClient.BaseURL = sbSrv.URL

		var clockMu sync.Mutex
		nowTime := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithSponsorBlockClient(sbClient),
			WithLaunchDefaults(LaunchOpts{
				SocketPath: srv.sockPath,
				Mode:       "window",
			}),
		)
		defer client.Close()
		client.sbMu.Lock()
		client.nowFn = func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return nowTime
		}
		client.sbMu.Unlock()

		if !client.SponsorBlockEnabled() {
			t.Fatal("expected NewIPCClient to enable SponsorBlock by default")
		}

		var serverConn net.Conn
		select {
		case serverConn = <-srv.accepts:
		case <-time.After(time.Second):
			t.Fatal("server did not accept client connection")
		}

		client.SetAutoplay(false)
		if err := client.Play(context.Background(), model.Video{ID: "sbVideo1234", Title: "Sponsored"}, "window"); err != nil {
			t.Fatalf("Play failed: %v", err)
		}

		// Wait for async SponsorBlock segment fetch to populate client state.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			client.sbMu.Lock()
			fetched := client.sbFetched
			n := len(client.sbSegments)
			client.sbMu.Unlock()
			if fetched && n == 2 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		seekCount := func() int {
			cmdMu.Lock()
			defer cmdMu.Unlock()
			return len(seeks)
		}

		// Outside segment -> no skip.
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 9.5}); err != nil {
			t.Fatalf("send time-pos 9.5: %v", err)
		}
		// Inside [seg.End-0.5, seg.End) -> no skip (30.0 - 0.5 = 29.5).
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 29.6}); err != nil {
			t.Fatalf("send time-pos 29.6: %v", err)
		}
		time.Sleep(40 * time.Millisecond)
		if seekCount() != 0 {
			t.Fatalf("expected 0 seeks outside skip window, got %v", seeks)
		}

		// Paused inside segment -> must NOT skip.
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "pause", "data": true}); err != nil {
			t.Fatalf("send pause true: %v", err)
		}
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 12.0}); err != nil {
			t.Fatalf("send time-pos 12.0 while paused: %v", err)
		}
		time.Sleep(40 * time.Millisecond)
		if seekCount() != 0 {
			t.Fatalf("expected 0 seeks while paused, got %v", seeks)
		}

		// Unpause and emit time-pos inside [10.0, 29.5) -> triggers skip to 30.0 without deadlocking readLoop.
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "pause", "data": false}); err != nil {
			t.Fatalf("send pause false: %v", err)
		}
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 14.0}); err != nil {
			t.Fatalf("send time-pos 14.0: %v", err)
		}

		deadline = time.Now().Add(2 * time.Second)
		for seekCount() < 1 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if seekCount() != 1 {
			t.Fatalf("expected 1 SponsorBlock skip seek, got %d (%v)", seekCount(), seeks)
		}
		cmdMu.Lock()
		if seeks[0] != 30.0 {
			cmdMu.Unlock()
			t.Fatalf("first seek = %v, want 30.0", seeks[0])
		}
		cmdMu.Unlock()

		// Confirm readLoop is responsive immediately after skip (no mutex/IPC deadlock).
		if err := client.TogglePause(context.Background()); err != nil {
			t.Fatalf("TogglePause after SponsorBlock skip failed (possible readLoop deadlock): %v", err)
		}

		// Advance clock by 2s (<5s cooldown) and re-emit inside same segment -> must NOT re-skip.
		clockMu.Lock()
		nowTime = nowTime.Add(2 * time.Second)
		clockMu.Unlock()
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 15.0}); err != nil {
			t.Fatalf("send time-pos 15.0 during cooldown: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
		if seekCount() != 1 {
			t.Fatalf("expected cooldown to suppress duplicate skip within 5s, got %v", seeks)
		}

		// Advance clock past 5s cooldown -> same segment CAN skip again.
		clockMu.Lock()
		nowTime = nowTime.Add(4 * time.Second)
		clockMu.Unlock()
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 16.0}); err != nil {
			t.Fatalf("send time-pos 16.0 after cooldown: %v", err)
		}
		deadline = time.Now().Add(2 * time.Second)
		for seekCount() < 2 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if seekCount() != 2 {
			t.Fatalf("expected 2nd skip after 5s cooldown expired, got %v", seeks)
		}

		// Disabling SponsorBlock suppresses skipping on seg-uuid-2.
		client.SetSponsorBlock(false)
		if client.SponsorBlockEnabled() {
			t.Fatal("expected SponsorBlockEnabled() == false")
		}
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 55.0}); err != nil {
			t.Fatalf("send time-pos 55.0 while disabled: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
		if seekCount() != 2 {
			t.Fatalf("expected no skip when SponsorBlock disabled, got %v", seeks)
		}

		// Re-enabling SponsorBlock skips seg-uuid-2 when time-pos is observed.
		client.SetSponsorBlock(true)
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 56.0}); err != nil {
			t.Fatalf("send time-pos 56.0 after re-enable: %v", err)
		}
		deadline = time.Now().Add(2 * time.Second)
		for seekCount() < 3 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if seekCount() != 3 {
			t.Fatalf("expected 3rd skip after re-enabling SponsorBlock, got %v", seeks)
		}
		cmdMu.Lock()
		if seeks[2] != 70.0 {
			cmdMu.Unlock()
			t.Fatalf("third seek = %v, want 70.0", seeks[2])
		}
		cmdMu.Unlock()

		sbMu.Lock()
		reqs := slices.Clone(sbRequested)
		sbMu.Unlock()
		if !slices.Equal(reqs, []string{"sbVideo1234"}) {
			t.Fatalf("SponsorBlock requested IDs = %v, want [sbVideo1234]", reqs)
		}
	})

	t.Run("ignores playlist IDs RD/PL and clears prior segments", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		ytdlp := writeStubBin(t, binDir, "yt-dlp", `printf '{"id":"plItem00001"}\n'`)

		var sbHits atomic.Int32
		sbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sbHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"segment":[5.0,20.0],"category":"sponsor","UUID":"uuid-single"}]`))
		}))
		defer sbSrv.Close()

		var seekHits atomic.Int32
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) >= 1 && fmt.Sprint(cmd[0]) == "seek" {
				seekHits.Add(1)
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		sbClient := NewSponsorBlockClient()
		sbClient.BaseURL = sbSrv.URL

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithSponsorBlockClient(sbClient),
			WithLaunchDefaults(LaunchOpts{
				SocketPath: srv.sockPath,
				Mode:       "window",
				YtdlpBin:   ytdlp,
			}),
		)
		defer client.Close()
		client.SetAutoplay(false)

		var serverConn net.Conn
		select {
		case serverConn = <-srv.accepts:
		case <-time.After(time.Second):
			t.Fatal("server did not accept connection")
		}

		// Play single 11-char video first -> fetches segments.
		if err := client.Play(context.Background(), model.Video{ID: "singleVid11", Title: "Single"}, "window"); err != nil {
			t.Fatalf("Play single failed: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for sbHits.Load() < 1 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if sbHits.Load() != 1 {
			t.Fatalf("expected 1 SponsorBlock fetch for single video, got %d", sbHits.Load())
		}

		// Play RD and PL playlists -> must NOT query SponsorBlock and must clear active state.
		for _, plID := range []string{"RDdQw4w9WgXcQ", "PLplaylist123"} {
			if err := client.Play(context.Background(), model.Video{ID: plID, Title: "Playlist"}, "window"); err != nil {
				t.Fatalf("Play %s failed: %v", plID, err)
			}
		}
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "time-pos", "data": 10.0}); err != nil {
			t.Fatalf("send time-pos: %v", err)
		}
		time.Sleep(60 * time.Millisecond)

		if sbHits.Load() != 1 {
			t.Fatalf("expected playlists not to trigger SponsorBlock fetches, hits=%d", sbHits.Load())
		}
		if seekHits.Load() != 0 {
			t.Fatalf("expected stale segments cleared on playlist Play, got %d seeks", seekHits.Load())
		}
	})
}

func TestIPCClient_ToggleVideoMode(t *testing.T) {
	t.Run("disconnected toggles launchDefaults.AudioOnly and updates mpv launch args", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		argsLog := filepath.Join(tmpDir, "mpv-args.log")

		writeStubBin(t, binDir, "mpv", fmt.Sprintf(`printf '%%s\n' "$@" > %q`, argsLog))
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		sockPath := filepath.Join(tmpDir, "run", "mpv.sock")
		client := NewIPCClient(
			WithSocketPath(sockPath),
			WithBackoff(200*time.Millisecond, 500*time.Millisecond),
			WithLaunchDefaults(LaunchOpts{
				SocketPath: sockPath,
				Mode:       "window",
			}),
		)
		defer client.Close()

		videoOn, err := client.ToggleVideoMode(context.Background())
		if err != nil {
			t.Fatalf("first ToggleVideoMode unexpected error: %v", err)
		}
		if videoOn {
			t.Fatal("expected videoOn=false after toggling from window mode to audio-only")
		}

		// Launch mpv while AudioOnly=true and verify --no-video is emitted.
		if err := client.Play(context.Background(), model.Video{ID: "toggleVid01", Title: "AudioLaunch"}, "auto"); err != nil {
			t.Fatalf("Play in audio-only mode failed: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		var rawArgs []byte
		for time.Now().Before(deadline) {
			if rawArgs, err = os.ReadFile(argsLog); err == nil && len(rawArgs) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(rawArgs) == 0 {
			t.Fatalf("timed out waiting for mpv launch args: %v", err)
		}
		if !slices.Contains(strings.Split(strings.TrimSpace(string(rawArgs)), "\n"), "--no-video") {
			t.Fatalf("expected --no-video in mpv launch args after ToggleVideoMode, got:\n%s", string(rawArgs))
		}

		// Toggle back to video ON.
		videoOn, err = client.ToggleVideoMode(context.Background())
		if err != nil {
			t.Fatalf("second ToggleVideoMode unexpected error: %v", err)
		}
		if !videoOn {
			t.Fatal("expected videoOn=true after toggling back")
		}
	})

	t.Run("connected reads and toggles live vid property without stop or loadfile", func(t *testing.T) {
		var (
			mu          sync.Mutex
			currentVid  = "auto"
			cmds        [][]string
			failNextSet bool
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			parts := make([]string, len(cmd))
			for i, item := range cmd {
				parts[i] = fmt.Sprint(item)
			}
			mu.Lock()
			cmds = append(cmds, parts)
			if len(cmd) >= 2 && parts[0] == "get_property" && parts[1] == "vid" {
				val := currentVid
				mu.Unlock()
				_ = json.NewEncoder(conn).Encode(map[string]any{
					"data":       val,
					"error":      "success",
					"request_id": reqID,
				})
				return
			}
			if len(cmd) >= 3 && parts[0] == "set_property" && parts[1] == "vid" {
				if failNextSet {
					failNextSet = false
					mu.Unlock()
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"error":      "property unavailable",
						"request_id": reqID,
					})
					return
				}
				currentVid = parts[2]
			}
			mu.Unlock()
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(
			WithSocketPath(srv.sockPath),
			WithCommandTimeout(2*time.Second),
			WithLaunchDefaults(LaunchOpts{
				SocketPath: srv.sockPath,
				Mode:       "window",
			}),
		)
		defer client.Close()

		// auto -> no (videoOn = false)
		videoOn, err := client.ToggleVideoMode(context.Background())
		if err != nil {
			t.Fatalf("ToggleVideoMode auto->no error: %v", err)
		}
		if videoOn {
			t.Fatal("expected videoOn=false when switching vid to no")
		}
		client.launchMu.RLock()
		audioOnly := client.launchDefaults.AudioOnly
		client.launchMu.RUnlock()
		if !audioOnly {
			t.Fatal("expected launchDefaults.AudioOnly=true after switching vid to no")
		}

		// no -> auto (videoOn = true)
		videoOn, err = client.ToggleVideoMode(context.Background())
		if err != nil {
			t.Fatalf("ToggleVideoMode no->auto error: %v", err)
		}
		if !videoOn {
			t.Fatal("expected videoOn=true when switching vid to auto")
		}
		client.launchMu.RLock()
		audioOnly = client.launchDefaults.AudioOnly
		client.launchMu.RUnlock()
		if audioOnly {
			t.Fatal("expected launchDefaults.AudioOnly=false after switching vid to auto")
		}

		// Failed set_property must return error and keep AudioOnly unchanged.
		mu.Lock()
		failNextSet = true
		mu.Unlock()
		if _, err := client.ToggleVideoMode(context.Background()); err == nil {
			t.Fatal("expected ToggleVideoMode error when set_property vid fails")
		}
		client.launchMu.RLock()
		audioOnly = client.launchDefaults.AudioOnly
		client.launchMu.RUnlock()
		if audioOnly {
			t.Fatal("expected launchDefaults.AudioOnly unchanged after failed set_property")
		}

		// Verify no stop or loadfile commands were issued during live toggles.
		mu.Lock()
		gotCmds := slices.Clone(cmds)
		mu.Unlock()
		var setVidValues []string
		for _, c := range gotCmds {
			if len(c) > 0 && (c[0] == "stop" || c[0] == "loadfile") {
				t.Fatalf("ToggleVideoMode must not restart playback, saw cmd=%v", c)
			}
			if len(c) == 3 && c[0] == "set_property" && c[1] == "vid" {
				setVidValues = append(setVidValues, c[2])
			}
		}
		wantSetVids := []string{"no", "auto", "no"}
		if !slices.Equal(setVidValues, wantSetVids) {
			t.Fatalf("set_property vid sequence = %v, want %v", setVidValues, wantSetVids)
		}
	})
}

func TestIPCClient_ChaptersAndSeekAbsolute(t *testing.T) {
	t.Run("Chapters parses chapter-list, handles empty list, and propagates errors", func(t *testing.T) {
		var (
			mu       sync.Mutex
			mode     = "populated"
			lastCmds [][]any
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			cp := make([]any, len(cmd))
			copy(cp, cmd)
			mu.Lock()
			lastCmds = append(lastCmds, cp)
			curMode := mode
			mu.Unlock()

			if len(cmd) >= 2 && fmt.Sprint(cmd[0]) == "get_property" && fmt.Sprint(cmd[1]) == "chapter-list" {
				switch curMode {
				case "populated":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"data": []any{
							map[string]any{"title": "Intro", "time": 0},
							map[string]any{"title": "Chorus", "time": 63.75},
							map[string]any{"time": 120.5},
						},
						"error":      "success",
						"request_id": reqID,
					})
				case "empty":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"data":       []any{},
						"error":      "success",
						"request_id": reqID,
					})
				case "unavailable":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"error":      "property unavailable",
						"request_id": reqID,
					})
				case "badtype":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"data":       "unexpected-string",
						"error":      "success",
						"request_id": reqID,
					})
				}
				return
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(WithSocketPath(srv.sockPath), WithCommandTimeout(2*time.Second))
		defer client.Close()

		chapters, err := client.Chapters(context.Background())
		if err != nil {
			t.Fatalf("Chapters() unexpected error: %v", err)
		}
		want := []Chapter{
			{Title: "Intro", Time: 0},
			{Title: "Chorus", Time: 63.75},
			{Title: "", Time: 120.5},
		}
		if !reflect.DeepEqual(chapters, want) {
			t.Fatalf("Chapters() = %+v, want %+v", chapters, want)
		}

		mu.Lock()
		mode = "empty"
		mu.Unlock()
		chapters, err = client.Chapters(context.Background())
		if err != nil {
			t.Fatalf("empty Chapters() unexpected error: %v", err)
		}
		if len(chapters) != 0 {
			t.Fatalf("empty Chapters() = %+v, want empty slice", chapters)
		}

		mu.Lock()
		mode = "unavailable"
		mu.Unlock()
		if _, err := client.Chapters(context.Background()); err == nil {
			t.Fatal("expected error when chapter-list is unavailable")
		}

		mu.Lock()
		mode = "badtype"
		mu.Unlock()
		if _, err := client.Chapters(context.Background()); err == nil {
			t.Fatal("expected error for unexpected chapter-list payload type")
		}

		// Verify SeekAbsolute sends ["seek", seconds, "absolute"].
		if err := client.SeekAbsolute(context.Background(), 42.5); err != nil {
			t.Fatalf("SeekAbsolute(42.5) unexpected error: %v", err)
		}
		mu.Lock()
		var seekCmd []any
		for _, c := range lastCmds {
			if len(c) > 0 && fmt.Sprint(c[0]) == "seek" {
				seekCmd = c
			}
		}
		mu.Unlock()
		if len(seekCmd) != 3 || fmt.Sprint(seekCmd[0]) != "seek" || seekCmd[1] != 42.5 || fmt.Sprint(seekCmd[2]) != "absolute" {
			t.Fatalf("SeekAbsolute sent command %#v, want [seek 42.5 absolute]", seekCmd)
		}
	})

	t.Run("Chapters and SeekAbsolute fail when disconnected", func(t *testing.T) {
		client := NewIPCClient(WithSocketPath(filepath.Join(t.TempDir(), "missing.sock")))
		defer client.Close()

		if _, err := client.Chapters(context.Background()); err == nil {
			t.Fatal("expected error from Chapters when disconnected")
		}
		if err := client.SeekAbsolute(context.Background(), 15.0); err == nil {
			t.Fatal("expected error from SeekAbsolute when disconnected")
		}
	})
}

func TestFakeEngine_SponsorBlockVideoModeChaptersAndSeekAbsolute(t *testing.T) {
	fake := NewFakeEngine()
	defer fake.Close()

	ctx := context.Background()
	if !fake.SponsorBlockEnabled() {
		t.Fatal("expected FakeEngine SponsorBlock enabled by default")
	}
	fake.SetSponsorBlock(false)
	if fake.SponsorBlockEnabled() {
		t.Fatal("expected FakeEngine SponsorBlock disabled after SetSponsorBlock(false)")
	}

	videoOn, err := fake.ToggleVideoMode(ctx)
	if err != nil || videoOn {
		t.Fatalf("first ToggleVideoMode = (%v, %v), want (false, nil)", videoOn, err)
	}
	videoOn, err = fake.ToggleVideoMode(ctx)
	if err != nil || !videoOn {
		t.Fatalf("second ToggleVideoMode = (%v, %v), want (true, nil)", videoOn, err)
	}

	wantChapters := []Chapter{
		{Title: "Start", Time: 0},
		{Title: "Solo", Time: 95.5},
	}
	fake.SetChapters(wantChapters)
	gotChapters, err := fake.Chapters(ctx)
	if err != nil {
		t.Fatalf("Chapters error: %v", err)
	}
	if !reflect.DeepEqual(gotChapters, wantChapters) {
		t.Fatalf("Chapters = %+v, want %+v", gotChapters, wantChapters)
	}
	// Mutate returned slice and confirm internal slice is unchanged.
	gotChapters[0].Title = "Tampered"
	gotChapters2, _ := fake.Chapters(ctx)
	if !reflect.DeepEqual(gotChapters2, wantChapters) {
		t.Fatalf("FakeEngine Chapters returned aliased slice: %+v", gotChapters2)
	}

	if err := fake.SeekAbsolute(ctx, 88.25); err != nil {
		t.Fatalf("SeekAbsolute error: %v", err)
	}
	if fake.Status().TimePos != 88.25 {
		t.Fatalf("TimePos = %v, want 88.25", fake.Status().TimePos)
	}

	calls := fake.Calls()
	var methods []string
	for _, c := range calls {
		methods = append(methods, c.Method)
	}
	wantMethods := []string{"SetSponsorBlock", "ToggleVideoMode", "ToggleVideoMode", "Chapters", "Chapters", "SeekAbsolute"}
	if !slices.Equal(methods, wantMethods) {
		t.Fatalf("FakeEngine recorded calls = %v, want %v", methods, wantMethods)
	}
}

func TestIPCClient_TryPlayUpdatesYtdlFormatOnQualityChange(t *testing.T) {
	var (
		mu   sync.Mutex
		cmds [][]any
	)
	srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
		cp := make([]any, len(cmd))
		copy(cp, cmd)
		mu.Lock()
		cmds = append(cmds, cp)
		mu.Unlock()
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"error":      "success",
			"request_id": reqID,
		})
	})

	client := NewIPCClient(
		WithSocketPath(srv.sockPath),
		WithCommandTimeout(2*time.Second),
		WithLaunchDefaults(LaunchOpts{
			SocketPath: srv.sockPath,
			Mode:       "window",
			Quality:    1080,
		}),
	)
	defer client.Close()

	client.SetQuality(context.Background(), 360)
	if err := client.Play(context.Background(), model.Video{ID: "vid360test1", Title: "Low Data Video"}, "window"); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	mu.Lock()
	gotCmds := slices.Clone(cmds)
	mu.Unlock()

	wantFmt := "bestvideo[height<=360]+bestaudio/best[height<=360]/best"
	fmtIdx, loadIdx := -1, -1
	for i, c := range gotCmds {
		if len(c) >= 3 && fmt.Sprint(c[0]) == "set_property" && fmt.Sprint(c[1]) == "ytdl-format" && fmt.Sprint(c[2]) == wantFmt {
			fmtIdx = i
		}
		if len(c) >= 2 && fmt.Sprint(c[0]) == "loadfile" && strings.Contains(fmt.Sprint(c[1]), "vid360test1") {
			loadIdx = i
		}
	}
	if fmtIdx < 0 || loadIdx <= fmtIdx {
		t.Fatalf("expected set_property ytdl-format=%q before loadfile, cmds=%v", wantFmt, gotCmds)
	}
}

func TestIPCClient_PlayResetsGlobalStartAndPassesPerFileStartNone(t *testing.T) {
	var (
		mu   sync.Mutex
		cmds [][]any
	)
	srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
		cp := make([]any, len(cmd))
		copy(cp, cmd)
		mu.Lock()
		cmds = append(cmds, cp)
		mu.Unlock()
		_ = json.NewEncoder(conn).Encode(map[string]any{
			"error":      "success",
			"request_id": reqID,
		})
	})

	client := NewIPCClient(
		WithSocketPath(srv.sockPath),
		WithCommandTimeout(2*time.Second),
		WithLaunchDefaults(LaunchOpts{
			SocketPath: srv.sockPath,
			Mode:       "window",
			Quality:    720,
		}),
	)
	defer client.Close()

	// Seed stale playback metrics as if an outgoing track was at 00:25 / 03:00.
	client.applyPropertyChange("time-pos", 25.0)
	client.applyPropertyChange("duration", 180.0)

	if client.hasManagedProc() {
		t.Fatal("expected reattached test client without managed proc")
	}

	if err := client.Play(context.Background(), model.Video{ID: "reset123456", Title: "Fresh Song"}, "window"); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	mu.Lock()
	gotCmds := slices.Clone(cmds)
	mu.Unlock()

	resetStartIdx, clearTitleIdx, loadIdx := -1, -1, -1
	var loadOpts map[string]any
	for i, c := range gotCmds {
		if len(c) >= 3 && fmt.Sprint(c[0]) == "set_property" && fmt.Sprint(c[1]) == "start" && fmt.Sprint(c[2]) == "none" {
			if resetStartIdx < 0 {
				resetStartIdx = i
			}
		}
		if len(c) >= 3 && fmt.Sprint(c[0]) == "set_property" && fmt.Sprint(c[1]) == "force-media-title" {
			val := fmt.Sprint(c[2])
			if val == "" && clearTitleIdx < 0 {
				clearTitleIdx = i
			}
			if val == "Fresh Song" {
				t.Fatalf("unexpected global set_property force-media-title=%q in %v", val, gotCmds)
			}
		}
		if len(c) >= 5 && fmt.Sprint(c[0]) == "loadfile" && strings.Contains(fmt.Sprint(c[1]), "reset123456") {
			loadIdx = i
			loadOpts, _ = c[4].(map[string]any)
		}
	}

	if resetStartIdx < 0 || loadIdx <= resetStartIdx {
		t.Fatalf("expected set_property start none before loadfile, cmds=%v", gotCmds)
	}
	if clearTitleIdx < 0 || loadIdx <= clearTitleIdx {
		t.Fatalf("expected set_property force-media-title \"\" before loadfile, cmds=%v", gotCmds)
	}
	if loadOpts == nil || fmt.Sprint(loadOpts["start"]) != "none" || fmt.Sprint(loadOpts["force-media-title"]) != "Fresh Song" {
		t.Fatalf("expected loadfile per-file opts start=none & force-media-title='Fresh Song', got %#v", loadOpts)
	}

	st := client.Status()
	if st.Title != "Fresh Song" || st.TimePos != 0 || st.Duration != 0 {
		t.Fatalf("expected clean reset status for Fresh Song, got %+v", st)
	}
}

func TestIPCClient_ObserveSpeedAndLoopFile(t *testing.T) {
	srv := startTestMPVServer(t, nil)
	client := NewIPCClient(
		WithSocketPath(srv.sockPath),
		WithBackoff(20*time.Millisecond, 100*time.Millisecond),
	)
	defer client.Close()

	if got := client.Status().Speed; got != 1.0 {
		t.Fatalf("initial Speed = %v, want 1.0", got)
	}

	var serverConn net.Conn
	select {
	case serverConn = <-srv.accepts:
	case <-time.After(time.Second):
		t.Fatalf("server did not accept client connection")
	}

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := client.Subscribe(subCtx)

	waitStatus := func(desc string, pred func(Status) bool) Status {
		t.Helper()
		if st := client.Status(); pred(st) {
			return st
		}
		deadline := time.After(2 * time.Second)
		for {
			select {
			case st := <-ch:
				if pred(st) {
					return st
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %s; last=%+v", desc, client.Status())
			}
		}
	}

	if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "speed", "data": 1.5}); err != nil {
		t.Fatalf("send speed: %v", err)
	}
	waitStatus("speed=1.5", func(st Status) bool { return st.Speed == 1.5 })

	if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "speed", "data": 0}); err != nil {
		t.Fatalf("send speed=0: %v", err)
	}
	waitStatus("speed<=0 resets to 1.0", func(st Status) bool { return st.Speed == 1.0 })

	if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "speed", "data": -2.5}); err != nil {
		t.Fatalf("send speed=-2.5: %v", err)
	}
	if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "loop-file", "data": "inf"}); err != nil {
		t.Fatalf("send loop-file=inf: %v", err)
	}
	waitStatus("loop-file=inf", func(st Status) bool { return st.Speed == 1.0 && st.LoopFile })

	for _, offVal := range []any{"no", "false", "", false} {
		// First enable via bool or string, then disable with offVal.
		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "loop-file", "data": "yes"}); err != nil {
			t.Fatalf("send loop-file=yes: %v", err)
		}
		waitStatus("loop-file=yes", func(st Status) bool { return st.LoopFile })

		if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "loop-file", "data": offVal}); err != nil {
			t.Fatalf("send loop-file=%v: %v", offVal, err)
		}
		waitStatus(fmt.Sprintf("loop-file=%v", offVal), func(st Status) bool { return !st.LoopFile })
	}

	if err := srv.SendJSON(serverConn, map[string]any{"event": "property-change", "name": "loop-file", "data": "true"}); err != nil {
		t.Fatalf("send loop-file=true: %v", err)
	}
	waitStatus("loop-file=true", func(st Status) bool { return st.LoopFile })

	// Disconnect must reset Speed=1.0 and LoopFile=false.
	srv.CloseConnections()
	waitStatus("disconnect reset", func(st Status) bool {
		return !st.IsRunning && st.Speed == 1.0 && !st.LoopFile
	})
}

func TestIPCClient_SetSpeedAndToggleLoop(t *testing.T) {
	t.Run("connected commands, clamping, and broadcasts", func(t *testing.T) {
		var (
			mu      sync.Mutex
			cmds    [][]any
			failCmd string
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			cp := make([]any, len(cmd))
			copy(cp, cmd)
			mu.Lock()
			if len(cmd) > 0 && fmt.Sprint(cmd[0]) != "observe_property" {
				cmds = append(cmds, cp)
			}
			shouldFail := failCmd != "" && len(cmd) > 1 && fmt.Sprint(cmd[1]) == failCmd
			mu.Unlock()

			errStr := "success"
			if shouldFail {
				errStr = "property unavailable"
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      errStr,
				"request_id": reqID,
			})
		})

		client := NewIPCClient(WithSocketPath(srv.sockPath), WithCommandTimeout(2*time.Second))
		defer client.Close()

		subCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := client.Subscribe(subCtx)

		ctx := context.Background()
		if err := client.SetSpeed(ctx, 1.5); err != nil {
			t.Fatalf("SetSpeed(1.5): %v", err)
		}
		if err := client.SetSpeed(ctx, 0.1); err != nil {
			t.Fatalf("SetSpeed(0.1): %v", err)
		}
		if err := client.SetSpeed(ctx, 5.0); err != nil {
			t.Fatalf("SetSpeed(5.0): %v", err)
		}
		if err := client.SetSpeed(ctx, math.NaN()); err != nil {
			t.Fatalf("SetSpeed(NaN): %v", err)
		}

		// Verify broadcasts received.
		var speeds []float64
		deadline := time.After(2 * time.Second)
		for len(speeds) < 4 {
			select {
			case st := <-ch:
				speeds = append(speeds, st.Speed)
			case <-deadline:
				t.Fatalf("timed out waiting for SetSpeed broadcasts; got %v", speeds)
			}
		}
		if !slices.Equal(speeds, []float64{1.5, 0.5, 2.0, 1.0}) {
			t.Fatalf("broadcast speeds = %v, want [1.5 0.5 2 1]", speeds)
		}

		// ToggleLoop false -> true -> false.
		loopOn, err := client.ToggleLoop(ctx)
		if err != nil || !loopOn || !client.Status().LoopFile {
			t.Fatalf("first ToggleLoop = (%v, %v), status.LoopFile=%v", loopOn, err, client.Status().LoopFile)
		}
		loopOn, err = client.ToggleLoop(ctx)
		if err != nil || loopOn || client.Status().LoopFile {
			t.Fatalf("second ToggleLoop = (%v, %v), status.LoopFile=%v", loopOn, err, client.Status().LoopFile)
		}

		// Verify mpv errors do not mutate cached status.
		mu.Lock()
		failCmd = "speed"
		mu.Unlock()
		if err := client.SetSpeed(ctx, 1.75); err == nil {
			t.Fatal("expected error from SetSpeed when mpv fails")
		}
		if client.Status().Speed != 1.0 {
			t.Fatalf("Speed mutated on error: got %v, want 1.0", client.Status().Speed)
		}

		mu.Lock()
		failCmd = "loop-file"
		mu.Unlock()
		if loopOn, err := client.ToggleLoop(ctx); err == nil || loopOn {
			t.Fatalf("expected error from ToggleLoop when mpv fails, got (%v, %v)", loopOn, err)
		}

		mu.Lock()
		gotCmds := slices.Clone(cmds)
		mu.Unlock()

		want := [][]any{
			{"set_property", "speed", 1.5},
			{"set_property", "speed", 0.5},
			{"set_property", "speed", 2.0},
			{"set_property", "speed", 1.0},
			{"set_property", "loop-file", "inf"},
			{"set_property", "loop-file", "no"},
			{"set_property", "speed", 1.75},
			{"set_property", "loop-file", "inf"},
		}
		if !reflect.DeepEqual(gotCmds, want) {
			t.Fatalf("sent commands = %#v, want %#v", gotCmds, want)
		}
	})

	t.Run("disconnected errors", func(t *testing.T) {
		client := NewIPCClient(WithSocketPath(filepath.Join(t.TempDir(), "missing.sock")))
		defer client.Close()

		if err := client.SetSpeed(context.Background(), 1.25); err == nil {
			t.Fatal("expected error from SetSpeed when disconnected")
		}
		if _, err := client.ToggleLoop(context.Background()); err == nil {
			t.Fatal("expected error from ToggleLoop when disconnected")
		}
	})
}

func TestIPCClient_PlaylistMethods(t *testing.T) {
	t.Run("Playlist, PlayPlaylistIndex, RemovePlaylistIndex, ClearPlaylist", func(t *testing.T) {
		var (
			mu   sync.Mutex
			mode = "ok"
			cmds [][]any
		)
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			if len(cmd) > 0 && fmt.Sprint(cmd[0]) != "observe_property" {
				cp := make([]any, len(cmd))
				copy(cp, cmd)
				mu.Lock()
				cmds = append(cmds, cp)
				mu.Unlock()
			}

			mu.Lock()
			curMode := mode
			mu.Unlock()

			if len(cmd) >= 2 && fmt.Sprint(cmd[0]) == "get_property" && fmt.Sprint(cmd[1]) == "playlist" {
				switch curMode {
				case "ok":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"data": []any{
							map[string]any{"title": "Pink Floyd - Time", "filename": "https://yt/1", "current": true},
							map[string]any{"title": "", "filename": "https://yt/2", "current": false},
							map[string]any{"filename": "https://yt/3"},
						},
						"error":      "success",
						"request_id": reqID,
					})
				case "nil":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"data":       nil,
						"error":      "success",
						"request_id": reqID,
					})
				case "badtype":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"data":       "not-a-slice",
						"error":      "success",
						"request_id": reqID,
					})
				case "err":
					_ = json.NewEncoder(conn).Encode(map[string]any{
						"error":      "property unavailable",
						"request_id": reqID,
					})
				}
				return
			}

			if curMode == "err" {
				_ = json.NewEncoder(conn).Encode(map[string]any{
					"error":      "command failed",
					"request_id": reqID,
				})
				return
			}

			_ = json.NewEncoder(conn).Encode(map[string]any{
				"error":      "success",
				"request_id": reqID,
			})
		})

		client := NewIPCClient(WithSocketPath(srv.sockPath), WithCommandTimeout(2*time.Second))
		defer client.Close()

		ctx := context.Background()
		items, err := client.Playlist(ctx)
		if err != nil {
			t.Fatalf("Playlist() error: %v", err)
		}
		wantItems := []PlaylistItem{
			{Index: 0, Title: "Pink Floyd - Time", Filename: "https://yt/1", Current: true},
			{Index: 1, Title: "https://yt/2", Filename: "https://yt/2", Current: false},
			{Index: 2, Title: "https://yt/3", Filename: "https://yt/3", Current: false},
		}
		if !reflect.DeepEqual(items, wantItems) {
			t.Fatalf("Playlist() = %+v, want %+v", items, wantItems)
		}

		mu.Lock()
		mode = "nil"
		mu.Unlock()
		emptyItems, err := client.Playlist(ctx)
		if err != nil || len(emptyItems) != 0 {
			t.Fatalf("Playlist(nil) = (%+v, %v), want empty non-error", emptyItems, err)
		}

		mu.Lock()
		mode = "badtype"
		mu.Unlock()
		if _, err := client.Playlist(ctx); err == nil {
			t.Fatal("expected error for unexpected playlist payload type")
		}

		mu.Lock()
		mode = "ok"
		mu.Unlock()
		if err := client.PlayPlaylistIndex(ctx, 2); err != nil {
			t.Fatalf("PlayPlaylistIndex(2): %v", err)
		}
		if err := client.RemovePlaylistIndex(ctx, 1); err != nil {
			t.Fatalf("RemovePlaylistIndex(1): %v", err)
		}
		if err := client.ClearPlaylist(ctx); err != nil {
			t.Fatalf("ClearPlaylist(): %v", err)
		}

		if err := client.PlayPlaylistIndex(ctx, -1); err == nil {
			t.Fatal("expected error for negative PlayPlaylistIndex")
		}
		if err := client.RemovePlaylistIndex(ctx, -1); err == nil {
			t.Fatal("expected error for negative RemovePlaylistIndex")
		}

		mu.Lock()
		mode = "err"
		mu.Unlock()
		if _, err := client.Playlist(ctx); err == nil {
			t.Fatal("expected error from Playlist when mpv fails")
		}
		if err := client.PlayPlaylistIndex(ctx, 0); err == nil {
			t.Fatal("expected error from PlayPlaylistIndex when mpv fails")
		}

		mu.Lock()
		gotCmds := slices.Clone(cmds)
		mu.Unlock()

		// Confirm expected command sequence for PlayPlaylistIndex, RemovePlaylistIndex, ClearPlaylist.
		foundPos, foundClearTitle, foundRemove, foundClear := false, false, false, false
		for i, c := range gotCmds {
			if len(c) == 3 && fmt.Sprint(c[0]) == "set_property" && fmt.Sprint(c[1]) == "playlist-pos" && c[2] == float64(2) {
				foundPos = true
				if i+1 < len(gotCmds) {
					next := gotCmds[i+1]
					if len(next) == 3 && fmt.Sprint(next[0]) == "set_property" && fmt.Sprint(next[1]) == "force-media-title" && fmt.Sprint(next[2]) == "" {
						foundClearTitle = true
					}
				}
			}
			if len(c) == 2 && fmt.Sprint(c[0]) == "playlist-remove" && c[1] == float64(1) {
				foundRemove = true
			}
			if len(c) == 1 && fmt.Sprint(c[0]) == "playlist-clear" {
				foundClear = true
			}
		}
		if !foundPos || !foundClearTitle || !foundRemove || !foundClear {
			t.Fatalf("missing expected playlist IPC commands in %#v", gotCmds)
		}
	})

	t.Run("disconnected playlist methods fail", func(t *testing.T) {
		client := NewIPCClient(WithSocketPath(filepath.Join(t.TempDir(), "missing.sock")))
		defer client.Close()

		ctx := context.Background()
		if _, err := client.Playlist(ctx); err == nil {
			t.Fatal("expected Playlist error when disconnected")
		}
		if err := client.PlayPlaylistIndex(ctx, 0); err == nil {
			t.Fatal("expected PlayPlaylistIndex error when disconnected")
		}
		if err := client.RemovePlaylistIndex(ctx, 0); err == nil {
			t.Fatal("expected RemovePlaylistIndex error when disconnected")
		}
		if err := client.ClearPlaylist(ctx); err == nil {
			t.Fatal("expected ClearPlaylist error when disconnected")
		}
	})
}

func TestIPCClient_PlayFrom(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	argsFile := filepath.Join(tmpDir, "mpv-args.txt")

	writeStubBin(t, binDir, "mpv", fmt.Sprintf("printf '%%s\\n' \"$@\" > %q\nexit 0", argsFile))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	sockPath := filepath.Join(tmpDir, "run", "mpv.sock")
	client := NewIPCClient(
		WithSocketPath(sockPath),
		WithLaunchDefaults(LaunchOpts{
			SocketPath: sockPath,
			Mode:       "window",
			Quality:    720,
		}),
	)
	client.SetAutoplay(false)
	defer client.Close()

	vid := model.Video{ID: "resume12345", Title: "Resume Track"}
	if err := client.PlayFrom(context.Background(), vid, "window", 42.5); err != nil {
		t.Fatalf("PlayFrom error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var rawArgs []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(argsFile)
		if err == nil && len(b) > 0 {
			rawArgs = b
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if len(rawArgs) == 0 {
		t.Fatalf("timed out waiting for stub mpv args file")
	}

	argsText := string(rawArgs)
	if !strings.Contains(argsText, "--start=42.5") {
		t.Fatalf("expected --start=42.5 in mpv args, got:\n%s", argsText)
	}
	if !strings.Contains(argsText, "--force-media-title=Resume Track") {
		t.Fatalf("expected --force-media-title=Resume Track in mpv args, got:\n%s", argsText)
	}
	if st := client.Status(); st.Title != "Resume Track" || st.TimePos != 42.5 {
		t.Fatalf("unexpected status after PlayFrom: %+v", st)
	}
}

func TestGetStatus_SpeedAndLoopFile(t *testing.T) {
	t.Run("populates speed and loop-file when available", func(t *testing.T) {
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			prop := ""
			if len(cmd) >= 2 {
				prop = fmt.Sprint(cmd[1])
			}
			switch prop {
			case "pause":
				_ = json.NewEncoder(conn).Encode(map[string]any{"data": false, "error": "success"})
			case "media-title":
				_ = json.NewEncoder(conn).Encode(map[string]any{"data": "Shine On", "error": "success"})
			case "speed":
				_ = json.NewEncoder(conn).Encode(map[string]any{"data": 1.25, "error": "success"})
			case "loop-file":
				_ = json.NewEncoder(conn).Encode(map[string]any{"data": "inf", "error": "success"})
			default:
				_ = json.NewEncoder(conn).Encode(map[string]any{"error": "property unavailable"})
			}
		})

		t.Setenv("YT_SOCK", srv.sockPath)
		st, err := GetStatus()
		if err != nil {
			t.Fatalf("GetStatus() error: %v", err)
		}
		if !st.IsRunning || st.Title != "Shine On" || st.Speed != 1.25 || !st.LoopFile {
			t.Fatalf("GetStatus() = %+v, want IsRunning=true Title='Shine On' Speed=1.25 LoopFile=true", st)
		}
	})

	t.Run("non-fatal when speed and loop-file are unavailable", func(t *testing.T) {
		srv := startTestMPVServer(t, func(conn net.Conn, cmd []any, reqID int64) {
			prop := ""
			if len(cmd) >= 2 {
				prop = fmt.Sprint(cmd[1])
			}
			if prop == "pause" {
				_ = json.NewEncoder(conn).Encode(map[string]any{"data": true, "error": "success"})
				return
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{"error": "property unavailable"})
		})

		t.Setenv("YT_SOCK", srv.sockPath)
		st, err := GetStatus()
		if err != nil {
			t.Fatalf("GetStatus() error: %v", err)
		}
		if !st.IsRunning || !st.Paused || st.Speed != 0 || st.LoopFile {
			t.Fatalf("GetStatus() = %+v, want IsRunning=true Paused=true Speed=0 LoopFile=false", st)
		}
	})
}

func TestFakeEngine_SpeedLoopPlaylistAndPlayFrom(t *testing.T) {
	fake := NewFakeEngine()
	defer fake.Close()

	if fake.Status().Speed != 1.0 {
		t.Fatalf("initial Speed = %v, want 1.0", fake.Status().Speed)
	}

	ctx := context.Background()
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := fake.Subscribe(subCtx)

	if err := fake.SetSpeed(ctx, 0.2); err != nil {
		t.Fatalf("SetSpeed(0.2): %v", err)
	}
	if fake.Status().Speed != 0.5 {
		t.Fatalf("Speed after low clamp = %v, want 0.5", fake.Status().Speed)
	}
	if err := fake.SetSpeed(ctx, 3.0); err != nil {
		t.Fatalf("SetSpeed(3.0): %v", err)
	}
	if fake.Status().Speed != 2.0 {
		t.Fatalf("Speed after high clamp = %v, want 2.0", fake.Status().Speed)
	}
	if err := fake.SetSpeed(ctx, 1.25); err != nil {
		t.Fatalf("SetSpeed(1.25): %v", err)
	}
	if fake.Status().Speed != 1.25 {
		t.Fatalf("Speed = %v, want 1.25", fake.Status().Speed)
	}

	loopOn, err := fake.ToggleLoop(ctx)
	if err != nil || !loopOn || !fake.Status().LoopFile {
		t.Fatalf("first ToggleLoop = (%v, %v), status=%+v", loopOn, err, fake.Status())
	}
	loopOn, err = fake.ToggleLoop(ctx)
	if err != nil || loopOn || fake.Status().LoopFile {
		t.Fatalf("second ToggleLoop = (%v, %v), status=%+v", loopOn, err, fake.Status())
	}

	seedPlaylist := []PlaylistItem{
		{Index: 0, Title: "Track 0", Filename: "f0", Current: true},
		{Index: 1, Title: "Track 1", Filename: "f1", Current: false},
		{Index: 2, Title: "Track 2", Filename: "f2", Current: false},
	}
	fake.SetPlaylist(seedPlaylist)
	if fake.Status().PlaylistCount != 3 {
		t.Fatalf("PlaylistCount = %d, want 3", fake.Status().PlaylistCount)
	}

	gotPl, err := fake.Playlist(ctx)
	if err != nil || !reflect.DeepEqual(gotPl, seedPlaylist) {
		t.Fatalf("Playlist = (%+v, %v), want %+v", gotPl, err, seedPlaylist)
	}
	// Mutating returned slice must not alias internal slice.
	gotPl[0].Title = "Mutated"
	gotPl2, _ := fake.Playlist(ctx)
	if !reflect.DeepEqual(gotPl2, seedPlaylist) {
		t.Fatalf("FakeEngine Playlist returned aliased slice: %+v", gotPl2)
	}

	if err := fake.PlayPlaylistIndex(ctx, 2); err != nil {
		t.Fatalf("PlayPlaylistIndex(2): %v", err)
	}
	gotPl, _ = fake.Playlist(ctx)
	if !gotPl[2].Current || gotPl[0].Current || fake.Status().Title != "Track 2" {
		t.Fatalf("unexpected state after PlayPlaylistIndex(2): pl=%+v status=%+v", gotPl, fake.Status())
	}

	if err := fake.RemovePlaylistIndex(ctx, 0); err != nil {
		t.Fatalf("RemovePlaylistIndex(0): %v", err)
	}
	gotPl, _ = fake.Playlist(ctx)
	wantAfterRemove := []PlaylistItem{
		{Index: 0, Title: "Track 1", Filename: "f1", Current: false},
		{Index: 1, Title: "Track 2", Filename: "f2", Current: true},
	}
	if !reflect.DeepEqual(gotPl, wantAfterRemove) || fake.Status().PlaylistCount != 2 {
		t.Fatalf("after RemovePlaylistIndex(0): pl=%+v count=%d", gotPl, fake.Status().PlaylistCount)
	}

	if err := fake.ClearPlaylist(ctx); err != nil {
		t.Fatalf("ClearPlaylist(): %v", err)
	}
	gotPl, _ = fake.Playlist(ctx)
	wantAfterClear := []PlaylistItem{
		{Index: 0, Title: "Track 2", Filename: "f2", Current: true},
	}
	if !reflect.DeepEqual(gotPl, wantAfterClear) || fake.Status().PlaylistCount != 1 {
		t.Fatalf("after ClearPlaylist: pl=%+v count=%d", gotPl, fake.Status().PlaylistCount)
	}

	if err := fake.PlayFrom(ctx, model.Video{ID: "abc123", Title: "Resumed"}, "audio", 91.5); err != nil {
		t.Fatalf("PlayFrom: %v", err)
	}
	if st := fake.Status(); !st.IsRunning || st.Title != "Resumed" || st.TimePos != 91.5 || st.Paused {
		t.Fatalf("after PlayFrom: status=%+v", st)
	}

	// Drain at least one broadcast update.
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected status update on Subscribe channel")
	}

	// Verify error injection and negative/out-of-range index validation.
	if err := fake.PlayPlaylistIndex(ctx, -1); err == nil {
		t.Fatal("expected error for negative PlayPlaylistIndex")
	}
	if err := fake.PlayPlaylistIndex(ctx, 99); err == nil {
		t.Fatal("expected error for out-of-range PlayPlaylistIndex")
	}
	if err := fake.RemovePlaylistIndex(ctx, -1); err == nil {
		t.Fatal("expected error for negative RemovePlaylistIndex")
	}
	if err := fake.RemovePlaylistIndex(ctx, 99); err == nil {
		t.Fatal("expected error for out-of-range RemovePlaylistIndex")
	}

	wantErr := fmt.Errorf("boom")
	fake.SetError(wantErr)
	if err := fake.SetSpeed(ctx, 1.0); err == nil {
		t.Fatal("expected SetSpeed error")
	}
	if _, err := fake.ToggleLoop(ctx); err == nil {
		t.Fatal("expected ToggleLoop error")
	}
	if _, err := fake.Playlist(ctx); err == nil {
		t.Fatal("expected Playlist error")
	}
	if err := fake.PlayPlaylistIndex(ctx, 0); err == nil {
		t.Fatal("expected PlayPlaylistIndex error")
	}
	if err := fake.RemovePlaylistIndex(ctx, 0); err == nil {
		t.Fatal("expected RemovePlaylistIndex error")
	}
	if err := fake.ClearPlaylist(ctx); err == nil {
		t.Fatal("expected ClearPlaylist error")
	}
	if err := fake.PlayFrom(ctx, model.Video{ID: "x"}, "auto", 10); err == nil {
		t.Fatal("expected PlayFrom error")
	}
	fake.SetError(nil)

	// Concurrent stress test under -race.
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				fake.SetPlaylist([]PlaylistItem{
					{Index: 0, Title: "A", Current: true},
					{Index: 1, Title: "B", Current: false},
				})
				_ = fake.SetSpeed(ctx, 0.5+float64(i%4)*0.5)
				_, _ = fake.ToggleLoop(ctx)
				_, _ = fake.Playlist(ctx)
				_ = fake.PlayPlaylistIndex(ctx, id%2)
				_ = fake.RemovePlaylistIndex(ctx, 0)
				_ = fake.ClearPlaylist(ctx)
				_ = fake.PlayFrom(ctx, model.Video{ID: "v", Title: "T"}, "window", float64(i))
				_ = fake.Status()
				_ = fake.Calls()
			}
		}(g)
	}
	wg.Wait()
}
