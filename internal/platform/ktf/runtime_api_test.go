package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// newTestRuntime prepares the platform runtime over the synthetic client the
// initialization tests use, which is enough for the runtime Java natives:
// they need guest memory, the JVM, and the platform arena, not a real game.
func newTestRuntime(t *testing.T) (*Client, *initializationRuntime) {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	client.runtime = runtime
	return client, runtime
}

func newIntArray(t *testing.T, client *Client, length int32) *jvm.Object {
	t.Helper()
	array, err := client.JVM().NewArray(jvm.Type{Kind: jvm.TypeInt}, length)
	if err != nil {
		t.Fatal(err)
	}
	return array
}

func TestEventQueueAnswersQueuedEventsThenRedraw(t *testing.T) {
	client, runtime := newTestRuntime(t)
	queue := runtime.runtimeEventQueueObject()
	array := newIntArray(t, client, eventQueueSlots)

	runtime.postGuestEvent(guestEvent{kind: eventKindKey, param1: KeyPressed, param2: KeyFire})
	if _, err := runtimeEventQueueGetNextEvent(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(queue), jvm.ReferenceValue(array)}); err != nil {
		t.Fatalf("getNextEvent() error = %v", err)
	}
	event, err := loadGuestEvent(array)
	if err != nil {
		t.Fatal(err)
	}
	if event != (guestEvent{kind: eventKindKey, param1: KeyPressed, param2: KeyFire}) {
		t.Fatalf("queued event = %+v", event)
	}
	if !runtime.guestEventLoop {
		t.Fatal("getNextEvent did not record that the guest drives the event loop")
	}

	// An empty queue answers the platform's per-frame redraw request, which is
	// what keeps a guest loop drawing.
	if _, err := runtimeEventQueueGetNextEvent(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(queue), jvm.ReferenceValue(array)}); err != nil {
		t.Fatalf("getNextEvent() error = %v", err)
	}
	event, err = loadGuestEvent(array)
	if err != nil {
		t.Fatal(err)
	}
	if event.kind != eventKindRepaint {
		t.Fatalf("idle event = %+v, want a redraw request", event)
	}
}

func TestEventQueueDispatchesKeysToCardStack(t *testing.T) {
	client, runtime := newTestRuntime(t)
	var seen []int32
	if err := client.JVM().RegisterNative("test/Card", "keyNotify", "(II)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		for _, argument := range arguments[1:] {
			value, err := argument.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			seen = append(seen, value)
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	card := &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)}
	runtime.displayCards = append(runtime.displayCards, card)

	array := newIntArray(t, client, eventQueueSlots)
	if err := storeGuestEvent(array, guestEvent{kind: eventKindKey, param1: KeyReleased, param2: KeyLeft}); err != nil {
		t.Fatal(err)
	}
	queue := runtime.runtimeEventQueueObject()
	if _, err := runtimeEventQueueDispatchEvent(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(queue), jvm.ReferenceValue(array)}); err != nil {
		t.Fatalf("dispatchEvent() error = %v", err)
	}
	if len(seen) != 2 || seen[0] != KeyReleased || seen[1] != KeyLeft {
		t.Fatalf("card received %v, want [%d %d]", seen, KeyReleased, KeyLeft)
	}
}

func TestSendKeyQueuesOnceTheGuestDrivesTheEventLoop(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.guestEventLoop = true
	if err := client.SendKey(t.Context(), KeyPressed, KeyNum0); err != nil {
		t.Fatalf("SendKey() error = %v", err)
	}
	if len(runtime.events) != 1 {
		t.Fatalf("queued %d events, want 1", len(runtime.events))
	}
	if got := runtime.events[0]; got != (guestEvent{kind: eventKindKey, param1: KeyPressed, param2: KeyNum0}) {
		t.Fatalf("queued event = %+v", got)
	}
}

func TestEventQueueRejectsUnknownEventKind(t *testing.T) {
	client, runtime := newTestRuntime(t)
	array := newIntArray(t, client, eventQueueSlots)
	if err := storeGuestEvent(array, guestEvent{kind: 7}); err != nil {
		t.Fatal(err)
	}
	queue := runtime.runtimeEventQueueObject()
	_, err := runtimeEventQueueDispatchEvent(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(queue), jvm.ReferenceValue(array)})
	if !client.JVM().IsGuestException(err, "java/lang/IllegalArgumentException") {
		t.Fatalf("dispatchEvent(unknown) error = %v, want IllegalArgumentException", err)
	}
}

func TestDataBaseSortRecordAppliesGuestFilterAndComparator(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &runtimeDataBaseStore{name: "scores", records: [][]byte{{3}, {9}, nil, {1}, {7}}}
	runtime.databases = map[string]*runtimeDataBaseStore{"scores": store}
	database := &jvm.Object{ClassName: runtimeDataBaseClass, Native: store, Fields: make(map[string]jvm.Value)}

	// The filter drops records above five and the comparator orders by value,
	// both through guest code the runtime calls back into.
	if err := client.JVM().RegisterNative("test/Filter", "filter", "([B)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		record, err := recordArgument(arguments[1])
		if err != nil {
			return jvm.VoidValue(), err
		}
		if record[0] <= 5 {
			return jvm.IntValue(1), nil
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.JVM().RegisterNative("test/Comparator", "compare", "([B[B)I", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		left, err := recordArgument(arguments[1])
		if err != nil {
			return jvm.VoidValue(), err
		}
		right, err := recordArgument(arguments[2])
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(int32(left[0]) - int32(right[0])), nil
	}); err != nil {
		t.Fatal(err)
	}
	filter := &jvm.Object{ClassName: "test/Filter", Fields: make(map[string]jvm.Value)}
	comparator := &jvm.Object{ClassName: "test/Comparator", Fields: make(map[string]jvm.Value)}

	result, err := runtimeDataBaseSortRecord(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(database), jvm.ReferenceValue(filter), jvm.ReferenceValue(comparator),
	})
	if err != nil {
		t.Fatalf("sortRecord() error = %v", err)
	}
	identifiers, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	_, values, err := jvm.ArraySnapshot(identifiers)
	if err != nil {
		t.Fatal(err)
	}
	// Records 3 (value 1) and 0 (value 3) pass the filter, in that order.
	if len(values) != 2 {
		t.Fatalf("sorted %d identifiers, want 2", len(values))
	}
	first, _ := values[0].Int32()
	second, _ := values[1].Int32()
	if first != 3 || second != 0 {
		t.Fatalf("sorted identifiers = [%d %d], want [3 0]", first, second)
	}
}

func recordArgument(value jvm.Value) ([]byte, error) {
	object, err := value.Reference()
	if err != nil {
		return nil, err
	}
	return jvm.ByteArraySnapshot(object)
}

func TestGraphicsTranslateMovesDrawingAndClip(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)

	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0xffffff)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsTranslate(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(10), jvm.IntValue(20)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(2), jvm.IntValue(2)}); err != nil {
		t.Fatal(err)
	}
	if pixel := readScreenPixel(t, runtime, state, 10, 20); pixel == 0 {
		t.Fatal("translated fill did not reach (10, 20)")
	}
	if pixel := readScreenPixel(t, runtime, state, 0, 0); pixel != 0 {
		t.Fatal("translated fill also drew at the untranslated origin")
	}

	// A clip set after a translation is expressed in the same translated
	// space, and reading it back reports the caller's coordinates again.
	if _, err := runtimeGraphicsSetClip(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	clipX, err := runtimeGraphicsClipValue(0)(runtime, client.JVM(), []jvm.Value{receiver})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := clipX.Int32(); value != 0 {
		t.Fatalf("getClipX() = %d, want 0", value)
	}
	if _, err := runtimeGraphicsFillRect(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(3), jvm.IntValue(3), jvm.IntValue(4), jvm.IntValue(4)}); err != nil {
		t.Fatal(err)
	}
	if pixel := readScreenPixel(t, runtime, state, 13, 23); pixel != 0 {
		t.Fatal("fill outside the translated clip still drew")
	}
}

func readScreenPixel(t *testing.T, runtime *initializationRuntime, state *runtimeGraphicsState, x, y uint32) uint16 {
	t.Helper()
	var data [2]byte
	if err := runtime.client.core.Memory().Read(state.target.pixels+y*state.target.bpl+x*2, data[:]); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint16(data[:])
}

func TestFileSystemListsRenamesAndEncodesNames(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.AttachFilesystem(map[string][]byte{"save/one.dat": []byte("one"), "other.dat": []byte("other")})
	runtime.guestFiles = map[string][]byte{"save/two.dat": []byte("two")}

	listed, err := runtimeFileSystemList(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(client.JVM().NewString("save"))})
	if err != nil {
		t.Fatalf("list() error = %v", err)
	}
	vector, err := listed.Reference()
	if err != nil {
		t.Fatal(err)
	}
	size, err := client.JVM().InvokeVirtual(vector, "size", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := size.Int32(); count != 2 {
		t.Fatalf("list(save) reported %d entries, want 2", count)
	}

	// A deleted file leaves the listing, or a title that lists its save
	// directory to see what it has still sees what it just threw away.
	if _, err := runtimeFileSystemUnlink(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(client.JVM().NewString("save/one.dat"))}); err != nil {
		t.Fatalf("unlink() error = %v", err)
	}
	if count := listedCount(t, client, runtime, "save"); count != 1 {
		t.Fatalf("list(save) reported %d entries after a delete, want 1", count)
	}

	if _, err := runtimeFileSystemRename(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(client.JVM().NewString("save/two.dat")),
		jvm.ReferenceValue(client.JVM().NewString("save/three.dat")),
	}); err != nil {
		t.Fatalf("rename() error = %v", err)
	}
	if _, exists := runtime.guestFile("save/two.dat"); exists {
		t.Fatal("renamed file is still readable under its old name")
	}
	data, exists := runtime.guestFile("save/three.dat")
	if !exists || string(data) != "two" {
		t.Fatalf("renamed file = %q/%t", data, exists)
	}

	encoded, err := runtimeFileSystemCString(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(client.JVM().NewString("가.dat"))})
	if err != nil {
		t.Fatalf("toCString() error = %v", err)
	}
	array, err := encoded.Reference()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := jvm.ByteArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes) == 0 || bytes[len(bytes)-1] != 0 || decodeEUCKR(bytes[:len(bytes)-1]) != "가.dat" {
		t.Fatalf("toCString() = %v", bytes)
	}
}

func TestJletAnswersDescriptorProperties(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.AttachAppProperties(map[string]string{"AID": "0102DD43", "APPNAME": "테스트"})
	jlet := &jvm.Object{ClassName: runtimeJletClass, Fields: make(map[string]jvm.Value)}

	value, err := runtimeJletGetAppProperty(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(jlet), jvm.ReferenceValue(client.JVM().NewString("appname"))})
	if err != nil {
		t.Fatalf("getAppProperty() error = %v", err)
	}
	property, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := jvm.StringText(property); !ok || text != "테스트" {
		t.Fatalf("getAppProperty(appname) = %q/%t", text, ok)
	}

	missing, err := runtimeJletGetAppProperty(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(jlet), jvm.ReferenceValue(client.JVM().NewString("absent"))})
	if err != nil {
		t.Fatal(err)
	}
	if reference, _ := missing.Reference(); reference != nil {
		t.Fatalf("getAppProperty(absent) = %v, want null", reference)
	}
}

func TestTextComponentKeepsItsText(t *testing.T) {
	client, runtime := newTestRuntime(t)
	field := &jvm.Object{ClassName: runtimeTextFieldComponentClass, Fields: make(map[string]jvm.Value)}
	if _, err := runtimeTextComponentConstructorWithText(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(field), jvm.ReferenceValue(client.JVM().NewString("name")), jvm.IntValue(0),
	}); err != nil {
		t.Fatalf("TextFieldComponent constructor error = %v", err)
	}
	value, err := runtimeTextComponentGetString(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(field)})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := value.Reference()
	if got, ok := jvm.StringText(text); !ok || got != "name" {
		t.Fatalf("getString() = %q/%t", got, ok)
	}

	empty := &jvm.Object{ClassName: runtimeTextComponentClass, Fields: make(map[string]jvm.Value)}
	value, err = runtimeTextComponentGetString(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(empty)})
	if err != nil {
		t.Fatal(err)
	}
	text, _ = value.Reference()
	if got, ok := jvm.StringText(text); !ok || got != "" {
		t.Fatalf("getString() without text = %q/%t", got, ok)
	}
}

// TestGraphicsCharacterRangesMatchStringDrawing pins the argument order of the
// range-taking text calls: a wrong order still draws something, so only
// comparing against the equivalent drawString catches it.
func TestGraphicsCharacterRangesMatchStringDrawing(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	receiver := jvm.ReferenceValue(graphics)
	state := graphics.Native.(*runtimeGraphicsState)
	if _, err := runtimeGraphicsSetColor(runtime, client.JVM(), []jvm.Value{receiver, jvm.IntValue(0xffffff)}); err != nil {
		t.Fatal(err)
	}

	characters, err := client.JVM().NewArray(jvm.Type{Kind: jvm.TypeChar}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := jvm.SetArrayRange(characters, 0, []jvm.Value{jvm.IntValue('A'), jvm.IntValue('B')}); err != nil {
		t.Fatal(err)
	}

	reference := drawnRegion(t, runtime, state, func() error {
		_, err := runtimeGraphicsDrawString(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.ReferenceValue(client.JVM().NewString("B")), jvm.IntValue(40), jvm.IntValue(40), jvm.IntValue(0),
		})
		return err
	})
	chars := drawnRegion(t, runtime, state, func() error {
		_, err := runtimeGraphicsDrawChars(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.ReferenceValue(characters), jvm.IntValue(1), jvm.IntValue(1),
			jvm.IntValue(40), jvm.IntValue(40), jvm.IntValue(0),
		})
		return err
	})
	substring := drawnRegion(t, runtime, state, func() error {
		_, err := runtimeGraphicsDrawSubstring(runtime, client.JVM(), []jvm.Value{
			receiver, jvm.ReferenceValue(client.JVM().NewString("AB")), jvm.IntValue(1), jvm.IntValue(1),
			jvm.IntValue(40), jvm.IntValue(40), jvm.IntValue(0),
		})
		return err
	})

	lit := 0
	for _, pixel := range reference {
		if pixel != 0 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("drawString drew nothing to compare against")
	}
	for index := range reference {
		if chars[index] != reference[index] {
			t.Fatalf("drawChars differs from drawString at pixel %d", index)
		}
		if substring[index] != reference[index] {
			t.Fatalf("drawSubstring differs from drawString at pixel %d", index)
		}
	}
}

// drawnRegion clears a patch of the screen, runs one drawing call, and returns
// the pixels it produced.
func drawnRegion(t *testing.T, runtime *initializationRuntime, state *runtimeGraphicsState, draw func() error) []uint16 {
	t.Helper()
	if err := runtime.graphicsFillRect(state, 30, 30, 40, 40, 0); err != nil {
		t.Fatal(err)
	}
	if err := draw(); err != nil {
		t.Fatal(err)
	}
	pixels := make([]uint16, 0, 40*40)
	for y := uint32(30); y < 70; y++ {
		for x := uint32(30); x < 70; x++ {
			pixels = append(pixels, readScreenPixel(t, runtime, state, x, y))
		}
	}
	return pixels
}

// listedCount is how many names FileSystem.list answers under a prefix.
func listedCount(t *testing.T, client *Client, runtime *initializationRuntime, prefix string) int32 {
	t.Helper()
	listed, err := runtimeFileSystemList(runtime, client.JVM(),
		[]jvm.Value{jvm.ReferenceValue(client.JVM().NewString(prefix))})
	if err != nil {
		t.Fatalf("list() error = %v", err)
	}
	vector, err := listed.Reference()
	if err != nil {
		t.Fatal(err)
	}
	size, err := client.JVM().InvokeVirtual(vector, "size", "()I")
	if err != nil {
		t.Fatal(err)
	}
	count, _ := size.Int32()
	return count
}

// A pointer takes the same two roads a key does. Ten of the local titles carry
// a pointerNotify body of their own, and a title that drives its own event
// loop has to be handed the event rather than have its card called behind the
// loop's back.
func TestSendPointerTakesTheSameTwoRoadsAKeyDoes(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.guestEventLoop = true
	if err := client.SendPointer(t.Context(), PointerDragged, 17, 39); err != nil {
		t.Fatalf("SendPointer() error = %v", err)
	}
	want := guestEvent{kind: eventKindPointer, param1: PointerDragged, param2: 17, param3: 39}
	if len(runtime.events) != 1 || runtime.events[0] != want {
		t.Fatalf("queued %+v, want one %+v", runtime.events, want)
	}

	var seen []int32
	if err := client.JVM().RegisterNative("test/Card", "pointerNotify", "(III)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		for _, argument := range arguments[1:] {
			value, err := argument.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			seen = append(seen, value)
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)})
	array := newIntArray(t, client, eventQueueSlots)
	if err := storeGuestEvent(array, want); err != nil {
		t.Fatal(err)
	}
	queue := runtime.runtimeEventQueueObject()
	if _, err := runtimeEventQueueDispatchEvent(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(queue), jvm.ReferenceValue(array)}); err != nil {
		t.Fatalf("dispatchEvent() error = %v", err)
	}
	if len(seen) != 3 || seen[0] != PointerDragged || seen[1] != 17 || seen[2] != 39 {
		t.Fatalf("card received %v, want [%d 17 39]", seen, PointerDragged)
	}
}

// The spec is explicit that openDataBase throws DataBaseException when create
// is false and the database is not there, and that throw is how a title finds
// out it is running for the first time. Answering with an empty database
// instead told one title its save existed: it read record zero, caught the
// record exception that came back, and dereferenced the array the read never
// produced.
func TestOpenDataBaseThrowsWhenItIsNotThereAndWasNotAskedToCreateIt(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.saveStore = NewDirectorySaveStore(t.TempDir())
	name := jvm.ReferenceValue(client.JVM().NewString("scores"))
	open := func(create int32) (jvm.Value, error) {
		return runtimeOpenDataBase(runtime, client.JVM(), []jvm.Value{name, jvm.IntValue(64), jvm.IntValue(create)})
	}

	if _, err := open(0); !client.JVM().IsGuestException(err, runtimeDataBaseExceptionClass) {
		t.Fatalf("openDataBase(create=false) on nothing = %v, want DataBaseException", err)
	}

	// Creating it makes it exist from that moment, with no record in it yet,
	// so the next open finds it rather than throwing again — including in a
	// later session, which reaches it through the save store.
	if _, err := open(1); err != nil {
		t.Fatalf("openDataBase(create=true) error = %v", err)
	}
	if _, err := open(0); err != nil {
		t.Fatalf("openDataBase(create=false) after creating it = %v", err)
	}
	if _, ok := client.saveStore.LoadSave("jdb/scores"); !ok {
		t.Fatal("a created database left nothing behind for the next session")
	}
	runtime.databases = nil
	if _, err := open(0); err != nil {
		t.Fatalf("openDataBase(create=false) on a stored database = %v", err)
	}
}
