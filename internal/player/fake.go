package player

import (
	"context"
	"sync"

	"github.com/e12jassir/ytx/internal/model"
)

// FakeCall records a single method invocation on FakeEngine.
type FakeCall struct {
	Method  string
	Video   model.Video
	Mode    string
	Seconds int
	Delta   float64
	Enabled bool
}

// FakeEngine is a thread-safe mock implementation of Engine for tests.
type FakeEngine struct {
	mu           sync.Mutex
	calls        []FakeCall
	status       Status
	err          error
	subs         map[uint64]chan Status
	nextSubID    uint64
	closed       bool
	done         chan struct{}
	sponsorBlock bool
	audioOnly    bool
	chapters     []Chapter
}

// NewFakeEngine creates a new FakeEngine with default initial status.
func NewFakeEngine() *FakeEngine {
	return &FakeEngine{
		subs:         make(map[uint64]chan Status),
		done:         make(chan struct{}),
		sponsorBlock: true,
	}
}

// SetError configures an error to be returned by subsequent control calls.
func (f *FakeEngine) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// SetStatus replaces the current Status and notifies all subscribers.
func (f *FakeEngine) SetStatus(st Status) {
	f.mu.Lock()
	f.status = st
	f.broadcastLocked(st)
	f.mu.Unlock()
}

// SetChapters configures the chapter list returned by Chapters.
func (f *FakeEngine) SetChapters(chapters []Chapter) {
	f.mu.Lock()
	copied := make([]Chapter, len(chapters))
	copy(copied, chapters)
	f.chapters = copied
	f.mu.Unlock()
}

// Calls returns a snapshot of all recorded method calls.
func (f *FakeEngine) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// Play records the Play call, updates Status, and notifies subscribers.
func (f *FakeEngine) Play(_ context.Context, v model.Video, mode string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Play", Video: v, Mode: mode})
	if f.err != nil {
		return f.err
	}
	f.status.IsRunning = true
	f.status.Title = v.Title
	f.status.Paused = false
	f.status.TimePos = 0
	if f.status.PlaylistCount == 0 {
		f.status.PlaylistCount = 1
	}
	f.broadcastLocked(f.status)
	return nil
}

// Enqueue records the Enqueue call, increments PlaylistCount, and notifies subscribers.
func (f *FakeEngine) Enqueue(_ context.Context, v model.Video) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Enqueue", Video: v})
	if f.err != nil {
		return f.err
	}
	f.status.IsRunning = true
	f.status.PlaylistCount++
	f.broadcastLocked(f.status)
	return nil
}

// TogglePause records the call, toggles Paused, and notifies subscribers.
func (f *FakeEngine) TogglePause(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "TogglePause"})
	if f.err != nil {
		return f.err
	}
	f.status.Paused = !f.status.Paused
	f.broadcastLocked(f.status)
	return nil
}

// Next records the Next call and notifies subscribers.
func (f *FakeEngine) Next(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Next"})
	if f.err != nil {
		return f.err
	}
	f.status.TimePos = 0
	f.broadcastLocked(f.status)
	return nil
}

// Prev records the Prev call and notifies subscribers.
func (f *FakeEngine) Prev(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Prev"})
	if f.err != nil {
		return f.err
	}
	f.status.TimePos = 0
	f.broadcastLocked(f.status)
	return nil
}

// Seek records the Seek call, adjusts TimePos, and notifies subscribers.
func (f *FakeEngine) Seek(_ context.Context, seconds int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Seek", Seconds: seconds})
	if f.err != nil {
		return f.err
	}
	f.status.TimePos += float64(seconds)
	if f.status.TimePos < 0 {
		f.status.TimePos = 0
	}
	f.broadcastLocked(f.status)
	return nil
}

// SeekAbsolute records the SeekAbsolute call, sets TimePos, and notifies subscribers.
func (f *FakeEngine) SeekAbsolute(_ context.Context, seconds float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "SeekAbsolute", Delta: seconds})
	if f.err != nil {
		return f.err
	}
	if seconds < 0 {
		seconds = 0
	}
	f.status.TimePos = seconds
	f.broadcastLocked(f.status)
	return nil
}

// SetSponsorBlock records the call and toggles FakeEngine SponsorBlock state.
func (f *FakeEngine) SetSponsorBlock(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "SetSponsorBlock", Enabled: enabled})
	f.sponsorBlock = enabled
}

// SponsorBlockEnabled returns whether SponsorBlock skipping is enabled on FakeEngine.
func (f *FakeEngine) SponsorBlockEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sponsorBlock
}

// ToggleVideoMode records the call, toggles video/audio-only mode, and returns videoOn.
func (f *FakeEngine) ToggleVideoMode(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "ToggleVideoMode"})
	if f.err != nil {
		return !f.audioOnly, f.err
	}
	f.audioOnly = !f.audioOnly
	return !f.audioOnly, nil
}

// Chapters records the call and returns configured fake chapters.
func (f *FakeEngine) Chapters(_ context.Context) ([]Chapter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Chapters"})
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Chapter, len(f.chapters))
	copy(out, f.chapters)
	return out, nil
}

// AddVolume records the AddVolume call, adjusts Volume, and notifies subscribers.
func (f *FakeEngine) AddVolume(_ context.Context, delta float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "AddVolume", Delta: delta})
	if f.err != nil {
		return f.err
	}
	f.status.Volume += delta
	if f.status.Volume < 0 {
		f.status.Volume = 0
	}
	f.broadcastLocked(f.status)
	return nil
}

// Status returns the current cached Status.
func (f *FakeEngine) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// Subscribe returns a channel that receives Status updates until ctx is canceled or Close is called.
func (f *FakeEngine) Subscribe(ctx context.Context) <-chan Status {
	if ctx == nil {
		ctx = context.Background()
	}
	f.mu.Lock()
	if f.closed || ctx.Err() != nil {
		f.mu.Unlock()
		ch := make(chan Status)
		close(ch)
		return ch
	}

	ch := make(chan Status, 16)
	id := f.nextSubID
	f.nextSubID++
	f.subs[id] = ch
	f.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-f.done:
		}
		f.mu.Lock()
		if subCh, ok := f.subs[id]; ok {
			delete(f.subs, id)
			close(subCh)
		}
		f.mu.Unlock()
	}()

	return ch
}

// Close marks FakeEngine closed and closes all subscriber channels.
func (f *FakeEngine) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.done)
	for id, ch := range f.subs {
		delete(f.subs, id)
		close(ch)
	}
	return nil
}

func (f *FakeEngine) broadcastLocked(st Status) {
	if f.closed {
		return
	}
	for _, ch := range f.subs {
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
