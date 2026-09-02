package skt

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Player states from javax.microedition.media.Player.
const (
	playerClosed     int32 = 0
	playerUnrealized int32 = 100
	playerRealized   int32 = 200
	playerPrefetched int32 = 300
	playerStarted    int32 = 400
)

// maxMediaBytes bounds one decoded sound so a broken or hostile stream cannot
// make the runtime allocate without limit.
const maxMediaBytes = 8 << 20

// playerData is one JSR-135 Player. The state machine is the specified one;
// start() hands the decoded sequence to the same backend.Audio timeline every
// other sound in this runtime plays on.
type playerData struct {
	mu          sync.Mutex
	state       int32
	contentType string
	handle      backend.AudioHandle
	duration    time.Duration
	loops       int32
	mediaTime   int64
	listeners   []*jvm.Object
}

// AttachAudioSink supplies the Host audio sink MIDP players play through.
// Without one the players still run their state machine and report their
// events, so a game that waits for STARTED is not stuck; it simply makes no
// sound.
func (runtime *Runtime) AttachAudioSink(sink backend.AudioSink) {
	if runtime == nil {
		return
	}
	runtime.audioMu.Lock()
	runtime.audio = backend.NewAudio(sink)
	runtime.audioMu.Unlock()
}

// AdvanceAudio moves the audio timeline to now. A Host with a frame loop calls
// it once per frame; a Host that only starts a MIDlet and reads its state
// never has to.
func (runtime *Runtime) AdvanceAudio(now time.Duration) {
	runtime.audioMu.Lock()
	audio := runtime.audio
	runtime.audioMu.Unlock()
	if audio != nil {
		audio.Advance(now)
	}
}

func (runtime *Runtime) audioTimeline() *backend.Audio {
	runtime.audioMu.Lock()
	defer runtime.audioMu.Unlock()
	return runtime.audio
}

func playerArgument(arguments []jvm.Value, index int) (*jvm.Object, *playerData, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, nil, err
	}
	if object == nil {
		return nil, nil, newGuestException("java/lang/NullPointerException", "Player is null")
	}
	data, ok := object.Native.(*playerData)
	if object.ClassName != midp.PlayerClass || !ok || data == nil {
		return nil, nil, fmt.Errorf("argument %d is not a Player", index)
	}
	return object, data, nil
}

// createPlayerFromStream reads the whole stream, because a Player must be able
// to answer getDuration and to restart, and a MIDP InputStream is not
// seekable.
func (runtime *Runtime) createPlayerFromStream(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if stream == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "media stream is null")
	}
	contentType, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, err := runtime.readGuestStream(stream)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.newPlayer(vm, data, contentType)
}

// createPlayerFromLocator serves the locators this runtime can honor. A
// network locator is refused rather than accepted and left silent, because a
// game told a player exists waits for events that will never come.
func (runtime *Runtime) createPlayerFromLocator(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	locator, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch {
	case locator == midp.ToneDeviceLocator:
		return runtime.newPlayer(vm, nil, "audio/x-tone-seq")
	case strings.HasPrefix(locator, "resource:") || strings.HasPrefix(locator, "/"):
		name := strings.TrimPrefix(strings.TrimPrefix(locator, "resource:"), "/")
		data, ok := runtime.Archive.Resource(name)
		if !ok {
			return jvm.VoidValue(), newGuestException("java/io/IOException", "media resource not found: "+locator)
		}
		return runtime.newPlayer(vm, data, "")
	}
	return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, "unsupported media locator: "+locator)
}

// newPlayer decodes the media now so an undecodable file fails at
// createPlayer, where MIDP says the failure belongs, instead of silently at
// start().
func (runtime *Runtime) newPlayer(_ *jvm.VM, data []byte, contentType string) (jvm.Value, error) {
	player := &playerData{state: playerUnrealized, contentType: contentType, loops: 1}
	if len(data) > 0 {
		audio := runtime.audioTimeline()
		if audio == nil {
			// Without a Host sink there is nothing to load into, but the
			// content still has to be judged playable here rather than later.
			if len(smaf.Play(data)) == 0 {
				return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass,
					"unsupported media content")
			}
		} else {
			handle, err := audio.Load(data)
			if err != nil {
				return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
			}
			player.handle = handle
			player.duration, _ = audio.Length(handle)
		}
		if contentType == "" {
			player.contentType = "application/vnd.smaf"
		}
	}
	return jvm.ReferenceValue(&jvm.Object{
		ClassName: midp.PlayerClass,
		Fields:    make(map[string]jvm.Value),
		Native:    player,
	}), nil
}

// readGuestStream drains a guest InputStream through its own read method, so
// any stream the runtime or the application provides works.
func (runtime *Runtime) readGuestStream(stream *jvm.Object) ([]byte, error) {
	buffer, err := runtime.VM.NewArray(jvm.Type{Kind: jvm.TypeByte}, 4096)
	if err != nil {
		return nil, err
	}
	var data []byte
	for {
		result, err := runtime.VM.InvokeVirtual(stream, "read", "([B)I", jvm.ReferenceValue(buffer))
		if err != nil {
			return nil, fmt.Errorf("read media stream: %w", err)
		}
		count, err := result.Int32()
		if err != nil {
			return nil, err
		}
		if count <= 0 {
			break
		}
		_, values, err := jvm.ArraySnapshot(buffer)
		if err != nil {
			return nil, err
		}
		for index := 0; index < int(count) && index < len(values); index++ {
			raw, valueErr := values[index].Int32()
			if valueErr != nil {
				return nil, valueErr
			}
			data = append(data, byte(raw))
		}
		if len(data) > maxMediaBytes {
			return nil, newGuestException(midp.MediaExceptionClass, "media stream is too large")
		}
	}
	return data, nil
}

func (runtime *Runtime) playerRealize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.playerTransition(arguments, playerRealized)
}

func (runtime *Runtime) playerPrefetch(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.playerTransition(arguments, playerPrefetched)
}

// playerTransition moves a player forward through the state machine. MIDP
// allows skipping states, so realize() on a prefetched player is a no-op
// rather than an error.
func (runtime *Runtime) playerTransition(arguments []jvm.Value, target int32) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if player.state < target {
		player.state = target
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) playerStart(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	if player.state == playerClosed {
		player.mu.Unlock()
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if player.state == playerStarted {
		player.mu.Unlock()
		return jvm.VoidValue(), nil
	}
	player.state = playerStarted
	handle := player.handle
	// A negative loop count is JSR-135's "loop forever"; any count above one
	// is the same repeat to a timeline that only knows repeat or not.
	repeat := player.loops < 0 || player.loops > 1
	player.mu.Unlock()

	if audio := runtime.audioTimeline(); audio != nil && handle != 0 {
		if err := audio.Play(handle, runtime.audioNow(), repeat); err != nil {
			return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
		}
	}
	return jvm.VoidValue(), runtime.notifyPlayerListeners(object, player, midp.PlayerEventStarted)
}

// audioNow is the timeline reading a sound starts at. The runtime has no
// frame clock of its own, so it uses the same wall clock RMS stamps with,
// which a Host driving AdvanceAudio also passes.
func (runtime *Runtime) audioNow() time.Duration {
	return time.Duration(runtime.clockMillis()) * time.Millisecond
}

func (runtime *Runtime) playerStop(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	if player.state != playerStarted {
		player.mu.Unlock()
		return jvm.VoidValue(), nil
	}
	player.state = playerPrefetched
	handle := player.handle
	player.mu.Unlock()
	if audio := runtime.audioTimeline(); audio != nil && handle != 0 {
		audio.Stop(handle)
	}
	return jvm.VoidValue(), runtime.notifyPlayerListeners(object, player, midp.PlayerEventStopped)
}

func (runtime *Runtime) playerDeallocate(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	handle := player.handle
	if player.state > playerRealized {
		player.state = playerRealized
	}
	player.mu.Unlock()
	if audio := runtime.audioTimeline(); audio != nil && handle != 0 {
		audio.Stop(handle)
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) playerClose(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	if player.state == playerClosed {
		player.mu.Unlock()
		return jvm.VoidValue(), nil
	}
	player.state = playerClosed
	handle := player.handle
	player.handle = 0
	player.mu.Unlock()
	if audio := runtime.audioTimeline(); audio != nil && handle != 0 {
		_ = audio.Close(handle)
	}
	return jvm.VoidValue(), runtime.notifyPlayerListeners(object, player, midp.PlayerEventClosed)
}

func (runtime *Runtime) playerState(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	return jvm.IntValue(player.state), nil
}

func (runtime *Runtime) playerDuration(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	if player.duration == 0 {
		// JSR-135's TIME_UNKNOWN, which is the honest answer for a player
		// whose content the runtime never measured.
		return jvm.LongValue(-1), nil
	}
	return jvm.LongValue(player.duration.Microseconds()), nil
}

func (runtime *Runtime) playerMediaTime(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	return jvm.LongValue(player.mediaTime), nil
}

func (runtime *Runtime) setPlayerMediaTime(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	now, err := arguments[1].Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	// Only the start of the media is a position this timeline can seek to;
	// answering anything else would report a time the sink is not at.
	if now != 0 {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, "only media time 0 can be set")
	}
	player.mu.Lock()
	player.mediaTime = 0
	handle, started := player.handle, player.state == playerStarted
	repeat := player.loops < 0 || player.loops > 1
	player.mu.Unlock()
	if started {
		if audio := runtime.audioTimeline(); audio != nil && handle != 0 {
			if err := audio.Play(handle, runtime.audioNow(), repeat); err != nil {
				return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
			}
		}
	}
	return jvm.LongValue(0), nil
}

func (runtime *Runtime) setPlayerLoopCount(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	count, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if count == 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "loop count 0")
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	if player.state == playerStarted {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is started")
	}
	player.loops = count
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) playerContentType(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	if player.state < playerRealized {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is not realized")
	}
	return jvm.ReferenceValue(vm.NewString(player.contentType)), nil
}

func (runtime *Runtime) addPlayerListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil || listener == nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	for _, existing := range player.listeners {
		if existing == listener {
			return jvm.VoidValue(), nil
		}
	}
	player.listeners = append(player.listeners, listener)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) removePlayerListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil || listener == nil {
		return jvm.VoidValue(), err
	}
	player.mu.Lock()
	defer player.mu.Unlock()
	remaining := player.listeners[:0]
	for _, existing := range player.listeners {
		if existing != listener {
			remaining = append(remaining, existing)
		}
	}
	player.listeners = remaining
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) notifyPlayerListeners(object *jvm.Object, player *playerData, event string) error {
	player.mu.Lock()
	listeners := append([]*jvm.Object(nil), player.listeners...)
	player.mu.Unlock()
	for _, listener := range listeners {
		if _, err := runtime.VM.InvokeVirtual(listener, "playerUpdate",
			"(Ljavax/microedition/media/Player;Ljava/lang/String;Ljava/lang/Object;)V",
			jvm.ReferenceValue(object), jvm.ReferenceValue(runtime.VM.NewString(event)),
			jvm.ReferenceValue(nil)); err != nil {
			if absorbed := runtime.absorbUncaughtCallback("playerUpdate "+listener.ClassName, err); absorbed != nil {
				return fmt.Errorf("deliver playerUpdate %s: %w", event, absorbed)
			}
		}
	}
	return nil
}

// playTone plays one note. The Host sink speaks the same note events the SMAF
// player produces, so a tone is a two-event sequence rather than a second
// path into the sink.
func (runtime *Runtime) playTone(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	note, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	duration, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	volume, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if note < 0 || note > 127 || duration < 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("tone note %d duration %d", note, duration))
	}
	audio := runtime.audioTimeline()
	if audio == nil {
		return jvm.VoidValue(), nil
	}
	handle, err := audio.LoadEvents([]smaf.Event{
		{Time: 0, Type: smaf.EventNoteOn, Channel: 0, Note: uint8(note), Velocity: clampVolume(volume)},
		{Time: uint32(duration), Type: smaf.EventNoteOff, Channel: 0, Note: uint8(note)},
		{Time: uint32(duration), Type: smaf.EventEnd},
	})
	if err != nil {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
	}
	if err := audio.Play(handle, runtime.audioNow(), false); err != nil {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
	}
	return jvm.VoidValue(), nil
}

func clampVolume(volume int32) uint8 {
	switch {
	case volume <= 0:
		return 0
	case volume >= 100:
		return 127
	}
	return uint8(volume * 127 / 100)
}

// supportedContentTypes and supportedProtocols report exactly what this
// runtime can open, so a game that checks before creating a Player gets the
// same answer createPlayer would give it.
func (runtime *Runtime) supportedContentTypes(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return stringArray(vm, []string{"application/vnd.smaf", "audio/x-tone-seq"})
}

func (runtime *Runtime) supportedProtocols(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return stringArray(vm, []string{"device", "resource"})
}

func stringArray(vm *jvm.VM, values []string) (jvm.Value, error) {
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: jvm.StringClass}, int32(len(values)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	elements := make([]jvm.Value, len(values))
	for index, value := range values {
		elements[index] = jvm.ReferenceValue(vm.NewString(value))
	}
	if err := jvm.SetArrayRange(array, 0, elements); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}
