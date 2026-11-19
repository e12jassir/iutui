//go:build linux

package platform

import (
	"math"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	mprisBusName     = "org.mpris.MediaPlayer2.iutui"
	mprisObjectPath  = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRootIface   = "org.mpris.MediaPlayer2"
	mprisPlayerIface = "org.mpris.MediaPlayer2.Player"

	mprisMinSeekMicros = math.MinInt64 / int64(time.Microsecond)
	mprisMaxSeekMicros = math.MaxInt64 / int64(time.Microsecond)
)

// MPRISBridge publishes playback state and handles remote media-key controls over D-Bus MPRIS2.
type MPRISBridge struct {
	cb        MPRISCallbacks
	mu        sync.RWMutex
	conn      *dbus.Conn
	props     *prop.Properties
	active    bool
	closed    bool
	hasProps  bool
	lastProps MPRISProps
	lastAt    time.Time
	closeOnce sync.Once
	closeErr  error
}

type mprisRoot struct{}

func (mprisRoot) Raise() *dbus.Error { return nil }
func (mprisRoot) Quit() *dbus.Error  { return nil }

type mprisPlayer struct {
	bridge *MPRISBridge
}

func (p *mprisPlayer) invoke(fn func()) *dbus.Error {
	if p == nil || p.bridge == nil || fn == nil {
		return nil
	}
	b := p.bridge
	b.mu.RLock()
	active := !b.closed && b.active
	b.mu.RUnlock()
	if active {
		fn()
	}
	return nil
}

func (p *mprisPlayer) Next() *dbus.Error      { return p.invoke(p.bridge.cb.OnNext) }
func (p *mprisPlayer) Previous() *dbus.Error  { return p.invoke(p.bridge.cb.OnPrevious) }
func (p *mprisPlayer) Pause() *dbus.Error     { return p.invoke(p.bridge.cb.OnPause) }
func (p *mprisPlayer) PlayPause() *dbus.Error { return p.invoke(p.bridge.cb.OnPlayPause) }
func (p *mprisPlayer) Stop() *dbus.Error      { return p.invoke(p.bridge.cb.OnStop) }
func (p *mprisPlayer) Play() *dbus.Error      { return p.invoke(p.bridge.cb.OnPlay) }

// DBusSeek is exported as "Seek" via ExportWithMap to avoid go vet's stdmethods
// false positive against io.Seeker.Seek(int64, int) (int64, error).
func (p *mprisPlayer) DBusSeek(offset int64) *dbus.Error {
	if p == nil || p.bridge == nil {
		return nil
	}
	b := p.bridge
	b.mu.RLock()
	active := !b.closed && b.active
	b.mu.RUnlock()
	if active && b.cb.OnSeek != nil {
		b.cb.OnSeek(clampMicrosToDuration(offset))
	}
	return nil
}

func (p *mprisPlayer) SetPosition(trackID dbus.ObjectPath, position int64) *dbus.Error {
	if p == nil || p.bridge == nil || position < 0 {
		return nil
	}
	b := p.bridge
	b.mu.Lock()
	if b.closed || !b.active || !b.hasProps {
		b.mu.Unlock()
		return nil
	}
	currentTrack, _ := b.lastProps.Metadata["mpris:trackid"].(string)
	if currentTrack == "" || string(trackID) != currentTrack {
		b.mu.Unlock()
		return nil
	}
	if lengthUs, ok := b.lastProps.Metadata["mpris:length"].(int64); ok && lengthUs > 0 && position > lengthUs {
		b.mu.Unlock()
		return nil
	}
	deltaUs := position - b.lastProps.Position
	conn := b.conn
	b.mu.Unlock()

	if b.cb.OnSeek != nil {
		b.cb.OnSeek(clampMicrosToDuration(deltaUs))
	}
	if conn != nil && conn.Connected() {
		_ = conn.Emit(mprisObjectPath, mprisPlayerIface+".Seeked", position)
	}
	return nil
}

func (p *mprisPlayer) OpenUri(uri string) *dbus.Error {
	_ = uri
	return nil
}

// NewMPRISBridge connects a dedicated private session bus connection and exports
// the MPRIS2 MediaPlayer2 and Player interfaces. If no session bus is available
// or claiming org.mpris.MediaPlayer2.iutui fails, it returns an inactive no-op bridge.
func NewMPRISBridge(cb MPRISCallbacks) *MPRISBridge {
	b := &MPRISBridge{cb: cb}

	// Respect explicit empty DBUS_SESSION_BUS_ADDRESS (e.g. headless/disabled env or tests)
	// without falling back to /run/user/<uid>/bus or spawning dbus-launch.
	if addr, set := os.LookupEnv("DBUS_SESSION_BUS_ADDRESS"); set && addr == "" {
		return b
	}

	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return b
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return b
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return b
	}

	rootObj := mprisRoot{}
	playerObj := &mprisPlayer{bridge: b}

	if err := conn.Export(rootObj, mprisObjectPath, mprisRootIface); err != nil {
		_ = conn.Close()
		return b
	}
	if err := conn.ExportWithMap(playerObj, map[string]string{"DBusSeek": "Seek"}, mprisObjectPath, mprisPlayerIface); err != nil {
		_ = conn.Close()
		return b
	}

	props, err := prop.Export(conn, mprisObjectPath, initialMPRISPropMap())
	if err != nil {
		_ = conn.Close()
		return b
	}

	playerMethods := introspect.Methods(playerObj)
	for i := range playerMethods {
		if playerMethods[i].Name == "DBusSeek" {
			playerMethods[i].Name = "Seek"
		}
	}

	node := &introspect.Node{
		Name: string(mprisObjectPath),
		Interfaces: []introspect.Interface{
			prop.IntrospectData,
			{
				Name:       mprisRootIface,
				Methods:    introspect.Methods(rootObj),
				Properties: props.Introspection(mprisRootIface),
			},
			{
				Name:    mprisPlayerIface,
				Methods: playerMethods,
				Signals: []introspect.Signal{
					{
						Name: "Seeked",
						Args: []introspect.Arg{
							{Name: "Position", Type: "x"},
						},
					},
				},
				Properties: props.Introspection(mprisPlayerIface),
			},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), mprisObjectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		_ = conn.Close()
		return b
	}

	reply, err := conn.RequestName(mprisBusName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		unexportMPRIS(conn)
		_ = conn.Close()
		return b
	}

	b.conn = conn
	b.props = props
	b.active = true
	return b
}

// Active reports whether the MPRIS2 D-Bus bridge is connected and owns its bus name.
func (b *MPRISBridge) Active() bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.active && !b.closed && b.conn != nil && b.conn.Connected()
}

// UpdateStatus publishes changed playback properties over D-Bus thread-safely.
func (b *MPRISBridge) UpdateStatus(st MPRISStatus) {
	if b == nil {
		return
	}

	next := BuildMPRISProps(st)
	now := time.Now()

	b.mu.Lock()
	if !b.active || b.closed || b.props == nil || b.conn == nil {
		b.mu.Unlock()
		return
	}
	if !b.conn.Connected() {
		b.active = false
		b.mu.Unlock()
		return
	}

	if !b.hasProps || next.PlaybackStatus != b.lastProps.PlaybackStatus {
		if !b.setProp("PlaybackStatus", next.PlaybackStatus) {
			b.mu.Unlock()
			return
		}
	}
	if !b.hasProps || next.Rate != b.lastProps.Rate {
		if !b.setProp("Rate", next.Rate) {
			b.mu.Unlock()
			return
		}
	}
	if !b.hasProps || next.Volume != b.lastProps.Volume {
		if !b.setProp("Volume", next.Volume) {
			b.mu.Unlock()
			return
		}
	}
	if !b.hasProps || !metadataEqual(next.Metadata, b.lastProps.Metadata) {
		if !b.setProp("Metadata", toDBusMetadata(next.Metadata)) {
			b.mu.Unlock()
			return
		}
	}
	if !b.hasProps || next.Position != b.lastProps.Position {
		if !b.setProp("Position", next.Position) {
			b.mu.Unlock()
			return
		}
	}

	emitSeeked := false
	if b.hasProps &&
		next.PlaybackStatus != "Stopped" &&
		b.lastProps.PlaybackStatus != "Stopped" &&
		metadataTrackID(next.Metadata) == metadataTrackID(b.lastProps.Metadata) {
		elapsedUs := max(now.Sub(b.lastAt).Microseconds(), 0)
		expectedUs := b.lastProps.Position
		if b.lastProps.PlaybackStatus == "Playing" {
			expectedUs += int64(float64(elapsedUs) * b.lastProps.Rate)
		}
		diff := next.Position - expectedUs
		if diff < -1_500_000 || diff > 2_000_000 {
			emitSeeked = true
		}
	}

	b.lastProps = next
	b.lastAt = now
	b.hasProps = true
	conn := b.conn
	b.mu.Unlock()

	if emitSeeked && conn != nil && conn.Connected() {
		_ = conn.Emit(mprisObjectPath, mprisPlayerIface+".Seeked", next.Position)
	}
}

// Close unexports MPRIS2 objects, releases the bus name, and closes the private D-Bus connection.
func (b *MPRISBridge) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.active = false
		conn := b.conn
		b.conn = nil
		b.props = nil
		b.mu.Unlock()

		if conn != nil {
			unexportMPRIS(conn)
			_, _ = conn.ReleaseName(mprisBusName)
			b.closeErr = conn.Close()
		}
	})
	return b.closeErr
}

// setProp updates a property on prop.Properties while recovering if the underlying
// D-Bus transport closed concurrently (SetMust panics if Emit fails). Caller holds b.mu.
func (b *MPRISBridge) setProp(name string, val any) bool {
	if b.props == nil {
		return false
	}
	ok := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				ok = false
			}
		}()
		b.props.SetMust(mprisPlayerIface, name, val)
	}()
	if !ok {
		b.active = false
	}
	return ok
}

func unexportMPRIS(conn *dbus.Conn) {
	_ = conn.Export(nil, mprisObjectPath, mprisRootIface)
	_ = conn.Export(nil, mprisObjectPath, mprisPlayerIface)
	_ = conn.Export(nil, mprisObjectPath, "org.freedesktop.DBus.Properties")
	_ = conn.Export(nil, mprisObjectPath, "org.freedesktop.DBus.Introspectable")
}

func initialMPRISPropMap() prop.Map {
	return prop.Map{
		mprisRootIface: {
			"CanQuit": {
				Value: false,
				Emit:  prop.EmitConst,
			},
			"CanRaise": {
				Value: false,
				Emit:  prop.EmitConst,
			},
			"HasTrackList": {
				Value: false,
				Emit:  prop.EmitConst,
			},
			"Identity": {
				Value: "iutui",
				Emit:  prop.EmitConst,
			},
			"DesktopEntry": {
				Value: "iutui",
				Emit:  prop.EmitConst,
			},
			"SupportedUriSchemes": {
				Value: []string{"https", "http"},
				Emit:  prop.EmitConst,
			},
			"SupportedMimeTypes": {
				Value: []string{"video/*", "audio/*"},
				Emit:  prop.EmitConst,
			},
		},
		mprisPlayerIface: {
			"PlaybackStatus": {
				Value: "Stopped",
				Emit:  prop.EmitTrue,
			},
			"Rate": {
				Value: 1.0,
				Emit:  prop.EmitTrue,
			},
			"Metadata": {
				Value: map[string]dbus.Variant{},
				Emit:  prop.EmitTrue,
			},
			"Volume": {
				Value: 1.0,
				Emit:  prop.EmitTrue,
			},
			"Position": {
				Value: int64(0),
				Emit:  prop.EmitFalse,
			},
			"MinimumRate": {
				Value: 0.5,
				Emit:  prop.EmitConst,
			},
			"MaximumRate": {
				Value: 2.0,
				Emit:  prop.EmitConst,
			},
			"CanGoNext": {
				Value: true,
				Emit:  prop.EmitConst,
			},
			"CanGoPrevious": {
				Value: true,
				Emit:  prop.EmitConst,
			},
			"CanPlay": {
				Value: true,
				Emit:  prop.EmitConst,
			},
			"CanPause": {
				Value: true,
				Emit:  prop.EmitConst,
			},
			"CanSeek": {
				Value: true,
				Emit:  prop.EmitConst,
			},
			"CanControl": {
				Value: true,
				Emit:  prop.EmitConst,
			},
		},
	}
}

func toDBusMetadata(meta map[string]any) map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(meta))
	for k, v := range meta {
		if k == "mpris:trackid" {
			if s, ok := v.(string); ok {
				out[k] = dbus.MakeVariant(dbus.ObjectPath(s))
				continue
			}
		}
		out[k] = dbus.MakeVariant(v)
	}
	return out
}

func metadataEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func metadataTrackID(meta map[string]any) string {
	s, _ := meta["mpris:trackid"].(string)
	return s
}

func clampMicrosToDuration(us int64) time.Duration {
	if us < mprisMinSeekMicros {
		return time.Duration(mprisMinSeekMicros) * time.Microsecond
	}
	if us > mprisMaxSeekMicros {
		return time.Duration(mprisMaxSeekMicros) * time.Microsecond
	}
	return time.Duration(us) * time.Microsecond
}
