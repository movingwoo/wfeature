package ktf

import (
	"encoding/binary"
	"fmt"
	"slices"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Guest-visible instance fields are the last place a bound object still has two
// storages. An array solved this by making guest memory the only one
// (guest_array.go), and a static field solved it by living inside its field
// record; an instance field of a runtime-owned class cannot, because the Go
// side of it is a Go value rather than a word — a string's characters are a Go
// string, and no offset in guest memory holds them.
//
// So the two are made to agree at the boundary the guest crosses, which is the
// same place a runtime call already reads its arguments. On the way out
// whatever a runtime method changed is published into the payload words; on the
// way in a payload that no longer matches the Go value is reported, because
// only the guest can have written it.
//
// Only a class that publishes instance fields to the guest has anything to
// synchronize. Everything else in the runtime Java table keeps its state on the
// Go side and hands it over through method calls, so it has no second copy to
// disagree with. The table below is the whole mechanism: a class that gains a
// published instance field gains an entry, and
// fieldSyncTableCoversPublishedFields fails the build if it does not.
type fieldSync struct {
	// mutators names the methods after which the Go value may no longer be what
	// the payload holds. Publishing after every call instead would put two
	// guest reads on paths as hot as String.charAt, which is the most-called
	// runtime method a local title has; a constructor is the only place a
	// CLDC string's characters are ever decided.
	mutators []string
	// publish writes the Go value into the guest payload when they disagree,
	// and reports whether it wrote.
	publish func(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error)
	// reserved is the number of payload bytes this class's own field records
	// describe. A guest subclass whose own fields start inside that range is
	// compiled against a runtime where this class published nothing, so its
	// payload is not this class's to write. Zero means every instance is.
	reserved uint32
	// adopt reports whether the guest payload has stopped describing the Go
	// value. It never writes: no local title has been seen to write one of
	// these words, and a repair built on no evidence would be a guess about
	// which side is right. It runs in debug builds, where finding the first
	// title that does is worth two guest reads per crossing.
	adopt func(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error)
}

var fieldSyncs = map[string]fieldSync{
	"java/lang/String": {
		mutators: []string{"<init>"},
		publish:  publishGuestString,
		adopt:    adoptGuestString,
	},
	// A Card publishes the four geometry words and the transparency flag the
	// specification declares protected, because a title reads `w` off its own
	// canvas rather than calling getWidth. Which constructor ran decides them
	// and move and resize change them, so those three are the mutators.
	runtimeCardClass: {
		mutators: []string{"<init>", "move", "resize"},
		publish:  publishGuestCardBounds,
		adopt:    adoptGuestCardBounds,
		reserved: cardFieldsSize,
	},
	// A byte sink publishes the one word that says where its bytes are,
	// because a title reads buf instead of calling toByteArray when it is
	// about to hand the bytes on rather than keep them. The mutators are the
	// three calls that can put a different array behind the field: the
	// constructor allocates one, a write replaces it when it outgrows it, and
	// reset starts again. Publishing compares before it writes, so a write
	// that only fills the array it already had costs one guest read.
	jvm.ByteArrayOutputStreamClass: {
		mutators: []string{"<init>", "write", "reset"},
		publish:  publishGuestByteSinkBuffer,
		adopt:    adoptGuestByteSinkBuffer,
		reserved: byteArrayOutputStreamFieldsSize,
	},
}

// byteArrayOutputStreamFieldsSize is how many payload bytes the runtime's own
// ByteArrayOutputStream field records describe: the one buf reference.
const byteArrayOutputStreamFieldsSize = 4

// publishGuestByteSinkBuffer points the payload word at the guest array the Go
// buffer is. The array has to be bound for the guest to have anything to read,
// and binding is what gives it its guest memory in the first place — an array
// the guest has never seen has no address until something asks for one.
func publishGuestByteSinkBuffer(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error) {
	value, err := runtime.client.vm.Field(object, jvm.ByteArrayOutputStreamClass, "buf", "[B")
	if err != nil {
		return false, err
	}
	buffer, err := value.Reference()
	if err != nil {
		return false, err
	}
	var wanted uint32
	if buffer != nil {
		if err := runtime.ensureResultBound(buffer); err != nil {
			return false, fmt.Errorf("bind KTF byte sink buffer: %w", err)
		}
		bound, ok := runtime.client.vm.AOTAddress(buffer)
		if !ok {
			return false, fmt.Errorf("KTF byte sink buffer has no guest address")
		}
		wanted = bound
	}
	current, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "byte sink buffer")
	if err != nil {
		return false, err
	}
	if current[0] == wanted {
		return false, nil
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, wanted)
	if err := runtime.client.core.Memory().Write(address+javaInstanceSize+javaInstanceHeader, payload); err != nil {
		return false, fmt.Errorf("write KTF byte sink buffer at %#x: %w", address, err)
	}
	return true, nil
}

// adoptGuestByteSinkBuffer reports a sink whose payload word has stopped
// naming the Go array. The field is protected rather than private, so a
// subclass could assign it; nothing local has been seen to, and swapping the
// Go buffer for whatever the word names would be a guess about which side is
// right.
func adoptGuestByteSinkBuffer(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error) {
	value, err := runtime.client.vm.Field(object, jvm.ByteArrayOutputStreamClass, "buf", "[B")
	if err != nil {
		return false, err
	}
	buffer, err := value.Reference()
	if err != nil {
		return false, err
	}
	var expected uint32
	if buffer != nil {
		if bound, ok := runtime.client.vm.AOTAddress(buffer); ok {
			expected = bound
		} else {
			return false, nil
		}
	}
	current, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "byte sink buffer")
	if err != nil {
		return false, err
	}
	return current[0] != expected, nil
}

// publishGuestFields brings the guest payload of a bound object back in step
// with the Go value a runtime method has just changed.
func (runtime *initializationRuntime) publishGuestFields(object *jvm.Object, method runtimeJavaMethod) error {
	if object == nil {
		return nil
	}
	// The key is the class whose method just ran rather than the object's own
	// class: a title's canvas is a guest subclass of Card, and it is Card's
	// constructor that decides Card's words on it.
	sync, ok := fieldSyncs[method.class]
	if !ok || sync.publish == nil {
		return nil
	}
	mutated := false
	for _, name := range sync.mutators {
		if name == method.name {
			mutated = true
			break
		}
	}
	if !mutated {
		return nil
	}
	address, bound := runtime.client.vm.AOTAddress(object)
	if !bound {
		// A Go-only object has no payload yet. Whatever it holds is written
		// when it is bound, which is where every published field starts.
		return nil
	}
	if !runtime.guestReservesRuntimeBlock(object, method.class, sync.reserved) {
		return nil
	}
	written, err := sync.publish(runtime, address, object)
	if err != nil {
		return fmt.Errorf("publish KTF %s fields at %#x: %w", object.ClassName, address, err)
	}
	if written {
		runtime.countDiagnostic("field publish " + object.ClassName)
	}
	return nil
}

// adoptGuestFields reports a bound object whose payload no longer describes its
// Go value. Between two runtime calls only the guest runs, so a difference here
// is a guest write to a published field — the case that would make general
// field synchronization something this has to do rather than watch for.
func (runtime *initializationRuntime) adoptGuestFields(object *jvm.Object) {
	if object == nil || !backend.DebugBuild() {
		return
	}
	sync, ok := fieldSyncs[object.ClassName]
	if !ok || sync.adopt == nil {
		return
	}
	address, bound := runtime.client.vm.AOTAddress(object)
	if !bound {
		return
	}
	if !runtime.guestReservesRuntimeBlock(object, object.ClassName, sync.reserved) {
		return
	}
	diverged, err := sync.adopt(runtime, address, object)
	if err != nil {
		runtime.countDiagnostic("field adopt failed " + object.ClassName)
		return
	}
	if diverged {
		runtime.countDiagnostic("field divergence " + object.ClassName)
	}
}

// readGuestStringFields reads the three words a string publishes: the character
// array, the view's first index into it, and its length.
func (runtime *initializationRuntime) readGuestStringFields(address uint32) (value, offset, count uint32, err error) {
	words, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 3, "string fields")
	if err != nil {
		return 0, 0, 0, err
	}
	return words[0], words[1], words[2], nil
}

// readGuestStringUnits reads the UTF-16 units a published string view names. A
// view the array cannot hold is refused rather than clamped, because a length
// this cannot trust is exactly the case the caller must not treat as content.
func (runtime *initializationRuntime) readGuestStringUnits(value, offset, count uint32) ([]uint16, bool, error) {
	if value == 0 {
		return nil, false, nil
	}
	if count > maxJavaStringUnits {
		return nil, false, nil
	}
	lengths, err := runtime.readAOTWords(value+javaInstanceSize+javaInstanceHeader, 1, "string content length")
	if err != nil {
		return nil, false, err
	}
	length := lengths[0]
	if offset > length || count > length-offset {
		return nil, false, nil
	}
	if count == 0 {
		return nil, true, nil
	}
	elements := value + javaInstanceSize + javaInstanceHeader + javaArrayLengthSize + offset*2
	data, err := runtime.readAOTBytes(elements, uint64(count)*2, "string content")
	if err != nil {
		return nil, false, err
	}
	units := make([]uint16, count)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2:])
	}
	return units, true, nil
}

// publishGuestString gives a string's characters a guest character array and
// points the payload words at it. A guest that constructs a string itself gets
// the instance allocated before its constructor runs, so without this the words
// it reads describe the empty string the object was allocated as.
func publishGuestString(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error) {
	text, ok := jvm.StringText(object)
	if !ok {
		return false, nil
	}
	units := utf16.Encode([]rune(text))
	if uint64(len(units)) > uint64(maxJavaStringUnits) {
		return false, fmt.Errorf("KTF string content %d units exceeds %d", len(units), maxJavaStringUnits)
	}
	value, offset, count, err := runtime.readGuestStringFields(address)
	if err != nil {
		return false, err
	}
	if value != 0 && count == uint32(len(units)) {
		current, valid, readErr := runtime.readGuestStringUnits(value, offset, count)
		if readErr != nil {
			return false, readErr
		}
		if valid && slices.Equal(current, units) {
			return false, nil
		}
	}
	arrayAddress, err := runtime.allocateGuestCharArray(units)
	if err != nil {
		return false, fmt.Errorf("allocate KTF string content: %w", err)
	}
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[0:], arrayAddress)
	binary.LittleEndian.PutUint32(payload[4:], 0)
	binary.LittleEndian.PutUint32(payload[8:], uint32(len(units)))
	if err := runtime.client.core.Memory().Write(address+javaInstanceSize+javaInstanceHeader, payload); err != nil {
		return false, fmt.Errorf("write KTF string fields at %#x: %w", address, err)
	}
	return true, nil
}

// adoptGuestString reports a string whose published view no longer spells what
// the Go string holds.
func adoptGuestString(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error) {
	text, ok := jvm.StringText(object)
	if !ok {
		return false, nil
	}
	value, offset, count, err := runtime.readGuestStringFields(address)
	if err != nil {
		return false, err
	}
	if value == 0 {
		// Nothing has been published yet, so there is no guest write to find.
		return false, nil
	}
	units, valid, err := runtime.readGuestStringUnits(value, offset, count)
	if err != nil || !valid {
		return false, err
	}
	return !slices.Equal(units, utf16.Encode([]rune(text))), nil
}

// cardFieldsSize is how many payload bytes the runtime's own Card field records
// describe: the four geometry words and the transparency flag.
const cardFieldsSize = 20

// guestReservesRuntimeBlock reports whether a bound object's payload leaves its
// first `size` bytes to the runtime class `owner`.
//
// It should always be yes, and the reason is worth writing down: a guest class
// record's field offsets are not baked into the image. The client computes them
// when it prepares the class, from the instance size its parent declares — so
// declaring these bytes on Card moved every canvas subclass's own first field
// from zero to twenty in every archive here, without the archives changing.
// That is what makes publishing a field on a runtime class safe at all.
//
// The check stays because the consequence of that assumption being wrong
// somewhere is silent: a publish into a payload whose own fields live there
// overwrites them, and the title that breaks is one that was working. An
// unknown chain answers no, which costs a geometry word the guest reads as
// zero.
func (runtime *initializationRuntime) guestReservesRuntimeBlock(object *jvm.Object, owner string, size uint32) bool {
	if size == 0 {
		return true
	}
	if object == nil {
		return false
	}
	name := object.ClassName
	for depth := 0; depth < aotHierarchyLimit; depth++ {
		if name == owner {
			return true
		}
		class, ok := runtime.client.vm.AOTClass(name)
		if !ok {
			return false
		}
		for _, field := range class.Fields {
			if field.AccessFlags&0x0008 != 0 {
				continue
			}
			if field.Offset < size {
				return false
			}
		}
		if class.SuperName == "" {
			return false
		}
		name = class.SuperName
	}
	return false
}

// cardBoundsFields are the payload words a Card publishes, in the order their
// field records give them offsets.
var cardBoundsFields = []string{"x:I", "y:I", "w:I", "h:I", "transparent:Z"}

// readGuestCardBounds reads the five words a Card publishes.
func (runtime *initializationRuntime) readGuestCardBounds(address uint32) ([]uint32, error) {
	return runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, uint32(len(cardBoundsFields)), "card bounds")
}

// cardBoundsValues answers what the Go side of a Card says its words are.
func cardBoundsValues(object *jvm.Object) []uint32 {
	words := make([]uint32, len(cardBoundsFields))
	for index, name := range cardBoundsFields {
		value, ok := object.Fields[name]
		if !ok {
			continue
		}
		number, err := value.Int32()
		if err != nil {
			continue
		}
		words[index] = uint32(number)
	}
	return words
}

// publishGuestCardBounds writes a Card's geometry into the payload a guest
// reads it from. A canvas is allocated before its constructor chain runs, so
// without this the words a title reads describe an empty card.
func publishGuestCardBounds(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error) {
	current, err := runtime.readGuestCardBounds(address)
	if err != nil {
		return false, err
	}
	wanted := cardBoundsValues(object)
	if slices.Equal(current, wanted) {
		return false, nil
	}
	payload := make([]byte, len(wanted)*4)
	for index, word := range wanted {
		binary.LittleEndian.PutUint32(payload[index*4:], word)
	}
	if err := runtime.client.core.Memory().Write(address+javaInstanceSize+javaInstanceHeader, payload); err != nil {
		return false, fmt.Errorf("write KTF card bounds at %#x: %w", address, err)
	}
	return true, nil
}

// adoptGuestCardBounds reports a card whose payload has stopped describing the
// Go value. The specification declares these fields protected, so a subclass
// may assign them; nothing local has been seen to, and a repair built on no
// evidence would be a guess about which side is right.
func adoptGuestCardBounds(runtime *initializationRuntime, address uint32, object *jvm.Object) (bool, error) {
	current, err := runtime.readGuestCardBounds(address)
	if err != nil {
		return false, err
	}
	return !slices.Equal(current, cardBoundsValues(object)), nil
}
