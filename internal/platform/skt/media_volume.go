package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Each control retains its Player, including when the guest keeps only the
// control. The reverse reference gives repeated queries one stable identity.
type volumeControlData struct{ player *jvm.Object }

func playerVolumeControl(object *jvm.Object, player *playerData) *jvm.Object {
	// The caller holds player.mu.
	if player.volumeControl == nil {
		player.volumeControl = &jvm.Object{ClassName: midp.RuntimeVolumeControlClass,
			Fields: make(map[string]jvm.Value), Native: &volumeControlData{player: object}}
	}
	return player.volumeControl
}

func (runtime *Runtime) playerControl(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state < playerRealized {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is not realized")
	}
	name, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if name == nil {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "control name is null")
	}
	typeName, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if typeName != "VolumeControl" && typeName != "javax.microedition.media.control.VolumeControl" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(playerVolumeControl(object, player)), nil
}

func (runtime *Runtime) playerControls(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, player, err := playerArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state < playerRealized {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is not realized")
	}
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: midp.ControlClass}, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := jvm.SetArrayRange(array, 0, []jvm.Value{jvm.ReferenceValue(playerVolumeControl(object, player))}); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

func volumeControlArgument(arguments []jvm.Value) (*jvm.Object, *jvm.Object, *playerData, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	if object == nil {
		return nil, nil, nil, newGuestException("java/lang/NullPointerException", "VolumeControl is null")
	}
	control, ok := object.Native.(*volumeControlData)
	if object.ClassName != midp.RuntimeVolumeControlClass || !ok || control == nil {
		return nil, nil, nil, fmt.Errorf("receiver is not a VolumeControl")
	}
	owner, player, err := playerArgument([]jvm.Value{jvm.ReferenceValue(control.player)}, 0)
	return object, owner, player, err
}

func (runtime *Runtime) playerVolumeLevel(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, _, player, err := volumeControlArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	level, _, err := runtime.audioTimeline().SoundVolume(player.handle)
	return jvm.IntValue(int32(level)), err
}

func (runtime *Runtime) playerVolumeMuted(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, _, player, err := volumeControlArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.lockPlayerAudio(player)
	defer runtime.unlockPlayerAudio(player)
	if player.state == playerClosed {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	_, muted, err := runtime.audioTimeline().SoundVolume(player.handle)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if muted {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) setPlayerVolumeLevel(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	control, owner, player, err := volumeControlArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	level, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	level = min(max(level, 0), 100)
	runtime.lockPlayerAudio(player)
	if player.state == playerClosed {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if err := runtime.syncPlayerLocked(owner, player); err != nil {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), err
	}
	previous, _, err := runtime.audioTimeline().SoundVolume(player.handle)
	if err != nil {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), err
	}
	if previous == int(level) {
		runtime.unlockPlayerAudio(player)
		return jvm.IntValue(level), nil
	}
	if err := runtime.audioTimeline().SetSoundVolume(player.handle, int(level)); err != nil {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), err
	}
	err = runtime.queuePlayerEventLocked(owner, player, midp.PlayerEventVolumeChanged, control)
	runtime.unlockPlayerAudio(player)
	return jvm.IntValue(level), err
}

func (runtime *Runtime) setPlayerVolumeMute(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	control, owner, player, err := volumeControlArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	muted := value != 0
	runtime.lockPlayerAudio(player)
	if player.state == playerClosed {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "Player is closed")
	}
	if err := runtime.syncPlayerLocked(owner, player); err != nil {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), err
	}
	_, previous, err := runtime.audioTimeline().SoundVolume(player.handle)
	if err != nil {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), err
	}
	if previous == muted {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), nil
	}
	if err := runtime.audioTimeline().SetSoundMuted(player.handle, muted); err != nil {
		runtime.unlockPlayerAudio(player)
		return jvm.VoidValue(), err
	}
	err = runtime.queuePlayerEventLocked(owner, player, midp.PlayerEventVolumeChanged, control)
	runtime.unlockPlayerAudio(player)
	return jvm.VoidValue(), err
}
