package lgt

import (
	"fmt"
	"sort"
)

// Where a member sits: a field's word inside an object, and a virtual method's
// slot inside a vtable. The compiled code holds neither — it holds an index
// into one of the out arrays, and this is what fills those in. See docs/lgt.md,
// "The slot rules, and what they say about the platform's own classes".
//
// The rules are two, and both are the module's own arithmetic rather than this
// platform's choice:
//
//   - A field's slot is its position in the class's run, counted from
//     `instanceSize - fieldCount`. The compiler baked `instanceSize` into the
//     class record assuming the platform's superclass layout, so subtracting
//     the class's own fields is what reads that assumption back out.
//   - A virtual method's slot is its position among the class's *new* methods,
//     counted from the superclass's vtable size — and a run holds overrides as
//     well as new methods. An override takes the slot its superclass gave the
//     same name and descriptor, so the two are told apart by name rather than
//     by counting.
//
// Both are self-checking: the numbers have to end exactly at the sizes the
// record declares, and a layout that overruns one is a misreading rather than
// a difference of opinion.

// javaObjectVTableSize is how many slots `java/lang/Object` takes, which is not
// negotiable: an application class rooted at Object arrives with a vtable the
// compiler built and numbered from 11.
const javaObjectVTableSize = 11

// javaPlatformSupers is the platform class hierarchy, as the WIPI specification
// gives it, for the classes whose superclass is not `java/lang/Object`. It is
// only consulted for slot placement, so a class missing from it is treated as
// rooted at Object — which is right for every class the specification roots
// there, and harmless for a class nothing extends.
var javaPlatformSupers = map[string]string{
	"org/kwis/msp/lwc/ContainerComponent":   "org/kwis/msp/lwc/Component",
	"org/kwis/msp/lwc/ShellComponent":       "org/kwis/msp/lwc/ContainerComponent",
	"org/kwis/msp/lwc/AnnunciatorComponent": "org/kwis/msp/lwc/ShellComponent",
	"org/kwis/msp/lwc/DialogComponent":      "org/kwis/msp/lwc/ShellComponent",
	"org/kwis/msp/lwc/FormComponent":        "org/kwis/msp/lwc/ContainerComponent",
	"org/kwis/msp/lwc/ListComponent":        "org/kwis/msp/lwc/FormComponent",
	"org/kwis/msp/lwc/TextComponent":        "org/kwis/msp/lwc/Component",
	"org/kwis/msp/lwc/TextBoxComponent":     "org/kwis/msp/lwc/TextComponent",
	"org/kwis/msp/lwc/TextFieldComponent":   "org/kwis/msp/lwc/TextComponent",
	"org/kwis/msp/lwc/LabelComponent":       "org/kwis/msp/lwc/Component",
	"org/kwis/msp/lwc/CheckboxComponent":    "org/kwis/msp/lwc/LabelComponent",
	"org/kwis/msp/lwc/ListItemComponent":    "org/kwis/msp/lwc/LabelComponent",
	"org/kwis/msp/lwc/ProxyCard":            "org/kwis/msp/lcdui/Card",

	// The stream hierarchy, for the same reason: a `DataInputStream` inherits
	// `InputStream`'s slots, and a wrapper reached through one is reported —
	// and looked up — under the class whose vtable built the stub.
	"java/io/DataInputStream":       "java/io/InputStream",
	"java/io/ByteArrayInputStream":  "java/io/InputStream",
	"java/io/DataOutputStream":      "java/io/OutputStream",
	"java/io/ByteArrayOutputStream": "java/io/OutputStream",

	// The exception hierarchy, which is not about slot placement at all: it is
	// what the catch test walks. A `catch (Exception e)` around a call that
	// throws `IOException` is one of the commonest shapes there is, and with
	// every class rooted directly at Object it does not match — the module
	// rethrows, and the title stops on an exception it had handling for.
	"java/lang/Throwable":                               "java/lang/Object",
	"java/lang/Exception":                               "java/lang/Throwable",
	"java/lang/Error":                                   "java/lang/Throwable",
	"java/lang/RuntimeException":                        "java/lang/Exception",
	"java/io/IOException":                               "java/lang/Exception",
	"java/lang/ClassNotFoundException":                  "java/lang/Exception",
	"java/lang/InterruptedException":                    "java/lang/Exception",
	"java/lang/IllegalAccessException":                  "java/lang/Exception",
	"java/lang/InstantiationException":                  "java/lang/Exception",
	"java/lang/ArithmeticException":                     "java/lang/RuntimeException",
	"java/lang/ArrayStoreException":                     "java/lang/RuntimeException",
	"java/lang/ClassCastException":                      "java/lang/RuntimeException",
	"java/lang/IllegalArgumentException":                "java/lang/RuntimeException",
	"java/lang/IllegalStateException":                   "java/lang/RuntimeException",
	"java/lang/IndexOutOfBoundsException":               "java/lang/RuntimeException",
	"java/lang/NegativeArraySizeException":              "java/lang/RuntimeException",
	"java/lang/NullPointerException":                    "java/lang/RuntimeException",
	"java/lang/NumberFormatException":                   "java/lang/IllegalArgumentException",
	"java/lang/ArrayIndexOutOfBoundsException":          "java/lang/IndexOutOfBoundsException",
	"java/lang/StringIndexOutOfBoundsException":         "java/lang/IndexOutOfBoundsException",
	"java/lang/OutOfMemoryError":                        "java/lang/Error",
	"java/lang/VirtualMachineError":                     "java/lang/Error",
	"java/io/EOFException":                              "java/io/IOException",
	"java/io/InterruptedIOException":                    "java/io/IOException",
	"java/io/UnsupportedEncodingException":              "java/io/IOException",
	"java/io/UTFDataFormatException":                    "java/io/IOException",
	"org/kwis/msp/io/FileSystemException":               "java/io/IOException",
	"org/kwis/msp/db/DataBaseException":                 "java/lang/Exception",
	"org/kwis/msp/db/DataBaseRecordException":           "org/kwis/msp/db/DataBaseException",
	"org/kwis/msf/io/SchemeNotFoundException":           "java/io/IOException",
	"javax/microedition/io/ConnectionNotFoundException": "java/io/IOException",
}

// javaLayoutClass is one class's layout, whether the platform owns it or the
// application declares it.
type javaLayoutClass struct {
	Name         string
	Super        string
	InstanceSize uint32
	VTableSize   uint32
	// Virtual is the slot this class gives each name and descriptor it
	// declares. A subclass looks through it before allocating a slot of its
	// own, which is what makes an override share one.
	Virtual map[string]uint32
	// Application reports whether the module declared the class. The platform's
	// own are laid out here; the application's are read back out of its records.
	Application bool
	// Measured reports whether a platform class's vtable size came from a
	// subclass rather than from counting what it declares. Only the measured
	// one is the size the compiler assumed, and a second subclass has to agree
	// with it.
	Measured bool
}

// javaLayout is every class this module has had laid out so far.
type javaLayout struct {
	classes map[string]*javaLayoutClass
}

func newJavaLayout() *javaLayout {
	layout := &javaLayout{classes: map[string]*javaLayoutClass{}}
	layout.classes["java/lang/Object"] = &javaLayoutClass{
		Name:       "java/lang/Object",
		VTableSize: javaObjectVTableSize,
		Virtual:    map[string]uint32{},
	}
	return layout
}

func javaMemberKey(member javaMemberRef) string { return member.Name + member.Descriptor }

// class answers a class's layout, creating an empty platform one if this is the
// first mention of it.
func (layout *javaLayout) class(name string) *javaLayoutClass {
	if existing, ok := layout.classes[name]; ok {
		return existing
	}
	class := &javaLayoutClass{Name: name, Super: javaPlatformSuper(name), Virtual: map[string]uint32{}}
	layout.classes[name] = class
	return class
}

func javaPlatformSuper(name string) string {
	if name == "java/lang/Object" {
		return ""
	}
	if super, ok := javaPlatformSupers[name]; ok {
		return super
	}
	return "java/lang/Object"
}

// vtableSize answers how many slots a class takes, laying out any platform
// superclass that has not been reached yet.
func (layout *javaLayout) vtableSize(name string) uint32 {
	if name == "" {
		return 0
	}
	class := layout.class(name)
	if class.VTableSize != 0 {
		return class.VTableSize
	}
	// A platform class nothing has measured takes its superclass's slots plus
	// the ones it declares. Only a class the application extends has a size
	// that has to agree with anything, and that one is measured rather than
	// assumed.
	class.VTableSize = layout.vtableSize(class.Super) + uint32(len(class.Virtual))
	return class.VTableSize
}

// findVirtual walks a class's superclasses for a name and descriptor, which is
// how an override finds the slot it has to share.
func (layout *javaLayout) findVirtual(name, key string) (uint32, bool) {
	for name != "" {
		class, ok := layout.classes[name]
		if !ok {
			return 0, false
		}
		if slot, ok := class.Virtual[key]; ok {
			return slot, true
		}
		name = class.Super
	}
	return 0, false
}

// layoutPlatformClasses gives every platform class the load call names a slot
// for each virtual method it declares. The slots are its own — a class's
// methods start where its superclass's vtable ends — so two unrelated platform
// classes may well share a number, and an instance of either only ever
// dispatches through its own.
func (layout *javaLayout) layoutPlatformClasses(surface *javaSurface) (map[uint32]uint32, error) {
	answers := map[uint32]uint32{}
	// In hierarchy order, so a superclass is sized before a subclass asks.
	classes := append([]javaAPIClass(nil), surface.Classes...)
	sort.SliceStable(classes, func(a, b int) bool {
		return javaPlatformDepth(classes[a].Name) < javaPlatformDepth(classes[b].Name)
	})
	for _, api := range classes {
		class := layout.class(api.Name)
		next := layout.vtableSize(class.Super)
		for index := api.VirtualMethods.Start; index < api.VirtualMethods.Start+api.VirtualMethods.Count; index++ {
			if int(index) >= len(surface.VirtualMethods) {
				return nil, fmt.Errorf("platform class %s claims virtual entry %d of %d",
					api.Name, index, len(surface.VirtualMethods))
			}
			key := javaMemberKey(surface.VirtualMethods[index])
			slot, inherited := layout.findVirtual(class.Super, key)
			if !inherited {
				slot = next
				next++
			}
			class.Virtual[key] = slot
			answers[index] = slot
		}
		if class.VTableSize < next {
			class.VTableSize = next
		}
	}
	return answers, nil
}

func javaPlatformDepth(name string) int {
	depth := 0
	for super := javaPlatformSuper(name); super != ""; super = javaPlatformSuper(super) {
		depth++
		if depth > len(javaPlatformSupers)+1 {
			break
		}
	}
	return depth
}

// javaClassLayoutEntry is one entry of the table the module keeps for its own
// classes: the runs of the shared member tables that belong to one class.
type javaClassLayoutEntry struct {
	Name           string
	Fields         javaRun
	StaticFields   javaRun
	VirtualMethods javaRun
	Methods        javaRun
	StaticMethods  javaRun
}

// readJavaClassLayoutEntry reads one 24-byte entry of the module's own class
// table: a name and the five runs, in the same shape the platform class table
// uses.
func (client *Client) readJavaClassLayoutEntry(at uint32) (javaClassLayoutEntry, error) {
	if at == 0 || at&3 != 0 {
		return javaClassLayoutEntry{}, fmt.Errorf("class layout entry %#x is null or unaligned", at)
	}
	words := make([]uint32, 6)
	for index := range words {
		word, err := client.readWord(at + uint32(index)*4)
		if err != nil {
			return javaClassLayoutEntry{}, err
		}
		words[index] = word
	}
	name, ok := client.readPrintableString(words[0])
	if !ok {
		return javaClassLayoutEntry{}, fmt.Errorf("class layout entry %#x has no name", at)
	}
	return javaClassLayoutEntry{
		Name:           name,
		Fields:         javaRunOf(words[1]),
		StaticFields:   javaRunOf(words[2]),
		VirtualMethods: javaRunOf(words[3]),
		Methods:        javaRunOf(words[4]),
		StaticMethods:  javaRunOf(words[5]),
	}, nil
}

// defineJavaClass answers the per-class layout call: one application class's
// fields and virtual methods, numbered against the class record's own sizes.
func (client *Client) defineJavaClass(values []uint32) error {
	if client.javaLink == nil || client.javaLink.layout == nil {
		return fmt.Errorf("a class was laid out before the platform classes were loaded")
	}
	if len(values) < 12 {
		return fmt.Errorf("the per-class layout call takes 12 arguments, not %d", len(values))
	}
	entry, err := client.readJavaClassLayoutEntry(values[0])
	if err != nil {
		return err
	}
	record, err := client.readJavaClass(values[1], nil)
	if err != nil {
		return fmt.Errorf("the class %s handed over at %#x: %w", entry.Name, values[1], err)
	}
	if record.Name != entry.Name {
		return fmt.Errorf("the layout entry names %s and the record names %s", entry.Name, record.Name)
	}
	answers, err := client.javaLink.layout.layoutApplicationClass(record, entry, client.javaLink.surface)
	if err != nil {
		return err
	}
	if err := client.writeJavaSlots(values[7], answers.Fields); err != nil {
		return fmt.Errorf("write %s's field answers at %#x: %w", record.Name, values[7], err)
	}
	if err := client.writeJavaSlots(values[9], answers.Virtual); err != nil {
		return fmt.Errorf("write %s's virtual method answers at %#x: %w", record.Name, values[9], err)
	}
	if client.logger != nil {
		// `entries` is which of the surface's field-table entries this class
		// answered, which is what a field index read out of a disassembly is
		// an index into: it says which class owns the field an out-array
		// offset names.
		client.logger.Debug("LGT java class laid out",
			"class", record.Name, "super", record.Super+record.SuperName,
			"instance", record.InstanceSize, "vtable", record.VTableSize,
			"fields", len(answers.Fields), "virtual", len(answers.Virtual),
			"entries", describeJavaEntries(answers.Fields))
	}
	return nil
}

// describeJavaEntries names the stretch of a member table a class answered,
// as `first..last`, for the debug line above.
func describeJavaEntries(answers map[uint32]uint32) string {
	if len(answers) == 0 {
		return "none"
	}
	first, last := ^uint32(0), uint32(0)
	for entry := range answers {
		if entry < first {
			first = entry
		}
		if entry > last {
			last = entry
		}
	}
	return fmt.Sprintf("%d..%d", first, last)
}

// javaClassAnswers is what one application class's layout comes to: a slot per
// member table entry, keyed by the entry's index.
type javaClassAnswers struct {
	Fields  map[uint32]uint32
	Virtual map[uint32]uint32
}

// layoutApplicationClass gives one application class its field and vtable
// slots. The record is the module's own, and it is what the arithmetic is
// checked against: the last field has to land one short of the instance size
// and the last new method one short of the vtable size.
func (layout *javaLayout) layoutApplicationClass(
	record javaClass, entry javaClassLayoutEntry, surface *javaSurface) (javaClassAnswers, error) {

	answers := javaClassAnswers{Fields: map[uint32]uint32{}, Virtual: map[uint32]uint32{}}
	super := record.Super
	if record.SuperName != "" {
		super = record.SuperName
	}
	class := &javaLayoutClass{
		Name: record.Name, Super: super, InstanceSize: uint32(record.InstanceSize),
		VTableSize: uint32(record.VTableSize), Virtual: map[string]uint32{}, Application: true,
	}

	if entry.Fields.Count > uint32(record.InstanceSize) {
		return answers, fmt.Errorf("class %s declares %d fields in an object of %d words",
			record.Name, entry.Fields.Count, record.InstanceSize)
	}
	base := uint32(record.InstanceSize) - entry.Fields.Count
	if superClass, ok := layout.classes[super]; ok && superClass.Application &&
		superClass.InstanceSize != base {
		return answers, fmt.Errorf("class %s puts its fields at %d, where %s ends at %d",
			record.Name, base, super, superClass.InstanceSize)
	}
	for offset := uint32(0); offset < entry.Fields.Count; offset++ {
		answers.Fields[entry.Fields.Start+offset] = base + offset
	}

	// The superclass's size is what new slots are counted from. For a platform
	// superclass the module is the only thing that knows it, and this is where
	// it says: the class's own new methods are the ones its superclass does not
	// declare, so the size is what is left when they are taken off the end.
	newMethods := uint32(0)
	keys := make([]string, 0, entry.VirtualMethods.Count)
	for offset := uint32(0); offset < entry.VirtualMethods.Count; offset++ {
		index := entry.VirtualMethods.Start + offset
		if int(index) >= len(surface.VirtualMethods) {
			return answers, fmt.Errorf("class %s claims virtual entry %d of %d",
				record.Name, index, len(surface.VirtualMethods))
		}
		key := javaMemberKey(surface.VirtualMethods[index])
		keys = append(keys, key)
		if _, inherited := layout.findVirtual(super, key); !inherited {
			newMethods++
		}
	}
	if newMethods > uint32(record.VTableSize) {
		return answers, fmt.Errorf("class %s declares %d new methods in a vtable of %d",
			record.Name, newMethods, record.VTableSize)
	}
	next := uint32(record.VTableSize) - newMethods
	if superClass, ok := layout.classes[super]; ok {
		if superClass.Application && superClass.VTableSize != next {
			return answers, fmt.Errorf("class %s starts its methods at %d, where %s ends at %d",
				record.Name, next, super, superClass.VTableSize)
		}
		if !superClass.Application {
			// The platform's own class, as this module has it. Its declared
			// methods have to fit below where the subclass starts, and a second
			// subclass has to leave it the same room as the first.
			if superClass.Measured && superClass.VTableSize != next {
				return answers, fmt.Errorf("%s was measured at %d slots, and %s counts on %d",
					super, superClass.VTableSize, record.Name, next)
			}
			for _, slot := range superClass.Virtual {
				if slot >= next {
					return answers, fmt.Errorf(
						"%s puts a method at slot %d, but %s starts its own at %d",
						super, slot, record.Name, next)
				}
			}
			superClass.VTableSize, superClass.Measured = next, true
		}
	}
	for offset, key := range keys {
		index := entry.VirtualMethods.Start + uint32(offset)
		slot, inherited := layout.findVirtual(super, key)
		if !inherited {
			slot = next
			next++
		}
		class.Virtual[key] = slot
		answers.Virtual[index] = slot
	}
	layout.classes[record.Name] = class
	return answers, nil
}
