package ktf

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	maxClipEvents       = 4096
	clipListenerField   = "$playListener:Lorg/kwis/msp/media/PlayListener;"
	clipUpdateSignature = "(Lorg/kwis/msp/media/Clip;II)V"
)

type clipEvent struct {
	clip, listener *jvm.Object
	code           int32
}

func runtimePlayListenerDefinition() runtimeJavaClass {
	definition := runtimeInterfaceClass(runtimePlayListenerClass,
		runtimeJavaMethod{name: "playUpdate", descriptor: clipUpdateSignature})
	for _, event := range []struct {
		name string
		code int32
	}{
		{"ERROR", wipi.PlayEventError}, {"END_OF_DATA", wipi.PlayEventEndOfData},
		{"START", wipi.PlayEventStart}, {"STOP", wipi.PlayEventStop},
		{"PAUSE", wipi.PlayEventPause}, {"RESUME", wipi.PlayEventResume},
		{"RECORD", wipi.PlayEventRecord}, {"FULL_OF_DATA", wipi.PlayEventFullOfData},
	} {
		value := uint32(event.code)
		definition.fields = append(definition.fields, runtimeJavaField{
			name: event.name, descriptor: "I", accessFlags: 0x0019,
			initializer: func(*initializationRuntime) (uint32, error) { return value, nil },
		})
	}
	return definition
}

func clipListener(clip *jvm.Object) (*jvm.Object, error) {
	if clip == nil {
		return nil, nil
	}
	value, ok := clip.Fields[clipListenerField]
	if !ok {
		return nil, nil
	}
	listener, err := value.Reference()
	if err != nil {
		return nil, fmt.Errorf("KTF Clip listener field is not a reference")
	}
	return listener, nil
}

// KTF's JVM AOT metadata does not carry implemented interfaces. Validate the
// concrete callback and executable entry instead of rejecting valid AOT
// listeners using the bytecode-only interface declaration.
func (runtime *initializationRuntime) validateClipListener(listener *jvm.Object) error {
	if listener == nil {
		return nil
	}
	vm := runtime.client.vm
	address, bound := vm.AOTObjectAddress(listener)
	if !bound {
		if vm.IsInstance(listener, runtimePlayListenerClass) {
			return nil
		}
		return fmt.Errorf("KTF listener is not a PlayListener")
	}
	classAddress, err := runtime.aotObjectClassAddress(address)
	if err != nil {
		return err
	}
	metadata, ok := vm.AOTClassAt(classAddress)
	if !ok || metadata.Name != listener.ClassName {
		return fmt.Errorf("KTF listener has a different AOT class binding")
	}
	if err := runtime.validateAOTMediaType(classAddress, listener.ClassName, runtimePlayListenerClass); err != nil {
		return err
	}
	method, found, err := vm.FindAOTMethod(classAddress, "playUpdate", clipUpdateSignature)
	if err == nil && !found {
		method, found, err = runtime.aotMethodFromGuestRecords(classAddress, "playUpdate", clipUpdateSignature)
	}
	if err != nil || !found || method.AccessFlags&(jvm.AccessStatic|jvm.AccessAbstract) != 0 {
		return fmt.Errorf("KTF listener has no concrete playback callback")
	}
	entry := method.Body
	if method.AccessFlags&jvm.AccessNative != 0 {
		entry = method.NativeBody
	}
	width := uint64(4)
	if entry&1 != 0 {
		width = 2
	} else if entry&3 != 0 {
		return fmt.Errorf("KTF listener callback is unaligned")
	}
	if entry == 0 {
		return fmt.Errorf("KTF listener callback has no entry")
	}
	return runtime.client.core.Memory().ValidateRange(entry&^1, width, armcore.PermissionExecute)
}

// Read the guest records again: cached summaries in a checkpoint are not
// evidence that its callback owner actually implements the declared type.
//
// Only a walk that reads every record and never meets the target answers no,
// as checkAOTType does. An older relocatable module's implements list holds
// reference cells naming an interface rather than class records, and refusing
// a listener for that stopped one title's startApp at setListener. A record
// the walk cannot read leaves the answer open, and the caller's checks on the
// callback still apply.
func (runtime *initializationRuntime) validateAOTMediaType(address uint32, name, target string) error {
	pending, seen := []uint32{address}, map[uint32]bool{address: true}
	undecided := false
	for next := 0; next < len(pending) && next < maxAOTHierarchyDepth; next++ {
		summary, err := runtime.readAOTClassSummary(pending[next])
		if err != nil {
			if next == 0 {
				return err
			}
			undecided = true
			continue
		}
		if next == 0 && summary.name != name {
			return fmt.Errorf("KTF media guest class differs from its binding")
		}
		if summary.name == target {
			return nil
		}
		for index := -1; index < len(summary.interfaces); index++ {
			parent := summary.parent
			if index >= 0 {
				parent = summary.interfaces[index]
			}
			if parent != 0 && !seen[parent] {
				seen[parent] = true
				pending = append(pending, parent)
			}
		}
	}
	if undecided && len(pending) <= maxAOTHierarchyDepth {
		runtime.countDiagnostic("media type undecided for " + target)
		return nil
	}
	return fmt.Errorf("KTF media object does not implement %s within the class graph limit", target)
}

func (runtime *initializationRuntime) validateClipObject(object *jvm.Object, callback bool) error {
	if object == nil {
		return fmt.Errorf("KTF media Clip is null")
	}
	// Player's existing vendor overloads accept BaseClip, but the WIPI
	// listener callback specifically requires its Clip subclass.
	target := "org/kwis/msp/media/BaseClip"
	if callback || object.ClassName == wipi.ClipClass {
		target = wipi.ClipClass
	}
	vm := runtime.client.vm
	if address, bound := vm.AOTObjectAddress(object); bound {
		class, err := runtime.aotObjectClassAddress(address)
		if err != nil {
			return err
		}
		return runtime.validateAOTMediaType(class, object.ClassName, target)
	}
	if !vm.IsInstance(object, target) {
		return fmt.Errorf("KTF media owner is not a Clip")
	}
	return nil
}

func runtimeClipSetListener(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := clipReceiver(arguments, "Clip.setListener")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Clip.setListener expects one listener")
	}
	listener, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.validateClipListener(listener); err != nil {
		return jvm.VoidValue(), err
	}
	if clip.Fields == nil {
		clip.Fields = make(map[string]jvm.Value)
	}
	clip.Fields[clipListenerField] = jvm.ReferenceValue(listener)
	return jvm.VoidValue(), nil
}

func (runtime *initializationRuntime) queueClipEvent(clip *jvm.Object, code int32) error {
	listener, err := clipListener(clip)
	if err != nil || listener == nil {
		return err
	}
	if len(runtime.mediaEvents) >= maxClipEvents {
		return fmt.Errorf("KTF pending media event count exceeds %d", maxClipEvents)
	}
	runtime.mediaEvents = append(runtime.mediaEvents, clipEvent{clip, listener, code})
	return nil
}

func (runtime *initializationRuntime) syncClipPlayback(clip *jvm.Object, state *clipState) error {
	if !state.loaded || runtime.client.audio == nil {
		return nil
	}
	progress, err := runtime.client.audio.PlaybackState(state.handle)
	if err != nil {
		return err
	}
	if progress.Completed < state.completed {
		return fmt.Errorf("KTF Clip completion count moved backward")
	}
	listener, err := clipListener(clip)
	if err != nil {
		return err
	}
	if listener != nil {
		count := progress.Completed - state.completed
		if count > uint64(maxClipEvents-len(runtime.mediaEvents)) {
			return fmt.Errorf("KTF Clip completion notifications exceed limit")
		}
		for range count {
			runtime.mediaEvents = append(runtime.mediaEvents, clipEvent{clip, listener, wipi.PlayEventEndOfData})
		}
	}
	state.completed = progress.Completed
	if !progress.Playing && !progress.Paused {
		state.owner = nil
	}
	return nil
}

// Audio service and native calls run under client.run. Sort handles so
// callbacks from different owners have deterministic observation order.
func (runtime *initializationRuntime) syncClipCompletions(now time.Duration) error {
	if runtime.client.audio != nil {
		runtime.client.audio.Advance(now)
	}
	type loadedClip struct {
		object *jvm.Object
		state  *clipState
	}
	var clips []loadedClip
	for key, state := range runtime.clips {
		if state.loaded {
			clips = append(clips, loadedClip{key.Value(), state})
		}
	}
	slices.SortFunc(clips, func(a, b loadedClip) int { return cmp.Compare(a.state.handle, b.state.handle) })
	for _, clip := range clips {
		if err := runtime.syncClipPlayback(clip.object, clip.state); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *initializationRuntime) deliverClipEvent(event clipEvent) error {
	_, err := runtime.client.vm.InvokeVirtual(event.listener, "playUpdate", clipUpdateSignature,
		jvm.ReferenceValue(event.clip), jvm.IntValue(event.code), jvm.IntValue(0))
	return runtime.client.absorbUncaughtCallback("playUpdate", err)
}

// Media callbacks use the Host's event service even when a guest event loop
// consumes its own generic events. Report only work that service can advance;
// a paused score has no natural-end deadline.
func (runtime *initializationRuntime) mediaDeadlines(now time.Time, consider func(time.Time)) {
	if len(runtime.mediaEvents) != 0 {
		consider(now)
	}
	audio := runtime.client.audio
	if audio == nil {
		return
	}
	speed := runtime.client.speedOrDefault()
	age := max(time.Duration(0), now.Sub(runtime.clockBase))
	scaled := float64(age) * speed
	if scaled >= float64(math.MaxInt64) {
		consider(now)
		return
	}
	guestNow := time.Duration(scaled)
	for key, state := range runtime.clips {
		if !state.loaded {
			continue
		}
		listener, err := clipListener(key.Value())
		if err != nil {
			consider(now)
			continue
		}
		if listener == nil {
			continue
		}
		end, ok := audio.NextCompletion(state.handle)
		if !ok {
			if audio.Playing(state.handle) {
				consider(now) // A zero-length one-shot ends at its first advance.
			}
			continue
		}
		wait := math.Ceil(float64(max(time.Duration(0), end-guestNow)) / speed)
		if wait < float64(math.MaxInt64) {
			consider(now.Add(time.Duration(wait)))
		}
	}
}
