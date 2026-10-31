package wipicrt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/glyph"
	"github.com/movingwoo/wfeature/internal/wipic"
	"golang.org/x/text/encoding/korean"
)

// This file collects the small helpers the WIPI-C handlers share: guest memory
// reads and writes, storage names, fonts, the guest-call nesting bound, and the
// platform answers a handler asks for through Hooks.

// SyncClipCompletions advances the audio clock so a playing WIPI-C clip retires
// on the guest's timeline, and then lets the platform reconcile clips of its
// own with it.
func (runtime *Runtime) SyncClipCompletions(now time.Duration) error {
	if runtime.Client.Audio != nil {
		runtime.Client.Audio.Advance(now)
	}
	if runtime.Hooks.SyncClips != nil {
		return runtime.Hooks.SyncClips(now)
	}
	return nil
}

// HookBuffer lends the scratch space a hooked memcpy, memset or strcpy needs.
//
// **These were the largest allocation this platform made.** A hooked memcpy
// reads the source into a buffer and writes that buffer to the destination,
// and it made a fresh one every call: measured over two thousand ticks of one
// local title that was 302 MB, 72% of everything the run allocated — the
// buffer is sized by the guest's own request, so a title that moves a sprite
// a few dozen times a frame allocates that much a few dozen times a frame,
// only to drop it before the next instruction.
//
// It can be lent for the same reason a frame can: nothing keeps it. The bytes
// are read in and written out inside one call, and no hook here returns a
// slice or hands one to anything that outlives the crossing. **And nothing
// else is running**: this platform grants one guest thread a slice at a time
// and blocks until it parks, so runtime state is never touched by two
// goroutines at once — see workers.go, which is the same invariant every other
// map and buffer on this runtime already rests on.
//
// The bytes are not cleared. Every caller fills the whole slice before reading
// it — a copy reads the source over it, a fill writes every byte — so what a
// previous call left is overwritten rather than seen, and clearing it would be
// paying for the zeroing that `make` was doing.
func (runtime *Runtime) HookBuffer(length uint32) []byte {
	if length > hookWindowLimit {
		return make([]byte, length)
	}
	if uint32(cap(runtime.hookWindow)) < length {
		runtime.hookWindow = make([]byte, length, hookWindowLimit)
	}
	return runtime.hookWindow[:length]
}

// HookRange refuses a range the guest cannot hold before a buffer is sized
// from its length. The length is the guest's own r2, and a negative computed
// size arrives as nearly four gigabytes: allocating and filling that much
// first, only for the write to refuse it, can take the whole process down. A
// length the reusable window covers costs nothing to allocate, so only a
// longer one is checked here; the read or write after it still checks every
// range.
func HookRange(memory *armcore.Memory, address, length uint32, permission armcore.Permission) error {
	if length <= hookWindowLimit {
		return nil
	}
	return memory.ValidateRange(address, uint64(length), permission)
}

// GuestStringLength measures the string at start for the strlen and strcpy
// hooks, with the scan every KTF string reader shares (scanGuestString).
// maxGuestStringLength bounds the walk, so a string that is never terminated
// fails instead of reading the address space.
func (runtime *Runtime) GuestStringLength(memory *armcore.Memory, start uint32) (uint32, error) {
	var buffer [GuestStringBuffer]byte
	length, terminated, err := ScanGuestString(memory, start, MaxGuestStringLength, 1, &buffer)
	if err != nil {
		return 0, err
	}
	if !terminated {
		return 0, fmt.Errorf("%s guest string at %#x is not terminated within %d bytes", runtime.platformName(), start, MaxGuestStringLength)
	}
	return length, nil
}

const MaxGuestStringLength = 1 << 20

// SplitRemovalList reads one back, dropping only the empty line an empty list
// leaves behind.
func SplitRemovalList(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	names := make([]string, 0, len(lines))
	for _, name := range lines {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ReservedStorageName answers whether a guest-chosen name addresses one of the
// lists its own table keeps. The test is against the key the name normalizes
// to rather than against the name: NormalizeSaveKey drops empty and "."
// components, so "./.removed" and ".removed/" reach the same file.
func ReservedStorageName(scope, name string) bool {
	key, err := backend.NormalizeSaveKey(scope + "/" + name)
	if err != nil {
		return false
	}
	rest, found := strings.CutPrefix(key, scope+"/")
	return found && ReservedStorageNames[scope][rest]
}

// StorableName is what both tables accept from a guest. A name has to be a
// save key on its own, has to survive the removal list's own encoding, and
// must not address the list itself.
//
// The list is names joined by newlines and read back a line at a time with the
// surrounding space trimmed, so a name carrying either would not come back as
// itself: deleting "A\nB" would hide the unrelated databases A and B, and
// deleting "save " would hide "save", which nobody deleted. Neither table
// bounded those characters, and both accept whatever string the guest built.
// It does not bound the length. That bound is the WIPI C record database's
// own, from its specification, and applying it to the Java class would refuse
// names that class has always accepted — eleven Korean characters are
// thirty-three bytes, so a title using one, and any save already written under
// it, would stop working.
func StorableName(scope, name string) bool {
	if name == "" {
		return false
	}
	// Only the line separators. A name with a space around it is a name a
	// title may already have a save under, and the list carries it as it is.
	if strings.ContainsAny(name, "\n\r") {
		return false
	}
	// And it has to be a key of its own under the scope. NormalizeSaveKey
	// refuses "..", and it *collapses* ".", "/" and "./" away — so those
	// passed, and the store then wrote the scope itself: a regular file named
	// "jdb" where the directory belongs, after which every jdb write for that
	// title fails with "not a directory" and stays failing across sessions,
	// the removal list among them. What survives normalization has to still
	// be the scope and a name under it.
	key, err := backend.NormalizeSaveKey(scope + "/" + name)
	if err != nil {
		return false
	}
	if rest, under := strings.CutPrefix(key, scope+"/"); !under || rest == "" {
		return false
	}
	return !ReservedStorageName(scope, name)
}

// NameTooLong reports whether a name passes maxWIPIFileName, counted on the
// path it stands for: a backslash is a slash, and leading separators, empty
// and "." segments are not part of it.
func NameTooLong(name string) bool {
	return len(ListingPath(strings.ReplaceAll(name, "\\", "/"))) > MaxWIPIFileName
}

// MaxFileNameRead is how far a C name is read looking for its end. A name
// that runs past it is not a name, and stops the run as an unterminated
// string does everywhere here.
const MaxFileNameRead = 4096

// StorageAvailable answers the budget less what the title has stored, never
// below zero. A store that cannot be read is answered as if nothing were
// stored: the figure is advice, and a title told it has no room refuses to
// save.
func (runtime *Runtime) StorageAvailable() uint32 {
	used, err := runtime.storageUsed()
	if err != nil {
		runtime.CountDiagnostic(fmt.Sprintf("storage used: %v", err))
		used = 0
	}
	if used >= StorageBudget {
		return 0
	}
	return uint32(StorageBudget - used)
}
func (runtime *Runtime) ReadAOTWords(address, count uint32, label string) ([]uint32, error) {
	if count == 0 {
		return nil, nil
	}
	if address == 0 || address&3 != 0 {
		return nil, fmt.Errorf("%s address %#x is null or unaligned", label, address)
	}
	data, err := runtime.ReadAOTBytes(address, uint64(count)*4, label)
	if err != nil {
		return nil, err
	}
	words := make([]uint32, count)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return words, nil
}

// GraphicsTextWidth mirrors the runtime font advance used by Font metrics.
func (runtime *Runtime) GraphicsTextWidth(text []rune) int32 {
	width := int32(0)
	for _, character := range text {
		width += runtime.GraphicsCharAdvance(character)
	}
	return width
}

// RGBFromPixel565 expands a 16-bit target pixel to a 0x00RRGGBB word, keeping
// the top bits in the low positions so white stays white.
func RGBFromPixel565(pixel uint16) uint32 {
	red := uint32(pixel>>11&0x1f) << 3
	green := uint32(pixel>>5&0x3f) << 2
	blue := uint32(pixel&0x1f) << 3
	return red<<16 | green<<8 | blue | red>>5<<16 | green>>6<<8 | blue>>5
}

// FontFace is the pixel face every KTF game's text is drawn and measured with.
//
// It is one face for the whole library rather than one per screen size. The
// screen size the descriptor declares looked like the answer — half these
// archives say 176x220 and half say 240x320 — but it is not: a 240x320 title
// that lays a save slot out as "레벨" and a number in columns runs the label
// into the number and out of the box when its text is drawn at 16, and the
// same title is correct at the smaller size. A game reads its own layout out
// of the font's metrics, and the metrics these games were written against are
// the small ones whatever screen they declare.
func (runtime *Runtime) FontFace() *glyph.Face {
	return glyph.Handset()
}

func (runtime *Runtime) FontHeight() int32 {
	return int32(runtime.FontFace().Height())
}

func (runtime *Runtime) FontBaseline() int32 {
	return int32(runtime.FontFace().Ascent)
}

// serviceTimersLocked also lets a Host text commit finish a timer-owned key
// delivery without releasing the guest execution lock between characters.

func DecodeEUCKR(data []byte) string {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(decoded)
}
func (runtime *Runtime) ReadWord(address uint32) (uint32, error) {
	words, err := runtime.ReadAOTWords(address, 1, "module word")
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

func (runtime *Runtime) WriteWord(address, value uint32) error {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	if err := runtime.Client.core.Memory().Write(address, word[:]); err != nil {
		return fmt.Errorf("write %s guest word at %#x: %w", runtime.platformName(), address, err)
	}
	return nil
}

// InvalidateClip drops any loaded sound because the clip's bytes changed.
func (runtime *Runtime) InvalidateClip(state *ClipState) {
	if state.Loaded && runtime.Client.Audio != nil {
		_ = runtime.Client.Audio.Close(state.Handle)
	}
	state.Loaded = false
	state.Completed = 0
}

// Audio service and native calls run under client.run. Sort handles so
// callbacks from different owners have deterministic observation order.
// guestFile resolves a File path against in-memory writes first, persisted
// save writes second, and the mounted outer-archive filesystem last. Guest
// names may carry a leading slash that the mounted names do not. A removed
// path resolves to none of the three.
func (runtime *Runtime) GuestFile(name string) ([]byte, bool) {
	if runtime.RemovedGuestFiles()[strings.TrimPrefix(name, "/")] {
		return nil, false
	}
	for _, candidate := range []string{name, strings.TrimPrefix(name, "/")} {
		if data, exists := runtime.GuestFiles[candidate]; exists {
			return data, true
		}
	}
	if data, exists := runtime.LoadSave("fs/" + strings.TrimPrefix(name, "/")); exists {
		return data, true
	}
	for _, candidate := range []string{name, strings.TrimPrefix(name, "/")} {
		if data, exists := runtime.Client.Files[candidate]; exists {
			return data, true
		}
	}
	return nil, false
}

// RemovedGuestFiles is the set of deleted paths, read from the store once per
// session and kept in memory after that. Names are kept without their leading
// slash, which is the form the resolution above compares.
func (runtime *Runtime) RemovedGuestFiles() map[string]bool {
	if runtime.RemovedFiles != nil {
		return runtime.RemovedFiles
	}
	runtime.RemovedFiles = make(map[string]bool)
	if data, exists := runtime.LoadSave(guestFileRemovedKey); exists {
		for _, line := range SplitRemovalList(data) {
			runtime.RemovedFiles[line] = true
		}
	}
	return runtime.RemovedFiles
}

// ===== bridge2: nesting, fonts, faults, timers =====
// svcStub builds the Thumb stub a guest reaches the platform through. It
// stashes the identifier in r12, crosses the supervisor-call boundary and
// returns through lr, so the guest calls it like any other function and a
// handler that answers with nothing still returns to the caller.
//
// Three places build one and they had drifted into three copies of the same
// twelve bytes: the platform's callback tables, the hooks written over a
// recognized C routine in the guest image, and the trap tables the native
// package's loader plants. The identifier is what tells them apart, so the
// bytes are shared and only the category and the identifier differ.
func SVCStub(category, id uint32) []byte {
	return []byte{
		0x10, 0xb4, // push {r4}
		0x02, 0x4c, // ldr r4, [pc, #8]
		0xa4, 0x46, // mov r12, r4
		0x10, 0xbc, // pop {r4}
		byte(category), 0xdf, // svc #category
		0x70, 0x47, // bx lr
		byte(id), byte(id >> 8), byte(id >> 16), byte(id >> 24),
	}
}

// serviceTimersLocked also lets a Host text commit finish a timer-owned key
// delivery without releasing the guest execution lock between characters.
// storageUsed counts each file and database the title has written at its size
// less the size of the archive's file of the same name, never below zero. A
// title's own content that it shipped is not space it used, and a save that
// rewrites a packaged file only uses what it added. Databases count their
// record bytes. The lists the platform keeps beside them are not counted.
//
// What this session holds in memory wins over the store for the same name,
// because it is newer, and a session without a store has nothing else.
func (runtime *Runtime) storageUsed() (int, error) {
	if runtime.Hooks.StorageUsed != nil {
		return runtime.Hooks.StorageUsed()
	}
	// With nothing but the WIPI-C file table, the storage a program occupies
	// is the bytes its open files hold over what the package shipped.
	used := 0
	for _, store := range runtime.CFiles {
		if len(store.Data) > store.Packaged {
			used += len(store.Data) - store.Packaged
		}
	}
	return used, nil
}
func (runtime *Runtime) GraphicsCharAdvance(character rune) int32 {
	if character == 0 {
		return 0
	}
	// WIPI Java uses five-pixel printable ASCII cells beside ten-pixel
	// Korean syllables. Space-padded overlays replace ASCII with one space
	// and Korean with two; the font asset's proportional advances would
	// leave colored fringes after punctuation and narrow Latin characters.
	if character >= ' ' && character <= '~' {
		return 5
	}
	return int32(runtime.FontFace().Render(character).Advance)
}

// DescribeGuestFault turns a guest instruction fault into the same evidence a
// guest throw already carries. A fault comes back as an address and an opcode
// — "read guest memory at 0x4" — and the address is never the question: what
// was being dispatched, on which object, and from which Java method are. The
// registers at the faulting instruction hold all three, so they are named the
// way every other boundary event names a guest word.
//
// It reads only, and every read is best effort: a fault has already left the
// guest somewhere unexpected, so a probe that fails says so and the rest of
// the report still arrives.
func (runtime *Runtime) DescribeGuestFault(faultContext armcore.Context, fault *armcore.InstructionError) string {
	if runtime.Hooks.DescribeGuestFault != nil {
		return runtime.Hooks.DescribeGuestFault(faultContext, fault)
	}
	registers := faultContext.Registers[:armcore.RegisterSP]
	stackPointer := faultContext.Registers[armcore.RegisterSP]
	linkRegister := faultContext.Registers[armcore.RegisterLR]
	// With no metadata to symbolize against, the fault is reported by its
	// registers and the address it names. The engine has already advanced the
	// program counter past the faulting instruction, so the address that means
	// anything is the one the fault itself carries.
	parts := []string{fmt.Sprintf("regs=%#x sp=%#x lr=%#x pc=%#x", registers, stackPointer, linkRegister, fault.PC)}
	var access *armcore.AccessError
	if errors.As(fault.Cause, &access) {
		parts = append(parts, fmt.Sprintf("fault=%#x", access.Address))
	}
	return strings.Join(parts, " ")
}

// GuestFault reports whether an error from a guest run is an instruction fault
// rather than one of the two control-flow errors the AOT bridge raises through
// the same return.
func (runtime *Runtime) GuestFault(err error) (*armcore.InstructionError, bool) {
	if runtime.Hooks.NotAFault != nil && runtime.Hooks.NotAFault(err) {
		return nil, false
	}
	var fault *armcore.InstructionError
	if errors.As(err, &fault) {
		return fault, true
	}
	return nil, false
}

func (runtime *Runtime) ReadAOTBytes(address uint32, size uint64, label string) ([]byte, error) {
	if address == 0 || size == 0 || size > MaxPlatformAllocation {
		return nil, fmt.Errorf("%s range at %#x has invalid size %d", label, address, size)
	}
	if uint64(address)+size > uint64(1)<<32 {
		return nil, fmt.Errorf("%s range at %#x overflows guest memory", label, address)
	}
	data := make([]byte, int(size))
	if err := runtime.Client.core.Memory().Read(address, data); err != nil {
		return nil, fmt.Errorf("read %s at %#x: %w", label, address, err)
	}
	if runtime.Hooks.NoteRead != nil {
		runtime.Hooks.NoteRead(address, data, false)
	}
	return data, nil
}
func (runtime *Runtime) EnterAOTCall() error {
	owner := runtime.AOTCallOwner()
	if runtime.AOTCallDepth[owner] >= MaxAOTCallDepth {
		return fmt.Errorf("%s guest call nesting exceeds %d", runtime.Client.Platform.CallName, MaxAOTCallDepth)
	}
	if runtime.AOTCallDepth == nil {
		runtime.AOTCallDepth = make(map[*armcore.Thread]uint32)
	}
	runtime.AOTCallDepth[owner]++
	return nil
}

// Derived ARM call frames belong to the worker holding the grant, not to a
// new logical thread. Host callbacks belong to the root client thread.
func (runtime *Runtime) AOTCallOwner() *armcore.Thread {
	if runtime.Hooks.WorkerThread != nil {
		if thread, ok := runtime.Hooks.WorkerThread(); ok {
			return thread
		}
	}
	if runtime.Client.Thread != nil {
		return runtime.Client.Thread
	}
	return runtime.CurrentThread
}

func (runtime *Runtime) LeaveAOTCall(thread *armcore.Thread) {
	if depth := runtime.AOTCallDepth[thread]; depth > 1 {
		runtime.AOTCallDepth[thread] = depth - 1
		return
	}
	delete(runtime.AOTCallDepth, thread)
}

// Blend565 mixes a source pixel into a destination one by coverage, in the
// 5/6/5 space both already live in. Full coverage is the source untouched, so
// a glyph that lands on whole pixels — which every glyph on the 16-dot grid
// does — comes out of this exactly as it did when text was plotted rather than
// blended.
func Blend565(destination, source uint16, coverage uint8) uint16 {
	if coverage == 0xff {
		return source
	}
	mix := func(from, to uint32) uint32 {
		return (from*uint32(0xff-coverage) + to*uint32(coverage) + 0x7f) / 0xff
	}
	red := mix(uint32(destination>>11&0x1f), uint32(source>>11&0x1f))
	green := mix(uint32(destination>>5&0x3f), uint32(source>>5&0x3f))
	blue := mix(uint32(destination&0x1f), uint32(source&0x1f))
	return uint16(red<<11 | green<<5 | blue)
}

// A removal list is one encoding, written here and read here, because it was
// four: the two database tables, the guest filesystem and the WIPI C file
// table each had their own pair, and the pairs disagreed about trimming. A
// name with a space around it went out as itself and came back trimmed, so
// deleting "save " hid "save" — found once per list, one review round at a
// time. Names are joined by newlines and read back exactly as written; what
// cannot survive that is refused by storableName rather than mangled here.
func JoinRemovalList(names []string) []byte {
	sort.Strings(names)
	return []byte(strings.Join(names, "\n"))
}

// SystemProperty answers a WIPI system property. The subscriber-number
// properties come from the handset number this session runs as; the rest from
// the shared property table.
func (client *Client) SystemProperty(name string) (string, bool) {
	if name == "PHONENUMBER" || name == "MIN" {
		return client.SubscriberNumber, true
	}
	value, ok := wipic.SystemProperties[name]
	return value, ok
}
