package lgt

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The platform's own Java classes, as an AOT title reaches them.
//
// A title calls a platform method one of two ways: through the static-method
// array, which the platform filled with an entry point per table entry
// (java_link.go), or through a vtable slot the platform filled in
// (java_runtime.go). Either way it arrives here as an SVC, and this is the
// table of what is implemented.
//
// **The key is the class, the name and the descriptor together**, because that
// is what the module hands over and it is all it hands over. The class comes
// from the run the entry falls in, so an entry that no class claims cannot be
// dispatched and is reported instead.
//
// Nothing is implemented by guessing at what a method should do. A method is
// here when a title has been watched reaching for it and the specification says
// what it answers; everything else stops the title with its own name, which is
// worth more than a plausible answer that is wrong.

// javaPlatformMethod is one implemented method. Words is how many argument
// words it takes, receiver included, because the descriptor alone does not say
// whether an entry is entered with one: the table carries constructors and
// instance methods beside true statics.
type javaPlatformMethod struct {
	Words       int
	Implementat func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error)
}

func javaPlatformKey(class string, member javaMemberRef) string {
	return class + "." + member.Name + member.Descriptor
}

// javaPlatformMethods is the whole of the platform Java API this runtime
// serves. It is short, and it is meant to grow one measured entry at a time.
var javaPlatformMethods = map[string]javaPlatformMethod{
	// A Jlet's constructor. The application's own Jlet subclass calls it as the
	// first thing in its own constructor, on an object the module has already
	// allocated, so there is nothing to build — a Jlet's own state is this
	// platform's, and this platform keeps it on its own side.
	"org/kwis/msp/lcdui/Jlet.<init>()V": {Words: 1, Implementat: javaNoResult},
	// java/lang/Object's constructor, reached the same way.
	"java/lang/Object.<init>()V": {Words: 1, Implementat: javaNoResult},

	// The widgets a Jlet builds before it shows anything. Their state is the
	// platform's, and this platform keeps what it has of it on its own side, so
	// constructing one is taking delivery of an object the module allocated.
	"org/kwis/msp/lwc/AnnunciatorComponent.<init>(Z)V": {Words: 2, Implementat: javaNoResult},
	"org/kwis/msp/lcdui/Card.<init>()V":                {Words: 1, Implementat: javaNoResult},
	// Showing the annunciator is showing the handset's own status bar, which
	// this platform does not draw at all — so it takes no room either, and a
	// title that lays its card out below one gets the whole screen.
	"org/kwis/msp/lwc/AnnunciatorComponent.show()V":      {Words: 1, Implementat: javaNoResult},
	"org/kwis/msp/lwc/AnnunciatorComponent.getHeight()I": {Words: 1, Implementat: javaZeroResult},
	"org/kwis/msp/lwc/Component.getHeight()I":            {Words: 1, Implementat: javaZeroResult},

	// A random source. The specification's no-argument constructor seeds it
	// from the clock, which is the guest's clock here, so a title that reseeds
	// gets a different sequence and a title that runs twice does too.
	"java/util/Random.<init>()V": {Words: 1, Implementat: javaRandomConstructor},
	// The seeded form, which is the same generator started at a place the
	// title chose: a run it means to be able to repeat.
	"java/util/Random.<init>(J)V": {Words: 3, Implementat: javaRandomSetSeed},

	// A growable list; see java_vector.go. The capacity and the growth step are
	// hints about an array this platform does not keep, so all three forms
	// build the same empty list.
	"java/util/Vector.<init>()V":   {Words: 1, Implementat: javaVectorConstructor},
	"java/util/Vector.<init>(I)V":  {Words: 2, Implementat: javaVectorConstructor},
	"java/util/Vector.<init>(II)V": {Words: 3, Implementat: javaVectorConstructor},
	// A Stack is that same list reached from one end; see javaPlatformSupers
	// for why its own methods are Vector's slots.
	"java/util/Stack.<init>()V": {Words: 1, Implementat: javaVectorConstructor},

	// The size of the drawing surface. A Card is the whole screen on this
	// platform, and so is the display, which is what the two pairs answer with.
	"org/kwis/msp/lcdui/Card.getWidth()I":     {Words: 1, Implementat: javaScreenWidth},
	"org/kwis/msp/lcdui/Card.getHeight()I":    {Words: 1, Implementat: javaScreenHeight},
	"org/kwis/msp/lcdui/Display.getWidth()I":  {Words: 1, Implementat: javaScreenWidth},
	"org/kwis/msp/lcdui/Display.getHeight()I": {Words: 1, Implementat: javaScreenHeight},
	// The card that is on the screen, or null before anything is pushed. A
	// launcher asks for it first thing, and null is the honest answer there
	// rather than a stop: the title has not shown anything yet.
	"org/kwis/msp/lcdui/Display.getDockedCard()Lorg/kwis/msp/lcdui/Card;": {
		Words: 1, Implementat: javaDockedCard},

	// The handset's backlight. The specification's own description of
	// alwaysOn is that the light stays on while the application runs, which on
	// a platform with no backlight to hold is nothing to do rather than
	// something unimplemented.
	"org/kwis/msp/handset/BackLight.alwaysOn()V": {Implementat: javaNoResult},

	// Collection is the platform's business and this platform's answer is that
	// it has none to do; the specification defines the call as a request.
	"java/lang/System.gc()V": {Implementat: javaNoResult},

	// The block move every title uses to grow an array. Its element width is
	// the array's own; see java_array.go.
	"java/lang/System.arraycopy(Ljava/lang/Object;ILjava/lang/Object;II)V": {
		Words: 5, Implementat: javaArrayCopy},

	// Pictures. A title reads its own resource, hands the bytes over, and the
	// answer is an object standing for a surface this platform decoded; see
	// java_stream.go.
	"org/kwis/msp/lcdui/Image.createImage([BII)Lorg/kwis/msp/lcdui/Image;": {
		Words: 3, Implementat: javaCreateImage},
	"org/kwis/msp/lcdui/Image.createImage(Ljava/lang/String;)Lorg/kwis/msp/lcdui/Image;": {
		Words: 1, Implementat: javaCreateImageNamed},
	"org/kwis/msp/lcdui/Image.getWidth()I":  {Words: 1, Implementat: javaImageWidth},
	"org/kwis/msp/lcdui/Image.getHeight()I": {Words: 1, Implementat: javaImageHeight},

	// The system volume, which is the same level `MC_mdaSetVolume` sets: one
	// handset, one volume, whichever side of the platform asks for it.
	"org/kwis/msp/media/Volume.set(I)V": {Words: 1, Implementat: javaSetVolume},
	// Sound; see java_sound.go.
	"org/kwis/msp/media/Clip.<init>(Ljava/lang/String;[B)V": {Words: 3, Implementat: javaClipConstructor},
	// The two forms that name a type and no data. The specification's second
	// argument is a buffer size, and the buffer is this platform's business
	// rather than the title's: what both build is an empty clip of that type,
	// which a title then fills through the data calls or plays as silence.
	"org/kwis/msp/media/Clip.<init>(Ljava/lang/String;)V":  {Words: 2, Implementat: javaClipEmpty},
	"org/kwis/msp/media/Clip.<init>(Ljava/lang/String;I)V": {Words: 3, Implementat: javaClipEmpty},
	"org/kwis/msp/media/Clip.<init>(Ljava/lang/String;Ljava/lang/String;)V": {
		Words: 3, Implementat: javaClipFromFile},
	"org/kwis/msp/media/Clip.setVolume(I)Z": {Words: 2, Implementat: javaClipSetVolume},
	"org/kwis/msp/media/Clip.setListener(Lorg/kwis/msp/media/PlayListener;)V": {
		Words: 2, Implementat: javaClipSetListener},
	"org/kwis/msp/media/Player.play(Lorg/kwis/msp/media/Clip;Z)Z": {
		Words: 2, Implementat: javaPlayerPlay},
	"org/kwis/msp/media/Player.stop(Lorg/kwis/msp/media/Clip;)Z":   {Words: 1, Implementat: javaPlayerStop},
	"org/kwis/msp/media/Player.resume(Lorg/kwis/msp/media/Clip;)Z": {Words: 1, Implementat: javaPlayerResume},
	// No vibrator here, and nothing observable is lost by saying so — the same
	// answer the WIPI C call gives.
	"org/kwis/msp/media/Vibrator.on(II)V": {Words: 2, Implementat: javaNoResult},

	// The date, which a title reads rather than the clock; see java_calendar.go.
	"java/util/Calendar.getInstance()Ljava/util/Calendar;": {Implementat: javaCalendarGetInstance},

	// The instant behind a date, which a title stores as a number; see
	// java_calendar.go.
	"java/util/Date.<init>()V":  {Words: 1, Implementat: javaDateNow},
	"java/util/Date.<init>(J)V": {Words: 3, Implementat: javaDateAt},

	// Numbers written into a sink; see java_stream.go. The wrapper stands for
	// the sink it was built on rather than holding bytes of its own.
	"java/io/DataOutputStream.<init>(Ljava/io/OutputStream;)V": {Words: 2, Implementat: javaWrapSink},

	// The sink a title builds a block of bytes in; see java_stream.go. Both of
	// the specification's constructors open the same empty one.
	"java/io/ByteArrayOutputStream.<init>()V":  {Words: 1, Implementat: javaByteSinkConstructor},
	"java/io/ByteArrayOutputStream.<init>(I)V": {Words: 2, Implementat: javaByteSinkConstructor},

	// A stream that reads numbers instead of bytes. It is a wrapper: the
	// object stands for the same open resource the stream it is built on does.
	"java/io/DataInputStream.<init>(Ljava/io/InputStream;)V": {
		Words: 2, Implementat: javaWrapStream},
	// A reader is a wrapper too, and for the same reason: it stands for the
	// stream it was built on, so a read through either moves the one cursor.
	// **What it decodes with is not settled here** — nothing has been watched
	// reading characters out of one yet — and it does not have to be for the
	// wrapping to be right.
	"java/io/InputStreamReader.<init>(Ljava/io/InputStream;)V": {
		Words: 2, Implementat: javaWrapStream},

	// A stream over bytes a title already holds, which is how one reads a
	// record back out of its own save. There is nothing to open: the array is
	// the stream's whole content, copied because the title may write into the
	// array afterwards.
	"java/io/ByteArrayInputStream.<init>([B)V": {
		Words: 2, Implementat: javaByteStreamConstructor},

	// A title's own save, on the filesystem a Clet writes into; see
	// java_file.go.
	// The constructor comes in both of the specification's forms: with the
	// sharing level and without it. Nothing here shares a file, so the two open
	// the same way.
	"org/kwis/msp/io/File.<init>(Ljava/lang/String;II)V":     {Words: 4, Implementat: javaFileOpen},
	"org/kwis/msp/io/File.<init>(Ljava/lang/String;I)V":      {Words: 3, Implementat: javaFileOpen},
	"org/kwis/msp/io/File.sizeOf()I":                         {Words: 1, Implementat: javaFileSize},
	"org/kwis/msp/io/File.read([B)I":                         {Words: 2, Implementat: javaFileRead},
	"org/kwis/msp/io/File.read([BII)I":                       {Words: 4, Implementat: javaFileRead},
	"org/kwis/msp/io/File.write([B)I":                        {Words: 2, Implementat: javaFileWrite},
	"org/kwis/msp/io/File.write([BII)I":                      {Words: 4, Implementat: javaFileWrite},
	"org/kwis/msp/io/File.write(I)I":                         {Words: 2, Implementat: javaFileWriteByte},
	"org/kwis/msp/io/File.close()V":                          {Words: 1, Implementat: javaFileClose},
	"org/kwis/msp/io/FileSystem.exists(Ljava/lang/String;)Z": {Words: 1, Implementat: javaFileExists},
	// The form that names which directory to look in. A title here has one —
	// its own — so the flag chooses nothing and the two forms answer the same
	// way; the name is still the name.
	"org/kwis/msp/io/FileSystem.exists(Ljava/lang/String;I)Z": {Words: 2, Implementat: javaFileExists},
	"org/kwis/msp/io/FileSystem.remove(Ljava/lang/String;)V":  {Words: 1, Implementat: javaFileRemove},
	// How much room a title has left. Saves live in Host storage without a
	// handset's quota behind them, so the answer is the same generous constant
	// a database reports its own free space from — a title asks in order to
	// decide whether it can save at all, and this platform can.
	"org/kwis/msp/io/FileSystem.available()I": {Implementat: javaFileSystemAvailable},

	// The clock a title reads. It is the same clock every other LGT call reads,
	// which is what keeps a Java title's idea of time and a Clet's the same.
	"java/lang/System.currentTimeMillis()J": {Implementat: javaCurrentTimeMillis},

	// The two-argument arithmetic a compiler cannot inline, because the
	// application calls them through the platform's own table.
	"java/lang/Math.abs(I)I": {Words: 1, Implementat: javaMathAbs},
	"java/lang/Math.min(II)I": {Words: 2, Implementat: func(
		_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		if int32(arguments[0]) < int32(arguments[1]) {
			return arguments[0], nil
		}
		return arguments[1], nil
	}},
	"java/lang/Math.max(II)I": {Words: 2, Implementat: func(
		_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		if int32(arguments[0]) > int32(arguments[1]) {
			return arguments[0], nil
		}
		return arguments[1], nil
	}},

	// The same pair over sixty-four bits. A long is two words, low first, so
	// the four argument words are the two values and the answer goes back in
	// the register pair every other long-valued call here uses.
	"java/lang/Math.max(JJ)J": {Words: 4, Implementat: javaMathLong(false)},
	"java/lang/Math.min(JJ)J": {Words: 4, Implementat: javaMathLong(true)},

	// Text. What a String or a StringBuffer holds is kept on this platform's
	// side, keyed by the object the module allocated; see java_string.go.
	"java/lang/String.valueOf(I)Ljava/lang/String;": {Words: 1, Implementat: javaStringValueOf},
	"java/lang/String.<init>()V":                    {Words: 1, Implementat: javaStringEmpty},
	"java/lang/String.<init>([B)V":                  {Words: 2, Implementat: javaStringConstructor},
	"java/lang/String.<init>([BII)V":                {Words: 4, Implementat: javaStringConstructor},
	"java/lang/String.<init>([CII)V":                {Words: 4, Implementat: javaStringFromChars},
	"java/lang/StringBuffer.<init>()V":              {Words: 1, Implementat: javaBufferConstructor},
	// The capacity form is a hint about a buffer this platform grows on
	// demand, so it builds the same empty buffer the no-argument form does —
	// and it takes the no-argument path, because its one argument is a number
	// rather than the text the other form starts from.
	"java/lang/StringBuffer.<init>(I)V": {Words: 2, Implementat: javaBufferEmpty},
	"java/lang/StringBuffer.<init>(Ljava/lang/String;)V": {
		Words: 2, Implementat: javaBufferConstructor},

	// The two objects a title asks the platform for by name. Each is one
	// instance for the life of the title, which is what the specification says
	// they are: the default display, and the runtime.
	"org/kwis/msp/lcdui/Display.getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;": {
		Implementat: javaPlatformSingleton("org/kwis/msp/lcdui/Display")},
	"java/lang/Runtime.getRuntime()Ljava/lang/Runtime;": {
		Implementat: javaPlatformSingleton("java/lang/Runtime")},

	// There is no network behind this platform, and the specification gives
	// the call a number for exactly that: `connect` answers -1 when the
	// attempt fails. A title that dials reaches its own offline path, which is
	// the same place the WIPI C block's refusal takes one (wipic_net.go).
	"org/kwis/msf/io/Network.connect()I": {Implementat: func(
		_ *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
		return ^uint32(0), nil
	}},
	"org/kwis/msf/io/Network.disconnect()V": {Implementat: javaNoResult},
	// The class has nothing to build: its methods are static and its state is
	// the handset's radio, which is not there. A title that constructs one is
	// taking delivery of the object the module allocated.
	"org/kwis/msf/io/Network.<init>()V": {Words: 1, Implementat: javaNoResult},

	// The socket factory, refused the way the specification refuses it. A
	// title that gets past `connect` dials here, and until this existed the
	// call was **absent** rather than refused: an unimplemented slot stops the
	// session with a host error no guest `catch` can see, which is the failure
	// docs/network.md exists to avoid. `find` is documented as throwing
	// `SchemeNotFoundException` when it cannot make a socket, so that is what
	// a handset with no coverage does and what a title's own error path was
	// written around.
	"org/kwis/msf/io/URL.find(Ljava/lang/String;)Lorg/kwis/msf/io/Socket;": {
		Words: 1, Implementat: javaURLFind},
	"org/kwis/msf/io/URL.<init>()V": {Words: 1, Implementat: javaNoResult},

	// The handset's own values, out of the table the WIPI C call answers from:
	// one handset, one answer, whichever side of the platform asks for it.
	// The specification defines the identifiers as the ones `MH_sysGetInformation`
	// takes and says an unrecognised one throws, which is what a title that
	// asks for something this handset does not have gets — a named refusal
	// rather than an empty string it would take for an answer.
	"org/kwis/msp/handset/HandsetProperty.getSystemProperty(Ljava/lang/String;)Ljava/lang/String;": {
		Words: 1, Implementat: javaSystemProperty},

	// A title's game loop runs on a thread of its own; see java_thread.go.
	"java/lang/Thread.<init>(Ljava/lang/Runnable;)V":     {Words: 2, Implementat: javaThreadConstructor},
	"java/lang/Thread.<init>()V":                         {Words: 1, Implementat: javaThreadConstructor},
	"java/lang/Thread.sleep(J)V":                         {Words: 2, Implementat: javaThreadSleep},
	"java/lang/Thread.currentThread()Ljava/lang/Thread;": {Implementat: javaCurrentThread},
	"java/lang/Thread.yield()V":                          {Implementat: javaThreadYield},
}

// The drawing surface is one table of its own, in java_screen.go, and it joins
// this one here. Keeping it separate is what keeps the screen's calls together
// with the state they share.
func init() {
	// `java/lang/Thread` slot 10 takes the receiver alone and drops the answer,
	// and **where it is called is what names it**: immediately after a
	// `Thread(Runnable)` was constructed and stored into a field, past a null
	// check on it, as the last thing `startApp` does before returning. That is
	// `start` — the one Thread method a title calls there, and the only reading
	// under which `startApp` returning leaves the game running. See
	// java_thread.go for what starting one does. It is registered here rather
	// than in the table above because starting a thread runs guest code, which
	// reaches this table back.
	//
	// Slot 14 takes the receiver and one number, and the number is a literal
	// ten. Its call site is the first thing a game thread's `run` does: ask
	// for the running thread, null-check it, and dispatch this slot on it with
	// ten, dropping the answer. **`setPriority(int)` is the only method this
	// class declares that takes an argument at all**, so no other reading of
	// the site is available — and ten is `MAX_PRIORITY`, which is what a game
	// loop asks for there. The numbering agrees: this class's own run is
	// `start`, `run`, `interrupt`, `isAlive`, `setPriority`, and `start` at 10
	// is the anchor above, which puts `setPriority` at exactly 14.
	javaBakedVirtualSlots[javaThreadClass] = map[uint32]javaBakedSlot{
		10: {Called: "start()V", Method: javaPlatformMethod{Words: 1, Implementat: javaThreadStart}},
		14: {Called: "setPriority(I)V",
			Method: javaPlatformMethod{Words: 2, Implementat: javaThreadSetPriority}},
	}
	for _, table := range []map[string]javaPlatformMethod{javaGraphicsMethods, javaDatabaseMethods} {
		for key, method := range table {
			if _, duplicate := javaPlatformMethods[key]; duplicate {
				panic("LGT java platform method declared twice: " + key)
			}
			javaPlatformMethods[key] = method
		}
	}
}

// javaBakedSlot is a method the compiler numbered itself, and how it is
// reported: a class the module lists no virtual methods for gets no slot from
// this platform — **the number is baked into the call site** — so there is no
// name to look it up by, and Called is what this platform is prepared to say
// about it rather than a signature read from anywhere.
type javaBakedSlot struct {
	Called string
	Method javaPlatformMethod
}

// javaBakedVirtualSlots is what the call sites of such a slot come to. Every
// entry below was read off a call site — how many arguments it sets, and what
// it does with the answer — and **the slots those sites settled turned out to
// spell a rule**, which is what makes a new one cheap:
//
//	a class's own methods begin at slot 10, in the order the library class
//	declares them, and a method that overrides one keeps the slot the class
//	above it gave that method.
//
// It is a reading of the numbers rather than a document: nothing in the
// specification says it, and the specification's own listings are alphabetical,
// which is a different order and the wrong one. What says it is that twenty-two
// slots settled one at a time from call sites fall on it exactly, across seven
// classes and two inheritance chains — `Object` 1/4/5, `String` 10/28/33/34,
// `StringBuffer` 10/13/18/22/23, `Vector` 15/29, `InputStream` 10/11/12/13/14/15,
// `DataInputStream` 25 and `Class` 16. Six of those are one consecutive run,
// which no coincidence survives.
//
// So a call site now has a second, independent reading beside it, and the two
// are worth taking together: the site says what the answer is used as, the rule
// says which method sits at the number. Where they agree the slot is settled;
// where they disagree, the site wins and the disagreement is worth writing down.
// See docs/lgt.md, "Two kinds of virtual call".
//
// `java/lang/Runtime` declares `exit(I)`, `freeMemory()`, `totalMemory()` and
// `gc()`, so they fall at 10, 11, 12 and 13. Slot 13 is independently known to
// be `gc()` — one title reaches it 60 times through a one-line helper whose
// whole body is `Runtime.getRuntime().<13>()`, always before an allocation, and
// nothing calls `freeMemory` sixty times for its side effect — and that one
// confirmed position is what fixes the rest of the run.
//
// **Slot 12 is `totalMemory`, not `freeMemory`.** No call site can tell the two
// apart: the one site in the library drops the answer, and the disassembly
// confirms it, so this is the rule's reading and nothing else. It answers the
// arena's whole size where it used to answer what is left of it, which for a
// title that sizes a cache off the heap is a different number.
//
// A slot with no entry here is still reported by class and number rather than
// guessed at.
var javaBakedVirtualSlots = map[string]map[uint32]javaBakedSlot{
	"java/lang/Runtime": {
		11: {Called: "freeMemory()J", Method: javaPlatformMethod{Words: 1, Implementat: javaFreeMemory}},
		12: {Called: "totalMemory()J", Method: javaPlatformMethod{Words: 1, Implementat: javaTotalMemory}},
		13: {Called: "gc()V", Method: javaPlatformMethod{Words: 1, Implementat: javaNoResult}},
	},
	// `java/lang/Object`, whose eleven slots every class inherits: the stub a
	// call arrives through is the one Object's own vtable was built with, so
	// what is implemented here answers for every object that does not override
	// it. Slot 1 takes the receiver and nothing else, and its answer is
	// dispatched on immediately after — an object with methods, which of what
	// Object declares is `getClass`.
	"java/lang/Object": {
		1: {Called: "getClass()Ljava/lang/Class;",
			Method: javaPlatformMethod{Words: 1, Implementat: javaGetClass}},
		// Slot 5 takes the receiver alone and its answer is dropped, and where
		// two titles call it says which no-argument Object method it is: at the
		// end of a `synchronized` method that has just written three fields of
		// the receiver, on the straight line between the last store and the
		// monitor being given back. That is `notify` — the one Object method a
		// publisher calls there — and not `wait`, which would have to sit
		// inside a loop rather than before a return.
		//
		// **There is nothing for it to do here.** A thread parked on this
		// platform is one whose slice ended or whose `sleep` has not elapsed;
		// none of them is waiting on an object, because a contended monitor
		// retries on its own (java_thread.go). So the notification has no
		// queue to move anything out of.
		5: {Called: "notify()V", Method: javaPlatformMethod{Words: 1, Implementat: javaNoResult}},
		// Slot 9 takes the receiver alone, drops its answer, and is called on
		// the very object the instruction before it took the lock of — inside
		// a `synchronized` body, on the object being synchronized on. Of what
		// this class declares that is `wait`, and the run agrees: `getClass`
		// at 1 and `notify` at 5 are four apart, which is what `hashCode`,
		// `equals` and `toString` fill, and the three `wait` forms follow
		// `notifyAll` at 6 — the no-argument one last, at 9.
		9: {Called: "wait()V", Method: javaPlatformMethod{Words: 1, Implementat: javaObjectWait}},
		// Slot 7 is the same call with a deadline: the receiver and a long,
		// which the one site here passes as forty milliseconds. It sits two
		// before the no-argument form, where `wait(long)` and
		// `wait(long, int)` are declared.
		7: {Called: "wait(J)V", Method: javaPlatformMethod{Words: 3, Implementat: javaObjectWaitTimed}},
	},
	// `java/lang/Class` slot 16 takes one argument, a String, and answers an
	// object the caller null-checks and then reads bytes out of — the resource
	// stream every local Java title loads its data through. See java_stream.go.
	javaClassClass: {
		16: {Called: "getResourceAsStream(Ljava/lang/String;)Ljava/io/InputStream;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaGetResourceAsStream}},
	},
	// `java/io/InputStream` slot 10 takes nothing and answers a byte the
	// callers shift together into a halfword, which is `read`.
	// Slots 11 and 12 take an array, and 12 takes an offset and a count with
	// it, which is the `read` pair either side of `read()`.
	javaInputStreamClass: {
		10: {Called: "read()I", Method: javaPlatformMethod{Words: 1, Implementat: javaStreamRead}},
		11: {Called: "read([B)I", Method: javaPlatformMethod{Words: 2, Implementat: javaStreamReadArray}},
		12: {Called: "read([BII)I", Method: javaPlatformMethod{Words: 4, Implementat: javaStreamReadArray}},
		// Slot 15 takes the receiver alone, its answer is dropped, and the
		// caller nulls its own reference to the stream straight after: `close`.
		// Slot 14 takes the receiver alone and its answer is the length of the
		// array the caller allocates next, then reads the whole stream into:
		// `available`.
		// Slot 13 sets **two** argument registers, 0x26 and 0, and its answer
		// is dropped: a long, in a loop that reads fixed-size records out of a
		// resource. That is `skip`, whose count is a long and whose answer a
		// title stepping over a record has no use for.
		13: {Called: "skip(J)J", Method: javaPlatformMethod{Words: 3, Implementat: javaStreamSkip}},
		14: {Called: "available()I", Method: javaPlatformMethod{Words: 1, Implementat: javaStreamAvailable}},
		15: {Called: "close()V", Method: javaPlatformMethod{Words: 1, Implementat: javaStreamClose}},
	},
	// `java/io/DataInputStream` slot 25 takes the receiver alone and its answer
	// is stored into an array the compiled code strides two bytes through, so
	// it reads a big-endian halfword. **Whether it is `readShort` or
	// `readUnsignedShort` cannot be told here** — the store truncates either to
	// the same sixteen bits — so it is sign-extended, which is what the `short`
	// array it lands in means.
	javaDataInputStreamClass: {
		// Slot 20 takes the receiver, a byte array and two numbers, and the
		// numbers its caller passes are a zero and the length it has just read
		// out of the file — a read that must fill the array. Slot 23 takes the
		// receiver alone; the numbering puts `readByte` there, three slots
		// into this class's own run.
		// Slot 19 is the same call without bounds — one byte array, filled to
		// its own length — and it is the first slot of this class's own run,
		// which is what the four slots settled from call sites put there.
		19: {Called: "readFully([B)V",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStreamReadFully}},
		20: {Called: "readFully([BII)V",
			Method: javaPlatformMethod{Words: 4, Implementat: javaStreamReadFully}},
		// Slot 22 takes the receiver alone — the register beside it still
		// holds the stream this one was built on, which the site loaded to
		// reach the receiver and never reloaded — and it sits one before the
		// `readByte` below. This class's run is `readFully([B)`,
		// `readFully([BII)`, `skipBytes`, `readBoolean`, `readByte`, and the
		// four slots settled from their own call sites — 20, 23, 25 and 28 —
		// pin every place in it. A save that stores a flag reads it back here.
		22: {Called: "readBoolean()Z",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStreamReadBoolean}},
		23: {Called: "readByte()B",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStreamReadByte}},
		// Slot 24 is the same byte kept unsigned, which is the pair this class
		// declares one after the other and the run puts here.
		24: {Called: "readUnsignedByte()I",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStreamReadUnsignedByte}},
		25: {Called: "a big-endian halfword",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStreamReadShort}},
		// Slot 27 takes the receiver alone and sits two past the halfword
		// above, where this class declares `readUnsignedShort` and then
		// `readChar`. It is the unsigned one either way — a `char` is a
		// sixteen-bit unsigned value — so the two bytes are not sign-extended
		// here, which is the whole difference from 25.
		27: {Called: "readChar()C",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStreamReadChar}},
		// Slot 28 takes the receiver alone. This class's own methods start
		// after the nine it inherits and overrides from `InputStream`, which
		// puts `readShort` at 25 — where the halfword above already is — and
		// `readInt` three further on. A title reading its own table file reads
		// a count with it before it reads the rows.
		28: {Called: "readInt()I",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStreamReadInt}},
	},
	// `java/io/InputStreamReader` slot 11 takes the receiver and a `char[]`,
	// and the run it falls in is the reader hierarchy's own: `read()`,
	// `read(char[])`, `read(char[], int, int)`, in the order the class library
	// declares them, from 10. A call that passes one array and no bounds is
	// the middle one. **The bytes are decoded the way every other text this
	// platform reads is** — the handset's own encoding — so a reader and a
	// `String(byte[])` over the same bytes agree.
	// Slot 18 is the same run's last: `skip`, `ready`, `markSupported`,
	// `mark`, `reset`, `close`. It takes the receiver alone and its answer is
	// dropped, and closing the reader closes the stream underneath it, which
	// is the one both objects stand for.
	javaInputStreamReaderClass: {
		11: {Called: "read([C)I", Method: javaPlatformMethod{Words: 2, Implementat: javaReaderRead}},
		18: {Called: "close()V", Method: javaPlatformMethod{Words: 1, Implementat: javaStreamClose}},
	},
	// `java/lang/StringBuffer`. Slot 18 takes one reference and answers one:
	// its call sites build a string constant, pass it as the only argument, and
	// dispatch the *same* slot again on what came back, which is `append`
	// returning the buffer for the next call in the chain. All three local Java
	// titles reach it, and one reaches it with the answer of the previous
	// append as its receiver twice over.
	//
	// Slot 4 is below the eleven `java/lang/Object` takes, so it is one of
	// Object's own and StringBuffer overrides it: the call sites set no
	// argument, and the answer is passed straight on where a String is wanted —
	// at the end of a chain of appends, which is `toString`.
	// `java/lang/String` slot 33 takes the receiver alone and answers something
	// used where a String is. **What its one call site is doing is what names
	// it**: the code around it splits a byte array on carriage returns — the
	// comparison is against 13, and the title's own text resources are all
	// CRLF — builds a String out of each run of bytes through
	// `String([BII)`, calls this slot on it, and stores the answer into a
	// `String[]`. Every line after the first therefore begins with the line
	// feed the split left behind, and `trim` is the method that takes it off;
	// it is also the idiom a line splitter is written with.
	javaStringClass: {
		// Slot 3 is one of `java/lang/Object`'s that `String` overrides, and
		// both readings agree on which: the rule puts `equals` third in
		// Object's declaration order, and the call site is a title comparing
		// the handset property it just read against a string constant and
		// branching on the answer. Nothing else Object declares takes one
		// reference and answers a boolean.
		3: {Called: "equals(Ljava/lang/Object;)Z",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStringEquals}},
		// Slot 10 takes the receiver alone and answers a number its caller
		// compares an index against, twice over a run of Strings, in a routine
		// that lays text out. That is `length`. **It sits inside the eleven
		// slots `java/lang/Object` takes**, which the application classes fix,
		// so either the compiler numbers `String` against a header of its own
		// or those eleven are not all methods; what the call site does with
		// the answer is the stronger evidence, and a `hashCode` compared
		// against a position would be nothing at all.
		10: {Called: "length()I",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStringLength}},
		// Slot 28 takes two numbers — the second computed from the first at
		// its call site — and answers something used where a String is. Of
		// what String declares, `substring(int, int)` is the only method with
		// that shape.
		28: {Called: "substring(II)Ljava/lang/String;",
			Method: javaPlatformMethod{Words: 3, Implementat: javaStringSubstring}},
		// Slots 11, 14, 16, 19 and 26 fill the gaps of a run already anchored
		// at both ends: charAt, getBytes with no argument, compareTo, the
		// one-argument startsWith and the two-argument indexOf. Each one's
		// call site agrees with what it is given — the charAt caller passes an
		// index, the indexOf caller passes a zero where a fromIndex goes, the
		// compareTo and startsWith callers pass one String, and the getBytes
		// caller passes nothing at all.
		11: {Called: "charAt(I)C",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStringCharAt}},
		14: {Called: "getBytes()[B",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStringBytes}},
		16: {Called: "compareTo(Ljava/lang/String;)I",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStringComparison}},
		19: {Called: "startsWith(Ljava/lang/String;)Z",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStringStartsWith}},
		// Slot 21 takes the receiver and one number, and the instruction after
		// the call compares its answer with zero and branches when it is
		// negative — an index with -1 for "not there". This class declares
		// four `indexOf`/`lastIndexOf` forms over a character before the two
		// over a string, and the run puts the first of them here; the number
		// the one local site passes is `U+3000`, the ideographic space, which
		// is a character rather than anything else it could be.
		21: {Called: "indexOf(I)I",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStringIndexOfChar}},
		// Slot 22 is the same search from an index, which this class declares
		// immediately after. Its site passes `0x26` and a zero — an ampersand
		// and the start of the string — so the character and the place to
		// start are the right way round.
		22: {Called: "indexOf(II)I",
			Method: javaPlatformMethod{Words: 3, Implementat: javaStringIndexOfChar}},
		26: {Called: "indexOf(Ljava/lang/String;I)I",
			Method: javaPlatformMethod{Words: 3, Implementat: javaStringIndexOfTextFrom}},
		// Slot 29 sits one past substring and takes the receiver and one
		// String, which the declaration order makes `concat`.
		29: {Called: "concat(Ljava/lang/String;)Ljava/lang/String;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStringConcat}},
		33: {Called: "trim()Ljava/lang/String;",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStringTrim}},
		// Slot 34 takes the receiver alone and **what its answer is read as is
		// what names it**: two titles pass it straight to a helper that reads
		// its length out of the first word of its block and then indexes it
		// with a one-place shift, comparing each element against 0x80 to
		// measure how wide the text will draw. A block of sixteen-bit elements
		// is a `char[]`, and the method that answers one is `toCharArray`.
		34: {Called: "toCharArray()[C",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStringToCharArray}},
	},
	// `java/util/Vector`. Slot 15 takes the receiver alone and its answer is
	// **taken apart into bytes** — shifted right by 24, 16 and 8 and stored one
	// at a time into a byte array — by the helper its caller hands it to. That
	// is a number written big-endian into a save buffer, and the number a save
	// routine writes before it walks a list is `size`.
	// `java/util/Random` slot 12 takes the receiver alone and its answer goes
	// straight into a division, guarded against `INT_MIN / -1` — so it is a
	// signed int over the whole range, which is `nextInt()`. A bounded
	// `nextInt(int)` would carry the bound at the call site and need no guard.
	//
	// **Where a slot number comes from.** A class's own instance methods take
	// the slots from 10 upward in the order the specification declares them, a
	// method that overrides an inherited one takes the slot it inherits, and a
	// static takes none. That rule was read off the slots settled from their
	// call sites rather than assumed; docs/lgt.md has the anchors it rests on
	// and why it is worth more than any one of them. Every slot here is placed
	// by it **and** checked against what its own call site passes, because the
	// rule alone would only be an argument.
	"java/util/Random": {
		// Slot 10 is the first method the specification declares, and the call
		// carries a sixty-four bit value in two registers where a caller of
		// anything else on this class would carry nothing. A title that seeds
		// its generator by hand is a title whose sequence is meant to repeat.
		10: {Called: "setSeed(J)V", Method: javaPlatformMethod{Words: 3, Implementat: javaRandomSetSeed}},
		12: {Called: "nextInt()I", Method: javaPlatformMethod{Words: 1, Implementat: javaRandomNext}},
	},
	// `java/util/Calendar`. Slot 14 is the fifth method the specification
	// declares that is not a static and does not override one of Object's, so
	// it is `get(int)` — the only method on this class a title calls at all,
	// once per component of the date it shows.
	javaCalendarClass: {
		14: {Called: "get(I)I", Method: javaPlatformMethod{Words: 2, Implementat: javaCalendarGet}},
		// Slot 19 takes the receiver alone and is the tenth of this class's
		// own run, which the specification's declaration order makes
		// `getTimeZone`.
		19: {Called: "getTimeZone()Ljava/util/TimeZone;",
			Method: javaPlatformMethod{Words: 1, Implementat: javaCalendarZone}},
		// Slot 29 is where the site wins over the rule, which is the case the
		// rule was written with. The site is three calls long and leaves
		// nothing to read into it: `Calendar.getInstance()`, then a
		// `new Date()` on the line after, then this slot on the calendar with
		// that Date as its only argument and its answer dropped. **The one
		// method this class declares that takes a Date is `setTime`**, so
		// there is no second reading of the call — but the numbering puts
		// `setTime` at 11, second in this class's own run, and 29 is eight
		// past where that run ends. Either this module was compiled against a
		// fuller Calendar than the one the other slots here were read off, or
		// the run does not start where it appears to; nothing read so far
		// says which, and the disagreement is the point rather than a detail.
		// Nothing else in the local set dispatches this slot.
		29: {Called: "setTime(Ljava/util/Date;)V",
			Method: javaPlatformMethod{Words: 2, Implementat: javaCalendarSetTime}},
	},
	// `java/util/Date` and `java/util/TimeZone`, both of whose own runs are
	// short enough to state whole: a Date declares getTime then setTime, and a
	// TimeZone declares getOffset, getRawOffset, useDaylightTime and getID.
	javaDateClass: {
		10: {Called: "getTime()J", Method: javaPlatformMethod{Words: 1, Implementat: javaDateTime}},
	},
	javaTimeZoneClass: {
		11: {Called: "getRawOffset()I", Method: javaPlatformMethod{Words: 1, Implementat: javaTimeZoneOffset}},
	},
	// `java/io/ByteArrayOutputStream`. The three writes it inherits from
	// `java/io/OutputStream` take that class's slots — 10, 11 and 12 — and the
	// three of its own follow the five OutputStream declares, at 15, 16 and 17
	// in the order the specification declares them.
	// `java/io/DataOutputStream`. It overrides four of `java/io/OutputStream`'s
	// five and takes their slots, so its own ten writes run from 15 in the
	// order the specification declares them.
	javaDataOutputStreamClass: {
		10: {Called: "write(I)V", Method: javaPlatformMethod{Words: 2, Implementat: javaByteSinkWrite}},
		11: {Called: "write([B)V", Method: javaPlatformMethod{Words: 2, Implementat: javaByteSinkWriteAll}},
		12: {Called: "write([BII)V", Method: javaPlatformMethod{Words: 4, Implementat: javaByteSinkWriteRange}},
		13: {Called: "flush()V", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkFlush}},
		14: {Called: "close()V", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkFlush}},
		15: {Called: "writeBoolean(Z)V", Method: javaPlatformMethod{Words: 2, Implementat: javaSinkAppend(1)}},
		16: {Called: "writeByte(I)V", Method: javaPlatformMethod{Words: 2, Implementat: javaSinkAppend(1)}},
		17: {Called: "writeShort(I)V", Method: javaPlatformMethod{Words: 2, Implementat: javaSinkAppend(2)}},
		18: {Called: "writeChar(I)V", Method: javaPlatformMethod{Words: 2, Implementat: javaSinkAppend(2)}},
		19: {Called: "writeInt(I)V", Method: javaPlatformMethod{Words: 2, Implementat: javaSinkAppend(4)}},
		20: {Called: "writeLong(J)V", Method: javaPlatformMethod{Words: 3, Implementat: javaSinkWriteLong}},
		24: {Called: "writeUTF(Ljava/lang/String;)V", Method: javaPlatformMethod{Words: 2, Implementat: javaSinkWriteUTF}},
	},
	javaByteSinkClass: {
		10: {Called: "write(I)V", Method: javaPlatformMethod{Words: 2, Implementat: javaByteSinkWrite}},
		11: {Called: "write([B)V", Method: javaPlatformMethod{Words: 2, Implementat: javaByteSinkWriteAll}},
		12: {Called: "write([BII)V", Method: javaPlatformMethod{Words: 4, Implementat: javaByteSinkWriteRange}},
		13: {Called: "flush()V", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkFlush}},
		14: {Called: "close()V", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkFlush}},
		15: {Called: "reset()V", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkReset}},
		16: {Called: "toByteArray()[B", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkBytes}},
		17: {Called: "size()I", Method: javaPlatformMethod{Words: 1, Implementat: javaByteSinkSize}},
	},
	javaVectorClass: {
		15: {Called: "size()I", Method: javaPlatformMethod{Words: 1, Implementat: javaVectorSize}},
		// Slot 19 takes the receiver and one reference and its answer is kept.
		// **One call site cannot choose between `indexOf`, `contains` and
		// `removeElement`**, which all take one reference and answer something,
		// and this is where the numbering above earns its keep: `size` at 15
		// and `addElement` at 29 are fourteen apart, which is exactly what this
		// class's declaration order puts between them, and the same order puts
		// `contains` at 18, `indexOf(Object)` at 19 and `removeElement` at 30.
		//
		// The site agrees with the number rather than merely allowing it. It
		// dispatches this slot on a vector of strings with a string that is in
		// it, and hands the answer straight to `new char[n][]` and
		// `new int[n][3]` — a count of rows, which `indexOf` answers and a
		// boolean does not.
		// Slot 16 takes the receiver alone and its answer is compared with
		// zero and branched on — a boolean rather than the count `size`
		// answers one slot below it, which is the pair this class declares in
		// that order.
		16: {Called: "isEmpty()Z", Method: javaPlatformMethod{Words: 1, Implementat: javaVectorEmpty}},
		19: {Called: "indexOf(Ljava/lang/Object;)I",
			Method: javaPlatformMethod{Words: 2, Implementat: javaVectorIndexOf}},
		// Slot 23 is where a title's save stops, and two things settle it. The
		// numbering that puts `size` at 15 and `addElement` at 29 puts
		// `elementAt(int)` at 23 — it is the fourteenth method this class
		// declares, and every anchor around it agrees. And the call site says
		// the answer is a reference: it lands in a register the next
		// instruction tests against zero, and the branch that survives the
		// test loads a vtable out of it and dispatches. None of this class's
		// int or boolean methods can be read that way, and of the ones that
		// answer an object this is the one that takes an index — which the
		// call passes as zero.
		23: {Called: "elementAt(I)Ljava/lang/Object;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaVectorAt}},
		// Slots 24 and 27 arrive as a pair, and the pair is what confirms both:
		// the title takes the answer of 24 — receiver only, an object it then
		// dispatches on — works with it, and calls 27 with a literal zero and
		// drops the answer. `firstElement()` and `removeElementAt(0)` are a
		// vector drained as a queue, and the numbering puts them at exactly
		// those two slots.
		24: {Called: "firstElement()Ljava/lang/Object;",
			Method: javaPlatformMethod{Words: 1, Implementat: javaVectorFirst}},
		27: {Called: "removeElementAt(I)V",
			Method: javaPlatformMethod{Words: 2, Implementat: javaVectorRemoveAt}},
		// Slot 28 takes the receiver, one reference and a number, and the
		// number its caller passes is zero — an element put at the front of a
		// list rather than on the end, which is `insertElementAt` and is what
		// a title keeping a most-recent-first list does.
		28: {Called: "insertElementAt(Ljava/lang/Object;I)V",
			Method: javaPlatformMethod{Words: 3, Implementat: javaVectorInsertAt}},
		// Slot 29 takes the receiver and one reference, and **its answer is
		// dropped**: the instruction after the call reloads the base register
		// and never reads r0. Of the one-reference methods on this class, the
		// void one is `addElement` — a caller has no reason to throw away what
		// `removeElement`, `contains` or `indexOf` answered. The call site
		// agrees on the rest of it too: the vector comes out of a static field
		// and is null-checked, the argument is the object built one
		// instruction earlier, and the whole thing sits in a loop counting a
		// local up to a field.
		29: {Called: "addElement(Ljava/lang/Object;)V",
			Method: javaPlatformMethod{Words: 2, Implementat: javaVectorAdd}},
		// Slot 31 takes the receiver alone — the instruction before the
		// dispatch moves the vector into r0 and sets nothing else — and its
		// answer is dropped: the site reloads a base register and stores a
		// zero without reading r0. That is a no-argument void method, and the
		// numbering that put the eight slots above where their own call sites
		// found them puts `removeAllElements` at 31 and nothing else there.
		31: {Called: "removeAllElements()V",
			Method: javaPlatformMethod{Words: 1, Implementat: javaVectorClear}},
	},
	// `java/util/Stack`, whose own methods follow Vector's — 22 of them from
	// 10, `toString` taking Object's slot rather than one of its own, so this
	// class's run starts at 32. It declares `push`, `pop`, `peek`, `empty` and
	// `search` in that order. Slot 32 takes the receiver and one reference,
	// which is `push`; the specification has it answer the item it was given.
	javaStackClass: {
		32: {Called: "push(Ljava/lang/Object;)Ljava/lang/Object;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaStackPush}},
		// Slot 33 takes the receiver alone and its answer is dispatched on,
		// which is `pop` — the next method this class declares and the one a
		// title that has just pushed comes back for.
		33: {Called: "pop()Ljava/lang/Object;",
			Method: javaPlatformMethod{Words: 1, Implementat: javaStackPop}},
	},
	javaStringBufferClass: {
		4: {Called: "toString()Ljava/lang/String;",
			Method: javaPlatformMethod{Words: 1, Implementat: javaBufferToString}},
		// Slot 17 sits one before the String append and takes one reference
		// that is not a String: the call site hands it the exception it just
		// caught, on its way to a line it prints. `append(Object)` is what the
		// specification declares there.
		17: {Called: "append(Ljava/lang/Object;)Ljava/lang/StringBuffer;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaBufferAppendObject}},
		18: {Called: "append(Ljava/lang/String;)Ljava/lang/StringBuffer;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaBufferAppendText}},
		// Slot 23 takes one word and answers the buffer, the same shape as
		// slot 18. **What the word is, is what tells them apart**: its call
		// site loads a local that the loop around it increments by one and
		// compares against thirteen, so it is a number and not a reference,
		// which makes this `append(int)`.
		// Slot 10 takes the receiver alone and its answer is tested against
		// zero right after a loop that appended to the buffer: `length`. It is
		// **the same slot number `java/lang/String`'s length takes**, which is
		// what a platform header declaring the two side by side would produce.
		10: {Called: "length()I",
			Method: javaPlatformMethod{Words: 1, Implementat: javaBufferLength}},
		// Slot 13 takes the receiver and a zero, drops its answer, and runs
		// where a splitter has just stored the token it built and is about to
		// build the next one. Emptying the buffer there is `setLength(0)`;
		// the other one-argument candidates answer something the call site
		// would have kept.
		13: {Called: "setLength(I)V",
			Method: javaPlatformMethod{Words: 2, Implementat: javaBufferSetLength}},
		// Slot 22 takes one word too, but its call site loads it with `ldrh`
		// out of a `char[]` element inside a loop over one — a code unit, not
		// a number to render, which is `append(char)`.
		22: {Called: "append(C)Ljava/lang/StringBuffer;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaBufferAppendChar}},
		// Slot 27 takes the receiver and two numbers, and the pair its caller
		// passes is a zero and the buffer's own length: a buffer emptied for
		// the next line rather than rebuilt. The numbering puts `delete` there
		// and nothing else on this class takes two numbers and answers the
		// buffer.
		27: {Called: "delete(II)Ljava/lang/StringBuffer;",
			Method: javaPlatformMethod{Words: 3, Implementat: javaBufferDelete}},
		23: {Called: "append(I)Ljava/lang/StringBuffer;",
			Method: javaPlatformMethod{Words: 2, Implementat: javaBufferAppendInt}},
	},
}

// javaFreeMemory answers what is left of the arena the guest allocates from,
// which is what the WIPI C call of the same name answers.
//
// **It is a `long`, so the high word has to be written.** A method that answers
// one puts it in r1 and the low half in r0, the way `currentTimeMillis` does;
// answering only r0 leaves whatever the caller happened to have there as the
// top half of the number, and a title comparing `freeMemory() < n` compares
// against that. The arena is megabytes, so the high word is zero — but it has
// to be a written zero rather than a hoped-for one.
func javaFreeMemory(
	client *Client, _ context.Context, thread *armcore.Thread, _ []uint32,
) (uint32, error) {
	if err := thread.SetRegister(1, 0); err != nil {
		return 0, err
	}
	return uint32(client.heap.capacity() - client.heap.used()), nil
}

// javaTotalMemory answers the whole of the arena, which is what
// `Runtime.totalMemory` means: the heap the runtime has, not the part of it
// still free. It is a `long` for the same reason `freeMemory` is.
func javaTotalMemory(
	client *Client, _ context.Context, thread *armcore.Thread, _ []uint32,
) (uint32, error) {
	if err := thread.SetRegister(1, 0); err != nil {
		return 0, err
	}
	return uint32(client.heap.capacity()), nil
}

// javaRandomNext answers the next number of a title's own generator, over the
// whole signed range the language gives `nextInt`.
func javaRandomNext(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	source, ok := client.javaRuntimeState().random[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a generator this platform built", arguments[0])
	}
	return uint32(source.Uint32()), nil
}

// javaFileSystemAvailable is the room left for a title's own files.
func javaFileSystemAvailable(
	_ *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	return databaseCapacity, nil
}

// javaRandomSetSeed reseeds a generator, which the language defines as putting
// it back to the sequence that seed names — so a title that reseeds with a
// number it stored gets the run it had before.
func javaRandomSetSeed(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	// The constructor arrives here too, on an object that has no generator
	// yet: seeding one and reseeding one are the same act.
	runtime := client.javaRuntimeState()
	seed := int64(arguments[2])<<32 | int64(arguments[1])
	runtime.random[arguments[0]] = rand.New(rand.NewSource(seed))
	return 0, nil
}

// javaRandomConstructor seeds the receiver's own generator. The object is the
// module's; what stands behind it is this platform's, keyed by the object.
func javaRandomConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().random[arguments[0]] = rand.New(rand.NewSource(client.clock.unixMillis()))
	return 0, nil
}

// javaSetVolume moves the level every other sound call reads.
func javaSetVolume(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.volume = clampVolume(int32(arguments[0]))
	return 0, nil
}

func javaScreenWidth(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	return uint32(client.screen.width), nil
}

func javaScreenHeight(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	return uint32(client.screen.height), nil
}

// javaURLFind refuses a socket the way the specification refuses one, and
// names what was dialled while doing it: a refused connection that reports the
// address is what says afterwards whether a title stopped at its own server or
// at this platform.
func javaURLFind(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	url, ok := client.javaText(arguments[0])
	if !ok {
		url = fmt.Sprintf("the string at %#x", arguments[0])
	}
	if client.logger != nil {
		client.logger.Debug("LGT java socket refused", "url", url)
	}
	return 0, client.throwJavaPlatform(thread, javaSchemeNotFoundClass, ": "+url)
}

// javaSchemeNotFoundClass is `find`'s documented failure, and the
// specification roots it at `IOException` — which is what makes a title's
// `catch (IOException)` around a connection attempt match it.
const javaSchemeNotFoundClass = "org/kwis/msf/io/SchemeNotFoundException"

func javaNoResult(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return 0, nil
}

// javaZeroResult is for a method that answers a number this platform has none
// of, where zero is the true answer rather than a placeholder.
func javaZeroResult(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return 0, nil
}

func javaMathAbs(_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	value := int32(arguments[0])
	if value < 0 {
		value = -value
	}
	return uint32(value), nil
}

// javaCurrentTimeMillis answers the epoch milliseconds a title reads, as the
// two words a 64-bit result comes back in.
func javaCurrentTimeMillis(
	client *Client, _ context.Context, thread *armcore.Thread, _ []uint32,
) (uint32, error) {
	milliseconds := uint64(client.clock.unixMillis())
	if err := thread.SetRegister(1, uint32(milliseconds>>32)); err != nil {
		return 0, err
	}
	return uint32(milliseconds), nil
}

// javaPlatformSingleton answers the one instance of a platform class a title
// asks for by name, building it on the first ask.
func javaPlatformSingleton(name string) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
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
}

// callJavaPlatformStatic services a call through the static-method array.
func (client *Client) callJavaPlatformStatic(
	ctx context.Context, thread *armcore.Thread, index uint32,
) (bool, error) {
	link := client.javaLink
	if link == nil || link.surface == nil || int(index) >= len(link.surface.StaticMethods) {
		return false, nil
	}
	member := link.surface.StaticMethods[index]
	owner, known := link.surface.ownerOf(
		func(class javaAPIClass) javaRun { return class.StaticMethods }, index)
	if !known {
		return false, nil
	}
	method, ok := javaPlatformMethods[javaPlatformKey(owner, member)]
	if !ok {
		called, resolved, byShape := client.javaStaticByDescriptor(owner, index, member)
		if !byShape {
			return false, nil
		}
		method, ok = resolved, true
		member = javaMemberRef{Name: called, Descriptor: member.Descriptor}
	}
	return true, client.callJavaMethod(ctx, thread, owner, member.String(), method)
}

// javaStaticByDescriptor resolves an entry the module left **unnamed but
// described**: one local title hands over a `()I` on `org/kwis/msf/io/Network`
// whose name pointer is null, between the named `disconnect()V` and
// `<init>()V` of the same class.
//
// The descriptor and the class are enough when they leave one answer: of the
// methods this platform implements on that class, take those with that
// descriptor and drop every one another entry of the same run already names.
// **One survivor is a resolution; two are not**, and an ambiguous entry is
// reported unnamed rather than guessed at.
func (client *Client) javaStaticByDescriptor(
	owner string, index uint32, member javaMemberRef,
) (string, javaPlatformMethod, bool) {
	link := client.javaLink
	if member.Name != "" || member.Descriptor == "" || link == nil || link.surface == nil {
		return "", javaPlatformMethod{}, false
	}
	var run javaRun
	for _, class := range link.surface.Classes {
		if class.Name == owner && class.StaticMethods.contains(index) {
			run = class.StaticMethods
		}
	}
	claimed := map[string]bool{}
	for entry := run.Start; entry < run.Start+run.Count && int(entry) < len(link.surface.StaticMethods); entry++ {
		if named := link.surface.StaticMethods[entry]; named.Name != "" {
			claimed[named.Name] = true
		}
	}
	name, method := "", javaPlatformMethod{}
	for key, candidate := range javaPlatformMethods {
		prefix := owner + "."
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, member.Descriptor) {
			continue
		}
		called := strings.TrimSuffix(strings.TrimPrefix(key, prefix), member.Descriptor)
		if claimed[called] {
			continue
		}
		if name != "" {
			// More than one fits, so the descriptor does not settle it.
			return "", javaPlatformMethod{}, false
		}
		name, method = called, candidate
	}
	if name == "" {
		return "", javaPlatformMethod{}, false
	}
	if client.logger != nil {
		client.logger.Debug("LGT java unnamed static entry resolved by its descriptor",
			"class", owner, "entry", index, "method", name+member.Descriptor)
	}
	return name, method, true
}

// callJavaPlatformVirtual services a dispatch through a platform class's vtable
// slot.
//
// **A slot is only a number, and which numbers mean anything is not uniform.**
// The virtual methods the module lists in its class table are the platform's to
// number, so a slot in one of those runs can be turned back into a name and
// dispatched by name here. The rest — a class the module lists no virtual
// methods for — are numbered by the compiler against a platform this
// implementation does not have, and all that can be said about one is which
// class and which slot. See docs/lgt.md, "Two kinds of virtual call".
func (client *Client) callJavaPlatformVirtual(
	ctx context.Context, thread *armcore.Thread, slot uint32,
) (bool, error) {
	if name, index, ok := client.javaRuntimeState().javaVirtualSlotParts(slot); ok {
		// **A slot a class inherits is reported against the class whose vtable
		// first built the stub**, which is wherever the chain stopped having
		// its own — so a `java/lang/Object` slot reached through a Jlet arrives
		// named for the Jlet. A number valid for a class is valid for every
		// class below it, so the walk up the chain is what makes one entry
		// answer for all of them.
		for owner := name; owner != ""; owner = javaPlatformSuper(owner) {
			if baked, served := javaBakedVirtualSlots[owner][index]; served {
				return true, client.callJavaMethod(ctx, thread, name, baked.Called, baked.Method)
			}
			if owner == "java/lang/Object" {
				break
			}
		}
	}
	class, member, known := client.javaVirtualMember(slot)
	if !known {
		return false, nil
	}
	method, ok := javaPlatformMethods[javaPlatformKey(class, member)]
	if !ok {
		return false, nil
	}
	return true, client.callJavaMethod(ctx, thread, class, member.String(), method)
}

// callJavaMethod runs one implemented platform method: read its arguments the
// way the ABI passes them, run it, and answer in r0.
func (client *Client) callJavaMethod(
	ctx context.Context, thread *armcore.Thread, class, called string, method javaPlatformMethod,
) error {
	arguments, err := client.javaArguments(thread, method.Words)
	if err != nil {
		return err
	}
	result, err := method.Implementat(client, ctx, thread, arguments)
	if errors.Is(err, errJavaThrowHandled) {
		// The method threw and a try region took it. The guest is already in
		// its handler with the exception in the answer register, and writing a
		// result over it is what made a caught failure read as a success.
		return nil
	}
	if err != nil {
		// The stack is part of the failure, not a detail beside it: a platform
		// method that cannot answer stops a title in one of the title's own
		// methods, and which one is what says whether the gap is on a path that
		// matters.
		return fmt.Errorf("%s.%s: %w (in %s)", class, called, err, client.javaBacktraceLine(thread))
	}
	if client.logger != nil {
		// The caller's address is logged with the call because a Java method
		// says nothing about which of a title's own methods asked for it, and
		// finding that out from the name alone means disassembling the whole
		// module. `lr` is the return address into the module's own code.
		caller, _ := thread.Register(14)
		client.logger.Debug("LGT java platform call",
			"method", class+"."+called, "arguments", formatWords(arguments),
			"result", result, "from", fmt.Sprintf("%#x", caller))
	}
	return thread.SetRegister(0, result)
}

// javaVirtualMember answers which method a platform class's vtable slot stands
// for, when the module's own tables say.
func (client *Client) javaVirtualMember(slot uint32) (string, javaMemberRef, bool) {
	if client.javaLink == nil || client.javaLink.layout == nil || client.javaLink.surface == nil {
		return "", javaMemberRef{}, false
	}
	name, index, known := client.javaRuntimeState().javaVirtualSlotParts(slot)
	if !known {
		return "", javaMemberRef{}, false
	}
	// The same walk the baked slots take, and for the same reason: a class
	// inherits its superclass's slots, and the name a slot was numbered under
	// is the class that declared the method, not the one dispatching on it.
	for owner := name; owner != ""; owner = javaPlatformSuper(owner) {
		laid, ok := client.javaLink.layout.classes[owner]
		if ok {
			for key, assigned := range laid.Virtual {
				if assigned != index {
					continue
				}
				for _, member := range client.javaLink.surface.VirtualMethods {
					if javaMemberKey(member) == key {
						return owner, member, true
					}
				}
			}
		}
		if owner == "java/lang/Object" {
			break
		}
	}
	return name, javaMemberRef{}, false
}

// javaArguments reads a call's argument words: four in registers, the rest on
// the stack, which is the ABI every other call on this platform uses.
func (client *Client) javaArguments(thread *armcore.Thread, words int) ([]uint32, error) {
	values := make([]uint32, words)
	for index := range values {
		if index < 4 {
			value, err := thread.Register(index)
			if err != nil {
				return nil, err
			}
			values[index] = value
			continue
		}
		stack, err := thread.Register(armcore.RegisterSP)
		if err != nil {
			return nil, err
		}
		value, err := client.readWord(stack + uint32(index-4)*4)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

// javaMathLong is `Math.min(long, long)` and `Math.max(long, long)`, which
// differ only in which way the comparison runs.
func javaMathLong(smallest bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(
		_ *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
	) (uint32, error) {
		left := int64(uint64(arguments[1])<<32 | uint64(arguments[0]))
		right := int64(uint64(arguments[3])<<32 | uint64(arguments[2]))
		answer := left
		if (right < left) == smallest {
			answer = right
		}
		if err := thread.SetRegister(1, uint32(uint64(answer)>>32)); err != nil {
			return 0, err
		}
		return uint32(uint64(answer)), nil
	}
}
