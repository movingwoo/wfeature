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
// synchronize, and today that is java/lang/String alone. Everything else in the
// runtime Java table keeps its state on the Go side and hands it over through
// method calls, so it has no second copy to disagree with. The table below is
// the whole mechanism: a class that gains a published instance field gains an
// entry, and fieldSyncTableCoversPublishedFields fails the build if it does not.
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
}

// publishGuestFields brings the guest payload of a bound object back in step
// with the Go value a runtime method has just changed.
func (runtime *initializationRuntime) publishGuestFields(object *jvm.Object, method runtimeJavaMethod) error {
	if object == nil {
		return nil
	}
	sync, ok := fieldSyncs[object.ClassName]
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
