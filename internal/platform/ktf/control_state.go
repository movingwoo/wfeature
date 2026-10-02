package ktf

import (
	"bytes"
	"fmt"
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
}
type restoredRuntimeControl struct {
	saved     runtimeControlState
	input     cInputState
	network   []wipicNetCallback
	clips     map[uint32]*wipicMediaClip
	clipOrder []uint32
}

func (runtime *initializationRuntime) captureControlState(now time.Time) (runtimeControlState, error) {
	if runtime.cInput.pendingValid != nil || runtime.repaintServicing || runtime.resultBindingDepth != 0 {
		return runtimeControlState{}, fmt.Errorf("KTF control capture has an active Host input, paint or binding operation")
	}
	if len(runtime.pendingNetCallbacks) > maxPendingNetCallbacks || len(runtime.wipicClips) > maxWIPICMediaClips || len(runtime.wipicClipOrder) > maxWIPICMediaClips || len(runtime.cInput.pending) > 2 {
		return runtimeControlState{}, fmt.Errorf("KTF control state exceeds queue limits")
	}
	age := now.Sub(runtime.clockBase)
	if age == time.Duration(math.MinInt64) || !runtime.clockBase.Add(age).Equal(now) {
		return runtimeControlState{}, fmt.Errorf("KTF clock anchor exceeds duration range")
	}
	input := &runtime.cInput
	saved := runtimeControlState{VirtualBaseMillis: runtime.virtualBaseMillis, ClockAge: age,
		RepaintPending: runtime.repaintPending, GuestFlushedOwnFrame: runtime.guestFlushedOwnFrame, GuestHasPainted: runtime.guestHasPainted,
		RoundsSinceGuestPaint: int64(runtime.roundsSinceGuestPaint), GuestEventLoop: runtime.guestEventLoop, DestroyCallbackStarted: runtime.destroyCallbackStarted,
		CInput: runtimeCInputState{OwnerAddress: input.owner.address, OwnerValue: input.owner.value, Mode: input.mode,
			Revision: input.revision, Calls: input.calls, Activations: input.activations, Active: input.active,
			DiscardCarrier: input.discardCarrier, ClearPending: input.clearPending, Pending: bytes.Clone(input.pending), TimerPointer: input.timerPointer, TimerCallback: input.timerCallback},
		ClipOrder: slices.Clone(runtime.wipicClipOrder)}
	for _, callback := range runtime.pendingNetCallbacks {
		saved.Network = append(saved.Network, runtimeNetCallbackState{Callback: callback.callback, Param: callback.param})
	}
	seen := make(map[*wipicMediaClip]bool)
	for _, address := range metadataKeys(runtime.wipicClips) {
		clip := runtime.wipicClips[address]
		if clip == nil || seen[clip] || len(clip.mediaType) > 64 || len(clip.state.data) > maxClipBufferBytes {
			return runtimeControlState{}, fmt.Errorf("KTF C clip has invalid ownership or size")
		}
		seen[clip] = true
		saved.Clips = append(saved.Clips, runtimeCClipState{Address: address, MediaType: []byte(clip.mediaType), Data: bytes.Clone(clip.state.data),
			Handle: clip.state.handle, Loaded: clip.state.loaded, Played: clip.state.played})
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
	if saved.CInput.Mode >= uint32(len(inputModes)) || len(saved.CInput.Pending) > 2 || len(saved.Network) > maxPendingNetCallbacks || len(saved.Clips) > maxWIPICMediaClips || len(saved.ClipOrder) != len(saved.Clips) {
		return invalid("control state exceeds queue limits")
	}
	memory := runtime.client.core.Memory()
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
		input: cInputState{owner: cInputOwner{address: input.OwnerAddress, value: input.OwnerValue}, mode: input.Mode,
			revision: input.Revision, calls: input.Calls, activations: input.Activations, active: input.Active,
			discardCarrier: input.DiscardCarrier, clearPending: input.ClearPending, pending: bytes.Clone(input.Pending), timerPointer: input.TimerPointer, timerCallback: input.TimerCallback},
		clips: make(map[uint32]*wipicMediaClip, len(saved.Clips)), clipOrder: slices.Clone(saved.ClipOrder)}
	for _, callback := range saved.Network {
		if callback.Callback == 0 {
			return invalid("network callback is null")
		}
		if err := memory.ValidateRange(callback.Callback&^1, 2, armcore.PermissionExecute); err != nil {
			return restoredRuntimeControl{}, err
		}
		result.network = append(result.network, wipicNetCallback{callback: callback.Callback, param: callback.Param})
	}
	for i, clip := range saved.Clips {
		if clip.Address == 0 || clip.Address&3 != 0 || i > 0 && saved.Clips[i-1].Address >= clip.Address || len(clip.MediaType) > 64 || bytes.IndexByte(clip.MediaType, 0) >= 0 || len(clip.Data) > maxClipBufferBytes || clip.Loaded && clip.Handle == 0 {
			return invalid("C clip has invalid address, handle or data")
		}
		if err := memory.ValidateRange(clip.Address, wipicMediaClipRecordSize, armcore.PermissionReadWrite); err != nil {
			return restoredRuntimeControl{}, err
		}
		result.clips[clip.Address] = &wipicMediaClip{mediaType: string(clip.MediaType), state: clipState{data: bytes.Clone(clip.Data), handle: clip.Handle, loaded: clip.Loaded, played: clip.Played}}
	}
	seen := make(map[uint32]bool, len(saved.ClipOrder))
	for _, address := range saved.ClipOrder {
		if seen[address] || result.clips[address] == nil {
			return invalid("C clip eviction order is invalid")
		}
		seen[address] = true
	}
	return result, nil
}

func (restored restoredRuntimeControl) adopt(runtime *initializationRuntime, now time.Time) {
	saved := restored.saved
	runtime.virtualBaseMillis, runtime.clockBase = saved.VirtualBaseMillis, now.Add(-saved.ClockAge)
	runtime.repaintPending, runtime.guestFlushedOwnFrame, runtime.guestHasPainted = saved.RepaintPending, saved.GuestFlushedOwnFrame, saved.GuestHasPainted
	runtime.roundsSinceGuestPaint, runtime.guestEventLoop, runtime.destroyCallbackStarted = int(saved.RoundsSinceGuestPaint), saved.GuestEventLoop, saved.DestroyCallbackStarted
	// The JVM card reference was adopted by the shared heap-root component.
	restored.input.card = runtime.cInput.card
	runtime.cInput = restored.input
	runtime.pendingNetCallbacks = restored.network
	runtime.wipicClips, runtime.wipicClipOrder = restored.clips, restored.clipOrder
}
