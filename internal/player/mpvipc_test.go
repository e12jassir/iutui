package player

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
