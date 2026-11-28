package platform

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildMPRISPropsPlaybackStates(t *testing.T) {
	t.Parallel()

	stopped := BuildMPRISProps(MPRISStatus{
		IsRunning: false,
		Paused:    false,
		Title:     "Ignored While Stopped",
		Artist:    "Ignored Artist",
		Position:  42.0,
		Duration:  120.0,
		TrackID:   "abc123",
	})
	if stopped.PlaybackStatus != "Stopped" {
		t.Fatalf("stopped PlaybackStatus = %q, want %q", stopped.PlaybackStatus, "Stopped")
	}
	if stopped.Position != 0 {
		t.Fatalf("stopped Position = %d, want 0", stopped.Position)
	}
	if len(stopped.Metadata) != 0 {
		t.Fatalf("stopped Metadata = %#v, want empty map", stopped.Metadata)
	}

	playing := BuildMPRISProps(MPRISStatus{
		IsRunning: true,
		Paused:    false,
		Title:     "Echoes",
		TrackID:   "pinkfloyd",
	})
	if playing.PlaybackStatus != "Playing" {
		t.Fatalf("playing PlaybackStatus = %q, want %q", playing.PlaybackStatus, "Playing")
	}

	paused := BuildMPRISProps(MPRISStatus{
		IsRunning: true,
		Paused:    true,
		Title:     "Echoes",
		TrackID:   "pinkfloyd",
	})
	if paused.PlaybackStatus != "Paused" {
		t.Fatalf("paused PlaybackStatus = %q, want %q", paused.PlaybackStatus, "Paused")
	}
}

func TestBuildMPRISPropsVolumeClamping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "negative", in: -25, want: 0.0},
		{name: "zero", in: 0, want: 0.0},
		{name: "half", in: 50, want: 0.5},
		{name: "fraction", in: 42.5, want: 0.425},
		{name: "full", in: 100, want: 1.0},
		{name: "over_100", in: 175, want: 1.0},
		{name: "nan", in: math.NaN(), want: 0.0},
		{name: "neg_inf", in: math.Inf(-1), want: 0.0},
		{name: "pos_inf", in: math.Inf(1), want: 1.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := BuildMPRISProps(MPRISStatus{Volume: tc.in})
			if math.Abs(got.Volume-tc.want) > 1e-9 {
				t.Fatalf("Volume(%v) = %v, want %v", tc.in, got.Volume, tc.want)
			}
		})
	}
}

func TestBuildMPRISPropsRateDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "zero", in: 0, want: 1.0},
		{name: "negative", in: -1.5, want: 1.0},
		{name: "nan", in: math.NaN(), want: 1.0},
		{name: "pos_inf", in: math.Inf(1), want: 1.0},
		{name: "half_speed", in: 0.5, want: 0.5},
		{name: "custom", in: 1.25, want: 1.25},
		{name: "double", in: 2.0, want: 2.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := BuildMPRISProps(MPRISStatus{Rate: tc.in})
			if got.Rate != tc.want {
				t.Fatalf("Rate(%v) = %v, want %v", tc.in, got.Rate, tc.want)
			}
		})
	}
}

func TestBuildMPRISPropsMicrosecondsAndMetadata(t *testing.T) {
	t.Parallel()

	props := BuildMPRISProps(MPRISStatus{
		IsRunning: true,
		Paused:    false,
		Title:     "Time",
		Artist:    "Pink Floyd",
		ArtURL:    "https://i.ytimg.com/vi/JwYX52BP2Sk/hqdefault.jpg",
		Duration:  413.456,
		Position:  45.789,
		Volume:    80,
		Rate:      1.25,
		TrackID:   "JwYX52BP2Sk",
	})

	if props.Position != 45_789_000 {
		t.Fatalf("Position = %d, want 45789000", props.Position)
	}
	if gotLen, ok := props.Metadata["mpris:length"].(int64); !ok || gotLen != 413_456_000 {
		t.Fatalf("mpris:length = %#v, want int64(413456000)", props.Metadata["mpris:length"])
	}
	if gotTitle, _ := props.Metadata["xesam:title"].(string); gotTitle != "Time" {
		t.Fatalf("xesam:title = %#v, want %q", props.Metadata["xesam:title"], "Time")
	}
	if gotArtist, ok := props.Metadata["xesam:artist"].([]string); !ok || !reflect.DeepEqual(gotArtist, []string{"Pink Floyd"}) {
		t.Fatalf("xesam:artist = %#v, want []string{\"Pink Floyd\"}", props.Metadata["xesam:artist"])
	}
	if gotArt, _ := props.Metadata["mpris:artUrl"].(string); gotArt != "https://i.ytimg.com/vi/JwYX52BP2Sk/hqdefault.jpg" {
		t.Fatalf("mpris:artUrl = %#v, want thumbnail URL", props.Metadata["mpris:artUrl"])
	}
	if gotTrack, _ := props.Metadata["mpris:trackid"].(string); gotTrack != "/org/mpris/MediaPlayer2/Track/JwYX52BP2Sk" {
		t.Fatalf("mpris:trackid = %#v, want valid track path", props.Metadata["mpris:trackid"])
	}

	minimal := BuildMPRISProps(MPRISStatus{
		IsRunning: true,
		Position:  -3.5,
		Duration:  0,
	})
	if minimal.Position != 0 {
		t.Fatalf("negative Position = %d, want 0", minimal.Position)
	}
	for _, omittedKey := range []string{"mpris:length", "xesam:title", "xesam:artist", "mpris:artUrl"} {
		if _, exists := minimal.Metadata[omittedKey]; exists {
			t.Fatalf("expected key %q to be omitted for minimal status, got %#v", omittedKey, minimal.Metadata[omittedKey])
		}
	}
}

func TestBuildMPRISPropsTrackIDSanitization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want string
	}{
		{raw: "", want: "/org/mpris/MediaPlayer2/Track/current"},
		{raw: "   \t ", want: "/org/mpris/MediaPlayer2/Track/current"},
		{raw: "dQw4w9WgXcQ", want: "/org/mpris/MediaPlayer2/Track/dQw4w9WgXcQ"},
		{raw: "-a_b/c.d e:123?x=1", want: "/org/mpris/MediaPlayer2/Track/_a_b_c_d_e_123_x_1"},
		{raw: "///", want: "/org/mpris/MediaPlayer2/Track/___"},
	}

	for _, tc := range cases {
		props := BuildMPRISProps(MPRISStatus{IsRunning: true, TrackID: tc.raw})
		got, ok := props.Metadata["mpris:trackid"].(string)
		if !ok {
			t.Fatalf("TrackID(%q) missing mpris:trackid string: %#v", tc.raw, props.Metadata)
		}
		if got != tc.want {
			t.Fatalf("TrackID(%q) = %q, want %q", tc.raw, got, tc.want)
		}
		if !isValidDBusObjectPath(got) {
			t.Fatalf("TrackID(%q) produced invalid D-Bus object path %q", tc.raw, got)
		}
	}
}

func isValidDBusObjectPath(path string) bool {
	if !strings.HasPrefix(path, "/") || (len(path) > 1 && strings.HasSuffix(path, "/")) {
		return false
	}
	if path == "/" {
		return true
	}
	for _, seg := range strings.Split(path[1:], "/") {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			isIdentChar := (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'
			if !isIdentChar {
				return false
			}
		}
	}
	return true
}

func TestMPRISBridgeDisabledWithoutDBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")

	bridge := NewMPRISBridge(MPRISCallbacks{
		OnPlayPause: func() {},
		OnPlay:      func() {},
		OnPause:     func() {},
		OnNext:      func() {},
		OnPrevious:  func() {},
		OnSeek:      func(time.Duration) {},
		OnStop:      func() {},
	})
	if bridge == nil {
		t.Fatal("NewMPRISBridge returned nil; expected safe inactive bridge")
	}
	if bridge.Active() {
		t.Fatal("Active() = true when DBUS_SESSION_BUS_ADDRESS is unset; want false")
	}

	bridge.UpdateStatus(MPRISStatus{
		IsRunning: true,
		Title:     "Test Track",
		Artist:    "Test Artist",
		Duration:  120,
		Position:  10,
		Volume:    75,
		Rate:      1.0,
		TrackID:   "test-id",
	})
	bridge.UpdateStatus(MPRISStatus{IsRunning: true, Paused: true, TrackID: "test-id"})
	bridge.UpdateStatus(MPRISStatus{IsRunning: false})

	if err := bridge.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
}

func TestMPRISBridgeUnreachableBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "missing.sock"))

	bridge := NewMPRISBridge(MPRISCallbacks{})
	if bridge == nil {
		t.Fatal("NewMPRISBridge returned nil; expected safe inactive bridge")
	}
	if bridge.Active() {
		t.Fatal("Active() = true for missing D-Bus socket; want false")
	}
	bridge.UpdateStatus(MPRISStatus{IsRunning: true, Title: "Offline"})
	if err := bridge.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestMPRISBridgeNilAndConcurrentSafety(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")

	var nilBridge *MPRISBridge
	if nilBridge.Active() {
		t.Fatal("nilBridge.Active() = true; want false")
	}
	nilBridge.UpdateStatus(MPRISStatus{IsRunning: true, Title: "NoPanic"})
	if err := nilBridge.Close(); err != nil {
		t.Fatalf("nilBridge.Close() error = %v, want nil", err)
	}

	bridge := NewMPRISBridge(MPRISCallbacks{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 20 {
				_ = bridge.Active()
				bridge.UpdateStatus(MPRISStatus{
					IsRunning: j%2 == 0,
					Position:  float64(id + j),
					Volume:    float64(j * 5),
					TrackID:   "concurrent-track",
				})
			}
		}(i)
	}
	wg.Wait()
	if err := bridge.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestMPRISBridgeLiveSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("live D-Bus MPRIS2 session test only runs on Linux")
	}
	dbusDaemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	dbusSend, err := exec.LookPath("dbus-send")
	if err != nil {
		t.Skip("dbus-send not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sockPath := filepath.Join(t.TempDir(), "bus.sock")
	daemonCmd := exec.CommandContext(ctx,
		dbusDaemon,
		"--session",
		"--nofork",
		"--nopidfile",
		"--address=unix:path="+sockPath,
		"--print-address",
	)
	stdout, err := daemonCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Kill()
		}
		_ = daemonCmd.Wait()
	})

	addrCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			addrCh <- strings.TrimSpace(scanner.Text())
		}
	}()

	var busAddr string
	select {
	case busAddr = <-addrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dbus-daemon address")
	}
	if busAddr == "" {
		t.Fatal("dbus-daemon returned empty address")
	}

	t.Setenv("DBUS_SESSION_BUS_ADDRESS", busAddr)

	var (
		mu     sync.Mutex
		events []string
		seeks  []time.Duration
	)
	record := func(name string) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	}

	bridge := NewMPRISBridge(MPRISCallbacks{
		OnPlayPause: func() { record("PlayPause") },
		OnPlay:      func() { record("Play") },
		OnPause:     func() { record("Pause") },
		OnNext:      func() { record("Next") },
		OnPrevious:  func() { record("Previous") },
		OnStop:      func() { record("Stop") },
		OnSeek: func(delta time.Duration) {
			mu.Lock()
			seeks = append(seeks, delta)
			mu.Unlock()
		},
	})
	defer func() { _ = bridge.Close() }()

	if !bridge.Active() {
		t.Fatal("bridge.Active() = false on live dbus-daemon; want true")
	}

	// Second instance requesting the same well-known name must degrade cleanly.
	duplicate := NewMPRISBridge(MPRISCallbacks{})
	if duplicate.Active() {
		_ = duplicate.Close()
		t.Fatal("duplicate bridge Active() = true; expected false when bus name is already claimed")
	}
	if err := duplicate.Close(); err != nil {
		t.Fatalf("duplicate.Close() = %v, want nil", err)
	}

	bridge.UpdateStatus(MPRISStatus{
		IsRunning: true,
		Paused:    false,
		Title:     "Echoes",
		Artist:    "Pink Floyd",
		ArtURL:    "https://example.com/cover.jpg",
		Duration:  180.0,
		Position:  12.5,
		Volume:    75.0,
		Rate:      1.5,
		TrackID:   "vid-123",
	})

	callBus := func(args ...string) string {
		t.Helper()
		fullArgs := append([]string{"--session", "--print-reply", "--dest=org.mpris.MediaPlayer2.iutui", "/org/mpris/MediaPlayer2"}, args...)
		cmd := exec.CommandContext(ctx, dbusSend, fullArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("dbus-send %v failed: %v\n%s", args, err, string(out))
		}
		return string(out)
	}

	introXML := callBus("org.freedesktop.DBus.Introspectable.Introspect")
	for _, want := range []string{
		`org.mpris.MediaPlayer2`,
		`org.mpris.MediaPlayer2.Player`,
		`method name="Seek"`,
		`method name="SetPosition"`,
		`method name="OpenUri"`,
		`signal name="Seeked"`,
	} {
		if !strings.Contains(introXML, want) {
			t.Fatalf("introspection missing %q:\n%s", want, introXML)
		}
	}

	rootProps := callBus("org.freedesktop.DBus.Properties.GetAll", "string:org.mpris.MediaPlayer2")
	for _, want := range []string{`"Identity"`, `"iutui"`, `"DesktopEntry"`, `"CanQuit"`, `boolean false`, `"HasTrackList"`} {
		if !strings.Contains(rootProps, want) {
			t.Fatalf("root properties missing %q:\n%s", want, rootProps)
		}
	}

	playerProps := callBus("org.freedesktop.DBus.Properties.GetAll", "string:org.mpris.MediaPlayer2.Player")
	for _, want := range []string{
		`"PlaybackStatus"`, `"Playing"`,
		`"Rate"`, `double 1.5`,
		`"Volume"`, `double 0.75`,
		`"Position"`, `int64 12500000`,
		`"mpris:trackid"`, `object path "/org/mpris/MediaPlayer2/Track/vid_123"`,
		`"mpris:length"`, `int64 180000000`,
		`"xesam:title"`, `"Echoes"`,
		`"xesam:artist"`, `"Pink Floyd"`,
		`"mpris:artUrl"`, `"https://example.com/cover.jpg"`,
	} {
		if !strings.Contains(playerProps, want) {
			t.Fatalf("player properties missing %q:\n%s", want, playerProps)
		}
	}

	callBus("org.mpris.MediaPlayer2.Raise")
	callBus("org.mpris.MediaPlayer2.Quit")
	callBus("org.mpris.MediaPlayer2.Player.OpenUri", "string:https://example.com/watch")
	callBus("org.mpris.MediaPlayer2.Player.PlayPause")
	callBus("org.mpris.MediaPlayer2.Player.Play")
	callBus("org.mpris.MediaPlayer2.Player.Pause")
	callBus("org.mpris.MediaPlayer2.Player.Next")
	callBus("org.mpris.MediaPlayer2.Player.Previous")
	callBus("org.mpris.MediaPlayer2.Player.Stop")
	callBus("org.mpris.MediaPlayer2.Player.Seek", "int64:5000000")
	callBus("org.mpris.MediaPlayer2.Player.SetPosition", "objpath:/org/mpris/MediaPlayer2/Track/vid_123", "int64:30000000")
	callBus("org.mpris.MediaPlayer2.Player.SetPosition", "objpath:/org/mpris/MediaPlayer2/Track/wrong_id", "int64:1000000")

	mu.Lock()
	gotEvents := append([]string(nil), events...)
	gotSeeks := append([]time.Duration(nil), seeks...)
	mu.Unlock()

	wantEvents := []string{"PlayPause", "Play", "Pause", "Next", "Previous", "Stop"}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("callback events = %v, want %v", gotEvents, wantEvents)
	}
	wantSeeks := []time.Duration{5 * time.Second, 17500 * time.Millisecond}
	if !reflect.DeepEqual(gotSeeks, wantSeeks) {
		t.Fatalf("seek callbacks = %v, want %v", gotSeeks, wantSeeks)
	}

	// Verify state update transitions cleanly to Paused and Stopped.
	bridge.UpdateStatus(MPRISStatus{IsRunning: true, Paused: true, TrackID: "vid-123"})
	pausedProps := callBus("org.freedesktop.DBus.Properties.Get", "string:org.mpris.MediaPlayer2.Player", "string:PlaybackStatus")
	if !strings.Contains(pausedProps, `"Paused"`) {
		t.Fatalf("expected PlaybackStatus Paused, got:\n%s", pausedProps)
	}

	if err := bridge.Close(); err != nil {
		t.Fatalf("bridge.Close() = %v, want nil", err)
	}
	if bridge.Active() {
		t.Fatal("bridge.Active() = true after Close(); want false")
	}
	// Updating after Close must remain panic-free.
	bridge.UpdateStatus(MPRISStatus{IsRunning: false})

	// Also verify panic-free behavior if bus daemon dies while a second live bridge is active.
	bridgeDrop := NewMPRISBridge(MPRISCallbacks{})
	if !bridgeDrop.Active() {
		t.Fatal("bridgeDrop.Active() = false before daemon kill")
	}
	_ = daemonCmd.Process.Kill()
	_ = daemonCmd.Wait()
	// Give the connection reader goroutine a brief chance to notice EOF, or exercise immediately.
	for i := range 5 {
		bridgeDrop.UpdateStatus(MPRISStatus{IsRunning: true, Title: fmt.Sprintf("drop-%d", i), TrackID: "vid"})
		time.Sleep(5 * time.Millisecond)
	}
	_ = bridgeDrop.Close()
}
