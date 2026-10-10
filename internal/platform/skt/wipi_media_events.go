package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The Player lock protects its associated Clip's listener too. Taking the
// Clip volume lock here would reverse the established Clip -> Player order.
func (player *playerData) wipiClipObjectLocked() *jvm.Object {
	if player.wipiClip == nil {
		return player.wipiOwner.Value()
	}
	return player.wipiClip
}

func (player *playerData) wipiListenerLocked() *jvm.Object {
	object := player.wipiClipObjectLocked()
	if object == nil {
		return nil
	}
	clip, ok := object.Native.(*wipiClipData)
	if !ok || clip == nil {
		return nil
	}
	return clip.listener
}

// WIPI uses one recipient and integer events, but shares the deferred queue
// and bound with MIDP. Replacement/removal cannot rewrite an earlier event.
func (runtime *Runtime) queueWIPIEventLocked(object *jvm.Object, player *playerData, event int32) error {
	listener := player.wipiListenerLocked()
	if listener == nil {
		return nil
	}
	runtime.mediaMu.Lock()
	defer runtime.mediaMu.Unlock()
	if len(runtime.mediaEvents) >= maxPlayerEvents {
		return fmt.Errorf("pending media event count exceeds %d", maxPlayerEvents)
	}
	runtime.mediaEvents = append(runtime.mediaEvents, playerEvent{
		Player: object, Name: wipiPlaybackNames[event], Data: player.wipiClipObjectLocked(),
		Listeners: []*jvm.Object{listener},
	})
	return nil
}

func (runtime *Runtime) deliverWIPIEvent(event playerEvent, code int32) error {
	if len(event.Listeners) != 1 || event.Listeners[0] == nil {
		return fmt.Errorf("WIPI media event has an invalid recipient")
	}
	listener := event.Listeners[0]
	_, err := runtime.VM.InvokeVirtual(listener, "playUpdate", "(Lorg/kwis/msp/media/Clip;II)V",
		jvm.ReferenceValue(event.Data), jvm.IntValue(code), jvm.IntValue(0))
	if absorbed := runtime.absorbUncaughtCallback("playUpdate "+listener.ClassName, err); absorbed != nil {
		return fmt.Errorf("deliver playUpdate %d: %w", code, absorbed)
	}
	return nil
}

// Private names reuse the existing strict checkpoint event shape. The data
// object is the Clip; these playback notifications have no additional parm.
var wipiPlaybackNames = [...]string{
	wipi.PlayEventEndOfData: "wipi.endOfData",
	wipi.PlayEventStart:     "wipi.start",
	wipi.PlayEventStop:      "wipi.stop",
	wipi.PlayEventPause:     "wipi.pause",
	wipi.PlayEventResume:    "wipi.resume",
}

func wipiPlaybackEvent(name string) (int32, bool) {
	for code := wipi.PlayEventEndOfData; code <= wipi.PlayEventResume; code++ {
		if name == wipiPlaybackNames[code] {
			return code, true
		}
	}
	return 0, false
}

// Called at a parked capture barrier or after detached heap fixups. No guest
// code runs while validating the native ownership cycle and listener type.
func (runtime *Runtime) validateCheckpointWIPIClip(clip *wipiClipData) error {
	if clip == nil || clip.object == nil || clip.object.Native != clip || !runtime.VM.IsInstance(clip.object, wipi.ClipClass) ||
		clip.listener != nil && !runtime.VM.IsInstance(clip.listener, wipi.PlayListenerClass) {
		return fmt.Errorf("SKT checkpoint WIPI Clip identity or listener is invalid")
	}
	if clip.player == nil {
		return nil
	}
	player, err := checkpointNative[playerData](clip.player)
	if err != nil || player == nil || clip.player.ClassName != midp.PlayerClass || player.wipiClipObjectLocked() != clip.object {
		return fmt.Errorf("SKT checkpoint WIPI Clip has a different Player owner")
	}
	return nil
}
