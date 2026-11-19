package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

var (
	// ErrNotConnected is returned when a command is issued without an active mpv IPC connection.
	ErrNotConnected = errors.New("mpv socket not connected")
	// ErrClientClosed is returned when an operation is attempted on a closed IPCClient.
	ErrClientClosed = errors.New("mpv ipc client closed")
)

var observedProperties = []string{
	"time-pos",
	"duration",
	"pause",
	"volume",
	"media-title",
	"playlist-count",
	"speed",
	"loop-file",
}

// IPCOption configures an IPCClient.
type IPCOption func(*IPCClient)

// WithSocketPath sets an explicit UNIX socket path, bypassing FindSocket discovery.
func WithSocketPath(path string) IPCOption {
	return func(c *IPCClient) {
		c.socketFinder = func() string { return path }
	}
}

// WithSocketFinder sets a custom socket discovery function.
func WithSocketFinder(fn func() string) IPCOption {
	return func(c *IPCClient) {
		if fn != nil {
			c.socketFinder = fn
		}
	}
}

// WithBackoff configures the initial and maximum reconnect backoff durations.
func WithBackoff(initial, max time.Duration) IPCOption {
	return func(c *IPCClient) {
		if initial > 0 {
			c.initialBackoff = initial
		}
		if max > 0 {
			c.maxBackoff = max
		}
	}
}

// WithCommandTimeout configures the default timeout for IPC commands when ctx has no deadline.
func WithCommandTimeout(d time.Duration) IPCOption {
	return func(c *IPCClient) {
		if d > 0 {
			c.cmdTimeout = d
		}
	}
}

// WithLaunchDefaults configures native mpv launch options used when no IPC
// socket is currently connected.
func WithLaunchDefaults(opts LaunchOpts) IPCOption {
	return func(c *IPCClient) {
		if opts.Mode == "audio" {
			opts.AudioOnly = true
		}
		c.launchDefaults = opts
		if opts.FastPlayback {
			c.resolver = NewStreamResolver(opts.YtdlpBin, opts.Quality)
			c.resolver.SetCookieJar(opts.CookieJarPath)
			if opts.AudioOnly {
				c.resolver.SetAudioOnly(true)
			}
		}
	}
}

// WithSponsorBlockClient configures the SponsorBlockClient used to fetch skip segments.
func WithSponsorBlockClient(sb *SponsorBlockClient) IPCOption {
	return func(c *IPCClient) {
		if sb != nil {
			c.sbClient = sb
		}
	}
}

// Chapter represents a single playback chapter marker reported by mpv.
type Chapter struct {
	Title string  `json:"title"`
	Time  float64 `json:"time"`
}

type ipcResponse struct {
	Data      any    `json:"data"`
	RequestID *int64 `json:"request_id"`
	Error     string `json:"error"`
}

type ipcMessage struct {
	Event     string `json:"event"`
	Reason    string `json:"reason"`
	Name      string `json:"name"`
	Data      any    `json:"data"`
	RequestID *int64 `json:"request_id"`
	Error     string `json:"error"`
}

// IPCClient implements Engine over a persistent mpv JSON IPC UNIX domain socket.
type IPCClient struct {
	socketFinder   func() string
	initialBackoff time.Duration
	maxBackoff     time.Duration
	cmdTimeout     time.Duration
	launchMu       sync.RWMutex
	launchDefaults LaunchOpts
	lastVideo      model.Video
	lastMode       string
	resolver       *StreamResolver

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wakeCh chan struct{}
	wg     sync.WaitGroup

	connMu  sync.RWMutex
	writeMu sync.Mutex
	conn    net.Conn

	procMu        sync.Mutex
	mpvProc       *os.Process
	procAudioOnly bool

	playMu     sync.Mutex
	playSeq    atomic.Uint64
	playCancel context.CancelFunc
	step3Mu    sync.Mutex

	autoplay            atomic.Bool
	fallbackMu          sync.RWMutex
	autoplayFallbackIDs []string

	sponsorBlock   atomic.Bool
	sbClient       *SponsorBlockClient
	sbMu           sync.Mutex
	sbSeq          uint64
	sbVideoID      string
	sbSegments     []Segment
	sbLastSkipped  map[string]time.Time
	sbTimeObserved bool
	sbFetched      bool
	nowFn          func() time.Time

	nextReqID atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan ipcResponse

	statusMu sync.RWMutex
	status   Status

	subMu     sync.Mutex
	subs      map[uint64]chan Status
	nextSubID uint64
	closed    bool

	closeOnce sync.Once
}

// NewIPCClient creates and starts a persistent mpv IPC client.
func NewIPCClient(opts ...IPCOption) *IPCClient {
	ctx, cancel := context.WithCancel(context.Background())
	c := &IPCClient{
		socketFinder:   FindSocket,
		initialBackoff: 100 * time.Millisecond,
		maxBackoff:     5 * time.Second,
		cmdTimeout:     2 * time.Second,
		sbClient:       NewSponsorBlockClient(),
		nowFn:          time.Now,
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		wakeCh:         make(chan struct{}, 1),
		pending:        make(map[int64]chan ipcResponse),
		status:         Status{Speed: 1.0},
		subs:           make(map[uint64]chan Status),
	}
	c.autoplay.Store(true)
	c.sponsorBlock.Store(true)
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	// Attempt an immediate non-blocking connect so callers see an active socket right away.
	if conn, err := c.dialAndObserve(); err == nil {
		c.setConn(conn)
		c.setRunning(true)
	}

	c.wg.Add(1)
	go c.run()
	return c
}

func (c *IPCClient) run() {
	defer c.wg.Done()
	backoff := c.initialBackoff

	for {
		if c.ctx.Err() != nil {
			return
		}

		conn := c.getConn()
		if conn == nil {
			dialed, err := c.dialAndObserve()
			if err != nil {
				select {
				case <-c.ctx.Done():
					return
				case <-c.wakeCh:
					backoff = c.initialBackoff
				case <-time.After(backoff):
					backoff *= 2
					if backoff > c.maxBackoff {
						backoff = c.maxBackoff
					}
				}
				continue
			}
			c.connMu.Lock()
			if c.ctx.Err() != nil {
				c.connMu.Unlock()
				_ = dialed.Close()
				return
			}
			if c.conn != nil {
				conn = c.conn
				c.connMu.Unlock()
				_ = dialed.Close()
			} else {
				c.conn = dialed
				c.connMu.Unlock()
				conn = dialed
				c.setRunning(true)
			}
		}

		backoff = c.initialBackoff
		c.readLoop(conn)
		c.handleDisconnect(conn)

		if c.ctx.Err() != nil {
			return
		}

		select {
		case <-c.ctx.Done():
			return
		case <-c.wakeCh:
			backoff = c.initialBackoff
		case <-time.After(backoff):
			backoff *= 2
			if backoff > c.maxBackoff {
				backoff = c.maxBackoff
			}
		}
	}
}

func (c *IPCClient) dialAndObserve() (net.Conn, error) {
	sockPath := c.socketFinder()
	if sockPath == "" {
		return nil, ErrNotConnected
	}

	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		return nil, err
	}

	for i, prop := range observedProperties {
		reqID := c.nextReqID.Add(1)
		payload, err := json.Marshal(map[string]any{
			"command":    []any{"observe_property", i + 1, prop},
			"request_id": reqID,
		})
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if _, err := conn.Write(append(payload, '\n')); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func (c *IPCClient) readLoop(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			c.handleLine(trimmed)
		}
		if err != nil {
			return
		}
	}
}

func (c *IPCClient) handleLine(line []byte) {
	var msg ipcMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}

	if msg.RequestID != nil {
		c.pendingMu.Lock()
		ch, ok := c.pending[*msg.RequestID]
		if ok {
			delete(c.pending, *msg.RequestID)
		}
		c.pendingMu.Unlock()

		if ok {
			ch <- ipcResponse{
				Data:      msg.Data,
				RequestID: msg.RequestID,
				Error:     msg.Error,
			}
		}
		return
	}

	switch msg.Event {
	case "property-change":
		c.applyPropertyChange(msg.Name, msg.Data)
	case "end-file":
		c.statusMu.Lock()
		c.status.TimePos = 0
		st := c.status
		c.statusMu.Unlock()
		if msg.Reason == "eof" {
			c.clearSponsorBlockState(0)
			c.spawnClearForcedTitle()
		}
		c.broadcast(st)
	}
}

func (c *IPCClient) spawnClearForcedTitle() {
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}
	go func() {
		defer c.wg.Done()
		ctx, cancel := context.WithTimeout(c.ctx, 400*time.Millisecond)
		defer cancel()
		_, _ = c.sendCommand(ctx, "set_property", "force-media-title", "")
	}()
}

func (c *IPCClient) applyPropertyChange(name string, data any) {
	c.statusMu.Lock()
	changed := false
	timePosUpdated := false
	switch name {
	case "time-pos":
		if f, ok := toFloat64(data); ok {
			c.status.TimePos = f
			changed = true
			timePosUpdated = true
		} else if data == nil && c.status.TimePos != 0 {
			c.status.TimePos = 0
			changed = true
		}
	case "duration":
		if f, ok := toFloat64(data); ok {
			c.status.Duration = f
			changed = true
		} else if data == nil && c.status.Duration != 0 {
			c.status.Duration = 0
			changed = true
		}
	case "pause":
		if b, ok := data.(bool); ok {
			c.status.Paused = b
			changed = true
		}
	case "volume":
		if f, ok := toFloat64(data); ok {
			c.status.Volume = f
			changed = true
		}
	case "media-title":
		if s, ok := data.(string); ok {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" && !strings.HasPrefix(trimmed, "videoplayback") && trimmed != c.status.Title {
				c.status.Title = trimmed
				changed = true
			}
		} else if data == nil && c.status.Title != "" {
			c.status.Title = ""
			changed = true
		}
	case "playlist-count":
		if f, ok := toFloat64(data); ok {
			c.status.PlaylistCount = int(f)
			changed = true
		}
	case "speed":
		if f, ok := toFloat64(data); ok {
			if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				f = 1.0
			}
			c.status.Speed = f
			changed = true
		}
	case "loop-file":
		if b, ok := parseLoopFileValue(data); ok {
			c.status.LoopFile = b
			changed = true
		}
	}
	st := c.status
	c.statusMu.Unlock()

	if changed {
		c.broadcast(st)
	}
	if timePosUpdated {
		c.markSponsorTimeObserved()
		if !st.Paused && c.SponsorBlockEnabled() {
			c.checkAndSkipSponsorSegment(st.TimePos)
		}
	}
}

func (c *IPCClient) handleDisconnect(conn net.Conn) {
	c.connMu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.connMu.Unlock()

	_ = conn.Close()
	c.failPending(ErrNotConnected)
	c.clearSponsorBlockState(0)

	c.statusMu.Lock()
	c.status = Status{IsRunning: false, Speed: 1.0}
	st := c.status
	c.statusMu.Unlock()

	c.broadcast(st)
}

func (c *IPCClient) failPending(err error) {
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		select {
		case ch <- ipcResponse{Error: err.Error()}:
		default:
		}
	}
	c.pendingMu.Unlock()
}

func (c *IPCClient) getConn() net.Conn {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}

func (c *IPCClient) setConn(conn net.Conn) {
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
}

func (c *IPCClient) setRunning(running bool) {
	c.statusMu.Lock()
	c.status.IsRunning = running
	st := c.status
	c.statusMu.Unlock()
	c.broadcast(st)
}

func (c *IPCClient) isConnected() bool {
	return c.getConn() != nil
}

func (c *IPCClient) signalWake() {
	if c.wakeCh == nil {
		return
	}
	select {
	case c.wakeCh <- struct{}{}:
	default:
	}
}

func (c *IPCClient) tryConnectImmediate(maxWait time.Duration) bool {
	return c.tryConnectImmediateCtx(c.ctx, maxWait)
}

func (c *IPCClient) tryConnectImmediateCtx(ctx context.Context, maxWait time.Duration) bool {
	if c.ctx.Err() != nil {
		return false
	}
	if ctx == nil {
		ctx = c.ctx
	}
	if ctx.Err() != nil {
		return false
	}
	if c.isConnected() {
		return true
	}
	if maxWait <= 0 {
		return false
	}

	deadline := time.Now().Add(maxWait)
	for {
		if c.ctx.Err() != nil || ctx.Err() != nil {
			return false
		}
		if c.isConnected() {
			return true
		}
		if dialed, err := c.dialAndObserve(); err == nil {
			c.connMu.Lock()
			if c.ctx.Err() != nil || ctx.Err() != nil {
				c.connMu.Unlock()
				_ = dialed.Close()
				return false
			}
			if c.conn != nil {
				c.connMu.Unlock()
				_ = dialed.Close()
				return true
			}
			c.conn = dialed
			c.connMu.Unlock()
			c.setRunning(true)
			c.signalWake()
			return true
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		wait := 25 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return false
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func (c *IPCClient) sendCommand(ctx context.Context, args ...any) (ipcResponse, error) {
	if c.ctx.Err() != nil {
		return ipcResponse{}, ErrClientClosed
	}

	conn := c.getConn()
	if conn == nil {
		return ipcResponse{}, ErrNotConnected
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cmdTimeout)
		defer cancel()
	}

	reqID := c.nextReqID.Add(1)
	payload, err := json.Marshal(map[string]any{
		"command":    args,
		"request_id": reqID,
	})
	if err != nil {
		return ipcResponse{}, err
	}

	respCh := make(chan ipcResponse, 1)
	c.pendingMu.Lock()
	c.pending[reqID] = respCh
	c.pendingMu.Unlock()

	c.writeMu.Lock()
	_, writeErr := conn.Write(append(payload, '\n'))
	c.writeMu.Unlock()

	if writeErr != nil {
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return ipcResponse{}, writeErr
	}

	select {
	case resp := <-respCh:
		if resp.Error != "" && resp.Error != "success" {
			return resp, fmt.Errorf("mpv ipc error: %s", resp.Error)
		}
		return resp, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return ipcResponse{}, ctx.Err()
	case <-c.done:
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return ipcResponse{}, ErrClientClosed
	}
}

func formatSelector(quality int, audioOnly bool) string {
	if audioOnly {
		return "bestaudio/best"
	}
	if quality > 0 {
		return fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", quality, quality)
	}
	return "bestvideo+bestaudio/best"
}

func formatSelectorForVideo(quality int, audioOnly, isLive bool) string {
	if audioOnly {
		return "bestaudio/best"
	}
	if isLive {
		if quality <= 0 {
			return "best/bestvideo+bestaudio"
		}
		return fmt.Sprintf("best[height<=%d]/bestvideo[height<=%d]+bestaudio/best", quality, quality)
	}
	return formatSelector(quality, false)
}

// SetQuality updates the preferred max vertical resolution (0 = best available)
// for future launches, clears stale stream prefetch cache, and updates a live mpv.
func (c *IPCClient) SetQuality(ctx context.Context, quality int) {
	c.launchMu.Lock()
	c.launchDefaults.Quality = quality
	audioOnly := c.launchDefaults.AudioOnly
	if c.resolver != nil {
		c.resolver.SetQuality(quality)
	}
	c.launchMu.Unlock()
	if c.isConnected() {
		_, _ = c.sendCommand(ctx, "set_property", "ytdl-format", formatSelector(quality, audioOnly))
	}
}

// SetPlaybackProfile updates both resolution quality and audio-only mode, persists
// them into launch defaults + resolver, and seamlessly relaunches an active managed
// mpv process at the current timestamp so video windows open/close cleanly on Wayland.
func (c *IPCClient) SetPlaybackProfile(ctx context.Context, quality int, audioOnly bool) error {
	if c.ctx.Err() != nil {
		return ErrClientClosed
	}

	c.launchMu.Lock()
	c.launchDefaults.Quality = quality
	c.launchDefaults.AudioOnly = audioOnly
	if audioOnly {
		c.launchDefaults.Mode = "audio"
	} else if c.launchDefaults.Mode == "audio" || c.launchDefaults.Mode == "" {
		c.launchDefaults.Mode = "window"
	}
	lastVid := c.lastVideo
	if c.resolver != nil {
		c.resolver.SetQuality(quality)
		c.resolver.SetAudioOnly(audioOnly)
	}
	c.launchMu.Unlock()

	if !c.hasManagedProc() {
		if c.isConnected() {
			_, _ = c.sendCommand(ctx, "set_property", "ytdl-format", formatSelectorForVideo(quality, audioOnly, lastVid.IsLive))
			wantVid := "auto"
			if audioOnly {
				wantVid = "no"
			}
			_, err := c.sendCommand(ctx, "set_property", "vid", wantVid)
			return err
		}
		return nil
	}

	st := c.Status()
	if lastVid.ID == "" {
		if c.isConnected() {
			_, _ = c.sendCommand(ctx, "set_property", "ytdl-format", formatSelector(quality, audioOnly))
			wantVid := "auto"
			if audioOnly {
				wantVid = "no"
			}
			_, err := c.sendCommand(ctx, "set_property", "vid", wantVid)
			return err
		}
		return nil
	}
	if conn := c.getConn(); conn != nil {
		c.handleDisconnect(conn)
	}
	c.killManagedMPV()
	mode := "video"
	if audioOnly {
		mode = "audio"
	}
	return c.playWithResume(ctx, lastVid, mode, st.TimePos, st.Paused)
}

// TogglePause toggles playback pause state in mpv.
func (c *IPCClient) TogglePause(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "cycle", "pause")
	return err
}

// Pause pauses playback in mpv.
func (c *IPCClient) Pause(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "set_property", "pause", true)
	return err
}

// Next skips to the next track in the playlist.
func (c *IPCClient) Next(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "playlist-next")
	if err == nil {
		_, _ = c.sendCommand(ctx, "set_property", "force-media-title", "")
	}
	return err
}

// Prev skips to the previous track in the playlist.
func (c *IPCClient) Prev(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "playlist-prev")
	if err == nil {
		_, _ = c.sendCommand(ctx, "set_property", "force-media-title", "")
	}
	return err
}

// Seek seeks relative seconds in mpv and optimistically updates cached position.
func (c *IPCClient) Seek(ctx context.Context, seconds int) error {
	_, err := c.sendCommand(ctx, "seek", seconds)
	if err == nil {
		c.statusMu.Lock()
		nextPos := c.status.TimePos + float64(seconds)
		if nextPos < 0 {
			nextPos = 0
		}
		if c.status.Duration > 0 && nextPos > c.status.Duration {
			nextPos = c.status.Duration
		}
		c.status.TimePos = nextPos
		st := c.status
		c.statusMu.Unlock()
		c.broadcast(st)
	}
	return err
}

// SeekAbsolute seeks playback to an absolute timestamp in seconds in mpv.
func (c *IPCClient) SeekAbsolute(ctx context.Context, seconds float64) error {
	_, err := c.sendCommand(ctx, "seek", seconds, "absolute")
	return err
}

// ToggleVideoMode toggles audio-only vs video mode in launch defaults and on mpv.
// When managing a live mpv child process, it relaunches cleanly at the current
// timestamp with --no-video --force-window=no (or video window) so window managers
// never keep an orphan black window open.
func (c *IPCClient) ToggleVideoMode(ctx context.Context) (bool, error) {
	if c.ctx.Err() != nil {
		return false, ErrClientClosed
	}
	if c.hasManagedProc() {
		newAudioOnly := !c.isProcAudioOnly()
		c.launchMu.RLock()
		q := c.launchDefaults.Quality
		c.launchMu.RUnlock()
		err := c.SetPlaybackProfile(ctx, q, newAudioOnly)
		return !newAudioOnly, err
	}
	if !c.isConnected() {
		c.launchMu.Lock()
		c.launchDefaults.AudioOnly = !c.launchDefaults.AudioOnly
		if c.launchDefaults.AudioOnly {
			c.launchDefaults.Mode = "audio"
		} else if c.launchDefaults.Mode == "audio" {
			c.launchDefaults.Mode = "window"
		}
		audioOnly := c.launchDefaults.AudioOnly
		if c.resolver != nil {
			c.resolver.SetAudioOnly(audioOnly)
		}
		videoOn := !audioOnly
		c.launchMu.Unlock()
		return videoOn, nil
	}

	resp, err := c.sendCommand(ctx, "get_property", "vid")
	if err != nil {
		c.launchMu.RLock()
		videoOn := !c.launchDefaults.AudioOnly
		c.launchMu.RUnlock()
		return videoOn, err
	}

	var newAudioOnly bool
	if resp.Data == nil {
		c.launchMu.RLock()
		newAudioOnly = !c.launchDefaults.AudioOnly
		c.launchMu.RUnlock()
	} else {
		newAudioOnly = !isVideoDisabledValue(resp.Data)
	}

	wantVid := "auto"
	if newAudioOnly {
		wantVid = "no"
	}
	if _, err := c.sendCommand(ctx, "set_property", "vid", wantVid); err != nil {
		c.launchMu.RLock()
		videoOn := !c.launchDefaults.AudioOnly
		c.launchMu.RUnlock()
		return videoOn, err
	}

	c.launchMu.Lock()
	c.launchDefaults.AudioOnly = newAudioOnly
	if newAudioOnly {
		c.launchDefaults.Mode = "audio"
	} else if c.launchDefaults.Mode == "audio" {
		c.launchDefaults.Mode = "window"
	}
	if c.resolver != nil {
		c.resolver.SetAudioOnly(newAudioOnly)
	}
	c.launchMu.Unlock()
	return !newAudioOnly, nil
}

func isVideoDisabledValue(v any) bool {
	switch val := v.(type) {
	case string:
		return val == "no" || val == "false"
	case bool:
		return !val
	default:
		return false
	}
}

// Chapters queries mpv's "chapter-list" property and parses chapter titles and timestamps.
func (c *IPCClient) Chapters(ctx context.Context) ([]Chapter, error) {
	resp, err := c.sendCommand(ctx, "get_property", "chapter-list")
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return []Chapter{}, nil
	}
	rawList, ok := resp.Data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected chapter-list payload type %T", resp.Data)
	}
	chapters := make([]Chapter, 0, len(rawList))
	for _, item := range rawList {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var ch Chapter
		if title, ok := m["title"].(string); ok {
			ch.Title = title
		}
		if t, ok := toFloat64(m["time"]); ok {
			ch.Time = t
		}
		chapters = append(chapters, ch)
	}
	return chapters, nil
}

// AddVolume adjusts playback volume by delta.
func (c *IPCClient) AddVolume(ctx context.Context, delta float64) error {
	_, err := c.sendCommand(ctx, "add", "volume", delta)
	return err
}

// SetSpeed sets mpv playback speed clamped between 0.5 and 2.0.
func (c *IPCClient) SetSpeed(ctx context.Context, speed float64) error {
	clamped := clampSpeed(speed)
	if _, err := c.sendCommand(ctx, "set_property", "speed", clamped); err != nil {
		return err
	}
	c.statusMu.Lock()
	c.status.Speed = clamped
	st := c.status
	c.statusMu.Unlock()
	c.broadcast(st)
	return nil
}

// ToggleLoop toggles single-file looping between "inf" and "no" on mpv.
func (c *IPCClient) ToggleLoop(ctx context.Context) (bool, error) {
	cur := c.Status().LoopFile
	next := !cur
	val := "no"
	if next {
		val = "inf"
	}
	if _, err := c.sendCommand(ctx, "set_property", "loop-file", val); err != nil {
		return cur, err
	}
	c.statusMu.Lock()
	c.status.LoopFile = next
	st := c.status
	c.statusMu.Unlock()
	c.broadcast(st)
	return next, nil
}

// Playlist queries mpv's "playlist" property and returns the active playlist entries.
func (c *IPCClient) Playlist(ctx context.Context) ([]PlaylistItem, error) {
	resp, err := c.sendCommand(ctx, "get_property", "playlist")
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return []PlaylistItem{}, nil
	}
	rawList, ok := resp.Data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected playlist payload type %T", resp.Data)
	}
	items := make([]PlaylistItem, 0, len(rawList))
	for i, raw := range rawList {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item := PlaylistItem{Index: i}
		if fn, ok := m["filename"].(string); ok {
			item.Filename = fn
		}
		if title, ok := m["title"].(string); ok {
			item.Title = title
		}
		if strings.TrimSpace(item.Title) == "" {
			item.Title = item.Filename
		}
		if cur, ok := m["current"].(bool); ok {
			item.Current = cur
		}
		items = append(items, item)
	}
	return items, nil
}

// PlayPlaylistIndex switches playback to the playlist entry at index.
func (c *IPCClient) PlayPlaylistIndex(ctx context.Context, index int) error {
	if index < 0 {
		return fmt.Errorf("invalid playlist index: %d", index)
	}
	if _, err := c.sendCommand(ctx, "set_property", "playlist-pos", index); err != nil {
		return err
	}
	_, _ = c.sendCommand(ctx, "set_property", "force-media-title", "")
	return nil
}

// RemovePlaylistIndex removes the playlist entry at index.
func (c *IPCClient) RemovePlaylistIndex(ctx context.Context, index int) error {
	if index < 0 {
		return fmt.Errorf("invalid playlist index: %d", index)
	}
	_, err := c.sendCommand(ctx, "playlist-remove", index)
	return err
}

// ClearPlaylist removes all playlist entries except the currently playing item.
func (c *IPCClient) ClearPlaylist(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "playlist-clear")
	return err
}

// Status returns the latest cached playback Status.
func (c *IPCClient) Status() Status {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status
}

// Subscribe returns a channel that receives Status updates until ctx is canceled or Close is called.
func (c *IPCClient) Subscribe(ctx context.Context) <-chan Status {
	if ctx == nil {
		ctx = context.Background()
	}

	c.subMu.Lock()
	if c.closed || ctx.Err() != nil {
		c.subMu.Unlock()
		ch := make(chan Status)
		close(ch)
		return ch
	}

	ch := make(chan Status, 16)
	id := c.nextSubID
	c.nextSubID++
	c.subs[id] = ch
	c.subMu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		c.subMu.Lock()
		if subCh, ok := c.subs[id]; ok {
			delete(c.subs, id)
			close(subCh)
		}
		c.subMu.Unlock()
	}()

	return ch
}

func (c *IPCClient) broadcast(st Status) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if c.closed {
		return
	}
	for _, ch := range c.subs {
		select {
		case ch <- st:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- st:
			default:
			}
		}
	}
}

// Close shuts down the IPC client, stops any managed mpv process, closes the
// socket, and releases all subscribers.
func (c *IPCClient) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		close(c.done)
		c.killManagedMPV()

		c.connMu.Lock()
		conn := c.conn
		c.conn = nil
		c.connMu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}

		c.failPending(ErrClientClosed)

		c.subMu.Lock()
		c.closed = true
		for id, ch := range c.subs {
			delete(c.subs, id)
			close(ch)
		}
		c.subMu.Unlock()
	})

	c.wg.Wait()
	return nil
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

const (
	minPlaybackSpeed = 0.5
	maxPlaybackSpeed = 2.0
)

func clampSpeed(speed float64) float64 {
	if math.IsNaN(speed) {
		return 1.0
	}
	if speed < minPlaybackSpeed {
		return minPlaybackSpeed
	}
	if speed > maxPlaybackSpeed {
		return maxPlaybackSpeed
	}
	return speed
}

func parseLoopFileValue(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "inf", "yes", "true":
			return true, true
		case "no", "false", "":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}
