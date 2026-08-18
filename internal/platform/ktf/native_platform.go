package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// NativePlatform answers the earlier KTF package's platform surface.
//
// Where `native_client.go` maps a module and traps every slot so a run reports
// what the module asked for, this is the other half: the slots whose meaning
// has been established, answered. Everything else stays a trap, so a run still
// stops at the first slot that matters and names it. See docs/ktf.md, "An
// earlier KTF package".
//
// The surface has three parts, and they are not one table:
//
//   - The flat table below the module's load address. The module reaches it
//     through the word at ImageBase-4 and indexes it by byte offset. A static
//     sweep of the module finds sixteen slots reachable in its code, which is
//     the whole of this half.
//   - The object handed to the entry, whose first three vtable entries raise a
//     count, drop it and answer a query by number. The module keeps it in the
//     application object it builds and calls further methods on it.
//   - One object per interface number it queries. Each is a table of its own,
//     so what the module calls on one is recorded apart from every other.
type NativePlatform struct {
	client  *NativeClient
	archive *NativeArchive
	clock   Clock
	// pace is the same clock, kept in its own type so the rate can be changed
	// and a guest duration converted back to what it costs a Host. source is
	// the clock underneath it, which is what a probe drives.
	pace   *backend.SpeedClock
	source Clock
	// started is the guest's zero for its millisecond clock. The module reads
	// that clock, subtracts a saved reading and loops while the difference is
	// below a bound, so what it needs is a monotonic millisecond count rather
	// than a date.
	started time.Time
	// application is the object the module's factory built, which the module
	// asks the platform to hand back rather than threading through its own
	// calls.
	application uint32
	// files holds every open file by the object the module was handed, and
	// fileTable is the table all of those objects point at.
	files     map[uint32]*nativeOpenFile
	fileTable uint32
	// installed guards Install against a second pass, which would build a
	// second set of trap surfaces for the same tables.
	installed bool
	// screen is what the title draws into.
	screen *nativeScreen
	// screenWidth and screenHeight are the handset the title is told it runs
	// on. Zero is the platform's own; see SetScreen.
	screenWidth  int
	screenHeight int
	// images holds every image the title asked the factory for, by the object
	// it was handed back.
	images map[uint32]*nativeImage
	// listeners are the functions the module registered on the event sources.
	listeners []NativeListener
	// timedDue is when the service the title is waiting on finishes.
	timedDue     time.Time
	timedRunning bool
	// frame is the callback the title registered for its own stepping.
	frame *nativeSchedule
	// opens records every open the module asked for.
	opens []NativeFileOpen
	// written holds what the title has written this session, by lower-cased
	// base name. It shadows the package's own copy of the same file.
	written map[string][]byte
	// unsaved names the entries of written the store has not been given yet.
	// See NativePlatform.keep.
	unsaved map[string]bool
	// saves is where those writes go to outlive the session.
	saves SaveStore
	// audio owns what the title plays, and clip is the one it has loaded.
	audio    *backend.Audio
	clip     backend.AudioHandle
	sounding bool
	// clipRefusals counts clips this platform would not play.
	clipRefusals int
	// messages holds the status lines the title asked the handset to show.
	messages []string
	// storeFailures counts writes the store refused.
	storeFailures int
}

// The flat platform table, by the byte offset the module indexes it with.
// Nothing here is named in the module: the numbering is not the one the
// descriptor package's kernel table uses, where the allocator sits at 20.
const (
	// nativeSlotAllocate takes a size in r0 and returns a block. It is the
	// first thing the module calls, before anything else exists.
	nativeSlotAllocate = 0x68
	// nativeSlotFree gives one back.
	nativeSlotFree = 0x6c
	// nativeSlotInitial is called once, with three zeroes, immediately after
	// the module caches the platform table, and its result is kept in the
	// application object. What it answers is not established; answering zero
	// has not stopped a run yet.
	nativeSlotInitial = 0x8c
	// nativeSlotMilliseconds answers a millisecond clock. One call site saves
	// a reading and spins until a later one is 2,000 higher; another seeds the
	// module's own generator with it.
	nativeSlotMilliseconds = 0xac
	// nativeSlotCurrentApplication answers with the object the module's
	// factory built. The module asks for it from thirty-nine call sites rather
	// than carrying it, and reads the fields it set on it itself.
	nativeSlotCurrentApplication = 0xc0
)

// The vtable of the object handed to the module's entry, by byte offset.
const (
	nativeObjectAddRef         = 0x00
	nativeObjectRelease        = 0x04
	nativeObjectQueryInterface = 0x08
	// nativeObjectSchedule takes an interval, a function and a context, and
	// the title's start event ends by calling it. What it registers is the
	// title's frame: the function it hands over reads the clock and steps the
	// game, so a run that never calls it back sees a title that loaded its
	// data and then did nothing.
	nativeObjectSchedule = 0x2c
	// nativeObjectDisplayInfo fills a caller-supplied record. Its first two
	// halfwords are taken as a size and kept; the halfword at 0xe is kept
	// beside it.
	nativeObjectDisplayInfo = 0x10
)

// The interface numbers the module queries, in the order it asks for them.
const (
	// nativeInterfaceApplication is queried by the class factory while it
	// builds the application object, and the module checks the pointer it
	// passed rather than the result.
	nativeInterfaceApplication = 0x1001001
	// nativeInterfaceMemory is asked how much memory is free and whether a
	// given size can be had. The module walks it down from 64KB by halves
	// looking for the largest block it can keep.
	nativeInterfaceMemory = 0x1001002
	// nativeInterfaceFile is asked for the title's own resource files by
	// name.
	nativeInterfaceFile = 0x1001003
	// nativeInterfaceEventSource and nativeInterfaceSecondEventSource are the
	// two the module hands a function and a context to, and then waits on: one
	// of the two functions it registers clears the flag its own modal wait
	// spins on. Which events each carries is not established, so nothing is
	// delivered through them yet.
	// nativeInterfaceSound is the player the title hands SMAF files to, and
	// the source that reports the end of one.
	nativeInterfaceSound = 0x1002000
	// nativeInterfaceTimed is the service the title starts with a duration in
	// milliseconds and then waits on: it sets a flag beside the call and
	// clears it only when this source reports the end.
	nativeInterfaceTimed = 0x100100b
)

// nativeSourceAddListener is the method both event sources take a function
// and a context through.
const nativeSourceAddListener = 0x08

// The memory interface's methods, by byte offset.
const (
	// nativeMemoryFits is asked with a size and answers whether it is
	// available.
	nativeMemoryFits = 0x18
	// nativeMemoryFree answers the free byte count.
	nativeMemoryFree = 0x1c
)

// nativeInterfaceSurface names the trap surface serving one interface number.
func nativeInterfaceSurface(identifier uint32) NativeSurface {
	return NativeSurface(fmt.Sprintf("interface %#x", identifier))
}

// NewNativePlatform prepares the platform half of the earlier package. A nil
// clock uses the wall clock, which is what a Host showing the title to a
// person wants; a probe measuring what the guest computes passes a
// ManualClock.
func NewNativePlatform(client *NativeClient, archive *NativeArchive, clock Clock) *NativePlatform {
	if clock == nil {
		clock = wallClock{}
	}
	pace := backend.NewSpeedClock(clock.Now)
	return &NativePlatform{
		client:  client,
		archive: archive,
		clock:   pace,
		pace:    pace,
		source:  clock,
		started: pace.Now(),
	}
}

// SetScreen names the handset the title is told it runs on, with zero for
// either side selecting the platform's own 240x320. It has the same contract
// as the descriptor package's Client.SetScreen — the size the display record
// reports and the size of the frame the title draws into are one answer, read
// from here by both — and it has to be called before Install builds the
// screen.
func (platform *NativePlatform) SetScreen(width, height int) {
	if platform == nil {
		return
	}
	platform.screenWidth = width
	platform.screenHeight = height
}

// screenSize is the handset to answer with, with the platform's own standing
// in for whatever was not chosen.
func (platform *NativePlatform) screenSize() (int, int) {
	width, height := runtimeDisplayPixelWidth, runtimeDisplayPixelHeight
	if platform != nil && platform.screenWidth > 0 && platform.screenHeight > 0 {
		width, height = platform.screenWidth, platform.screenHeight
	}
	return width, height
}

// Install registers every handler whose slot is established. It is called
// before the module runs, and again for nothing afterwards: the application
// object the module builds is read through the platform rather than bound
// here, so no handler has to be replaced once a run has started.
func (platform *NativePlatform) Install() error {
	if platform.installed {
		return nil
	}
	platform.installed = true
	client := platform.client
	client.Serve(NativePlatformTable, nativeSlotAllocate, platform.allocate)
	client.Serve(NativePlatformTable, nativeSlotFree, platform.free)
	client.Serve(NativePlatformTable, nativeSlotInitial, nativeAnswerZero)
	client.Serve(NativePlatformTable, nativeSlotMilliseconds, platform.milliseconds)
	client.Serve(NativePlatformTable, nativeSlotCurrentApplication, platform.currentApplication)
	client.Serve(NativePlatformTable, nativeSlotCreateObject, platform.createObject)

	client.Serve(NativeEntryObject, nativeObjectAddRef, nativeAnswerOne)
	client.Serve(NativeEntryObject, nativeObjectRelease, nativeAnswerOne)
	client.ServeQueryInterface(NativeEntryObject, nativeObjectQueryInterface)
	client.Serve(NativeEntryObject, nativeObjectDisplayInfo, platform.displayInfo)
	client.Serve(NativeEntryObject, nativeObjectSchedule, platform.schedule)

	memory := nativeInterfaceSurface(nativeInterfaceMemory)
	client.Serve(memory, nativeMemoryFits, platform.memoryFits)
	client.Serve(memory, nativeMemoryFree, platform.memoryFree)
	platform.installLibrary()
	platform.images = map[uint32]*nativeImage{}
	platform.installScreen()
	platform.installSound()
	platform.installTimed()
	platform.installRemaining()
	for _, identifier := range []uint32{nativeInterfaceSound, nativeInterfaceTimed} {
		client.Serve(nativeInterfaceSurface(identifier), nativeSourceAddListener, platform.addListener(identifier))
	}
	return platform.installFiles()
}

func nativeAnswerZero(*armcore.Thread) (uint32, error) { return 0, nil }
func nativeAnswerOne(*armcore.Thread) (uint32, error)  { return 1, nil }

// allocate answers the platform table's allocator.
func (platform *NativePlatform) allocate(thread *armcore.Thread) (uint32, error) {
	size, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	return platform.client.Allocate(size)
}

// free answers the platform table's deallocator. A pointer the platform never
// handed out is ignored rather than refused: the module frees what it built
// during a failed start-up too, and a run that stops on a stray free reports
// the free instead of what failed before it.
func (platform *NativePlatform) free(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	platform.client.Free(address)
	return 0, nil
}

// milliseconds answers the module's clock.
func (platform *NativePlatform) milliseconds(*armcore.Thread) (uint32, error) {
	return uint32(platform.clock.Now().Sub(platform.started) / time.Millisecond), nil
}

// currentApplication hands back the object the module's factory built.
func (platform *NativePlatform) currentApplication(*armcore.Thread) (uint32, error) {
	if platform.application == 0 {
		return 0, fmt.Errorf("KTF native module asked for the current application before it created one")
	}
	return platform.application, nil
}

// nativeDisplayRecordSize covers the halfwords the module reads out of the
// display record: a size at 0 and 2, and one more at 0xe.
const nativeDisplayRecordSize = 0x10

// displayInfo fills the record the module hands in. It reads the first two
// halfwords as a size and keeps them, and keeps the halfword at 0xe beside
// them; the rest of the record is not read by this title, so it is zeroed
// rather than invented.
func (platform *NativePlatform) displayInfo(thread *armcore.Thread) (uint32, error) {
	out, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	width, height := platform.screenSize()
	record := make([]byte, nativeDisplayRecordSize)
	binary.LittleEndian.PutUint16(record[0:], uint16(width))
	binary.LittleEndian.PutUint16(record[2:], uint16(height))
	binary.LittleEndian.PutUint16(record[0xe:], nativeDisplayBitsPerPixel)
	if err := platform.client.core.Memory().Write(out, record); err != nil {
		return 0, fmt.Errorf("write KTF native display record at %#x: %w", out, err)
	}
	return 1, nil
}

// nativeDisplayBitsPerPixel is what the descriptor package's display info
// reports, and this package's screen is the same handset's.
const nativeDisplayBitsPerPixel = 16

// memoryFits answers whether a size is available. The module asks this in a
// loop from 64KB downwards and keeps the first size that both this answers and
// the allocator delivers, so answering generously costs nothing: the
// allocation that follows is the real test.
func (platform *NativePlatform) memoryFits(thread *armcore.Thread) (uint32, error) {
	size, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if uint64(size) > platform.client.AvailableMemory() {
		return 0, nil
	}
	return 1, nil
}

// memoryFree answers the free byte count.
func (platform *NativePlatform) memoryFree(*armcore.Thread) (uint32, error) {
	return uint32(platform.client.AvailableMemory()), nil
}

// Boot performs the package's start-up protocol and delivers the first event.
//
// The three steps are the module's, not ours: its entry answers with a factory
// through an out parameter, the factory builds the title's object from the
// identifier the information file carries, and a small integer sent to that
// object is an event. See docs/ktf.md.
func (platform *NativePlatform) Boot(ctx context.Context) error {
	if err := platform.Create(ctx); err != nil {
		return err
	}
	return platform.Start(ctx)
}

// Create runs the first two steps: the entry answers with a factory, and the
// factory builds the title's object. It is separate from Start because the
// object only exists between them, and everything a probe wants to watch —
// the fields the title sets on itself — lives in it.
func (platform *NativePlatform) Create(ctx context.Context) error {
	if err := platform.Install(); err != nil {
		return err
	}
	if err := platform.client.Start(ctx); err != nil {
		return fmt.Errorf("KTF native entry: %w", err)
	}
	identifier, ok := platform.archive.ApplicationIdentifier()
	if !ok {
		return fmt.Errorf("KTF module information file carries no application identifier")
	}
	application, err := platform.client.CreateApplication(ctx, identifier)
	if err != nil {
		return fmt.Errorf("KTF native application %#x: %w", identifier, err)
	}
	platform.application = application
	return nil
}

// Start delivers the first event, which is what runs the title's own start-up:
// it loads its data, draws its first screen and registers the frame it wants
// to be called back on.
func (platform *NativePlatform) Start(ctx context.Context) error {
	if platform.application == 0 {
		return fmt.Errorf("KTF native start event before the application was created")
	}
	if _, err := platform.client.SendEvent(ctx, platform.application, nativeEventStart, 0, 0); err != nil {
		return fmt.Errorf("KTF native start event: %w", err)
	}
	return nil
}

// Images reports how many of the title's own bitmaps the platform has built.
func (platform *NativePlatform) Images() int { return len(platform.images) }

// Application reports the object the module built, once Boot has run.
func (platform *NativePlatform) Application() uint32 { return platform.application }

// The last of the module's flat table, and the objects it asks for that are
// not the screen, the files, the memory or the player.
//
// Slot 47 is a second deallocator. Its call site is what says so: the module
// picks between it and the ordinary free on a flag it keeps beside each entry,
// and what it hands over is one of the objects the factory built. So an object
// is destroyed through the slot that knows what it is, and everything else
// through free.
const nativeSlotDestroyObject = 0xbc

// The screen's two remaining methods. The first is how the title asks the
// platform for a status line — it passes a flag word, a message and a
// length — and the second it calls once a second while it is running, which is
// what a handset's "keep the backlight on" looks like. Neither answer is read
// by the title, so both are recorded rather than invented.
const (
	nativeScreenMessage   = 0x10
	nativeScreenKeepAwake = 0x24
)

// nativeInterfaceHandset is the object the title queries for a record it turns
// into a 26-character identifier, padding whatever the record leaves empty
// with `X`. Its two methods fill that record and drop the object.
const (
	nativeInterfaceHandset = 0x18000fe
	nativeHandsetRelease   = 0x04
	nativeHandsetRecord    = 0x08
)

// nativeHandsetRecordSize is what the module clears before it asks. It copies
// the first ten bytes and then sixteen more from offset ten, so the record is
// two fields and the identifier is made of both.
const nativeHandsetRecordSize = 0x26

// installRemaining registers the slots that are established but whose answers
// the title does not read.
func (platform *NativePlatform) installRemaining() {
	client := platform.client
	client.Serve(NativePlatformTable, nativeSlotDestroyObject, platform.destroyObject)

	screen := nativeInterfaceSurface(nativeInterfaceApplication)
	client.Serve(screen, nativeScreenMessage, platform.screenMessage)
	client.Serve(screen, nativeScreenKeepAwake, nativeAnswerOne)

	handset := nativeInterfaceSurface(nativeInterfaceHandset)
	client.Serve(handset, nativeHandsetRecord, platform.handsetRecord)
	client.Serve(handset, nativeHandsetRelease, nativeAnswerOne)
}

// destroyObject drops an object the factory built.
func (platform *NativePlatform) destroyObject(thread *armcore.Thread) (uint32, error) {
	object, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if object == 0 {
		return 0, nil
	}
	delete(platform.images, object)
	platform.client.Free(object)
	return 1, nil
}

// screenMessage records the status line the title asked for. A Host has
// nowhere to put it — the title draws its own screen and this is the
// handset's furniture — so it is kept for a run to report rather than drawn
// over what the title is drawing.
func (platform *NativePlatform) screenMessage(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		return 1, nil
	}
	text, err := platform.readString(address)
	if err != nil {
		// A message that cannot be read is not worth failing a run over.
		return 1, nil
	}
	platform.messages = append(platform.messages, decodeEUCKR(text))
	return 1, nil
}

// Messages reports the status lines the title asked the handset to show.
func (platform *NativePlatform) Messages() []string { return platform.messages }

// handsetRecord fills the record the title turns into its identifier.
//
// What belongs in it is the handset's own identity, and the only part of that
// this emulator has is the subscriber number every platform here already
// answers with. The title hashes the result and compares it against a hash it
// computed the same way, so what is in the record decides nothing on its own —
// but a record left empty becomes an identifier of twenty-six `X`s, which is
// the same identifier on every handset that ever ran it.
func (platform *NativePlatform) handsetRecord(thread *armcore.Thread) (uint32, error) {
	out, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if out == 0 {
		return 0, nil
	}
	record := make([]byte, nativeHandsetRecordSize)
	copy(record, wipic.SubscriberNumber())
	if err := platform.client.core.Memory().Write(out, record); err != nil {
		return 0, fmt.Errorf("write KTF native handset record at %#x: %w", out, err)
	}
	return 1, nil
}
