package lgt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// callSlot drives one WIPI C slot the way the SVC handler would, so a slot's
// contract can be checked without assembling a module that calls it.
func callSlot(t *testing.T, client *Client, slot uint32, arguments ...uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range arguments {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.handleWIPICSVC(context.Background(), thread, slot); err != nil {
		t.Fatalf("slot %#x: %v", slot, err)
	}
	result, err := thread.Register(0)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func fixtureClient(t *testing.T) *Client {
	t.Helper()
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// The 0x1f8 table is LGT's own and its contents are unknown, but every
// non-Java module resolves it during its initializer. Refusing the table there
// stops a module before it has registered anything, which is far from the call
// it would have made.
func TestOEMImportTableResolvesRatherThanStoppingTheInitializer(t *testing.T) {
	client := fixtureClient(t)

	stub, err := client.importFunction(importTableOEM, oemSlotConfigure)
	if err != nil {
		t.Fatalf("the OEM table did not resolve: %v", err)
	}
	if stub == 0 {
		t.Fatal("the OEM table resolved to a null pointer")
	}
	// An unrecorded slot in the table still resolves, for the same reason a
	// WIPI C slot does: a module resolves everything it might use at startup.
	if _, err := client.importFunction(importTableOEM, 0x400); err != nil {
		t.Fatalf("an unrecorded OEM slot refused to resolve: %v", err)
	}

	// The one slot modules call is accepted and answers without disturbing
	// anything. Reaching any other one is reported rather than assumed.
	thread := armcore.NewThread(armcore.NewContext())
	if err := client.handleOEMSVC(context.Background(), thread, oemSlotConfigure); err != nil {
		t.Fatalf("the configure slot was not accepted: %v", err)
	}
	if err := client.handleOEMSVC(context.Background(), thread, 0x400); err == nil {
		t.Fatal("an unrecorded OEM slot was reached without being reported")
	}
}

// MC_knlGetResourceID and MC_knlGetResource are a pair: the first turns a name
// into an id, the second reads that id. Answering the size from the first call
// looks right until the second one arrives, because the size is then what lands
// where a name was expected.
func TestResourceIDIsAHandleTheResourceCallAccepts(t *testing.T) {
	client := fixtureClient(t)

	const packaged = "data/hello.txt"
	name, err := client.allocateBytes(append([]byte(packaged), 0))
	if err != nil {
		t.Fatal(err)
	}
	size, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}

	identifier := callSlot(t, client, slotGetResourceID, name, size)
	if identifier == 0 || int32(identifier) < 0 {
		t.Fatalf("resource id = %#x, want a handle", identifier)
	}
	length, err := client.readWord(size)
	if err != nil {
		t.Fatal(err)
	}
	if length != uint32(len("packaged")) {
		t.Fatalf("resource size = %d, want %d", length, len("packaged"))
	}

	buffer, err := client.allocate(uint64(length))
	if err != nil {
		t.Fatal(err)
	}
	// The read answers 0, not the byte count: its callers treat a nonzero
	// answer as the failure branch and free the buffer it just filled.
	if read := callSlot(t, client, slotGetResource, identifier, buffer, length); read != 0 {
		t.Fatalf("resource read = %#x, want 0", read)
	}
	content := make([]byte, length)
	if err := client.core.Memory().Read(buffer, content); err != nil {
		t.Fatal(err)
	}
	if string(content) != "packaged" {
		t.Fatalf("resource content = %q", content)
	}

	// A buffer too small for the resource is the one documented failure.
	short, err := client.allocate(uint64(length))
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotGetResource, identifier, short, length-1); int32(result) != wipiShortBuffer {
		t.Fatalf("a short buffer answered %#x, want %d", result, wipiShortBuffer)
	}

	// A missing resource reports itself and writes a zero size, so a title that
	// checks the size rather than the id still sees nothing there.
	missing, err := client.allocateBytes(append([]byte("data/absent.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotGetResourceID, missing, size); int32(result) >= 0 {
		t.Fatalf("a missing resource answered %#x", result)
	}
	if length, err := client.readWord(size); err != nil || length != 0 {
		t.Fatalf("missing resource size = %d/%v, want 0", length, err)
	}
}

// The id is the resource's identity, so asking twice for the same name has to
// answer the same value. A title here reads its resource list at boot and
// keeps `{id, size}` beside each name, then loads a resource by asking for its
// id again and searching that table for it: a fresh handle per call makes
// every one of those searches miss, and the title sizes its next allocation
// from the zero the miss left behind.
func TestResourceIDIsStablePerName(t *testing.T) {
	client := fixtureClient(t)

	size, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	identifier := func(resource string) uint32 {
		t.Helper()
		name, err := client.allocateBytes(append([]byte(resource), 0))
		if err != nil {
			t.Fatal(err)
		}
		result := callSlot(t, client, slotGetResourceID, name, size)
		if int32(result) < 0 {
			t.Fatalf("resource %q answered %#x", resource, result)
		}
		return result
	}

	first := identifier("data/hello.txt")
	if again := identifier("data/hello.txt"); again != first {
		t.Fatalf("the same resource answered %#x then %#x", first, again)
	}
	// Two names still have to be told apart, since the id is what a title
	// searches its own table with.
	if other := identifier("data/other.txt"); other == first {
		t.Fatalf("two resources share the id %#x", other)
	}
}

// MC_knlCalloc is "allocate and zero" and takes one size, not C's (count,
// size) pair. Multiplying in a second argument that is really the caller's
// stack turns a small request into one that cannot be served, and the game then
// walks into the null it was handed.
func TestCallocTakesOneSizeAndZeroesIt(t *testing.T) {
	client := fixtureClient(t)

	// The second register holds something large, as the caller's stack pointer
	// would. A count-times-size reading of it cannot be served.
	address := callSlot(t, client, slotCalloc, 64, 0x40000000)
	if address == 0 {
		t.Fatal("calloc answered null for a 64 byte request")
	}
	content := make([]byte, 64)
	if err := client.core.Memory().Read(address, content); err != nil {
		t.Fatal(err)
	}
	for index, value := range content {
		if value != 0 {
			t.Fatalf("calloc byte %d = %#x, want zero", index, value)
		}
	}
}

// The system properties are shared with the other WIPI platform: the question
// is about the handset, not about which runtime is being asked.
func TestSystemPropertyAnswersTheHandsetQuestions(t *testing.T) {
	client := fixtureClient(t)

	name, err := client.allocateBytes(append([]byte("PHONEMODEL"), 0))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotGetProperty, name, out, 32); int32(result) != wipiSuccess {
		t.Fatalf("PHONEMODEL = %d, want success", int32(result))
	}
	value, err := client.readCString(out)
	if err != nil || value == "" {
		t.Fatalf("PHONEMODEL value = %q/%v", value, err)
	}

	// A buffer too small to hold the answer is refused rather than overrun.
	if result := callSlot(t, client, slotGetProperty, name, out, 2); int32(result) == wipiSuccess {
		t.Fatal("a short buffer was filled anyway")
	}

	// A property this platform has no answer for is refused, not invented.
	unknown, err := client.allocateBytes(append([]byte("NOT_A_PROPERTY"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotGetProperty, unknown, out, 32); int32(result) == wipiSuccess {
		t.Fatal("an unknown property was answered")
	}
}

// MC_knlSprintk renders into a guest buffer. The renderer is shared with the
// other WIPI platform, so this checks the wiring rather than the subset.
func TestSprintkRendersIntoTheGuestBuffer(t *testing.T) {
	client := fixtureClient(t)

	format, err := client.allocateBytes(append([]byte("%s-%d-%04x"), 0))
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.allocateBytes(append([]byte("slot"), 0))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range []uint32{out, format, text, 7} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	// The fourth vararg lives on the stack, which is where the guest would put
	// it; point the stack at a word holding it.
	stack, err := client.allocateWords([]uint32{0x2a})
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	written, err := client.wipicSprintk(thread)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := client.readCString(out)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "slot-7-002a" {
		t.Fatalf("sprintk rendered %q, want %q", rendered, "slot-7-002a")
	}
	if int(written) != len(rendered) {
		t.Fatalf("sprintk returned %d, want the %d bytes it wrote", written, len(rendered))
	}
}

// A slot this platform accepts without implementing is recorded with its
// reason, because it fails differently from an unimplemented one: the game
// believes it was served. Anything not on that list still stops and says so.
func TestUnknownSlotsAreAcceptedOnlyWhenRecorded(t *testing.T) {
	client := fixtureClient(t)

	// The list being empty is the good outcome and not a broken fixture: every
	// slot that was on it has since been identified. What has to keep holding
	// is that anything still on it carries a reason and is not also
	// implemented, and that a slot on neither list stops.
	for slot, reason := range acceptedUnknownSlots {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("slot %#x is accepted without a reason", slot)
		}
		if knownWIPICSlot(slot) {
			t.Fatalf("slot %#x is implemented and does not belong on this list", slot)
		}
		callSlot(t, client, slot)
	}

	thread := armcore.NewThread(armcore.NewContext())
	if err := client.handleWIPICSVC(context.Background(), thread, 0xfffe); err == nil {
		t.Fatal("an unrecorded slot was served instead of reported")
	}
}

// MC_imGetSupportedModes answers a `M_Char **`: a widget loads the first
// pointer and hands it to strstr, so a zero faults inside the game rather than
// at the call, and a fresh table per call would leak.
func TestSupportedInputModesAnswerAStringArray(t *testing.T) {
	client := fixtureClient(t)

	count := callSlot(t, client, slotIMGetSupportedModeCount)
	if count != uint32(len(inputModes)) {
		t.Fatalf("the mode count answered %d, want %d", count, len(inputModes))
	}

	table := callSlot(t, client, slotIMGetSupportedModes)
	if table == 0 {
		t.Fatal("the supported modes answered null")
	}
	for index, want := range inputModes {
		pointer, err := client.readWord(table + uint32(index)*4)
		if err != nil {
			t.Fatalf("mode %d is not readable: %v", index, err)
		}
		text, err := client.readCString(pointer)
		if err != nil {
			t.Fatalf("mode %d's code is not readable: %v", index, err)
		}
		if text != want {
			t.Fatalf("mode %d named %q, want %q", index, text, want)
		}
	}
	if again := callSlot(t, client, slotIMGetSupportedModes); again != table {
		t.Fatalf("a second call answered %#x, want the same table %#x", again, table)
	}
}

// The mode a widget selects is the one the automaton reports back, and a mode
// outside the list is refused rather than stored: the specification answers
// this call with 1 or 0 rather than with an error code.
func TestCurrentInputModeIsKeptAndBounded(t *testing.T) {
	client := fixtureClient(t)

	if applied := callSlot(t, client, slotIMSetCurrentMode, 2); applied != 1 {
		t.Fatalf("setting mode 2 answered %d, want 1", applied)
	}
	if mode := callSlot(t, client, slotIMGetCurrentMode); mode != 2 {
		t.Fatalf("the current mode is %d, want 2", mode)
	}
	beyond := uint32(len(inputModes))
	if applied := callSlot(t, client, slotIMSetCurrentMode, beyond); applied != 0 {
		t.Fatalf("setting mode %d answered %d, want 0", beyond, applied)
	}
	if mode := callSlot(t, client, slotIMGetCurrentMode); mode != 2 {
		t.Fatalf("a refused mode changed the current one to %d", mode)
	}
}

// MC_imHandleInput composes nothing here, and "nothing" has to be said in the
// caller's completed-string buffer rather than left as whatever the stack held.
func TestHandleInputComposesNothingAndSaysSo(t *testing.T) {
	client := fixtureClient(t)

	buffer, err := client.allocateBytes([]byte("stale"))
	if err != nil {
		t.Fatalf("allocate the completed-string buffer: %v", err)
	}
	size, err := client.allocateWords([]uint32{8})
	if err != nil {
		t.Fatalf("allocate the buffer size: %v", err)
	}

	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range []uint32{'5', 1, buffer, size} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatalf("set r%d: %v", index, err)
		}
	}
	if err := client.handleWIPICSVC(context.Background(), thread, slotIMHandleInput); err != nil {
		t.Fatalf("handle input: %v", err)
	}

	text, err := client.readCString(buffer)
	if err != nil {
		t.Fatalf("the completed string is not readable: %v", err)
	}
	if text != "" {
		t.Fatalf("the completed string is %q, want it emptied", text)
	}
	capacity, err := client.readWord(size)
	if err != nil {
		t.Fatalf("the buffer size is not readable: %v", err)
	}
	if capacity != 8 {
		t.Fatalf("the caller's capacity became %d, want it left at 8", capacity)
	}
}

// The composing buffer is the fifth argument and arrives on the stack, which
// is where a wrongly recovered pointer would write into the game's own stack
// instead. The shape is one real caller's: two eight-byte buffers built on its
// own stack, their addresses stored at [sp] and [sp+4], and MH_IMA_FLUSH as
// the key — a text widget resetting the automaton before it starts.
func TestHandleInputEmptiesTheComposingBufferFromTheStack(t *testing.T) {
	client := fixtureClient(t)

	completed, err := client.allocateBytes([]byte("stale\x00"))
	if err != nil {
		t.Fatal(err)
	}
	composing, err := client.allocateBytes([]byte("half\x00"))
	if err != nil {
		t.Fatal(err)
	}
	sizes, err := client.allocateWords([]uint32{8, 8})
	if err != nil {
		t.Fatal(err)
	}
	// The caller's frame: the stacked pair sits at [sp] and [sp+4].
	frame, err := client.allocateWords([]uint32{composing, sizes + 4})
	if err != nil {
		t.Fatal(err)
	}

	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range []uint32{imaFlushKey, 0x1f6, completed, sizes} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(armcore.RegisterSP, frame); err != nil {
		t.Fatal(err)
	}
	if err := client.handleWIPICSVC(context.Background(), thread, slotIMHandleInput); err != nil {
		t.Fatalf("handle input: %v", err)
	}

	for name, buffer := range map[string]uint32{"completed": completed, "composing": composing} {
		text, err := client.readCString(buffer)
		if err != nil {
			t.Fatalf("the %s string is not readable: %v", name, err)
		}
		if text != "" {
			t.Errorf("the %s string is %q, want it emptied", name, text)
		}
	}
	// Both sizes are the caller's capacities and the specification marks them
	// in-only, so neither may be rewritten.
	for index, address := range []uint32{sizes, sizes + 4} {
		capacity, err := client.readWord(address)
		if err != nil {
			t.Fatal(err)
		}
		if capacity != 8 {
			t.Errorf("capacity %d became %d, want it left at 8", index, capacity)
		}
	}
	result, err := thread.Register(0)
	if err != nil {
		t.Fatal(err)
	}
	if result != 0 {
		t.Errorf("the automaton answered %d, want 0 for a key it did not handle", result)
	}
}

// MC_fsFileAttribute fills three words and the size is the third. The callers
// wrap it twice, once returning the word at offset 8 and once the word at
// offset 4, and allocate their read buffer from the first of those.
func TestFileAttributeReportsTheSizeAsTheThirdWord(t *testing.T) {
	client := fixtureClient(t)

	name, err := client.allocateBytes(append([]byte("data/hello.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.allocate(12)
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotFsFileAttribute, name, info, 1); int32(result) != wipiSuccess {
		t.Fatalf("a packaged file answered %#x", result)
	}
	size, err := client.readWord(info + 8)
	if err != nil {
		t.Fatal(err)
	}
	if size != uint32(len("packaged")) {
		t.Fatalf("size = %d, want %d", size, len("packaged"))
	}

	missing, err := client.allocateBytes(append([]byte("data/absent.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotFsFileAttribute, missing, info, 1); int32(result) != wipiNoEntry {
		t.Fatalf("a missing file answered %#x, want %d", result, wipiNoEntry)
	}
}

// A removed file is gone, including one whose bytes are packaged in the
// archive. This is the shape of a title's new-game flow: it deletes the save
// slot and then asks whether a save is there, and reads the answer as whether
// this is a fresh game. Answering that a deleted file still exists costs the
// whole opening a new game begins with.
func TestRemovedFilesStopExisting(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()

	packaged, err := client.allocateBytes(append([]byte("data/hello.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if result := callSlot(t, client, slotFsIsExist, packaged); int32(result) != wipiSuccess {
		t.Fatalf("a packaged file answered %d before removal, want %d", int32(result), wipiSuccess)
	}
	if result := callSlot(t, client, slotFsRemove, packaged); int32(result) != wipiSuccess {
		t.Fatalf("removal answered %d", int32(result))
	}
	if result := callSlot(t, client, slotFsIsExist, packaged); int32(result) != wipiNoEntry {
		t.Fatalf("a removed file answered %d, want %d", int32(result), wipiNoEntry)
	}
	if result := callSlot(t, client, slotFsOpen, packaged, fileOpenReadOnly); int32(result) != wipiNoEntry {
		t.Fatalf("opening a removed file answered %d, want %d", int32(result), wipiNoEntry)
	}

	// Writing the path brings it back, and reading it answers what was
	// written rather than the packaged bytes underneath.
	handle := callSlot(t, client, slotFsOpen, packaged, fileOpenWriteTruncate)
	if int32(handle) < 0 {
		t.Fatalf("opening a removed file for writing answered %d", int32(handle))
	}
	payload, err := client.allocateBytes([]byte("fresh"))
	if err != nil {
		t.Fatal(err)
	}
	if written := callSlot(t, client, slotFsWrite, handle, payload, 5); int32(written) != 5 {
		t.Fatalf("write answered %d, want 5", int32(written))
	}
	if result := callSlot(t, client, slotFsClose, handle); int32(result) != wipiSuccess {
		t.Fatalf("close answered %d", int32(result))
	}
	if result := callSlot(t, client, slotFsIsExist, packaged); int32(result) != wipiSuccess {
		t.Fatalf("a rewritten file answered %d, want %d", int32(result), wipiSuccess)
	}
	if data, ok := client.readFile("data/hello.txt"); !ok || string(data) != "fresh" {
		t.Fatalf("the rewritten file reads %q (%v), want %q", data, ok, "fresh")
	}
}

// The removal survives the session, because the store it is recorded in is the
// same one the saves are in: a title that deletes its save, exits and comes
// back must not find the save again.
func TestRemovalSurvivesAReload(t *testing.T) {
	store := newMemorySaveStore()

	first := fixtureClient(t)
	first.saveStore = store
	first.removeFile("data/hello.txt")

	second := fixtureClient(t)
	second.saveStore = store
	if _, ok := second.readFile("data/hello.txt"); ok {
		t.Fatal("a file removed in an earlier session is back")
	}
}

// memorySaveStore is a SaveStore with no directory behind it, for the tests
// that only care what the boundary was told.
type memorySaveStore struct{ entries map[string][]byte }

func newMemorySaveStore() *memorySaveStore {
	return &memorySaveStore{entries: make(map[string][]byte)}
}

func (store *memorySaveStore) LoadSave(name string) ([]byte, bool) {
	data, ok := store.entries[name]
	return data, ok
}

func (store *memorySaveStore) StoreSave(name string, data []byte) error {
	store.entries[name] = append([]byte(nil), data...)
	return nil
}

// MC_knlDefTimer registers the callback and MC_knlSetTimer arms it. The arming
// call's third register is the parameter, not the callback: its second and
// third carry one 64-bit timeout without the even-register alignment a modern
// ABI would give it. Reading the callback from there runs the parameter as
// code, which faults at whatever the parameter happens to point at.
func TestTimerCallbackComesFromDefineNotFromArm(t *testing.T) {
	client := fixtureClient(t)

	structure, err := client.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	const callback = 0x4771
	const parameter = 0x15000a8
	callSlot(t, client, slotDefTimer, structure, callback)
	callSlot(t, client, slotSetTimer, structure, 0x2f, 0, parameter)

	entry := client.timers[structure]
	if entry == nil {
		t.Fatalf("no timer was registered against %#x", structure)
	}
	if entry.callback != callback {
		t.Fatalf("callback = %#x, want the address the define call carried (%#x)", entry.callback, callback)
	}
	if entry.param != parameter {
		t.Fatalf("parameter = %#x, want %#x", entry.param, parameter)
	}
	if !entry.armed {
		t.Fatal("the timer was not armed")
	}
	// The timeout is 64-bit across two registers, so a high word counts.
	callSlot(t, client, slotSetTimer, structure, 0, 1, parameter)
	if entry.dueAt <= time.Duration(0xffffffff)*time.Millisecond {
		t.Fatalf("dueAt = %v, want the high word to have counted", entry.dueAt)
	}

	callSlot(t, client, slotUnsetTimer, structure)
	if entry.armed {
		t.Fatal("the timer stayed armed after being unset")
	}
}

// MC_knlCurrentTime is declared `M_Int64 MC_knlCurrentTime()` and its unit is
// milliseconds since 1970, so both halves of the answer have to be written. A
// title's loading screen holds a start time, subtracts the two as 64-bit
// values and spins until the difference passes a deadline: leaving the high
// word alone leaves whatever the register happened to hold in the difference,
// and the wait never ends. Answering a count since the run started instead of
// an epoch is the same trap one step further out — the difference is right
// while the value is not, so a title that formats the time or compares it to a
// stored one is quietly wrong.
func TestCurrentTimeAnswersSixtyFourBitsOfEpochMilliseconds(t *testing.T) {
	client := fixtureClient(t)
	client.clock.advance(2 * time.Second)

	for _, slot := range []uint32{slotCurrentTime} {
		thread := armcore.NewThread(armcore.NewContext())
		// A caller's scratch registers hold whatever the last call left, which
		// is what a stale high word would be read out of.
		if err := thread.SetRegister(1, 0xdeadbeef); err != nil {
			t.Fatal(err)
		}
		if err := client.handleWIPICSVC(context.Background(), thread, slot); err != nil {
			t.Fatalf("slot %#x: %v", slot, err)
		}
		low, err := thread.Register(0)
		if err != nil {
			t.Fatal(err)
		}
		high, err := thread.Register(1)
		if err != nil {
			t.Fatal(err)
		}
		answered := int64(uint64(high)<<32 | uint64(low))
		want := client.clock.unixMillis()
		if answered != want {
			t.Fatalf("slot %#x answered %d, want the epoch milliseconds %d", slot, answered, want)
		}
		if high == 0 {
			t.Fatalf("slot %#x answered a high word of zero, which no epoch time has", slot)
		}
	}
}

// The clock is answered inside the quantum, and the fast answer has to be the
// slow one. Two things separate them and both are behaviour a title reads: the
// fast path writes the context it is handed rather than the thread, and it
// stands down while a trace is recording, because a run recording its platform
// calls has to record the call that dominates it.
func TestTheFastClockAnswersWhatTheSlotAnswersAndStandsDownForATrace(t *testing.T) {
	client := fixtureClient(t)
	client.clock.advance(2 * time.Second)

	guest := armcore.NewContext()
	guest.Registers[12] = slotCurrentTime
	guest.Registers[1] = 0xdeadbeef
	if !client.fastSupervisorCall(&guest, svcCategoryWIPIC) {
		t.Fatal("the clock slot was not answered inside the quantum")
	}
	answered := int64(uint64(guest.Registers[1])<<32 | uint64(guest.Registers[0]))
	want := client.clock.unixMillis()
	if answered != want {
		t.Fatalf("the fast clock answered %d, want the epoch milliseconds %d", answered, want)
	}

	// Every other slot, and every other table, is the ordinary handler's.
	guest.Registers[12] = slotFreeMemory
	if client.fastSupervisorCall(&guest, svcCategoryWIPIC) {
		t.Fatal("a slot with no fast answer was answered inside the quantum")
	}
	guest.Registers[12] = slotCurrentTime
	if client.fastSupervisorCall(&guest, svcCategoryStdlib) {
		t.Fatal("a slot number from another table was read as the clock")
	}

	client.trace = newSVCTrace(4)
	if client.fastSupervisorCall(&guest, svcCategoryWIPIC) {
		t.Fatal("the fast path answered while a trace was recording, which loses the call")
	}
}

// A rename moves the bytes and leaves nothing behind at the old name. The two
// refusals the specification names are what separate it from a copy, so both
// are exercised: renaming a file that is not there, and renaming onto a name
// that is.
func TestRenameMovesAFileAndRefusesTheTwoDocumentedCases(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()

	source, err := client.allocateBytes(append([]byte("data/hello.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	target, err := client.allocateBytes(append([]byte("data/moved.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	missing, err := client.allocateBytes(append([]byte("data/absent.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}

	if result := callSlot(t, client, slotFsRename, missing, target, 1); int32(result) != wipiNoEntry {
		t.Fatalf("renaming an absent file answered %d, want %d", int32(result), wipiNoEntry)
	}
	if result := callSlot(t, client, slotFsRename, source, target, 1); int32(result) != wipiSuccess {
		t.Fatalf("rename answered %d", int32(result))
	}
	if data, ok := client.readFile("data/moved.txt"); !ok || string(data) != "packaged" {
		t.Fatalf("the renamed file reads %q (%v), want %q", data, ok, "packaged")
	}
	if result := callSlot(t, client, slotFsIsExist, source); int32(result) != wipiNoEntry {
		t.Fatalf("the old name answered %d after a rename, want %d", int32(result), wipiNoEntry)
	}

	// The destination is occupied now, and a second rename onto it has to
	// leave both names as they are.
	if result := callSlot(t, client, slotFsRename, target, target, 1); int32(result) != wipiSuccess {
		t.Fatalf("renaming a file onto itself answered %d", int32(result))
	}
	writeBack := callSlot(t, client, slotFsOpen, source, fileOpenWriteTruncate)
	if int32(writeBack) < 0 {
		t.Fatalf("reopening the old name for writing answered %d", int32(writeBack))
	}
	payload, err := client.allocateBytes([]byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if written := callSlot(t, client, slotFsWrite, writeBack, payload, 6); int32(written) != 6 {
		t.Fatalf("write answered %d, want 6", int32(written))
	}
	if result := callSlot(t, client, slotFsClose, writeBack); int32(result) != wipiSuccess {
		t.Fatalf("close answered %d", int32(result))
	}
	if result := callSlot(t, client, slotFsRename, source, target, 1); int32(result) != wipiExists {
		t.Fatalf("renaming onto an existing name answered %d, want %d", int32(result), wipiExists)
	}
	if data, ok := client.readFile("data/moved.txt"); !ok || string(data) != "packaged" {
		t.Fatalf("the refused rename changed the destination to %q (%v)", data, ok)
	}
	if data, ok := client.readFile("data/hello.txt"); !ok || string(data) != "second" {
		t.Fatalf("the refused rename changed the source to %q (%v)", data, ok)
	}
}
