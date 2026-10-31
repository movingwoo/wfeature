package clet

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The record a Clet checkpoint carries. A Clet is called and returns: between
// two service rounds nothing of it is running, so its whole execution state is
// the ARM core with the program's memory in it, the client thread's registers,
// and what the runtime keeps beside the memory — the arena, the timers, the
// event queue, the open files by name, the clips, the input method and the
// pacing. Everything is recorded at that boundary, and capture refuses one it
// is not at. See session_checkpoint.go for what a checkpoint is and is not.

// checkpointVersion is the layout of checkpointState and every record in it.
const checkpointVersion = 1

type checkpointState struct {
	Version uint32
	// TimerLimit and DisableAuthentication are session settings the record
	// depends on: the second changes the program's own code, since the
	// identity compatibility is applied to it before it is loaded.
	TimerLimit            int
	DisableAuthentication bool
	Client                clientState
	Runtime               runtimeState
	Storage               storageState
}

// clientState is the client's half: the image the record belongs to, the core
// and the client thread, and the frame, pacing and device records.
type clientState struct {
	ImageName []byte
	ImageHash [32]byte
	Core      armcore.CoreState
	Thread    armcore.RootThreadState
	// Root is the root table the loader handed the program, and Repaint,
	// TimeRecord and TokenNext are the rest of cletState.
	Root                      uint32
	Repaint                   bool
	TimeRecord, TokenNext     uint32
	Subscriber                string
	Authentication            backend.AuthenticationStatus
	Frame                     []byte
	FrameWidth, FrameHeight   int
	ScreenWidth, ScreenHeight int
	FlushCount                uint32
	FramePending              bool
	PartialFrame              bool
	Speed                     float64
	ClientWake                deadlineState
	NextRoundPaint            deadlineState
	PaintLoad                 float64
	PaintCost, EntryCost      time.Duration
	WorkBaseline              uint64
	// The service policy is the session's, and a record taken under another
	// one is refused rather than run under this one.
	ThreadSliceSteps, ServiceSteps uint64
	ServiceWait                    time.Duration
	Audio                          *backend.AudioState
	Vibration                      backend.VibrationState
}

// runtimeState is what the runtime keeps beside guest memory.
type runtimeState struct {
	Arena              arenaState
	CodeCursor         uint64
	Stubs              []stubState
	WIPICAllocations   []allocationState
	ImageSourceBuffers []addressPairState
	ReleasedWIPIC      []uint32
	ReleasedWIPICOrder []uint32
	UserMemoryPools    []userPoolState
	PixelOp            *pixelOpState
	ScreenFramebuffer  uint32
	UserMemory         uint32
	InputModeTable     uint32
	LEDs               int32
	// An opacity mask can be shared between an image and its framebuffer, so
	// the masks are recorded once and the handles name them by position.
	Opacities          []opacityState
	FramebufferOpacity []framebufferOpacityState
	VirtualBaseMillis  int64
	ClockAge           time.Duration
	Timers             []timerState
	Events             [][4]int32
	GuestEventLoop     bool
	RepaintPending     bool
	GuestFlushedOwn    bool
	CInput             cInputRecord
	Network            []netCallbackState
	Clips              []clipRecord
	ClipOrder          []uint32
	MutedSources       []uint32
}

// deadlineState is an instant as the time left until it, so a record does not
// carry the clock it was taken on. Set says there is an instant at all.
type deadlineState struct {
	Set       bool
	Remaining time.Duration
}

// maxDeadlineOffset bounds how far from the moment of capture a recorded
// instant may lie. A timer is at most an hour ahead at the slowest speed and a
// paused session's overdue work lies behind, so this is a guard on the
// arithmetic and not a policy.
const maxDeadlineOffset = 100_000 * time.Hour

func captureDeadline(instant, now time.Time) deadlineState {
	if instant.IsZero() {
		return deadlineState{}
	}
	return deadlineState{Set: true, Remaining: instant.Sub(now)}
}

func (deadline deadlineState) valid() bool {
	if !deadline.Set {
		return deadline.Remaining == 0
	}
	return deadline.Remaining > -maxDeadlineOffset && deadline.Remaining < maxDeadlineOffset
}

func (deadline deadlineState) restore(now time.Time) time.Time {
	if !deadline.Set {
		return time.Time{}
	}
	return now.Add(deadline.Remaining)
}

type arenaState struct {
	Base                            uint32
	Limit, Cursor, HighWater, Freed uint64
	Free                            []arenaBlockState
}
type arenaBlockState struct{ Start, End uint64 }
type stubState struct {
	Key     uint64
	Address uint32
}
type allocationState struct {
	Address uint32
	Size    uint64
}
type addressPairState struct{ Address, Target uint32 }
type userPoolState struct{ Base, Size, Cursor, FreeCalls uint32 }
type pixelOpState struct {
	Function, Param uint32
	Results         []pixelResultState
}
type pixelResultState struct {
	Pair   uint32
	Result uint16
}
type opacityState struct {
	Width, Height int
	Opaque        []byte
}
type framebufferOpacityState struct{ Handle, Opacity uint32 }
type timerState struct {
	Pointer, Callback, Param uint32
	Delay                    uint64
	Due                      deadlineState
}
type cInputRecord struct {
	OwnerAddress, OwnerValue, Mode       uint32
	Revision, Calls, Activations         uint64
	Active, DiscardCarrier, ClearPending bool
	Pending                              []byte
	TimerPointer, TimerCallback          uint32
}
type netCallbackState struct{ Callback, Param uint32 }
type clipRecord struct {
	Address         uint32
	MediaType, Data []byte
	Handle          backend.AudioHandle
	Loaded, Played  bool
	Completed       uint64
	// Volume is the clip's own level from 0 to 100.
	Volume int32
}

const (
	// maxCheckpointEntries bounds the table records together.
	maxCheckpointEntries = 1 << 20
	// maxOpacityBytes bounds the opacity masks together.
	maxOpacityBytes = 64 << 20
	// maxCheckpointStorageBytes bounds what the files a checkpoint names may
	// hold between them when a load reads them back.
	maxCheckpointStorageBytes = 64 << 20
)

func sortedKeys[K cmp.Ordered, V any](values map[K]V) []K {
	return slices.Sorted(maps.Keys(values))
}

// checkpointIdle refuses a capture anywhere but between two whole service
// rounds: nothing of the program running, no Host operation half done.
func (client *Client) checkpointIdle() error {
	if client == nil || client.Core() == nil || client.Thread == nil || client.clet == nil || client.runtime == nil {
		return fmt.Errorf("SKT Clet checkpoint session is not started")
	}
	runtime := client.runtime
	if !client.initializationStarted || client.prepared != client.runtime || client.workersStopped || client.clet.destroyStarted {
		return fmt.Errorf("SKT Clet checkpoint requires a running program")
	}
	if client.ServiceDepth != 0 || client.ActiveTimer != nil || !client.ServingDue.IsZero() || len(runtime.RoundTimers) != 0 ||
		runtime.CurrentThread != nil || runtime.CurrentContext != nil || runtime.RepaintServicing || len(runtime.AOTCallDepth) != 0 {
		return fmt.Errorf("SKT Clet checkpoint requires an idle session boundary")
	}
	if runtime.CInput.PendingValid != nil || runtime.CInput.Delivering {
		return fmt.Errorf("SKT Clet checkpoint cannot capture a text entry in progress")
	}
	if runtime.SaveReadError != nil {
		return fmt.Errorf("SKT Clet checkpoint requires successful storage reads")
	}
	// A Clet has no File class: nothing here writes the guest file table, and
	// a record that holds no copy of a save relies on that.
	if len(runtime.GuestFiles) != 0 || len(runtime.GuestFileWrites) != 0 {
		return fmt.Errorf("SKT Clet checkpoint found guest files it does not record")
	}
	return nil
}

// captureCheckpointState records the client at now. The caller holds client.run
// and has checked checkpointIdle.
func (client *Client) captureCheckpointState(now time.Time) (clientState, runtimeState, storageState, error) {
	runtime := client.runtime
	saved := clientState{ImageName: []byte(client.image.Name), ImageHash: sha256.Sum256(client.image.Data),
		Root: client.clet.root, Repaint: client.clet.repaint, TimeRecord: client.clet.timeRecord, TokenNext: client.clet.tokenNext,
		Subscriber: client.SubscriberNumber, Authentication: client.authentication,
		Frame: bytes.Clone(client.FrameRGBA), FrameWidth: client.FrameWidth, FrameHeight: client.FrameHeight,
		ScreenWidth: client.ScreenWidth, ScreenHeight: client.ScreenHeight, FlushCount: client.FlushCount,
		FramePending: client.FramePending, PartialFrame: client.PartialFrame, Speed: client.SpeedOrDefault(),
		ClientWake: captureDeadline(client.ClientWakeAt, now), NextRoundPaint: captureDeadline(client.NextRoundPaint, now),
		PaintLoad: client.PaintLoad, PaintCost: client.PaintCost, EntryCost: client.EntryCost, WorkBaseline: client.WorkBaseline,
		ThreadSliceSteps: client.ThreadSliceSteps, ServiceSteps: client.ServiceSteps, ServiceWait: client.ServiceWait}
	var err error
	if saved.Vibration, err = client.Vibrator.CaptureState(); err != nil {
		return clientState{}, runtimeState{}, storageState{}, err
	}
	if client.Audio != nil {
		audio, err := client.Audio.CaptureStateAt(now)
		if err != nil {
			return clientState{}, runtimeState{}, storageState{}, err
		}
		saved.Audio = &audio
	}
	if saved.Thread, err = client.Core().CaptureRootThread(client.Thread); err != nil {
		return clientState{}, runtimeState{}, storageState{}, err
	}
	state, err := runtime.captureRuntimeState(now)
	if err != nil {
		return clientState{}, runtimeState{}, storageState{}, err
	}
	storage, err := runtime.captureStorageState()
	if err != nil {
		return clientState{}, runtimeState{}, storageState{}, err
	}
	// The memory goes last: it is the largest record, and copying it is what
	// a refused capture should not have paid for.
	if saved.Core, err = client.Core().CaptureState(); err != nil {
		return clientState{}, runtimeState{}, storageState{}, err
	}
	return saved, state, storage, nil
}

func (runtime *initializationRuntime) captureRuntimeState(now time.Time) (runtimeState, error) {
	entries := uint64(len(runtime.Stubs)) + uint64(len(runtime.WIPICAllocations)) + uint64(len(runtime.ImageSourceBuffers)) +
		uint64(len(runtime.ReleasedWIPIC)) + uint64(len(runtime.ReleasedWIPICOrder)) + uint64(len(runtime.UserMemoryPools)) +
		uint64(len(runtime.FramebufferOpacity))
	if runtime.PixelOps != nil {
		entries += uint64(len(runtime.PixelOps.Results))
	}
	if entries > maxCheckpointEntries || len(runtime.PendingTimers) > wipicrt.MaxPendingTimers || len(runtime.Events) > wipicrt.MaxQueuedEvents ||
		len(runtime.PendingNetCallbacks) > wipicrt.MaxPendingNetCallbacks || len(runtime.WIPICClips) > wipicrt.MaxWIPICMediaClips ||
		len(runtime.WIPICClipOrder) > wipicrt.MaxWIPICMediaClips || len(runtime.CInput.Pending) > 2 {
		return runtimeState{}, fmt.Errorf("SKT Clet runtime state exceeds its limits")
	}
	age := now.Sub(runtime.ClockBase)
	if age == time.Duration(math.MinInt64) || age == time.Duration(math.MaxInt64) || !runtime.ClockBase.Add(age).Equal(now) {
		return runtimeState{}, fmt.Errorf("SKT Clet clock anchor exceeds its duration range")
	}
	input := &runtime.CInput
	saved := runtimeState{CodeCursor: runtime.CodeCursor, ScreenFramebuffer: runtime.ScreenFramebuffer,
		UserMemory: runtime.UserMemoryInterface, InputModeTable: runtime.InputModeTableAddress, LEDs: runtime.Leds,
		VirtualBaseMillis: runtime.VirtualBaseMillis, ClockAge: age,
		GuestEventLoop: runtime.GuestEventLoop, RepaintPending: runtime.RepaintPending, GuestFlushedOwn: runtime.GuestFlushedOwnFrame,
		CInput: cInputRecord{OwnerAddress: input.Owner.Address, OwnerValue: input.Owner.Value, Mode: input.Mode,
			Revision: input.Revision, Calls: input.Calls, Activations: input.Activations, Active: input.Active,
			DiscardCarrier: input.DiscardCarrier, ClearPending: input.ClearPending, Pending: bytes.Clone(input.Pending),
			TimerPointer: input.TimerPointer, TimerCallback: input.TimerCallback},
		ReleasedWIPIC: sortedKeys(runtime.ReleasedWIPIC), ReleasedWIPICOrder: slices.Clone(runtime.ReleasedWIPICOrder),
		ClipOrder: slices.Clone(runtime.WIPICClipOrder)}
	var err error
	if saved.Arena, err = captureArenaState(runtime.Arena); err != nil {
		return runtimeState{}, err
	}
	for _, key := range sortedKeys(runtime.Stubs) {
		saved.Stubs = append(saved.Stubs, stubState{Key: key, Address: runtime.Stubs[key]})
	}
	for _, address := range sortedKeys(runtime.WIPICAllocations) {
		saved.WIPICAllocations = append(saved.WIPICAllocations, allocationState{Address: address, Size: runtime.WIPICAllocations[address]})
	}
	for _, image := range sortedKeys(runtime.ImageSourceBuffers) {
		saved.ImageSourceBuffers = append(saved.ImageSourceBuffers, addressPairState{Address: image, Target: runtime.ImageSourceBuffers[image]})
	}
	for _, base := range sortedKeys(runtime.UserMemoryPools) {
		pool := runtime.UserMemoryPools[base]
		if pool == nil || pool.Base != base {
			return runtimeState{}, fmt.Errorf("SKT Clet user memory pool has invalid ownership")
		}
		saved.UserMemoryPools = append(saved.UserMemoryPools, userPoolState{Base: pool.Base, Size: pool.Size, Cursor: pool.Cursor, FreeCalls: pool.FreeCalls})
	}
	if cache := runtime.PixelOps; cache != nil {
		saved.PixelOp = &pixelOpState{Function: cache.Function, Param: cache.Param}
		for _, pair := range sortedKeys(cache.Results) {
			saved.PixelOp.Results = append(saved.PixelOp.Results, pixelResultState{Pair: pair, Result: cache.Results[pair]})
		}
	}
	masks := make(map[*wipicrt.ImageOpacity]uint32)
	var maskBytes uint64
	for _, handle := range sortedKeys(runtime.FramebufferOpacity) {
		mask := runtime.FramebufferOpacity[handle]
		if mask == nil || mask.Width <= 0 || mask.Height <= 0 || len(mask.Opaque) != mask.Width*mask.Height {
			return runtimeState{}, fmt.Errorf("SKT Clet opacity mask is invalid")
		}
		id, seen := masks[mask]
		if !seen {
			maskBytes += uint64(len(mask.Opaque))
			if maskBytes > maxOpacityBytes {
				return runtimeState{}, fmt.Errorf("SKT Clet opacity masks exceed their limit")
			}
			opaque := make([]byte, len(mask.Opaque))
			for index, drawn := range mask.Opaque {
				if drawn {
					opaque[index] = 1
				}
			}
			saved.Opacities = append(saved.Opacities, opacityState{Width: mask.Width, Height: mask.Height, Opaque: opaque})
			id = uint32(len(saved.Opacities))
			masks[mask] = id
		}
		saved.FramebufferOpacity = append(saved.FramebufferOpacity, framebufferOpacityState{Handle: handle, Opacity: id})
	}
	for _, timer := range runtime.PendingTimers {
		saved.Timers = append(saved.Timers, timerState{Pointer: timer.Pointer, Callback: timer.Callback, Param: timer.Param, Delay: timer.Delay, Due: captureDeadline(timer.Due, now)})
	}
	for _, event := range runtime.Events {
		saved.Events = append(saved.Events, [4]int32{event.Kind, event.Param1, event.Param2, event.Param3})
	}
	for _, callback := range runtime.PendingNetCallbacks {
		saved.Network = append(saved.Network, netCallbackState{Callback: callback.Callback, Param: callback.Param})
	}
	for _, source := range sortedKeys(runtime.WIPICMutedSources) {
		if runtime.WIPICMutedSources[source] {
			saved.MutedSources = append(saved.MutedSources, source)
		}
	}
	seen := make(map[*wipicrt.WIPICMediaClip]bool, len(runtime.WIPICClips))
	for _, address := range sortedKeys(runtime.WIPICClips) {
		clip := runtime.WIPICClips[address]
		if clip == nil || seen[clip] || len(clip.MediaType) > 64 || len(clip.State.Data) > wipicrt.MaxClipBufferBytes {
			return runtimeState{}, fmt.Errorf("SKT Clet clip has invalid ownership or size")
		}
		seen[clip] = true
		saved.Clips = append(saved.Clips, clipRecord{Address: address, MediaType: []byte(clip.MediaType), Data: bytes.Clone(clip.State.Data),
			Handle: clip.State.Handle, Loaded: clip.State.Loaded, Played: clip.State.Played, Completed: clip.State.Completed, Volume: clip.Volume})
	}
	return saved, nil
}

func captureArenaState(arena *wipicrt.GuestArena) (arenaState, error) {
	if arena == nil || len(arena.Free) > maxCheckpointEntries {
		return arenaState{}, fmt.Errorf("SKT Clet arena is missing or exceeds the free-block limit")
	}
	saved := arenaState{Base: arena.Base, Limit: arena.Limit, Cursor: arena.Cursor, HighWater: arena.HighWater, Freed: arena.Freed}
	for _, block := range arena.Free {
		saved.Free = append(saved.Free, arenaBlockState{Start: block.Start, End: block.End})
	}
	return saved, nil
}

func restoreArenaState(saved arenaState, base uint32, size uint64) (*wipicrt.GuestArena, error) {
	end := uint64(base) + size
	if size == 0 || end > 1<<32 || saved.Base != base || saved.Limit != end || saved.Cursor < uint64(base) || saved.Cursor > saved.HighWater ||
		saved.HighWater > end || saved.Cursor%wipicrt.ArenaAlignment != 0 || saved.HighWater%wipicrt.ArenaAlignment != 0 || len(saved.Free) > maxCheckpointEntries {
		return nil, fmt.Errorf("SKT Clet arena state has invalid bounds")
	}
	var freed uint64
	for index, block := range saved.Free {
		if block.Start < uint64(base) || block.Start >= block.End || block.End >= saved.Cursor || block.Start%wipicrt.ArenaAlignment != 0 ||
			block.End%wipicrt.ArenaAlignment != 0 || index > 0 && saved.Free[index-1].End >= block.Start {
			return nil, fmt.Errorf("SKT Clet arena free list is not bounded and coalesced")
		}
		freed += block.End - block.Start
	}
	if freed != saved.Freed {
		return nil, fmt.Errorf("SKT Clet arena free-byte count differs from its blocks")
	}
	arena := &wipicrt.GuestArena{Base: base, Limit: end, Cursor: saved.Cursor, HighWater: saved.HighWater, Freed: freed}
	for _, block := range saved.Free {
		arena.Free = append(arena.Free, wipicrt.ArenaBlock{Start: block.Start, End: block.End})
	}
	return arena, nil
}

// expectedMappings is the memory a Clet runs in: its code, its data and BSS,
// the client stack, and the platform's data arena and stub space. A record
// whose memory is laid out any other way is not this program's.
func expectedMappings(program CletProgram) []armcore.MappingState {
	mappings := []armcore.MappingState{
		{Address: ImageBase, Size: uint64(len(program.Code)), Permission: armcore.PermissionReadWriteExecute},
		{Address: program.DataAddress, Size: uint64(len(program.Data)) + uint64(program.BSSSize), Permission: armcore.PermissionReadWrite},
		{Address: wipicrt.ThreadStackBase, Size: wipicrt.ThreadStackSize, Permission: armcore.PermissionReadWrite},
		{Address: wipicrt.PlatformDataBase, Size: wipicrt.PlatformDataSize, Permission: armcore.PermissionReadWrite},
		{Address: wipicrt.PlatformCodeBase, Size: wipicrt.PlatformCodeSize, Permission: armcore.PermissionReadExecute},
	}
	slices.SortFunc(mappings, func(a, b armcore.MappingState) int { return cmp.Compare(a.Address, b.Address) })
	return mappings
}

// validate checks the client record against the client this build loaded
// from the archive, before anything of it is used.
func (saved clientState) validate(client *Client, options SessionOptions) error {
	if !bytes.Equal(saved.ImageName, []byte(client.image.Name)) || saved.ImageHash != sha256.Sum256(client.image.Data) {
		return fmt.Errorf("SKT Clet checkpoint belongs to another program: %w", backend.ErrCheckpointIdentity)
	}
	if !slices.Equal(saved.Core.Memory.Mappings, expectedMappings(client.clet.program)) {
		return fmt.Errorf("SKT Clet checkpoint memory is not laid out as the program is")
	}
	if saved.Thread.StepBudget != 0 || !stackContains(saved.Thread.Context.Registers[armcore.RegisterSP]) || saved.WorkBaseline > saved.Core.Steps {
		return fmt.Errorf("SKT Clet checkpoint client thread or instruction baseline is invalid")
	}
	if saved.ThreadSliceSteps != options.ThreadSliceSteps || saved.ServiceSteps != options.ServiceSteps || saved.ServiceWait != options.ServiceWait {
		return fmt.Errorf("SKT Clet checkpoint service policy is incompatible: %w", backend.ErrCheckpointVersion)
	}
	if saved.Authentication != client.authentication {
		return fmt.Errorf("SKT Clet checkpoint was taken under another identity compatibility: %w", backend.ErrCheckpointVersion)
	}
	if len(saved.Subscriber) > 64 || !bytes.Equal([]byte(saved.Subscriber), bytes.ToValidUTF8([]byte(saved.Subscriber), nil)) {
		return fmt.Errorf("SKT Clet checkpoint subscriber is invalid")
	}
	if saved.Root < wipicrt.PlatformDataBase || uint64(saved.Root)+cletRootSlots*4 > uint64(wipicrt.PlatformDataBase)+wipicrt.PlatformDataSize || saved.Root&3 != 0 {
		return fmt.Errorf("SKT Clet checkpoint root table is outside the platform data")
	}
	if saved.ScreenWidth < 0 || saved.ScreenHeight < 0 || saved.ScreenWidth > wipicrt.MaxWIPICFramebufferSide || saved.ScreenHeight > wipicrt.MaxWIPICFramebufferSide ||
		(saved.ScreenWidth == 0) != (saved.ScreenHeight == 0) {
		return fmt.Errorf("SKT Clet checkpoint screen shape is invalid")
	}
	if len(saved.Frame) == 0 {
		if saved.FrameWidth != 0 || saved.FrameHeight != 0 {
			return fmt.Errorf("SKT Clet checkpoint retained screen is missing")
		}
	} else if saved.FrameWidth <= 0 || saved.FrameHeight <= 0 || saved.FrameWidth > wipicrt.MaxWIPICFramebufferSide || saved.FrameHeight > wipicrt.MaxWIPICFramebufferSide ||
		uint64(len(saved.Frame)) != uint64(saved.FrameWidth)*uint64(saved.FrameHeight)*4 {
		return fmt.Errorf("SKT Clet checkpoint retained screen shape is invalid")
	}
	if math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < backend.SpeedFloor || saved.Speed > backend.SpeedCeiling ||
		math.IsNaN(saved.PaintLoad) || math.IsInf(saved.PaintLoad, 0) || saved.PaintLoad < 0 || saved.PaintCost < 0 || saved.EntryCost < 0 {
		return fmt.Errorf("SKT Clet checkpoint pacing state is invalid")
	}
	if !saved.ClientWake.valid() || !saved.NextRoundPaint.valid() {
		return fmt.Errorf("SKT Clet checkpoint deadline is invalid")
	}
	return saved.Vibration.Validate()
}

// stackContains reports whether a stack pointer lies in the client stack, its
// top included: an empty stack points one past its last word.
func stackContains(address uint32) bool {
	return address >= wipicrt.ThreadStackBase && uint64(address) <= uint64(wipicrt.ThreadStackBase)+wipicrt.ThreadStackSize && address&3 == 0
}

// restoreRuntimeState builds the runtime a record describes over a core the
// record's memory was restored into. It maps nothing and runs no guest code:
// the memory has the platform regions already, which validate checked.
func restoreRuntimeState(client *Client, saved runtimeState, audio *backend.AudioState, now time.Time) (*initializationRuntime, error) {
	entries := uint64(len(saved.Stubs)) + uint64(len(saved.WIPICAllocations)) + uint64(len(saved.ImageSourceBuffers)) +
		uint64(len(saved.ReleasedWIPIC)) + uint64(len(saved.ReleasedWIPICOrder)) + uint64(len(saved.UserMemoryPools)) +
		uint64(len(saved.FramebufferOpacity)) + uint64(len(saved.Opacities))
	if saved.PixelOp != nil {
		entries += uint64(len(saved.PixelOp.Results))
	}
	if entries > maxCheckpointEntries || len(saved.ReleasedWIPIC) > wipicrt.MaxReleasedWIPIC || len(saved.ReleasedWIPICOrder) > wipicrt.MaxReleasedWIPIC ||
		len(saved.Timers) > wipicrt.MaxPendingTimers || len(saved.Events) > wipicrt.MaxQueuedEvents || len(saved.Network) > wipicrt.MaxPendingNetCallbacks ||
		len(saved.Clips) > wipicrt.MaxWIPICMediaClips || len(saved.ClipOrder) != len(saved.Clips) || len(saved.MutedSources) > wipicrt.MaxWIPICMutedSources {
		return nil, fmt.Errorf("SKT Clet runtime state exceeds its limits")
	}
	if saved.CodeCursor < uint64(wipicrt.PlatformCodeBase) || saved.CodeCursor > uint64(wipicrt.PlatformCodeBase)+wipicrt.PlatformCodeSize || saved.CodeCursor&3 != 0 {
		return nil, fmt.Errorf("SKT Clet stub cursor is invalid")
	}
	if saved.ClockAge <= -maxDeadlineOffset || saved.ClockAge >= maxDeadlineOffset {
		return nil, fmt.Errorf("SKT Clet clock anchor is invalid")
	}
	memory := client.Core().Memory()
	runtime := &initializationRuntime{client: client}
	runtime.Runtime = &wipicrt.Runtime{
		Hooks:                 runtime.hooks(),
		Client:                client.Client,
		CollectAt:             wipicrt.CollectionFloor,
		CodeCursor:            saved.CodeCursor,
		Stubs:                 make(map[uint64]uint32, len(saved.Stubs)),
		StubKeys:              make(map[uint32]uint64, len(saved.Stubs)),
		WIPICAllocations:      make(map[uint32]uint64, len(saved.WIPICAllocations)),
		ImageSourceBuffers:    make(map[uint32]uint32, len(saved.ImageSourceBuffers)),
		ReleasedWIPIC:         make(map[uint32]struct{}, len(saved.ReleasedWIPIC)),
		ReleasedWIPICOrder:    slices.Clone(saved.ReleasedWIPICOrder),
		FramebufferOpacity:    make(map[uint32]*wipicrt.ImageOpacity, len(saved.FramebufferOpacity)),
		WIPICClips:            make(map[uint32]*wipicrt.WIPICMediaClip, len(saved.Clips)),
		WIPICClipOrder:        slices.Clone(saved.ClipOrder),
		WIPICMutedSources:     make(map[uint32]bool, len(saved.MutedSources)),
		CFiles:                make(map[string]*wipicrt.RuntimeCFile),
		CFileHandles:          make(map[uint32]*wipicrt.RuntimeCFileHandle),
		DetachedCFiles:        make(map[string]*wipicrt.RuntimeCFile),
		AOTCallDepth:          make(map[*armcore.Thread]uint32),
		GuestFiles:            make(map[string][]byte),
		GuestFileWrites:       make(map[string]uint64),
		UserMemoryPools:       make(map[uint32]*wipicrt.UserMemoryPool, len(saved.UserMemoryPools)),
		ScreenFramebuffer:     saved.ScreenFramebuffer,
		UserMemoryInterface:   saved.UserMemory,
		InputModeTableAddress: saved.InputModeTable,
		Leds:                  saved.LEDs,
		VirtualBaseMillis:     saved.VirtualBaseMillis,
		ClockBase:             now.Add(-saved.ClockAge),
		GuestEventLoop:        saved.GuestEventLoop,
		RepaintPending:        saved.RepaintPending,
		GuestFlushedOwnFrame:  saved.GuestFlushedOwn,
		Trace:                 wipicrt.TraceRing{Limit: client.TraceLimit},
	}
	var err error
	if runtime.Arena, err = restoreArenaState(saved.Arena, wipicrt.PlatformDataBase, wipicrt.PlatformDataSize); err != nil {
		return nil, err
	}
	stubSpans := make([]wipicrt.ArenaBlock, 0, len(saved.Stubs))
	for index, stub := range saved.Stubs {
		start := uint64(stub.Address &^ 1)
		if stub.Key>>32 > 0xff || stub.Address&1 == 0 || start&3 != 0 || start < uint64(wipicrt.PlatformCodeBase) || start+wipicrt.SVCStubSize > saved.CodeCursor ||
			index > 0 && saved.Stubs[index-1].Key >= stub.Key {
			return nil, fmt.Errorf("SKT Clet stub record is invalid")
		}
		if _, taken := runtime.StubKeys[stub.Address]; taken {
			return nil, fmt.Errorf("SKT Clet stub address is shared")
		}
		runtime.Stubs[stub.Key] = stub.Address
		runtime.StubKeys[stub.Address] = stub.Key
		stubSpans = append(stubSpans, wipicrt.ArenaBlock{Start: start, End: start + wipicrt.SVCStubSize})
	}
	if err := validateSpans(stubSpans); err != nil {
		return nil, err
	}
	allocations := make([]wipicrt.ArenaBlock, 0, len(saved.WIPICAllocations))
	for index, record := range saved.WIPICAllocations {
		end := uint64(record.Address) + wipicrt.AlignArenaSize(record.Size)
		if record.Size <= uint64(wipicrt.WIPICAllocationOverhead) || record.Size > wipicrt.MaxPlatformAllocation || record.Address < wipicrt.PlatformDataBase || record.Address&3 != 0 ||
			end > saved.Arena.Cursor || index > 0 && saved.WIPICAllocations[index-1].Address >= record.Address {
			return nil, fmt.Errorf("SKT Clet memory blocks are invalid, repeated or unordered")
		}
		free, _ := slices.BinarySearchFunc(saved.Arena.Free, uint64(record.Address), func(block arenaBlockState, start uint64) int { return cmp.Compare(block.End, start) })
		for free < len(saved.Arena.Free) && saved.Arena.Free[free].End <= uint64(record.Address) {
			free++
		}
		if free < len(saved.Arena.Free) && saved.Arena.Free[free].Start < end {
			return nil, fmt.Errorf("SKT Clet memory block overlaps free space")
		}
		runtime.WIPICAllocations[record.Address] = record.Size
		allocations = append(allocations, wipicrt.ArenaBlock{Start: uint64(record.Address), End: end})
	}
	if err := validateSpans(allocations); err != nil {
		return nil, err
	}
	for index, pair := range saved.ImageSourceBuffers {
		if index > 0 && saved.ImageSourceBuffers[index-1].Address >= pair.Address || runtime.WIPICAllocations[pair.Address] == 0 || runtime.WIPICAllocations[pair.Target] == 0 {
			return nil, fmt.Errorf("SKT Clet image source ownership has no live block")
		}
		runtime.ImageSourceBuffers[pair.Address] = pair.Target
	}
	released := func(address uint32) bool {
		return address >= wipicrt.PlatformDataBase && uint64(address) < saved.Arena.HighWater && address&3 == 0
	}
	for index, address := range saved.ReleasedWIPIC {
		if !released(address) || runtime.WIPICAllocations[address] != 0 || index > 0 && saved.ReleasedWIPIC[index-1] >= address || !slices.Contains(saved.ReleasedWIPICOrder, address) {
			return nil, fmt.Errorf("SKT Clet released block set is invalid")
		}
		runtime.ReleasedWIPIC[address] = struct{}{}
	}
	for _, address := range saved.ReleasedWIPICOrder {
		if !released(address) {
			return nil, fmt.Errorf("SKT Clet released block order is invalid")
		}
	}
	for index, pool := range saved.UserMemoryPools {
		if pool.Base == 0 || pool.Size == 0 || pool.Cursor > pool.Size || index > 0 && saved.UserMemoryPools[index-1].Base >= pool.Base {
			return nil, fmt.Errorf("SKT Clet user memory pool is invalid")
		}
		if err := memory.ValidateRange(pool.Base, uint64(pool.Size), armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
		runtime.UserMemoryPools[pool.Base] = &wipicrt.UserMemoryPool{Base: pool.Base, Size: pool.Size, Cursor: pool.Cursor, FreeCalls: pool.FreeCalls}
	}
	if cache := saved.PixelOp; cache != nil {
		if cache.Function == 0 {
			return nil, fmt.Errorf("SKT Clet pixel operation cache has no function")
		}
		if err := memory.ValidateRange(cache.Function&^1, 2, armcore.PermissionExecute); err != nil {
			return nil, err
		}
		runtime.PixelOps = &wipicrt.PixelOpCache{Function: cache.Function, Param: cache.Param, Results: make(map[uint32]uint16, len(cache.Results))}
		for index, record := range cache.Results {
			if index > 0 && cache.Results[index-1].Pair >= record.Pair {
				return nil, fmt.Errorf("SKT Clet pixel results are repeated or unordered")
			}
			runtime.PixelOps.Results[record.Pair] = record.Result
		}
	}
	for _, address := range []uint32{saved.ScreenFramebuffer, saved.UserMemory, saved.InputModeTable} {
		if address == 0 {
			continue
		}
		if address&3 != 0 {
			return nil, fmt.Errorf("SKT Clet runtime binding is not word-aligned")
		}
		if err := memory.ValidateRange(address, 4, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
	}
	masks := make([]*wipicrt.ImageOpacity, len(saved.Opacities))
	var maskBytes uint64
	for index, record := range saved.Opacities {
		if record.Width <= 0 || record.Height <= 0 || record.Width > wipicrt.MaxWIPICFramebufferSide || record.Height > wipicrt.MaxWIPICFramebufferSide ||
			uint64(len(record.Opaque)) != uint64(record.Width)*uint64(record.Height) {
			return nil, fmt.Errorf("SKT Clet opacity mask shape is invalid")
		}
		maskBytes += uint64(len(record.Opaque))
		if maskBytes > maxOpacityBytes {
			return nil, fmt.Errorf("SKT Clet opacity masks exceed their limit")
		}
		mask := &wipicrt.ImageOpacity{Width: record.Width, Height: record.Height, Opaque: make([]bool, len(record.Opaque))}
		for pixel, value := range record.Opaque {
			if value > 1 {
				return nil, fmt.Errorf("SKT Clet opacity mask value is invalid")
			}
			mask.Opaque[pixel] = value == 1
		}
		masks[index] = mask
	}
	used := make([]bool, len(masks))
	for index, record := range saved.FramebufferOpacity {
		if record.Opacity == 0 || uint64(record.Opacity) > uint64(len(masks)) || record.Handle == 0 ||
			index > 0 && saved.FramebufferOpacity[index-1].Handle >= record.Handle {
			return nil, fmt.Errorf("SKT Clet framebuffer opacity record is invalid")
		}
		runtime.FramebufferOpacity[record.Handle] = masks[record.Opacity-1]
		used[record.Opacity-1] = true
	}
	if slices.Contains(used, false) {
		return nil, fmt.Errorf("SKT Clet opacity mask is held by nothing")
	}
	for _, timer := range saved.Timers {
		if !timer.Due.Set || !timer.Due.valid() {
			return nil, fmt.Errorf("SKT Clet timer record is invalid")
		}
		// A record whose callback word was null arms a timer the next round
		// drops without running, so a null callback is part of the record.
		if timer.Callback != 0 {
			if err := memory.ValidateRange(timer.Callback&^1, 2, armcore.PermissionExecute); err != nil {
				return nil, err
			}
		}
		runtime.PendingTimers = append(runtime.PendingTimers, wipicrt.WIPICTimer{Pointer: timer.Pointer, Callback: timer.Callback, Param: timer.Param, Delay: timer.Delay, Due: timer.Due.restore(now)})
	}
	for _, event := range saved.Events {
		runtime.Events = append(runtime.Events, wipicrt.GuestEvent{Kind: event[0], Param1: event[1], Param2: event[2], Param3: event[3]})
	}
	for _, callback := range saved.Network {
		if callback.Callback == 0 {
			return nil, fmt.Errorf("SKT Clet network callback is null")
		}
		if err := memory.ValidateRange(callback.Callback&^1, 2, armcore.PermissionExecute); err != nil {
			return nil, err
		}
		runtime.PendingNetCallbacks = append(runtime.PendingNetCallbacks, wipicrt.WIPICNetCallback{Callback: callback.Callback, Param: callback.Param})
	}
	if err := runtime.restoreInputRecord(saved.CInput); err != nil {
		return nil, err
	}
	handles := make(map[backend.AudioHandle]bool)
	if audio != nil {
		for _, sound := range audio.Sounds {
			handles[sound.Handle] = true
		}
	}
	for index, clip := range saved.Clips {
		if clip.Address == 0 || clip.Address&3 != 0 || index > 0 && saved.Clips[index-1].Address >= clip.Address || len(clip.MediaType) > 64 ||
			bytes.IndexByte(clip.MediaType, 0) >= 0 || len(clip.Data) > wipicrt.MaxClipBufferBytes || clip.Loaded && (clip.Handle == 0 || !handles[clip.Handle]) ||
			clip.Volume < 0 || clip.Volume > wipicrt.WIPICClipFullVolume {
			return nil, fmt.Errorf("SKT Clet clip has an invalid address, sound or level")
		}
		if err := memory.ValidateRange(clip.Address, wipicrt.WIPICMediaClipRecordSize, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
		runtime.WIPICClips[clip.Address] = &wipicrt.WIPICMediaClip{MediaType: string(clip.MediaType), Volume: clip.Volume,
			State: wipicrt.ClipState{Data: bytes.Clone(clip.Data), Handle: clip.Handle, Loaded: clip.Loaded, Played: clip.Played, Completed: clip.Completed}}
	}
	order := make(map[uint32]bool, len(saved.ClipOrder))
	for _, address := range saved.ClipOrder {
		if order[address] || runtime.WIPICClips[address] == nil {
			return nil, fmt.Errorf("SKT Clet clip eviction order is invalid")
		}
		order[address] = true
	}
	for index, source := range saved.MutedSources {
		if index > 0 && saved.MutedSources[index-1] >= source {
			return nil, fmt.Errorf("SKT Clet muted sources are not in increasing order")
		}
		runtime.WIPICMutedSources[source] = true
	}
	runtime.RebaseArenaShadow()
	return runtime, nil
}

// restoreInputRecord adopts the input method's record. Whether the title has
// used its input method at all is not part of it; see cInputUsed.
func (runtime *initializationRuntime) restoreInputRecord(input cInputRecord) error {
	if input.Mode >= uint32(len(wipicrt.InputModes)) || len(input.Pending) > 2 {
		return fmt.Errorf("SKT Clet input method record is invalid")
	}
	memory := runtime.client.Core().Memory()
	for _, address := range []uint32{input.OwnerAddress, input.TimerPointer} {
		if address == 0 {
			continue
		}
		if address&3 != 0 {
			return fmt.Errorf("SKT Clet input method pointer is not word-aligned")
		}
		if err := memory.ValidateRange(address, 4, armcore.PermissionRead); err != nil {
			return err
		}
	}
	if input.TimerCallback != 0 {
		if err := memory.ValidateRange(input.TimerCallback&^1, 2, armcore.PermissionExecute); err != nil {
			return err
		}
	}
	runtime.CInput = wipicrt.CInputState{Owner: wipicrt.CInputOwner{Address: input.OwnerAddress, Value: input.OwnerValue}, Mode: input.Mode,
		Revision: input.Revision, Calls: input.Calls, Activations: input.Activations, Active: input.Active,
		DiscardCarrier: input.DiscardCarrier, ClearPending: input.ClearPending, Pending: bytes.Clone(input.Pending),
		TimerPointer: input.TimerPointer, TimerCallback: input.TimerCallback}
	return nil
}

func validateSpans(spans []wipicrt.ArenaBlock) error {
	slices.SortFunc(spans, func(a, b wipicrt.ArenaBlock) int { return cmp.Compare(a.Start, b.Start) })
	for index := 1; index < len(spans); index++ {
		if spans[index-1].End > spans[index].Start {
			return fmt.Errorf("SKT Clet checkpoint spans overlap")
		}
	}
	return nil
}
