package skt

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"weak"

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
	mu            sync.Mutex
	state         int32
	contentType   string
	handle        backend.AudioHandle
	duration      time.Duration
	loops         int32
	mediaTime     int64
	completed     uint64
	listeners     []*jvm.Object
	volumeControl *jvm.Object
	wipiClip      *jvm.Object
	wipiOwner     weak.Pointer[jvm.Object]
}

// AttachAudioSink supplies the Host audio sink MIDP players play through.
// Without one the players still run their state machine and report their
// events, so a game that waits for STARTED is not stuck; it simply makes no
// sound.
//
// **The timeline exists whether or not a Host is listening.** It used to be
// made here, so a run with no speaker had no timeline at all and every audio
// call was a no-op: nothing decoded, no clip had a length, and a title's sound
// behaved differently under the CLI than under the browser — which is where a
// title that waits on its own music went unnoticed. A sink attached later swaps
// into the timeline that is already there, keeping the clips a title loaded
// while it was starting up.
func (runtime *Runtime) AttachAudioSink(sink backend.AudioSink) {
	if runtime == nil {
		return
	}
	runtime.audioTimeline().SetSink(sink)
}

// AdvanceAudio moves the audio timeline to now. A Host with a frame loop calls
// it once per frame; a Host that only starts a MIDlet and reads its state
// never has to.
//
// **The reading is the runtime's own, not the Host's.** It used to be an
// argument, and the two Hosts passed two different clocks while the instant a
// sound *started* from came from a third — an absolute wall-clock stamp, which
// is decades ahead of either. A sound therefore began far in the future and no
// event was ever due: this platform has never emitted a note through a sink,
// on either Host. The other two WIPI runtimes advance from their own guest
// clock and always did.
func (runtime *Runtime) AdvanceAudio() {
	if runtime == nil {
		return
	}
	runtime.dispatchMu.Lock()
	defer runtime.dispatchMu.Unlock()
	// Merge Players, raw clips and tones at one common boundary. Release the
	// timeline before taking any Player lock; guest transitions take those
	// locks in the opposite order and reconcile any newer progress themselves.
	runtime.audioTimelineMu.Lock()
	runtime.audioTimeline().Advance(runtime.GuestElapsed())
	runtime.audioTimelineMu.Unlock()
	for _, object := range runtime.mediaPlayerSnapshot() {
		player := object.Native.(*playerData)
		runtime.lockPlayerAudio(player)
		err := runtime.observePlayerLocked(object, player)
		runtime.unlockPlayerAudio(player)
		if err != nil {
			_ = runtime.fail("advance Player", err)
			return
		}
	}
}

// Player state and its clock-dependent backend operations form one transition.
// Global score advancement may finish a different owner, so serialize the whole
// query-and-mutate interval across Players and raw clips. Never hold mediaMu or
// audioTimelineMu while acquiring a Player lock.
func (runtime *Runtime) lockPlayerAudio(player *playerData) {
	player.mu.Lock()
	runtime.audioTimelineMu.Lock()
}

func (runtime *Runtime) unlockPlayerAudio(player *playerData) {
	runtime.audioTimelineMu.Unlock()
	player.mu.Unlock()
}

func (runtime *Runtime) audioTimeline() *backend.Audio {
	if runtime == nil {
		return nil
	}
	runtime.audioMu.Lock()
	defer runtime.audioMu.Unlock()
	if runtime.audio == nil {
		runtime.audio = backend.NewAudio(nil)
		runtime.audio.SetLogger(runtime.logger)
		_ = runtime.audio.SetPlaybackRate(0, runtime.Speed())
		// Match the handset level exposed by the existing SKVM getter.
		runtime.audio.SetVolume(50)
	}
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
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "media stream is null")
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
	reference, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if reference == nil {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "media locator is null")
	}
	locator, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch {
	case locator == midp.ToneDeviceLocator:
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, "tone sequence playback is not supported")
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
	if strings.EqualFold(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]), "audio/x-tone-seq") {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, "tone sequence playback is not supported")
	}
	if len(data) == 0 {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, "media content is empty")
	}
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
	object := &jvm.Object{
		ClassName: midp.PlayerClass,
		Fields:    make(map[string]jvm.Value),
		Native:    player,
	}
	if err := runtime.registerMediaPlayer(player.handle, object); err != nil {
		_ = runtime.audioTimeline().Close(player.handle)
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
	}
	return jvm.ReferenceValue(object), nil
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
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
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
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	now := runtime.audioNow()
	if err := runtime.syncPlayerAtLocked(object, player, now); err != nil {
		return jvm.VoidValue(), err
	}
	if player.state == playerStarted {
		return jvm.VoidValue(), nil
	}
	audio := runtime.audioTimeline()
	if audio.Paused(player.handle) {
		err = audio.Resume(player.handle, now)
	} else {
		err = audio.PlayCount(player.handle, now, player.loops)
		if err == nil {
			player.completed, player.mediaTime = 0, 0
		}
	}
	if err != nil {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
	}
	player.state = playerStarted
	return jvm.VoidValue(), runtime.queuePlayerTimeLocked(object, player, midp.PlayerEventStarted, player.mediaTime)
}

// audioNow is the timeline reading a sound starts at, and it is the same clock
// AdvanceAudio moves the timeline along — the MIDlet's own elapsed time. The
// wall clock RMS stamps its records with is a different question with a
// different answer, and using it here is what silenced this platform.
func (runtime *Runtime) audioNow() time.Duration {
	return runtime.GuestElapsed()
}

func (runtime *Runtime) playerStop(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.stopPlayer(arguments, playerPrefetched)
}

func (runtime *Runtime) playerDeallocate(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.stopPlayer(arguments, playerRealized)
}

func (runtime *Runtime) stopPlayer(arguments []jvm.Value, target int32) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	now := runtime.audioNow()
	if err := runtime.syncPlayerAtLocked(object, player, now); err != nil {
		return jvm.VoidValue(), err
	}
	started := player.state == playerStarted
	if started {
		if err := runtime.audioTimeline().Pause(player.handle, now); err != nil {
			return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
		}
	}
	if player.state > target {
		player.state = target
	}
	if started {
		return jvm.VoidValue(), runtime.queuePlayerTimeLocked(object, player, midp.PlayerEventStopped, player.mediaTime)
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) playerClose(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), nil
	}
	if err := runtime.syncPlayerLocked(object, player); err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.audioTimeline().Close(player.handle); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.unregisterMediaPlayer(player.handle)
	player.state, player.handle = playerClosed, 0
	player.wipiClip = nil
	return jvm.VoidValue(), runtime.queuePlayerEventLocked(object, player, midp.PlayerEventClosed, nil)
}

func (runtime *Runtime) playerState(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if err := runtime.syncPlayerLocked(object, player); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(player.state), nil
}

func (runtime *Runtime) playerDuration(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	return jvm.LongValue(player.duration.Microseconds()), nil
}

func (runtime *Runtime) playerMediaTime(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if err := runtime.syncPlayerLocked(object, player); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.LongValue(player.mediaTime), nil
}

func (runtime *Runtime) setPlayerMediaTime(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	position, err := arguments[1].Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state < playerRealized {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is not realized")
	}
	// Negative positions clamp to zero. Other seeks remain unsupported;
	// reporting success would claim a position the output has not reached.
	if position > 0 {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, "only media time 0 can be set")
	}
	now := runtime.audioNow()
	if err := runtime.syncPlayerAtLocked(object, player, now); err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.audioTimeline().Rewind(player.handle, now); err != nil {
		return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
	}
	player.mediaTime = 0
	return jvm.LongValue(0), nil
}

func (runtime *Runtime) setPlayerLoopCount(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	count, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if count == 0 || count < -1 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid loop count")
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if err := runtime.syncPlayerLocked(object, player); err != nil {
		return jvm.VoidValue(), err
	}
	if player.state == playerStarted || player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is started or closed")
	}
	if audio := runtime.audioTimeline(); audio.Paused(player.handle) {
		if err := audio.SetLoopCount(player.handle, count); err != nil {
			return jvm.VoidValue(), newGuestException(midp.MediaExceptionClass, err.Error())
		}
	}
	player.loops = count
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) playerContentType(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state < playerRealized {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is not realized")
	}
	return jvm.ReferenceValue(vm.NewString(player.contentType)), nil
}

func (runtime *Runtime) addPlayerListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if listener == nil {
		return jvm.VoidValue(), nil
	}
	if err := runtime.syncPlayerLocked(object, player); err != nil {
		return jvm.VoidValue(), err
	}
	for _, existing := range player.listeners {
		if existing == listener {
			return jvm.VoidValue(), nil
		}
	}
	if len(player.listeners) >= maxPlayerListeners {
		return jvm.VoidValue(), fmt.Errorf("media player listener count exceeds %d", maxPlayerListeners)
	}
	player.listeners = append(player.listeners, listener)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) removePlayerListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if err := runtime.syncPlayerLocked(object, player); err != nil {
		return jvm.VoidValue(), err
	}
	remaining := player.listeners[:0]
	for _, existing := range player.listeners {
		if existing != listener {
			remaining = append(remaining, existing)
		}
	}
	player.listeners = remaining
	return jvm.VoidValue(), nil
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
	if note < 0 || note > 127 || duration <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("tone note %d duration %d", note, duration))
	}
	runtime.audioTimelineMu.Lock()
	defer runtime.audioTimelineMu.Unlock()
	audio := runtime.audioTimeline()
	if audio == nil {
		return jvm.VoidValue(), nil
	}
	err = audio.PlayTransient([]smaf.Event{
		{Time: 0, Type: smaf.EventNoteOn, Channel: 0, Note: uint8(note), Velocity: clampVolume(volume)},
		{Time: uint32(duration), Type: smaf.EventNoteOff, Channel: 0, Note: uint8(note)},
		{Time: uint32(duration), Type: smaf.EventEnd},
	}, runtime.audioNow())
	if err != nil {
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
func (runtime *Runtime) supportedContentTypes(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	protocol, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if protocol != nil {
		name, err := stringArgument(arguments, 0)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if !strings.EqualFold(name, "resource") {
			return stringArray(vm, nil)
		}
	}
	return stringArray(vm, []string{"application/vnd.smaf"})
}

func (runtime *Runtime) supportedProtocols(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	contentType, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if contentType != nil {
		name, err := stringArgument(arguments, 0)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if !strings.EqualFold(name, "application/vnd.smaf") {
			return stringArray(vm, nil)
		}
	}
	return stringArray(vm, []string{"resource"})
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
