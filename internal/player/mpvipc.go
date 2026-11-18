package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	c.status = Status{IsRunning: false}
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

// Prefetch asynchronously resolves direct stream URLs for v in the background.
func (c *IPCClient) Prefetch(v model.Video) {
	if c.resolver != nil {
		c.resolver.Prefetch(c.ctx, v)
	}
}

// SetAutoplay enables or disables automatic continuous queueing for single videos.
func (c *IPCClient) SetAutoplay(enabled bool) {
	if c == nil {
		return
	}
	c.autoplay.Store(enabled)
}

// SetAutoplayFallback configures fallback video IDs used by autoplay when
// YouTube does not return an RD Mix playlist for the active video.
func (c *IPCClient) SetAutoplayFallback(ids []string) {
	if c == nil {
		return
	}
	copied := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			copied = append(copied, trimmed)
		}
	}
	c.fallbackMu.Lock()
	c.autoplayFallbackIDs = copied
	c.fallbackMu.Unlock()
}

const sponsorBlockSkipCooldown = 5 * time.Second

func (c *IPCClient) now() time.Time {
	if c != nil && c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

// SetSponsorBlock enables or disables automatic SponsorBlock segment skipping.
func (c *IPCClient) SetSponsorBlock(enabled bool) {
	if c == nil {
		return
	}
	c.sponsorBlock.Store(enabled)
	if !enabled {
		return
	}

	c.sbMu.Lock()
	videoID := c.sbVideoID
	seq := c.sbSeq
	needsFetch := videoID != "" && !c.sbFetched && c.sbClient != nil && c.isCurrentPlay(seq)
	c.sbMu.Unlock()
	if needsFetch {
		c.spawnSponsorBlockFetch(c.ctx, seq, videoID)
	}
}

// SponsorBlockEnabled reports whether automatic SponsorBlock segment skipping is enabled.
func (c *IPCClient) SponsorBlockEnabled() bool {
	if c == nil {
		return false
	}
	return c.sponsorBlock.Load()
}

func isSingleYouTubeVideoID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) != 11 || model.IsPlaylistID(id) {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !isAlnum && ch != '_' && ch != '-' {
			return false
		}
	}
	return true
}

func (c *IPCClient) clearSponsorBlockState(seq uint64) {
	c.sbMu.Lock()
	c.sbSeq = seq
	c.sbVideoID = ""
	c.sbSegments = nil
	c.sbLastSkipped = nil
	c.sbTimeObserved = false
	c.sbFetched = false
	c.sbMu.Unlock()
}

func (c *IPCClient) initSponsorBlockForPlay(ctx context.Context, seq uint64, rawVideoID string) {
	vid := strings.TrimSpace(rawVideoID)
	if !isSingleYouTubeVideoID(vid) {
		c.clearSponsorBlockState(seq)
		return
	}

	c.sbMu.Lock()
	if !c.isCurrentPlay(seq) {
		c.sbMu.Unlock()
		return
	}
	c.sbSeq = seq
	c.sbVideoID = vid
	c.sbSegments = nil
	c.sbLastSkipped = make(map[string]time.Time)
	c.sbTimeObserved = false
	c.sbFetched = false
	c.sbMu.Unlock()

	if c.SponsorBlockEnabled() && c.sbClient != nil {
		c.spawnSponsorBlockFetch(ctx, seq, vid)
	}
}

func (c *IPCClient) spawnSponsorBlockFetch(ctx context.Context, seq uint64, videoID string) {
	if ctx == nil {
		ctx = c.ctx
	}
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && ctx.Err() == nil && c.isCurrentPlay(seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if canSpawn {
		go c.fetchSponsorBlockSegments(ctx, seq, videoID)
	}
}

func (c *IPCClient) fetchSponsorBlockSegments(ctx context.Context, seq uint64, videoID string) {
	defer c.wg.Done()

	if !c.isCurrentPlay(seq) || ctx.Err() != nil || c.sbClient == nil {
		return
	}

	segs, err := c.sbClient.FetchSegments(ctx, videoID)
	if err != nil || !c.isCurrentPlay(seq) || ctx.Err() != nil {
		return
	}

	c.sbMu.Lock()
	observed := false
	if c.isCurrentPlay(seq) && c.sbSeq == seq && c.sbVideoID == videoID {
		c.sbSegments = segs
		c.sbFetched = true
		observed = c.sbTimeObserved
	}
	c.sbMu.Unlock()

	if observed && c.SponsorBlockEnabled() {
		st := c.Status()
		if st.IsRunning && !st.Paused {
			c.checkAndSkipSponsorSegment(st.TimePos)
		}
	}
}

func (c *IPCClient) markSponsorTimeObserved() {
	c.sbMu.Lock()
	if c.sbVideoID != "" {
		c.sbTimeObserved = true
	}
	c.sbMu.Unlock()
}

func (c *IPCClient) checkAndSkipSponsorSegment(timePos float64) {
	if !c.SponsorBlockEnabled() {
		return
	}

	now := c.now()
	var (
		shouldSkip bool
		targetEnd  float64
		seq        uint64
	)

	c.sbMu.Lock()
	if c.sbVideoID != "" && len(c.sbSegments) > 0 && c.isCurrentPlay(c.sbSeq) {
		for _, seg := range c.sbSegments {
			if timePos >= seg.Start && timePos < seg.End-0.5 {
				key := seg.UUID
				if key == "" {
					key = fmt.Sprintf("%.3f-%.3f", seg.Start, seg.End)
				}
				if last, ok := c.sbLastSkipped[key]; ok && now.Sub(last) < sponsorBlockSkipCooldown {
					continue
				}
				if c.sbLastSkipped == nil {
					c.sbLastSkipped = make(map[string]time.Time)
				}
				c.sbLastSkipped[key] = now
				targetEnd = seg.End
				seq = c.sbSeq
				shouldSkip = true
				break
			}
		}
	}
	c.sbMu.Unlock()

	if !shouldSkip {
		return
	}

	c.spawnSponsorSeek(seq, targetEnd)
}

func (c *IPCClient) spawnSponsorSeek(seq uint64, seconds float64) {
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && c.isCurrentPlay(seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if !canSpawn {
		return
	}

	go func() {
		defer c.wg.Done()
		if !c.SponsorBlockEnabled() || !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
			return
		}
		seekCtx, cancel := context.WithTimeout(c.ctx, c.cmdTimeout)
		defer cancel()
		_ = c.SeekAbsolute(seekCtx, seconds)
	}()
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

func (c *IPCClient) hasManagedProc() bool {
	c.procMu.Lock()
	defer c.procMu.Unlock()
	if c.mpvProc == nil {
		return false
	}
	if err := c.mpvProc.Signal(syscall.Signal(0)); err != nil {
		c.mpvProc = nil
		c.procAudioOnly = false
		return false
	}
	return true
}

func (c *IPCClient) isProcAudioOnly() bool {
	c.procMu.Lock()
	defer c.procMu.Unlock()
	return c.procAudioOnly
}

func (c *IPCClient) killManagedMPV() {
	c.procMu.Lock()
	proc := c.mpvProc
	c.mpvProc = nil
	c.procAudioOnly = false
	c.procMu.Unlock()
	if proc == nil {
		return
	}
	if proc.Pid > 0 {
		_ = syscall.Kill(-proc.Pid, syscall.SIGTERM)
	}
	_ = proc.Kill()
}

func (c *IPCClient) isCurrentPlay(seq uint64) bool {
	return c.playSeq.Load() == seq
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

const streamWaitTimeout = 2200 * time.Millisecond

type resolvedPlayTarget struct {
	targetURL           string
	url                 string
	fallbackURL         string
	audioFile           string
	seedID              string
	effectiveMode       string
	playlistIDs         []string
	playlistFetched     bool
	isMix               bool
	isAutoplay          bool
	launchAllowPlaylist bool
}

// Play starts playback of v using the active socket or launches mpv directly.
// Any active or in-flight playback is immediately interrupted so the latest Play wins.
func (c *IPCClient) Play(ctx context.Context, v model.Video, mode string) error {
	return c.playWithResume(ctx, v, mode, 0, false)
}

func (c *IPCClient) playWithResume(ctx context.Context, v model.Video, mode string, startPos float64, startPaused bool) error {
	if c.ctx.Err() != nil {
		return ErrClientClosed
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	c.launchMu.Lock()
	c.lastVideo = v
	c.lastMode = mode
	c.launchMu.Unlock()

	if v.Title != "" {
		c.statusMu.Lock()
		c.status.Title = v.Title
		c.status.TimePos = startPos
		st := c.status
		c.statusMu.Unlock()
		if st.IsRunning {
			c.broadcast(st)
		}
	}

	c.playMu.Lock()
	seq := c.playSeq.Add(1)
	prevCancel := c.playCancel
	mixCtx, mixCancel := context.WithCancel(c.ctx)
	c.playCancel = mixCancel
	c.playMu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}

	c.clearSponsorBlockState(seq)

	waitCtx, waitCancel := context.WithCancel(mixCtx)
	defer waitCancel()
	if ctx != nil && ctx != context.Background() && ctx != context.TODO() {
		stopWatch := context.AfterFunc(ctx, waitCancel)
		defer stopWatch()
	}

	skipReconnect, ok := c.cutActivePlayback(waitCtx, seq)
	if !ok {
		return nil
	}

	plan := c.resolvePlayTarget(waitCtx, v, mode)
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return nil
	}

	if err := c.playOnSingleMPV(ctx, waitCtx, seq, v, plan, skipReconnect, startPos, startPaused); err != nil {
		return err
	}

	c.initSponsorBlockForPlay(mixCtx, seq, v.ID)
	c.scheduleMixAppend(mixCtx, seq, plan)
	return nil
}

func (c *IPCClient) cutActivePlayback(waitCtx context.Context, seq uint64) (skipReconnect bool, ok bool) {
	if c.isConnected() || (c.hasManagedProc() && c.tryConnectImmediateCtx(waitCtx, 200*time.Millisecond)) {
		if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
			return false, false
		}
		conn := c.getConn()
		stopCtx, stopCancel := context.WithTimeout(waitCtx, 500*time.Millisecond)
		_, stopErr := c.sendCommand(stopCtx, "stop")
		stopCancel()
		if stopErr != nil {
			if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
				return false, false
			}
			if conn != nil {
				c.handleDisconnect(conn)
			}
			c.killManagedMPV()
			return true, true
		}
		return false, true
	}
	if c.hasManagedProc() {
		if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
			return false, false
		}
		c.killManagedMPV()
		return true, true
	}
	return false, true
}

func (c *IPCClient) resolvePlayTarget(waitCtx context.Context, v model.Video, mode string) resolvedPlayTarget {
	c.launchMu.RLock()
	audioOnly := c.launchDefaults.AudioOnly
	c.launchMu.RUnlock()

	effectiveMode := mode
	if effectiveMode == "mix" {
		effectiveMode = "auto"
	}
	if audioOnly && (effectiveMode == "" || effectiveMode == "auto") {
		effectiveMode = "audio"
	}
	targetURL, allowPlaylist, seedID := model.BuildTargetURL(v.ID, mode == "mix")

	plan := resolvedPlayTarget{
		targetURL:     targetURL,
		url:           targetURL,
		fallbackURL:   targetURL,
		seedID:        seedID,
		effectiveMode: effectiveMode,
		isMix:         mode == "mix" || model.IsPlaylistID(v.ID),
	}

	wantAudio := effectiveMode == "audio"
	applyResolved := func(st ResolvedStream) {
		if wantAudio {
			if st.AudioURL != "" {
				plan.url = st.AudioURL
			}
			plan.audioFile = ""
			return
		}
		if st.VideoURL != "" {
			plan.url = st.VideoURL
			plan.audioFile = st.AudioURL
		}
	}

	if !allowPlaylist {
		if mode != "mix" && c.autoplay.Load() && !v.IsLive && v.ID != "" {
			if seed := strings.TrimSpace(v.ID); seed != "" {
				plan.isAutoplay = true
				plan.seedID = seed
				plan.targetURL = "https://www.youtube.com/watch?v=" + seed + "&list=RD" + seed
			}
		}
		if c.resolver != nil {
			if st, ok := c.resolver.WaitOrGetForMode(waitCtx, v.ID, wantAudio, streamWaitTimeout); ok {
				applyResolved(st)
			}
		}
		return plan
	}

	if seedID != "" {
		plan.url = "https://www.youtube.com/watch?v=" + seedID
		plan.fallbackURL = plan.url
		if c.resolver != nil {
			if st, ok := c.resolver.WaitOrGetForMode(waitCtx, seedID, wantAudio, streamWaitTimeout); ok {
				applyResolved(st)
			}
		}
		return plan
	}

	ids := c.fetchPlaylistIDs(waitCtx, targetURL, 25)
	plan.playlistFetched = true
	if len(ids) > 0 {
		plan.seedID = ids[0]
		plan.url = "https://www.youtube.com/watch?v=" + plan.seedID
		plan.fallbackURL = plan.url
		plan.playlistIDs = ids[1:]
		if c.resolver != nil {
			if st, ok := c.resolver.WaitOrGetForMode(waitCtx, plan.seedID, wantAudio, streamWaitTimeout); ok {
				applyResolved(st)
			}
		}
		return plan
	}

	plan.launchAllowPlaylist = true
	return plan
}

func (c *IPCClient) playOnSingleMPV(ctx, waitCtx context.Context, seq uint64, v model.Video, plan resolvedPlayTarget, skipReconnect bool, startPos float64, startPaused bool) error {
	if !skipReconnect && !c.isConnected() {
		_ = c.tryConnectImmediateCtx(waitCtx, 150*time.Millisecond)
	}

	c.step3Mu.Lock()
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		c.step3Mu.Unlock()
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		c.step3Mu.Unlock()
		return ctx.Err()
	}

	if startPos <= 0.5 && !startPaused && c.tryPlayViaIPC(waitCtx, seq, v, plan) {
		c.step3Mu.Unlock()
		return nil
	}
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		c.step3Mu.Unlock()
		return nil
	}

	c.launchMu.RLock()
	opts := c.launchDefaults
	c.launchMu.RUnlock()

	if opts.SocketPath == "" {
		c.step3Mu.Unlock()
		return PlayVideo(v, plan.effectiveMode)
	}

	c.killManagedMPV()
	c.procMu.Lock()
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		c.procMu.Unlock()
		c.step3Mu.Unlock()
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(opts.SocketPath), 0o700)
	_ = os.Remove(opts.SocketPath)
	opts.VideoID = v.ID
	opts.Title = v.Title
	opts.URL = plan.url
	opts.AudioFile = plan.audioFile
	opts.AllowPlaylist = plan.launchAllowPlaylist
	opts.IsLive = v.IsLive
	opts.StartPos = startPos
	opts.StartPaused = startPaused
	if plan.effectiveMode != "" && plan.effectiveMode != "auto" {
		opts.Mode = plan.effectiveMode
	}
	switch plan.effectiveMode {
	case "video", "pip", "fs":
		opts.AudioOnly = false
	case "audio":
		opts.AudioOnly = true
	default:
		if !opts.AudioOnly && opts.Mode == "audio" {
			opts.Mode = "window"
		}
	}
	proc, err := StartMPVProcess("mpv", opts)
	if err != nil {
		c.procMu.Unlock()
		c.step3Mu.Unlock()
		return err
	}
	c.mpvProc = proc
	c.procAudioOnly = opts.AudioOnly || opts.Mode == "audio"
	c.procMu.Unlock()
	c.step3Mu.Unlock()
	c.signalWake()
	return nil
}

func (c *IPCClient) tryPlayViaIPC(waitCtx context.Context, seq uint64, v model.Video, plan resolvedPlayTarget) bool {
	if !c.isConnected() {
		return false
	}
	c.launchMu.RLock()
	audioOnly := c.launchDefaults.AudioOnly
	quality := c.launchDefaults.Quality
	c.launchMu.RUnlock()

	wantAudioOnly := false
	wantVid := "auto"
	switch plan.effectiveMode {
	case "audio":
		wantVid = "no"
		wantAudioOnly = true
	case "video", "pip", "fs":
		wantVid = "auto"
		wantAudioOnly = false
	default:
		if audioOnly {
			wantVid = "no"
			wantAudioOnly = true
		}
	}
	// If our managed mpv process was started in a different window mode (e.g.
	// video window vs --no-video --force-window=no), force a clean relaunch so
	// Wayland/Hyprland never leaves a blank window open or fails to open one.
	if c.hasManagedProc() && c.isProcAudioOnly() != wantAudioOnly {
		return false
	}
	_, _ = c.sendCommand(waitCtx, "set_property", "vid", wantVid)
	_, _ = c.sendCommand(waitCtx, "set_property", "ytdl-format", formatSelectorForVideo(quality, wantAudioOnly, v.IsLive))
	_, _ = c.sendCommand(waitCtx, "set_property", "audio-files", []string{})
	if c.hasManagedProc() {
		_, _ = c.sendCommand(waitCtx, "set_property", "force-media-title", "")
	}
	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return true
	}

	var err error
	perFileOpts := map[string]any{}
	if v.Title != "" && c.hasManagedProc() {
		perFileOpts["force-media-title"] = v.Title
	}
	if plan.audioFile != "" {
		perFileOpts["audio-file"] = plan.audioFile
		_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace", -1, perFileOpts)
		if err != nil && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace", 0, "audio-file="+plan.audioFile)
		}
		if err != nil && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.fallbackURL, "replace")
		}
	} else {
		if len(perFileOpts) > 0 {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace", -1, perFileOpts)
		}
		if (len(perFileOpts) == 0 || err != nil) && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.url, "replace")
		}
		if err != nil && plan.url != plan.fallbackURL && c.isCurrentPlay(seq) && c.ctx.Err() == nil && c.isConnected() {
			_, err = c.sendCommand(waitCtx, "loadfile", plan.fallbackURL, "replace")
		}
	}

	if !c.isCurrentPlay(seq) || c.ctx.Err() != nil {
		return true
	}
	if err != nil {
		return false
	}

	if v.Title != "" && !c.hasManagedProc() {
		_, _ = c.sendCommand(waitCtx, "set_property", "force-media-title", v.Title)
	}
	if v.Title != "" {
		c.statusMu.Lock()
		c.status.Title = v.Title
		st := c.status
		c.statusMu.Unlock()
		c.broadcast(st)
	}
	if c.isCurrentPlay(seq) {
		_, _ = c.sendCommand(waitCtx, "set_property", "pause", false)
	}
	return true
}

func (c *IPCClient) scheduleMixAppend(mixCtx context.Context, seq uint64, plan resolvedPlayTarget) {
	if (!plan.isMix && !plan.isAutoplay) || plan.seedID == "" || (plan.playlistFetched && len(plan.playlistIDs) == 0) {
		return
	}
	c.subMu.Lock()
	canSpawn := !c.closed && c.ctx.Err() == nil && c.isCurrentPlay(seq)
	if canSpawn {
		c.wg.Add(1)
	}
	c.subMu.Unlock()
	if canSpawn {
		go c.appendMixTracks(mixCtx, seq, plan.targetURL, plan.seedID, plan.playlistIDs, plan.isAutoplay)
	}
}

func (c *IPCClient) appendMixTracks(mixCtx context.Context, seq uint64, targetURL, seedID string, playlistIDs []string, isAutoplay bool) {
	defer c.wg.Done()

	if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
		return
	}

	if len(playlistIDs) == 0 {
		playlistIDs = c.fetchPlaylistIDs(mixCtx, targetURL, 20)
	}
	if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
		return
	}

	remaining := deduplicateTrackIDs(playlistIDs, seedID)
	if len(remaining) == 0 && isAutoplay {
		c.fallbackMu.RLock()
		fallbackIDs := append([]string(nil), c.autoplayFallbackIDs...)
		c.fallbackMu.RUnlock()
		remaining = deduplicateTrackIDs(fallbackIDs, seedID)
	}
	if len(remaining) == 0 {
		return
	}

	if c.resolver != nil {
		c.resolver.Prefetch(mixCtx, model.Video{ID: remaining[0]})
	}

	if !c.isConnected() && !c.tryConnectImmediateCtx(mixCtx, 2*time.Second) {
		return
	}

	for _, id := range remaining {
		if !c.isCurrentPlay(seq) || mixCtx.Err() != nil {
			return
		}
		if _, err := c.sendCommand(mixCtx, "loadfile", "https://www.youtube.com/watch?v="+id, "append"); err != nil {
			return
		}
	}
}

func deduplicateTrackIDs(rawIDs []string, excludeID string) []string {
	seen := map[string]struct{}{excludeID: {}}
	var out []string
	for _, id := range rawIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (c *IPCClient) fetchPlaylistIDs(ctx context.Context, targetURL string, limit int) []string {
	if targetURL == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if limit <= 0 {
		limit = 25
	}

	c.launchMu.RLock()
	ytdlpBin := c.launchDefaults.YtdlpBin
	cookieJar := c.launchDefaults.CookieJarPath
	c.launchMu.RUnlock()
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}

	args := []string{
		"--flat-playlist",
		"--dump-json",
		"--no-warnings",
		"--ignore-errors",
		"--socket-timeout", "8",
		"--playlist-end", strconv.Itoa(limit),
	}
	if isValidCookieJar(cookieJar) {
		args = append(args, "--cookies", cookieJar)
	}
	args = append(args, targetURL)

	cmd := exec.CommandContext(ctx, ytdlpBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 100 * time.Millisecond

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil && stdout.Len() == 0 {
		return nil
	}

	return parsePlaylistIDs(stdout.Bytes(), limit)
}

func parsePlaylistIDs(data []byte, limit int) []string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}

	var ids []string
	seen := make(map[string]struct{})
	addID := func(raw string) {
		id := strings.TrimSpace(raw)
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if trimmed[0] == '{' {
		var pl struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(trimmed, &pl); err == nil && len(pl.Entries) > 0 {
			for _, e := range pl.Entries {
				addID(e.ID)
				if limit > 0 && len(ids) >= limit {
					break
				}
			}
			return ids
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var entry struct {
			ID   string `json:"id"`
			Type string `json:"_type"`
		}
		if err := json.Unmarshal(line, &entry); err == nil && entry.Type != "playlist" {
			addID(entry.ID)
			if limit > 0 && len(ids) >= limit {
				break
			}
		}
	}
	return ids
}

// Enqueue appends v to the active playlist or delegates to yt __do enqueue.
func (c *IPCClient) Enqueue(ctx context.Context, v model.Video) error {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", v.ID)
	if c.isConnected() {
		if v.Title != "" {
			if _, err := c.sendCommand(ctx, "loadfile", url, "append-play", -1, map[string]any{"force-media-title": v.Title}); err == nil {
				return nil
			}
		}
		_, err := c.sendCommand(ctx, "loadfile", url, "append-play")
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, resolveYtBin(), "__do", "enqueue", v.ID, url, v.Title, v.Channel, v.Duration)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
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

// Seek seeks relative seconds in mpv.
func (c *IPCClient) Seek(ctx context.Context, seconds int) error {
	_, err := c.sendCommand(ctx, "seek", seconds)
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
