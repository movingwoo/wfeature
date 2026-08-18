package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A module from the earlier KTF package reaches the platform through one flat
// table of function pointers, and it finds that table in the word immediately
// below its own load address:
//
//	ldr r0, [pc, #n]      ; a link-time offset
//	add r0, pc, r0        ; the module's own base, position independently
//	ldr r0, [r0, #-4]     ; the platform table pointer the loader planted
//	ldr r1, [r0, #0x68]   ; one of its functions
//	bx  r1
//
// That is a different binding from the descriptor package, whose entry is
// handed initialization arguments and asks for interfaces by name, and it is
// simpler: there is nothing to negotiate, so a loader only has to put a table
// where the module will look for it.
//
// What is *in* that table is the open question. The slot numbering is not the
// one this platform serves for the descriptor package — the module calls slot
// 26 as an allocator where the kernel table here has its allocator at 20 — so
// rather than guess a mapping, every slot is filled with a distinct trap. The
// module then names the slots it wants, in the order it wants them, and the
// list it produces is the specification for what to implement. See docs/ktf.md.
const (
	// nativeTableSlots bounds the trap table. The module indexes it with a
	// byte offset, so this is four times the largest reachable offset.
	nativeTableSlots = 128

	// The loader plants the platform table pointer in the word below the
	// module, which needs a mapping of its own: the module's own mapping
	// starts at the image base.
	nativeHeaderBase = ImageBase - nativePageSize

	// A surface is one trapped table of function pointers. Two exist before
	// the module runs — the platform table below the image, and the object
	// handed to the entry — and the rest are created as the module asks for
	// them: it queries the platform for an interface by number and expects a
	// pointer to an object back, so each answered query needs a table of its
	// own to tell its calls from every other surface's.
	maxNativeSurfaces = 16

	// Each surface gets a page for its table and a page for its stubs, which
	// is more than either needs and keeps the arithmetic legible.
	nativeTableBase   = platformDataBase
	nativeStubBase    = platformCodeBase
	nativeScratchBase = platformDataBase + maxNativeSurfaces*nativePageSize

	// nativeEntryObject is the object handed to the entry as its first
	// argument, and nativeEntryOut is the out parameter it writes through.
	nativeEntryObject = nativeScratchBase
	nativeEntryOut    = nativeScratchBase + 0x200
	// nativeApplicationOut receives the title's own object.
	nativeApplicationOut = nativeScratchBase + 0x300

	// The module asks the platform for memory before it does anything else, so
	// a probe that cannot allocate sees one call and nothing after it. The
	// arena is bump-only here: this client exists to find out what a module
	// asks for, and reclaiming is a question for the platform that answers it.
	nativeArenaBase = platformDataBase + (maxNativeSurfaces+1)*nativePageSize
	nativeArenaSize = 32 << 20

	// nativeBlockAlignment is what an ARM procedure call standard block needs,
	// and the arena's own alignment is four. Rounding every size to it in
	// Allocate is what holds the whole arena on that grid rather than only the
	// first block: the cursor moves by a rounded size and nothing else, and a
	// released block goes back and comes out again on the same boundary.
	nativeBlockAlignment = 8

	// svcCategoryNativeTable is this client's only supervisor-call category:
	// a native client runs on a core of its own with a handler of its own, so
	// which surface a stub belongs to travels in the identifier rather than in
	// the category, and the category only has to be a number.
	svcCategoryNativeTable uint32 = 1
)

// NativeSurface names which trap table a call went through.
type NativeSurface string

const (
	// NativePlatformTable is the flat table below the module's load address.
	NativePlatformTable NativeSurface = "platform"
	// NativeEntryObject is the table reached through the entry's first
	// argument.
	NativeEntryObject NativeSurface = "entry object"
)

// NativeCall records one call the module made into a trapped table.
type NativeCall struct {
	// Surface names which table was indexed.
	Surface NativeSurface
	// Offset is the byte offset the module indexed the table with, which is
	// how its own code names the call.
	Offset uint32
	// Slot is that offset as a word index.
	Slot uint32
	// Arguments holds r0 to r3 as the call was made.
	Arguments [4]uint32
	// Caller is the link register, which names the instruction after the call
	// site and so the code to disassemble.
	Caller uint32
	// Served reports whether the platform answered with anything but zero.
	Served bool
}

// NativeSlotHandler answers one table slot. Returning an error stops the run.
type NativeSlotHandler func(thread *armcore.Thread) (uint32, error)

// NativeClient runs a module from the earlier KTF package.
type NativeClient struct {
	core    *armcore.Core
	thread  *armcore.Thread
	archive *NativeArchive
	mapped  uint32
	calls   []NativeCall
	served  map[nativeSlotKey]NativeSlotHandler
	arena   *guestArena
	// blocks records the size of every live allocation, because the module
	// gives a block back as a bare pointer.
	blocks map[uint32]uint64

	surfaces   []nativeSurfaceTable
	interfaces map[uint32]uint32

	traceLimit int
	traceLog   []uint32
}

// nativeSurfaceTable is one trapped table of function pointers.
type nativeSurfaceTable struct {
	name  NativeSurface
	table uint32
	stubs uint32
}

// LoadNativeClient maps a module and plants a fully trapping platform table
// below it.
func LoadNativeClient(archive *NativeArchive, options armcore.CoreOptions) (*NativeClient, error) {
	if archive == nil || len(archive.Module) == 0 {
		return nil, fmt.Errorf("KTF native archive carries no module")
	}
	mapped, err := nativeMappedSize(archive)
	if err != nil {
		return nil, err
	}
	if uint64(ImageBase)+uint64(mapped) > uint64(ThreadStackBase) {
		return nil, fmt.Errorf("KTF native module maps %d bytes and overlaps the guest stack", mapped)
	}

	core := armcore.NewCore(options)
	memory := core.Memory()
	// The module is position independent and carries no relocation table, so
	// it is loaded rather than relocated: where the descriptor package's entry
	// point walks a table adding its own load delta to every word, this one has
	// nothing to walk and runs wherever it is put.
	if err := memory.Map(ImageBase, uint64(mapped), armcore.PermissionReadWriteExecute); err != nil {
		return nil, fmt.Errorf("map KTF native module: %w", err)
	}
	if err := memory.Load(ImageBase, archive.Module); err != nil {
		return nil, fmt.Errorf("load KTF native module: %w", err)
	}
	if err := memory.Map(nativeHeaderBase, nativePageSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF native module header: %w", err)
	}
	if err := memory.Map(ThreadStackBase, ThreadStackSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF native stack: %w", err)
	}
	if err := memory.Map(nativeTableBase, maxNativeSurfaces*nativePageSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF native trap tables: %w", err)
	}
	if err := memory.Map(nativeScratchBase, nativePageSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF native entry scratch: %w", err)
	}
	if err := memory.Map(nativeArenaBase, nativeArenaSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF native arena: %w", err)
	}
	if err := memory.Map(nativeStubBase, maxNativeSurfaces*nativePageSize, armcore.PermissionReadExecute); err != nil {
		return nil, fmt.Errorf("map KTF native trap stubs: %w", err)
	}

	client := &NativeClient{
		core:       core,
		mapped:     mapped,
		served:     map[nativeSlotKey]NativeSlotHandler{},
		interfaces: map[uint32]uint32{},
		arena:      newGuestArena(nativeArenaBase, nativeArenaSize),
		blocks:     map[uint32]uint64{},
	}
	platformTable, err := client.addSurface(NativePlatformTable)
	if err != nil {
		return nil, err
	}
	entryTable, err := client.addSurface(NativeEntryObject)
	if err != nil {
		return nil, err
	}
	// The entry's first argument is an object, and an object here is a pointer
	// to its table.
	object := make([]byte, 4)
	binary.LittleEndian.PutUint32(object, entryTable)
	if err := memory.Load(nativeEntryObject, object); err != nil {
		return nil, fmt.Errorf("plant KTF native entry object: %w", err)
	}

	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, platformTable)
	if err := memory.Load(ImageBase-4, pointer); err != nil {
		return nil, fmt.Errorf("plant KTF native platform table pointer: %w", err)
	}

	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	client.thread = armcore.NewThread(initial)
	client.archive = archive
	return client, nil
}

// AddSurface builds a trapped table that is not tied to an interface number,
// for the objects a served slot hands back. Every instance of one kind shares
// its table, so a handler tells them apart by the object it is called on.
func (client *NativeClient) AddSurface(name NativeSurface) (uint32, error) {
	return client.addSurface(name)
}

// addSurface builds one trapped table and returns its address. Every slot is a
// stub carrying its own surface and slot number across the supervisor-call
// boundary, so a call names where it came from as well as what it wanted.
func (client *NativeClient) addSurface(name NativeSurface) (uint32, error) {
	index := len(client.surfaces)
	if index >= maxNativeSurfaces {
		return 0, fmt.Errorf("KTF native client already has %d trap surfaces", maxNativeSurfaces)
	}
	surface := nativeSurfaceTable{
		name:  name,
		table: nativeTableBase + uint32(index)*nativePageSize,
		stubs: nativeStubBase + uint32(index)*nativePageSize,
	}
	memory := client.core.Memory()
	table := make([]byte, nativeTableSlots*4)
	for slot := range nativeTableSlots {
		address := surface.stubs + uint32(slot*svcStubSize)
		if err := memory.Load(address, svcStub(svcCategoryNativeTable, uint32(index)<<16|uint32(slot))); err != nil {
			return 0, fmt.Errorf("load KTF native trap stub %d of surface %q: %w", slot, name, err)
		}
		// Every stub is Thumb, which the low bit of its address says.
		binary.LittleEndian.PutUint32(table[slot*4:], address|1)
	}
	if err := memory.Load(surface.table, table); err != nil {
		return 0, fmt.Errorf("load KTF native trap table for surface %q: %w", name, err)
	}
	client.surfaces = append(client.surfaces, surface)
	return surface.table, nil
}

// nativeMappedSize decides how much to map for a module.
//
// It maps the module's own length rounded to a page, and nothing beyond it.
// That is not an assumption but a measurement: this module addresses its data
// PC-relatively, holds no absolute address at all, and reads exactly one word
// from outside its image — the platform table pointer the loader plants below
// it. Everything else it works on it asks the platform to allocate, including
// the object it keeps its whole state in. A local title runs its start-up, its
// data loading and thousands of frames byte-identically with no allowance past
// the image and with the 0x8000 an earlier pass mapped.
//
// So the second size the information file names is not a BSS size for this
// module. If a module of this generation ever does need space past its image,
// it faults on the first access rather than running on quietly, and the fault
// names the address — which is the report that would say so.
func nativeMappedSize(archive *NativeArchive) (uint32, error) {
	rounded := (uint64(len(archive.Module)) + nativePageSize - 1) &^ (nativePageSize - 1)
	named, ok := archive.Info.ModuleSpan(len(archive.Module))
	if !ok {
		return 0, fmt.Errorf("KTF module information file names no size matching a %d byte module", len(archive.Module))
	}
	if uint64(named) != rounded {
		return 0, fmt.Errorf("KTF module information file names %#x for a %d byte module, want %#x", named, len(archive.Module), rounded)
	}
	if rounded > maxClientMappedSize {
		return 0, fmt.Errorf("KTF native module maps %d bytes, limit %d", rounded, maxClientMappedSize)
	}
	return uint32(rounded), nil
}

// Serve answers one table slot from here on. It is how a slot moves from the
// call list into the platform: name it, answer it, and run again to see what
// the module asks for next.
func (client *NativeClient) Serve(surface NativeSurface, offset uint32, handler NativeSlotHandler) {
	client.served[nativeSlotKey{surface: surface, slot: offset / 4}] = handler
}

// nativeSlotKey names one slot of one trap surface.
type nativeSlotKey struct {
	surface NativeSurface
	slot    uint32
}

// Start calls the module's entry point.
//
// The entry is the first instruction of the module, in ARM state. It shifts its
// three arguments up and hands them to an internal dispatcher along with a
// selector of its own, and that dispatcher rejects a null first or third
// argument before doing anything else — a run started with zeroes returns
// immediately and calls nothing, which reads exactly like a module that does
// not work. The third argument is written through, so it is an out parameter;
// what the first one points at is not yet known, so it is given writable
// scratch rather than nothing.
func (client *NativeClient) Start(ctx context.Context) error {
	arguments := []uint32{nativeEntryObject, nativeScratchBase + 0x100, nativeEntryOut}
	_, err := client.core.Call(ctx, client.thread, ImageBase, ReturnAddress, arguments, client.handleSupervisorCall)
	return err
}

// Trace records the instruction addresses the guest executes, up to limit.
//
// Attaching a debugger slows the core to one instruction per quantum, so this
// is a probe facility and not something a run does by default. It exists
// because the module's own control flow is the question here: a call that
// returns success without asking the platform for anything has gone somewhere,
// and the trace is what says where.
func (client *NativeClient) Trace(limit int) {
	client.traceLimit = limit
	client.traceLog = client.traceLog[:0]
	client.core.AttachDebugger(func(_ context.Context, core *armcore.Core, thread *armcore.Thread, _ armcore.DebugStop) error {
		if len(client.traceLog) < client.traceLimit {
			executing := thread.Context()
			client.traceLog = append(client.traceLog, executing.PC())
		}
		core.StepOnce()
		return nil
	})
	client.core.StepOnce()
}

// StopTrace detaches the debugger and returns what was recorded.
func (client *NativeClient) StopTrace() []uint32 {
	client.core.AttachDebugger(nil)
	return client.traceLog
}

// CallExport calls one of the module's exported functions on this client's
// thread, with the trap tables still in force, so what it asks for is recorded
// the same way the entry's calls were.
func (client *NativeClient) CallExport(ctx context.Context, address uint32, arguments []uint32) (uint32, error) {
	// The result comes off the summary rather than the thread. Call derives a
	// temporary context and restores the parent by leaving it untouched, so
	// reading r0 from the thread afterwards reports whatever the parent held —
	// which is zero on a fresh client, and reads exactly like a call that
	// succeeded.
	summary, err := client.core.Call(ctx, client.thread, address, ReturnAddress, arguments, client.handleSupervisorCall)
	if err != nil {
		return 0, err
	}
	return summary.Context.Registers[0], nil
}

// EntryRecord is the block a module hands back through its entry's out
// parameter. The module builds it, fills it with pointers to its own functions
// and returns it, which makes it the module's half of the ABI: the platform
// table is what the module calls, and this is what the platform calls.
type EntryRecord struct {
	// Address is where the block was allocated.
	Address uint32
	// Words is the block verbatim.
	Words []uint32
	// Functions holds the module's exported entry points, from the table the
	// block's first word points at.
	Functions []uint32
}

// ReadEntryRecord follows the entry's out parameter to the block behind it.
func (client *NativeClient) ReadEntryRecord() (EntryRecord, error) {
	address, err := client.EntryResult()
	if err != nil {
		return EntryRecord{}, err
	}
	if address == 0 {
		return EntryRecord{}, fmt.Errorf("KTF native entry wrote no record")
	}
	// The block is nine words: a pointer to the module's own function table,
	// a one, the three values the entry was given, and the function table
	// itself sitting at the block's tail.
	const recordWords = 9
	words, err := client.readWords(address, recordWords)
	if err != nil {
		return EntryRecord{}, fmt.Errorf("read KTF native entry record: %w", err)
	}
	record := EntryRecord{Address: address, Words: words}
	const exportedFunctions = 4
	if functions, err := client.readWords(words[0], exportedFunctions); err == nil {
		record.Functions = functions
	}
	return record, nil
}

// ReadObjectTable reads the function table an object's first word points at,
// as guest addresses. An object here is a pointer to its table, so this is how
// a probe sees what a module handed back.
func (client *NativeClient) ReadObjectTable(object uint32, count int) (uint32, []uint32, error) {
	table, err := client.ReadWord(object)
	if err != nil {
		return 0, nil, err
	}
	functions, err := client.readWords(table, count)
	if err != nil {
		return table, nil, err
	}
	return table, functions, nil
}

// ReadFields reads an object's own words, which is how a probe finds the
// function pointers a module stored in itself rather than in a table.
func (client *NativeClient) ReadFields(object uint32, count int) ([]uint32, error) {
	return client.readWords(object, count)
}

// ReadWord reads one guest word, which is how a probe reads an out parameter
// the module wrote through.
func (client *NativeClient) ReadWord(address uint32) (uint32, error) {
	words, err := client.readWords(address, 1)
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

func (client *NativeClient) readWords(address uint32, count int) ([]uint32, error) {
	data := make([]byte, count*4)
	if err := client.core.Memory().Read(address, data); err != nil {
		return nil, err
	}
	words := make([]uint32, count)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return words, nil
}

// EntryResult reads the word the entry's out parameter received.
func (client *NativeClient) EntryResult() (uint32, error) {
	word := make([]byte, 4)
	if err := client.core.Memory().Read(nativeEntryOut, word); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(word), nil
}

// Steps reports how many instructions this client's core has executed, which
// is how a probe tells a frame that ran the game from one that returned
// immediately.
func (client *NativeClient) Steps() uint64 { return client.core.Steps() }

// Calls reports every platform-table call the module made, in order.
func (client *NativeClient) Calls() []NativeCall {
	return client.calls
}

// SlotSummary reports how many times each slot was called, lowest first. It is
// the list that says what implementing this package actually costs.
func (client *NativeClient) SlotSummary() []NativeSlotCount {
	counts := map[nativeSlotKey]*NativeSlotCount{}
	for _, call := range client.calls {
		key := nativeSlotKey{surface: call.Surface, slot: call.Slot}
		entry, ok := counts[key]
		if !ok {
			entry = &NativeSlotCount{Surface: call.Surface, Offset: call.Offset, Slot: call.Slot, First: call.Caller}
			counts[key] = entry
		}
		entry.Count++
	}
	summary := make([]NativeSlotCount, 0, len(counts))
	for _, entry := range counts {
		summary = append(summary, *entry)
	}
	sort.Slice(summary, func(a, b int) bool {
		if summary[a].Surface != summary[b].Surface {
			return summary[a].Surface < summary[b].Surface
		}
		return summary[a].Offset < summary[b].Offset
	})
	return summary
}

// NativeSlotCount is one row of a run's slot summary.
type NativeSlotCount struct {
	Surface NativeSurface
	Offset  uint32
	Slot    uint32
	Count   int
	// First is the link register of the first call, which names the module
	// code to disassemble when a slot has to be identified.
	First uint32
}

func (client *NativeClient) handleSupervisorCall(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	if call.Immediate != svcCategoryNativeTable {
		return fmt.Errorf("unknown KTF native supervisor call %#x at %#x", call.Immediate, call.Address)
	}
	identifier, err := thread.Register(12)
	if err != nil {
		return err
	}
	index, slot := identifier>>16, identifier&0xffff
	if int(index) >= len(client.surfaces) {
		return fmt.Errorf("KTF native trap surface %d does not exist", index)
	}
	if slot >= nativeTableSlots {
		return fmt.Errorf("KTF native trap slot %d is outside the table", slot)
	}
	surface := client.surfaces[index].name
	record := NativeCall{Surface: surface, Offset: slot * 4, Slot: slot}
	for index := range record.Arguments {
		if record.Arguments[index], err = thread.Register(index); err != nil {
			return err
		}
	}
	if record.Caller, err = thread.Register(armcore.RegisterLR); err != nil {
		return err
	}

	result := uint32(0)
	if handler, ok := client.served[nativeSlotKey{surface: surface, slot: slot}]; ok {
		if result, err = handler(thread); err != nil {
			client.calls = append(client.calls, record)
			return fmt.Errorf("KTF native platform slot %#x (offset %#x): %w", slot, record.Offset, err)
		}
		record.Served = true
	}
	client.calls = append(client.calls, record)
	return thread.SetRegister(0, result)
}

// Allocate hands out zeroed guest memory from this client's arena and
// remembers the block's size, which is what lets Free give it back: the module
// hands a block back as a bare pointer.
func (client *NativeClient) Allocate(size uint32) (uint32, error) {
	if size == 0 {
		return 0, fmt.Errorf("KTF native allocation of zero bytes")
	}
	if uint64(size) > maxPlatformAllocation {
		return 0, fmt.Errorf("KTF native allocation %d exceeds %d bytes", size, maxPlatformAllocation)
	}
	// Eight-byte alignment is what an ARM procedure call standard block needs,
	// and a module that stores doubles into what it was handed needs it here.
	aligned := (uint64(size) + nativeBlockAlignment - 1) &^ uint64(nativeBlockAlignment-1)
	address, ok := client.arena.allocate(aligned)
	if !ok {
		return 0, fmt.Errorf("KTF native arena exhausted at %d bytes", size)
	}
	client.blocks[address] = aligned
	if err := client.core.Memory().Write(address, make([]byte, aligned)); err != nil {
		return 0, fmt.Errorf("clear KTF native allocation at %#x: %w", address, err)
	}
	return address, nil
}

// Free gives a block back. A pointer this client never handed out is ignored
// rather than refused: the module frees what it built during a start-up that
// failed too, and a run that stops on a stray free reports the free instead of
// what failed before it.
func (client *NativeClient) Free(address uint32) {
	size, ok := client.blocks[address]
	if !ok {
		return
	}
	delete(client.blocks, address)
	client.arena.release(address, size)
}

// AvailableMemory reports what a further allocation could still claim, which
// is the number the module asks the platform for before sizing its own caches.
func (client *NativeClient) AvailableMemory() uint64 { return client.arena.available() }

// ServeAllocator answers one table slot as an allocator taking its size in r0.
func (client *NativeClient) ServeAllocator(surface NativeSurface, offset uint32) {
	client.Serve(surface, offset, func(thread *armcore.Thread) (uint32, error) {
		size, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		return client.Allocate(size)
	})
}

// ServeQueryInterface answers one slot as an interface query.
//
// The module asks the platform for an interface by number and passes a pointer
// to write the answer through — and it checks *that pointer*, not the return
// value, so a handler that only returns something is a handler that fails. Each
// distinct number gets a trap surface of its own, so what the module goes on to
// call on the object it was handed is recorded apart from every other surface.
func (client *NativeClient) ServeQueryInterface(surface NativeSurface, offset uint32) {
	client.Serve(surface, offset, func(thread *armcore.Thread) (uint32, error) {
		identifier, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		out, err := thread.Register(2)
		if err != nil {
			return 0, err
		}
		object, err := client.InterfaceObject(identifier)
		if err != nil {
			return 0, err
		}
		word := make([]byte, 4)
		binary.LittleEndian.PutUint32(word, object)
		if err := client.core.Memory().Write(out, word); err != nil {
			return 0, fmt.Errorf("write KTF native interface %#x to %#x: %w", identifier, out, err)
		}
		return 0, nil
	})
}

// InterfaceObject returns the trapped object answering one interface number,
// building it on first use. An object is a pointer to its table, which is the
// shape both sides of this protocol use.
func (client *NativeClient) InterfaceObject(identifier uint32) (uint32, error) {
	if address, ok := client.interfaces[identifier]; ok {
		return address, nil
	}
	table, err := client.addSurface(NativeSurface(fmt.Sprintf("interface %#x", identifier)))
	if err != nil {
		return 0, err
	}
	address, err := client.Allocate(4)
	if err != nil {
		return 0, err
	}
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, table)
	if err := client.core.Memory().Write(address, word); err != nil {
		return 0, fmt.Errorf("write KTF native interface object %#x: %w", identifier, err)
	}
	client.interfaces[identifier] = address
	return address, nil
}

// The start-up protocol, in the order a loader performs it.
//
// The package's two halves negotiate through one reference-counted interface
// shape, used on both sides: an object is a pointer to a table whose first two
// entries raise and drop a count and whose third dispatches. Nothing is named —
// every step is a number — and the numbers that matter come out of the
// information file rather than out of the module.
//
//  1. Start calls the module's entry with the platform object, and the module
//     answers with a factory object written through an out parameter.
//  2. CreateApplication dispatches on that factory with the identifier the
//     information file carries, and the module answers with the title's own
//     object — again through an out parameter, which is what the module checks
//     rather than the result.
//  3. SendEvent dispatches on the title's object with a small integer. That is
//     an event number, not an identifier: the title's own handler switches on
//     0, 1, 2, 3 and above, which is the Clet event contract the specification
//     describes.
//
// Two of those steps look like they succeeded when they have not. The factory
// rejects a null second or fourth argument before doing anything, and returns
// the same way it would on any other failure; and the created object arrives in
// the out parameter, so a caller reading the result alone sees zero and calls
// it success. Both cost a session here. See docs/ktf.md.

// ApplicationIdentifier is the number the information file and the module
// agree on, and the one CreateApplication needs.
//
// Which of the file's unlabelled numbers it is comes from two anchors landing
// in the same record: that record's last word is the image base, and its first
// word is the constant the module's own dispatch compares its argument
// against. Neither anchor alone would pick it out — the file has other round
// numbers, and taking the first record's first word picks up a 0x1000 that the
// module refuses.
func (archive *NativeArchive) ApplicationIdentifier() (uint32, bool) {
	for _, record := range archive.Info.Records {
		if len(record) < 2 || record[0] == 0 {
			continue
		}
		for _, word := range record[1:] {
			if word == ImageBase {
				return record[0], true
			}
		}
	}
	return 0, false
}

// CreateApplication asks the factory the entry returned for the title's own
// object.
func (client *NativeClient) CreateApplication(ctx context.Context, identifier uint32) (uint32, error) {
	record, err := client.ReadEntryRecord()
	if err != nil {
		return 0, err
	}
	if len(record.Functions) < 3 {
		return 0, fmt.Errorf("KTF native entry record exports %d functions, want at least 3", len(record.Functions))
	}
	out := nativeApplicationOut
	if _, err := client.CallExport(ctx, record.Functions[2],
		[]uint32{record.Address, nativeEntryObject, identifier, out}); err != nil {
		return 0, err
	}
	object, err := client.ReadWord(out)
	if err != nil {
		return 0, err
	}
	if object == 0 {
		return 0, fmt.Errorf("KTF native module refused to create application %#x", identifier)
	}
	return object, nil
}

// SendEvent dispatches one event on the title's object.
func (client *NativeClient) SendEvent(ctx context.Context, object, event, first, second uint32) (uint32, error) {
	_, functions, err := client.ReadObjectTable(object, 3)
	if err != nil {
		return 0, err
	}
	if len(functions) < 3 || functions[2] == 0 {
		return 0, fmt.Errorf("KTF native application object at %#x has no dispatch", object)
	}
	return client.CallExport(ctx, functions[2], []uint32{object, event, first, second})
}
