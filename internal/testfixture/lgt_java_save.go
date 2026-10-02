package testfixture

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// The AOT Java save fixture is the Java half of the pair lgt_save.go opens: a
// title whose application code is Java compiled to ARM, which reaches its save
// through the platform's Java classes instead of the WIPI C file calls. It
// keeps the Clet's words and keys, so the contract there is this one's too.
//
// A Java title of this platform has three ways to a save, and they do not hold
// a pending write in the same place — or at all. LGTJavaSaveSurface chooses
// which one the keys act on; a test writes it the way it writes the progress
// word.
const (
	// LGTJavaSaveSurface is one of the three surfaces below. It is read when a
	// key arrives.
	LGTJavaSaveSurface = lgtDataBase + 0x170
	// LGTJavaSaveHeldStream is the output stream kept open on the File that
	// LGTSaveHeld names, and zero when the File is held without one.
	LGTJavaSaveHeldStream = lgtDataBase + 0x174
	// LGTJavaSaveHeldDatabase is the DataBase a hold on the record store left
	// open, and zero when none is.
	LGTJavaSaveHeldDatabase = lgtDataBase + 0x178

	lgtJavaByteArray = lgtDataBase + 0x17c // the `byte[]` type, once resolved
	lgtJavaTables    = lgtDataBase + 0x180 // everything the module lays out for itself

	// LGTJavaSaveSurfaceFile is `org/kwis/msp/io/File` read and written
	// directly. It is the Clet's surface under its Java name: one filesystem,
	// one set of open handles, and a write waits in the handle's buffer until
	// the File is closed.
	LGTJavaSaveSurfaceFile uint32 = 0
	// LGTJavaSaveSurfaceStream is the same File written through the stream
	// `File.openOutputStream` answers and read through `File.openInputStream`.
	// A write waits one step further back: in the stream, until a flush or the
	// File's close moves it into the handle's buffer.
	LGTJavaSaveSurfaceStream uint32 = 1
	// LGTJavaSaveSurfaceDatabase is `org/kwis/msp/db/DataBase`, where the two
	// parts are records 0 and 1. Every change is stored as it is made, so
	// nothing waits anywhere; what a hold keeps is the open DataBase.
	LGTJavaSaveSurfaceDatabase uint32 = 2

	// LGTJavaSaveDatabaseName is the record store as the title names it, and
	// LGTJavaSaveDatabaseKey the save store's key for its container. The File
	// surfaces use LGTSaveFileName and LGTSaveFileKey.
	LGTJavaSaveDatabaseName = "save"
	LGTJavaSaveDatabaseKey  = "fs/" + LGTJavaSaveDatabaseName + ".db"

	// LGTJavaSaveOwner is the Java title's own save owner.
	LGTJavaSaveOwner = "PF0000JS"

	// LGTJavaSaveKeyFlush flushes the stream LGTJavaSaveHeldStream names: what
	// was written to it moves into the File's buffer, and still not into the
	// store.
	LGTJavaSaveKeyFlush int32 = '0'
)

// The tables a Java module resolves from, and the functions of each this one
// uses. The Java table has no names of its own; these say what each call does.
const (
	lgtImportTableStdlib uint32 = 0x1
	lgtImportTableJava   uint32 = 0x64

	lgtJavaStringConstant uint32 = 0x09 // (group, UTF-16 units, length, cache) -> String
	lgtJavaPrepareClass   uint32 = 0x0b // (class record) -> class
	lgtJavaArrayType      uint32 = 0x0e // (dimensions, element class, element code) -> class
	lgtJavaAllocate       uint32 = 0x0f // (class) -> instance
	lgtJavaAllocateArray  uint32 = 0x10 // (array class, length) -> array
	lgtJavaLoadClasses    uint32 = 0x14 // the platform classes the module links against
	lgtJavaEnterTry       uint32 = 0x1f // () -> the buffer setjmp is handed
	lgtJavaLeaveTry       uint32 = 0x20
	lgtJavaLauncher       uint32 = 0x83 // (entry class, 0, argc, argv)
	lgtStdlibSetJump      uint32 = 0x32

	// The element code of `byte`, and the slots of the two stream classes the
	// module lists no methods for: the compiler numbers those itself.
	lgtJavaByteCode        uint32 = 8
	lgtJavaStreamWrite     uint32 = 11 // OutputStream.write(byte[])
	lgtJavaStreamFlush     uint32 = 13 // OutputStream.flush()
	lgtJavaStreamRead      uint32 = 11 // InputStream.read(byte[])
	lgtJavaFileTruncate    uint32 = 3
	lgtJavaFileReadWrite   uint32 = 4
	lgtJavaFileReadOnly    uint32 = 1
	lgtJavaKeyPressed      uint32 = 1
	lgtJavaClassHeaderSize uint32 = 0x4c
)

const (
	lgtJavaGlobalLoadClasses = iota
	lgtJavaGlobalLauncher
	lgtJavaGlobalStringConstant
	lgtJavaGlobalPrepareClass
	lgtJavaGlobalAllocate
	lgtJavaGlobalArrayType
	lgtJavaGlobalAllocateArray
	lgtJavaGlobalEnterTry
	lgtJavaGlobalLeaveTry
	lgtJavaGlobalSetJump
)

// lgtJavaAPI is one platform class the module links against: the static
// methods it calls through the address table the platform fills in, and the
// virtual methods whose slot it asks the platform for.
type lgtJavaAPI struct {
	class    string
	statics  []string
	virtuals []string
}

var lgtJavaSaveAPI = []lgtJavaAPI{
	{"org/kwis/msp/lcdui/Jlet", []string{"<init>()V"}, nil},
	{"org/kwis/msp/lcdui/Card", []string{"<init>()V"}, []string{"repaint()V"}},
	{"org/kwis/msp/lcdui/Display",
		[]string{"getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;"},
		[]string{"pushCard(Lorg/kwis/msp/lcdui/Card;)V"}},
	{"org/kwis/msp/lcdui/Graphics", nil, []string{"setColor(I)V", "setPixel(II)V"}},
	{"org/kwis/msp/io/File",
		[]string{"<init>(Ljava/lang/String;I)V"},
		[]string{"sizeOf()I", "read([B)I", "write([B)I", "close()V",
			"openOutputStream()Ljava/io/OutputStream;", "openInputStream()Ljava/io/InputStream;"}},
	{"org/kwis/msp/io/FileSystem",
		[]string{"exists(Ljava/lang/String;)Z", "remove(Ljava/lang/String;)V"}, nil},
	{"org/kwis/msp/db/DataBase",
		[]string{"openDataBase(Ljava/lang/String;IZ)Lorg/kwis/msp/db/DataBase;", "deleteDataBase(Ljava/lang/String;)V"},
		[]string{"closeDataBase()V", "getNumberOfRecords()I", "insertRecord([B)I", "updateRecord(I[B)V", "selectRecord(I)[B"}},
}

// lgtJavaLinkage is where the platform's answers to the load call land, named
// the way the code below asks for them: "File.write", "DataBase.openDataBase".
type lgtJavaLinkage struct {
	// statics is the address of the word a static method's entry point is
	// written to, virtuals the address of the halfword a virtual method's slot
	// is written to, and classes the word that answers the class itself.
	statics, virtuals, classes map[string]uint32
	// load is the load call's eleven arguments: the class table, the five
	// member tables, and the five arrays the answers go to.
	load [11]uint32
}

func lgtJavaMemberKey(class, member string) string {
	name, _, _ := strings.Cut(member, "(")
	return class[strings.LastIndexByte(class, '/')+1:] + "." + name
}

// lgtJavaLink lays the platform class table out and says where each answer
// will be.
//
// **The five member tables are one run of memory and the class table follows
// them.** A table carries no length: it ends where the next one starts, so the
// order here is the layout, and each has at least one entry so that no two
// start at the same address. Every class's static run opens with the two
// unnamed entries that answer the class itself.
func lgtJavaLink(data *lgtData, names func(string) uint32) lgtJavaLinkage {
	link := lgtJavaLinkage{statics: map[string]uint32{}, virtuals: map[string]uint32{}, classes: map[string]uint32{}}
	type member struct{ name, descriptor string }
	split := func(text string) member {
		name, descriptor, _ := strings.Cut(text, "(")
		return member{name, "(" + descriptor}
	}
	var virtuals, statics []member
	type run struct{ virtual, static uint32 }
	runs := make([]run, len(lgtJavaSaveAPI))
	for index, api := range lgtJavaSaveAPI {
		runs[index].virtual = uint32(len(api.virtuals))<<16 | uint32(len(virtuals))
		for _, text := range api.virtuals {
			virtuals = append(virtuals, split(text))
		}
		runs[index].static = uint32(len(api.statics)+2)<<16 | uint32(len(statics))
		statics = append(statics, member{}, member{})
		for _, text := range api.statics {
			statics = append(statics, split(text))
		}
	}

	fields := data.reserve(8)
	staticFields := data.reserve(8)
	virtualTable := data.reserve(uint32(len(virtuals)) * 8)
	methods := data.reserve(8)
	staticTable := data.reserve(uint32(len(statics)) * 8)
	classes := data.reserve(4 + uint32(len(lgtJavaSaveAPI))*24)
	fieldsOut := data.reserve(2)
	staticFieldsOut := data.reserve(2)
	virtualOut := data.reserve(uint32(len(virtuals)) * 2)
	methodsOut := data.reserve(2)
	staticOut := data.reserve(uint32(len(statics)) * 4)
	link.load = [11]uint32{classes, fields, staticFields, virtualTable, methods, staticTable,
		fieldsOut, staticFieldsOut, virtualOut, methodsOut, staticOut}

	place := func(table uint32, members []member) {
		for index, entry := range members {
			if entry.name != "" {
				data.words(table+uint32(index)*8, names(entry.name), names(entry.descriptor))
			}
		}
	}
	place(virtualTable, virtuals)
	place(staticTable, statics)
	data.words(classes, uint32(len(lgtJavaSaveAPI)))
	for index, api := range lgtJavaSaveAPI {
		data.words(classes+4+uint32(index)*24, names(api.class), 0, 0, runs[index].virtual, 0, runs[index].static)
		virtual, static := runs[index].virtual&0xffff, runs[index].static&0xffff
		for offset, text := range api.virtuals {
			link.virtuals[lgtJavaMemberKey(api.class, text)] = virtualOut + (virtual+uint32(offset))*2
		}
		for offset, text := range api.statics {
			link.statics[lgtJavaMemberKey(api.class, text)] = staticOut + (static+2+uint32(offset))*4
		}
		link.classes[lgtJavaMemberKey(api.class, "")] = staticOut + (static+1)*4
	}
	return link
}

// lgtJavaMethod is one method an application class declares: its name, its
// descriptor and where its compiled body starts.
type lgtJavaMethod struct {
	name, descriptor string
	body             uint32
}

// lgtJavaClassRecord lays out one application class the way the module's
// compiler does: a header, the handle the rest of the module names the class
// by, and the methods it declares, each with its own entry point. The class
// extends a platform class, so it carries no dispatch table of its own and the
// platform builds one. It answers the handle.
func lgtJavaClassRecord(data *lgtData, names func(string) uint32, name, super string, instance, slots uint32, methods []lgtJavaMethod) uint32 {
	header := data.reserve(lgtJavaClassHeaderSize + 12 + 4 + uint32(len(methods))*28)
	handle := header + lgtJavaClassHeaderSize
	data.words(header+0x00, 0x21)
	data.words(header+0x08, names(name))
	data.words(header+0x10, names(super))
	data.words(header+0x18, instance|2<<16) // the size in words, and "loaded"
	data.words(header+0x24, slots<<16|slots)
	data.words(header+0x38, handle+12) // the method run: a count, then the records
	data.words(header+0x44, 0xfffffffe)
	data.words(handle, 0, 0, header, uint32(len(methods)))
	for index, method := range methods {
		data.words(handle+16+uint32(index)*28,
			handle, names(method.name), names(method.descriptor), 1, uint32(index), method.body, 0)
	}
	return handle
}

// LGTJavaSaveArchive is an AOT Java title with a save: an app_info/JAR/module
// archive that starts through the ordinary loader, where the module hands the
// platform its class table, asks for the launcher, and is entered through the
// Jlet the launcher builds. The Jlet pushes a Card; the Card's `paint` counts
// a frame, draws the count and asks to be painted again, and its `keyNotify`
// acts on the keys.
//
// What each action is to the platform, by surface:
//
//	          File                          File through streams           DataBase
//	save      File(truncate), write of      File(truncate), stream,        openDataBase(create), both
//	          eight bytes, close            write of eight bytes, close    records put, closeDataBase
//	patch     File(read-write), write of    File(read-write), stream,      openDataBase(create), record 0
//	          four bytes, close             write of four bytes, close     put, closeDataBase
//	read      exists, File(read-only),      the same, read through         openDataBase in a try region,
//	          sizeOf, read, close           openInputStream                both records selected, close
//	delete    FileSystem.remove             FileSystem.remove              deleteDataBase in a try region
//	hold      File(read-write), write of    File(read-write), stream,      openDataBase(create), record 0
//	          part A, and no close          write of part A, no flush      put, and no close
//	truncate  File(truncate), no close      File(truncate), stream         nothing: a record store has
//	                                                                       no open that truncates
//	finish    write of part B through what was kept — the stream when     record 1 put through the
//	          there is one — and File.close                                DataBase that was kept, close
//	flush     OutputStream.flush on the stream that was kept
//
// A record is "put" by updateRecord when the store already has one under that
// number and by insertRecord when it has not.
//
// **Where a pending write is, and where it is not.** A File's write goes to
// the same buffer a Clet's does — the platform client's table of open handles,
// `files`, with the entry's `dirty` set — and LGTSaveHeld names the File object
// the Java runtime's own `files` table maps to that handle. A stream's write
// goes to the Java runtime's `sinks` table instead, under the stream object
// LGTJavaSaveHeldStream names and bound to its File through `sinkFiles`; the
// handle is untouched and clean until a flush or the File's close drains the
// stream into it. A DataBase holds nothing back: every put rewrites the stored
// container, and what the Java runtime's `databases` table keeps under
// LGTJavaSaveHeldDatabase is the open object and its copy of the records.
//
// The File surfaces share one kept File, and the DataBase has one of its own;
// a second hold or truncate while one is kept does nothing.
func LGTJavaSaveArchive() ([]byte, error) {
	data := &lgtData{bytes: make([]byte, lgtJavaTables-lgtDataBase), next: lgtJavaTables}
	// A name is placed once, wherever the section has room when it is first
	// asked for.
	pool := map[string]uint32{}
	names := func(value string) uint32 {
		if address, placed := pool[value]; placed {
			return address
		}
		pool[value] = data.text(value)
		return pool[value]
	}
	link := lgtJavaLink(data, names)

	// A string constant is UTF-16 units in the image and one word the platform
	// caches the String it builds in.
	constant := func(text string) (units, length, cache uint32) {
		units = data.reserve(uint32(len(text)) * 2)
		for index, symbol := range []byte(text) {
			binary.LittleEndian.PutUint16(data.bytes[units-lgtDataBase+uint32(index)*2:], uint16(symbol))
		}
		return units, uint32(len(text)), data.reserve(4)
	}
	fileUnits, fileLength, fileCache := constant(LGTSaveFileName)
	databaseUnits, databaseLength, databaseCache := constant(LGTJavaSaveDatabaseName)

	p := newLGTProgram(lgtTextBase)
	global := func(index int) { p.through(lgtGlobals + uint32(index)*4) }
	static := func(name string) {
		if link.statics[name] == 0 {
			p.fail("no static method %s", name)
		}
		p.through(link.statics[name])
	}
	// class leaves a platform class in r0, the way a `new` asks for one.
	class := func(name string) {
		if link.classes[name+"."] == 0 {
			p.fail("no class %s", name)
		}
		p.through(link.classes[name+"."])
	}
	// virtual dispatches on the object in r0 through the slot the platform
	// answered, and baked through a slot the compiler numbered itself.
	virtual := func(name string) {
		if link.virtuals[name] == 0 {
			p.fail("no virtual method %s", name)
		}
		p.literal(lgtIP, link.virtuals[name])
		p.emit(lgtLdrsh(lgtIP, lgtIP), lgtLdr(lgtLR, 0, 0), lgtAddShift(lgtIP, lgtLR, lgtIP, 2),
			lgtLdr(lgtIP, lgtIP, 4), lgtMovLRPC, lgtBX(lgtIP))
	}
	baked := func(slot uint32) {
		p.emit(lgtLdr(lgtIP, 0, 0), lgtLdr(lgtIP, lgtIP, 4+slot*4), lgtMovLRPC, lgtBX(lgtIP))
	}
	increment := func(address uint32) {
		p.literal(4, address)
		p.emit(lgtLdr(0, 4, 0), lgtAddImm(0, 0, 1), lgtStr(0, 4, 0))
	}
	// try opens a region: a throw from inside it comes back to the branch.
	try := func(handler string) {
		global(lgtJavaGlobalEnterTry)
		global(lgtJavaGlobalSetJump)
		p.emit(lgtCmpImm(0, 0))
		p.branch(lgtNE, handler)
	}
	leave := func() { global(lgtJavaGlobalLeaveTry) }

	// entry(param1, param2): resolve every platform function, then hand the
	// platform the init struct. See lgtCletBase.
	entry := p.here()
	p.emit(0x46c04778)
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0))
	p.emit(lgtLdr(5, 1, 4))
	p.literal(6, lgtImportRequests)
	p.literal(7, lgtGlobals)
	p.label("resolve")
	p.emit(lgtLdrPost(0, 6, 4), lgtCmpImm(0, 0))
	p.branch(lgtEQ, "resolved")
	p.emit(lgtLdrPost(1, 6, 4), lgtMovLRPC, lgtBX(5))
	p.emit(lgtStrPost(0, 7, 4))
	p.branch(lgtAL, "resolve")
	p.label("resolved")
	p.literal(0, lgtInitStruct)
	p.emit(lgtStr(0, 4, 512+20))
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	// init(): hand over the platform class table, then ask for the launcher.
	// The load call takes eleven arguments, four in registers and seven on the
	// stack. The launcher is the application's whole start: it builds the Jlet
	// argv names and enters its startApp before it comes back.
	initialise := p.here()
	p.emit(lgtPushLR)
	p.emit(lgtSubImm(lgtSP, lgtSP, 28))
	for index := 4; index < 11; index++ {
		p.literal(0, link.load[index])
		p.emit(lgtStr(0, lgtSP, uint32(index-4)*4))
	}
	for index := uint32(0); index < 4; index++ {
		p.literal(index, link.load[index])
	}
	global(lgtJavaGlobalLoadClasses)
	p.emit(lgtAddImm(lgtSP, lgtSP, 28))
	argv := data.reserve(16)
	data.words(argv, names("Save"), names(""), names("true"), names("true"))
	p.literal(0, names("org/kwis/msp/lcdui/Main"))
	p.emit(lgtMovImm(1, 0), lgtMovImm(2, 4))
	p.literal(3, argv)
	global(lgtJavaGlobalLauncher)
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	// Save.<init>(): the Jlet's own constructor first, as the language has it.
	jletInit := p.here()
	p.emit(lgtPushLR)
	static("Jlet.<init>")
	p.emit(lgtPopPC)

	// Save.startApp(String[]): count the start, build the card and push it.
	cardHandle := data.reserve(4) // patched below, once the record is laid out
	startApp := p.here()
	p.emit(lgtPushLR)
	increment(LGTCheckpointStartupCounter)
	p.literal(0, cardHandle)
	p.emit(lgtLdr(0, 0, 0))
	global(lgtJavaGlobalPrepareClass)
	global(lgtJavaGlobalAllocate)
	p.emit(lgtMovReg(5, 0))
	static("Card.<init>")
	static("Display.getDefaultDisplay")
	p.emit(lgtMovReg(1, 5))
	virtual("Display.pushCard")
	p.emit(lgtPopPC)
	p.pool()

	// SaveCard.paint(Graphics): count the frame, draw the count as the colour
	// of the first pixel, and ask for the next frame.
	paint := p.here()
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(5, 0), lgtMovReg(6, 1))
	increment(LGTCheckpointFrameCounter)
	p.emit(lgtLslImm(1, 0, 3), lgtMovReg(0, 6))
	virtual("Graphics.setColor")
	p.emit(lgtMovReg(0, 6), lgtMovImm(1, 0), lgtMovImm(2, 0))
	virtual("Graphics.setPixel")
	p.emit(lgtMovReg(0, 5))
	virtual("Card.repaint")
	p.emit(lgtPopPC)
	p.pool()

	// bytes(length) -> a new byte[length].
	p.label("bytes")
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0))
	p.literal(5, lgtJavaByteArray)
	p.emit(lgtLdr(0, 5, 0), lgtCmpImm(0, 0))
	p.branch(lgtNE, "bytes typed")
	p.emit(lgtMovImm(0, 1), lgtMovImm(1, 0), lgtMovImm(2, lgtJavaByteCode))
	global(lgtJavaGlobalArrayType)
	p.emit(lgtStr(0, 5, 0))
	p.label("bytes typed")
	p.emit(lgtMovReg(1, 4))
	global(lgtJavaGlobalAllocateArray)
	p.emit(lgtPopPC)
	p.pool()

	// part(which) -> a byte[4] holding part A for zero and part B otherwise.
	// An array's elements start one word into the block its third word names.
	p.label("part")
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0))
	p.emit(lgtMovImm(0, 4))
	p.call("bytes")
	p.emit(lgtLdr(1, 0, 8))
	p.literal(3, LGTSaveProgress)
	p.emit(lgtLdr(2, 3, 0), lgtCmpImm(4, 0))
	p.branch(lgtEQ, "part stored")
	p.literal(3, LGTSaveXOR)
	p.emit(lgtEorReg(2, 2, 3))
	p.label("part stored")
	p.emit(lgtStr(2, 1, 4), lgtPopPC)
	p.pool()

	// both() -> a byte[8] holding part A and then part B.
	p.label("both")
	p.emit(lgtPushLR)
	p.emit(lgtMovImm(0, 8))
	p.call("bytes")
	p.emit(lgtLdr(1, 0, 8))
	p.literal(3, LGTSaveProgress)
	p.emit(lgtLdr(2, 3, 0), lgtStr(2, 1, 4))
	p.literal(3, LGTSaveXOR)
	p.emit(lgtEorReg(2, 2, 3), lgtStr(2, 1, 8), lgtPopPC)
	p.pool()

	// fileName() and databaseName() -> the String constant.
	for _, text := range []struct {
		label                string
		units, length, cache uint32
	}{{"file name", fileUnits, fileLength, fileCache}, {"database name", databaseUnits, databaseLength, databaseCache}} {
		p.label(text.label)
		p.emit(lgtPushLR)
		p.emit(lgtMovImm(0, 0))
		p.literal(1, text.units)
		p.emit(lgtMovImm(2, text.length))
		p.literal(3, text.cache)
		global(lgtJavaGlobalStringConstant)
		p.emit(lgtPopPC)
		p.pool()
	}

	// openFile(mode) -> new File(name, mode).
	p.label("open file")
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0))
	class("File")
	global(lgtJavaGlobalAllocate)
	p.emit(lgtMovReg(5, 0))
	p.call("file name")
	p.emit(lgtMovReg(1, 0), lgtMovReg(0, 5), lgtMovReg(2, 4))
	static("File.<init>")
	p.emit(lgtMovReg(0, 5), lgtPopPC)
	p.pool()

	// streamOf(file) -> file.openOutputStream() on the stream surface, and
	// null on the other.
	p.label("stream of")
	p.emit(lgtPushLR)
	p.literal(3, LGTJavaSaveSurface)
	p.emit(lgtLdr(3, 3, 0), lgtCmpImm(3, LGTJavaSaveSurfaceStream))
	p.branch(lgtEQ, "stream opens")
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.label("stream opens")
	virtual("File.openOutputStream")
	p.emit(lgtPopPC)
	p.pool()

	// write(file, stream, bytes): stream.write(bytes) when there is a stream,
	// file.write(bytes) when there is not.
	p.label("write")
	p.emit(lgtPushLR)
	p.emit(lgtCmpImm(1, 0))
	p.branch(lgtEQ, "write direct")
	p.emit(lgtMovReg(0, 1), lgtMovReg(1, 2))
	baked(lgtJavaStreamWrite)
	p.emit(lgtPopPC)
	p.label("write direct")
	p.emit(lgtMovReg(1, 2))
	virtual("File.write")
	p.emit(lgtPopPC)
	p.pool()

	// openDatabase(create) -> DataBase.openDataBase(name, 4, create), which
	// throws when there is none and none was asked for.
	p.label("open database")
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0))
	p.call("database name")
	p.emit(lgtMovImm(1, 4), lgtMovReg(2, 4))
	static("DataBase.openDataBase")
	p.emit(lgtPopPC)
	p.pool()

	// put(database, record, bytes): updateRecord for a record the store has,
	// insertRecord for one it has not.
	p.label("put")
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0), lgtMovReg(5, 1), lgtMovReg(6, 2))
	virtual("DataBase.getNumberOfRecords")
	p.emit(lgtCmpReg(5, 0))
	p.branch(lgtLO, "put updates")
	p.emit(lgtMovReg(0, 4), lgtMovReg(1, 6))
	virtual("DataBase.insertRecord")
	p.emit(lgtPopPC)
	p.label("put updates")
	p.emit(lgtMovReg(0, 4), lgtMovReg(1, 5), lgtMovReg(2, 6))
	virtual("DataBase.updateRecord")
	p.emit(lgtPopPC)
	p.pool()

	// putPart(database, record): put the part that record holds.
	p.label("put part")
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0), lgtMovReg(5, 1))
	p.emit(lgtMovReg(0, 1))
	p.call("part")
	p.emit(lgtMovReg(2, 0), lgtMovReg(0, 4), lgtMovReg(1, 5))
	p.call("put")
	p.emit(lgtPopPC)

	// SaveCard.keyNotify(type, key): remember what arrived, and act on a key
	// press. r4 is the surface for the whole of it.
	keyNotify := p.here()
	p.emit(lgtPushAll)
	p.literal(3, LGTCheckpointLastEvent)
	p.emit(lgtStr(1, 3, 0), lgtStr(2, 3, 4))
	p.emit(lgtCmpImm(1, lgtJavaKeyPressed))
	p.branch(lgtNE, "done")
	p.literal(3, LGTJavaSaveSurface)
	p.emit(lgtLdr(4, 3, 0))
	for _, action := range []struct {
		key  int32
		name string
	}{
		{LGTSaveKeySave, "save"}, {LGTSaveKeyPatch, "patch"}, {LGTSaveKeyRead, "read"},
		{LGTSaveKeyDelete, "delete"}, {LGTSaveKeyHold, "hold"}, {LGTSaveKeyTruncate, "truncate"},
		{LGTSaveKeyFinish, "finish"}, {LGTJavaSaveKeyFlush, "flush"},
	} {
		p.emit(lgtCmpImm(2, uint32(action.key)))
		p.branch(lgtEQ, action.name)
	}
	p.label("done")
	p.emit(lgtMovImm(0, 1), lgtPopAll)
	p.pool()

	// The File actions open the File into r5 and its stream, or null, into r6.
	opened := func(mode uint32) {
		p.emit(lgtMovImm(0, mode))
		p.call("open file")
		p.emit(lgtMovReg(5, 0))
		p.call("stream of")
		p.emit(lgtMovReg(6, 0))
	}
	written := func() {
		p.emit(lgtMovReg(2, 0), lgtMovReg(0, 5), lgtMovReg(1, 6))
		p.call("write")
	}
	closed := func() {
		p.emit(lgtMovReg(0, 5))
		virtual("File.close")
	}
	database := func(label string) {
		p.emit(lgtCmpImm(4, LGTJavaSaveSurfaceDatabase))
		p.branch(lgtEQ, label)
	}

	p.label("save")
	database("save database")
	opened(lgtJavaFileTruncate)
	p.call("both")
	written()
	closed()
	p.branch(lgtAL, "done")
	p.label("save database")
	p.emit(lgtMovImm(0, 1))
	p.call("open database")
	p.emit(lgtMovReg(5, 0), lgtMovImm(1, 0))
	p.call("put part")
	p.emit(lgtMovReg(0, 5), lgtMovImm(1, 1))
	p.call("put part")
	p.label("database closes")
	p.emit(lgtMovReg(0, 5))
	virtual("DataBase.closeDataBase")
	p.branch(lgtAL, "done")
	p.pool()

	p.label("patch")
	database("patch database")
	opened(lgtJavaFileReadWrite)
	p.emit(lgtMovImm(0, 0))
	p.call("part")
	written()
	closed()
	p.branch(lgtAL, "done")
	p.label("patch database")
	p.emit(lgtMovImm(0, 1))
	p.call("open database")
	p.emit(lgtMovReg(5, 0), lgtMovImm(1, 0))
	p.call("put part")
	p.branch(lgtAL, "database closes")
	p.pool()

	// The four words a read answers in are one run: seen A, seen B, status,
	// length. All four are cleared first, and from the moment the save is
	// known to be there the status says "error" until its bytes are in — which
	// is also what it says if the platform throws on the way.
	p.label("read")
	p.literal(7, LGTSaveSeenA)
	p.emit(lgtMovImm(0, 0), lgtStr(0, 7, 0), lgtStr(0, 7, 4), lgtStr(0, 7, 8), lgtStr(0, 7, 12))
	database("read database")
	p.call("file name")
	static("FileSystem.exists")
	p.emit(lgtCmpImm(0, 0))
	p.branch(lgtEQ, "done")
	p.emit(lgtMovImm(0, LGTSaveStatusError), lgtStr(0, 7, 8))
	p.emit(lgtMovImm(0, lgtJavaFileReadOnly))
	p.call("open file")
	p.emit(lgtMovReg(5, 0))
	virtual("File.sizeOf")
	p.emit(lgtStr(0, 7, 12))
	p.emit(lgtMovImm(0, 8))
	p.call("bytes")
	p.emit(lgtMovReg(6, 0))
	p.emit(lgtCmpImm(4, LGTJavaSaveSurfaceStream))
	p.branch(lgtEQ, "read stream")
	p.emit(lgtMovReg(0, 5), lgtMovReg(1, 6))
	virtual("File.read")
	p.branch(lgtAL, "read copies")
	p.label("read stream")
	p.emit(lgtMovReg(0, 5))
	virtual("File.openInputStream")
	p.emit(lgtMovReg(1, 6))
	baked(lgtJavaStreamRead)
	p.label("read copies")
	p.emit(lgtLdr(1, 6, 8), lgtLdr(0, 1, 4), lgtStr(0, 7, 0), lgtLdr(0, 1, 8), lgtStr(0, 7, 4))
	closed()
	p.emit(lgtMovImm(0, LGTSaveStatusFound), lgtStr(0, 7, 8))
	p.branch(lgtAL, "done")
	p.pool()

	// A database that is not there is an exception rather than an answer, so
	// the open is in a try region and the handler leaves "missing" standing.
	p.label("read database")
	try("done")
	p.emit(lgtMovImm(0, 0))
	p.call("open database")
	p.emit(lgtMovReg(5, 0))
	leave()
	p.emit(lgtMovImm(0, LGTSaveStatusError), lgtStr(0, 7, 8))
	p.emit(lgtMovReg(0, 5))
	virtual("DataBase.getNumberOfRecords")
	p.emit(lgtMovReg(6, 0))
	for record := uint32(0); record < 2; record++ {
		p.emit(lgtCmpImm(6, record+1))
		p.branch(lgtLO, "read database closes")
		p.emit(lgtMovReg(0, 5), lgtMovImm(1, record))
		virtual("DataBase.selectRecord")
		p.emit(lgtLdr(1, 0, 8), lgtLdr(2, 1, 0), lgtLdr(3, 1, 4), lgtStr(3, 7, record*4))
		p.emit(lgtLdr(0, 7, 12), lgtAddReg(0, 0, 2), lgtStr(0, 7, 12))
	}
	p.label("read database closes")
	p.emit(lgtMovReg(0, 5))
	virtual("DataBase.closeDataBase")
	p.emit(lgtMovImm(0, LGTSaveStatusFound), lgtStr(0, 7, 8))
	p.branch(lgtAL, "done")
	p.pool()

	p.label("delete")
	database("delete database")
	p.call("file name")
	static("FileSystem.remove")
	p.branch(lgtAL, "done")
	p.label("delete database")
	try("done")
	p.call("database name")
	static("DataBase.deleteDataBase")
	leave()
	p.branch(lgtAL, "done")
	p.pool()

	p.label("hold")
	database("hold database")
	p.literal(7, LGTSaveHeld)
	p.emit(lgtLdr(0, 7, 0), lgtCmpImm(0, 0))
	p.branch(lgtNE, "done")
	opened(lgtJavaFileReadWrite)
	p.emit(lgtMovImm(0, 0))
	p.call("part")
	written()
	p.label("file kept")
	p.literal(3, LGTJavaSaveHeldStream)
	p.emit(lgtStr(5, 7, 0), lgtStr(6, 3, 0))
	p.branch(lgtAL, "done")
	p.label("hold database")
	p.literal(7, LGTJavaSaveHeldDatabase)
	p.emit(lgtLdr(0, 7, 0), lgtCmpImm(0, 0))
	p.branch(lgtNE, "done")
	p.emit(lgtMovImm(0, 1))
	p.call("open database")
	p.emit(lgtMovReg(5, 0), lgtMovImm(1, 0))
	p.call("put part")
	p.emit(lgtStr(5, 7, 0))
	p.branch(lgtAL, "done")
	p.pool()

	p.label("truncate")
	database("done")
	p.literal(7, LGTSaveHeld)
	p.emit(lgtLdr(0, 7, 0), lgtCmpImm(0, 0))
	p.branch(lgtNE, "done")
	opened(lgtJavaFileTruncate)
	p.branch(lgtAL, "file kept")
	p.pool()

	p.label("finish")
	database("finish database")
	p.literal(7, LGTSaveHeld)
	p.emit(lgtLdr(5, 7, 0), lgtCmpImm(5, 0))
	p.branch(lgtEQ, "done")
	p.literal(8, LGTJavaSaveHeldStream)
	p.emit(lgtLdr(6, 8, 0))
	p.emit(lgtMovImm(0, 1))
	p.call("part")
	written()
	closed()
	p.emit(lgtMovImm(0, 0), lgtStr(0, 7, 0), lgtStr(0, 8, 0))
	p.branch(lgtAL, "done")
	p.label("finish database")
	p.literal(7, LGTJavaSaveHeldDatabase)
	p.emit(lgtLdr(5, 7, 0), lgtCmpImm(5, 0))
	p.branch(lgtEQ, "done")
	p.emit(lgtMovReg(0, 5), lgtMovImm(1, 1))
	p.call("put part")
	p.emit(lgtMovImm(0, 0), lgtStr(0, 7, 0))
	p.branch(lgtAL, "database closes")
	p.pool()

	p.label("flush")
	p.literal(3, LGTJavaSaveHeldStream)
	p.emit(lgtLdr(0, 3, 0), lgtCmpImm(0, 0))
	p.branch(lgtEQ, "done")
	baked(lgtJavaStreamFlush)
	p.branch(lgtAL, "done")

	code, err := p.finish()
	if err != nil {
		return nil, err
	}
	if lgtTextBase+uint32(len(code)) > lgtDataBase {
		return nil, fmt.Errorf("LGT fixture module: %d bytes of code reach the data section", len(code))
	}

	// The two application classes. The sizes are the ones a module states for
	// a class that adds nothing to the platform class it extends.
	lgtJavaClassRecord(data, names, "Save", "org/kwis/msp/lcdui/Jlet", 6, 21, []lgtJavaMethod{
		{"<init>", "()V", jletInit},
		{"startApp", "([Ljava/lang/String;)V", startApp},
	})
	data.words(cardHandle, lgtJavaClassRecord(data, names, "SaveCard", "org/kwis/msp/lcdui/Card", 13, 26, []lgtJavaMethod{
		{"paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", paint},
		{"keyNotify", "(II)Z", keyNotify},
	}))
	data.words(lgtInitStruct, 0, initialise, 0)
	data.words(lgtImportRequests,
		lgtImportTableJava, lgtJavaLoadClasses,
		lgtImportTableJava, lgtJavaLauncher,
		lgtImportTableJava, lgtJavaStringConstant,
		lgtImportTableJava, lgtJavaPrepareClass,
		lgtImportTableJava, lgtJavaAllocate,
		lgtImportTableJava, lgtJavaArrayType,
		lgtImportTableJava, lgtJavaAllocateArray,
		lgtImportTableJava, lgtJavaEnterTry,
		lgtImportTableJava, lgtJavaLeaveTry,
		lgtImportTableStdlib, lgtStdlibSetJump,
		0, 0)

	jar, err := fixtureZIP([]fixtureEntry{{"binary.mod", lgtELF(code, data.bytes, entry)}, {"data/packaged.txt", []byte("packaged")}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{
		{"app_info", []byte("AID=0102JSAV\nPID=" + LGTJavaSaveOwner + "\nMClass=Save\nName=Java Save Fixture\n")},
		{"0102JSAV.jar", jar},
	})
}
