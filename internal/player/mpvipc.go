package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/e12jassir/ytx/internal/model"
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

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup

	connMu  sync.RWMutex
	writeMu sync.Mutex
	conn    net.Conn

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
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		pending:        make(map[int64]chan ipcResponse),
		subs:           make(map[uint64]chan Status),
	}
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
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > c.maxBackoff {
					backoff = c.maxBackoff
				}
				continue
			}
			conn = dialed
			c.setConn(conn)
			c.setRunning(true)
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
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
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
		c.broadcast(st)
	}
}

func (c *IPCClient) applyPropertyChange(name string, data any) {
	c.statusMu.Lock()
	changed := false
	switch name {
	case "time-pos":
		if f, ok := toFloat64(data); ok {
			c.status.TimePos = f
			changed = true
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
			c.status.Title = s
			changed = true
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
}

func (c *IPCClient) handleDisconnect(conn net.Conn) {
	c.connMu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.connMu.Unlock()

	_ = conn.Close()
	c.failPending(ErrNotConnected)

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

// Play starts playback of v using the active socket or falls back to PlayVideo.
func (c *IPCClient) Play(ctx context.Context, v model.Video, mode string) error {
	if c.isConnected() {
		url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", v.ID)
		_, err := c.sendCommand(ctx, "loadfile", url, "replace")
		return err
	}
	return PlayVideo(v, mode)
}

// Enqueue appends v to the active playlist or delegates to yt __do enqueue.
func (c *IPCClient) Enqueue(ctx context.Context, v model.Video) error {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", v.ID)
	if c.isConnected() {
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

// Next skips to the next track in the playlist.
func (c *IPCClient) Next(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "playlist-next")
	return err
}

// Prev skips to the previous track in the playlist.
func (c *IPCClient) Prev(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "playlist-prev")
	return err
}

// Seek seeks relative seconds in mpv.
func (c *IPCClient) Seek(ctx context.Context, seconds int) error {
	_, err := c.sendCommand(ctx, "seek", seconds)
	return err
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

// Close shuts down the IPC client, closes the socket, and releases all subscribers.
func (c *IPCClient) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		close(c.done)

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
