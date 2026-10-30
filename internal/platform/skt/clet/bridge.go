package clet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/encoding/korean"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/glyph"
)

// This file collects the small KTF WIPI-C helpers the copied handlers call,
// brought over unchanged except where a Clet's lack of a Java heap simplifies
// them. See docs/skt-native.md.

// syncClipCompletions advances the audio clock so a playing WIPI-C clip retires
// on the guest's timeline. A Clet has no Java Clip objects to reconcile.
func (runtime *initializationRuntime) syncClipCompletions(now time.Duration) error {
	if runtime.client.audio != nil {
		runtime.client.audio.Advance(now)
	}
	return nil
}

// hookBuffer lends the scratch space a hooked memcpy, memset or strcpy needs.
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
func (runtime *initializationRuntime) hookBuffer(length uint32) []byte {
	if length > hookWindowLimit {
		return make([]byte, length)
	}
	if uint32(cap(runtime.hookWindow)) < length {
		runtime.hookWindow = make([]byte, length, hookWindowLimit)
	}
	return runtime.hookWindow[:length]
}

// hookRange refuses a range the guest cannot hold before a buffer is sized
// from its length. The length is the guest's own r2, and a negative computed
// size arrives as nearly four gigabytes: allocating and filling that much
// first, only for the write to refuse it, can take the whole process down. A
// length the reusable window covers costs nothing to allocate, so only a
// longer one is checked here; the read or write after it still checks every
// range.
func hookRange(memory *armcore.Memory, address, length uint32, permission armcore.Permission) error {
	if length <= hookWindowLimit {
		return nil
	}
	return memory.ValidateRange(address, uint64(length), permission)
}

// guestStringLength measures the string at start for the strlen and strcpy
// hooks, with the scan every KTF string reader shares (scanGuestString).
// maxGuestStringLength bounds the walk, so a string that is never terminated
// fails instead of reading the address space.
func guestStringLength(memory *armcore.Memory, start uint32) (uint32, error) {
	var buffer [guestStringBuffer]byte
	length, terminated, err := scanGuestString(memory, start, maxGuestStringLength, 1, &buffer)
	if err != nil {
		return 0, err
	}
	if !terminated {
		return 0, fmt.Errorf("KTF guest string at %#x is not terminated within %d bytes", start, maxGuestStringLength)
	}
	return length, nil
}

const maxGuestStringLength = 1 << 20

// splitRemovalList reads one back, dropping only the empty line an empty list
// leaves behind.
func splitRemovalList(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	names := make([]string, 0, len(lines))
	for _, name := range lines {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// reservedStorageName answers whether a guest-chosen name addresses one of the
// lists its own table keeps. The test is against the key the name normalizes
// to rather than against the name: NormalizeSaveKey drops empty and "."
// components, so "./.removed" and ".removed/" reach the same file.
func reservedStorageName(scope, name string) bool {
	key, err := backend.NormalizeSaveKey(scope + "/" + name)
	if err != nil {
		return false
	}
	rest, found := strings.CutPrefix(key, scope+"/")
	return found && reservedStorageNames[scope][rest]
}

// storableName is what both tables accept from a guest. A name has to be a
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
func storableName(scope, name string) bool {
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
	return !reservedStorageName(scope, name)
}

// nameTooLong reports whether a name passes maxWIPIFileName, counted on the
// path it stands for: a backslash is a slash, and leading separators, empty
// and "." segments are not part of it.
func nameTooLong(name string) bool {
	return len(listingPath(strings.ReplaceAll(name, "\\", "/"))) > maxWIPIFileName
}

// maxFileNameRead is how far a C name is read looking for its end. A name
// that runs past it is not a name, and stops the run as an unterminated
// string does everywhere here.
const maxFileNameRead = 4096

// storageAvailable answers the budget less what the title has stored, never
// below zero. A store that cannot be read is answered as if nothing were
// stored: the figure is advice, and a title told it has no room refuses to
// save.
func (runtime *initializationRuntime) storageAvailable() uint32 {
	used, err := runtime.storageUsed()
	if err != nil {
		runtime.countDiagnostic(fmt.Sprintf("storage used: %v", err))
		used = 0
	}
	if used >= storageBudget {
		return 0
	}
	return uint32(storageBudget - used)
}
func (runtime *initializationRuntime) readAOTWords(address, count uint32, label string) ([]uint32, error) {
	if count == 0 {
		return nil, nil
	}
	if address == 0 || address&3 != 0 {
		return nil, fmt.Errorf("%s address %#x is null or unaligned", label, address)
	}
	data, err := runtime.readAOTBytes(address, uint64(count)*4, label)
	if err != nil {
		return nil, err
	}
	words := make([]uint32, count)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return words, nil
}

// graphicsTextWidth mirrors the runtime font advance used by Font metrics.
func (runtime *initializationRuntime) graphicsTextWidth(text []rune) int32 {
	width := int32(0)
	for _, character := range text {
		width += runtime.graphicsCharAdvance(character)
	}
	return width
}

// rgbFromPixel565 expands a 16-bit target pixel to a 0x00RRGGBB word, keeping
// the top bits in the low positions so white stays white.
func rgbFromPixel565(pixel uint16) uint32 {
	red := uint32(pixel>>11&0x1f) << 3
	green := uint32(pixel>>5&0x3f) << 2
	blue := uint32(pixel&0x1f) << 3
	return red<<16 | green<<8 | blue | red>>5<<16 | green>>6<<8 | blue>>5
}

// fontFace is the pixel face every KTF game's text is drawn and measured with.
//
// It is one face for the whole library rather than one per screen size. The
// screen size the descriptor declares looked like the answer — half these
// archives say 176x220 and half say 240x320 — but it is not: a 240x320 title
// that lays a save slot out as "레벨" and a number in columns runs the label
// into the number and out of the box when its text is drawn at 16, and the
// same title is correct at the smaller size. A game reads its own layout out
// of the font's metrics, and the metrics these games were written against are
// the small ones whatever screen they declare.
func (runtime *initializationRuntime) fontFace() *glyph.Face {
	return glyph.Handset()
}

func (runtime *initializationRuntime) fontHeight() int32 {
	return int32(runtime.fontFace().Height())
}

func (runtime *initializationRuntime) fontBaseline() int32 {
	return int32(runtime.fontFace().Ascent)
}

// serviceTimersLocked also lets a Host text commit finish a timer-owned key
// delivery without releasing the guest execution lock between characters.

func decodeEUCKR(data []byte) string {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(decoded)
}
func (runtime *initializationRuntime) readWord(address uint32) (uint32, error) {
	words, err := runtime.readAOTWords(address, 1, "module word")
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

func (runtime *initializationRuntime) writeWord(address, value uint32) error {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	if err := runtime.client.core.Memory().Write(address, word[:]); err != nil {
		return fmt.Errorf("write KTF guest word at %#x: %w", address, err)
	}
	return nil
}

// invalidateClip drops any loaded sound because the clip's bytes changed.
func (runtime *initializationRuntime) invalidateClip(state *clipState) {
	if state.loaded && runtime.client.audio != nil {
		_ = runtime.client.audio.Close(state.handle)
	}
	state.loaded = false
	state.completed = 0
}

// Audio service and native calls run under client.run. Sort handles so
// callbacks from different owners have deterministic observation order.
// guestFile resolves a File path against in-memory writes first, persisted
// save writes second, and the mounted outer-archive filesystem last. Guest
// names may carry a leading slash that the mounted names do not. A removed
// path resolves to none of the three.
func (runtime *initializationRuntime) guestFile(name string) ([]byte, bool) {
	if runtime.removedGuestFiles()[strings.TrimPrefix(name, "/")] {
		return nil, false
	}
	for _, candidate := range []string{name, strings.TrimPrefix(name, "/")} {
		if data, exists := runtime.guestFiles[candidate]; exists {
			return data, true
		}
	}
	if data, exists := runtime.loadSave("fs/" + strings.TrimPrefix(name, "/")); exists {
		return data, true
	}
	for _, candidate := range []string{name, strings.TrimPrefix(name, "/")} {
		if data, exists := runtime.client.files[candidate]; exists {
			return data, true
		}
	}
	return nil, false
}

// removedGuestFiles is the set of deleted paths, read from the store once per
// session and kept in memory after that. Names are kept without their leading
// slash, which is the form the resolution above compares.
func (runtime *initializationRuntime) removedGuestFiles() map[string]bool {
	if runtime.removedFiles != nil {
		return runtime.removedFiles
	}
	runtime.removedFiles = make(map[string]bool)
	if data, exists := runtime.loadSave(guestFileRemovedKey); exists {
		for _, line := range splitRemovalList(data) {
			runtime.removedFiles[line] = true
		}
	}
	return runtime.removedFiles
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
func svcStub(category, id uint32) []byte {
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
func (runtime *initializationRuntime) storageUsed() (int, error) {
	// A Clet uses only the WIPI-C file table, so the storage it occupies is the
	// bytes its open files hold over what the package shipped. The record and
	// Java database tables a KTF title also counts have no caller here.
	used := 0
	for _, store := range runtime.cFiles {
		if len(store.data) > store.packaged {
			used += len(store.data) - store.packaged
		}
	}
	return used, nil
}
func (runtime *initializationRuntime) graphicsCharAdvance(character rune) int32 {
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
	return int32(runtime.fontFace().Render(character).Advance)
}

// describeGuestFault turns a guest instruction fault into the same evidence a
// guest throw already carries. A fault comes back as an address and an opcode
// — "read guest memory at 0x4" — and the address is never the question: what
// was being dispatched, on which object, and from which Java method are. The
// registers at the faulting instruction hold all three, so they are named the
// way every other boundary event names a guest word.
//
// It reads only, and every read is best effort: a fault has already left the
// guest somewhere unexpected, so a probe that fails says so and the rest of
// the report still arrives.
func (runtime *initializationRuntime) describeGuestFault(faultContext armcore.Context, fault *armcore.InstructionError) string {
	registers := faultContext.Registers[:armcore.RegisterSP]
	stackPointer := faultContext.Registers[armcore.RegisterSP]
	linkRegister := faultContext.Registers[armcore.RegisterLR]
	// A Clet carries no AOT metadata, so the fault is reported by its registers
	// and the address it names rather than symbolized against guest methods. The
	// engine has already advanced the program counter past the faulting
	// instruction, so the address that means anything is the one the fault
	// itself carries.
	parts := []string{fmt.Sprintf("regs=%#x sp=%#x lr=%#x pc=%#x", registers, stackPointer, linkRegister, fault.PC)}
	var access *armcore.AccessError
	if errors.As(fault.Cause, &access) {
		parts = append(parts, fmt.Sprintf("fault=%#x", access.Address))
	}
	return strings.Join(parts, " ")
}

// guestFault reports whether an error from a guest run is an instruction fault
// rather than one of the two control-flow errors the AOT bridge raises through
// the same return.
func guestFault(err error) (*armcore.InstructionError, bool) {
	var fault *armcore.InstructionError
	if errors.As(err, &fault) {
		return fault, true
	}
	return nil, false
}

func (runtime *initializationRuntime) readAOTBytes(address uint32, size uint64, label string) ([]byte, error) {
	if address == 0 || size == 0 || size > maxPlatformAllocation {
		return nil, fmt.Errorf("%s range at %#x has invalid size %d", label, address, size)
	}
	if uint64(address)+size > uint64(1)<<32 {
		return nil, fmt.Errorf("%s range at %#x overflows guest memory", label, address)
	}
	data := make([]byte, int(size))
	if err := runtime.client.core.Memory().Read(address, data); err != nil {
		return nil, fmt.Errorf("read %s at %#x: %w", label, address, err)
	}
	return data, nil
}
func (runtime *initializationRuntime) enterAOTCall() error {
	owner := runtime.aotCallOwner()
	if runtime.aotCallDepth[owner] >= maxAOTCallDepth {
		return fmt.Errorf("KTF AOT guest call nesting exceeds %d", maxAOTCallDepth)
	}
	if runtime.aotCallDepth == nil {
		runtime.aotCallDepth = make(map[*armcore.Thread]uint32)
	}
	runtime.aotCallDepth[owner]++
	return nil
}

// Derived ARM call frames belong to the worker holding the grant, not to a
// new logical thread. Host callbacks belong to the root client thread.
func (runtime *initializationRuntime) aotCallOwner() *armcore.Thread {
	if runtime.client.thread != nil {
		return runtime.client.thread
	}
	return runtime.currentThread
}

func (runtime *initializationRuntime) leaveAOTCall(thread *armcore.Thread) {
	if depth := runtime.aotCallDepth[thread]; depth > 1 {
		runtime.aotCallDepth[thread] = depth - 1
		return
	}
	delete(runtime.aotCallDepth, thread)
}
