package lgt

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// What a checkpoint holds of a running title
//
// A title's state is in two places. Its own half is guest memory and the
// registers of its threads, which the ARM core records as they are. The other
// half is what this platform keeps for it on the Go side — which surface a
// handle names, where an open file's cursor is, what a timer is armed for —
// and none of that is in guest memory to be found. This file is that half: one
// record per table the client holds, written so that a client built from the
// records answers every platform call the way the original would have.
//
// **Nothing here is encoded by reflection over the runtime's own structures.**
// Those hold pointers, maps, channels and functions, and a record made by
// walking them would keep whatever happened to be reachable. Each table is
// copied field by field into a plain record instead, so what a checkpoint
// carries is what this file names, and a field added to the runtime without a
// line here is caught by the test that counts them rather than silently left
// behind.
//
// A string that came from the guest is kept as bytes. A file name is in the
// handset's own encoding and a Java string may hold half of a surrogate pair,
// and the record format refuses text that is not UTF-8.

// ErrCheckpointBusy reports a session that is not at a boundary a checkpoint
// can be taken from. It is not a failure of the session: the same request a
// frame later is answered.
var ErrCheckpointBusy = errors.New("LGT checkpoint requires an idle session boundary")

const (
	// clientStateVersion is the layout of clientState. Version 1 carried the
	// bytes of every open file, database and file stream, and both path lists.
	clientStateVersion = 2
	// maxStateRecords bounds a table whose entries are structures, and
	// maxStatePacked one that is packed into bytes. Both are far above what a
	// title reaches — the heaviest local one tracks a few thousand objects —
	// and exist so a record cannot ask for an allocation its bytes do not pay
	// for.
	maxStateRecords = 1 << 16
	maxStatePacked  = 1 << 22
	// maxStateBytes bounds everything a record holds on the Go side beside
	// guest memory: surface pixels, file and stream contents, sound data.
	maxStateBytes = 96 << 20
	// maxStateScreen bounds each side of the display a record states. Neither
	// Host starts a session larger than this, and the display is the one
	// surface a record may bring that the surface allocator does not have to
	// hold: three Host-side buffers are allocated at its size, so it is held
	// to what a session can have been started with rather than to what a
	// surface can be.
	maxStateScreen = 1024
)

type arenaSpanState struct{ Start, End uint64 }

// arenaState is one allocator: how far it has handed out, what it has taken
// back, and every block still outstanding, packed as address and size pairs.
type arenaState struct {
	Cursor uint64
	Free   []arenaSpanState
	Blocks []byte
}

type stubState struct{ Category, Slot, Address uint32 }

// surfaceState is one drawable surface. Its pixels are in the record only when
// the runtime's copy differs from what guest memory holds at the same address,
// which for a Clet's surfaces it rarely does: the guest half is already in the
// memory image, and keeping it twice doubles the largest part of a record.
type surfaceState struct {
	Handle, Address uint32
	Width, Height   int
	Guest           bool
	Pixels          []byte
	Opaque          []byte
	ColorKeyed      bool
	TransparentKey  uint16
	DrawnHere       bool
}

type timerState struct {
	Structure, Callback, Param uint32
	DueAt                      time.Duration
	Armed                      bool
}

type pixelOpState struct {
	Function, Param uint32
	// Results is the answers the guest has already given, packed as a pair of
	// pixels and the pixel it answered. Keeping them is what keeps a restored
	// title from being asked again — and charged the instructions again.
	Results []byte
}

type netConnectState struct {
	Offline         bool
	Generation      uint64
	Callback, Param uint32
	DueAt           time.Duration
}

type eventState struct{ Kind, Param1, Param2 uint32 }

type clipState struct {
	Handle, Callback, Status uint32
	Pending                  []uint32
	MediaType, Data          []byte
	Volume                   int32
	Sound                    uint32
	Loaded, Paused, Repeat   bool
	Listener                 uint32
}

type levelState struct {
	Source uint32
	Level  int32
}

// fileState is one open file: which handle it is, of which path, at which
// cursor. What the file holds is not here. A checkpoint is taken with every
// buffer stored, so the bytes are in the store, and a load reads them from it.
type fileState struct {
	Handle   uint32
	Name     []byte
	Cursor   int
	Writable bool
	// Truncated says the handle's open asked for an empty file, and Length is
	// how many bytes it held when the checkpoint was taken. A load gives such
	// a handle at most that much of the file as it is then; see
	// openFile.truncated. Length is zero for any other handle.
	Truncated bool
	Length    int
}

type textInputState struct {
	Active          bool
	Revision, Calls uint64
	Pending         []byte
}

type resourceState struct {
	Name   []byte
	Handle uint32
}

// clientState is everything one loaded title holds outside its save files.
type clientState struct {
	Version    uint32
	ModuleHash [32]byte
	Width      int
	Height     int
	Subscriber []byte

	Core   armcore.CoreState
	Thread armcore.RootThreadState

	Arena, Surfaces, Heap arenaState
	CodeCursor            uint32
	Stubs                 []stubState
	Clet                  CletFunctions

	Screen       surfaceState
	Framebuffers []surfaceState
	Presented    []byte
	Flushes      uint64
	NextHandle   uint32

	Timers            []timerState
	PixelOps          []pixelOpState
	InstalledPixelOps []uint32
	NetConnects       []netConnectState
	NetGeneration     uint64
	Events            []eventState

	Elapsed  time.Duration
	Baseline uint64

	Audio        backend.AudioState
	Clips        []clipState
	Volume       int32
	SourceVolume []levelState
	SourceMuted  []uint32
	Vibration    backend.VibrationState

	// Files are the open handles. The two path lists a title's file calls
	// keep are not in a record at all: a load reads both from the store.
	Files []fileState

	TimeStorage, StrtokScan uint32
	Random                  *guestRandomState
	InputMode               uint32
	InputModeTable          uint32
	TextInput               textInputState
	ApplicationID           uint32
	Resources               []resourceState
	Imports                 []byte

	JavaApplication bool
	Java            *javaState
}

// stateBudget counts the bytes a record holds, so a table that would take the
// whole of it is refused at capture rather than written and refused at load.
type stateBudget struct{ used uint64 }

func (budget *stateBudget) charge(size int) error {
	if size < 0 || uint64(size) > maxStateBytes-budget.used {
		return fmt.Errorf("LGT checkpoint payload exceeds %d bytes", maxStateBytes)
	}
	budget.used += uint64(size)
	return nil
}

func packHalfwords(values []uint16) []byte {
	data := make([]byte, len(values)*2)
	for index, value := range values {
		binary.LittleEndian.PutUint16(data[index*2:], value)
	}
	return data
}

func unpackHalfwords(data []byte) []uint16 {
	values := make([]uint16, len(data)/2)
	for index := range values {
		values[index] = binary.LittleEndian.Uint16(data[index*2:])
	}
	return values
}

func packWords(values []uint32) []byte {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:], value)
	}
	return data
}

func unpackWords(data []byte) []uint32 {
	values := make([]uint32, len(data)/4)
	for index := range values {
		values[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return values
}

func packBools(values []bool) []byte {
	data := make([]byte, (len(values)+7)/8)
	for index, value := range values {
		if value {
			data[index/8] |= 1 << (index % 8)
		}
	}
	return data
}

func unpackBools(data []byte, count int) []bool {
	values := make([]bool, count)
	for index := range values {
		values[index] = data[index/8]&(1<<(index%8)) != 0
	}
	return values
}

func (a *arena) captureState() arenaState {
	saved := arenaState{Cursor: a.cursor}
	for _, block := range a.free {
		saved.Free = append(saved.Free, arenaSpanState{block.start, block.end})
	}
	saved.Blocks = make([]byte, 0, len(a.sizes)*8)
	for _, address := range slices.Sorted(maps.Keys(a.sizes)) {
		saved.Blocks = binary.LittleEndian.AppendUint32(saved.Blocks, address)
		saved.Blocks = binary.LittleEndian.AppendUint32(saved.Blocks, uint32(a.sizes[address]))
	}
	return saved
}

// restoreArena rebuilds an allocator and checks that what it was handed is one:
// the blocks outstanding and the spans taken back have to cover exactly what
// the cursor says has been handed out, with nothing counted twice. An arena
// that passes hands out the same addresses the original would have next.
func restoreArena(saved arenaState, base uint32, size uint64) (*arena, error) {
	invalid := func() (*arena, error) {
		return nil, fmt.Errorf("LGT checkpoint arena at %#x has invalid blocks or spans", base)
	}
	restored := newArena(base, size)
	// A block is at least one unit of alignment, and the spans taken back are
	// kept joined, so there is at most one more of them than there are blocks.
	// Both follow from the covering check below; they are asked first so that
	// the tables it is made from are never larger than the region allows.
	blocks := len(saved.Blocks) / 8
	if saved.Cursor < uint64(base) || saved.Cursor > restored.limit || saved.Cursor%arenaAlignment != 0 ||
		len(saved.Blocks)%8 != 0 || uint64(blocks) > size/arenaAlignment || len(saved.Free) > blocks+1 {
		return invalid()
	}
	restored.cursor = saved.Cursor
	type span struct{ start, end uint64 }
	spans := make([]span, 0, blocks+len(saved.Free))
	for offset := 0; offset < len(saved.Blocks); offset += 8 {
		address := binary.LittleEndian.Uint32(saved.Blocks[offset:])
		length := uint64(binary.LittleEndian.Uint32(saved.Blocks[offset+4:]))
		if length == 0 || length%arenaAlignment != 0 {
			return invalid()
		}
		if _, duplicate := restored.sizes[address]; duplicate {
			return invalid()
		}
		restored.sizes[address] = length
		restored.outstanding += length
		spans = append(spans, span{uint64(address), uint64(address) + length})
	}
	for index, block := range saved.Free {
		// The free list is kept sorted and coalesced, and the next allocation
		// depends on its order, so it is taken as it was written.
		if block.End <= block.Start || index > 0 && saved.Free[index-1].End >= block.Start {
			return invalid()
		}
		restored.free = append(restored.free, arenaBlock{start: block.Start, end: block.End})
		spans = append(spans, span{block.Start, block.End})
	}
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	cursor := uint64(base)
	for _, covered := range spans {
		if covered.start != cursor {
			return invalid()
		}
		cursor = covered.end
	}
	if cursor != saved.Cursor {
		return invalid()
	}
	return restored, nil
}

// validateSurfaces checks every surface against the allocators it came out of:
// its pixels are one block of the surface region, large enough for its size,
// and its handle is one block of the platform's data. No two surfaces share
// either. A surface is the one table whose size a record states without
// carrying the bytes — its pixels may be the guest's own — so this is what
// keeps a record from claiming more pixels than the region it names can hold.
func validateSurfaces(screen surfaceState, others []surfaceState, data, pixels *arena) error {
	invalid := fmt.Errorf("LGT checkpoint has a surface outside the blocks the allocators handed out")
	addresses := make(map[uint32]bool, len(others)+1)
	check := func(surface surfaceState) bool {
		if surface.Address == 0 {
			return surface.Handle == 0
		}
		size := uint64(surface.Width) * uint64(surface.Height) * 2
		return !addresses[surface.Address] && pixels.sizes[surface.Address] >= size &&
			data.sizes[surface.Handle] >= surfaceRecordBytes
	}
	if !check(screen) {
		return invalid
	}
	addresses[screen.Address] = true
	for _, surface := range others {
		if !check(surface) {
			return invalid
		}
		addresses[surface.Address] = true
	}
	return nil
}

// checkpointIdle reports whether the client is between rounds with nothing of
// the guest's in flight on the platform's own thread. A guest thread parked in
// its own call is not "in flight": that is where every one of them is at a
// boundary, and what checkpoint_worker.go records.
func (client *Client) checkpointIdle() error {
	if client == nil || client.core == nil || client.thread == nil || client.archive == nil {
		return fmt.Errorf("LGT checkpoint session is not started")
	}
	if client.exited {
		return fmt.Errorf("LGT checkpoint cannot capture a title that has exited")
	}
	if client.saveReadError != nil {
		return fmt.Errorf("LGT checkpoint cannot capture after a failed save read: %w", client.saveReadError)
	}
	busy := client.javaThreadsStopped || client.activeJavaWorker != nil || client.javaCallDepth != 0 ||
		len(client.javaTry) != 0 || client.collecting || len(client.thread.LiveContexts()) != 1
	if run := client.javaRun; run != nil {
		busy = busy || run.keyCallback || len(run.pins) != 0
	}
	if busy {
		return ErrCheckpointBusy
	}
	return nil
}

func (client *Client) captureSurface(buffer *framebuffer, budget *stateBudget) (surfaceState, error) {
	if buffer == nil || buffer.width <= 0 || buffer.height <= 0 || len(buffer.pixels) != buffer.width*buffer.height ||
		buffer.opaque != nil && len(buffer.opaque) != len(buffer.pixels) {
		return surfaceState{}, fmt.Errorf("LGT checkpoint has a surface with an invalid shape")
	}
	saved := surfaceState{
		Handle: buffer.handle, Address: buffer.address, Width: buffer.width, Height: buffer.height,
		ColorKeyed: buffer.colorKeyed, TransparentKey: buffer.transparentKey, DrawnHere: buffer.drawnHere,
	}
	if buffer.address != 0 {
		guest := make([]uint16, len(buffer.pixels))
		if err := client.core.Memory().ReadHalfwords(buffer.address, guest); err == nil && slices.Equal(guest, buffer.pixels) {
			saved.Guest = true
		}
	}
	if !saved.Guest {
		if err := budget.charge(len(buffer.pixels) * 2); err != nil {
			return surfaceState{}, err
		}
		saved.Pixels = packHalfwords(buffer.pixels)
	}
	if buffer.opaque != nil {
		if err := budget.charge(len(buffer.opaque) / 8); err != nil {
			return surfaceState{}, err
		}
		saved.Opaque = packBools(buffer.opaque)
	}
	return saved, nil
}

func (saved surfaceState) validate(screen bool) error {
	invalid := func() error { return fmt.Errorf("LGT checkpoint has a surface with an invalid shape or address") }
	if saved.Width <= 0 || saved.Height <= 0 || saved.Width > 4096 || saved.Height > 4096 {
		return invalid()
	}
	count := saved.Width * saved.Height
	if saved.Guest != (saved.Pixels == nil) || !saved.Guest && len(saved.Pixels) != count*2 ||
		saved.Guest && saved.Address == 0 || saved.Opaque != nil && len(saved.Opaque) != (count+7)/8 {
		return invalid()
	}
	// The display is the one surface that exists before anything has asked for
	// it, with no handle and no guest pixels. Every other one has both.
	if (saved.Handle == 0) != (saved.Address == 0) || !screen && saved.Handle == 0 {
		return invalid()
	}
	return nil
}

func (client *Client) restoreSurface(saved surfaceState, screen bool) (*framebuffer, error) {
	if err := saved.validate(screen); err != nil {
		return nil, err
	}
	count := saved.Width * saved.Height
	buffer := &framebuffer{
		handle: saved.Handle, width: saved.Width, height: saved.Height, address: saved.Address,
		screen: screen, colorKeyed: saved.ColorKeyed, transparentKey: saved.TransparentKey, drawnHere: saved.DrawnHere,
	}
	if saved.Address != 0 {
		if err := client.core.Memory().ValidateRange(saved.Address, uint64(count)*2, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
		if err := client.core.Memory().ValidateRange(saved.Handle, surfaceRecordBytes, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
	}
	if saved.Guest {
		buffer.pixels = make([]uint16, count)
		if err := client.core.Memory().ReadHalfwords(saved.Address, buffer.pixels); err != nil {
			return nil, err
		}
	} else {
		buffer.pixels = unpackHalfwords(saved.Pixels)
	}
	if saved.Opaque != nil {
		buffer.opaque = unpackBools(saved.Opaque, count)
	}
	return buffer, nil
}

// captureClientState copies every table the client holds. The caller has
// already established the boundary; this takes the client's own lock for the
// fields a Host goroutine may read beside it.
func (client *Client) captureClientState() (clientState, error) {
	if err := client.checkpointIdle(); err != nil {
		return clientState{}, err
	}
	budget := &stateBudget{}
	saved := clientState{
		Version: clientStateVersion, ModuleHash: sha256.Sum256(client.archive.Module),
		Subscriber: []byte(client.subscriberNumber),
		Arena:      client.arena.captureState(), Surfaces: client.surfaces.captureState(), Heap: client.heap.captureState(),
		Volume: client.volume, TimeStorage: client.tmStorage, StrtokScan: client.strtokScan,
		InputMode: client.inputMode, InputModeTable: client.inputModeTableAddress,
		ApplicationID: client.applicationIDAddress, JavaApplication: client.javaApplication,
	}
	var err error
	if saved.Core, err = client.core.CaptureState(); err != nil {
		return clientState{}, err
	}
	if saved.Thread, err = client.core.CaptureRootThread(client.thread); err != nil {
		return clientState{}, err
	}
	if saved.Audio, err = client.audio.CaptureState(); err != nil {
		return clientState{}, err
	}
	if saved.Vibration, err = client.vibrator.CaptureState(); err != nil {
		return clientState{}, err
	}
	if client.cRandom != nil {
		random, err := client.cRandom.captureState()
		if err != nil {
			return clientState{}, err
		}
		saved.Random = &random
	}
	client.clock.mu.Lock()
	saved.Elapsed, saved.Baseline = client.clock.elapsed, client.clock.baseline
	client.clock.mu.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()
	saved.Width, saved.Height = client.screen.width, client.screen.height
	saved.CodeCursor, saved.Clet, saved.Flushes, saved.NextHandle = client.codeCurse, client.clet, client.flushes, client.nextHandle
	saved.NetGeneration = client.netGeneration
	for _, key := range slices.Sorted(maps.Keys(client.stubs)) {
		saved.Stubs = append(saved.Stubs, stubState{uint32(key >> 32), uint32(key), client.stubs[key]})
	}
	if saved.Screen, err = client.captureSurface(client.screen, budget); err != nil {
		return clientState{}, err
	}
	for _, handle := range slices.Sorted(maps.Keys(client.framebuffers)) {
		buffer := client.framebuffers[handle]
		if buffer == client.screen {
			continue
		}
		if buffer == nil || buffer.screen || buffer.handle != handle {
			return clientState{}, fmt.Errorf("LGT checkpoint has a surface registered under another handle")
		}
		surface, err := client.captureSurface(buffer, budget)
		if err != nil {
			return clientState{}, err
		}
		saved.Framebuffers = append(saved.Framebuffers, surface)
	}
	if len(client.presented) != len(client.screen.pixels) {
		return clientState{}, fmt.Errorf("LGT checkpoint has a presented frame of another size")
	}
	if err := budget.charge(len(client.presented) * 2); err != nil {
		return clientState{}, err
	}
	saved.Presented = packHalfwords(client.presented)
	for _, structure := range slices.Sorted(maps.Keys(client.timers)) {
		entry := client.timers[structure]
		saved.Timers = append(saved.Timers, timerState{entry.structure, entry.callback, entry.param, entry.dueAt, entry.armed})
	}
	for _, identity := range slices.Sorted(maps.Keys(client.pixelOps)) {
		cache := client.pixelOps[identity]
		record := pixelOpState{Function: uint32(identity >> 32), Param: uint32(identity), Results: make([]byte, 0, len(cache.results)*6)}
		for _, key := range slices.Sorted(maps.Keys(cache.results)) {
			record.Results = binary.LittleEndian.AppendUint32(record.Results, key)
			record.Results = binary.LittleEndian.AppendUint16(record.Results, cache.results[key])
		}
		if err := budget.charge(len(record.Results)); err != nil {
			return clientState{}, err
		}
		saved.PixelOps = append(saved.PixelOps, record)
	}
	for _, function := range slices.Sorted(maps.Keys(client.installedPixelOps)) {
		if client.installedPixelOps[function] {
			saved.InstalledPixelOps = append(saved.InstalledPixelOps, function)
		}
	}
	for _, entry := range client.netConnects {
		saved.NetConnects = append(saved.NetConnects, netConnectState{entry.offline, entry.generation, entry.callback, entry.param, entry.dueAt})
	}
	for _, event := range client.events {
		saved.Events = append(saved.Events, eventState{event.kind, event.param1, event.param2})
	}
	for _, handle := range slices.Sorted(maps.Keys(client.clips)) {
		clip := client.clips[handle]
		if clip == nil {
			return clientState{}, fmt.Errorf("LGT checkpoint has a missing clip")
		}
		if err := budget.charge(len(clip.data) + len(clip.mediaType)); err != nil {
			return clientState{}, err
		}
		saved.Clips = append(saved.Clips, clipState{
			Handle: handle, Callback: clip.callback, Status: clip.status, Pending: slices.Clone(clip.pending),
			MediaType: []byte(clip.mediaType), Data: bytes.Clone(clip.data), Volume: clip.volume,
			Sound: uint32(clip.handle), Loaded: clip.loaded, Paused: clip.javaPaused, Repeat: clip.javaRepeat,
			Listener: clip.listener,
		})
	}
	for _, source := range slices.Sorted(maps.Keys(client.sourceVolume)) {
		saved.SourceVolume = append(saved.SourceVolume, levelState{source, client.sourceVolume[source]})
	}
	for _, source := range slices.Sorted(maps.Keys(client.sourceMuted)) {
		if client.sourceMuted[source] {
			saved.SourceMuted = append(saved.SourceMuted, source)
		}
	}
	for _, handle := range slices.Sorted(maps.Keys(client.files)) {
		file := client.files[handle]
		if file == nil {
			return clientState{}, fmt.Errorf("LGT checkpoint has a missing open file")
		}
		// A record holds no file bytes, so a write the store has not been
		// given would be in no place at all once this session is gone. The
		// session capture stores them before it reaches here.
		if file.dirty {
			return clientState{}, fmt.Errorf("LGT checkpoint has an open file with writes the store has not been given")
		}
		// What the handle holds is charged although it is not recorded: a
		// load reads it back, on the same budget.
		if err := budget.charge(len(file.data) + len(file.name)); err != nil {
			return clientState{}, err
		}
		record := fileState{Handle: handle, Name: []byte(file.name), Cursor: file.cursor, Writable: file.writable, Truncated: file.truncated}
		if file.truncated {
			record.Length = len(file.data)
		}
		saved.Files = append(saved.Files, record)
	}
	if client.cTextInput.delivering {
		return clientState{}, ErrCheckpointBusy
	}
	saved.TextInput = textInputState{client.cTextInput.active, client.cTextInput.revision, client.cTextInput.calls, bytes.Clone(client.cTextInput.pending)}
	names := slices.Sorted(maps.Keys(client.resourceIDs))
	for _, name := range names {
		handle := client.resourceIDs[name]
		if client.resourceNames[handle] != name {
			return clientState{}, fmt.Errorf("LGT checkpoint resource tables disagree")
		}
		if err := budget.charge(len(name)); err != nil {
			return clientState{}, err
		}
		saved.Resources = append(saved.Resources, resourceState{[]byte(name), handle})
	}
	if len(client.resourceNames) != len(names) {
		return clientState{}, fmt.Errorf("LGT checkpoint resource tables disagree")
	}
	imports := make([][2]uint32, 0, len(client.imports))
	for key, resolved := range client.imports {
		if resolved {
			imports = append(imports, key)
		}
	}
	slices.SortFunc(imports, func(a, b [2]uint32) int {
		if a[0] != b[0] {
			return cmp.Compare(a[0], b[0])
		}
		return cmp.Compare(a[1], b[1])
	})
	for _, key := range imports {
		saved.Imports = binary.LittleEndian.AppendUint32(saved.Imports, key[0])
		saved.Imports = binary.LittleEndian.AppendUint32(saved.Imports, key[1])
	}
	if client.javaRun != nil {
		java, err := client.captureJavaState(budget)
		if err != nil {
			return clientState{}, err
		}
		saved.Java = java
	}
	if err := saved.validate(); err != nil {
		return clientState{}, err
	}
	if err := validateSurfaces(saved.Screen, saved.Framebuffers, client.arena, client.surfaces); err != nil {
		return clientState{}, err
	}
	return saved, nil
}

// validate checks what a record claims before anything is built from it. What
// depends on the restored memory image or on the Java tables is checked where
// those exist; this is the part that needs only the record.
func (saved clientState) validate() error {
	invalid := func(what string) error { return fmt.Errorf("LGT checkpoint has %s", what) }
	if saved.Version != clientStateVersion {
		return invalid("an unsupported client version")
	}
	if saved.Width <= 0 || saved.Height <= 0 || saved.Width > maxStateScreen || saved.Height > maxStateScreen ||
		saved.Screen.Width != saved.Width || saved.Screen.Height != saved.Height ||
		len(saved.Presented) != saved.Width*saved.Height*2 {
		return invalid("an invalid screen")
	}
	if len(saved.Subscriber) > 64 || !validPrintable(saved.Subscriber) {
		return invalid("an invalid subscriber identity")
	}
	for _, count := range []int{len(saved.Stubs), len(saved.Framebuffers), len(saved.Timers), len(saved.Clips),
		len(saved.SourceVolume), len(saved.SourceMuted), len(saved.Files), len(saved.Resources)} {
		if count > maxStateRecords {
			return invalid("a table past its limit")
		}
	}
	if len(saved.PixelOps) > maxCachedPixelOps || len(saved.InstalledPixelOps) > maxInstalledPixelOps ||
		len(saved.NetConnects) > 64 || len(saved.Events) > maxQueuedEvents || len(saved.Imports)%8 != 0 ||
		len(saved.Imports)/8 > maxStateRecords {
		return invalid("a queue or cache past its limit")
	}
	sp := saved.Thread.Context.Registers[armcore.RegisterSP]
	if saved.Thread.StepBudget != 0 || sp&3 != 0 || sp < stackBase || uint64(sp) > uint64(stackBase)+stackSize ||
		len(saved.Core.Memory.ThreadLocal) != 0 || len(saved.Thread.ThreadLocal) != 0 {
		return invalid("an invalid platform thread")
	}
	now, ok := saved.guestNow()
	if !ok {
		return invalid("an invalid guest clock")
	}
	code := uint64(saved.CodeCursor)
	if code < uint64(platformCodeBase) || code > uint64(platformCodeBase)+platformCodeSize || (saved.CodeCursor-platformCodeBase)%16 != 0 {
		return invalid("an invalid stub cursor")
	}
	seenStub := make(map[uint32]bool, len(saved.Stubs))
	for index, stub := range saved.Stubs {
		previous := stubState{}
		if index > 0 {
			previous = saved.Stubs[index-1]
		}
		ordered := index == 0 || previous.Category < stub.Category || previous.Category == stub.Category && previous.Slot < stub.Slot
		address := stub.Address &^ 1
		if !ordered || stub.Category > 0xff || stub.Address&1 == 0 || address < platformCodeBase || address >= saved.CodeCursor ||
			(address-platformCodeBase)%16 != 0 || seenStub[address] {
			return invalid("an invalid platform stub")
		}
		seenStub[address] = true
	}
	if err := saved.Screen.validate(true); err != nil {
		return err
	}
	for index, surface := range saved.Framebuffers {
		if index > 0 && saved.Framebuffers[index-1].Handle >= surface.Handle || surface.Handle == saved.Screen.Handle {
			return invalid("surfaces out of order")
		}
		if err := surface.validate(false); err != nil {
			return err
		}
	}
	for index, entry := range saved.Timers {
		if index > 0 && saved.Timers[index-1].Structure >= entry.Structure || entry.Structure == 0 || entry.Armed && entry.Callback == 0 {
			return invalid("an invalid timer")
		}
	}
	for index, cache := range saved.PixelOps {
		if index > 0 {
			previous := saved.PixelOps[index-1]
			if previous.Function > cache.Function || previous.Function == cache.Function && previous.Param >= cache.Param {
				return invalid("pixel operations out of order")
			}
		}
		if cache.Function == 0 || len(cache.Results)%6 != 0 || len(cache.Results)/6 > maxCachedPixelPairs {
			return invalid("an invalid pixel operation cache")
		}
		for offset := 6; offset < len(cache.Results); offset += 6 {
			if binary.LittleEndian.Uint32(cache.Results[offset-6:]) >= binary.LittleEndian.Uint32(cache.Results[offset:]) {
				return invalid("pixel operation answers out of order")
			}
		}
	}
	for index, function := range saved.InstalledPixelOps {
		if index > 0 && saved.InstalledPixelOps[index-1] >= function || function&1 == 0 {
			return invalid("an invalid installed pixel operation")
		}
	}
	for _, entry := range saved.NetConnects {
		if entry.Callback == 0 || entry.Generation > saved.NetGeneration {
			return invalid("an invalid pending dial")
		}
	}
	sounds := make(map[uint32]bool, len(saved.Audio.Sounds))
	for _, sound := range saved.Audio.Sounds {
		sounds[uint32(sound.Handle)] = true
	}
	if err := validateGuestAudio(saved.Audio, now); err != nil {
		return err
	}
	for index, clip := range saved.Clips {
		if index > 0 && saved.Clips[index-1].Handle >= clip.Handle || clip.Handle == 0 || len(clip.Pending) > 64 ||
			clip.Status > mediaResumed || clip.Volume < 0 || clip.Volume > mediaMaxVolume || len(clip.MediaType) > 256 ||
			clip.Loaded && !sounds[clip.Sound] {
			return invalid("an invalid media clip")
		}
		for _, status := range clip.Pending {
			if status == 0 || status > mediaResumed {
				return invalid("an invalid media transition")
			}
		}
	}
	if saved.Volume < 0 || saved.Volume > mediaMaxVolume {
		return invalid("an invalid volume")
	}
	for index, level := range saved.SourceVolume {
		if index > 0 && saved.SourceVolume[index-1].Source >= level.Source || level.Level < 0 || level.Level > mediaMaxVolume {
			return invalid("an invalid source volume")
		}
	}
	for index, source := range saved.SourceMuted {
		if index > 0 && saved.SourceMuted[index-1] >= source {
			return invalid("muted sources out of order")
		}
	}
	if err := saved.Vibration.Validate(); err != nil {
		return err
	}
	for index, file := range saved.Files {
		if index > 0 && saved.Files[index-1].Handle >= file.Handle || file.Handle == 0 || file.Handle >= saved.NextHandle ||
			file.Cursor < 0 || file.Cursor > math.MaxInt32 || len(file.Name) > 4096 || file.Truncated && !file.Writable ||
			file.Length < 0 || file.Length > maxStateBytes || !file.Truncated && file.Length != 0 {
			return invalid("an invalid open file")
		}
	}
	if saved.NextHandle == 0 {
		return invalid("an invalid handle counter")
	}
	if saved.Random != nil {
		if err := saved.Random.validate(); err != nil {
			return err
		}
	}
	if saved.InputMode >= uint32(len(inputModes)) || len(saved.TextInput.Pending) > 4096 {
		return invalid("an invalid input mode or pending text")
	}
	handles := make(map[uint32]bool, len(saved.Resources))
	for index, resource := range saved.Resources {
		if index > 0 && bytes.Compare(saved.Resources[index-1].Name, resource.Name) >= 0 || resource.Handle == 0 ||
			handles[resource.Handle] || len(resource.Name) > 4096 {
			return invalid("an invalid resource identity")
		}
		handles[resource.Handle] = true
	}
	// The other way round is a title that has not built its runtime yet.
	if saved.Java != nil && !saved.JavaApplication {
		return invalid("a Java runtime in a title that is not one")
	}
	return nil
}

const (
	// maxGuestClock bounds the elapsed time a record states, far past any
	// session and far enough inside the range of a duration that adding to it
	// cannot wrap.
	maxGuestClock = 100 * 365 * 24 * time.Hour
	// maxGuestWork bounds the time the instructions retired since the last
	// tick stand for. A tick moves the baseline, so at a boundary this is one
	// tick's work — or, before the first tick, the title's startup — and an
	// hour of it is hundreds of thousands of millions of instructions.
	maxGuestWork = time.Hour
)

// guestNow is what the restored clock reads before its first tick: the elapsed
// time, plus the time the instructions retired since the baseline stand for.
//
// **Both parts are a record's to state, and the first tick adds the second to
// the first.** A record that put its instruction count a long way past its
// baseline would move the clock by years in one tick, and everything that
// catches up with the clock — a repeating sound most of all — would be asked
// to catch up with all of it. So the work is bounded as well as the elapsed
// time, and it is this reading, not the elapsed time alone, that the audio
// timeline is checked against.
func (saved clientState) guestNow() (time.Duration, bool) {
	if saved.Elapsed < 0 || saved.Elapsed > maxGuestClock || saved.Baseline > saved.Core.Steps {
		return 0, false
	}
	work := (saved.Core.Steps - saved.Baseline) / guestInstructionsPerMillisecond
	if work > uint64(maxGuestWork/time.Millisecond) {
		return 0, false
	}
	return saved.Elapsed + time.Duration(work)*time.Millisecond, true
}

func validPrintable(text []byte) bool {
	for _, symbol := range text {
		if symbol < 0x20 || symbol > 0x7e {
			return false
		}
	}
	return true
}

// validateGuestAudio bounds the first restored tick using the shared cursor,
// frozen pause clock, pending payload and active-key work limits.
func validateGuestAudio(saved backend.AudioState, elapsed time.Duration) error {
	if err := backend.ValidateAudioCatchup(saved, elapsed); err != nil {
		return fmt.Errorf("LGT checkpoint: %w", err)
	}
	return nil
}

// expectedMappings is the memory map a client of this archive has once its
// guest threads have been given their stacks. A record may not bring a map of
// its own: what is mapped, and with what permission, is this platform's
// decision. The stacks are mapped on the freshly loaded core rather than
// listed, because the core joins neighbouring mappings and the list has to be
// in the form the core itself would report.
func expectedMappings(fresh *armcore.Core, stacks int) ([]armcore.MappingState, error) {
	for index := 0; index < stacks; index++ {
		base := javaThreadStackBase + uint32(index)*uint32(javaThreadStackSize)
		if err := fresh.Memory().Map(base, javaThreadStackSize, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
	}
	state, err := fresh.CaptureState()
	if err != nil {
		return nil, err
	}
	return state.Memory.Mappings, nil
}

// restoreClientState builds a client from a record, on a freshly loaded one.
// Nothing of the guest's runs: no entry point, no initializer, no startClet.
// The save store is the caller's to attach, and it is an isolated one until
// the load is committed.
func restoreClientState(archive *Archive, saved clientState, options Options) (*Client, error) {
	if err := saved.validate(); err != nil {
		return nil, err
	}
	if saved.ModuleHash != sha256.Sum256(archive.Module) {
		return nil, fmt.Errorf("LGT checkpoint belongs to another module")
	}
	options.Width, options.Height = saved.Width, saved.Height
	client, err := Load(archive, options)
	if err != nil {
		return nil, err
	}
	stacks := 0
	if saved.Java != nil {
		stacks = saved.Java.ThreadStacks
		if stacks < 0 || stacks > maxJavaThreads {
			return nil, fmt.Errorf("LGT checkpoint has an invalid guest thread stack count")
		}
	}
	mappings, err := expectedMappings(client.core, stacks)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(saved.Core.Memory.Mappings, mappings) {
		return nil, fmt.Errorf("LGT checkpoint changes memory mapping policy")
	}
	core, err := armcore.NewCoreFromState(saved.Core, armcore.CoreOptions{MaxSteps: options.MaxSteps, Quantum: cletQuantum})
	if err != nil {
		return nil, err
	}
	thread, err := core.RestoreRootThread(saved.Thread, 0)
	if err != nil {
		return nil, err
	}
	client.core, client.thread = core, thread
	core.SetFastSupervisorCall(client.fastSupervisorCall)
	client.clock = newGuestClock(core.Steps)
	client.clock.elapsed, client.clock.baseline = saved.Elapsed, saved.Baseline
	client.subscriberNumber = string(saved.Subscriber)

	if client.arena, err = restoreArena(saved.Arena, platformDataBase, platformDataSize); err != nil {
		return nil, err
	}
	if client.surfaces, err = restoreArena(saved.Surfaces, surfaceBase, surfaceSize); err != nil {
		return nil, err
	}
	if client.heap, err = restoreArena(saved.Heap, heapBase, heapSize); err != nil {
		return nil, err
	}
	if err := validateSurfaces(saved.Screen, saved.Framebuffers, client.arena, client.surfaces); err != nil {
		return nil, err
	}
	client.codeCurse, client.clet = saved.CodeCursor, saved.Clet
	client.stubs = make(map[uint64]uint32, len(saved.Stubs))
	for _, stub := range saved.Stubs {
		client.stubs[uint64(stub.Category)<<32|uint64(stub.Slot)] = stub.Address
	}
	if client.screen, err = client.restoreSurface(saved.Screen, true); err != nil {
		return nil, err
	}
	client.framebuffers = make(map[uint32]*framebuffer, len(saved.Framebuffers)+1)
	if client.screen.handle != 0 {
		client.framebuffers[client.screen.handle] = client.screen
	}
	for _, record := range saved.Framebuffers {
		buffer, err := client.restoreSurface(record, false)
		if err != nil {
			return nil, err
		}
		client.framebuffers[buffer.handle] = buffer
	}
	// The picture the Host last took is rebuilt from what the display held,
	// and marked as one it has not seen: a restored session's first frame is
	// the whole screen.
	client.presented = unpackHalfwords(saved.Presented)
	client.frameRGBA = make([]byte, len(client.presented)*4)
	client.framePending, client.flushes, client.nextHandle = true, saved.Flushes, saved.NextHandle
	client.timers = make(map[uint32]*timer, len(saved.Timers))
	for _, entry := range saved.Timers {
		client.timers[entry.Structure] = &timer{entry.Structure, entry.Callback, entry.Param, entry.DueAt, entry.Armed}
	}
	if len(saved.PixelOps) != 0 {
		client.pixelOps = make(map[uint64]*pixelOpCache, len(saved.PixelOps))
	}
	for _, record := range saved.PixelOps {
		cache := &pixelOpCache{results: make(map[uint32]uint16, len(record.Results)/6)}
		for offset := 0; offset < len(record.Results); offset += 6 {
			cache.results[binary.LittleEndian.Uint32(record.Results[offset:])] = binary.LittleEndian.Uint16(record.Results[offset+4:])
		}
		client.pixelOps[uint64(record.Function)<<32|uint64(record.Param)] = cache
	}
	if len(saved.InstalledPixelOps) != 0 {
		client.installedPixelOps = make(map[uint32]bool, len(saved.InstalledPixelOps))
		for _, function := range saved.InstalledPixelOps {
			client.installedPixelOps[function] = true
		}
	}
	client.netGeneration = saved.NetGeneration
	for _, entry := range saved.NetConnects {
		client.netConnects = append(client.netConnects, pendingNetConnect{entry.Offline, entry.Generation, entry.Callback, entry.Param, entry.DueAt})
	}
	for _, event := range saved.Events {
		client.events = append(client.events, pendingEvent{event.Kind, event.Param1, event.Param2})
	}
	if client.audio, err = backend.NewAudioFromState(saved.Audio, nil); err != nil {
		return nil, err
	}
	client.audio.SetLogger(client.logger)
	client.volume = saved.Volume
	client.clips = make(map[uint32]*mediaClip, len(saved.Clips))
	for _, clip := range saved.Clips {
		client.clips[clip.Handle] = &mediaClip{
			callback: clip.Callback, status: clip.Status, pending: slices.Clone(clip.Pending),
			mediaType: string(clip.MediaType), data: bytes.Clone(clip.Data), volume: clip.Volume,
			handle: backend.AudioHandle(clip.Sound), loaded: clip.Loaded, javaPaused: clip.Paused,
			javaRepeat: clip.Repeat, listener: clip.Listener,
		}
	}
	if len(saved.SourceVolume) != 0 {
		client.sourceVolume = make(map[uint32]int32, len(saved.SourceVolume))
		for _, level := range saved.SourceVolume {
			client.sourceVolume[level.Source] = level.Level
		}
	}
	if len(saved.SourceMuted) != 0 {
		client.sourceMuted = make(map[uint32]bool, len(saved.SourceMuted))
		for _, source := range saved.SourceMuted {
			client.sourceMuted[source] = true
		}
	}
	if err := client.vibrator.RestoreState(saved.Vibration); err != nil {
		return nil, err
	}
	// The handles come back without their bytes, and the two path lists
	// unread: committing the load fills both from the store the session will
	// run over. See checkpoint_storage.go.
	client.restoredStorage = &restoredStorage{lengths: make(map[uint32]int), recordSizes: make(map[uint32]uint32), cursors: make(map[uint32][2]int)}
	client.files = make(map[uint32]*openFile, len(saved.Files))
	for _, file := range saved.Files {
		client.files[file.Handle] = &openFile{name: string(file.Name), cursor: file.Cursor, writable: file.Writable, truncated: file.Truncated}
		if file.Truncated {
			client.restoredStorage.lengths[file.Handle] = file.Length
		}
	}
	client.removed, client.created = nil, nil
	client.tmStorage, client.strtokScan = saved.TimeStorage, saved.StrtokScan
	if saved.Random != nil {
		if client.cRandom, err = restoreGuestRandom(*saved.Random); err != nil {
			return nil, err
		}
	}
	client.inputMode, client.inputModeTableAddress = saved.InputMode, saved.InputModeTable
	client.cTextInput = cTextInputState{active: saved.TextInput.Active, revision: saved.TextInput.Revision,
		calls: saved.TextInput.Calls, pending: bytes.Clone(saved.TextInput.Pending)}
	client.applicationIDAddress, client.javaApplication = saved.ApplicationID, saved.JavaApplication
	if len(saved.Resources) != 0 {
		client.resourceIDs = make(map[string]uint32, len(saved.Resources))
		client.resourceNames = make(map[uint32]string, len(saved.Resources))
		for _, resource := range saved.Resources {
			client.resourceIDs[string(resource.Name)] = resource.Handle
			client.resourceNames[resource.Handle] = string(resource.Name)
		}
	}
	if len(saved.Imports) != 0 {
		client.imports = make(map[[2]uint32]bool, len(saved.Imports)/8)
		for offset := 0; offset < len(saved.Imports); offset += 8 {
			client.imports[[2]uint32{binary.LittleEndian.Uint32(saved.Imports[offset:]), binary.LittleEndian.Uint32(saved.Imports[offset+4:])}] = true
		}
	}
	if saved.Java != nil {
		if err := client.restoreJavaState(saved.Java); err != nil {
			client.StopJavaThreads()
			return nil, err
		}
	}
	return client, nil
}
