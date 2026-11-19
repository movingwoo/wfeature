package wipicrt

import (
	"context"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Platform is what a platform fixes about the WIPI-C engine it runs. KTF titles
// and SKT Clets call almost the same functions under almost the same numbers,
// so both run these handlers; what differs between the two is named here, in
// one place, rather than in two copies of the handlers.
type Platform struct {
	// Name begins the engine's errors and logs: "KTF" or "SKT Clet".
	Name string
	// CName names the WIPI-C interface in the error for a function no handler
	// answers: "KTF WIPI C" or "SKT WIPI C".
	CName string
	// CallName names the guest calls the nesting bound counts: "KTF AOT" or
	// "SKT Clet".
	CallName string
	// ExitError is the sentinel MC_knlExit ends the program with, which the
	// platform's Host matches to tell an ending from a failure.
	ExitError error
	// HandleFirstWord is where an identifier's first word points, as an offset
	// from the identifier, and HandleData where MC_GETDPTR finds the data, as
	// an offset from that word. The data starts at the same place on both
	// platforms, AllocationOverhead bytes in; the macros that reach it are the
	// handset's. On KTF the first word points at the size word four bytes on
	// and the data is eight past it; on SKT it points at the data itself, and
	// one SKT title copies a resource out of `*id` and reads a framebuffer's
	// fields at `**id`.
	HandleFirstWord, HandleData uint32
	// StaleRemovals says the platform's earlier engine wrote the removal list
	// of its C files without reading it back in a later session, so a save
	// written there may still be named on the list over bytes a later session
	// wrote. RemovedDatabases reads such a name as the file it holds.
	StaleRemovals bool
	// PixelOpDestinationFirst says the platform calls a context's pixel
	// operation with the pixel already on the surface first and the one being
	// drawn second, the order the specification's prose gives. KTF passes the
	// drawn pixel first, the order of the prototype's parameter names. See
	// wipic_pixelop.go.
	PixelOpDestinationFirst bool
	// ContextTransparentPixel says MC_grpDrawImage leaves out the source
	// pixels equal to the context's transparent pixel, as the specification
	// words the call ("투명정도와 투명색으로"). One SKT title sets the
	// transparent pixel to magenta before every image it draws and has no
	// other transparency, so its sprites and lettering came out on magenta
	// boxes. A context MC_grpInitContext cleared holds zero there, which is
	// read as no transparent pixel: another SKT title draws every image
	// through such a context. KTF titles that set the pixel draw correctly
	// without it, so KTF does not apply it.
	ContextTransparentPixel bool
	// TransparentImagePixel, when it is not zero, is the pixel
	// MC_grpCreateImage writes where the decoded image is transparent, in
	// place of the colour the encoding kept there. One SKT title decodes every
	// picture through MC_grpCreateImage, reads the pixels itself and skips
	// 0xFFF7 in each of its two blitters, a constant it hands its sprite
	// records for every picture whatever colour the picture declared
	// transparent; drawn with the kept colours instead, its sprites came out
	// on boxes of those colours. Zero keeps the encoding's colour, which LGT
	// and KTF titles key on.
	TransparentImagePixel uint16
}

// ClientHooks are the platform's parts of a service round. A hook left nil is
// work the platform does not have.
type ClientHooks struct {
	// Deadlines offers the platform's own parked work to the Host's next
	// deadline: cards waiting for their round paint, guest threads, queued
	// events. It is called with the run lock held.
	Deadlines func(consider func(time.Time))
	// TimerTaskRunning and StartTimerTask run a timer whose Task the platform
	// queued beside the WIPI-C timers, such as a java/util/Timer task.
	TimerTaskRunning func(timer WIPICTimer) bool
	StartTimerTask   func(timer WIPICTimer) error
}

// RuntimeHooks are the calls a WIPI-C handler makes into the rest of the
// platform's runtime. A hook left nil does what a platform with no Java side
// does, which is what the SKT Clet engine is.
type RuntimeHooks struct {
	// Supervisor answers every supervisor call the platform's guest code
	// makes, the WIPI-C ones among them. It is required.
	Supervisor armcore.SupervisorCallHandler
	// SyncClips reconciles the platform's own clips with audio that has just
	// been advanced to now.
	SyncClips func(now time.Duration) error
	// StorageUsed replaces the count of stored bytes the WIPI-C files alone
	// make, for a platform whose storage holds more than them.
	StorageUsed func() (int, error)
	// DescribeGuestFault replaces the registers-only report of a guest fault.
	DescribeGuestFault func(faultContext armcore.Context, fault *armcore.InstructionError) string
	// DescribeGuestWord names a word the guest handed the platform.
	DescribeGuestWord func(word uint32) string
	// NotAFault reports an error that ends a guest call without being a fault
	// in it, such as an exception unwinding through it.
	NotAFault func(err error) bool
	// NoteRead sees every guest read the engine makes of words, bytes and C
	// strings, for a platform that records what its loader read.
	NoteRead func(address uint32, data []byte, cString bool)
	// WorkerThread is the guest thread a call runs on when the platform is
	// running one of its own threads rather than the client thread.
	WorkerThread func() (*armcore.Thread, bool)
	// InputCardCurrent and InputActivated tie the C input method to the screen
	// it was opened on: activated records it, and current says it is still
	// the one shown.
	InputCardCurrent func() bool
	InputActivated   func()
	// RepaintCard is what a timer callback that asks for a repaint painted,
	// recorded on the timer for the platform.
	RepaintCard func() any
	// RecordDatabase answers the WIPI-C record database table, MC_db*, for a
	// platform that serves it.
	RecordDatabase func(thread *armcore.Thread, function uint32) (uint32, error)
	// ReservedKernelSlot is what the reserved kernel slot answers.
	ReservedKernelSlot func() uint32
	// Freed sees a WIPI-C memory identifier given back.
	Freed func(id uint32)
	// SaveBlocked refuses to store while the platform's storage cannot be.
	SaveBlocked func() error
	// CollectForAllocation frees what the platform can when an allocation
	// fails, and reports whether anything was.
	CollectForAllocation func() bool
	// CollectHostResources releases Host resources the guest no longer holds.
	CollectHostResources func()
	// ScreenFlushed sees every flush of the screen framebuffer.
	ScreenFlushed func()
}

// supervisor runs one supervisor call through the platform's handler.
func (runtime *Runtime) supervisor(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	return runtime.Hooks.Supervisor(ctx, thread, call)
}

// platformName and platformCName are the platform's names for the messages
// the engine writes; see Platform.
func (client *Client) platformName() string {
	if client == nil {
		return "WIPI-C"
	}
	return client.Platform.Name
}

func (runtime *Runtime) platformName() string {
	if runtime == nil {
		return "WIPI-C"
	}
	return runtime.Client.platformName()
}

func (runtime *Runtime) platformCName() string {
	if runtime == nil || runtime.Client == nil {
		return "WIPI C"
	}
	return runtime.Client.Platform.CName
}

// collectForAllocation lets the platform free what it can when an allocation
// the arena cannot satisfy is asked for. A platform with no collector has
// nothing to free, so the allocation simply fails.
func (runtime *Runtime) collectForAllocation() bool {
	return runtime.Hooks.CollectForAllocation != nil && runtime.Hooks.CollectForAllocation()
}

// CollectHostResources lets the platform release Host resources the guest no
// longer holds, such as the image surfaces and clips of collected objects.
func (runtime *Runtime) CollectHostResources() {
	if runtime.Hooks.CollectHostResources != nil {
		runtime.Hooks.CollectHostResources()
	}
}
