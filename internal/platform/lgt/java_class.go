package lgt

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// The application's own class records, as they arrive on the class-list call.
//
// A handle does not point at the start of its record: the header sits
// javaClassHeader bytes before it and the word at handle+8 points back at that
// header, which is what pairs the two. What follows the handle is the class's
// own members, and this is where they end:
//
//	handle + 0x00  zero
//	handle + 0x04  zero
//	handle + 0x08  the header, = handle - javaClassHeader
//	handle + 0x0c  field count n
//	handle + 0x10  n field records, five words each
//	               { owner, char *name, char *descriptor, kind, index }
//	               method count m
//	               m method records, seven words each
//
// The counts are the answer to "where does the list stop": walking to the end
// of the method records lands exactly on the next class's header, in every
// record that has this shape. That exactness is what confirms the reading —
// a wrong stride or a wrong count would leave the walk short or long, and it
// never does.
//
// Not every record has this shape, and the header says which do: a class whose
// header word at javaClassPool is zero carries its members inline as above,
// and one that points somewhere carries them through that pointer instead.
// Only the first is read here. See docs/lgt.md.
const (
	javaClassHeader = 0x4c
	javaClassPool   = 0x0c
	javaStaticWords = 0x48
	javaFieldWords  = 5
	javaMethodWords = 7
	// maxJavaStaticWords bounds the static run a record can claim. The largest
	// among the local Java titles is 448 words.
	maxJavaStaticWords = 1 << 14
	// A record is refused rather than trusted past these. The counts come out
	// of guest memory, and a count read from the wrong shape of record is a
	// large number, not a plausible one.
	maxJavaMembers         = 4096
	maxJavaStringConstants = 1 << 16
	maxJavaStringLength    = 1 << 16
)

// javaMember is one field or method record of an application class. A method
// record is two words longer than a field's, and the sixth is the one that
// matters: **it is the method's own entry point**, so a record says not only
// what a class declares but where the code for it is.
type javaMember struct {
	Name       string
	Descriptor string
	Kind       uint32
	Index      uint32
	Body       uint32
}

// javaClass is one application class record, read as far as it is understood.
type javaClass struct {
	Handle      uint32
	Header      uint32
	AccessFlags uint32
	Name        string
	// Super is the superclass name when the platform owns it, and SuperHandle
	// is another record's handle when the application declares it. Which one a
	// record carries is decided by where the header's super word points, so
	// exactly one of the two is set; SuperName is the application superclass's
	// own name, so that one field names the superclass either way.
	Super       string
	SuperHandle uint32
	SuperName   string
	// InstanceSize is how many words the compiler laid the object out in, and
	// VTableSize how many slots it numbered. Both count the superclass's, which
	// is what makes them a statement about the platform's layout as well as
	// this class's.
	InstanceSize uint16
	VTableSize   uint16
	// StaticWords is how many words of static storage the class keeps in its
	// class object's data block, after the words the class object itself uses.
	// Nothing inherits it: a class's statics are its own.
	StaticWords uint32
	// Body is the three thunks at header+0x2c. The platform calls the first to
	// have the class declare its members; the module calls the other two.
	Body [3]uint32
	// VTable is the dispatch table the compiler built for the class, and zero
	// for a class it could not lay out — those are the platform's to build.
	VTable  uint32
	Fields  []javaMember
	Methods []javaMember
	// End is the address one past the last method record, which is the next
	// class's header when the records are packed.
	End uint32
	// Inline reports whether the record carried its members the way above. A
	// record that did not carries its dispatch table instead; see Interfaces.
	Inline bool
	// Interfaces is what the record says about the interfaces the class
	// implements: a name or another record, and the vtable slot that
	// interface's methods start at.
	Interfaces []javaInterface
}

// javaInterface is one entry of a class record's interface table. Exactly one
// of Name and Handle is set, the same way a superclass is either a platform
// name or another record.
type javaInterface struct {
	Name   string
	Handle uint32
	Slot   uint32
}

// readJavaClass reads one application class record from its handle.
func (client *Client) readJavaClass(handle uint32, handles map[uint32]int) (javaClass, error) {
	if handle < 0x1000 || handle&3 != 0 {
		return javaClass{}, fmt.Errorf("class handle %#x is not a word-aligned address", handle)
	}
	header := handle - javaClassHeader
	back, err := client.readWord(handle + 8)
	if err != nil {
		return javaClass{}, err
	}
	if back != header {
		return javaClass{}, fmt.Errorf("class handle %#x does not point back at its header: %#x", handle, back)
	}
	class := javaClass{Handle: handle, Header: header, End: handle + 0x10}
	if class.AccessFlags, err = client.readWord(header); err != nil {
		return javaClass{}, err
	}
	name, err := client.readWord(header + 8)
	if err != nil {
		return javaClass{}, err
	}
	class.Name, _ = client.readPrintableString(name)
	super, err := client.readWord(header + 0x10)
	if err != nil {
		return javaClass{}, err
	}
	// A superclass is either a name the platform owns or another record's
	// handle, and which one is decided by asking the address whether it is a
	// record — not by whether the class list mentions it. The list holds the
	// classes the module resolves at run time, and a class it only ever extends
	// is absent from it while being present in the image.
	if _, declared := handles[super]; declared || client.isJavaClassHandle(super) {
		class.SuperHandle = super
		if name, err := client.readWord(super - javaClassHeader + 8); err == nil {
			class.SuperName, _ = client.readPrintableString(name)
		}
	} else {
		class.Super, _ = client.readPrintableString(super)
	}
	if class.InstanceSize, err = client.readHalfword(header + 0x18); err != nil {
		return javaClass{}, err
	}
	size, err := client.readWord(header + 0x24)
	if err != nil {
		return javaClass{}, err
	}
	class.VTableSize = uint16(size >> 16)
	statics, err := client.readWord(header + javaStaticWords)
	if err != nil {
		return javaClass{}, err
	}
	if statics > maxJavaStaticWords {
		return javaClass{}, fmt.Errorf("class %s claims %d words of static storage", class.Name, statics)
	}
	class.StaticWords = statics
	for index := range class.Body {
		if class.Body[index], err = client.readWord(header + 0x2c + uint32(index)*4); err != nil {
			return javaClass{}, err
		}
	}

	class.Interfaces, _ = client.readJavaInterfaces(header)

	pool, err := client.readWord(header + javaClassPool)
	if err != nil {
		return javaClass{}, err
	}
	class.VTable = pool
	if pool != 0 {
		return class, nil
	}
	fields, next, err := client.readJavaMembers(handle, handle+0x0c, javaFieldWords)
	if err != nil {
		return class, nil
	}
	methods, end, err := client.readJavaMembers(handle, next, javaMethodWords)
	if err != nil {
		return class, nil
	}
	class.Fields, class.Methods, class.End, class.Inline = fields, methods, end, true
	return class, nil
}

// readJavaInterfaces reads the interface table a class record carries.
//
// **This is how a class with no member records is reached at all.** A record
// whose pool word points somewhere is a class the compiler laid out itself: its
// dispatch table is in the image at that pointer and it declares no members,
// because the platform never had to number anything for it. What the platform
// still has to do is enter such a class through an interface it implements —
// the `run` of a thread's Runnable is the case that found this — and the
// interface table is what says where: a count, that many pointers to entries,
// and each entry a name or another record's handle followed by **the vtable
// slot that interface's methods start at**.
//
// So `java/lang/Runnable` at slot 10 means the class's `run` is vtable slot 10,
// with no member record anywhere. An interface with no methods carries a slot
// of zero, which is not a slot and is never used as one: only an interface
// whose contract pins the order down can name a method this way.
func (client *Client) readJavaInterfaces(header uint32) ([]javaInterface, error) {
	table, err := client.readWord(header + javaClassInterfaces)
	if err != nil || table == 0 {
		return nil, err
	}
	count, err := client.readWord(table)
	if err != nil {
		return nil, err
	}
	if count == 0 || count > maxJavaInterfaces {
		return nil, fmt.Errorf("a class record claims %d interfaces", count)
	}
	interfaces := make([]javaInterface, 0, count)
	for index := uint32(0); index < count; index++ {
		entry, err := client.readWord(table + 4 + index*4)
		if err != nil || entry == 0 {
			return interfaces, err
		}
		named, err := client.readWord(entry)
		if err != nil {
			return interfaces, err
		}
		slot, err := client.readWord(entry + 4)
		if err != nil {
			return interfaces, err
		}
		one := javaInterface{Slot: slot}
		if client.isJavaClassHandle(named) {
			one.Handle = named
			if name, err := client.readWord(named - javaClassHeader + 8); err == nil {
				one.Name, _ = client.readPrintableString(name)
			}
		} else {
			one.Name, _ = client.readPrintableString(named)
		}
		interfaces = append(interfaces, one)
	}
	return interfaces, nil
}

// javaClassInterfaces is where the header points at the interface table, and
// maxJavaInterfaces bounds what a record may claim: a count read off the wrong
// word is a large number rather than a plausible one.
const (
	javaClassInterfaces = 0x14
	maxJavaInterfaces   = 32
)

// javaClassSentinel ends every class record's header, and with the header
// back-pointer it is what makes a record recognisable on its own.
const javaClassSentinel = 0xfffffffe

// isJavaClassHandle reports whether an address is a class record's handle. Two
// words have to agree — the handle names its header, and the header ends with
// the sentinel — which is what keeps a plain name pointer from reading as one.
func (client *Client) isJavaClassHandle(address uint32) bool {
	if address < 0x1000 || address&3 != 0 {
		return false
	}
	back, err := client.readWord(address + 8)
	if err != nil || back != address-javaClassHeader {
		return false
	}
	sentinel, err := client.readWord(back + 0x44)
	return err == nil && sentinel == javaClassSentinel
}

// findJavaClassRecord answers the handle of the class record with a name, by
// walking the module's own sections for records. **The class list is not
// enough**: it holds the classes the module resolves at run time, and the
// launcher class — the one the platform is asked to construct — is exactly the
// one the module never resolves for itself. See docs/lgt.md, "What "Game"
// names, and why it is not in the class list".
func (client *Client) findJavaClassRecord(name string) (uint32, bool) {
	if client.module == nil || name == "" {
		return 0, false
	}
	// Every section is walked, including the executable one. A module marks
	// more sections executable than carry code — the section holding the class
	// records is one of them — so skipping by that flag skips the records
	// themselves, and the two words a record is recognised by are specific
	// enough that scanning code costs a pass and finds nothing.
	for _, section := range client.module.Sections {
		if section.Size < javaClassHeader {
			continue
		}
		for address := section.Address + javaClassHeader; address < section.Address+section.Size; address += 4 {
			if !client.isJavaClassHandle(address) {
				continue
			}
			pointer, err := client.readWord(address - javaClassHeader + 8)
			if err != nil {
				continue
			}
			if text, ok := client.readPrintableString(pointer); ok && text == name {
				return address, true
			}
		}
	}
	return 0, false
}

// readJavaMembers reads a count and that many member records of one width. It
// returns the address one past the last record, so the field list hands the
// method list its starting point.
func (client *Client) readJavaMembers(owner, at uint32, words int) ([]javaMember, uint32, error) {
	count, err := client.readWord(at)
	if err != nil {
		return nil, 0, err
	}
	if count > maxJavaMembers {
		return nil, 0, fmt.Errorf("member count %#x at %#x is not a count", count, at)
	}
	at += 4
	members := make([]javaMember, 0, count)
	for index := uint32(0); index < count; index++ {
		record := at + index*uint32(words)*4
		// The owner word is what makes a member record recognisable on its
		// own. A record that names another owner means the count or the width
		// is wrong, and reading on would be reading the next class.
		declared, err := client.readWord(record)
		if err != nil {
			return nil, 0, err
		}
		if declared != owner {
			return nil, 0, fmt.Errorf("member %d at %#x is owned by %#x, not %#x", index, record, declared, owner)
		}
		member := javaMember{}
		name, err := client.readWord(record + 4)
		if err != nil {
			return nil, 0, err
		}
		member.Name, _ = client.readPrintableString(name)
		descriptor, err := client.readWord(record + 8)
		if err != nil {
			return nil, 0, err
		}
		member.Descriptor, _ = client.readPrintableString(descriptor)
		if member.Kind, err = client.readWord(record + 12); err != nil {
			return nil, 0, err
		}
		if member.Index, err = client.readWord(record + 16); err != nil {
			return nil, 0, err
		}
		if words >= javaMethodWords {
			if member.Body, err = client.readWord(record + 20); err != nil {
				return nil, 0, err
			}
		}
		members = append(members, member)
	}
	return members, at + count*uint32(words)*4, nil
}

// javaClassList is what the class-list call hands over: the application's own
// classes, and the title's string constants.
type javaClassList struct {
	Table   uint32
	Out     uint32
	Classes []javaClass
	Strings []string
}

// takeJavaClassList reads the class list and the string constants behind it,
// and keeps them. Nothing runs on them yet; what they are for is the object
// model a later attempt builds, and reading them here is what keeps the shape
// honest against a real title.
func (client *Client) takeJavaClassList(table, out uint32) error {
	classes, err := client.readJavaClassList(table)
	if err != nil {
		return err
	}
	list := &javaClassList{Table: table, Out: out, Classes: classes}
	if list.Strings, err = client.readJavaStringPool(table, uint32(len(classes)), out); err != nil {
		return err
	}
	client.javaClasses = list
	if client.logger != nil {
		for _, line := range describeJavaClasses(classes) {
			client.logger.Debug("LGT java class", "record", line)
		}
		client.logger.Debug("LGT java string constants", "count", len(list.Strings))
	}
	return nil
}

// readJavaStringPool reads the constants that follow the class handles: a
// uint16 length and that many UTF-16LE code units, one pointer each.
//
// **The list ends where the output array begins.** The module lays the two out
// back to back, which is the only thing here that says how many constants
// there are — the count is nowhere in the table, and reading until a pointer
// stops looking like a string stops early on the first empty one.
func (client *Client) readJavaStringPool(table, classes, out uint32) ([]string, error) {
	start := table + 8 + classes*4
	if out <= start {
		return nil, nil
	}
	count := (out - start) / 4
	if count > maxJavaStringConstants {
		return nil, fmt.Errorf("%d string constants at %#x is not a count", count, start)
	}
	pool := make([]string, 0, count)
	for index := uint32(0); index < count; index++ {
		pointer, err := client.readWord(start + index*4)
		if err != nil {
			return nil, err
		}
		text, err := client.readJavaStringConstant(pointer)
		if err != nil {
			return nil, fmt.Errorf("string constant %d at %#x: %w", index, pointer, err)
		}
		pool = append(pool, text)
	}
	return pool, nil
}

func (client *Client) readJavaStringConstant(pointer uint32) (string, error) {
	if pointer == 0 {
		return "", nil
	}
	length, err := client.readHalfword(pointer)
	if err != nil {
		return "", err
	}
	return client.readJavaUTF16(pointer+2, uint32(length))
}

// readJavaUTF16 reads a run of UTF-16LE code units. It is what a string
// constant is made of, both where the pool carries its own length and where the
// module hands the length over itself.
func (client *Client) readJavaUTF16(address, length uint32) (string, error) {
	if length > maxJavaStringLength {
		return "", fmt.Errorf("length %d is not a length", length)
	}
	units := make([]uint16, length)
	for index := range units {
		unit, err := client.readHalfword(address + uint32(index)*2)
		if err != nil {
			return "", err
		}
		units[index] = unit
	}
	return string(utf16.Decode(units)), nil
}

// readJavaClassList reads the table the class-list call is handed: a count, a
// zero word, then that many handles, then the string constants.
func (client *Client) readJavaClassList(table uint32) ([]javaClass, error) {
	count, err := client.readWord(table)
	if err != nil {
		return nil, err
	}
	if count == 0 || count > maxJavaMembers {
		return nil, fmt.Errorf("class count %#x at %#x is not a count", count, table)
	}
	handles := make([]uint32, count)
	index := make(map[uint32]int, count)
	for slot := uint32(0); slot < count; slot++ {
		handle, err := client.readWord(table + 8 + slot*4)
		if err != nil {
			return nil, err
		}
		handles[slot] = handle
		index[handle] = int(slot)
	}
	classes := make([]javaClass, 0, count)
	for _, handle := range handles {
		class, err := client.readJavaClass(handle, index)
		if err != nil {
			return nil, err
		}
		classes = append(classes, class)
	}
	return classes, nil
}

// describeJavaClasses reports what was read, one line per class. It exists to
// keep the shape readable out of a real title and nothing consumes it: running
// one of these still needs the compiled bodies and a launcher, and a class list
// read but not run is not a title that starts.
func describeJavaClasses(classes []javaClass) []string {
	lines := make([]string, 0, len(classes))
	for _, class := range classes {
		super := class.Super
		if class.SuperHandle != 0 {
			super = fmt.Sprintf("handle %#x", class.SuperHandle)
		}
		if !class.Inline {
			lines = append(lines, fmt.Sprintf("%#x %q access %#x super %q members not inline",
				class.Handle, class.Name, class.AccessFlags, super))
			continue
		}
		names := make([]string, 0, len(class.Fields)+len(class.Methods))
		for _, field := range class.Fields {
			names = append(names, field.Name+":"+field.Descriptor)
		}
		// The body address goes in with the name, because an address is what
		// everything else here reports: a profile, a platform call's `from=`,
		// a backtrace frame and a disassembly all name code by where it
		// starts, and this line is what turns one back into a method.
		for _, method := range class.Methods {
			names = append(names, fmt.Sprintf("%s%s@%#x",
				method.Name, method.Descriptor, method.Body))
		}
		lines = append(lines, fmt.Sprintf("%#x %q access %#x super %q %d fields %d methods ends %#x [%s]",
			class.Handle, class.Name, class.AccessFlags, super,
			len(class.Fields), len(class.Methods), class.End, strings.Join(names, " ")))
	}
	return lines
}
