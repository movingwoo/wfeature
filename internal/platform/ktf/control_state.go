package ktf

import (
	"bytes"
	"fmt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

type runtimeControlState struct {
	VirtualBaseMillis                                     int64
	ClockAge                                              time.Duration
	RepaintPending, GuestFlushedOwnFrame, GuestHasPainted bool
	RoundsSinceGuestPaint                                 int64
	GuestEventLoop, DestroyCallbackStarted                bool
	CInput                                                runtimeCInputState
	Network                                               []runtimeNetCallbackState
	Clips                                                 []runtimeCClipState
	ClipOrder                                             []uint32
	// MutedSources lists, in increasing order, the WIPI C media sources whose
	// mute state was last set on.
	MutedSources []uint32
}
type runtimeCInputState struct {
	OwnerAddress, OwnerValue, Mode       uint32
	Revision, Calls, Activations         uint64
	Active, DiscardCarrier, ClearPending bool
	Pending                              []byte
	TimerPointer, TimerCallback          uint32
}
type runtimeNetCallbackState struct{ Callback, Param uint32 }
type runtimeCClipState struct {
	Address         uint32
	MediaType, Data []byte
	Handle          backend.AudioHandle
	Loaded, Played  bool
	// Volume is the clip's own level from 0 to 100.
	Volume int32
}
type restoredRuntimeControl struct {
	saved     runtimeControlState
	input     wipicrt.CInputState
	network   []wipicrt.WIPICNetCallback
	clips     map[uint32]*wipicrt.WIPICMediaClip
	clipOrder []uint32
	muted     map[uint32]bool
}

func (runtime *initializationRuntime) captureControlState(now time.Time) (runtimeControlState, error) {
	if runtime.CInput.PendingValid != nil || runtime.CInput.Delivering || runtime.javaInput.pending != nil || runtime.RepaintServicing || runtime.resultBindingDepth != 0 {
		return runtimeControlState{}, fmt.Errorf("KTF control capture has an active Host input, paint or binding operation")
	}
	if len(runtime.PendingNetCallbacks) > wipicrt.MaxPendingNetCallbacks || len(runtime.WIPICClips) > wipicrt.MaxWIPICMediaClips || len(runtime.WIPICClipOrder) > wipicrt.MaxWIPICMediaClips || len(runtime.CInput.Pending) > 2 {
		return runtimeControlState{}, fmt.Errorf("KTF control state exceeds queue limits")
	}
	age := now.Sub(runtime.ClockBase)
	if age == time.Duration(math.MinInt64) || !runtime.ClockBase.Add(age).Equal(now) {
		return runtimeControlState{}, fmt.Errorf("KTF clock anchor exceeds duration range")
	}
	input := &runtime.CInput
	saved := runtimeControlState{VirtualBaseMillis: runtime.VirtualBaseMillis, ClockAge: age,
		RepaintPending: runtime.RepaintPending, GuestFlushedOwnFrame: runtime.GuestFlushedOwnFrame, GuestHasPainted: runtime.guestHasPainted,
		RoundsSinceGuestPaint: int64(runtime.roundsSinceGuestPaint), GuestEventLoop: runtime.GuestEventLoop, DestroyCallbackStarted: runtime.destroyCallbackStarted,
		CInput: runtimeCInputState{OwnerAddress: input.Owner.Address, OwnerValue: input.Owner.Value, Mode: input.Mode,
			Revision: input.Revision, Calls: input.Calls, Activations: input.Activations, Active: input.Active,
			DiscardCarrier: input.DiscardCarrier, ClearPending: input.ClearPending, Pending: bytes.Clone(input.Pending), TimerPointer: input.TimerPointer, TimerCallback: input.TimerCallback},
		ClipOrder: slices.Clone(runtime.WIPICClipOrder)}
	for _, callback := range runtime.PendingNetCallbacks {
		saved.Network = append(saved.Network, runtimeNetCallbackState{Callback: callback.Callback, Param: callback.Param})
	}
	for _, source := range slices.Sorted(maps.Keys(runtime.WIPICMutedSources)) {
		if runtime.WIPICMutedSources[source] {
			saved.MutedSources = append(saved.MutedSources, source)
		}
	}
	seen := make(map[*wipicrt.WIPICMediaClip]bool)
	for _, address := range metadataKeys(runtime.WIPICClips) {
		clip := runtime.WIPICClips[address]
		if clip == nil || seen[clip] || len(clip.MediaType) > 64 || len(clip.State.Data) > wipicrt.MaxClipBufferBytes {
			return runtimeControlState{}, fmt.Errorf("KTF C clip has invalid ownership or size")
		}
		seen[clip] = true
		saved.Clips = append(saved.Clips, runtimeCClipState{Address: address, MediaType: []byte(clip.MediaType), Data: bytes.Clone(clip.State.Data),
			Handle: clip.State.Handle, Loaded: clip.State.Loaded, Played: clip.State.Played, Volume: clip.Volume})
	}
	if _, err := runtime.decodeControlState(saved); err != nil {
		return runtimeControlState{}, err
	}
	return saved, nil
}

func (runtime *initializationRuntime) decodeControlState(saved runtimeControlState) (restoredRuntimeControl, error) {
	invalid := func(message string) (restoredRuntimeControl, error) {
		return restoredRuntimeControl{}, fmt.Errorf("KTF %s", message)
	}
	if saved.ClockAge == time.Duration(math.MinInt64) || saved.RoundsSinceGuestPaint < 0 || uint64(saved.RoundsSinceGuestPaint) > uint64(^uint(0)>>1) {
		return invalid("control clock or paint counter is invalid")
	}
	if saved.CInput.Mode >= uint32(len(wipicrt.InputModes)) || len(saved.CInput.Pending) > 2 || len(saved.Network) > wipicrt.MaxPendingNetCallbacks || len(saved.Clips) > wipicrt.MaxWIPICMediaClips || len(saved.ClipOrder) != len(saved.Clips) || len(saved.MutedSources) > wipicrt.MaxWIPICMutedSources {
		return invalid("control state exceeds queue limits")
	}
	memory := runtime.client.Core().Memory()
	input := saved.CInput
	for _, address := range []uint32{input.OwnerAddress, input.TimerPointer} {
		if address == 0 {
			continue
		}
		if address&3 != 0 {
			return invalid("C input pointer is not word-aligned")
		}
		if err := memory.ValidateRange(address, 4, armcore.PermissionRead); err != nil {
			return restoredRuntimeControl{}, err
		}
	}
	if input.TimerCallback != 0 {
		if err := memory.ValidateRange(input.TimerCallback&^1, 2, armcore.PermissionExecute); err != nil {
			return restoredRuntimeControl{}, err
		}
	}
	result := restoredRuntimeControl{saved: saved,
		input: wipicrt.CInputState{Owner: wipicrt.CInputOwner{Address: input.OwnerAddress, Value: input.OwnerValue}, Mode: input.Mode,
			Revision: input.Revision, Calls: input.Calls, Activations: input.Activations, Active: input.Active,
			DiscardCarrier: input.DiscardCarrier, ClearPending: input.ClearPending, Pending: bytes.Clone(input.Pending), TimerPointer: input.TimerPointer, TimerCallback: input.TimerCallback},
		clips: make(map[uint32]*wipicrt.WIPICMediaClip, len(saved.Clips)), clipOrder: slices.Clone(saved.ClipOrder)}
	for _, callback := range saved.Network {
		if callback.Callback == 0 {
			return invalid("network callback is null")
		}
		if err := memory.ValidateRange(callback.Callback&^1, 2, armcore.PermissionExecute); err != nil {
			return restoredRuntimeControl{}, err
		}
		result.network = append(result.network, wipicrt.WIPICNetCallback{Callback: callback.Callback, Param: callback.Param})
	}
	for i, clip := range saved.Clips {
		if clip.Address == 0 || clip.Address&3 != 0 || i > 0 && saved.Clips[i-1].Address >= clip.Address || len(clip.MediaType) > 64 || bytes.IndexByte(clip.MediaType, 0) >= 0 || len(clip.Data) > wipicrt.MaxClipBufferBytes || clip.Loaded && clip.Handle == 0 {
			return invalid("C clip has invalid address, handle or data")
		}
		if clip.Volume < 0 || clip.Volume > wipicrt.WIPICClipFullVolume {
			return invalid("C clip volume is out of range")
		}
		if err := memory.ValidateRange(clip.Address, wipicrt.WIPICMediaClipRecordSize, armcore.PermissionReadWrite); err != nil {
			return restoredRuntimeControl{}, err
		}
		result.clips[clip.Address] = &wipicrt.WIPICMediaClip{MediaType: string(clip.MediaType), Volume: clip.Volume,
			State: wipicrt.ClipState{Data: bytes.Clone(clip.Data), Handle: clip.Handle, Loaded: clip.Loaded, Played: clip.Played}}
	}
	seen := make(map[uint32]bool, len(saved.ClipOrder))
	for _, address := range saved.ClipOrder {
		if seen[address] || result.clips[address] == nil {
			return invalid("C clip eviction order is invalid")
		}
		seen[address] = true
	}
	for index, source := range saved.MutedSources {
		if index > 0 && saved.MutedSources[index-1] >= source {
			return invalid("C media muted sources are not in increasing order")
		}
		if result.muted == nil {
			result.muted = make(map[uint32]bool, len(saved.MutedSources))
		}
		result.muted[source] = true
	}
	return result, nil
}

func (restored restoredRuntimeControl) adopt(runtime *initializationRuntime, now time.Time) {
	saved := restored.saved
	runtime.VirtualBaseMillis, runtime.ClockBase = saved.VirtualBaseMillis, now.Add(-saved.ClockAge)
	runtime.RepaintPending, runtime.GuestFlushedOwnFrame, runtime.guestHasPainted = saved.RepaintPending, saved.GuestFlushedOwnFrame, saved.GuestHasPainted
	runtime.roundsSinceGuestPaint, runtime.GuestEventLoop, runtime.destroyCallbackStarted = int(saved.RoundsSinceGuestPaint), saved.GuestEventLoop, saved.DestroyCallbackStarted
	// The card the input method belongs to is the runtime's own field, which
	// the shared heap-root component restored.
	runtime.CInput = restored.input
	runtime.PendingNetCallbacks = restored.network
	runtime.WIPICClips, runtime.WIPICClipOrder = restored.clips, restored.clipOrder
	runtime.WIPICMutedSources = restored.muted
}
