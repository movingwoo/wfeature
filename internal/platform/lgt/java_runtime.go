package lgt

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The object model an AOT title runs on: class objects, vtables and instances.
//
// The shapes are the module's, not this platform's, and every one of them was
// read out of a call site (docs/lgt.md, "The three thunks every class carries"
// and the sections after it):
//
//	object + 0x00   its class's vtable
//	object + 0x08   the data block: field n is word n of it
//	vtable - 0x04   the class
//	vtable + 0x04   slot 0, and slot n four bytes further on
//
// A class object is an object like any other, and the module reads two things
// out of it: the name, at word 2 of its data, and the state halfword at word 4,
// which is 5 once the class is initialised. Nothing else about it is the
// module's business, so nothing else is filled in.
const (
	javaObjectWords     = 3 // vtable, unused, data
	javaClassDataWords  = 5 // ..., name at 2, state at 4
	javaClassNameWord   = 2 //
	javaClassStateWord  = 4 //
	javaClassReady      = 3 // the module's own record: prepared
	javaClassInitedFlag = 5 // the class object: initialised
	// javaUnmeasuredVTableSize is how many slots a platform class gets when
	// nothing in the module says how many it has. See preparePlatformJavaClass.
	javaUnmeasuredVTableSize = 64
	// javaUnmeasuredInstanceWords is how large an instance of a platform class
	// gets when nothing in the module says. See allocateJavaObject.
	javaUnmeasuredInstanceWords = 64
	maxJavaVTableSlots          = 1024
	maxJavaInstanceSize         = 4096
)

// javaRuntimeClass is one class as this platform has laid it out.
type javaRuntimeClass struct {
	Name string
	// Handle is the module's own record, and zero for a platform class the
	// module never declares.
	Handle uint32
	Record javaClass
	// Object is what the resolve call answers, VTable what an instance's first
	// word points at, and Slots how many the vtable has.
	Object   uint32
	VTable   uint32
	Slots    uint32
	Instance uint32
	Super    *javaRuntimeClass
	// ElementBytes is how wide one element is, for an array type, and zero for
	// every other class. See java_array.go.
	ElementBytes uint32
	// StaticWords is how many words of storage this class's own static fields
	// need, past the words the class object itself uses. The module's record
	// says it for an application class; for a platform class the load call's
	// static-field table does. See allocateJavaClassObject.
	StaticWords uint32
	// Measured reports whether the module's own records said how large this
	// class is, which is only so for a platform class the application extends.
	Measured bool

	initialized bool
	// dataBlock is what the class object's third word held when it was built.
	// The module reads a class through that word on every use, so checking it
	// is what turns a stray write into a named failure; see checkJavaClassObject.
	dataBlock uint32
}

// javaRuntime is every class this title has had prepared, and the strings its
// constants became.
type javaRuntime struct {
	byHandle map[uint32]*javaRuntimeClass
	byObject map[uint32]*javaRuntimeClass
	byName   map[string]*javaRuntimeClass
	strings  map[uint32]string
	// singletons are the platform objects a title asks for by name — the
	// display, the runtime — each of which is one object for the life of the
	// title.
	singletons map[string]uint32
	// random is the generator behind each java/util/Random the title built.
	random map[uint32]*rand.Rand
	// streams are the resource streams a title has opened, and images the
	// surfaces its Image objects stand for. See java_stream.go.
	streams map[uint32]*javaStream
	images  map[uint32]uint32
	// files are the open files a title's File objects stand for, by the handle
	// the C path opened them with. See java_file.go.
	files map[uint32]uint32
	// graphics is the drawing state behind each Graphics object, and card is
	// the one the application pushed onto the display — the method the platform
	// paints a frame through. See java_screen.go and java_frame.go.
	// threads are the guest threads a title has built, by the object it holds
	// each one by, and workers are the ones that have been started. See
	// java_thread.go.
	threads map[uint32]*javaThread
	// monitors are the locks `synchronized` takes, by the object each is on.
	monitors   map[uint32]*javaMonitor
	mainThread uint32
	// vectors is what each java/util/Vector holds; see java_vector.go, and
	// databases the record stores a title has open; see java_database.go.
	vectors map[uint32][]uint32
	// calendars is the instant each java/util/Calendar stands for, and sinks
	// what each java/io/ByteArrayOutputStream has been written; see
	// java_calendar.go and java_stream.go.
	calendars map[uint32]int64
	sinks     map[uint32][]byte
	// wrapped is what a java/io/DataOutputStream writes through to: the sink
	// object it was built on. A wrapper stands for the same open sink, so it
	// holds the other object's handle rather than a second block of bytes.
	wrapped map[uint32]uint32
	// dates is the instant each java/util/Date stands for.
	dates          map[uint32]int64
	databases      map[uint32]*javaDatabase
	workers        []*javaWorker
	threadStacks   int
	graphics       map[uint32]*javaGraphics
	card           uint32
	cardDirty      bool
	screenGraphics uint32
	// named is every distinct string constant the title has built. A constant
	// is built by the code that holds it, so the set answers "has the title
	// reached the code that names this" — and the set, rather than a line per
	// build, is what keeps the answer readable: one title rebuilds the same
	// constant every frame.
	named map[string]bool
	// serial is what `Display.callSerially` has been handed and not yet run.
	// See java_frame.go.
	serial []uint32
}

func newJavaRuntime() *javaRuntime {
	return &javaRuntime{
		byHandle:   map[uint32]*javaRuntimeClass{},
		byObject:   map[uint32]*javaRuntimeClass{},
		byName:     map[string]*javaRuntimeClass{},
		strings:    map[uint32]string{},
		named:      map[string]bool{},
		singletons: map[string]uint32{},
		random:     map[uint32]*rand.Rand{},
		streams:    map[uint32]*javaStream{},
		images:     map[uint32]uint32{},
		files:      map[uint32]uint32{},
		graphics:   map[uint32]*javaGraphics{},
		threads:    map[uint32]*javaThread{},
		monitors:   map[uint32]*javaMonitor{},
		vectors:    map[uint32][]uint32{},
		calendars:  map[uint32]int64{},
		sinks:      map[uint32][]byte{},
		wrapped:    map[uint32]uint32{},
		dates:      map[uint32]int64{},
		databases:  map[uint32]*javaDatabase{},
	}
}

func (client *Client) javaRuntimeState() *javaRuntime {
	if client.javaRun == nil {
		client.javaRun = newJavaRuntime()
	}
	return client.javaRun
}

// prepareJavaClass lays a class out and builds everything the compiled code
// reaches for: its vtable, its class object, and the same for every class it
// extends. It is what the module's prepare call asks for, and the launcher
// asks for it too.
//
// **The superclass goes first.** A class's own methods are numbered from where
// its superclass's vtable ends, and its own fields from where the superclass's
// object ends, so a subclass laid out before its superclass is laid out against
// nothing.
func (client *Client) prepareJavaClass(
	ctx context.Context, thread *armcore.Thread, handle uint32,
) (*javaRuntimeClass, error) {
	runtime := client.javaRuntimeState()
	if class, ok := runtime.byHandle[handle]; ok {
		return class, nil
	}
	record, err := client.readJavaClass(handle, nil)
	if err != nil {
		return nil, fmt.Errorf("the class record at %#x: %w", handle, err)
	}
	if record.Name == "" {
		return nil, fmt.Errorf("the class record at %#x has no name", handle)
	}
	class := &javaRuntimeClass{
		Name: record.Name, Handle: handle, Record: record,
		Slots: uint32(record.VTableSize), Instance: uint32(record.InstanceSize),
		StaticWords: record.StaticWords,
	}
	if class.Slots > maxJavaVTableSlots || class.Instance > maxJavaInstanceSize {
		return nil, fmt.Errorf("class %s declares %d vtable slots and %d words",
			record.Name, class.Slots, class.Instance)
	}
	// Claim the handle before the superclass walk, so a record that names
	// itself somewhere up its own chain stops here rather than recursing.
	runtime.byHandle[handle] = class
	// **The class object is built before anything else it needs is.** Laying a
	// class out runs the module's own code, and that code resolves classes —
	// including, on the way through a class's own members, the class being laid
	// out. A half-built class answered with a null object turns into an
	// allocation of nothing several calls later, which is a long way from the
	// re-entry that caused it. Nothing in the object depends on the layout.
	if class.Object, err = client.allocateJavaClassObject(class); err != nil {
		return nil, err
	}
	class.dataBlock, _ = client.readWord(class.Object + 8)
	runtime.byObject[class.Object] = class
	runtime.byName[class.Name] = class

	if record.SuperHandle != 0 {
		if class.Super, err = client.prepareJavaClass(ctx, thread, record.SuperHandle); err != nil {
			return nil, err
		}
	} else if record.Super != "" {
		if class.Super, err = client.preparePlatformJavaClass(record.Super); err != nil {
			return nil, err
		}
	}

	// The class declares its members through the first of its three thunks,
	// which calls back into the platform with the runs that belong to it.
	if record.Body[0] != 0 {
		if _, err := client.callOn(ctx, thread, record.Body[0], []uint32{handle}); err != nil {
			return nil, fmt.Errorf("have class %s declare its members: %w", record.Name, err)
		}
	}
	if err := client.buildJavaVTable(class); err != nil {
		return nil, err
	}
	// The module gates its own use of a class on this halfword, so writing it
	// is what says the platform has done its half.
	if err := client.writeHalfword(record.Header+0x1a, javaClassReady); err != nil {
		return nil, err
	}
	if client.logger != nil {
		client.logger.Debug("LGT java class prepared", "class", class.Name,
			"vtable", class.VTable, "slots", class.Slots, "instance", class.Instance,
			"object", class.Object)
	}
	return class, nil
}

// preparePlatformJavaClass lays out a class the module only ever extends. Its
// size is whatever the module's records said it was, and every slot it has is a
// stub: nothing of the platform's own Java API is implemented, and a stub that
// is reached names the method it stands for.
func (client *Client) preparePlatformJavaClass(name string) (*javaRuntimeClass, error) {
	runtime := client.javaRuntimeState()
	if class, ok := runtime.byName[name]; ok {
		return class, nil
	}
	class := &javaRuntimeClass{Name: name}
	measured := false
	if client.javaLink != nil && client.javaLink.layout != nil {
		if laid, ok := client.javaLink.layout.classes[name]; ok {
			class.Slots = laid.VTableSize
			class.Instance = laid.InstanceSize
			// **A platform class's statics are storage the module reads
			// directly.** Its class object's block has to be long enough for
			// the run the load call numbered, or the module's own read of one
			// lands past the end of what the arena handed out.
			class.StaticWords = laid.StaticWords
			measured = laid.Measured
			class.Measured = laid.Measured
		}
	}
	if !measured && class.Slots < javaUnmeasuredVTableSize {
		// A class the application never extends has no size the module agrees
		// with, and it still gets dispatched through: **the compiler baked the
		// slot numbers of the platform's own classes**, so a call arrives at a
		// slot this platform never numbered. The table is made large enough to
		// catch those rather than small enough to be exact, because every slot
		// past the end is a branch to whatever word follows the table. What the
		// size costs is a page of stubs; what it buys is that an unimplemented
		// platform method reports its class and its slot instead of executing
		// unmapped memory.
		class.Slots = javaUnmeasuredVTableSize
	}
	// The class object comes first, the same way it does for a class the module
	// declares: a platform class has no record to be named by, so its own class
	// object is what stands in front of its vtable as its identity.
	object, err := client.allocateJavaClassObject(class)
	if err != nil {
		return nil, err
	}
	class.Object = object
	class.dataBlock, _ = client.readWord(object + 8)
	runtime.byObject[object] = class
	runtime.byName[name] = class
	if err := client.buildJavaVTable(class); err != nil {
		return nil, err
	}
	if err := client.initializePlatformStatics(class); err != nil {
		return nil, err
	}
	return class, nil
}

// javaPlatformStatics is what a platform class's static fields hold. There is
// one class in it, and what a title reaches for on it is the pair it logs
// through: `java/lang/System.out` and `err`. Nothing in the module writes
// either — a static this platform owns is read-only from the guest's side — so
// they are filled in when the class is laid out, and a field this map has no
// entry for keeps the zero its block was allocated with.
//
// **A null here is a title's own NullPointerException.** The module compiles
// `System.out.println(...)` to a null test and a throw around a vtable
// dispatch, so leaving the field empty does not skip the print: it stops the
// title, inside whatever routine happened to log a line. One local title
// reaches its first `println` from the constructor of its Jlet.
var javaPlatformStatics = map[string]map[string]string{
	// `err` is the same stream as `out` here. There is one place a line can go
	// and no way to read two of them apart afterwards, so giving them separate
	// objects would buy nothing and cost a title that logs to both the order
	// its own lines came in.
	"java/lang/System": {
		"outLjava/io/PrintStream;": javaPrintStreamClass,
		"errLjava/io/PrintStream;": javaPrintStreamClass,
	},
}

// initializePlatformStatics fills in the static fields this platform owns on a
// class it has just laid out.
func (client *Client) initializePlatformStatics(class *javaRuntimeClass) error {
	held, known := javaPlatformStatics[class.Name]
	if !known || client.javaLink == nil || client.javaLink.layout == nil {
		return nil
	}
	laid, ok := client.javaLink.layout.classes[class.Name]
	if !ok {
		return nil
	}
	for key, standsFor := range held {
		slot, declared := laid.Statics[key]
		if !declared || slot >= class.StaticWords {
			// The module did not ask for this field, so nothing reads it.
			continue
		}
		object, err := client.javaPlatformObject(standsFor)
		if err != nil {
			return err
		}
		if err := client.writeWord(class.dataBlock+(javaClassDataWords+slot)*4, object); err != nil {
			return err
		}
	}
	return nil
}

// javaPlatformObject answers the one instance of a platform class that stands
// for a facility rather than for data — the same object every time, the way
// `getDefaultDisplay` and `getDefaultFont` answer theirs.
func (client *Client) javaPlatformObject(name string) (uint32, error) {
	runtime := client.javaRuntimeState()
	if object, ok := runtime.singletons[name]; ok {
		return object, nil
	}
	class, err := client.preparePlatformJavaClass(name)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	runtime.singletons[name] = object
	return object, nil
}

// checkJavaClassObject reports a class object whose data pointer has been
// written over.
//
// **Nothing the platform hands the guest is out of the guest's reach**, and a
// class object is the one the guest touches most: every use of a class goes
// through the word this checks. A stray write there does not fail where it
// happens — it fails at the next resolve, as a read of whatever the word became,
// which reads like the class protocol having gone wrong rather than like memory
// having been overwritten. Checking it costs one read per resolve and names the
// class instead.
func (client *Client) checkJavaClassObject(class *javaRuntimeClass) error {
	if class.dataBlock == 0 {
		return nil
	}
	word, err := client.readWord(class.Object + 8)
	if err != nil {
		return err
	}
	if word != class.dataBlock {
		return fmt.Errorf(
			"%s's class object at %#x names %#x where it was built naming %#x: something wrote over it",
			class.Name, class.Object, word, class.dataBlock)
	}
	return nil
}

// buildJavaVTable fills the class's dispatch table. A class the compiler could
// lay out arrives with one inside its record, and its zeroes are the slots it
// inherits without overriding; a class extending a platform class arrives with
// none and gets one here. Either way the superclass's slots are copied in
// first and the class's own methods written over them, which is what an
// override is.
func (client *Client) buildJavaVTable(class *javaRuntimeClass) error {
	slots := class.Slots
	if class.Record.VTable != 0 {
		class.VTable = class.Record.VTable
	} else {
		table, err := client.allocate(uint64(slots+1) * 4)
		if err != nil {
			return err
		}
		// The word in front of slot 0 is the class, which is where a call site
		// finds the class of the object it is dispatching on. A class the
		// module declares is named by its own record; **a platform class has no
		// record**, and its class object stands in — the type check is handed
		// this word, and a zero there is a check with nothing to resolve.
		identity := class.Handle
		if identity == 0 {
			identity = class.Object
		}
		if err := client.writeWord(table, identity); err != nil {
			return err
		}
		class.VTable = table
	}
	inherited := uint32(0)
	if class.Super != nil {
		inherited = class.Super.Slots
		for slot := uint32(0); slot < inherited && slot < slots; slot++ {
			entry, err := client.readWord(class.Super.VTable + 4 + slot*4)
			if err != nil {
				return err
			}
			// A record that carries its own vtable has the compiler's answer
			// for the slots it overrides; only its zeroes are the platform's.
			if class.Record.VTable != 0 {
				own, err := client.readWord(class.VTable + 4 + slot*4)
				if err != nil {
					return err
				}
				if own != 0 {
					continue
				}
			}
			if err := client.writeWord(class.VTable+4+slot*4, entry); err != nil {
				return err
			}
		}
	}
	// Every slot the class does not fill answers with a stub that names it.
	for slot := inherited; slot < slots; slot++ {
		if class.Record.VTable != 0 {
			own, err := client.readWord(class.VTable + 4 + slot*4)
			if err != nil {
				return err
			}
			if own != 0 {
				continue
			}
		}
		stub, err := client.stub(svcCategoryJava, javaVirtualSlot(class.Name, slot))
		if err != nil {
			return err
		}
		if err := client.writeWord(class.VTable+4+slot*4, stub); err != nil {
			return err
		}
	}
	if class.Record.VTable != 0 || client.javaLink == nil || client.javaLink.layout == nil {
		return nil
	}
	// The class's own methods, at the slots the layout gave them.
	//
	// **An override's slot is often not in the class's own run.** A run holds
	// the methods whose slot the module needs answering, and a class that only
	// overrides what its superclass already declared needs none: the compiler
	// dispatches such a call through the *superclass's* slot, and the number is
	// already in the out array of that class. One local title's launcher
	// subclass has an empty run and four overrides, two of them of abstract
	// methods, so a lookup in the class's own map alone left the superclass's
	// abstract stubs in place and the title entered one.
	layout := client.javaLink.layout
	for _, method := range class.Record.Methods {
		slot, ok := layout.findVirtual(class.Name, method.Name+method.Descriptor)
		if !ok || method.Body == 0 || slot >= slots {
			continue
		}
		if err := client.writeWord(class.VTable+4+slot*4, method.Body); err != nil {
			return err
		}
	}
	return nil
}

// allocateJavaClassObject builds the object the resolve call answers.
func (client *Client) allocateJavaClassObject(class *javaRuntimeClass) (uint32, error) {
	name, err := client.allocateBytes(append([]byte(class.Name), 0))
	if err != nil {
		return 0, err
	}
	// **A class's static fields live in its class object's data block**, after
	// the words the class object itself uses, and the module's own record says
	// how many: a store at `data + 5 + slot` is what a `putstatic` compiles to.
	// The compiler bakes most of those offsets, so a block sized to the class
	// object's own use is a block the guest writes past — which is exactly what
	// overwrote a class object in two local titles, at an address the arena had
	// handed out for something else. See docs/lgt.md.
	data := make([]uint32, javaClassDataWords+class.StaticWords)
	data[javaClassNameWord] = name
	block, err := client.allocateWords(data)
	if err != nil {
		return 0, err
	}
	return client.allocateWords([]uint32{0, 0, block})
}

// allocateJavaObject builds an instance: the object, and the block its fields
// live in.
func (client *Client) allocateJavaObject(class *javaRuntimeClass) (uint32, error) {
	words := class.Instance
	if class.Handle == 0 && !class.Measured && words < javaUnmeasuredInstanceWords {
		// A platform class the application never extends, whose size nothing
		// says. **The compiler baked its field offsets** the same way it baked
		// its vtable slots, so the guest writes into an instance at offsets this
		// platform never chose, and a block sized to what is known is a block
		// the guest writes past — into whatever the arena handed out next. That
		// is a corruption with no symptom at the write and a fault far away
		// from it: one local title overwrote a class record's data pointer and
		// failed several calls later, resolving a class that had been fine.
		words = javaUnmeasuredInstanceWords
	}
	if words == 0 {
		words = 1
	}
	block, err := client.allocate(uint64(words) * 4)
	if err != nil {
		return 0, err
	}
	if err := client.core.Memory().Write(block, make([]byte, words*4)); err != nil {
		return 0, err
	}
	return client.allocateWords([]uint32{class.VTable, 0, block})
}

// initializeJavaClass runs a class's static initialiser, once. The module gates
// the call on the state halfword of the class object, so writing it before the
// initialiser runs is what keeps an initialiser that reaches back into its own
// class out of an endless recursion — the same order a real runtime uses.
func (client *Client) initializeJavaClass(
	ctx context.Context, thread *armcore.Thread, object, initializer uint32,
) error {
	runtime := client.javaRuntimeState()
	class, ok := runtime.byObject[object]
	if !ok {
		return fmt.Errorf("the class object %#x was not issued here", object)
	}
	if class.initialized {
		return nil
	}
	class.initialized = true
	block, err := client.readWord(object + 8)
	if err != nil {
		return err
	}
	if err := client.writeHalfword(block+javaClassStateWord*4, javaClassInitedFlag); err != nil {
		return err
	}
	if initializer == 0 {
		return nil
	}
	if _, err := client.callOn(ctx, thread, initializer, nil); err != nil {
		return fmt.Errorf("run %s's static initialiser at %#x: %w", class.Name, initializer, err)
	}
	return nil
}

// allocateJavaInstance answers the allocation call: an object of the class it
// is handed, which is the class object the resolve call answered.
func (client *Client) allocateJavaInstance(object uint32) (uint32, error) {
	runtime := client.javaRuntimeState()
	class, ok := runtime.byObject[object]
	if !ok {
		return 0, fmt.Errorf("the class object %#x was not issued here", object)
	}
	return client.allocateJavaObject(class)
}

// takeJavaStringConstant builds one of the title's string constants and stores
// it where the module's own helper says: the call carries the units, the length
// and the slot of the module's cache, and the cache is what makes the constant
// the same object every time it is asked for.
func (client *Client) takeJavaStringConstant(units, length, slot uint32) (uint32, error) {
	// **The call answers the string**, and the module's own helper for a
	// constant is nothing but this call: it looks the pool entry up, passes the
	// units, the length and the cache slot, and returns what came back. A zero
	// there is what made three constants append as "null" and a resource name
	// come out as "nullnullnull".
	if slot != 0 {
		if cached, err := client.readWord(slot); err == nil && cached != 0 {
			if _, ok := client.javaText(cached); ok {
				return cached, nil
			}
		}
	}
	text, err := client.readJavaUTF16(units, length)
	if err != nil {
		return 0, err
	}
	object, err := client.newJavaString(text)
	if err != nil {
		return 0, err
	}
	// A constant is built by the code that holds it, so the first build of one
	// is "the title reached the code that names this". It is how a question
	// like "did pressing the send key take the title down its save path" gets
	// an answer without a disassembly: the filename appears here, or the
	// branch never ran. Only the first build is reported, because a title that
	// rebuilds one every frame would otherwise bury every other answer.
	if runtime := client.javaRun; runtime != nil && runtime.named != nil && !runtime.named[text] {
		runtime.named[text] = true
		if client.logger != nil {
			client.logger.Debug("LGT java names", "text", text)
		}
	}
	if slot != 0 {
		if err := client.writeWord(slot, object); err != nil {
			return 0, err
		}
	}
	return object, nil
}

// javaVirtualSlot names an unimplemented vtable slot as an SVC slot of its own,
// so a call through one reports the class and the slot rather than branching
// into a zero.
func javaVirtualSlot(name string, slot uint32) uint32 {
	return javaSlotVirtual | uint32(javaNameTag(name))<<12 | slot&0xfff
}

// javaNameTag folds a class name into the bits a slot number has room for. The
// name itself is recovered from the runtime's own table when the slot is
// reported; the tag only has to tell two classes apart well enough to look one
// up, and a collision costs a wrong name in a message.
func javaNameTag(name string) uint16 {
	tag := uint16(0)
	for _, symbol := range []byte(name) {
		tag = tag*31 + uint16(symbol)
	}
	return tag & 0x1fff
}

func (client *Client) writeHalfword(address uint32, value uint16) error {
	data := make([]byte, 2)
	binary.LittleEndian.PutUint16(data, value)
	return client.core.Memory().Write(address, data)
}

// javaVirtualSlotParts answers the class and slot a dispatch stub stands for.
func (runtime *javaRuntime) javaVirtualSlotParts(slot uint32) (string, uint32, bool) {
	tag := uint16(slot >> 12 & 0x1fff)
	for name := range runtime.byName {
		if javaNameTag(name) == tag {
			return name, slot & 0xfff, true
		}
	}
	return "", slot & 0xfff, false
}

// describeJavaVirtualSlot names the slot a dispatch stub stands for.
func (client *Client) describeJavaVirtualSlot(slot uint32) string {
	runtime := client.javaRuntimeState()
	name, index, known := runtime.javaVirtualSlotParts(slot)
	if !known {
		return fmt.Sprintf("vtable slot %d", index)
	}
	if _, member, ok := client.javaVirtualMember(slot); ok {
		return fmt.Sprintf("%s.%s (vtable slot %d)", name, member, index)
	}
	// A class the module lists no virtual methods for: the compiler numbered
	// this slot against a platform header, so the number is all there is.
	return fmt.Sprintf("%s vtable slot %d of %d", name, index, runtime.byName[name].Slots)
}

// javaGetClass answers the class object of the object it is handed, which is
// what `Object.getClass` is for. The answer is dispatched on straight away, so
// it is given the vtable of `java/lang/Class` here rather than at the point it
// was built: a class object is allocated while the class it stands for is being
// laid out, and `java/lang/Class` is not necessarily one of the classes laid
// out yet.
func javaGetClass(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	class, ok := client.javaClassOfObject(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x was not issued here", arguments[0])
	}
	meta, err := client.preparePlatformJavaClass(javaClassClass)
	if err != nil {
		return 0, err
	}
	if err := client.writeWord(class.Object, meta.VTable); err != nil {
		return 0, err
	}
	return class.Object, nil
}
