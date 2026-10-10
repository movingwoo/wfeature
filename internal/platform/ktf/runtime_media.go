package ktf

import (
	"fmt"
	"path"
	"strings"
	"weak"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// WIPI's audio API splits a sound across two objects. A Clip is the data — a
// media type plus the bytes, which arrive by constructor, by setBuffer, or a
// chunk at a time through putData — and Player is a set of static methods that
// start and stop one. Nothing decodes until Player is asked to play, because a
// game commonly builds every clip it will ever use at startup and plays a
// handful of them.
//
// The bytes live here rather than in a guest field because they are Host data:
// the guest never reads them back, and keeping them out of the guest heap
// keeps the object collector from having to walk a megabyte of audio.

const maxClipBufferBytes = 1 << 20

// clipState is one Clip's Host-side data.
type clipState struct {
	data []byte
	// handle is the loaded sound, claimed on first play. It is dropped
	// whenever the data changes, so a clip refilled through putData reloads
	// rather than replaying what it held before.
	handle backend.AudioHandle
	loaded bool
	// played records that the player has taken what the clip held. The
	// specification says a clip's data "shrinks as the media player plays it
	// and grows again through putData", so bytes that have been played are no
	// longer in the buffer for the next fill to append to. See
	// runtimeClipPutData.
	played bool
	// Only active/paused playback keeps the Clip alive. The listener is a
	// field on the Clip, so idle listener-to-Clip cycles remain collectible.
	owner     *jvm.Object
	completed uint64
}

func (runtime *initializationRuntime) clip(receiver *jvm.Object) *clipState {
	if runtime.clips == nil {
		runtime.clips = map[weak.Pointer[jvm.Object]]*clipState{}
	}
	key := weak.Make(receiver)
	state, ok := runtime.clips[key]
	if !ok {
		state = &clipState{}
		runtime.clips[key] = state
	}
	return state
}

// invalidateClip drops any loaded sound because the clip's bytes changed.
func (runtime *initializationRuntime) invalidateClip(state *clipState) {
	if state.loaded && runtime.client.audio != nil {
		_ = runtime.client.audio.Close(state.handle)
	}
	state.loaded = false
	state.owner, state.completed = nil, 0
}

func clipReceiver(arguments []jvm.Value, method string) (*jvm.Object, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("%s expected a receiver", method)
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, fmt.Errorf("%s receiver is null", method)
	}
	return receiver, nil
}

// runtimeClipConstructor covers all four Clip constructors. Beyond the media
// type they differ only in where the initial bytes come from: none, a byte
// array, a length to reserve, or a packaged resource to read.
func runtimeClipConstructor(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := clipReceiver(arguments, "Clip constructor")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("Clip constructor expected a media type")
	}
	state := runtime.clip(receiver)
	if state.loaded {
		if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
			return jvm.VoidValue(), err
		}
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["type:Ljava/lang/String;"] = arguments[1]
	receiver.Fields["position:I"] = jvm.IntValue(0)
	receiver.Fields["stopTime:I"] = jvm.IntValue(0)
	receiver.Fields["volume:I"] = jvm.IntValue(100)

	state.data = nil
	state.played = false
	runtime.invalidateClip(state)
	if len(arguments) < 3 {
		return jvm.VoidValue(), nil
	}

	switch source, err := arguments[2].Reference(); {
	case err == nil && source != nil:
		// Either the sound's bytes or the name of a packaged resource holding
		// them. A byte array answers a snapshot; a string does not.
		if name, ok := jvm.StringText(source); ok {
			data, resolveErr := runtime.clipResource(receiver, name)
			if resolveErr != nil {
				return jvm.VoidValue(), resolveErr
			}
			state.data = data
			return jvm.VoidValue(), nil
		}
		data, snapshotErr := byteArrayBytes(source)
		if snapshotErr != nil {
			return jvm.VoidValue(), fmt.Errorf("Clip constructor data: %w", snapshotErr)
		}
		state.data = data
	default:
		// The remaining form reserves a buffer length, which this runtime does
		// not need to honour: putData grows the buffer as bytes arrive.
		if _, intErr := arguments[2].Int32(); intErr != nil {
			return jvm.VoidValue(), fmt.Errorf("Clip constructor third argument is neither data nor a length")
		}
	}
	_ = vm
	return jvm.VoidValue(), nil
}

// clipResource reads a packaged resource named relative to the clip's class,
// the same resolution Class.getResourceAsStream applies.
//
// **A name the archive does not hold answers no bytes rather than a failure.**
// The specification gives `Clip(String type, String resourceName)` no declared
// exception, so there is no way to tell the guest that its resource is missing
// and no reason to believe a handset stopped the program over one. A title
// found this by building its whole sound set in `startApp` from a numbering
// its own archive is sparse in — twelve clips packaged in a row and then every
// third one — and the first gap ended the session before its first frame. An
// empty clip plays nothing, which is what a handset with no data for that
// number would have done, and the miss is logged so a silent game is still
// traceable to its cause.
func (runtime *initializationRuntime) clipResource(receiver *jvm.Object, name string) ([]byte, error) {
	resourceName := name
	if strings.HasPrefix(resourceName, "/") {
		resourceName = strings.TrimPrefix(resourceName, "/")
	} else if className := receiver.ClassName; className != "" {
		if packageEnd := strings.LastIndex(className, "/"); packageEnd >= 0 {
			resourceName = className[:packageEnd+1] + resourceName
		}
	}
	cleaned := path.Clean(resourceName)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return nil, fmt.Errorf("Clip resource %q is outside the package", name)
	}
	data, ok := runtime.client.resources[cleaned]
	runtime.countDiagnostic(fmt.Sprintf("clip resource %s found=%t", cleaned, ok))
	if !ok {
		runtime.client.log("KTF clip resource is not packaged", "name", name)
		return nil, nil
	}
	return append([]byte(nil), data...), nil
}

// runtimeClipPutData appends a chunk of sound data and answers how much it
// took, which is how a game streams a clip in through a fixed-size buffer.
func runtimeClipPutData(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := clipReceiver(arguments, "BaseClip.putData")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 4 {
		return jvm.VoidValue(), fmt.Errorf("BaseClip.putData expected data, offset, and length, got %d arguments", len(arguments))
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := arguments[3].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil || offset < 0 || length <= 0 {
		return jvm.IntValue(0), nil
	}
	data, err := byteArrayBytes(array)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("BaseClip.putData data: %w", err)
	}
	if int64(offset)+int64(length) > int64(len(data)) {
		return jvm.VoidValue(), fmt.Errorf("BaseClip.putData range [%d, %d) is outside a %d byte array", offset, offset+length, len(data))
	}

	state := runtime.clip(receiver)
	if state.loaded {
		if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
			return jvm.VoidValue(), err
		}
	}
	// **A fill after a play starts a new sound, not a longer one.** The
	// specification has a clip's contents shrink as the player plays them and
	// grow again through this call, so once the player has taken what the clip
	// held there is nothing left here to append to.
	//
	// Nothing enforced that, and a clip is a title's to reuse: one local title
	// keeps a single Clip for every sound it makes and its whole protocol is
	// `Player.stop`, `putData`, `Player.play`. Appending left the second
	// sound's bytes behind the first sound's, and a SMAF file followed by a
	// second SMAF file parses as the first one — so every sound after the
	// first played the first sound again. Its title music was its logo sting,
	// looping, for as long as the title was left running.
	if state.played {
		state.data = state.data[:0]
		state.played = false
		runtime.invalidateClip(state)
	}
	room := maxClipBufferBytes - len(state.data)
	if room <= 0 {
		return jvm.IntValue(0), nil
	}
	if int(length) > room {
		length = int32(room)
	}
	state.data = append(state.data, data[offset:offset+length]...)
	runtime.invalidateClip(state)
	return jvm.IntValue(length), nil
}

// runtimeClipSetBuffer replaces the clip's data outright.
func runtimeClipSetBuffer(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := setClipBuffer(runtime, vm, arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

// runtimeClipSetBufferChecked is the same call where the handset runtime
// reports whether the data was taken. It is false when the clip kept none of
// it, which is what a caller that branches on the result is asking about.
func runtimeClipSetBufferChecked(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	taken, err := setClipBuffer(runtime, vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	result := int32(0)
	if taken > 0 {
		result = 1
	}
	return jvm.IntValue(result), nil
}

// setClipBuffer replaces the clip's data and answers how many bytes it kept.
func setClipBuffer(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (int32, error) {
	receiver, err := clipReceiver(arguments, "Clip.setBuffer")
	if err != nil {
		return 0, err
	}
	state := runtime.clip(receiver)
	if state.loaded {
		if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
			return 0, err
		}
	}
	state.data = nil
	state.played = false
	runtime.invalidateClip(state)
	if len(arguments) < 3 {
		return 0, nil
	}
	// setBuffer(data, size) is putData(data, 0, size).
	taken, err := runtimeClipPutData(runtime, vm, []jvm.Value{arguments[0], arguments[1], jvm.IntValue(0), arguments[2]})
	if err != nil {
		return 0, err
	}
	return taken.Int32()
}

func runtimeClipAvailableData(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := clipReceiver(arguments, "BaseClip.availableDataSize")
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(maxClipBufferBytes - len(runtime.clip(receiver).data))), nil
}

func runtimeClipClearData(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := clipReceiver(arguments, "BaseClip.clearData")
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.clip(receiver)
	if state.loaded {
		if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
			return jvm.VoidValue(), err
		}
	}
	state.data = nil
	runtime.invalidateClip(state)
	return jvm.VoidValue(), nil
}

// runtimeClipSetPosition records a playback position and accepts it. Nothing
// seeks: this runtime plays a clip from its start, and the games here set a
// position only to rewind to zero.
func runtimeClipSetPosition(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := clipReceiver(arguments, "Clip.setPosition")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Clip.setPosition expected a position")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["position:I"] = arguments[1]
	return jvm.IntValue(1), nil
}

// runtimeClipSetVolume remembers a clamped percentage and applies it to an
// already loaded clip without changing the device or Host volume.
func runtimeClipSetVolume(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := clipReceiver(arguments, "Clip.setVolume")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Clip.setVolume expected a level")
	}
	level, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	level = min(max(level, 0), 100)
	state := runtime.clip(receiver)
	if state.loaded && runtime.client.audio != nil {
		if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
			return jvm.VoidValue(), err
		}
		if err := runtime.client.audio.SetSoundVolume(state.handle, int(level)); err != nil {
			runtime.countDiagnostic(fmt.Sprintf("clip volume failed: %v", err))
			return jvm.IntValue(0), nil
		}
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["volume:I"] = jvm.IntValue(level)
	return jvm.IntValue(1), nil
}

func runtimeVolumeSet(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Volume.set expected a level")
	}
	level, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.client.audio.SetVolume(int(level))
	return jvm.VoidValue(), nil
}

func runtimeVolumeGet(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 0 {
		return jvm.VoidValue(), fmt.Errorf("Volume.get expected no arguments")
	}
	return jvm.IntValue(int32(runtime.client.audio.Volume())), nil
}

// playerClipState resolves the Clip a Player call names. Player's methods are
// static, so the clip is the first argument rather than a receiver.
func playerClipState(runtime *initializationRuntime, arguments []jvm.Value, method string) (*clipState, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("%s expected a clip", method)
	}
	clip, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if clip == nil {
		return nil, nil
	}
	return runtime.clip(clip), nil
}

// runtimePlayerPlay decodes the clip if it has not been decoded and starts it.
// A clip holding data this build cannot decode answers false rather than
// failing the call: the game treats that as "no sound" and carries on.
func runtimePlayerPlay(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := playerClipState(runtime, arguments, "Player.play")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if state == nil || len(state.data) == 0 || runtime.client.audio == nil {
		return jvm.IntValue(0), nil
	}
	receiver, _ := arguments[0].Reference()
	now := runtime.guestElapsed()
	if err := runtime.syncClipCompletions(now); err != nil {
		return jvm.VoidValue(), err
	}
	repeat := false
	if len(arguments) >= 2 {
		value, err := arguments[1].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		repeat = value != 0
	}

	if !state.loaded {
		receiver, _ := arguments[0].Reference() // Validated by playerClipState.
		level := int32(100)
		if saved, ok := receiver.Fields["volume:I"]; ok {
			level, err = saved.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
		}
		handle, loadErr := runtime.client.audio.Load(state.data)
		if loadErr != nil {
			runtime.countDiagnostic(fmt.Sprintf("clip load failed: %v", loadErr))
			return jvm.IntValue(0), nil
		}
		if err := runtime.client.audio.SetSoundVolume(handle, int(level)); err != nil {
			_ = runtime.client.audio.Close(handle)
			runtime.countDiagnostic(fmt.Sprintf("clip volume failed: %v", err))
			return jvm.IntValue(0), nil
		}
		state.handle, state.loaded = handle, true
	}
	if err := runtime.client.audio.Play(state.handle, now, repeat); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("clip play failed: %v", err))
		return jvm.IntValue(0), nil
	}
	// The player has the bytes now. The clip keeps them so that playing it
	// again needs no refill, but a refill starts over; see runtimeClipPutData.
	state.played = true
	state.owner, state.completed = receiver, 0
	return jvm.IntValue(1), runtime.queueClipEvent(receiver, wipi.PlayEventStart)
}

func runtimePlayerStop(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := playerClipState(runtime, arguments, "Player.stop")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if state == nil || !state.loaded || runtime.client.audio == nil {
		return jvm.IntValue(0), nil
	}
	receiver, _ := arguments[0].Reference()
	if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
		return jvm.VoidValue(), err
	}
	active := runtime.client.audio.Playing(state.handle) || runtime.client.audio.Paused(state.handle)
	runtime.client.audio.Stop(state.handle)
	state.owner = nil
	if active {
		return jvm.IntValue(1), runtime.queueClipEvent(receiver, wipi.PlayEventStop)
	}
	return jvm.IntValue(1), nil
}

func runtimePlayerPause(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := playerClipState(runtime, arguments, "Player.pause")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if state == nil || !state.loaded || runtime.client.audio == nil {
		return jvm.IntValue(0), nil
	}
	receiver, _ := arguments[0].Reference()
	now := runtime.guestElapsed()
	if err := runtime.syncClipCompletions(now); err != nil {
		return jvm.VoidValue(), err
	}
	if !runtime.client.audio.Playing(state.handle) {
		return jvm.IntValue(0), nil
	}
	if err := runtime.client.audio.Pause(state.handle, now); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("clip pause failed: %v", err))
		return jvm.IntValue(0), nil
	}
	state.owner = receiver
	return jvm.IntValue(1), runtime.queueClipEvent(receiver, wipi.PlayEventPause)
}

func runtimePlayerResume(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	state, err := playerClipState(runtime, arguments, "Player.resume")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if state == nil || !state.loaded || runtime.client.audio == nil || !runtime.client.audio.Paused(state.handle) {
		return jvm.IntValue(0), nil
	}
	now := runtime.guestElapsed()
	if err := runtime.syncClipCompletions(now); err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.client.audio.Resume(state.handle, now); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("clip resume failed: %v", err))
		return jvm.IntValue(0), nil
	}
	receiver, _ := arguments[0].Reference()
	state.owner = receiver
	return jvm.IntValue(1), runtime.queueClipEvent(receiver, wipi.PlayEventResume)
}

// byteArrayBytes reads a guest byte array into Host bytes.
func byteArrayBytes(array *jvm.Object) ([]byte, error) {
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return nil, err
	}
	data := make([]byte, len(values))
	for index, value := range values {
		element, err := value.Int32()
		if err != nil {
			return nil, err
		}
		data[index] = byte(element)
	}
	return data, nil
}
