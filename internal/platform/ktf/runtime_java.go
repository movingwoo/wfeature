package ktf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/glyph"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const runtimeJletClass = "org/kwis/msp/lcdui/Jlet"

const (
	runtimeAnnunciatorClass = "org/kwis/msp/lwc/AnnunciatorComponent"
	runtimeCardClass        = "org/kwis/msp/lcdui/Card"
	runtimeDisplayClass     = "org/kwis/msp/lcdui/Display"
	runtimeBackLightClass   = "org/kwis/msp/handset/BackLight"
)

type runtimeJavaClass struct {
	name         string
	superName    string
	accessFlags  uint16
	instanceSize uint16
	methods      []runtimeJavaMethod
	fields       []runtimeJavaField
}

// runtimeJavaField describes one guest-visible field record. KTF stores a
// static field's value word inline at record offset 12 and an instance
// field's byte offset in the same slot, so a static field may carry an
// initializer that produces that first value.
type runtimeJavaField struct {
	name        string
	descriptor  string
	accessFlags uint32
	offset      uint32
	initializer func(*initializationRuntime) (uint32, error)
}

type runtimeJavaMethod struct {
	class          string
	name           string
	descriptor     string
	accessFlags    uint16
	implementation runtimeJavaImplementation
}

type runtimeJavaImplementation func(*initializationRuntime, *jvm.VM, []jvm.Value) (jvm.Value, error)

type runtimeJavaInvocation struct {
	method    runtimeJavaMethod
	container bool
}

var runtimeJavaClasses map[string]runtimeJavaClass

// The table is populated in init because static-field initializers reference
// runtime helpers that resolve classes through this same table.
func init() {
	runtimeJavaClasses = map[string]runtimeJavaClass{
		// java/lang/Object publishes KTF metadata for the JVM-owned root class so
		// guest AOT classes extending it can resolve and call the real
		// constructor. A nil implementation delegates to the existing JVM builtin.
		"java/lang/Object": {
			name:        "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Object", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/lang/Object", name: "getClass", descriptor: "()Ljava/lang/Class;", accessFlags: 0x0011},
				{class: "java/lang/Object", name: "hashCode", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/Object", name: "equals", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/lang/Object", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
				// The monitor methods are how a game's worker thread parks
				// until the loader hands it something. Not publishing them
				// here is what turned a perfectly ordinary wait into a dead
				// session at startup. They take KTF implementations rather
				// than the JVM's: see runtimeObjectWait.
				{class: "java/lang/Object", name: "wait", descriptor: "()V", accessFlags: 0x0011, implementation: runtimeObjectWait},
				{class: "java/lang/Object", name: "wait", descriptor: "(J)V", accessFlags: 0x0011, implementation: runtimeObjectWait},
				{class: "java/lang/Object", name: "wait", descriptor: "(JI)V", accessFlags: 0x0011, implementation: runtimeObjectWait},
				{class: "java/lang/Object", name: "notify", descriptor: "()V", accessFlags: 0x0011, implementation: runtimeObjectNotify},
				{class: "java/lang/Object", name: "notifyAll", descriptor: "()V", accessFlags: 0x0011, implementation: runtimeObjectNotify},
			},
		},
		// java/lang/System exposes the JVM-owned builtins through KTF metadata.
		"java/lang/System": {
			name:        "java/lang/System",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				// currentTimeMillis delegates to the JVM builtin, which reads
				// the guest clock through the VM's Clock hook. See LoadClient.
				{class: "java/lang/System", name: "currentTimeMillis", descriptor: "()J", accessFlags: 0x0009},
				{class: "java/lang/System", name: "identityHashCode", descriptor: "(Ljava/lang/Object;)I", accessFlags: 0x0009},
				{class: "java/lang/System", name: "arraycopy", descriptor: "(Ljava/lang/Object;ILjava/lang/Object;II)V", accessFlags: 0x0009},
				{class: "java/lang/System", name: "gc", descriptor: "()V", accessFlags: 0x0009},
				{class: "java/lang/System", name: "getProperty", descriptor: "(Ljava/lang/String;)Ljava/lang/String;", accessFlags: 0x0009, implementation: runtimeSystemGetProperty},
				{class: "java/lang/System", name: "exit", descriptor: "(I)V", accessFlags: 0x0009},
			},
			fields: []runtimeJavaField{
				{name: "out", descriptor: "Ljava/io/PrintStream;", accessFlags: 0x0019, initializer: func(runtime *initializationRuntime) (uint32, error) {
					return runtimeSystemOut(runtime)
				}},
				// A title that prints a caught exception reaches for err
				// rather than out, and a field that is not there is a stop
				// rather than a discarded line. Both answer the same stream,
				// which discards.
				{name: "err", descriptor: "Ljava/io/PrintStream;", accessFlags: 0x0019, initializer: func(runtime *initializationRuntime) (uint32, error) {
					return runtimeSystemOut(runtime)
				}},
			},
		},
		// java/io/PrintStream keeps what a title writes about itself; see the
		// Host logging boundary instead of the guest console.
		"java/io/PrintStream": {
			name:        "java/io/PrintStream",
			superName:   "java/io/OutputStream",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/io/PrintStream", name: "print", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimePrintStreamText},
				{class: "java/io/PrintStream", name: "print", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "print", descriptor: "(C)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "println", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "println", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimePrintStreamText},
				{class: "java/io/PrintStream", name: "println", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "println", descriptor: "(C)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "println", descriptor: "(J)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "println", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "println", descriptor: "(Ljava/lang/Object;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "print", descriptor: "(J)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "print", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "print", descriptor: "(Ljava/lang/Object;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "write", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "java/io/PrintStream", name: "flush", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			},
		},
		// java/lang/Thread exposes the JVM-owned CLDC thread class. Constructors
		// and queries run through the shared JVM; start() of guest Runnables still
		// requires the JVM-to-ARM callback boundary.
		"java/lang/Thread": {
			name:        "java/lang/Thread",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Thread", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/lang/Thread", name: "<init>", descriptor: "(Ljava/lang/Runnable;)V", accessFlags: 0x0001},
				{class: "java/lang/Thread", name: "start", descriptor: "()V", accessFlags: 0x0001},
				// run is what start eventually calls, and a title that
				// subclasses Thread without overriding it inherits this one:
				// the core library's body runs the Runnable the thread was
				// constructed with, and does nothing when there was none.
				// Leaving it undeclared meant the lookup walked from the
				// subclass to here, found nothing, and ended a session that
				// was only starting a sound thread.
				{class: "java/lang/Thread", name: "run", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/lang/Thread", name: "isAlive", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/lang/Thread", name: "interrupt", descriptor: "()V", accessFlags: 0x0001},
				// A guest sleep parks the calling worker for as long as it
				// asked for, which is what sets the game's speed.
				{class: "java/lang/Thread", name: "sleep", descriptor: "(J)V", accessFlags: 0x0009, implementation: runtimeThreadSleep},
				{class: "java/lang/Thread", name: "yield", descriptor: "()V", accessFlags: 0x0009},
				{class: "java/lang/Thread", name: "currentThread", descriptor: "()Ljava/lang/Thread;", accessFlags: 0x0009, implementation: runtimeThreadCurrent},
				{class: "java/lang/Thread", name: "setPriority", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			},
		},
		// java/lang/Class answers class names and JAR resources for guest code.
		"java/lang/Class": {
			name:        "java/lang/Class",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Class", name: "getName", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeClassGetName},
				// forName resolves through the JVM builtin, which answers for a
				// class this runtime knows and raises ClassNotFoundException
				// otherwise. A title names its own class here, which the AOT
				// registry holds under the same name the descriptor uses.
				{class: "java/lang/Class", name: "forName", descriptor: "(Ljava/lang/String;)Ljava/lang/Class;", accessFlags: 0x0009},
				{class: "java/lang/Class", name: "getResourceAsStream", descriptor: "(Ljava/lang/String;)Ljava/io/InputStream;", accessFlags: 0x0001, implementation: runtimeClassGetResourceAsStream},
			},
		},
		// java/io stream classes expose the JVM-owned CLDC implementations.
		"java/io/OutputStream": {
			name:        "java/io/OutputStream",
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				{class: "java/io/OutputStream", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/io/OutputStream", name: "write", descriptor: "([B)V", accessFlags: 0x0001},
				{class: "java/io/OutputStream", name: "write", descriptor: "([BII)V", accessFlags: 0x0001},
				{class: "java/io/OutputStream", name: "write", descriptor: "(I)V", accessFlags: 0x0401},
				{class: "java/io/OutputStream", name: "flush", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/io/OutputStream", name: "close", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		"java/io/ByteArrayOutputStream": {
			name:        "java/io/ByteArrayOutputStream",
			superName:   "java/io/OutputStream",
			accessFlags: 0x0021,
			// The specification declares buf protected, and a title reads it
			// rather than calling toByteArray when what it wants is to hand
			// the bytes straight on — decoding an image out of the buffer it
			// just filled, where the copy toByteArray makes is the whole cost.
			// Publishing it means keeping the payload word in step with the Go
			// array; see fieldSyncs. count is not published: nothing in the
			// local set reads it, and a field that is not there fails loudly
			// where a word nobody maintains would be silently stale.
			instanceSize: byteArrayOutputStreamFieldsSize,
			fields: []runtimeJavaField{
				{name: "buf", descriptor: "[B", accessFlags: 0x0004, offset: 0},
			},
			methods: []runtimeJavaMethod{
				{class: "java/io/ByteArrayOutputStream", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayOutputStream", name: "<init>", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayOutputStream", name: "write", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayOutputStream", name: "write", descriptor: "([BII)V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayOutputStream", name: "toByteArray", descriptor: "()[B", accessFlags: 0x0001},
				{class: "java/io/ByteArrayOutputStream", name: "size", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/ByteArrayOutputStream", name: "reset", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		"java/io/DataOutputStream": {
			name:        "java/io/DataOutputStream",
			superName:   "java/io/OutputStream",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/io/DataOutputStream", name: "<init>", descriptor: "(Ljava/io/OutputStream;)V", accessFlags: 0x0001},
				{class: "java/io/DataOutputStream", name: "write", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/io/DataOutputStream", name: "write", descriptor: "([BII)V", accessFlags: 0x0001},
				{class: "java/io/DataOutputStream", name: "writeBoolean", descriptor: "(Z)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeByte", descriptor: "(I)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeShort", descriptor: "(I)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeChar", descriptor: "(I)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeInt", descriptor: "(I)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeLong", descriptor: "(J)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeUTF", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "writeChars", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0011},
				{class: "java/io/DataOutputStream", name: "flush", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/io/DataOutputStream", name: "close", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		"java/io/InputStream": {
			name:        "java/io/InputStream",
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				// The single-byte read is abstract, and leaving it out of the
				// metadata cost more than a failed lookup: the concrete
				// read()I of every subclass below then took a vtable slot of
				// its own rather than overriding the one a caller resolving
				// through InputStream dispatches to.
				{class: "java/io/InputStream", name: "read", descriptor: "()I", accessFlags: 0x0401},
				{class: "java/io/InputStream", name: "read", descriptor: "([B)I", accessFlags: 0x0001},
				{class: "java/io/InputStream", name: "read", descriptor: "([BII)I", accessFlags: 0x0001},
				{class: "java/io/InputStream", name: "available", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/InputStream", name: "close", descriptor: "()V", accessFlags: 0x0001},
				// The rest of the CLDC surface. Publishing it in one go beats
				// finding it a method at a time: every one of these costs a
				// game a fatal lookup at whatever point it first reaches for
				// it, which for skip was well into a playthrough.
				{class: "java/io/InputStream", name: "skip", descriptor: "(J)J", accessFlags: 0x0001},
				{class: "java/io/InputStream", name: "mark", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/io/InputStream", name: "markSupported", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/io/InputStream", name: "reset", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		"java/io/ByteArrayInputStream": {
			name:        "java/io/ByteArrayInputStream",
			superName:   "java/io/InputStream",
			accessFlags: 0x0021,
			// The specification declares buf protected here as it does on the
			// sink, and a title reads it for the same reason: what it wants is
			// the array it is streaming, without the copy. Keeping the payload
			// word in step with the Go array is fieldSyncs' job.
			instanceSize: byteArrayInputStreamFieldsSize,
			fields: []runtimeJavaField{
				{name: "buf", descriptor: "[B", accessFlags: 0x0004, offset: 0},
			},
			methods: []runtimeJavaMethod{
				{class: "java/io/ByteArrayInputStream", name: "<init>", descriptor: "([B)V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "<init>", descriptor: "([BII)V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "read", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "read", descriptor: "([BII)I", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "available", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "skip", descriptor: "(J)J", accessFlags: 0x0001},
				// A stream over an array overrides all three of these, and
				// declaring them here is what puts the override in the
				// dispatch table: a title calling reset through a variable
				// typed as the superclass otherwise reaches the abstract
				// class's own reset, which throws.
				{class: "java/io/ByteArrayInputStream", name: "mark", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "markSupported", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/io/ByteArrayInputStream", name: "reset", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		"java/io/DataInputStream": {
			name:        "java/io/DataInputStream",
			superName:   "java/io/InputStream",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/io/DataInputStream", name: "<init>", descriptor: "(Ljava/io/InputStream;)V", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "read", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "read", descriptor: "([BII)I", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "available", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "close", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "skip", descriptor: "(J)J", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "skipBytes", descriptor: "(I)I", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readBoolean", descriptor: "()Z", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readByte", descriptor: "()B", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readShort", descriptor: "()S", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readUnsignedShort", descriptor: "()I", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readInt", descriptor: "()I", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readLong", descriptor: "()J", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readUTF", descriptor: "()Ljava/lang/String;", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readUnsignedByte", descriptor: "()I", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readChar", descriptor: "()C", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readFully", descriptor: "([B)V", accessFlags: 0x0011},
				{class: "java/io/DataInputStream", name: "readFully", descriptor: "([BII)V", accessFlags: 0x0011},
				// The wrapper passes all three on to what it wraps, and it has
				// to declare them for the same reason the array stream does.
				{class: "java/io/DataInputStream", name: "mark", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "markSupported", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/io/DataInputStream", name: "reset", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		// java/lang/Math exposes the JVM-owned builtins through KTF metadata.
		// java/util/Calendar reads the guest clock through the JVM builtin, so a
		// game that stamps a save or gates an event on the date sees the same
		// time System.currentTimeMillis reports.
		"java/util/Calendar": {
			name:        "java/util/Calendar",
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				{class: "java/util/Calendar", name: "getInstance", descriptor: "()Ljava/util/Calendar;", accessFlags: 0x0009},
				{class: "java/util/Calendar", name: "getInstance", descriptor: "(Ljava/util/TimeZone;)Ljava/util/Calendar;", accessFlags: 0x0009},
				{class: "java/util/Calendar", name: "get", descriptor: "(I)I", accessFlags: 0x0001},
				{class: "java/util/Calendar", name: "set", descriptor: "(II)V", accessFlags: 0x0001},
				{class: "java/util/Calendar", name: "getTime", descriptor: "()Ljava/util/Date;", accessFlags: 0x0011},
				{class: "java/util/Calendar", name: "setTime", descriptor: "(Ljava/util/Date;)V", accessFlags: 0x0011},
			},
		},
		// java/util/GregorianCalendar is the concrete calendar, and a title
		// that constructs one directly rather than through the factory needs
		// it to *extend* Calendar here. The loader's fallback builds a class
		// that extends `java/lang/Object`, so a `Calendar.get` dispatched on
		// such an instance indexes Object's vtable at Calendar's slot — past
		// its end, into the bytes the name string was allocated with, which
		// the guest then follows as a pointer. One title faults on a read at
		// the characters of its own class name for exactly that reason.
		"java/util/GregorianCalendar": {
			name:        "java/util/GregorianCalendar",
			superName:   "java/util/Calendar",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/util/GregorianCalendar", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
			},
		},
		// java/util/Date is the instant behind Calendar, and a title reaches
		// for it directly when what it wants is a number rather than fields —
		// a stamp to store, or the two ends of an interval. A class the loader
		// answers for but whose methods are missing fails at the call rather
		// than at the load, so one card title ran for an hour before its first
		// getTime() ended the session.
		"java/util/Date": {
			name:        "java/util/Date",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/util/Date", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Date", name: "<init>", descriptor: "(J)V", accessFlags: 0x0001},
				{class: "java/util/Date", name: "getTime", descriptor: "()J", accessFlags: 0x0001},
				{class: "java/util/Date", name: "setTime", descriptor: "(J)V", accessFlags: 0x0001},
				{class: "java/util/Date", name: "equals", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/util/Date", name: "hashCode", descriptor: "()I", accessFlags: 0x0001},
			},
		},
		// java/lang/Runtime is CLDC's memory query. A game asks it how much room
		// is left before it loads the next block of assets, so the numbers have
		// to be the same ones MC_knlGetTotalMemory and MC_knlGetFreeMemory
		// answer — two views of one heap that disagreed would have a game free
		// what it just decided it could afford.
		"java/lang/Runtime": {
			name:        "java/lang/Runtime",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Runtime", name: "getRuntime", descriptor: "()Ljava/lang/Runtime;", accessFlags: 0x0009},
				{class: "java/lang/Runtime", name: "gc", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/lang/Runtime", name: "totalMemory", descriptor: "()J", accessFlags: 0x0001, implementation: runtimeTotalMemory},
				{class: "java/lang/Runtime", name: "freeMemory", descriptor: "()J", accessFlags: 0x0001, implementation: runtimeFreeMemory},
			},
		},
		"java/lang/Math": {
			name:        "java/lang/Math",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Math", name: "abs", descriptor: "(I)I", accessFlags: 0x0009},
				{class: "java/lang/Math", name: "abs", descriptor: "(J)J", accessFlags: 0x0009},
				{class: "java/lang/Math", name: "abs", descriptor: "(F)F", accessFlags: 0x0009},
				{class: "java/lang/Math", name: "abs", descriptor: "(D)D", accessFlags: 0x0009},
				{class: "java/lang/Math", name: "min", descriptor: "(II)I", accessFlags: 0x0009},
				{class: "java/lang/Math", name: "max", descriptor: "(II)I", accessFlags: 0x0009},
				// The long forms are what a title uses on a clock difference,
				// which is the one quantity in a CLDC game that does not fit
				// an int. The core library already had both bodies.
				{class: "java/lang/Math", name: "min", descriptor: "(JJ)J", accessFlags: 0x0009},
				{class: "java/lang/Math", name: "max", descriptor: "(JJ)J", accessFlags: 0x0009},
			},
		},
		// java/lang/Integer exposes the JVM-owned CLDC implementation. A class
		// missing from this table still resolves — ensureJavaClass allocates an
		// empty record for it — so the omission only shows up as a failed
		// method lookup the first time a title parses a number, which is deep
		// inside a menu rather than at startup.
		"java/lang/Integer": {
			name:        "java/lang/Integer",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Integer", name: "<init>", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/lang/Integer", name: "parseInt", descriptor: "(Ljava/lang/String;)I", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "parseInt", descriptor: "(Ljava/lang/String;I)I", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "toString", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "toHexString", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "toBinaryString", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "toOctalString", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "valueOf", descriptor: "(Ljava/lang/String;)Ljava/lang/Integer;", accessFlags: 0x0009},
				{class: "java/lang/Integer", name: "intValue", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/Integer", name: "byteValue", descriptor: "()B", accessFlags: 0x0001},
				{class: "java/lang/Integer", name: "shortValue", descriptor: "()S", accessFlags: 0x0001},
				{class: "java/lang/Integer", name: "longValue", descriptor: "()J", accessFlags: 0x0001},
				{class: "java/lang/Integer", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
			},
		},
		// java/lang/Boolean is the fourth box, and the one nothing published
		// until a title asked for it. TRUE and FALSE are the two instances the
		// specification names; they come from the core library's own class
		// initializer rather than from a pair made here, so guest code
		// comparing a boxed flag against the field with a pointer compare sees
		// the same object interpreted code would.
		"java/lang/Boolean": {
			name:        "java/lang/Boolean",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Boolean", name: "<init>", descriptor: "(Z)V", accessFlags: 0x0001},
				{class: "java/lang/Boolean", name: "booleanValue", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/lang/Boolean", name: "equals", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/lang/Boolean", name: "hashCode", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/Boolean", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
			},
			fields: []runtimeJavaField{
				{name: "TRUE", descriptor: "Ljava/lang/Boolean;", accessFlags: 0x0019, initializer: func(runtime *initializationRuntime) (uint32, error) {
					return runtimeBoxedBoolean(runtime, "TRUE")
				}},
				{name: "FALSE", descriptor: "Ljava/lang/Boolean;", accessFlags: 0x0019, initializer: func(runtime *initializationRuntime) (uint32, error) {
					return runtimeBoxedBoolean(runtime, "FALSE")
				}},
			},
		},
		// The other three boxed numbers, exposed the same way. A title that
		// parses a UI form's attributes asks for one class per attribute type
		// — a coordinate through java/lang/Short, a flag through
		// java/lang/Byte — and an unregistered class stops the form with a
		// failed method lookup rather than a wrong answer. See "An
		// unimplemented table call names its call site".
		"java/lang/Byte": {
			name:        "java/lang/Byte",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Byte", name: "<init>", descriptor: "(B)V", accessFlags: 0x0001},
				{class: "java/lang/Byte", name: "parseByte", descriptor: "(Ljava/lang/String;)B", accessFlags: 0x0009},
				{class: "java/lang/Byte", name: "byteValue", descriptor: "()B", accessFlags: 0x0001},
				{class: "java/lang/Byte", name: "intValue", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/Byte", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
			},
		},
		"java/lang/Short": {
			name:        "java/lang/Short",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Short", name: "<init>", descriptor: "(S)V", accessFlags: 0x0001},
				{class: "java/lang/Short", name: "parseShort", descriptor: "(Ljava/lang/String;)S", accessFlags: 0x0009},
				{class: "java/lang/Short", name: "shortValue", descriptor: "()S", accessFlags: 0x0001},
				{class: "java/lang/Short", name: "intValue", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/Short", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
			},
		},
		"java/lang/Long": {
			name:        "java/lang/Long",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/Long", name: "<init>", descriptor: "(J)V", accessFlags: 0x0001},
				{class: "java/lang/Long", name: "parseLong", descriptor: "(Ljava/lang/String;)J", accessFlags: 0x0009},
				{class: "java/lang/Long", name: "toString", descriptor: "(J)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/Long", name: "longValue", descriptor: "()J", accessFlags: 0x0001},
				{class: "java/lang/Long", name: "intValue", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/Long", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
			},
		},
		// java/util/Random exposes the JVM-owned CLDC implementation.
		"java/util/Random": {
			name:        "java/util/Random",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/util/Random", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Random", name: "<init>", descriptor: "(J)V", accessFlags: 0x0001},
				{class: "java/util/Random", name: "setSeed", descriptor: "(J)V", accessFlags: 0x0001},
				{class: "java/util/Random", name: "nextInt", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/util/Random", name: "nextInt", descriptor: "(I)I", accessFlags: 0x0001},
				{class: "java/util/Random", name: "nextLong", descriptor: "()J", accessFlags: 0x0001},
			},
		},
		// java/lang/String exposes the JVM-owned CLDC implementation through KTF
		// metadata; nil implementations delegate to the registered builtins.
		"java/lang/String": {
			name:         "java/lang/String",
			superName:    "java/lang/Object",
			accessFlags:  0x0031,
			instanceSize: 12,
			// Guest code reads string content directly through the original
			// field layout: the UTF-16 array, the view offset, and the length.
			fields: []runtimeJavaField{
				{name: "value", descriptor: "[C", accessFlags: 0x0002, offset: 0},
				{name: "offset", descriptor: "I", accessFlags: 0x0002, offset: 4},
				{name: "count", descriptor: "I", accessFlags: 0x0002, offset: 8},
			},
			methods: []runtimeJavaMethod{
				{class: "java/lang/String", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "([B)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "([BII)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "([BLjava/lang/String;)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "([BIILjava/lang/String;)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "([C)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "<init>", descriptor: "([CII)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "length", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "charAt", descriptor: "(I)C", accessFlags: 0x0001},
				{class: "java/lang/String", name: "equals", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/lang/String", name: "hashCode", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "concat", descriptor: "(Ljava/lang/String;)Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "getBytes", descriptor: "()[B", accessFlags: 0x0001},
				{class: "java/lang/String", name: "toCharArray", descriptor: "()[C", accessFlags: 0x0001},
				{class: "java/lang/String", name: "getChars", descriptor: "(II[CI)V", accessFlags: 0x0001},
				{class: "java/lang/String", name: "indexOf", descriptor: "(I)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "indexOf", descriptor: "(Ljava/lang/String;)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "indexOf", descriptor: "(Ljava/lang/String;I)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "indexOf", descriptor: "(II)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "lastIndexOf", descriptor: "(I)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "lastIndexOf", descriptor: "(II)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "startsWith", descriptor: "(Ljava/lang/String;)Z", accessFlags: 0x0001},
				{class: "java/lang/String", name: "substring", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "substring", descriptor: "(II)Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "compareTo", descriptor: "(Ljava/lang/String;)I", accessFlags: 0x0001},
				{class: "java/lang/String", name: "equalsIgnoreCase", descriptor: "(Ljava/lang/String;)Z", accessFlags: 0x0001},
				{class: "java/lang/String", name: "endsWith", descriptor: "(Ljava/lang/String;)Z", accessFlags: 0x0001},
				{class: "java/lang/String", name: "toUpperCase", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "toLowerCase", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "trim", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "replace", descriptor: "(CC)Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "getBytes", descriptor: "(Ljava/lang/String;)[B", accessFlags: 0x0001},
				{class: "java/lang/String", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/lang/String", name: "valueOf", descriptor: "(C)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/String", name: "valueOf", descriptor: "([C)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/String", name: "valueOf", descriptor: "([CII)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/String", name: "valueOf", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/String", name: "valueOf", descriptor: "(J)Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/lang/String", name: "valueOf", descriptor: "(Ljava/lang/Object;)Ljava/lang/String;", accessFlags: 0x0009},
			},
		},
		// java/lang/StringBuffer exposes the JVM-owned CLDC implementation through
		// KTF metadata; nil implementations delegate to the registered builtins.
		"java/lang/StringBuffer": {
			name:        "java/lang/StringBuffer",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "java/lang/StringBuffer", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "<init>", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "<init>", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "(C)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "(I)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "(Ljava/lang/String;)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "(Ljava/lang/Object;)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "(J)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "(Z)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "([C)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "append", descriptor: "([CII)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "delete", descriptor: "(II)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "insert", descriptor: "(IC)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "insert", descriptor: "(II)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "insert", descriptor: "(ILjava/lang/String;)Ljava/lang/StringBuffer;", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "charAt", descriptor: "(I)C", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "setCharAt", descriptor: "(IC)V", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "length", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "setLength", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/lang/StringBuffer", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
			},
		},
		// java/util/TimeZone is what a title asks for before it builds a
		// calendar. What this runtime has is the zone its clock runs in and
		// GMT; see the class in the core library for why there is no database
		// behind it.
		"java/util/TimeZone": {
			name:        "java/util/TimeZone",
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				{class: "java/util/TimeZone", name: "getDefault", descriptor: "()Ljava/util/TimeZone;", accessFlags: 0x0009},
				{class: "java/util/TimeZone", name: "getTimeZone", descriptor: "(Ljava/lang/String;)Ljava/util/TimeZone;", accessFlags: 0x0009},
				{class: "java/util/TimeZone", name: "getAvailableIDs", descriptor: "()[Ljava/lang/String;", accessFlags: 0x0009},
				{class: "java/util/TimeZone", name: "getID", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001},
				{class: "java/util/TimeZone", name: "getRawOffset", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/util/TimeZone", name: "useDaylightTime", descriptor: "()Z", accessFlags: 0x0001},
			},
		},
		// java/io/Reader and java/io/InputStreamReader are how a title reads a
		// bundled text resource as characters. The decoding is the core
		// library's; what is here is the metadata that makes the two classes
		// resolvable.
		"java/io/Reader": {
			name:        "java/io/Reader",
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				{class: "java/io/Reader", name: "<init>", descriptor: "()V", accessFlags: 0x0004},
				{class: "java/io/Reader", name: "read", descriptor: "()I", accessFlags: 0x0401},
				{class: "java/io/Reader", name: "read", descriptor: "([C)I", accessFlags: 0x0001},
				{class: "java/io/Reader", name: "read", descriptor: "([CII)I", accessFlags: 0x0401},
				{class: "java/io/Reader", name: "close", descriptor: "()V", accessFlags: 0x0401},
				{class: "java/io/Reader", name: "ready", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/io/Reader", name: "markSupported", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/io/Reader", name: "skip", descriptor: "(J)J", accessFlags: 0x0001},
			},
		},
		"java/io/InputStreamReader": {
			name:        "java/io/InputStreamReader",
			superName:   "java/io/Reader",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/io/InputStreamReader", name: "<init>", descriptor: "(Ljava/io/InputStream;)V", accessFlags: 0x0001},
				{class: "java/io/InputStreamReader", name: "<init>", descriptor: "(Ljava/io/InputStream;Ljava/lang/String;)V", accessFlags: 0x0001},
				{class: "java/io/InputStreamReader", name: "read", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/io/InputStreamReader", name: "read", descriptor: "([CII)I", accessFlags: 0x0001},
				{class: "java/io/InputStreamReader", name: "close", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/io/InputStreamReader", name: "ready", descriptor: "()Z", accessFlags: 0x0001},
			},
		},
		// java/util/Vector and java/util/Hashtable expose the JVM-owned CLDC
		// implementations through KTF metadata like StringBuffer does.
		"java/util/Vector": {
			name:        "java/util/Vector",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/util/Vector", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "<init>", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "<init>", descriptor: "(II)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "addElement", descriptor: "(Ljava/lang/Object;)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "elementAt", descriptor: "(I)Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "indexOf", descriptor: "(Ljava/lang/Object;)I", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "lastIndexOf", descriptor: "(Ljava/lang/Object;)I", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "insertElementAt", descriptor: "(Ljava/lang/Object;I)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "setElementAt", descriptor: "(Ljava/lang/Object;I)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "removeElement", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "removeElementAt", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "removeAllElements", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "contains", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "copyInto", descriptor: "([Ljava/lang/Object;)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "firstElement", descriptor: "()Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "lastElement", descriptor: "()Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "isEmpty", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "size", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "capacity", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "ensureCapacity", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "setSize", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "trimToSize", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Vector", name: "elements", descriptor: "()Ljava/util/Enumeration;", accessFlags: 0x0001},
			},
		},
		// java/util/Stack is Vector with the four methods that make it one,
		// and it is a subclass here as it is in the standard library — a title
		// that stores into it through the Vector half sees what it expects.
		// Without the class the loader answered with an empty record, which is
		// a stop several calls later at a field read rather than at the `new`.
		"java/util/Stack": {
			name:        "java/util/Stack",
			superName:   "java/util/Vector",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/util/Stack", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Stack", name: "push", descriptor: "(Ljava/lang/Object;)Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Stack", name: "pop", descriptor: "()Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Stack", name: "peek", descriptor: "()Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Stack", name: "empty", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/util/Stack", name: "search", descriptor: "(Ljava/lang/Object;)I", accessFlags: 0x0001},
			},
		},
		"java/util/Hashtable": {
			name:        "java/util/Hashtable",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "java/util/Hashtable", name: "<init>", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "<init>", descriptor: "(I)V", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "containsKey", descriptor: "(Ljava/lang/Object;)Z", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "get", descriptor: "(Ljava/lang/Object;)Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "put", descriptor: "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "remove", descriptor: "(Ljava/lang/Object;)Ljava/lang/Object;", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "isEmpty", descriptor: "()Z", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "size", descriptor: "()I", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "clear", descriptor: "()V", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "keys", descriptor: "()Ljava/util/Enumeration;", accessFlags: 0x0001},
				{class: "java/util/Hashtable", name: "elements", descriptor: "()Ljava/util/Enumeration;", accessFlags: 0x0001},
			},
		},
		// org/kwis/msp/handset/BackLight matches the original runtime's accepted
		// no-op backlight controls.
		runtimeBackLightClass: {
			name:        runtimeBackLightClass,
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: runtimeBackLightClass, name: "alwaysOn", descriptor: "()V", accessFlags: 0x0009, implementation: runtimeBackLightNoop},
				{class: runtimeBackLightClass, name: "on", descriptor: "(III)V", accessFlags: 0x0009, implementation: runtimeBackLightNoop},
				{class: runtimeBackLightClass, name: "off", descriptor: "()V", accessFlags: 0x0009, implementation: runtimeBackLightNoop},
				{class: runtimeBackLightClass, name: "before", descriptor: "()V", accessFlags: 0x0009, implementation: runtimeBackLightNoop},
			},
		},
		"org/kwis/msp/io/File":       runtimeFileClassDefinition(),
		runtimeFileOutputStreamClass: runtimeFileOutputStreamClassDefinition(),
		runtimeTimerClass:            runtimeTimerClassDefinition(),
		runtimeTimerTaskClass:        runtimeTimerTaskClassDefinition(),
		"org/kwis/msp/io/FileSystem": runtimeFileSystemClassDefinition(),
		runtimeJletClass: {
			name:        runtimeJletClass,
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{
					class:          runtimeJletClass,
					name:           "<init>",
					descriptor:     "()V",
					accessFlags:    0x0001,
					implementation: runtimeJletConstructor,
				},
				{class: runtimeJletClass, name: "getActiveJlet", descriptor: "()Lorg/kwis/msp/lcdui/Jlet;", accessFlags: 0x0009, implementation: runtimeJletGetActive},
				{class: runtimeJletClass, name: "getEventQueue", descriptor: "()Lorg/kwis/msp/lcdui/EventQueue;", accessFlags: 0x0001, implementation: runtimeJletGetEventQueue},
				{class: runtimeJletClass, name: "getAppProperty", descriptor: "(Ljava/lang/String;)Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeJletGetAppProperty},
				{class: runtimeJletClass, name: "notifyDestroyed", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeJletNotifyDestroyed},
				// The lifecycle callbacks the base class declares. A title
				// overrides the ones it cares about and its override opens by
				// calling super, which is what these are for — three local
				// titles died in their own pauseApp or resumeApp reporting
				// this class had no such method, the moment a Host started
				// making the calls at all. The base body does nothing, which
				// is what the specification says it does.
				{class: runtimeJletClass, name: "startApp", descriptor: "([Ljava/lang/String;)V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
				{class: runtimeJletClass, name: jletPauseApp, descriptor: "()V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
				{class: runtimeJletClass, name: jletResumeApp, descriptor: "()V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
				{class: runtimeJletClass, name: "destroyApp", descriptor: "(Z)V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
			},
		},
		runtimeCardClass: {
			name:        runtimeCardClass,
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			// The specification declares a Card's geometry and its
			// transparency flag protected, and a title reads `w` off its own
			// canvas rather than calling getWidth. See field_sync.go for how
			// the payload and the Go value are kept in step, and why a
			// subclass compiled against a runtime that published none of this
			// is left alone.
			instanceSize: cardFieldsSize,
			fields: []runtimeJavaField{
				{name: "x", descriptor: "I", accessFlags: 0x0004, offset: 0},
				{name: "y", descriptor: "I", accessFlags: 0x0004, offset: 4},
				{name: "w", descriptor: "I", accessFlags: 0x0004, offset: 8},
				{name: "h", descriptor: "I", accessFlags: 0x0004, offset: 12},
				{name: "bTrans", descriptor: "Z", accessFlags: 0x0004, offset: 16},
			},
			methods: []runtimeJavaMethod{
				{
					class:          runtimeCardClass,
					name:           "<init>",
					descriptor:     "()V",
					accessFlags:    0x0001,
					implementation: runtimeCardConstructor,
				},
				{
					class:          runtimeCardClass,
					name:           "<init>",
					descriptor:     "(I)V",
					accessFlags:    0x0001,
					implementation: runtimeCardConstructor,
				},
				{
					class:          runtimeCardClass,
					name:           "<init>",
					descriptor:     "(Z)V",
					accessFlags:    0x0001,
					implementation: runtimeCardConstructorTransparent,
				},
				{
					class:          runtimeCardClass,
					name:           "<init>",
					descriptor:     "(Lorg/kwis/msp/lcdui/Display;)V",
					accessFlags:    0x0001,
					implementation: runtimeCardConstructor,
				},
				{
					class:          runtimeCardClass,
					name:           "<init>",
					descriptor:     "(IIII)V",
					accessFlags:    0x0001,
					implementation: runtimeCardConstructorBounds,
				},
				{
					class:          runtimeCardClass,
					name:           "getWidth",
					descriptor:     "()I",
					accessFlags:    0x0001,
					implementation: runtimeCardScreenField("w:I", runtimeScreenWidth),
				},
				{
					class:          runtimeCardClass,
					name:           "getHeight",
					descriptor:     "()I",
					accessFlags:    0x0001,
					implementation: runtimeCardScreenField("h:I", runtimeScreenHeight),
				},
				{
					class:          runtimeCardClass,
					name:           "getX",
					descriptor:     "()I",
					accessFlags:    0x0001,
					implementation: runtimeCardIntField("x:I", 0),
				},
				{
					class:          runtimeCardClass,
					name:           "getY",
					descriptor:     "()I",
					accessFlags:    0x0001,
					implementation: runtimeCardIntField("y:I", 0),
				},
				{
					class:          runtimeCardClass,
					name:           "getDisplay",
					descriptor:     "()Lorg/kwis/msp/lcdui/Display;",
					accessFlags:    0x0001,
					implementation: runtimeCardGetDisplay,
				},
				{
					class:          runtimeCardClass,
					name:           "repaint",
					descriptor:     "()V",
					accessFlags:    0x0001,
					implementation: runtimeCardRepaint,
				},
				{
					class:          runtimeCardClass,
					name:           "repaint",
					descriptor:     "(IIII)V",
					accessFlags:    0x0001,
					implementation: runtimeCardRepaint,
				},
				{
					class:          runtimeCardClass,
					name:           "serviceRepaints",
					descriptor:     "()V",
					accessFlags:    0x0001,
					implementation: runtimeCardServiceRepaints,
				},
				{
					class:          runtimeCardClass,
					name:           "keyNotify",
					descriptor:     "(II)Z",
					accessFlags:    0x0001,
					implementation: runtimeComponentZero,
				},
				{class: runtimeCardClass, name: "<init>", descriptor: "(Lorg/kwis/msp/lcdui/Display;IIII)V", accessFlags: 0x0001, implementation: runtimeCardConstructorDisplayBounds},
				{class: runtimeCardClass, name: "<init>", descriptor: "(Lorg/kwis/msp/lcdui/Display;IIIIZ)V", accessFlags: 0x0001, implementation: runtimeCardConstructorDisplayBounds},
				{class: runtimeCardClass, name: "move", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeCardMove},
				{class: runtimeCardClass, name: "resize", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeCardResize},
				{class: runtimeCardClass, name: "isShown", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeCardIsShown},
				{class: runtimeCardClass, name: "showNotify", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeCardClass, name: "pointerNotify", descriptor: "(III)Z", accessFlags: 0x0004, implementation: runtimeComponentZero},
			},
		},
		// The lwc component hierarchy matches the original runtime so guest
		// method lookups through superclasses resolve.
		runtimeComponentClass: {
			name:        runtimeComponentClass,
			superName:   "java/lang/Object",
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				{class: runtimeComponentClass, name: "<init>", descriptor: "()V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
				{class: runtimeComponentClass, name: "getHeight", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeComponentZero},
				{class: runtimeComponentClass, name: "getWidth", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeComponentZero},
				{class: runtimeComponentClass, name: "keyNotify", descriptor: "(II)Z", accessFlags: 0x0001, implementation: runtimeComponentKeyNotify},
				{class: runtimeComponentClass, name: "focusNotify", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentBooleanField("focused:Z")},
				{class: runtimeComponentClass, name: "showNotify", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentBooleanField("shown:Z")},
				{class: runtimeComponentClass, name: "configure", descriptor: "(IIIII)V", accessFlags: 0x0001, implementation: runtimeComponentConfigure},
				{class: runtimeComponentClass, name: "setFocus", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentSetFocus},
				// A listener and the object it is to be handed back with.
				// Nothing fires one — no widget here is drawn, so no widget is
				// operated — but the pair is kept, because a title that sets a
				// listener reads it back to decide whether it has already
				// built its dialog.
				{class: runtimeComponentClass, name: "setEventListener", descriptor: "(Lorg/kwis/msp/lwc/EventListener;Ljava/lang/Object;)V", accessFlags: 0x0001, implementation: runtimeComponentSetEventListener},
				{class: runtimeComponentClass, name: "getEventListener", descriptor: "()Lorg/kwis/msp/lwc/EventListener;", accessFlags: 0x0001, implementation: runtimeComponentField(componentEventListenerField)},
				// A component asking to be redrawn is asking for the card it
				// sits in, because nothing here paints a component: the
				// toolkit's layout is absent and the children are kept only so
				// a title can walk them. Requesting the card's own repaint is
				// what the title is after — the screen is out of date — and it
				// draws through the same paint the card would have run anyway,
				// so the named rectangle adds nothing to honour. The
				// specification declares both forms on Component and again on
				// ContainerComponent, and the lookup walks parents, so
				// declaring them here serves every component in the tree.
				// The specification gives every component a background and a
				// foreground colour and no default beyond "it depends on the
				// component". Nothing here paints one, so the pair is kept and
				// answered: a title that sets a colour and reads it back sees
				// what it set, and one that only reads sees black.
				{class: runtimeComponentClass, name: "setBackground", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("Component.setBackground", "bg:I")},
				{class: runtimeComponentClass, name: "getBackground", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("bg:I", 0)},
				{class: runtimeComponentClass, name: "setForeground", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("Component.setForeground", "fg:I")},
				{class: runtimeComponentClass, name: "getForeground", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("fg:I", 0)},
				{class: runtimeComponentClass, name: "repaint", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeCardRepaint},
				{class: runtimeComponentClass, name: "repaint", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeCardRepaint},
			},
		},
		runtimeContainerComponentClass: {
			name:        runtimeContainerComponentClass,
			superName:   runtimeComponentClass,
			accessFlags: 0x0421,
			methods: []runtimeJavaMethod{
				{class: runtimeContainerComponentClass, name: "<init>", descriptor: "()V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
				{class: runtimeContainerComponentClass, name: "addComponent", descriptor: "(Lorg/kwis/msp/lwc/Component;)I", accessFlags: 0x0001, implementation: runtimeComponentAddComponent},
				{class: runtimeContainerComponentClass, name: "removeComponent", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentRemoveComponent},
				{class: runtimeContainerComponentClass, name: "removeComponent", descriptor: "(Lorg/kwis/msp/lwc/Component;)V", accessFlags: 0x0001, implementation: runtimeComponentRemoveComponent},
				{class: runtimeContainerComponentClass, name: "removeAllComponents", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentRemoveAllComponents},
				{class: runtimeContainerComponentClass, name: "getNumberOfComponent", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeComponentCount},
				{class: runtimeContainerComponentClass, name: "getComponent", descriptor: "(I)Lorg/kwis/msp/lwc/Component;", accessFlags: 0x0001, implementation: runtimeComponentAt},
				{class: runtimeContainerComponentClass, name: "getIndexOf", descriptor: "(Lorg/kwis/msp/lwc/Component;)I", accessFlags: 0x0001, implementation: runtimeComponentIndexOf},
				// Laying a container out and validating it are the toolkit's
				// work, and there is no toolkit here; the children are kept so
				// a title can walk them, not so anything positions them.
				{class: runtimeContainerComponentClass, name: "validate", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeContainerComponentClass, name: "invalidate", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeContainerComponentClass, name: "layout", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			},
		},
		runtimeShellComponentClass: {
			name:        runtimeShellComponentClass,
			superName:   runtimeContainerComponentClass,
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: runtimeShellComponentClass, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeShellComponentClass, name: "<init>", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeShellComponentClass, name: "setWorkComponent", descriptor: "(Lorg/kwis/msp/lwc/Component;)V", accessFlags: 0x0001, implementation: runtimeComponentSetWorkComponent},
				{class: runtimeShellComponentClass, name: "getX", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("x:I", 0)},
				{class: runtimeShellComponentClass, name: "getY", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("y:I", 0)},
				{class: runtimeShellComponentClass, name: "layout", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeShellComponentClass, name: "show", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeShellComponentClass, name: "hide", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			},
		},
		// org/kwis/msp/lcdui/Image currently supports mutable off-screen creation
		// queried by size; pixel operations arrive with the display path.
		"org/kwis/msp/lcdui/Image": {
			name:        "org/kwis/msp/lcdui/Image",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/lcdui/Image", name: "createImage", descriptor: "(II)Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0009, implementation: runtimeImageCreateSized},
				{class: "org/kwis/msp/lcdui/Image", name: "createImage", descriptor: "([BII)Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0009, implementation: runtimeImageCreateFromBytes},
				{class: "org/kwis/msp/lcdui/Image", name: "createImage", descriptor: "(Ljava/lang/String;)Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0009, implementation: runtimeImageCreateFromResource},
				{class: "org/kwis/msp/lcdui/Image", name: "getWidth", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("width:I", 0)},
				{class: "org/kwis/msp/lcdui/Image", name: "getHeight", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("height:I", 0)},
				{class: "org/kwis/msp/lcdui/Image", name: "isMutable", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeCardIntField("mutable:Z", 0)},
				{class: "org/kwis/msp/lcdui/Image", name: "getGraphics", descriptor: "()Lorg/kwis/msp/lcdui/Graphics;", accessFlags: 0x0001, implementation: runtimeImageGetGraphics},
				{class: "org/kwis/msp/lcdui/Image", name: "createImage", descriptor: "(Lorg/kwis/msp/lcdui/Image;)Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0009, implementation: runtimeImageCreateCopy},
				{class: "org/kwis/msp/lcdui/Image", name: "createSubImage", descriptor: "(IIIIZ)Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0001, implementation: runtimeImageCreateSubImage},
				{class: "org/kwis/msp/lcdui/Image", name: "drawImage", descriptor: "(Lorg/kwis/msp/lcdui/Image;IIIIIIII)V", accessFlags: 0x0001, implementation: runtimeImageDrawImage},
				{class: "org/kwis/msp/lcdui/Image", name: "setTransparentColor", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeImageSetTransparentColor},
				// Animated images are not decoded, so no image reports itself
				// animated and the playback controls stay unimplemented.
				{class: "org/kwis/msp/lcdui/Image", name: "isAnimated", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeComponentZero},
				// The specification says play does nothing for an image that is
				// not animated, and none of ours is, so the no-op here is the
				// contract rather than a stand-in. A title that calls it still
				// has to be able to, and stop is the call the specification
				// tells it to make afterwards.
				{class: "org/kwis/msp/lcdui/Image", name: "play", descriptor: "(Lorg/kwis/msp/lcdui/ImageObserver;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/lcdui/Image", name: "stop", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			},
		},
		// org/kwis/msp/lcdui/Graphics currently records its target; drawing
		// operations arrive with the display path.
		"org/kwis/msp/lcdui/Graphics": {
			name:        "org/kwis/msp/lcdui/Graphics",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/lcdui/Graphics", name: "setColor", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetColor},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setColor", descriptor: "(III)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetColor},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getColor", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsGetColor},
				{class: "org/kwis/msp/lcdui/Graphics", name: "fillRect", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsFillRect},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawRect", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawRect},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawLine", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawLine},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawImage", descriptor: "(Lorg/kwis/msp/lcdui/Image;III)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawImage},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setClip", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetClip},
				{class: "org/kwis/msp/lcdui/Graphics", name: "clipRect", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsClipRect},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawString", descriptor: "(Ljava/lang/String;III)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawString},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setAlpha", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetAlpha},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getAlpha", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsGetAlpha},
				{class: "org/kwis/msp/lcdui/Graphics", name: "translate", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeGraphicsTranslate},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getTranslateX", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsTranslation(false)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getTranslateY", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsTranslation(true)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "reset", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeGraphicsReset},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getRedComponent", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsColorComponent(16)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getGreenComponent", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsColorComponent(8)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getBlueComponent", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsColorComponent(0)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setStrokeStyle", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetStrokeStyle},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getStrokeStyle", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsGetStrokeStyle},
				{class: "org/kwis/msp/lcdui/Graphics", name: "isXORMode", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeGraphicsIsXORMode},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawChars", descriptor: "([CIIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawChars},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawSubstring", descriptor: "(Ljava/lang/String;IIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawSubstring},
				{class: "org/kwis/msp/lcdui/Graphics", name: "copyArea", descriptor: "(IIIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsCopyArea},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setPixel", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetPixel},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getPixel", descriptor: "(II)I", accessFlags: 0x0001, implementation: runtimeGraphicsGetPixel},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getPixels", descriptor: "(IIII[BII)V", accessFlags: 0x0001, implementation: runtimeGraphicsTransferPixels(false)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setPixels", descriptor: "(IIII[BII)V", accessFlags: 0x0001, implementation: runtimeGraphicsTransferPixels(true)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getRGBPixels", descriptor: "(IIII[III)V", accessFlags: 0x0001, implementation: runtimeGraphicsTransferRGBPixels(false)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setRGBPixels", descriptor: "(IIII[III)V", accessFlags: 0x0001, implementation: runtimeGraphicsTransferRGBPixels(true)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawPolygon", descriptor: "([I[I)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawPolygon(false)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "fillPolygon", descriptor: "([I[I)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawPolygon(true)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setGrayScale", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetGrayScale},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setXORMode", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeGraphicsSetXORMode},
				{class: "org/kwis/msp/lcdui/Graphics", name: "fillArc", descriptor: "(IIIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsFillArc},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawArc", descriptor: "(IIIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawArc},
				{class: "org/kwis/msp/lcdui/Graphics", name: "fillRoundRect", descriptor: "(IIIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsFillRoundRect},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawRoundRect", descriptor: "(IIIIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawRoundRect},
				{class: "org/kwis/msp/lcdui/Graphics", name: "drawChar", descriptor: "(CIII)V", accessFlags: 0x0001, implementation: runtimeGraphicsDrawChar},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getGrayScale", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsGetColor},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getClipX", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsClipValue(0)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getClipY", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsClipValue(1)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getClipWidth", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsClipValue(2)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getClipHeight", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGraphicsClipValue(3)},
				{class: "org/kwis/msp/lcdui/Graphics", name: "getFont", descriptor: "()Lorg/kwis/msp/lcdui/Font;", accessFlags: 0x0001, implementation: runtimeGraphicsGetFont},
				{class: "org/kwis/msp/lcdui/Graphics", name: "setFont", descriptor: "(Lorg/kwis/msp/lcdui/Font;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/lcdui/Graphics", name: "encodeImage", descriptor: "(IIII)[B", accessFlags: 0x0001, implementation: runtimeGraphicsEncodeImage},
			},
		},
		// org/kwis/msp/lcdui/Font reports the runtime-owned bitmap font metrics
		// shared with the MIDP path.
		"org/kwis/msp/lcdui/Font": {
			name:        "org/kwis/msp/lcdui/Font",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/lcdui/Font", name: "getFont", descriptor: "(III)Lorg/kwis/msp/lcdui/Font;", accessFlags: 0x0009, implementation: runtimeFontGetFont},
				{class: "org/kwis/msp/lcdui/Font", name: "getDefaultFont", descriptor: "()Lorg/kwis/msp/lcdui/Font;", accessFlags: 0x0009, implementation: runtimeFontGetFont},
				{class: "org/kwis/msp/lcdui/Font", name: "getHeight", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFontHeight},
				{class: "org/kwis/msp/lcdui/Font", name: "getBaselinePosition", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFontBaseline},
				{class: "org/kwis/msp/lcdui/Font", name: "stringWidth", descriptor: "(Ljava/lang/String;)I", accessFlags: 0x0001, implementation: runtimeFontStringWidth},
				{class: "org/kwis/msp/lcdui/Font", name: "charWidth", descriptor: "(C)I", accessFlags: 0x0001, implementation: runtimeFontCharWidth},
				{class: "org/kwis/msp/lcdui/Font", name: "charsWidth", descriptor: "([CII)I", accessFlags: 0x0001, implementation: runtimeFontCharsWidth},
				{class: "org/kwis/msp/lcdui/Font", name: "substringWidth", descriptor: "(Ljava/lang/String;II)I", accessFlags: 0x0001, implementation: runtimeFontSubstringWidth},
				{class: "org/kwis/msp/lcdui/Font", name: "getFace", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFontAttribute("face:I")},
				{class: "org/kwis/msp/lcdui/Font", name: "getStyle", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFontAttribute("style:I")},
				{class: "org/kwis/msp/lcdui/Font", name: "getSize", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeFontAttribute("size:I")},
				{class: "org/kwis/msp/lcdui/Font", name: "isPlain", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeFontStyleFlag(0)},
				{class: "org/kwis/msp/lcdui/Font", name: "isBold", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeFontStyleFlag(fontStyleBold)},
				{class: "org/kwis/msp/lcdui/Font", name: "isItalic", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeFontStyleFlag(fontStyleItalic)},
				{class: "org/kwis/msp/lcdui/Font", name: "isUnderlined", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeFontStyleFlag(fontStyleUnderlined)},
			},
			// The face, size and style constants a title passes to getFont.
			// They are read as statics rather than compiled in, so a class
			// without them stops a title at its first getFont call — which is
			// during startup for one that picks its font before it draws.
			fields: fontConstantFields(),
		},
		// org/kwis/msp/media/Clip stores its type and state; audio output arrives
		// with the sound path.
		"org/kwis/msp/media/BaseClip": {
			name:        "org/kwis/msp/media/BaseClip",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/media/BaseClip", name: "<init>", descriptor: "()V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/media/BaseClip", name: "putData", descriptor: "([BII)I", accessFlags: 0x0001, implementation: runtimeClipPutData},
				{class: "org/kwis/msp/media/BaseClip", name: "availableDataSize", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeClipAvailableData},
				{class: "org/kwis/msp/media/BaseClip", name: "clearData", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeClipClearData},
				// setBuffer is Clip's, moved up to the base class by the handset
				// runtime a title was built against, where it reports whether the
				// data was taken instead of returning nothing.
				{class: "org/kwis/msp/media/BaseClip", name: "setBuffer", descriptor: "([BI)Z", accessFlags: 0x0001, implementation: runtimeClipSetBufferChecked},
			},
		},
		"org/kwis/msp/media/Clip": {
			name:        "org/kwis/msp/media/Clip",
			superName:   "org/kwis/msp/media/BaseClip",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/media/Clip", name: "<init>", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeClipConstructor},
				{class: "org/kwis/msp/media/Clip", name: "<init>", descriptor: "(Ljava/lang/String;[B)V", accessFlags: 0x0001, implementation: runtimeClipConstructor},
				{class: "org/kwis/msp/media/Clip", name: "<init>", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeClipConstructor},
				{class: "org/kwis/msp/media/Clip", name: "setVolume", descriptor: "(I)Z", accessFlags: 0x0001, implementation: runtimeClipSetVolume},
				{class: "org/kwis/msp/media/Clip", name: "getVolume", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("volume:I", 0)},
				{class: "org/kwis/msp/media/Clip", name: "setBuffer", descriptor: "([BI)V", accessFlags: 0x0001, implementation: runtimeClipSetBuffer},
				{class: "org/kwis/msp/media/Clip", name: "setPosition", descriptor: "(I)Z", accessFlags: 0x0001, implementation: runtimeClipSetPosition},
				{class: "org/kwis/msp/media/Clip", name: "getPosition", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("position:I", 0)},
				{class: "org/kwis/msp/media/Clip", name: "setListener", descriptor: "(Lorg/kwis/msp/media/PlayListener;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/media/Clip", name: "<init>", descriptor: "(Ljava/lang/String;Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeClipConstructor},
				{class: "org/kwis/msp/media/Clip", name: "getType", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeClipGetType},
				{class: "org/kwis/msp/media/Clip", name: "setStopTime", descriptor: "(I)Z", accessFlags: 0x0001, implementation: runtimeClipSetStopTime},
				{class: "org/kwis/msp/media/Clip", name: "getStopTime", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("stopTime:I", 0)},
			},
		},
		"org/kwis/msp/media/Player": {
			name:        "org/kwis/msp/media/Player",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/media/Player", name: "play", descriptor: "(Lorg/kwis/msp/media/BaseClip;Z)Z", accessFlags: 0x0009, implementation: runtimePlayerPlay},
				{class: "org/kwis/msp/media/Player", name: "play", descriptor: "(Lorg/kwis/msp/media/Clip;Z)Z", accessFlags: 0x0009, implementation: runtimePlayerPlay},
				{class: "org/kwis/msp/media/Player", name: "stop", descriptor: "(Lorg/kwis/msp/media/BaseClip;)Z", accessFlags: 0x0009, implementation: runtimePlayerStop},
				{class: "org/kwis/msp/media/Player", name: "stop", descriptor: "(Lorg/kwis/msp/media/Clip;)Z", accessFlags: 0x0009, implementation: runtimePlayerStop},
				{class: "org/kwis/msp/media/Player", name: "pause", descriptor: "(Lorg/kwis/msp/media/BaseClip;)Z", accessFlags: 0x0009, implementation: runtimePlayerPause},
				{class: "org/kwis/msp/media/Player", name: "resume", descriptor: "(Lorg/kwis/msp/media/BaseClip;)Z", accessFlags: 0x0009, implementation: runtimePlayerResume},
				// The specification declares the whole of Player against Clip
				// as well as against BaseClip, and play and stop already had
				// both. A title that lowers the music for a cut scene and
				// brings it back uses the pair, so the two that were missing
				// stopped it at the bring-it-back rather than at the lower.
				{class: "org/kwis/msp/media/Player", name: "pause", descriptor: "(Lorg/kwis/msp/media/Clip;)Z", accessFlags: 0x0009, implementation: runtimePlayerPause},
				{class: "org/kwis/msp/media/Player", name: "resume", descriptor: "(Lorg/kwis/msp/media/Clip;)Z", accessFlags: 0x0009, implementation: runtimePlayerResume},
				// Recording needs a microphone the emulator does not offer, so
				// it is refused rather than accepted and silently ignored.
				{class: "org/kwis/msp/media/Player", name: "record", descriptor: "(Lorg/kwis/msp/media/BaseClip;)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
			},
		},
		"org/kwis/msp/media/Volume": {
			name:        "org/kwis/msp/media/Volume",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/media/Volume", name: "set", descriptor: "(I)V", accessFlags: 0x0009, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/media/Volume", name: "get", descriptor: "()I", accessFlags: 0x0009, implementation: runtimeComponentZero},
				{class: "org/kwis/msp/media/Volume", name: "setMute", descriptor: "(IZ)V", accessFlags: 0x0009, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/media/Volume", name: "getMute", descriptor: "(I)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
				{class: "org/kwis/msp/media/Volume", name: "getDefaultVolume", descriptor: "(I)I", accessFlags: 0x0009, implementation: runtimeComponentZero},
				{class: "org/kwis/msp/media/Volume", name: "setDefaultVolume", descriptor: "(II)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
			},
		},
		// org/kwis/msp/db/DataBase currently keeps records in process memory so
		// constructors that open stores can complete; durable persistence arrives
		// with the Host save path.
		"org/kwis/msp/db/DataBase": {
			name:        "org/kwis/msp/db/DataBase",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/db/DataBase", name: "openDataBase", descriptor: "(Ljava/lang/String;IZ)Lorg/kwis/msp/db/DataBase;", accessFlags: 0x0009, implementation: runtimeOpenDataBase},
				{class: "org/kwis/msp/db/DataBase", name: "openDataBase", descriptor: "(Ljava/lang/String;IZI)Lorg/kwis/msp/db/DataBase;", accessFlags: 0x0009, implementation: runtimeOpenDataBase},
				{class: "org/kwis/msp/db/DataBase", name: "getNumberOfRecords", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDataBaseCount},
				{class: "org/kwis/msp/db/DataBase", name: "closeDataBase", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: "org/kwis/msp/db/DataBase", name: "insertRecord", descriptor: "([B)I", accessFlags: 0x0001, implementation: runtimeDataBaseInsert},
				{class: "org/kwis/msp/db/DataBase", name: "insertRecord", descriptor: "([BII)I", accessFlags: 0x0001, implementation: runtimeDataBaseInsert},
				{class: "org/kwis/msp/db/DataBase", name: "selectRecord", descriptor: "(I)[B", accessFlags: 0x0001, implementation: runtimeDataBaseSelect},
				{class: "org/kwis/msp/db/DataBase", name: "updateRecord", descriptor: "(I[B)V", accessFlags: 0x0001, implementation: runtimeDataBaseUpdate},
				{class: "org/kwis/msp/db/DataBase", name: "deleteRecord", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeDataBaseDelete},
				{class: runtimeDataBaseClass, name: "selectRecord", descriptor: "(I[BI)V", accessFlags: 0x0001, implementation: runtimeDataBaseSelectInto},
				{class: runtimeDataBaseClass, name: "updateRecord", descriptor: "(I[BII)V", accessFlags: 0x0001, implementation: runtimeDataBaseUpdateRange},
				{class: runtimeDataBaseClass, name: "sortRecord", descriptor: "(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I", accessFlags: 0x0001, implementation: runtimeDataBaseSortRecord},
				{class: runtimeDataBaseClass, name: "getDataBaseName", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeDataBaseName},
				{class: runtimeDataBaseClass, name: "getDataBaseSize", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDataBaseSize},
				{class: runtimeDataBaseClass, name: "getRecordSize", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDataBaseRecordSize},
				{class: runtimeDataBaseClass, name: "getSizeAvailable", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDataBaseSizeAvailable},
				{class: runtimeDataBaseClass, name: "getLastModified", descriptor: "()J", accessFlags: 0x0001, implementation: runtimeDataBaseLastModified},
				{class: runtimeDataBaseClass, name: "listDataBases", descriptor: "()[Ljava/lang/String;", accessFlags: 0x0009, implementation: runtimeDataBaseListNames},
				{class: runtimeDataBaseClass, name: "deleteDataBase", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeDataBaseDeleteStore},
				{class: runtimeDataBaseClass, name: "deleteDataBase", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0009, implementation: runtimeDataBaseDeleteStore},
				{class: runtimeDataBaseClass, name: "getAccessMode", descriptor: "(Ljava/lang/String;)I", accessFlags: 0x0009, implementation: runtimeDataBaseAccessMode},
			},
		},
		// org/kwis/msp/handset/HandsetProperty answers the original runtime's
		// accepted empty property table, with the vibrator level pinned to zero.
		"org/kwis/msp/handset/HandsetProperty": {
			name:        "org/kwis/msp/handset/HandsetProperty",
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{class: "org/kwis/msp/handset/HandsetProperty", name: "getSystemProperty", descriptor: "(Ljava/lang/String;)Ljava/lang/String;", accessFlags: 0x0009, implementation: runtimeGetSystemProperty},
				{class: "org/kwis/msp/handset/HandsetProperty", name: "setSystemProperty", descriptor: "(Ljava/lang/String;Ljava/lang/String;)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
			},
		},
		runtimeAnnunciatorClass: {
			name:        runtimeAnnunciatorClass,
			superName:   "org/kwis/msp/lwc/ShellComponent",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{
					class:          runtimeAnnunciatorClass,
					name:           "<init>",
					descriptor:     "(Z)V",
					accessFlags:    0x0001,
					implementation: runtimeAnnunciatorConstructor,
				},
				{
					class:          runtimeAnnunciatorClass,
					name:           "show",
					descriptor:     "()V",
					accessFlags:    0x0001,
					implementation: runtimeAnnunciatorShow,
				},
				// The annunciator is drawn by the Host rather than laid out
				// here, so laying its children out is nothing to do. It is on
				// the class because a title calls it after showing the bar.
				{class: runtimeAnnunciatorClass, name: "layout", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			},
		},
		runtimeDisplayClass: {
			name:        runtimeDisplayClass,
			superName:   "java/lang/Object",
			accessFlags: 0x0021,
			methods: []runtimeJavaMethod{
				{
					class:          runtimeDisplayClass,
					name:           "getDefaultDisplay",
					descriptor:     "()Lorg/kwis/msp/lcdui/Display;",
					accessFlags:    0x0009,
					implementation: runtimeGetDefaultDisplay,
				},
				{
					class:          runtimeDisplayClass,
					name:           "getDisplay",
					descriptor:     "(Ljava/lang/String;)Lorg/kwis/msp/lcdui/Display;",
					accessFlags:    0x0009,
					implementation: runtimeGetNamedDisplay,
				},
				{class: runtimeDisplayClass, name: "getWidth", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDisplayWidth},
				{class: runtimeDisplayClass, name: "getHeight", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDisplayHeight},
				{class: runtimeDisplayClass, name: "isDoubleBuffered", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeDisplayTrue},
				{class: runtimeDisplayClass, name: "isColor", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeDisplayTrue},
				{class: runtimeDisplayClass, name: "numColors", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDisplayNumColors},
				// A touch reaches the guest now: a Host sends one, the session
				// carries it, and Client.SendPointer dispatches it down the card
				// stack or into the title's own event queue. Until it did, the
				// honest answer here was false — a game told it has a
				// touchscreen and then never given a touch is worse off than
				// one told the truth — and the answer changed with the delivery
				// rather than before it.
				//
				// Motion is answered the same way because the same call
				// carries it: POINT_DRAGGED is one of the three event types
				// SendPointer takes, so a title that asks whether it may
				// follow a finger is being told something true.
				{class: runtimeDisplayClass, name: "hasPointerEvents", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeDisplayTrue},
				{class: runtimeDisplayClass, name: "hasPointerMotionEvents", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeDisplayTrue},
				{class: runtimeDisplayClass, name: "hasRepeatEvents", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeDisplayTrue},
				{class: runtimeDisplayClass, name: "getBitsPerPixel", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDisplayBitsPerPixel},
				{class: runtimeDisplayClass, name: "getDockedCard", descriptor: "()Lorg/kwis/msp/lcdui/Card;", accessFlags: 0x0001, implementation: runtimeDisplayDockedCard},
				{class: runtimeDisplayClass, name: "pushCard", descriptor: "(Lorg/kwis/msp/lcdui/Card;)V", accessFlags: 0x0001, implementation: runtimeDisplayPushCard},
				{class: runtimeDisplayClass, name: "popCard", descriptor: "()Lorg/kwis/msp/lcdui/Card;", accessFlags: 0x0001, implementation: runtimeDisplayPopCard},
				{class: runtimeDisplayClass, name: "removeCard", descriptor: "(Lorg/kwis/msp/lcdui/Card;)Z", accessFlags: 0x0001, implementation: runtimeDisplayRemoveCard},
				{class: runtimeDisplayClass, name: "countCard", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDisplayCountCard},
				{class: runtimeDisplayClass, name: "removeAllCards", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeDisplayRemoveAllCards},
				{class: runtimeDisplayClass, name: "addJletEventListener", descriptor: "(Lorg/kwis/msp/lcdui/JletEventListener;)V", accessFlags: 0x0001, implementation: runtimeDisplayAddListener},
				{class: runtimeDisplayClass, name: "flush", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
				{class: runtimeDisplayClass, name: "callSerially", descriptor: "(Ljava/lang/Runnable;)V", accessFlags: 0x0001, implementation: runtimeDisplayCallSerially},
				{class: runtimeDisplayClass, name: "callSerially", descriptor: "(Ljava/lang/Runnable;I)V", accessFlags: 0x0001, implementation: runtimeDisplayCallSerially},
				{class: runtimeDisplayClass, name: "getGameAction", descriptor: "(I)I", accessFlags: 0x0009, implementation: runtimeDisplayGameAction},
				{class: runtimeDisplayClass, name: "getKeyCode", descriptor: "(I)I", accessFlags: 0x0009, implementation: runtimeDisplayKeyCode},
				{class: runtimeDisplayClass, name: "getKeyName", descriptor: "(I)Ljava/lang/String;", accessFlags: 0x0009, implementation: runtimeDisplayKeyName},
				{class: runtimeDisplayClass, name: "removeJletEventListener", descriptor: "(Lorg/kwis/msp/lcdui/JletEventListener;)V", accessFlags: 0x0001, implementation: runtimeDisplayRemoveListener},
				{class: runtimeDisplayClass, name: "grabKey", descriptor: "(ILorg/kwis/msp/lcdui/JletEventListener;)V", accessFlags: 0x0001, implementation: runtimeDisplayGrabKey},
				{class: runtimeDisplayClass, name: "ungrabKey", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeDisplayUngrabKey},
				{class: runtimeDisplayClass, name: "setDockedCard", descriptor: "(Lorg/kwis/msp/lcdui/Card;I)V", accessFlags: 0x0001, implementation: runtimeDisplaySetDockedCard},
			},
		},
		"java/lang/Throwable": runtimeThrowableClassDefinition(),
		// The runtime's own Enumeration over a snapshot, which is what a
		// Hashtable's two views are handed back as. A title never names it,
		// but it calls the interface methods on the object it was given, so
		// the class the object carries has to publish them.
		"net/wfeature/ArrayEnumeration": {
			name:        "net/wfeature/ArrayEnumeration",
			superName:   "java/lang/Object",
			accessFlags: 0x0031,
			methods: []runtimeJavaMethod{
				{class: "net/wfeature/ArrayEnumeration", name: "hasMoreElements", descriptor: "()Z", accessFlags: 0x0001},
				{class: "net/wfeature/ArrayEnumeration", name: "nextElement", descriptor: "()Ljava/lang/Object;", accessFlags: 0x0001},
			},
		},
		runtimeLEDClass:                runtimeLEDClassDefinition(),
		runtimeLabelComponentClass:     runtimeLabelComponentClassDefinition(),
		runtimeOEMDeviceClass:          runtimeOEMDeviceClassDefinition(),
		runtimeSYSThemeClass:           runtimeSYSThemeClassDefinition(),
		runtimeEventQueueClass:         runtimeEventQueueClassDefinition(),
		runtimeInputMethodHandlerClass: runtimeInputMethodHandlerClassDefinition(),
		runtimeMainClass:               runtimeMainClassDefinition(),
		runtimeTextComponentClass:      runtimeTextComponentClassDefinition(),
		runtimeTextFieldComponentClass: runtimeTextFieldComponentClassDefinition(),
		runtimeTextBoxComponentClass:   runtimeTextBoxComponentClassDefinition(),
		runtimeVibratorClass:           runtimeVibratorClassDefinition(),
		runtimeDialogComponentClass:    runtimeDialogComponentClassDefinition(),
		runtimeFormComponentClass:      runtimeFormComponentClassDefinition(),
		runtimeProgressComponentClass:  runtimeProgressComponentClassDefinition(),
		// The vendor's own widget toolkit, which one title uses for a single
		// text-entry dialog; see runtime_kfc.go.
		runtimeGFormClass:             runtimeGFormClassDefinition(runtimeGFormClass, runtimeShellComponentClass),
		runtimeGMenubarFormClass:      runtimeGFormClassDefinition(runtimeGMenubarFormClass, runtimeGFormClass),
		runtimeGMsgBoxClass:           runtimeGFormClassDefinition(runtimeGMsgBoxClass, runtimeGFormClass),
		runtimeGTextFieldClass:        runtimeGTextFieldClassDefinition(),
		runtimeGTextListenerClass:     runtimeGTextListenerClassDefinition(),
		runtimeNetworkClass:           runtimeNetworkClassDefinition(),
		runtimeURLClass:               runtimeURLClassDefinition(),
		runtimeDataBaseExceptionClass: runtimeExceptionClass(runtimeDataBaseExceptionClass, "java/lang/Exception"),
		runtimeDataBaseRecordClass:    runtimeExceptionClass(runtimeDataBaseRecordClass, runtimeDataBaseExceptionClass),
		runtimeSchemeExceptionClass:   runtimeExceptionClass(runtimeSchemeExceptionClass, "java/io/IOException"),
		// The runtime-owned interfaces exist so a guest that implements or
		// references one resolves it. Their methods dispatch by name on the
		// receiver, because an AOT class carries no interface list to link
		// vtable slots against.
		runtimeJletListenerClass: runtimeInterfaceClass(runtimeJletListenerClass,
			runtimeJavaMethod{name: "notifyEvent", descriptor: "(III)V"}),
		runtimeImageObserverClass: runtimeInterfaceClass(runtimeImageObserverClass,
			runtimeJavaMethod{name: "notify", descriptor: "(Lorg/kwis/msp/lcdui/Image;I)V"}),
		runtimeEventListenerClass: runtimeInterfaceClass(runtimeEventListenerClass,
			runtimeJavaMethod{name: "eventNotify", descriptor: "(IIIILjava/lang/Object;)Z"}),
		runtimeDataComparatorClass: runtimeInterfaceClass(runtimeDataComparatorClass,
			runtimeJavaMethod{name: "compare", descriptor: "([B[B)I"}),
		runtimeDataFilterClass: runtimeInterfaceClass(runtimeDataFilterClass,
			runtimeJavaMethod{name: "filter", descriptor: "([B)Z"}),
		runtimePlayListenerClass: runtimeInterfaceClass(runtimePlayListenerClass),
		// A Hashtable's two views are handed back as this interface, so a
		// title that walks one has to be able to resolve it.
		"java/util/Enumeration": runtimeInterfaceClass("java/util/Enumeration",
			runtimeJavaMethod{name: "hasMoreElements", descriptor: "()Z"},
			runtimeJavaMethod{name: "nextElement", descriptor: "()Ljava/lang/Object;"}),
	}
	// The exception hierarchy is published from the chain the JVM already
	// keeps, rather than a class at a time as titles turn up needing one. A
	// name that is not here does not fail to resolve — it gets the fallback
	// record, whose parent is Object and whose methods are Object's — and
	// that is the trap: `new Exception()` then resolves to `Object.<init>`,
	// `getMessage` on what a title caught resolves to nothing, and the title
	// stops inside its own error handling with no sign of what it was
	// handling. Declaring the chain costs one record each and makes the
	// records say what the `catch` matching has always said.
	for name, parent := range jvm.ThrowableParents() {
		if _, published := runtimeJavaClasses[name]; published {
			continue
		}
		runtimeJavaClasses[name] = runtimeExceptionClass(name, parent)
	}
}

// The handset these games were written for. It is the default rather than the
// rule: a Host may name another screen, and Client.SetScreen says what that
// changes and what it does not.
const (
	runtimeDisplayPixelWidth  = 240
	runtimeDisplayPixelHeight = 320
)

// runtimeScreenWidth and runtimeScreenHeight are the screen the Java surface
// answers with. They read the same size the WIPI-C surface does, so a game
// that asks Display and a game that asks MC_grpGetDisplayInfo are told the
// same thing.
func runtimeScreenWidth(runtime *initializationRuntime) int32 {
	width, _ := runtime.screenSize()
	return int32(width)
}

func runtimeScreenHeight(runtime *initializationRuntime) int32 {
	_, height := runtime.screenSize()
	return int32(height)
}

func runtimeGetNamedDisplay(runtime *initializationRuntime, vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return runtimeGetDefaultDisplay(runtime, vm, nil)
}

func runtimeDisplayWidth(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(runtimeScreenWidth(runtime)), nil
}

func runtimeDisplayHeight(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(runtimeScreenHeight(runtime)), nil
}

// runtimeDisplayCallSerially queues a Runnable for the Host event loop, which
// runs it like a started guest thread's run() — but on an idle pass rather
// than immediately. The timeout variant ignores its delay like the original
// runtime does.
//
// The distinction from Thread.start is the whole point and it is not a detail.
// A started thread is runnable now. A serial Runnable runs when the event loop
// next has nothing else to do, and the original loop takes one off the queue
// per pass and then waits out a frame. Games rely on that wait: one title's
// entire frame loop is a Runnable that re-queues itself, reads the clock, and
// returns without drawing until its interval is up. Dispatched immediately it
// re-queues immediately, so the guest is never idle, the Host never sleeps,
// and a core burns to produce the same twenty frames a second.
func runtimeDisplayCallSerially(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("Display.callSerially expected a Runnable, got %d arguments", len(arguments))
	}
	runnable, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if runnable == nil {
		return jvm.VoidValue(), nil
	}
	if len(runtime.pendingSerial) >= maxPendingThreads {
		return jvm.VoidValue(), fmt.Errorf("KTF pending serial runnable count exceeds %d", maxPendingThreads)
	}
	runtime.countDiagnostic("callSerially " + runnable.ClassName)
	runtime.pendingSerial = append(runtime.pendingSerial, runnable)
	return jvm.VoidValue(), nil
}

// runtimeDisplayGameAction maps a WIPI key code to its game action with the
// original Display::getGameAction table; unmapped keys report themselves.
func runtimeDisplayGameAction(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Display.getGameAction expected a key code, got %d arguments", len(arguments))
	}
	key, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch key {
	case KeyUp:
		return jvm.IntValue(1), nil
	case KeyDown:
		return jvm.IntValue(6), nil
	case KeyLeft:
		return jvm.IntValue(2), nil
	case KeyRight:
		return jvm.IntValue(5), nil
	case KeyFire:
		return jvm.IntValue(8), nil
	case KeyClear:
		return jvm.IntValue(99), nil
	}
	return jvm.IntValue(key), nil
}

// runtimeDisplayKeyCode reverses game actions to WIPI key codes with the
// original Display::getKeyCode table; unmapped actions report zero.
func runtimeDisplayKeyCode(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Display.getKeyCode expected a game action, got %d arguments", len(arguments))
	}
	action, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch action {
	case 1:
		return jvm.IntValue(KeyUp), nil
	case 2:
		return jvm.IntValue(KeyLeft), nil
	case 5:
		return jvm.IntValue(KeyRight), nil
	case 6:
		return jvm.IntValue(KeyDown), nil
	case 8:
		return jvm.IntValue(KeyFire), nil
	case 90:
		return jvm.IntValue(KeyLeftSoft), nil
	case 91:
		return jvm.IntValue(KeyRightSoft), nil
	case 92:
		return jvm.IntValue(-8), nil
	case 96:
		return jvm.IntValue(KeyVolumeUp), nil
	case 97:
		return jvm.IntValue(KeyVolumeDown), nil
	case 98:
		return jvm.IntValue(-15), nil
	case 99:
		return jvm.IntValue(KeyClear), nil
	}
	return jvm.IntValue(0), nil
}

func runtimeDisplayTrue(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(1), nil
}

func runtimeDisplayNumColors(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(1 << 16), nil
}

func runtimeDisplayBitsPerPixel(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(16), nil
}

func runtimeDisplayPushCard(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Display.pushCard expected receiver and card, got %d arguments", len(arguments))
	}
	card, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if card == nil {
		return jvm.VoidValue(), fmt.Errorf("Display.pushCard card is null")
	}
	previous := runtime.topCard()
	runtime.displayCards = append(runtime.displayCards, card)
	return jvm.VoidValue(), runtime.notifyCardShown(previous, card)
}

func runtimeDisplayPopCard(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	if len(runtime.displayCards) == 0 {
		return jvm.ReferenceValue(nil), nil
	}
	card := runtime.displayCards[len(runtime.displayCards)-1]
	runtime.displayCards = runtime.displayCards[:len(runtime.displayCards)-1]
	return jvm.ReferenceValue(card), runtime.notifyCardShown(card, runtime.topCard())
}

// topCard is the card the display is showing, which on this platform is the
// top of the pushed stack: `Card.isShown`, `repaintQueued` and `paintTopCard`
// all already answer against it.
func (runtime *initializationRuntime) topCard() *jvm.Object {
	if len(runtime.displayCards) == 0 {
		return nil
	}
	return runtime.displayCards[len(runtime.displayCards)-1]
}

// notifyCardShown tells the cards on either side of a change of top what
// happened. The specification puts it on the stack operations rather than on
// the paint — "카드가 pushCard, popCard에 의해서 보여지거나, 보이지 않게되는
// 경우에 showNotify라는 함수가 불립니다" — and one local title does all of its
// own initialization there. Its card's `showNotify(true)` is the only caller
// of the method that fills a field every one of its frames divides by, so
// without this it threw an `ArithmeticException` twice per frame inside
// `paint` and drew a black screen for as long as it was left running. See
// docs/ktf.md, "The ninth round: a card that was never told it was on the
// screen".
//
// Which card counts as shown is this runtime's existing rule rather than a new
// one: the top of the pushed stack, the same answer `Card.isShown` gives. So a
// card that is covered is hidden and a card that is uncovered is shown, and a
// push onto the card already on top notifies nobody.
func (runtime *initializationRuntime) notifyCardShown(hidden, shown *jvm.Object) error {
	if hidden == shown {
		return nil
	}
	if hidden != nil {
		if err := runtime.invokeShowNotify(hidden, false); err != nil {
			return err
		}
	}
	if shown != nil {
		return runtime.invokeShowNotify(shown, true)
	}
	return nil
}

func (runtime *initializationRuntime) invokeShowNotify(card *jvm.Object, shown bool) error {
	value := int32(0)
	if shown {
		value = 1
	}
	if _, err := runtime.client.vm.InvokeVirtual(card, "showNotify", "(Z)V", jvm.IntValue(value)); err != nil {
		return fmt.Errorf("KTF card %s showNotify(%t): %w", card.ClassName, shown, err)
	}
	return nil
}

func runtimeDisplayRemoveCard(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Display.removeCard expected receiver and card, got %d arguments", len(arguments))
	}
	card, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	for index, current := range runtime.displayCards {
		if current == card {
			previous := runtime.topCard()
			runtime.displayCards = append(runtime.displayCards[:index], runtime.displayCards[index+1:]...)
			return jvm.IntValue(1), runtime.notifyCardShown(previous, runtime.topCard())
		}
	}
	return jvm.IntValue(0), nil
}

func runtimeDisplayCountCard(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(int32(len(runtime.displayCards))), nil
}

func runtimeDisplayRemoveAllCards(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	previous := runtime.topCard()
	runtime.displayCards = nil
	return jvm.VoidValue(), runtime.notifyCardShown(previous, nil)
}

func runtimeBackLightNoop(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

func runtimeComponentNoop(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// runtimePrintStreamText keeps the line a title writes to System.out. It is
// the title's own commentary on what it is doing, which is the first evidence
// worth having when a run stops making progress, so it goes to the same place
// and the same level as a WIPI-C printk rather than being discarded. The
// numeric and character overloads stay silent: a title writes those a
// character at a time and the line they belong to is the one above.
func runtimePrintStreamText(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 2 {
		return jvm.VoidValue(), nil
	}
	text, err := arguments[1].Reference()
	if err != nil || text == nil {
		return jvm.VoidValue(), nil
	}
	line, ok := jvm.StringText(text)
	if !ok {
		return jvm.VoidValue(), nil
	}
	runtime.client.guestPrint(line)
	return jvm.VoidValue(), nil
}

const maxPendingThreads = 64

// serialDispatchInterval is how long the event loop waits between serial
// dispatches. It is one frame at sixty hertz, which is what the original loop
// sleeps after taking a Runnable off the queue, and it is the only thing that
// paces a Runnable that re-queues itself.
const serialDispatchInterval = 16 * time.Millisecond

// traceIdentityComparison records what an identity-based comparison actually
// received. java/lang/Object.equals is reference equality, so a guest table
// lookup built on it only works while one guest object maps to exactly one
// JVM object. When that breaks, the lookup silently misses and the guest takes
// its not-found path — which is invisible unless the operands are recorded.
func (runtime *initializationRuntime) traceIdentityComparison(method runtimeJavaMethod, receiver *jvm.Object, arguments []jvm.Value) {
	if method.name != "equals" || method.class != "java/lang/Object" || len(arguments) != 1 {
		return
	}
	other, err := arguments[0].Reference()
	if err != nil {
		return
	}
	runtime.countDiagnostic(fmt.Sprintf("equals %s vs %s -> %t",
		describeIdentity(receiver), describeIdentity(other), receiver == other))
}

// traceStringConversion records what String.getBytes actually produced. The
// game parses these byte arrays and indexes the result, so a conversion that
// yields fewer bytes than the text implies — above all an empty array for a
// non-empty string — is the runtime handing the guest something to overrun.
func (runtime *initializationRuntime) traceStringConversion(method runtimeJavaMethod, receiver *jvm.Object, result jvm.Value) {
	if method.name != "getBytes" || method.class != "java/lang/String" {
		return
	}
	text, ok := receiver.Native.(string)
	if !ok {
		return
	}
	length := -1
	if object, err := result.Reference(); err == nil && object != nil {
		if _, values, arrayErr := jvm.ArraySnapshot(object); arrayErr == nil {
			length = len(values)
		}
	}
	if length <= 0 && text != "" {
		runtime.countDiagnostic(fmt.Sprintf("getBytes empty for %q (%d chars)", text, len([]rune(text))))
		return
	}
	runtime.countDiagnostic(fmt.Sprintf("getBytes %d chars -> %d bytes", len([]rune(text)), length))
}

// describeIdentity names one operand of an identity comparison: its class, the
// Go identity that equality actually tests, and its text when it is a string,
// because two strings that read the same but compare false is the signature of
// a broken binding.
func describeIdentity(object *jvm.Object) string {
	if object == nil {
		return "null"
	}
	if text, ok := object.Native.(string); ok {
		return fmt.Sprintf("%s(%p)%q", object.ClassName, object, text)
	}
	return fmt.Sprintf("%s(%p)", object.ClassName, object)
}

// runtimeThreadCurrent answers the Thread the caller is running on. A guest
// worker carries the Thread object it was started from, so the answer is that
// one; a call from the client thread — startApp, a timer, a paint — is not on
// any guest thread, and gets one runtime-owned Thread that stands for it. What
// a title does with the result is compare it against a thread it started, so
// answering the client thread with a distinct object is the honest answer as
// well as the only one available.
func runtimeThreadCurrent(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	if worker := runtime.client.activeWorker; worker != nil && worker.javaThread != nil {
		return jvm.ReferenceValue(worker.javaThread), nil
	}
	thread := runtime.runtimeObjects["java/lang/Thread"]
	if thread == nil {
		thread = &jvm.Object{ClassName: "java/lang/Thread", Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects["java/lang/Thread"] = thread
	}
	return jvm.ReferenceValue(thread), nil
}

func runtimeThreadSleep(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Thread.sleep expected milliseconds, got %d arguments", len(arguments))
	}
	milliseconds, err := arguments[0].Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if milliseconds > 0 {
		// A sleeping guest thread ends its slice: the worker parks so the Host
		// can run timers and paint, and becomes eligible again once the sleep
		// has actually elapsed. Honouring the length is what holds a frame
		// loop to the rate the game chose.
		if err := runtime.sleepCurrentWorker(time.Duration(milliseconds) * time.Millisecond); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

// waitSliceWithoutTimeout is how long an untimed wait parks before returning.
// A guest thread that parks forever never comes back to be woken, because
// nothing on the KTF side records who is waiting on what.
const waitSliceWithoutTimeout = 50 * time.Millisecond

// runtimeObjectWait parks the calling guest worker instead of taking the JVM's
// monitor path.
//
// KTF answers the guest's monitorenter and monitorexit primitives with an
// accepted no-op — one guest core running cooperatively has no contention to
// arbitrate — so no thread ever owns a monitor as far as the JVM is concerned.
// Routing wait to the JVM builtin therefore did not park anything: it checked
// an ownership that KTF never establishes and raised
// IllegalMonitorStateException on the first wait a game made, which is how a
// game's loader thread died before it drew a pixel.
//
// Parking for the timeout is what the caller wanted, and returning early is
// something wait() is allowed to do at any time: a spurious wakeup is part of
// its contract, and every correct caller re-tests its condition in a loop. So
// an untimed wait parks for a slice and comes back rather than parking for
// good, which keeps a guest that is waiting on a condition another guest
// thread will satisfy from stopping the session dead.
func runtimeObjectWait(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) == 0 {
		return jvm.VoidValue(), fmt.Errorf("Object.wait expected a receiver")
	}
	wait := waitSliceWithoutTimeout
	if len(arguments) > 1 {
		milliseconds, err := arguments[1].Int64()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if milliseconds < 0 {
			const message = "negative wait timeout"
			return jvm.VoidValue(), &jvm.GuestException{
				Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: message},
				Message: message,
			}
		}
		// wait(0) means "no timeout" rather than "do not wait".
		if milliseconds > 0 {
			wait = time.Duration(milliseconds) * time.Millisecond
		}
	}
	return jvm.VoidValue(), runtime.sleepCurrentWorker(wait)
}

// runtimeObjectNotify accepts a notification without doing anything. Waiters
// return on their own — see runtimeObjectWait — so there is no queue here to
// release, and refusing the call would break a game that only ever notifies.
func runtimeObjectNotify(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) == 0 {
		return jvm.VoidValue(), fmt.Errorf("Object.notify expected a receiver")
	}
	return jvm.VoidValue(), nil
}

func runtimeComponentZero(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

// runtimeTotalMemory and runtimeFreeMemory report the platform data arena, the
// same heap MC_knlGetTotalMemory and MC_knlGetFreeMemory describe, through the
// one bounded view in arena.go.
func runtimeTotalMemory(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.LongValue(int64(runtime.arena.reportedTotal())), nil
}

func runtimeFreeMemory(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.LongValue(int64(runtime.arena.reportedFree())), nil
}

func runtimeImageCreateSized(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage expected width and height, got %d arguments", len(arguments))
	}
	width, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if width <= 0 || height <= 0 || width > 2048 || height > 2048 {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage size %dx%d is out of range", width, height)
	}
	// Mutable images back their pixels with a guest offscreen framebuffer so
	// Image.getGraphics drawing and Graphics.drawImage blitting share the
	// WIPI C pixel path. MIDP mutable images start out white.
	handle, err := runtime.newWIPICFramebufferRecord(uint32(width), uint32(height))
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.fillWIPICFramebuffer(handle, 0xffff); err != nil {
		return jvm.VoidValue(), err
	}
	image := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Image", Fields: map[string]jvm.Value{
		"width:I":            jvm.IntValue(width),
		"height:I":           jvm.IntValue(height),
		"mutable:Z":          jvm.IntValue(1),
		"guestFramebuffer:I": jvm.IntValue(int32(handle)),
	}}
	return jvm.ReferenceValue(image), nil
}

// runtimeImageFromEncoded decodes encoded image bytes into an Image object
// carrying its dimensions and decoded pixels.
func runtimeImageFromEncoded(runtime *initializationRuntime, data []byte) (jvm.Value, error) {
	decoded, err := decodeGuestImage(data)
	if err != nil {
		runtime.countDiagnostic(fmt.Sprintf("java image decode error %v len=%d", err, len(data)))
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: "undecodable image"},
			Message: "undecodable image",
		}
	}
	bounds := decoded.Bounds()
	image := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Image", Native: decoded, Fields: map[string]jvm.Value{
		"width:I":   jvm.IntValue(int32(bounds.Dx())),
		"height:I":  jvm.IntValue(int32(bounds.Dy())),
		"mutable:Z": jvm.IntValue(0),
	}}
	return jvm.ReferenceValue(image), nil
}

func runtimeImageCreateFromBytes(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage expected data, offset, and length, got %d arguments", len(arguments))
	}
	buffer, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if buffer == nil {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage data is null")
	}
	data, err := jvm.ByteArraySnapshot(buffer)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(data)) {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage range [%d, %d) is out of bounds", offset, offset+length)
	}
	return runtimeImageFromEncoded(runtime, data[offset:offset+length])
}

func runtimeImageCreateFromResource(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage expected resource name, got %d arguments", len(arguments))
	}
	nameObject, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("Image.createImage name is not a string")
	}
	data, exists := runtime.client.resources[strings.TrimPrefix(name, "/")]
	if !exists {
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/io/IOException", Native: "image not found: " + name},
			Message: "image not found: " + name,
		}
	}
	return runtimeImageFromEncoded(runtime, data)
}

func runtimeImageGetGraphics(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Image.getGraphics expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Image.getGraphics receiver is null")
	}
	graphics := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Graphics", Fields: map[string]jvm.Value{
		"img:Lorg/kwis/msp/lcdui/Image;": jvm.ReferenceValue(receiver),
	}}
	// A mutable image's Graphics draws into its guest offscreen framebuffer
	// through the same operations the screen framebuffer uses.
	if value, ok := receiver.Fields["guestFramebuffer:I"]; ok {
		handle, err := value.Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		target, err := runtime.readWIPICFramebuffer(uint32(handle))
		if err != nil {
			return jvm.VoidValue(), err
		}
		graphics.Native = &runtimeGraphicsState{
			target:     target,
			clipWidth:  int32(target.width),
			clipHeight: int32(target.height),
		}
	}
	return jvm.ReferenceValue(graphics), nil
}

// runtimeClipGetType answers the media type the clip was constructed with.
func runtimeClipGetType(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Clip.getType expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver != nil {
		if value, ok := receiver.Fields["type:Ljava/lang/String;"]; ok {
			return value, nil
		}
	}
	return jvm.ReferenceValue(vm.NewString("")), nil
}

// runtimeClipSetStopTime records the requested stop time and accepts it.
func runtimeClipSetStopTime(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Clip.setStopTime expected receiver and time, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Clip.setStopTime receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["stopTime:I"] = arguments[1]
	return jvm.IntValue(1), nil
}

// KTF font metrics follow the embedded NeoDGM pixel face, whose design grid
// Korean glyphs fill exactly; the runtime-authored 5x7 Latin glyphs sit on the
// same baseline. Which grid is the descriptor's answer — see fontFace — so the
// metrics are read off the face rather than fixed here. The glyph advance
// constant remains only as the fallback estimate for text that never reaches a
// renderer.
const runtimeFontGlyphAdvance = 6

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

func runtimeGraphicsGetFont(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Graphics.getFont expected receiver, got %d arguments", len(arguments))
	}
	return runtimeFontGetFont(runtime, vm, nil)
}

// Font styles match the original runtime's bit values; the renderer has one
// pixel face, so a style is recorded and reported rather than applied.
const (
	fontStyleBold       int32 = 1
	fontStyleItalic     int32 = 2
	fontStyleUnderlined int32 = 4
)

// fontConstantFields is the specification's Font constant set, values and all:
// faces at 0, 32 and 64, sizes at 8, 0 and 16, and the style bits above.
func fontConstantFields() []runtimeJavaField {
	constants := []struct {
		name  string
		value uint32
	}{
		{"FACE_SYSTEM", 0},
		{"FACE_MONOSPACE", 32},
		{"FACE_PROPORTIONAL", 64},
		{"SIZE_SMALL", 8},
		{"SIZE_MEDIUM", 0},
		{"SIZE_LARGE", 16},
		{"STYLE_PLAIN", 0},
		{"STYLE_BOLD", uint32(fontStyleBold)},
		{"STYLE_ITALIC", uint32(fontStyleItalic)},
		{"STYLE_UNDERLINED", uint32(fontStyleUnderlined)},
	}
	fields := make([]runtimeJavaField, 0, len(constants))
	for _, constant := range constants {
		value := constant.value
		fields = append(fields, runtimeJavaField{
			name:        constant.name,
			descriptor:  "I",
			accessFlags: 0x0019,
			initializer: func(*initializationRuntime) (uint32, error) { return value, nil },
		})
	}
	return fields
}

// runtimeFontGetFont answers the shared runtime font, recording the face,
// style, and size the caller asked for so the attribute queries report them
// back. There is one pixel face behind every request.
func runtimeFontGetFont(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	font := runtime.runtimeObjects["org/kwis/msp/lcdui/Font"]
	if font == nil {
		font = &jvm.Object{ClassName: "org/kwis/msp/lcdui/Font", Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects["org/kwis/msp/lcdui/Font"] = font
	}
	if len(arguments) == 3 {
		for index, name := range []string{"face:I", "style:I", "size:I"} {
			value, err := arguments[index].Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			font.Fields[name] = jvm.IntValue(value)
		}
	}
	return jvm.ReferenceValue(font), nil
}

func runtimeFontAttribute(key string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		if len(arguments) != 1 {
			return jvm.VoidValue(), fmt.Errorf("Font attribute accessor expected receiver, got %d arguments", len(arguments))
		}
		receiver, err := arguments[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if receiver != nil {
			if value, ok := receiver.Fields[key]; ok {
				return value, nil
			}
		}
		return jvm.IntValue(0), nil
	}
}

// runtimeFontStyleFlag answers one style query. A zero mask asks whether the
// font is plain, which is true exactly when no style bit is set.
func runtimeFontStyleFlag(mask int32) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		style, err := runtimeFontAttribute("style:I")(runtime, vm, arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		value, err := style.Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if mask == 0 {
			if value == 0 {
				return jvm.IntValue(1), nil
			}
			return jvm.IntValue(0), nil
		}
		if value&mask != 0 {
			return jvm.IntValue(1), nil
		}
		return jvm.IntValue(0), nil
	}
}

// runtimeFontCharsWidth measures a range of a character array with the same
// per-glyph advances the renderer draws with.
func runtimeFontCharsWidth(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 4 {
		return jvm.VoidValue(), fmt.Errorf("Font.charsWidth expected receiver, characters, offset, and length, got %d arguments", len(arguments))
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), fmt.Errorf("Font.charsWidth characters are null")
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := arguments[3].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return jvm.VoidValue(), fmt.Errorf("Font.charsWidth range [%d, %d) is out of bounds", offset, offset+length)
	}
	text := make([]rune, 0, length)
	for _, value := range values[offset : offset+length] {
		character, err := value.Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		text = append(text, rune(character))
	}
	return jvm.IntValue(runtime.graphicsTextWidth(text)), nil
}

// runtimeFontSubstringWidth measures part of a string with the renderer's own
// advances.
func runtimeFontSubstringWidth(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 4 {
		return jvm.VoidValue(), fmt.Errorf("Font.substringWidth expected receiver, text, offset, and length, got %d arguments", len(arguments))
	}
	textObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, _ := jvm.StringText(textObject)
	text := []rune(value)
	offset, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := arguments[3].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(text)) {
		return jvm.VoidValue(), fmt.Errorf("Font.substringWidth range [%d, %d) is out of bounds", offset, offset+length)
	}
	return jvm.IntValue(runtime.graphicsTextWidth(text[offset : offset+length])), nil
}

func runtimeFontHeight(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(runtime.fontHeight()), nil
}

func runtimeFontBaseline(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(runtime.fontBaseline()), nil
}

func runtimeFontStringWidth(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Font.stringWidth expected receiver and text, got %d arguments", len(arguments))
	}
	text, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, _ := jvm.StringText(text)
	return jvm.IntValue(runtime.graphicsTextWidth([]rune(value))), nil
}

func runtimeFontCharWidth(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) == 2 {
		if character, err := arguments[1].Int32(); err == nil {
			return jvm.IntValue(runtime.graphicsCharAdvance(rune(character))), nil
		}
	}
	return jvm.IntValue(runtimeFontGlyphAdvance), nil
}

// runtimeSystemOut materializes the shared System.out PrintStream with a full
// guest object layout so guest code can call methods on the field value.
func runtimeSystemOut(runtime *initializationRuntime) (uint32, error) {
	stream := runtime.runtimeObjects["java/io/PrintStream"]
	if stream == nil {
		stream = &jvm.Object{ClassName: "java/io/PrintStream", Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects["java/io/PrintStream"] = stream
	}
	if address, ok := runtime.client.vm.AOTAddress(stream); ok {
		return address, nil
	}
	classAddress, err := runtime.ensureJavaClass("java/io/PrintStream")
	if err != nil {
		return 0, err
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		return 0, fmt.Errorf("KTF PrintStream class at %#x is not registered", classAddress)
	}
	return runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), stream)
}

// runtimeBoxedBoolean answers one of java/lang/Boolean's two published
// instances with a guest address. Reading the static through the VM is what
// runs the core library's class initializer, so the object handed over is the
// one that class holds rather than a second box that would compare unequal.
func runtimeBoxedBoolean(runtime *initializationRuntime, name string) (uint32, error) {
	value, err := runtime.client.vm.StaticField(jvm.BooleanClass, name, "Ljava/lang/Boolean;")
	if err != nil {
		return 0, fmt.Errorf("read java/lang/Boolean.%s: %w", name, err)
	}
	object, err := value.Reference()
	if err != nil {
		return 0, err
	}
	if object == nil {
		return 0, fmt.Errorf("java/lang/Boolean.%s is null", name)
	}
	if err := runtime.ensureResultBound(object); err != nil {
		return 0, fmt.Errorf("bind java/lang/Boolean.%s: %w", name, err)
	}
	address, ok := runtime.client.vm.AOTAddress(object)
	if !ok {
		return 0, fmt.Errorf("java/lang/Boolean.%s has no guest address", name)
	}
	return address, nil
}

func runtimeClassReceiverName(arguments []jvm.Value) (string, error) {
	if len(arguments) < 1 {
		return "", fmt.Errorf("Class method expected receiver")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return "", err
	}
	if receiver == nil || receiver.ClassName != jvm.ClassClass {
		return "", fmt.Errorf("Class receiver is not a class object")
	}
	name, ok := receiver.Native.(string)
	if !ok || name == "" {
		return "", fmt.Errorf("Class object has no class name")
	}
	return name, nil
}

func runtimeClassGetName(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := runtimeClassReceiverName(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(runtime.client.vm.NewString(strings.ReplaceAll(name, "/", "."))), nil
}

func runtimeClassGetResourceAsStream(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	className, err := runtimeClassReceiverName(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("Class.getResourceAsStream expected resource name")
	}
	nameObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("Class.getResourceAsStream name is not a string")
	}
	resourceName := name
	if strings.HasPrefix(resourceName, "/") {
		resourceName = strings.TrimPrefix(resourceName, "/")
	} else if packageEnd := strings.LastIndex(className, "/"); packageEnd >= 0 {
		resourceName = className[:packageEnd+1] + resourceName
	}
	cleaned := path.Clean(resourceName)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return jvm.ReferenceValue(nil), nil
	}
	data, ok := runtime.client.resources[cleaned]
	runtime.countDiagnostic(fmt.Sprintf("stream %s found=%t", cleaned, ok))
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	array := jvm.NewByteArray(append([]byte(nil), data...))
	stream, err := vm.NewObject(jvm.ByteArrayInputStreamClass, "([B)V", jvm.ReferenceValue(array))
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("create KTF resource stream for %q: %w", cleaned, err)
	}
	return jvm.ReferenceValue(stream), nil
}

// runtimeDataBaseStore is the in-memory record list shared by every DataBase
// object opened with the same name. Record identifiers are one-based and a
// deleted slot stays nil, matching WIPI record identifier stability.
type runtimeDataBaseStore struct {
	name    string
	records [][]byte
}

// runtimeDataBaseException is what openDataBase owes a title that asked for a
// database it did not want created. The spec is explicit: opening with create
// false and no database throws DataBaseException, and that throw is how a
// title finds out it is running for the first time. Answering with an empty
// database instead told one title its save existed; it then read record zero,
// caught the record exception that came back, and dereferenced the array the
// read never produced.
func runtimeDataBaseException(message string) error {
	return &jvm.GuestException{
		Object:  &jvm.Object{ClassName: runtimeDataBaseExceptionClass, Native: message},
		Message: message,
	}
}

// persist writes the serialized record list through the Host save store.
func (store *runtimeDataBaseStore) persist(runtime *initializationRuntime) {
	runtime.storeSave("jdb/"+store.name, encodeSaveRecords(store.records))
}

const maxDataBaseRecords = 1 << 16

func runtimeOpenDataBase(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 3 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.openDataBase expected name, record size, and create flag, got %d arguments", len(arguments))
	}
	nameObject, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("DataBase.openDataBase name is not a string")
	}
	create, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	store := runtime.databases[name]
	if store == nil {
		store = &runtimeDataBaseStore{name: name}
		saved, present := runtime.loadSave("jdb/" + name)
		if present {
			records, decodeErr := decodeSaveRecords(saved)
			if decodeErr != nil {
				runtime.countDiagnostic(fmt.Sprintf("jdb load error %s: %v", name, decodeErr))
			} else {
				store.records = records
			}
		}
		if !present && create == 0 {
			runtime.countDiagnostic("jdb absent " + name)
			return jvm.VoidValue(), runtimeDataBaseException("database not found: " + name)
		}
		if runtime.databases == nil {
			runtime.databases = make(map[string]*runtimeDataBaseStore)
		}
		runtime.databases[name] = store
		// A database opened for creation exists from that moment, even with
		// no record in it yet, so the next open finds it rather than throwing
		// again.
		if !present {
			store.persist(runtime)
		}
	}
	database := &jvm.Object{
		ClassName: "org/kwis/msp/db/DataBase",
		Native:    store,
		Fields: map[string]jvm.Value{
			"name:Ljava/lang/String;": arguments[0],
		},
	}
	return jvm.ReferenceValue(database), nil
}

func runtimeDataBaseState(arguments []jvm.Value) (*runtimeDataBaseStore, error) {
	if len(arguments) < 1 {
		return nil, fmt.Errorf("DataBase method expected receiver")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, fmt.Errorf("DataBase receiver is null")
	}
	store, ok := receiver.Native.(*runtimeDataBaseStore)
	if !ok {
		return nil, fmt.Errorf("DataBase receiver has no record store")
	}
	return store, nil
}

func runtimeDataBaseCount(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	count := 0
	for _, record := range store.records {
		if record != nil {
			count++
		}
	}
	return jvm.IntValue(int32(count)), nil
}

func runtimeDataBaseInsert(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.insertRecord expected data")
	}
	data, err := runtimeDataBaseBytes(arguments[1:])
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(store.records) >= maxDataBaseRecords {
		return jvm.VoidValue(), fmt.Errorf("DataBase record count exceeds %d", maxDataBaseRecords)
	}
	store.records = append(store.records, data)
	store.persist(runtime)
	// WIPI record identifiers are zero-based, unlike one-based MIDP records.
	return jvm.IntValue(int32(len(store.records) - 1)), nil
}

// runtimeDataBaseBytes copies record content from a [B argument, applying the
// optional offset/length pair used by the wider insert and update overloads.
func runtimeDataBaseBytes(arguments []jvm.Value) ([]byte, error) {
	dataObject, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if dataObject == nil {
		return nil, fmt.Errorf("DataBase record data is null")
	}
	data, err := jvm.ByteArraySnapshot(dataObject)
	if err != nil {
		return nil, err
	}
	if len(arguments) >= 3 {
		offset, offsetErr := arguments[1].Int32()
		if offsetErr != nil {
			return nil, offsetErr
		}
		length, lengthErr := arguments[2].Int32()
		if lengthErr != nil {
			return nil, lengthErr
		}
		if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(data)) {
			return nil, fmt.Errorf("DataBase record range [%d, %d) is out of bounds", offset, offset+length)
		}
		data = data[offset : offset+length]
	}
	return append([]byte(nil), data...), nil
}

func runtimeDataBaseRecordIndex(store *runtimeDataBaseStore, arguments []jvm.Value) (int, error) {
	identifier, err := arguments[0].Int32()
	if err != nil {
		return 0, err
	}
	index := int(identifier)
	if index < 0 || index >= len(store.records) || store.records[index] == nil {
		return 0, fmt.Errorf("DataBase record %d does not exist", identifier)
	}
	return index, nil
}

func runtimeDataBaseSelect(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.selectRecord expected record identifier")
	}
	index, err := runtimeDataBaseRecordIndex(store, arguments[1:])
	if err != nil {
		// The specification's answer to a record identifier that is not there
		// is DataBaseRecordException, which a title catches. A Host error is
		// not catchable, and one title reads a record it has never written on
		// its first tick — a save that is not there yet — so the difference
		// was the difference between its opening screen and a dead thread.
		return jvm.VoidValue(), runtimeDataBaseRecordException(err)
	}
	return jvm.ReferenceValue(jvm.NewByteArray(append([]byte(nil), store.records[index]...))), nil
}

func runtimeDataBaseUpdate(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 3 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.updateRecord expected identifier and data")
	}
	index, err := runtimeDataBaseRecordIndex(store, arguments[1:])
	if err != nil {
		// The specification's answer to a record identifier that is not there
		// is DataBaseRecordException, which a title catches. A Host error is
		// not catchable, and one title reads a record it has never written on
		// its first tick — a save that is not there yet — so the difference
		// was the difference between its opening screen and a dead thread.
		return jvm.VoidValue(), runtimeDataBaseRecordException(err)
	}
	data, err := runtimeDataBaseBytes(arguments[2:])
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.records[index] = data
	store.persist(runtime)
	return jvm.VoidValue(), nil
}

func runtimeDataBaseDelete(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.deleteRecord expected record identifier")
	}
	index, err := runtimeDataBaseRecordIndex(store, arguments[1:])
	if err != nil {
		// The specification's answer to a record identifier that is not there
		// is DataBaseRecordException, which a title catches. A Host error is
		// not catchable, and one title reads a record it has never written on
		// its first tick — a save that is not there yet — so the difference
		// was the difference between its opening screen and a dead thread.
		return jvm.VoidValue(), runtimeDataBaseRecordException(err)
	}
	store.records[index] = nil
	store.persist(runtime)
	return jvm.VoidValue(), nil
}

func runtimeGetSystemProperty(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("HandsetProperty.getSystemProperty expected name, got %d arguments", len(arguments))
	}
	name, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	// The Java and WIPI C surfaces answer the same question about the same
	// handset, so they answer from one table. They did not before, and a
	// title that asked the Java one for a property the C one knows was told
	// nothing.
	value := ""
	if name != nil {
		if text, ok := jvm.StringText(name); ok {
			runtime.countDiagnostic("sysprop " + text)
			value = wipic.SystemProperties[text]
		}
	}
	return jvm.ReferenceValue(runtime.client.vm.NewString(value)), nil
}

// runtimeSystemGetProperty answers CLDC's own property call. The configuration
// and profile name what this class surface implements; everything else is the
// same handset table HandsetProperty and the WIPI C call answer from, because
// the question is about the handset rather than about which surface asked.
//
// An unknown name is null rather than empty. The specification says so, and a
// title that probes for an optional capability reads an empty string as "the
// handset has it and calls it nothing".
func runtimeSystemGetProperty(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("System.getProperty expected a name, got %d arguments", len(arguments))
	}
	name, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, ok := jvm.StringText(name)
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	runtime.countDiagnostic("sysprop " + text)
	if value, known := runtimeSystemPropertyTable[text]; known {
		return jvm.ReferenceValue(vm.NewString(value)), nil
	}
	if value, known := wipic.SystemProperties[text]; known {
		return jvm.ReferenceValue(vm.NewString(value)), nil
	}
	return jvm.ReferenceValue(nil), nil
}

// runtimeSystemPropertyTable is what CLDC guarantees, stated as what this
// runtime is rather than as a handset it is not.
var runtimeSystemPropertyTable = map[string]string{
	"microedition.platform":      "wfeature",
	"microedition.configuration": "CLDC-1.0",
	"microedition.profiles":      "",
	"microedition.encoding":      "EUC-KR",
	"microedition.locale":        "ko-KR",
}

func runtimeJletConstructor(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Jlet constructor expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Jlet constructor receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	// The application's own Jlet is the one getActiveJlet answers with, and it
	// owns the Display and event queue the rest of the runtime serves.
	runtime.activeJlet = receiver
	receiver.Fields["dis:Lorg/kwis/msp/lcdui/Display;"], _ = runtimeGetDefaultDisplay(runtime, nil, nil)
	receiver.Fields["eq:Lorg/kwis/msp/lcdui/EventQueue;"] = jvm.ReferenceValue(runtime.runtimeEventQueueObject())
	return jvm.VoidValue(), nil
}

func runtimeAnnunciatorConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("AnnunciatorComponent constructor expected receiver and flag, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	visible, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("AnnunciatorComponent constructor receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["visible:Z"] = jvm.IntValue(visible)
	return jvm.VoidValue(), nil
}

func runtimeAnnunciatorShow(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("AnnunciatorComponent.show expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("AnnunciatorComponent.show receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["visible:Z"] = jvm.IntValue(1)
	return jvm.VoidValue(), nil
}

func runtimeCardConstructor(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtimeCardInitialize(runtime, arguments, 0, 0, runtimeScreenWidth(runtime), runtimeScreenHeight(runtime), 0)
}

func runtimeCardConstructorTransparent(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	transparent := int32(0)
	if len(arguments) >= 2 {
		value, err := arguments[1].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		transparent = value
	}
	return runtimeCardInitialize(runtime, arguments, 0, 0, runtimeScreenWidth(runtime), runtimeScreenHeight(runtime), transparent)
}

func runtimeCardConstructorBounds(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 5 {
		return jvm.VoidValue(), fmt.Errorf("Card bounds constructor expected receiver and bounds, got %d arguments", len(arguments))
	}
	bounds := make([]int32, 4)
	for index := range bounds {
		value, err := arguments[index+1].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		bounds[index] = value
	}
	return runtimeCardInitialize(runtime, arguments, bounds[0], bounds[1], bounds[2], bounds[3], 0)
}

func runtimeCardInitialize(runtime *initializationRuntime, arguments []jvm.Value, x, y, width, height, transparent int32) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("Card constructor expected receiver")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Card constructor receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	display := runtime.runtimeObjects[runtimeDisplayClass]
	if display == nil {
		display = &jvm.Object{ClassName: runtimeDisplayClass, Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects[runtimeDisplayClass] = display
	}
	receiver.Fields["display:Lorg/kwis/msp/lcdui/Display;"] = jvm.ReferenceValue(display)
	receiver.Fields["x:I"] = jvm.IntValue(x)
	receiver.Fields["y:I"] = jvm.IntValue(y)
	receiver.Fields["w:I"] = jvm.IntValue(width)
	receiver.Fields["h:I"] = jvm.IntValue(height)
	receiver.Fields["transparent:Z"] = jvm.IntValue(transparent)
	return jvm.VoidValue(), nil
}

// runtimeCardIntField reads a Card geometry field set by its constructor and
// falls back to the default screen bounds for instances the constructor has
// not reached yet.
// runtimeCardScreenField is runtimeCardIntField for the two fields whose
// fallback is the screen rather than a constant. A Card built by the
// constructors above carries w and h already; this is what answers for one
// that reached the accessor without them.
func runtimeCardScreenField(key string, fallback func(*initializationRuntime) int32) runtimeJavaImplementation {
	return func(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		return runtimeCardIntField(key, fallback(runtime))(runtime, vm, arguments)
	}
}

func runtimeCardIntField(key string, fallback int32) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		if len(arguments) != 1 {
			return jvm.VoidValue(), fmt.Errorf("Card field accessor expected receiver, got %d arguments", len(arguments))
		}
		receiver, err := arguments[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if receiver == nil {
			return jvm.VoidValue(), fmt.Errorf("Card field accessor receiver is null")
		}
		if value, ok := receiver.Fields[key]; ok {
			return value, nil
		}
		return jvm.IntValue(fallback), nil
	}
}

func runtimeCardGetDisplay(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Card.getDisplay expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Card.getDisplay receiver is null")
	}
	if value, ok := receiver.Fields["display:Lorg/kwis/msp/lcdui/Display;"]; ok {
		return value, nil
	}
	return runtimeGetDefaultDisplay(runtime, nil, nil)
}

// runtimeCardRepaint records a pending repaint request. MIDP-style frame
// loops pair it with serviceRepaints, which performs the actual paint.
func runtimeCardRepaint(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.repaintPending = true
	// A game driving its own event loop is waiting in getNextEvent, so the
	// repaint request has to reach it as an event too.
	runtime.postRepaintEvent()
	return jvm.VoidValue(), nil
}

// runtimeCardServiceRepaints synchronously services a pending repaint by
// painting the receiver card into the screen framebuffer and presenting the
// frame, matching Card.serviceRepaints blocking semantics — the specification
// says in as many words that this call enters `paint` itself.
//
// **A card the display is not showing is not painted.** The frame loop already
// refuses one (`repaintQueued`, `paintTopCard`), and this was the one place
// that did not, so a card built but never pushed could be entered here and
// nowhere else. One local title loads its resources in stages and calls
// `repaint` and `serviceRepaints` between them to move its progress bar, on a
// card it pushes only once the load is done: entering `paint` there ran the
// title's drawing code against a state it had not built yet, and the title
// stopped in its own null check. Nothing is lost by skipping it — there is no
// screen for a card that is not on the display to output to.
func runtimeCardServiceRepaints(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Card.serviceRepaints expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("Card.serviceRepaints receiver is null")
	}
	if !runtime.cardIsShown(receiver) {
		return jvm.VoidValue(), nil
	}
	if !runtime.repaintPending || runtime.repaintServicing {
		return jvm.VoidValue(), nil
	}
	runtime.repaintPending = false
	runtime.repaintServicing = true
	defer func() { runtime.repaintServicing = false }()
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.ensureResultBound(graphics); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := vm.InvokeVirtual(receiver, "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", jvm.ReferenceValue(graphics)); err != nil {
		return jvm.VoidValue(), fmt.Errorf("service repaint of %s: %w", receiver.ClassName, err)
	}
	return jvm.VoidValue(), runtime.presentScreen()
}

func runtimeGetDefaultDisplay(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 0 {
		return jvm.VoidValue(), fmt.Errorf("Display.getDefaultDisplay expected no arguments, got %d", len(arguments))
	}
	display := runtime.runtimeObjects[runtimeDisplayClass]
	if display == nil {
		display = &jvm.Object{ClassName: runtimeDisplayClass, Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects[runtimeDisplayClass] = display
	}
	return jvm.ReferenceValue(display), nil
}

func (runtime *initializationRuntime) registerRuntimeJavaNatives() error {
	for _, definition := range runtimeJavaClasses {
		for _, method := range definition.methods {
			implementation := method.implementation
			if implementation == nil {
				// The JVM already owns this method; the KTF metadata only
				// exposes the existing implementation to guest code.
				continue
			}
			if err := runtime.client.vm.RegisterNative(method.class, method.name, method.descriptor, func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
				return implementation(runtime, vm, arguments)
			}); err != nil {
				return fmt.Errorf("register KTF runtime Java native %s.%s%s: %w", method.class, method.name, method.descriptor, err)
			}
		}
	}
	return nil
}

func (runtime *initializationRuntime) createRuntimeJavaClass(definition runtimeJavaClass) (uint32, error) {
	if class := runtime.classes[definition.name]; class != 0 {
		return class, nil
	}
	var parent uint32
	var err error
	if definition.superName != "" {
		parent, err = runtime.ensureJavaClass(definition.superName)
		if err != nil {
			return 0, fmt.Errorf("create KTF runtime Java class %s parent: %w", definition.name, err)
		}
	}
	classAddress, err := runtime.allocate(javaClassSize)
	if err != nil {
		return 0, err
	}
	nameAddress, err := runtime.allocateBytes(append([]byte(definition.name), 0))
	if err != nil {
		return 0, err
	}
	vtable, err := runtime.inheritedVTable(parent)
	if err != nil {
		return 0, fmt.Errorf("read KTF runtime Java class %s parent vtable: %w", definition.name, err)
	}
	methodPointers := make([]uint32, 0, len(definition.methods)+1)
	for _, method := range definition.methods {
		directStub, err := runtime.runtimeJavaStub(method, false)
		if err != nil {
			return 0, err
		}
		containerStub, err := runtime.runtimeJavaStub(method, true)
		if err != nil {
			return 0, err
		}
		fullName, err := runtime.allocateBytes(append([]byte{0}, append([]byte(method.descriptor+"+"+method.name), 0)...))
		if err != nil {
			return 0, err
		}
		methodAddress, err := runtime.allocate(javaMethodSize)
		if err != nil {
			return 0, err
		}
		slot := vtable.assign(methodAddress, method.name, method.descriptor)
		methodData := make([]byte, javaMethodSize)
		binary.LittleEndian.PutUint32(methodData[0:], directStub)
		binary.LittleEndian.PutUint32(methodData[4:], classAddress)
		binary.LittleEndian.PutUint32(methodData[8:], containerStub)
		binary.LittleEndian.PutUint32(methodData[12:], fullName)
		binary.LittleEndian.PutUint16(methodData[20:], slot)
		binary.LittleEndian.PutUint16(methodData[22:], method.accessFlags|0x0100)
		if err := runtime.client.core.Memory().Write(methodAddress, methodData); err != nil {
			return 0, fmt.Errorf("write KTF runtime Java method %s.%s%s: %w", method.class, method.name, method.descriptor, err)
		}
		methodPointers = append(methodPointers, methodAddress)
	}
	methodPointers = append(methodPointers, 0)
	methodTable, err := runtime.allocateWords(methodPointers)
	if err != nil {
		return 0, err
	}
	vtableAddress, err := runtime.allocateWords(append(vtable.pointers(), 0))
	if err != nil {
		return 0, err
	}
	// A static field's initializer runs after the class is registered rather
	// than here, so the records are written with their offset word first and
	// patched below. An initializer that has to resolve a class — and the
	// boxed flag's two instances have to resolve their own — would otherwise
	// re-enter this function for a class that is still being built.
	fieldPointers := make([]uint32, 0, len(definition.fields)+1)
	deferredFields := make([]uint32, len(definition.fields))
	for index, field := range definition.fields {
		fullName, err := runtime.allocateBytes(append([]byte{0}, append([]byte(field.descriptor+"+"+field.name), 0)...))
		if err != nil {
			return 0, err
		}
		fieldData := make([]byte, javaFieldSize)
		binary.LittleEndian.PutUint32(fieldData[0:], field.accessFlags)
		binary.LittleEndian.PutUint32(fieldData[4:], classAddress)
		binary.LittleEndian.PutUint32(fieldData[8:], fullName)
		binary.LittleEndian.PutUint32(fieldData[12:], field.offset)
		fieldAddress, err := runtime.allocateBytes(fieldData)
		if err != nil {
			return 0, err
		}
		if field.initializer != nil {
			deferredFields[index] = fieldAddress
		}
		fieldPointers = append(fieldPointers, fieldAddress)
	}
	fieldTable, err := runtime.allocateWords(append(fieldPointers, 0))
	if err != nil {
		return 0, err
	}
	descriptorAddress, err := runtime.allocate(javaDescriptorSize)
	if err != nil {
		return 0, err
	}
	descriptorData := make([]byte, javaDescriptorSize)
	binary.LittleEndian.PutUint32(descriptorData[0:], nameAddress)
	binary.LittleEndian.PutUint32(descriptorData[8:], parent)
	binary.LittleEndian.PutUint32(descriptorData[12:], methodTable)
	binary.LittleEndian.PutUint32(descriptorData[20:], fieldTable)
	binary.LittleEndian.PutUint16(descriptorData[24:], uint16(len(definition.methods)))
	binary.LittleEndian.PutUint16(descriptorData[26:], definition.instanceSize)
	binary.LittleEndian.PutUint16(descriptorData[28:], definition.accessFlags)
	if err := runtime.client.core.Memory().Write(descriptorAddress, descriptorData); err != nil {
		return 0, fmt.Errorf("write KTF runtime Java descriptor %s: %w", definition.name, err)
	}
	classData := make([]byte, javaClassSize)
	binary.LittleEndian.PutUint32(classData[0:], classAddress+4)
	binary.LittleEndian.PutUint32(classData[8:], descriptorAddress)
	binary.LittleEndian.PutUint32(classData[12:], vtableAddress)
	binary.LittleEndian.PutUint16(classData[16:], uint16(len(vtable.entries)))
	binary.LittleEndian.PutUint16(classData[18:], 8)
	if err := runtime.client.core.Memory().Write(classAddress, classData); err != nil {
		return 0, fmt.Errorf("write KTF runtime Java class %s: %w", definition.name, err)
	}
	metadata, err := runtime.readAOTClass(classAddress)
	if err != nil {
		return 0, fmt.Errorf("validate KTF runtime Java class %s: %w", definition.name, err)
	}
	if err := runtime.client.vm.RegisterAOTClass(metadata); err != nil {
		return 0, fmt.Errorf("register KTF runtime Java class %s: %w", definition.name, err)
	}
	if err := runtime.bindAOTClassObject(classAddress, definition.name); err != nil {
		return 0, err
	}
	runtime.classes[definition.name] = classAddress
	for index, field := range definition.fields {
		if deferredFields[index] == 0 {
			continue
		}
		value, err := field.initializer(runtime)
		if err != nil {
			return 0, fmt.Errorf("initialize KTF runtime Java field %s.%s: %w", definition.name, field.name, err)
		}
		valueData := make([]byte, 4)
		binary.LittleEndian.PutUint32(valueData, value)
		if err := runtime.client.core.Memory().Write(deferredFields[index]+12, valueData); err != nil {
			return 0, fmt.Errorf("write KTF runtime Java field %s.%s: %w", definition.name, field.name, err)
		}
	}
	return classAddress, nil
}

// runtimeVTable builds the KTF virtual dispatch table for a runtime-owned Java
// class. Guest virtual calls read the instance header's vtable-table index and
// the method record's vtable slot, so every runtime class must publish a real
// vtable spanning its hierarchy or dispatch lands in an unrelated class.
type runtimeVTable struct {
	entries []runtimeVTableEntry
}

type runtimeVTableEntry struct {
	address    uint32
	name       string
	descriptor string
}

// assign places a method in its override slot when the inherited hierarchy
// already declares the same name and descriptor, or appends a new slot, and
// returns the slot index recorded in the method's metadata.
func (vtable *runtimeVTable) assign(address uint32, name, descriptor string) uint16 {
	for index := range vtable.entries {
		if vtable.entries[index].name == name && vtable.entries[index].descriptor == descriptor {
			vtable.entries[index].address = address
			return uint16(index)
		}
	}
	vtable.entries = append(vtable.entries, runtimeVTableEntry{address: address, name: name, descriptor: descriptor})
	return uint16(len(vtable.entries) - 1)
}

func (vtable *runtimeVTable) pointers() []uint32 {
	result := make([]uint32, len(vtable.entries))
	for index, entry := range vtable.entries {
		result[index] = entry.address
	}
	return result
}

// inheritedVTable copies the registered parent vtable so subclass slots stay
// aligned with superclass dispatch indexes.
func (runtime *initializationRuntime) inheritedVTable(parent uint32) (*runtimeVTable, error) {
	vtable := &runtimeVTable{}
	if parent == 0 {
		return vtable, nil
	}
	metadata, ok := runtime.client.vm.AOTClassAt(parent)
	if !ok {
		return vtable, nil
	}
	for _, pointer := range metadata.VTable {
		method, _, err := runtime.readAOTMethod(pointer)
		if err != nil {
			return nil, err
		}
		vtable.entries = append(vtable.entries, runtimeVTableEntry{
			address:    pointer,
			name:       method.Name,
			descriptor: method.Descriptor,
		})
	}
	return vtable, nil
}

func (runtime *initializationRuntime) runtimeJavaStub(method runtimeJavaMethod, container bool) (uint32, error) {
	if runtime.nextNativeMethod == ^uint32(0) {
		return 0, fmt.Errorf("KTF runtime Java native method id space exhausted")
	}
	runtime.nextNativeMethod++
	id := runtime.nextNativeMethod
	stub, err := runtime.stub(svcCategoryRuntimeJava, id)
	if err != nil {
		return 0, err
	}
	runtime.nativeMethods[id] = runtimeJavaInvocation{method: method, container: container}
	return stub, nil
}

func (runtime *initializationRuntime) handleRuntimeJavaCall(thread *armcore.Thread, id uint32) (uint32, error) {
	invocation, ok := runtime.nativeMethods[id]
	if !ok {
		return 0, fmt.Errorf("unknown KTF runtime Java native method id %#x", id)
	}
	runtime.callbacks.RuntimeJavaCalls++
	method := invocation.method
	if lr, lrErr := thread.Register(armcore.RegisterLR); lrErr == nil {
		runtime.recordDiagnostic(diagEvent{
			kind:       diagJavaCall,
			name:       method.class,
			target:     method.name,
			descriptor: method.descriptor,
			site:       lr,
			hasSite:    true,
		})
	}
	methodType, err := jvm.ParseMethodDescriptor(method.descriptor)
	if err != nil {
		return 0, err
	}
	types := methodType.Parameters
	if method.accessFlags&0x0008 == 0 {
		types = append([]jvm.Type{{Kind: jvm.TypeReference, ClassName: method.class}}, types...)
	}
	var arguments []jvm.Value
	if invocation.container {
		dataAddress, registerErr := thread.Register(1)
		if registerErr != nil {
			return 0, registerErr
		}
		if dataAddress&3 != 0 {
			return 0, fmt.Errorf("KTF runtime Java argument container %#x is not word-aligned", dataAddress)
		}
		arguments, err = runtime.readRuntimeJavaArguments(dataAddress, types)
	} else {
		arguments, err = runtime.readDirectRuntimeJavaArguments(thread, types)
	}
	if err != nil {
		return 0, fmt.Errorf("read KTF runtime Java arguments for %s.%s%s: %w", method.class, method.name, method.descriptor, err)
	}
	// Only the guest has run since the last crossing, so a published field that
	// no longer matches its Go value was written by the guest. See field_sync.go.
	if backend.DebugBuild() {
		for _, argument := range arguments {
			if object, referenceErr := argument.Reference(); referenceErr == nil {
				runtime.adoptGuestFields(object)
			}
		}
	}
	var result jvm.Value
	if method.accessFlags&0x0008 != 0 {
		result, err = runtime.client.vm.InvokeStatic(method.class, method.name, method.descriptor, arguments...)
	} else {
		receiver, referenceErr := arguments[0].Reference()
		if referenceErr != nil {
			return 0, referenceErr
		}
		runtime.traceIdentityComparison(method, receiver, arguments[1:])
		if method.accessFlags&0x0400 != 0 {
			// An abstract declaration has no body to call non-virtually. The
			// guest reaches this stub having resolved through the declaring
			// class, so the receiver is what says which override runs.
			result, err = runtime.client.vm.InvokeVirtual(receiver, method.name, method.descriptor, arguments[1:]...)
		} else {
			result, err = runtime.client.vm.InvokeSpecial(receiver, method.class, method.name, method.descriptor, arguments[1:]...)
		}
		if err == nil {
			runtime.traceStringConversion(method, receiver, result)
			// A constructor is what decides a published field's value, and the
			// instance was allocated before it ran. See field_sync.go.
			if publishErr := runtime.publishGuestFields(receiver, method); publishErr != nil {
				return 0, publishErr
			}
		}
	}
	if err != nil {
		// JVM-raised Java exceptions unwind through the guest handler chain
		// instead of aborting the ARM run.
		var guest *jvm.GuestException
		if errors.As(err, &guest) {
			return 0, runtime.raiseGuestException(thread, guest)
		}
		return 0, err
	}
	if err := runtime.bindRuntimeJavaResult(result); err != nil {
		return 0, err
	}
	words, err := aotValueWords(result, methodType.Return, runtime.client.vm)
	if err != nil {
		return 0, fmt.Errorf("encode KTF runtime Java result for %s.%s%s: %w", method.class, method.name, method.descriptor, err)
	}
	var low, high uint32
	if len(words) > 0 {
		low = words[0]
	}
	if len(words) > 1 {
		high = words[1]
	}
	if err := thread.SetRegister(1, high); err != nil {
		return 0, err
	}
	return low, nil
}

func (runtime *initializationRuntime) bindRuntimeJavaResult(result jvm.Value) error {
	if result.Kind() != jvm.ValueReference {
		return nil
	}
	object, err := result.Reference()
	if err != nil || object == nil {
		return err
	}
	if _, ok := runtime.client.vm.AOTAddress(object); ok {
		return nil
	}
	if err := runtime.ensureResultBound(object); err != nil {
		return err
	}
	runtime.callbacks.Allocations++
	return nil
}

const maxResultBindingDepth = 16

// ensureResultBound gives a JVM-owned object crossing to guest code a real
// guest layout. Arrays receive their length and synchronized element storage;
// strings receive their content layout through the shared allocation hook.
func (runtime *initializationRuntime) ensureResultBound(object *jvm.Object) error {
	if object == nil {
		return nil
	}
	if _, ok := runtime.client.vm.AOTAddress(object); ok {
		return nil
	}
	if runtime.resultBindingDepth >= maxResultBindingDepth {
		return fmt.Errorf("KTF result binding exceeds depth %d", maxResultBindingDepth)
	}
	runtime.resultBindingDepth++
	defer func() { runtime.resultBindingDepth-- }()

	classAddress, err := runtime.ensureJavaClass(object.ClassName)
	if err != nil {
		return fmt.Errorf("resolve KTF runtime Java result class %s: %w", object.ClassName, err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		return fmt.Errorf("KTF runtime Java result class %s at %#x is not registered", object.ClassName, classAddress)
	}
	if strings.HasPrefix(object.ClassName, "[") {
		component, values, snapshotErr := jvm.ArraySnapshot(object)
		if snapshotErr != nil {
			return fmt.Errorf("snapshot KTF result array %s: %w", object.ClassName, snapshotErr)
		}
		size, sizeErr := aotArrayElementBytes(component)
		if sizeErr != nil {
			return sizeErr
		}
		payload := make([]byte, javaArrayLengthSize+len(values)*size)
		binary.LittleEndian.PutUint32(payload, uint32(len(values)))
		if _, err := runtime.allocateAOTObject(metadata, payload, object); err != nil {
			return fmt.Errorf("bind KTF result array %s: %w", object.ClassName, err)
		}
		return nil
	}
	if _, err := runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), object); err != nil {
		return fmt.Errorf("bind KTF runtime Java result %s: %w", object.ClassName, err)
	}
	return nil
}

func (runtime *initializationRuntime) readRuntimeJavaArguments(address uint32, types []jvm.Type) ([]jvm.Value, error) {
	slots := 0
	for _, typeInfo := range types {
		slots += typeInfo.Slots()
	}
	words, err := runtime.readAOTWords(address, uint32(slots), "runtime Java argument container")
	if err != nil {
		return nil, err
	}
	return runtime.decodeRuntimeJavaArguments(words, types)
}

func (runtime *initializationRuntime) readDirectRuntimeJavaArguments(thread *armcore.Thread, types []jvm.Type) ([]jvm.Value, error) {
	slots := 0
	for _, typeInfo := range types {
		slots += typeInfo.Slots()
	}
	words := make([]uint32, slots)
	registerSlots := min(slots, 3)
	for index := 0; index < registerSlots; index++ {
		value, err := thread.Register(index + 1)
		if err != nil {
			return nil, err
		}
		words[index] = value
	}
	if slots > registerSlots {
		stackAddress, err := thread.Register(armcore.RegisterSP)
		if err != nil {
			return nil, err
		}
		stackWords, err := runtime.readAOTWords(stackAddress, uint32(slots-registerSlots), "direct runtime Java stack arguments")
		if err != nil {
			return nil, err
		}
		copy(words[registerSlots:], stackWords)
	}
	return runtime.decodeRuntimeJavaArguments(words, types)
}

func (runtime *initializationRuntime) decodeRuntimeJavaArguments(words []uint32, types []jvm.Type) ([]jvm.Value, error) {
	arguments := make([]jvm.Value, 0, len(types))
	offset := 0
	for _, typeInfo := range types {
		low := words[offset]
		offset++
		switch typeInfo.Kind {
		case jvm.TypeBoolean, jvm.TypeByte, jvm.TypeChar, jvm.TypeShort, jvm.TypeInt:
			arguments = append(arguments, jvm.IntValue(int32(low)))
		case jvm.TypeLong:
			high := words[offset]
			offset++
			arguments = append(arguments, jvm.LongValue(int64(uint64(low)|uint64(high)<<32)))
		case jvm.TypeFloat:
			arguments = append(arguments, jvm.FloatValue(math.Float32frombits(low)))
		case jvm.TypeDouble:
			high := words[offset]
			offset++
			arguments = append(arguments, jvm.DoubleValue(math.Float64frombits(uint64(low)|uint64(high)<<32)))
		case jvm.TypeReference, jvm.TypeArray:
			if low == 0 {
				arguments = append(arguments, jvm.ReferenceValue(nil))
				continue
			}
			object, ok := runtime.client.vm.AOTObject(low)
			if !ok {
				return nil, fmt.Errorf("reference %#x is not bound to a JVM object", low)
			}
			arguments = append(arguments, jvm.ReferenceValue(object))
		default:
			return nil, fmt.Errorf("invalid runtime Java argument type %s", typeInfo.Descriptor())
		}
	}
	return arguments, nil
}
