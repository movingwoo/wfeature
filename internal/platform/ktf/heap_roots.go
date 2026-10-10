package ktf

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"time"
	"weak"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const maxHeapRootRecords = 1 << 18

// References are one-based positions in HeapState.Roots, with zero for null.
// Caller roots keep their original order; platform roots follow them and are
// adopted by their owners instead of being returned as additional caller roots.
type runtimeHeapRoots struct {
	CallerCount                         uint32
	DockedCard, ActiveJlet, FocusedText uint32
	CInputCard                          uint32
	RelaySocket                         uint32
	RelayOnline                         bool
	FocusedEditor                       uint32
	DisplayCards, PendingThreads        []uint32
	PendingSerial, JletListeners        []uint32
	Objects                             []heapNamedRoot
	GrabbedKeys                         []heapKeyRoot
	SerialPaintOwners                   []heapRootPair
	KFCKeys                             []heapKFCKey
	Timers                              []heapTimer
	Events                              [][4]int32
	SerialDue                           heapDeadline
	Clips                               []heapClip
	ImageSurfaces                       []heapSurface
}
type heapNamedRoot struct {
	Name []byte
	Root uint32
}
type heapKeyRoot struct {
	Key  int32
	Root uint32
}
type heapRootPair struct{ Owner, Target uint32 }
type heapKFCKey struct {
	Key                            int32
	Form, Field, Listener, Data    uint32
	Visibility, Children, Revision jvm.HeapValueState
	ListenerValue, DataValue       jvm.HeapValueState
	HasListener, HasData           bool
}
type heapTimer struct {
	Card, Task, Owner        uint32
	Pointer, Callback, Param uint32
	Delay                    uint64
	Due                      heapDeadline
	Period                   time.Duration
}
type heapDeadline struct {
	Set       bool
	Remaining time.Duration
}
type heapClip struct {
	Owner          uint32
	Data           []byte
	Handle         backend.AudioHandle
	Loaded, Played bool
}
type heapSurface struct{ Owner, Handle uint32 }

func captureHeapDeadline(instant, now time.Time) heapDeadline {
	if instant.IsZero() {
		return heapDeadline{}
	}
	return heapDeadline{Set: true, Remaining: instant.Sub(now)}
}
func (deadline heapDeadline) restore(now time.Time) time.Time {
	if !deadline.Set {
		return time.Time{}
	}
	return now.Add(deadline.Remaining)
}

type heapRootCapture struct {
	objects []*jvm.Object
	ids     map[*jvm.Object]uint32
	err     error
}

func (roots *heapRootCapture) add(object *jvm.Object) uint32 {
	if object == nil || roots.err != nil {
		return 0
	}
	if id := roots.ids[object]; id != 0 {
		return id
	}
	if len(roots.objects) >= maxHeapRootRecords {
		roots.err = fmt.Errorf("KTF platform heap roots exceed limit")
		return 0
	}
	root := uint32(len(roots.objects) + 1)
	roots.ids[object] = root
	roots.objects = append(roots.objects, object)
	return root
}
func (roots *heapRootCapture) list(objects []*jvm.Object) []uint32 {
	result := make([]uint32, len(objects))
	for i, object := range objects {
		result[i] = roots.add(object)
	}
	return result
}
func (roots *heapRootCapture) value(value jvm.Value) jvm.HeapValueState {
	record := jvm.HeapValueState{Kind: value.Kind()}
	switch value.Kind() {
	case jvm.ValueVoid:
	case jvm.ValueInt:
		v, _ := value.Int32()
		record.Bits = uint64(uint32(v))
	case jvm.ValueLong:
		v, _ := value.Int64()
		record.Bits = uint64(v)
	case jvm.ValueFloat:
		v, _ := value.Float32()
		record.Bits = uint64(math.Float32bits(v))
	case jvm.ValueDouble:
		v, _ := value.Float64()
		record.Bits = math.Float64bits(v)
	case jvm.ValueReference:
		v, _ := value.Reference()
		record.Reference = roots.add(v)
	default:
		roots.err = fmt.Errorf("KTF platform root has an unsupported JVM value")
	}
	return record
}

func (runtime *initializationRuntime) capturePlatformRoots(callers []*jvm.Object, context *heapNativeContext) (runtimeHeapRoots, []*jvm.Object, error) {
	now := context.now
	if runtime.activeSerialPaint != nil {
		return runtimeHeapRoots{}, nil, fmt.Errorf("KTF serial callback is active")
	}
	var cost uint64
	for _, count := range []int{len(callers), len(runtime.displayCards), len(runtime.pendingThreads), len(runtime.pendingSerial), len(runtime.jletListeners), len(runtime.runtimeObjects), len(runtime.grabbedKeys), len(runtime.serialPaintOwners), len(runtime.kfcOwnedKeys), len(runtime.clips), len(runtime.imageSurfaces)} {
		if count > maxHeapRootRecords {
			return runtimeHeapRoots{}, nil, fmt.Errorf("KTF platform root records exceed limit")
		}
		cost += uint64(count) * 128
	}
	if cost > maxHeapStorageBytes {
		return runtimeHeapRoots{}, nil, fmt.Errorf("KTF platform root records exceed data limit")
	}
	if len(runtime.pendingTimers) > maxPendingTimers || len(runtime.events) > maxQueuedEvents {
		return runtimeHeapRoots{}, nil, fmt.Errorf("KTF platform timer or event queue exceeds limit")
	}
	roots := &heapRootCapture{objects: slices.Clone(callers), ids: make(map[*jvm.Object]uint32)}
	for i, object := range callers {
		if object != nil && roots.ids[object] == 0 {
			roots.ids[object] = uint32(i + 1)
		}
	}
	saved := runtimeHeapRoots{CallerCount: uint32(len(callers)), DockedCard: roots.add(runtime.dockedCard), ActiveJlet: roots.add(runtime.activeJlet), CInputCard: roots.add(runtime.cInput.card),
		DisplayCards: roots.list(runtime.displayCards), PendingThreads: roots.list(runtime.pendingThreads), PendingSerial: roots.list(runtime.pendingSerial), JletListeners: roots.list(runtime.jletListeners), SerialDue: captureHeapDeadline(runtime.serialDueAt, now)}
	saved.RelayOnline = runtime.relayOnline
	if runtime.relaySocket != nil {
		// This temporary carrier roots the native socket even when no Java
		// wrapper or stream exists. It has no guest identity or ARM allocation;
		// adoption retains only its native payload, shared with any streams.
		saved.RelaySocket = roots.add(&jvm.Object{ClassName: jvm.ObjectClass, Native: runtime.relaySocket})
	}
	runtime.client.textMu.Lock()
	saved.FocusedText = roots.add(runtime.client.focusedText)
	var err error
	saved.FocusedEditor, err = context.captureEditor(runtime.client.textEditor)
	runtime.client.textMu.Unlock()
	if err != nil {
		return runtimeHeapRoots{}, nil, err
	}
	for name, object := range runtime.runtimeObjects {
		if name == heapMediaRoot {
			return runtimeHeapRoots{}, nil, fmt.Errorf("KTF runtime object uses the reserved media root")
		}
		cost += uint64(len(name))
		if len(name) > 65535 || cost > maxHeapStorageBytes {
			return runtimeHeapRoots{}, nil, fmt.Errorf("KTF runtime object name exceeds limit")
		}
		saved.Objects = append(saved.Objects, heapNamedRoot{Name: []byte(name), Root: roots.add(object)})
	}
	media, err := runtime.captureMediaRoots()
	if err != nil {
		return runtimeHeapRoots{}, nil, err
	}
	if media != nil {
		saved.Objects = append(saved.Objects, heapNamedRoot{Name: []byte(heapMediaRoot), Root: roots.add(media)})
	}
	for key, object := range runtime.grabbedKeys {
		saved.GrabbedKeys = append(saved.GrabbedKeys, heapKeyRoot{Key: key, Root: roots.add(object)})
	}
	for owner, target := range runtime.serialPaintOwners {
		saved.SerialPaintOwners = append(saved.SerialPaintOwners, heapRootPair{Owner: roots.add(owner), Target: roots.add(target)})
	}
	for key, owner := range runtime.kfcOwnedKeys {
		saved.KFCKeys = append(saved.KFCKeys, heapKFCKey{Key: key, Form: roots.add(owner.form), Field: roots.add(owner.field), Listener: roots.add(owner.event.listener), Data: roots.add(owner.event.data),
			Visibility: roots.value(owner.visibilityRevision), Children: roots.value(owner.childrenRevision), Revision: roots.value(owner.event.revision), ListenerValue: roots.value(owner.event.listenerValue), DataValue: roots.value(owner.event.dataValue), HasListener: owner.event.hasListener, HasData: owner.event.hasData})
	}
	for _, timer := range runtime.pendingTimers {
		saved.Timers = append(saved.Timers, heapTimer{Card: roots.add(timer.paintedCard), Task: roots.add(timer.task), Owner: roots.add(timer.owner), Pointer: timer.pointer, Callback: timer.callback, Param: timer.param, Delay: timer.delay, Due: captureHeapDeadline(timer.due, now), Period: timer.period})
	}
	for _, event := range runtime.events {
		saved.Events = append(saved.Events, [4]int32{event.kind, event.param1, event.param2, event.param3})
	}
	seenClips := make(map[*clipState]bool)
	for owner, clip := range runtime.clips {
		object := owner.Value()
		if object == nil || clip == nil || seenClips[clip] {
			return runtimeHeapRoots{}, nil, fmt.Errorf("KTF clip ownership requires collection or has unsupported sharing")
		}
		seenClips[clip] = true
		cost += uint64(len(clip.data))
		if len(clip.data) > maxClipBufferBytes || cost > maxHeapStorageBytes {
			return runtimeHeapRoots{}, nil, fmt.Errorf("KTF clip data exceeds heap limits")
		}
		saved.Clips = append(saved.Clips, heapClip{Owner: roots.add(object), Data: bytes.Clone(clip.data), Handle: clip.handle, Loaded: clip.loaded, Played: clip.played})
	}
	for owner, handle := range runtime.imageSurfaces {
		object := owner.Value()
		if object == nil {
			return runtimeHeapRoots{}, nil, fmt.Errorf("KTF image ownership requires collection")
		}
		saved.ImageSurfaces = append(saved.ImageSurfaces, heapSurface{Owner: roots.add(object), Handle: handle})
	}
	if roots.err != nil {
		return runtimeHeapRoots{}, nil, roots.err
	}
	// The same validation applies to live capture and untrusted restoration.
	identities := make([]uint32, len(roots.objects))
	for i, object := range roots.objects {
		identities[i] = roots.ids[object]
	}
	if err := saved.validate(identities); err != nil {
		return runtimeHeapRoots{}, nil, err
	}
	return saved, roots.objects, nil
}

func (saved runtimeHeapRoots) validate(rootIDs []uint32) error {
	rootCount := len(rootIDs)
	if rootCount > maxHeapRootRecords || uint64(saved.CallerCount) > uint64(rootCount) || len(saved.Timers) > maxPendingTimers || len(saved.Events) > maxQueuedEvents {
		return fmt.Errorf("KTF platform root count is invalid")
	}
	var cost uint64
	for _, count := range []int{len(saved.DisplayCards), len(saved.PendingThreads), len(saved.PendingSerial), len(saved.JletListeners), len(saved.Objects), len(saved.GrabbedKeys), len(saved.SerialPaintOwners), len(saved.KFCKeys), len(saved.Clips), len(saved.ImageSurfaces)} {
		if count > maxHeapRootRecords {
			return fmt.Errorf("KTF platform root records exceed limit")
		}
		cost += uint64(count) * 128
	}
	if cost > maxHeapStorageBytes {
		return fmt.Errorf("KTF platform root records exceed data limit")
	}
	identity := func(root uint32) uint32 {
		if root == 0 || uint64(root) > uint64(rootCount) {
			return 0
		}
		return rootIDs[root-1]
	}
	refs := []uint32{saved.DockedCard, saved.ActiveJlet, saved.FocusedText, saved.CInputCard, saved.RelaySocket}
	for _, list := range [][]uint32{saved.DisplayCards, saved.PendingThreads, saved.PendingSerial, saved.JletListeners} {
		refs = append(refs, list...)
	}
	checkValue := func(value jvm.HeapValueState) bool {
		refs = append(refs, value.Reference)
		if value.Kind > jvm.ValueReference {
			return false
		}
		if value.Kind == jvm.ValueVoid || value.Kind == jvm.ValueReference {
			return value.Bits == 0 && (value.Kind == jvm.ValueReference || value.Reference == 0)
		}
		return value.Reference == 0 && (value.Kind != jvm.ValueInt && value.Kind != jvm.ValueFloat || value.Bits <= 1<<32-1)
	}
	names := make(map[string]bool)
	for _, binding := range saved.Objects {
		name := string(binding.Name)
		if len(name) > 65535 || names[name] {
			return fmt.Errorf("KTF runtime object names are invalid or duplicated")
		}
		names[name] = true
		cost += uint64(len(name))
		refs = append(refs, binding.Root)
	}
	keys := make(map[int32]bool)
	for _, binding := range saved.GrabbedKeys {
		if keys[binding.Key] {
			return fmt.Errorf("KTF grabbed key is duplicated")
		}
		keys[binding.Key] = true
		refs = append(refs, binding.Root)
	}
	keys = make(map[int32]bool)
	for _, binding := range saved.KFCKeys {
		if keys[binding.Key] {
			return fmt.Errorf("KTF owned key is duplicated")
		}
		keys[binding.Key] = true
		refs = append(refs, binding.Form, binding.Field, binding.Listener, binding.Data)
		for _, value := range []jvm.HeapValueState{binding.Visibility, binding.Children, binding.Revision, binding.ListenerValue, binding.DataValue} {
			if !checkValue(value) {
				return fmt.Errorf("KTF owned key has an invalid JVM value")
			}
		}
	}
	owners := make(map[uint32]bool)
	for _, binding := range saved.SerialPaintOwners {
		owner := identity(binding.Owner)
		if owner == 0 || owners[owner] {
			return fmt.Errorf("KTF serial paint owner is null or duplicated")
		}
		owners[owner] = true
		refs = append(refs, binding.Owner, binding.Target)
	}
	for _, timer := range saved.Timers {
		if timer.Delay > maxTimerDelayMillis || timer.Period < 0 || timer.Period > time.Hour || !timer.Due.Set && timer.Due.Remaining != 0 {
			return fmt.Errorf("KTF timer delay is invalid")
		}
		refs = append(refs, timer.Card, timer.Task, timer.Owner)
	}
	owners = make(map[uint32]bool)
	for _, clip := range saved.Clips {
		owner := identity(clip.Owner)
		if owner == 0 || owners[owner] || len(clip.Data) > maxClipBufferBytes || clip.Loaded && clip.Handle == 0 {
			return fmt.Errorf("KTF clip owner or data is invalid")
		}
		owners[owner] = true
		refs = append(refs, clip.Owner)
		cost += uint64(len(clip.Data))
	}
	owners = make(map[uint32]bool)
	for _, surface := range saved.ImageSurfaces {
		owner := identity(surface.Owner)
		if owner == 0 || owners[owner] || surface.Handle == 0 {
			return fmt.Errorf("KTF image surface owner or handle is invalid")
		}
		owners[owner] = true
		refs = append(refs, surface.Owner)
	}
	if cost > maxHeapStorageBytes || !saved.SerialDue.Set && saved.SerialDue.Remaining != 0 {
		return fmt.Errorf("KTF platform roots exceed data limits or have an invalid deadline")
	}
	for _, ref := range refs {
		if uint64(ref) > uint64(rootCount) {
			return fmt.Errorf("KTF platform root reference is out of range")
		}
	}
	return nil
}

func (runtime *initializationRuntime) adoptPlatformRoots(saved runtimeHeapRoots, roots []*jvm.Object, context *heapNativeContext) {
	now := context.now
	object := func(id uint32) *jvm.Object {
		if id == 0 {
			return nil
		}
		return roots[id-1]
	}
	list := func(ids []uint32) []*jvm.Object {
		values := make([]*jvm.Object, len(ids))
		for i, id := range ids {
			values[i] = object(id)
		}
		return values
	}
	value := func(saved jvm.HeapValueState) jvm.Value {
		switch saved.Kind {
		case jvm.ValueInt:
			return jvm.IntValue(int32(saved.Bits))
		case jvm.ValueLong:
			return jvm.LongValue(int64(saved.Bits))
		case jvm.ValueFloat:
			return jvm.FloatValue(math.Float32frombits(uint32(saved.Bits)))
		case jvm.ValueDouble:
			return jvm.DoubleValue(math.Float64frombits(saved.Bits))
		case jvm.ValueReference:
			return jvm.ReferenceValue(object(saved.Reference))
		default:
			return jvm.VoidValue()
		}
	}
	runtime.dockedCard, runtime.activeJlet, runtime.cInput.card = object(saved.DockedCard), object(saved.ActiveJlet), object(saved.CInputCard)
	runtime.relayOnline, runtime.relaySocket = saved.RelayOnline, nil
	if saved.RelaySocket != 0 {
		runtime.relaySocket = object(saved.RelaySocket).Native.(*relaySocket)
	}
	runtime.client.textMu.Lock()
	runtime.client.focusedText = object(saved.FocusedText)
	runtime.client.textEditor = context.restoredEditors[saved.FocusedEditor]
	runtime.client.textMu.Unlock()
	runtime.displayCards, runtime.pendingThreads = list(saved.DisplayCards), list(saved.PendingThreads)
	runtime.pendingSerial, runtime.jletListeners = list(saved.PendingSerial), list(saved.JletListeners)
	runtime.serialDueAt = saved.SerialDue.restore(now)
	runtime.runtimeObjects = make(map[string]*jvm.Object, len(saved.Objects))
	for _, binding := range saved.Objects {
		if string(binding.Name) == heapMediaRoot {
			continue
		}
		runtime.runtimeObjects[string(binding.Name)] = object(binding.Root)
	}
	runtime.grabbedKeys = make(map[int32]*jvm.Object, len(saved.GrabbedKeys))
	for _, binding := range saved.GrabbedKeys {
		runtime.grabbedKeys[binding.Key] = object(binding.Root)
	}
	runtime.serialPaintOwners = make(map[*jvm.Object]*jvm.Object, len(saved.SerialPaintOwners))
	for _, binding := range saved.SerialPaintOwners {
		runtime.serialPaintOwners[object(binding.Owner)] = object(binding.Target)
	}
	runtime.kfcOwnedKeys = make(map[int32]kfcKeyOwner, len(saved.KFCKeys))
	for _, binding := range saved.KFCKeys {
		runtime.kfcOwnedKeys[binding.Key] = kfcKeyOwner{form: object(binding.Form), field: object(binding.Field), visibilityRevision: value(binding.Visibility), childrenRevision: value(binding.Children),
			event: runtimeComponentEventState{revision: value(binding.Revision), listenerValue: value(binding.ListenerValue), hasListener: binding.HasListener, listener: object(binding.Listener), dataValue: value(binding.DataValue), hasData: binding.HasData, data: object(binding.Data)}}
	}
	runtime.pendingTimers = make([]wipicTimer, len(saved.Timers))
	for i, timer := range saved.Timers {
		runtime.pendingTimers[i] = wipicTimer{paintedCard: object(timer.Card), task: object(timer.Task), owner: object(timer.Owner), pointer: timer.Pointer, callback: timer.Callback, param: timer.Param, delay: timer.Delay, due: timer.Due.restore(now), period: timer.Period}
	}
	runtime.events = make([]guestEvent, len(saved.Events))
	for i, event := range saved.Events {
		runtime.events[i] = guestEvent{kind: event[0], param1: event[1], param2: event[2], param3: event[3]}
	}
	runtime.clips = make(map[weak.Pointer[jvm.Object]]*clipState, len(saved.Clips))
	for _, clip := range saved.Clips {
		runtime.clips[weak.Make(object(clip.Owner))] = &clipState{data: bytes.Clone(clip.Data), handle: clip.Handle, loaded: clip.Loaded, played: clip.Played}
	}
	runtime.imageSurfaces = make(map[weak.Pointer[jvm.Object]]uint32, len(saved.ImageSurfaces))
	for _, surface := range saved.ImageSurfaces {
		runtime.imageSurfaces[weak.Make(object(surface.Owner))] = surface.Handle
	}
}
