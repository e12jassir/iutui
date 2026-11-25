package player

import (
	"context"
	"fmt"
	"sync"

	"github.com/e12jassir/iutui/internal/model"
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
	quality      int
	mode         string
	cookieJar    string
	chapters     []Chapter
	playlist     []PlaylistItem
	SubtitlesOn  bool
	PiPOn        bool
}

// NewFakeEngine creates a new FakeEngine with default initial status.
func NewFakeEngine() *FakeEngine {
	return &FakeEngine{
		status:       Status{Speed: 1.0, PlaylistPos: -1},
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

// SetPlaylist configures the playlist returned by Playlist and updates PlaylistCount.
func (f *FakeEngine) SetPlaylist(items []PlaylistItem) {
	f.mu.Lock()
	copied := make([]PlaylistItem, len(items))
	copy(copied, items)
	f.playlist = copied
	f.status.PlaylistCount = len(copied)
	pos := -1
	for i, item := range copied {
		if item.Current {
			pos = i
			if id := ExtractYouTubeID(item.Filename); id != "" {
				f.status.VideoID = id
			}
			break
		}
	}
	if pos < 0 && len(copied) > 0 {
		pos = 0
	}
	f.status.PlaylistPos = pos
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
	f.status.VideoID = model.MixSeedVideoID(v.ID)
	f.status.Paused = false
	f.status.TimePos = 0
	if f.status.PlaylistCount == 0 {
		f.status.PlaylistCount = 1
		f.status.PlaylistPos = 0
	} else if f.status.PlaylistPos < 0 {
		f.status.PlaylistPos = 0
	}
	f.broadcastLocked(f.status)
	return nil
}

// PlayFrom records the PlayFrom call, updates Status with startPos, and notifies subscribers.
func (f *FakeEngine) PlayFrom(_ context.Context, v model.Video, mode string, startPos float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "PlayFrom", Video: v, Mode: mode, Delta: startPos})
	if f.err != nil {
		return f.err
	}
	if startPos < 0 {
		startPos = 0
	}
	f.status.IsRunning = true
	f.status.Title = v.Title
	f.status.VideoID = model.MixSeedVideoID(v.ID)
	f.status.Paused = false
	f.status.TimePos = startPos
	if f.status.PlaylistCount == 0 {
		f.status.PlaylistCount = 1
		f.status.PlaylistPos = 0
	} else if f.status.PlaylistPos < 0 {
		f.status.PlaylistPos = 0
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
	if len(f.playlist) > 0 && f.status.PlaylistPos >= 0 && f.status.PlaylistPos+1 < len(f.playlist) {
		f.status.PlaylistPos++
		for i := range f.playlist {
			f.playlist[i].Current = i == f.status.PlaylistPos
		}
		f.status.Title = f.playlist[f.status.PlaylistPos].Title
		f.status.VideoID = ExtractYouTubeID(f.playlist[f.status.PlaylistPos].Filename)
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
	if len(f.playlist) > 0 && f.status.PlaylistPos > 0 {
		f.status.PlaylistPos--
		for i := range f.playlist {
			f.playlist[i].Current = i == f.status.PlaylistPos
		}
		f.status.Title = f.playlist[f.status.PlaylistPos].Title
		f.status.VideoID = ExtractYouTubeID(f.playlist[f.status.PlaylistPos].Filename)
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

// SetPlaybackProfile updates FakeEngine quality and audio-only state and records the call.
func (f *FakeEngine) SetPlaybackProfile(_ context.Context, quality int, audioOnly bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "SetPlaybackProfile", Seconds: quality, Enabled: audioOnly})
	if f.err != nil {
		return f.err
	}
	f.quality = quality
	f.audioOnly = audioOnly
	return nil
}

// AudioOnly reports the current FakeEngine audio-only flag.
func (f *FakeEngine) AudioOnly() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.audioOnly
}

// Quality reports the current FakeEngine max resolution.
func (f *FakeEngine) Quality() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quality
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

// SetVolume records SetVolume, clamps Volume to [5, 100], and notifies subscribers.
func (f *FakeEngine) SetVolume(_ context.Context, vol int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if vol < 5 {
		vol = 5
	} else if vol > 100 {
		vol = 100
	}
	f.calls = append(f.calls, FakeCall{Method: "SetVolume", Seconds: vol})
	if f.err != nil {
		return f.err
	}
	f.status.Volume = float64(vol)
	f.broadcastLocked(f.status)
	return nil
}

// SetLaunchMode records SetLaunchMode and updates FakeEngine mode & audioOnly state.
func (f *FakeEngine) SetLaunchMode(_ context.Context, mode string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "SetLaunchMode", Mode: mode})
	if f.err != nil {
		return f.err
	}
	f.mode = mode
	f.audioOnly = mode == "audio"
	return nil
}

// LaunchMode reports the configured FakeEngine launch mode.
func (f *FakeEngine) LaunchMode() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mode
}

// SetCookieJar records SetCookieJar on FakeEngine.
func (f *FakeEngine) SetCookieJar(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cookieJar = path
	f.calls = append(f.calls, FakeCall{Method: "SetCookieJar", Mode: path})
}

// CookieJar reports the configured FakeEngine cookie jar path.
func (f *FakeEngine) CookieJar() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cookieJar
}

// SetSpeed records the SetSpeed call, clamps Speed between 0.5 and 2.0, and notifies subscribers.
func (f *FakeEngine) SetSpeed(_ context.Context, speed float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "SetSpeed", Delta: speed})
	if f.err != nil {
		return f.err
	}
	f.status.Speed = clampSpeed(speed)
	f.broadcastLocked(f.status)
	return nil
}

// ToggleLoop records the ToggleLoop call, flips LoopFile, and notifies subscribers.
func (f *FakeEngine) ToggleLoop(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "ToggleLoop"})
	if f.err != nil {
		return f.status.LoopFile, f.err
	}
	f.status.LoopFile = !f.status.LoopFile
	f.broadcastLocked(f.status)
	return f.status.LoopFile, nil
}

// CycleSubtitles records the CycleSubtitles call and toggles SubtitlesOn.
func (f *FakeEngine) CycleSubtitles(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "CycleSubtitles"})
	if f.err != nil {
		return f.SubtitlesOn, f.err
	}
	f.SubtitlesOn = !f.SubtitlesOn
	return f.SubtitlesOn, nil
}

// TogglePiP records the TogglePiP call and toggles PiPOn.
func (f *FakeEngine) TogglePiP(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "TogglePiP"})
	if f.err != nil {
		return f.PiPOn, f.err
	}
	f.PiPOn = !f.PiPOn
	return f.PiPOn, nil
}

// Playlist records the call and returns a copy of the configured fake playlist.
func (f *FakeEngine) Playlist(_ context.Context) ([]PlaylistItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "Playlist"})
	if f.err != nil {
		return nil, f.err
	}
	out := make([]PlaylistItem, len(f.playlist))
	copy(out, f.playlist)
	return out, nil
}

// PlayPlaylistIndex records the call, activates the playlist item at index, and notifies subscribers.
func (f *FakeEngine) PlayPlaylistIndex(_ context.Context, index int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "PlayPlaylistIndex", Seconds: index})
	if f.err != nil {
		return f.err
	}
	if index < 0 {
		return fmt.Errorf("invalid playlist index: %d", index)
	}
	if len(f.playlist) > 0 {
		if index >= len(f.playlist) {
			return fmt.Errorf("playlist index out of range: %d", index)
		}
		for i := range f.playlist {
			f.playlist[i].Current = i == index
		}
		f.status.Title = f.playlist[index].Title
		f.status.VideoID = ExtractYouTubeID(f.playlist[index].Filename)
	}
	f.status.PlaylistPos = index
	f.status.IsRunning = true
	f.status.Paused = false
	f.status.TimePos = 0
	f.broadcastLocked(f.status)
	return nil
}

// RemovePlaylistIndex records the call, removes index from the fake playlist, and notifies subscribers.
func (f *FakeEngine) RemovePlaylistIndex(_ context.Context, index int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "RemovePlaylistIndex", Seconds: index})
	if f.err != nil {
		return f.err
	}
	if index < 0 {
		return fmt.Errorf("invalid playlist index: %d", index)
	}
	if len(f.playlist) > 0 {
		if index >= len(f.playlist) {
			return fmt.Errorf("playlist index out of range: %d", index)
		}
		f.playlist = append(f.playlist[:index], f.playlist[index+1:]...)
		curPos := -1
		for i := range f.playlist {
			f.playlist[i].Index = i
			if f.playlist[i].Current {
				curPos = i
			}
		}
		f.status.PlaylistCount = len(f.playlist)
		f.status.PlaylistPos = curPos
	} else if f.status.PlaylistCount > 0 {
		f.status.PlaylistCount--
	}
	f.broadcastLocked(f.status)
	return nil
}

// ClearPlaylist records the call, retains only the current item if present, and notifies subscribers.
func (f *FakeEngine) ClearPlaylist(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Method: "ClearPlaylist"})
	if f.err != nil {
		return f.err
	}
	var kept []PlaylistItem
	for _, item := range f.playlist {
		if item.Current {
			item.Index = 0
			kept = append(kept, item)
			break
		}
	}
	f.playlist = kept
	f.status.PlaylistCount = len(kept)
	if len(kept) > 0 {
		f.status.PlaylistPos = 0
	} else {
		f.status.PlaylistPos = -1
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
