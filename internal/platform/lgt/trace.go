package lgt

import (
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The platform's failures here are almost never at the instruction that
// reports them. A slot answers 0 or an error code, the game stores that word,
// and the dereference happens hundreds of instructions later in code that has
// no idea a platform call was involved. The fault address names the game, not
// the cause.
//
// So the last calls before a fault are the evidence, and this records them.
// It is a ring buffer rather than a log because the interesting run is
// millions of instructions long and only its tail matters, and because the
// logging boundary would serialize every one of them.

// SVCCall is one platform call as it was serviced.
type SVCCall struct {
	// Category and Slot are the SVC's own identity: which import table and
	// which entry in it.
	Category uint32
	Slot     uint32
	// Name is the slot's symbolic name, or an empty string when the slot has
	// no name here — which is itself the finding, since an unnamed slot is one
	// this platform is guessing at.
	Name string
	// Arguments are r0-r3 on entry. The AAPCS passes the rest on the stack;
	// four covers every slot this platform implements.
	Arguments [4]uint32
	// Result is r0 on return, and Failed says whether to read it at all.
	Result uint32
	Failed bool
	Error  string
	// ReturnTo is the link register on entry, which is the address in the
	// game's own code that made the call. This is the number that turns a
	// trace into a place to look.
	ReturnTo uint32
	// Detail is the call's arguments read as what they are, for the slots
	// whose arguments are names rather than numbers. `fsOpen(0x400fff40, 0x1)`
	// says nothing; `fsOpen("Save0.dat", read)` is the whole finding. It is
	// empty for every other slot, and the raw registers are printed either way.
	Detail string
}

// String renders one call the way the trace dump reads it.
func (call SVCCall) String() string {
	name := call.Name
	if name == "" {
		name = "unnamed"
	}
	var builder strings.Builder
	arguments := fmt.Sprintf("%#x, %#x, %#x, %#x",
		call.Arguments[0], call.Arguments[1], call.Arguments[2], call.Arguments[3])
	if call.Detail != "" {
		arguments = call.Detail + " | " + arguments
	}
	fmt.Fprintf(&builder, "%s %#x %s(%s)", svcCategoryName(call.Category), call.Slot, name, arguments)
	if call.Failed {
		fmt.Fprintf(&builder, " = failed: %s", call.Error)
	} else {
		fmt.Fprintf(&builder, " = %#x (%d)", call.Result, int32(call.Result))
	}
	fmt.Fprintf(&builder, " from %#x", call.ReturnTo)
	return builder.String()
}

// svcTrace is a fixed-size ring of the most recent calls.
type svcTrace struct {
	entries []SVCCall
	next    int
	filled  bool
}

func newSVCTrace(size int) *svcTrace {
	return &svcTrace{entries: make([]SVCCall, size)}
}

func (trace *svcTrace) record(call SVCCall) {
	trace.entries[trace.next] = call
	trace.next++
	if trace.next == len(trace.entries) {
		trace.next = 0
		trace.filled = true
	}
}

// calls returns the recorded calls oldest first.
func (trace *svcTrace) calls() []SVCCall {
	if !trace.filled {
		return append([]SVCCall(nil), trace.entries[:trace.next]...)
	}
	ordered := make([]SVCCall, 0, len(trace.entries))
	ordered = append(ordered, trace.entries[trace.next:]...)
	return append(ordered, trace.entries[:trace.next]...)
}

// SVCTrace returns the recorded platform calls, oldest first. It is empty
// unless Options.TraceSVC asked for a trace.
func (client *Client) SVCTrace() []SVCCall {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.trace == nil {
		return nil
	}
	return client.trace.calls()
}

// FormatSVCTrace renders the recorded calls one per line, oldest first, with
// the newest last because that is the one a fault follows.
func FormatSVCTrace(calls []SVCCall) string {
	var builder strings.Builder
	for index, call := range calls {
		fmt.Fprintf(&builder, "%4d  %s\n", index-len(calls), call)
	}
	return builder.String()
}

// traceEntry captures a call's arguments before it runs. The result is filled
// in by the deferred half, because a slot that fails part way through is
// exactly the case the trace exists for.
func (client *Client) traceEntry(thread *armcore.Thread, category, slot uint32) *SVCCall {
	if client.trace == nil {
		return nil
	}
	call := &SVCCall{Category: category, Slot: slot, Name: client.svcSlotName(category, slot)}
	for index := range call.Arguments {
		// A register that cannot be read is not worth failing a run over when
		// the only consumer is a diagnostic.
		value, err := thread.Register(index)
		if err != nil {
			break
		}
		call.Arguments[index] = value
	}
	if link, err := thread.Register(14); err == nil {
		call.ReturnTo = link
	}
	call.Detail = client.traceDetail(category, slot, call.Arguments)
	return call
}

// traceDetail reads the arguments of the slots that take names. It runs only
// for those slots: a title asks for the framebuffer width tens of thousands of
// times a second, and reading guest memory on every platform call would make
// the trace change what it is measuring.
func (client *Client) traceDetail(category, slot uint32, arguments [4]uint32) string {
	if category != svcCategoryWIPIC {
		return ""
	}
	switch slot {
	case slotFsOpen:
		return fmt.Sprintf("%q, %s", client.traceString(arguments[0]), fileOpenFlagName(arguments[1]))
	case slotFsIsExist, slotFsRemove, slotFsFileAttribute, slotFsMkDir, slotFsRmDir, slotFsList,
		slotGetResourceID, slotGetResource, slotGetDefaultVolume:
		return fmt.Sprintf("%q", client.traceString(arguments[0]))
	case slotSprintk:
		// The format is the second argument here and the first for printk;
		// both are what names the call site.
		return fmt.Sprintf("%q", client.traceString(arguments[1]))
	case slotPrintk:
		return fmt.Sprintf("%q", client.traceString(arguments[0]))
	}
	return ""
}

// traceString reads a guest string for the trace. A pointer that is not one is
// an ordinary outcome here — the trace runs before the slot has checked
// anything — so it reads as its number rather than as an error.
func (client *Client) traceString(address uint32) string {
	if address == 0 {
		return "<null>"
	}
	text, err := client.readCString(address)
	if err != nil {
		return fmt.Sprintf("<%#x>", address)
	}
	return text
}

// fileOpenFlagName names an MC_fsOpen mode, because which flag a title opened
// its save with is the difference between a read that may fail and a write
// that creates.
func fileOpenFlagName(flag uint32) string {
	switch flag {
	case fileOpenReadOnly:
		return "read"
	case fileOpenWriteOnly:
		return "write/append"
	case fileOpenWriteTruncate:
		return "write/truncate"
	case fileOpenReadWrite:
		return "read-write"
	}
	return fmt.Sprintf("mode %#x", flag)
}

// traceExit completes a captured call and files it.
func (client *Client) traceExit(thread *armcore.Thread, call *SVCCall, err error) {
	if call == nil || client.trace == nil {
		return
	}
	if err != nil {
		call.Failed = true
		call.Error = err.Error()
	} else if result, readErr := thread.Register(0); readErr == nil {
		call.Result = result
	}
	// The slot handlers take this lock themselves and have released it by the
	// time a call is filed, so taking it here cannot re-enter.
	client.mu.Lock()
	defer client.mu.Unlock()
	client.trace.record(*call)
	if client.traceOut == nil {
		return
	}
	if line := call.String(); client.traceLive == "" || strings.Contains(line, client.traceLive) {
		fmt.Fprintln(client.traceOut, line)
	}
}

func svcCategoryName(category uint32) string {
	switch category {
	case svcCategoryInit:
		return "init"
	case svcCategoryWIPIC:
		return "wipic"
	case svcCategoryStdlib:
		return "stdlib"
	case svcCategoryOEM:
		return "oem"
	case svcCategoryJava:
		return "java"
	}
	return fmt.Sprintf("category%d", category)
}

// svcSlotName names a slot for the trace, including the Java ones, which need
// the loaded module to name: a static entry and a vtable slot are numbers until
// the class tables the module handed over say what they stand for.
//
// **Without this a Java title's whole trace reads `unnamed`** — every line of
// it, since a Java title makes almost no C calls — and that is the trace of the
// titles whose remaining faults are all on the Java side. Naming them is what
// turns a screen that will not advance into a list of what it was asking for
// while it would not.
func (client *Client) svcSlotName(category, slot uint32) string {
	if category == svcCategoryJava {
		return client.javaSlotName(slot)
	}
	return svcSlotName(category, slot)
}

// javaSlotName is the compact form of the same naming a failure at a Java slot
// reports. A trace line wants the member and nothing else, so the entry number
// the failure message carries is left off here — the slot is already on the
// line.
func (client *Client) javaSlotName(slot uint32) string {
	if index, static := javaStaticMethodParts(slot); static {
		if client.javaLink == nil || client.javaLink.surface == nil ||
			int(index) >= len(client.javaLink.surface.StaticMethods) {
			return ""
		}
		member := client.javaLink.surface.StaticMethods[index].String()
		owner, known := client.javaLink.surface.ownerOf(
			func(class javaAPIClass) javaRun { return class.StaticMethods }, index)
		switch {
		case member == "":
			// The two entries every class's run opens with carry no name; both
			// answer with the class. See java.go.
			return owner + ".<class>"
		case known:
			return owner + "." + member
		}
		return member
	}
	if slot&javaSlotVirtual != 0 {
		name, index, known := client.javaRuntimeState().javaVirtualSlotParts(slot)
		if !known {
			return ""
		}
		if _, member, ok := client.javaVirtualMember(slot); ok {
			return name + "." + member.String()
		}
		return fmt.Sprintf("%s.slot%d", name, index)
	}
	return javaSVCNames[slot]
}

// svcSlotName names a slot for the trace. An empty answer means this platform
// has no name for it, which the dump prints as "unnamed" — the slots worth
// looking at first.
func svcSlotName(category, slot uint32) string {
	switch category {
	case svcCategoryInit:
		switch slot {
		case initSVCGetImportTable:
			return "getImportTable"
		case initSVCGetImportFunction:
			return "getImportFunction"
		}
	case svcCategoryWIPIC:
		return wipicSlotNames[slot]
	case svcCategoryStdlib:
		return stdlibSlotNames[slot]
	case svcCategoryOEM:
		switch slot {
		case oemSlotConfigure:
			return "configure"
		case oemSlotJava:
			return "java"
		}
	}
	return ""
}

// wipicSlotNames names the WIPI C slots this platform knows. The names are the
// platform's own, not the specification's symbols, because the specification
// names a function per module and this table is a flat index.
var wipicSlotNames = map[uint32]string{
	slotCletRegister:                "cletRegister",
	slotFramebufferPointer:          "framebufferPointer",
	slotFramebufferWidth:            "framebufferWidth",
	slotFramebufferHeight:           "framebufferHeight",
	slotFramebufferBpl:              "framebufferBpl",
	slotFramebufferBpp:              "framebufferBpp",
	slotPrintk:                      "printk",
	slotSprintk:                     "sprintk",
	slotGetCurProgramID:             "getCurProgramID",
	slotExit:                        "exit",
	slotAlloc:                       "alloc",
	slotCalloc:                      "calloc",
	slotFree:                        "free",
	slotTotalMemory:                 "totalMemory",
	slotFreeMemory:                  "freeMemory",
	slotDefTimer:                    "defTimer",
	slotSetTimer:                    "setTimer",
	slotUnsetTimer:                  "unsetTimer",
	slotCurrentTime:                 "currentTime",
	slotGetProperty:                 "getProperty",
	slotSetProperty:                 "setProperty",
	slotGetResourceID:               "getResourceID",
	slotGetResource:                 "getResource",
	slotProgramApplicationID:        "programApplicationID",
	slotGetImageProperty:            "getImageProperty",
	slotGetImageFramebuffer:         "getImageFramebuffer",
	slotGetScreenFramebuffer:        "getScreenFramebuffer",
	slotDestroyOffscreen:            "destroyOffscreen",
	slotCreateOffscreen:             "createOffscreen",
	slotInitContext:                 "initContext",
	slotSetContext:                  "setContext",
	slotGetContext:                  "getContext",
	slotPutPixel:                    "putPixel",
	slotDrawLine:                    "drawLine",
	slotDrawRect:                    "drawRect",
	slotFillRect:                    "fillRect",
	slotCopyFramebuffer:             "copyFramebuffer",
	slotDrawImage:                   "drawImage",
	slotCopyArea:                    "copyArea",
	slotDrawArc:                     "drawArc",
	slotFillArc:                     "fillArc",
	slotDrawString:                  "drawString",
	slotGetRGBPixels:                "getRGBPixels",
	slotSetRGBPixels:                "setRGBPixels",
	slotFlushLcd:                    "flushLcd",
	slotGetPixelFromRGB:             "getPixelFromRGB",
	slotGetRGBFromPixel:             "getRGBFromPixel",
	slotGetDisplayInfo:              "getDisplayInfo",
	slotRepaint:                     "repaint",
	slotGetFont:                     "getFont",
	slotGetFontHeight:               "getFontHeight",
	slotGetFontAscent:               "getFontAscent",
	slotGetFontDescent:              "getFontDescent",
	slotGetStringWidth:              "getStringWidth",
	slotCreateImage:                 "createImage",
	slotDestroyImage:                "destroyImage",
	slotDecodeNextImage:             "decodeNextImage",
	slotPostEvent:                   "postEvent",
	slotIMGetSupportedModeCount:     "imGetSupportedModeCount",
	slotIMGetSupportedModes:         "imGetSupportedModes",
	slotIMSetCurrentMode:            "imSetCurrentMode",
	slotIMGetCurrentMode:            "imGetCurrentMode",
	slotIMHandleInput:               "imHandleInput",
	slotFsOpen:                      "fsOpen",
	slotFsRead:                      "fsRead",
	slotFsWrite:                     "fsWrite",
	slotFsClose:                     "fsClose",
	slotFsSeek:                      "fsSeek",
	slotFsRemove:                    "fsRemove",
	slotFsRename:                    "fsRename",
	slotFsFileAttribute:             "fsFileAttribute",
	slotFsMkDir:                     "fsMkDir",
	slotFsRmDir:                     "fsRmDir",
	slotFsList:                      "fsList",
	slotFsTotalSpace:                "fsTotalSpace",
	slotFsAvailable:                 "fsAvailable",
	slotFsTell:                      "fsTell",
	slotGetProgramName:              "getProgramName",
	slotDbListDataBases:             "dbListDataBases",
	slotFsIsExist:                   "fsIsExist",
	slotNetConnect:                  "netConnect",
	slotNetSocket:                   "netSocket",
	slotBackLight:                   "backLight",
	slotVibrator:                    "vibrator",
	slotClipCreate:                  "clipCreate",
	slotClipFree:                    "clipFree",
	slotClipGetType:                 "clipGetType",
	slotClipPutData:                 "clipPutData",
	slotClipClearData:               "clipClearData",
	slotClipGetVolume:               "clipGetVolume",
	slotClipSetVolume:               "clipSetVolume",
	slotClipPlay:                    "clipPlay",
	slotClipPause:                   "clipPause",
	slotClipResume:                  "clipResume",
	slotClipStop:                    "clipStop",
	slotGetVolume:                   "getVolume",
	slotGetDefaultVolume:            "getDefaultVolume",
	slotSetVolume:                   "setVolume",
	slotClipAllocPlayer:             "clipAllocPlayer",
	slotClipFreePlayer:              "clipFreePlayer",
	slotSetSourceVolume:             "setSourceVolume",
	slotGetSourceVolume:             "getSourceVolume",
	slotSetMuteState:                "setMuteState",
	slotGetMuteState:                "getMuteState",
	slotUicCreateApplicationContext: "uicCreateApplicationContext",
	slotUicGetClass:                 "uicGetClass",
	slotUicCreate:                   "uicCreate",
}

// stdlibSlotNames names the C library slots.
var stdlibSlotNames = map[uint32]string{
	stdlibSprintf:   "sprintf",
	stdlibAtoi:      "atoi",
	stdlibStrcpy:    "strcpy",
	stdlibStrncpy:   "strncpy",
	stdlibStrcat:    "strcat",
	stdlibStrncat:   "strncat",
	stdlibStrcmp:    "strcmp",
	stdlibStrncmp:   "strncmp",
	stdlibStrchr:    "strchr",
	stdlibStrrchr:   "strrchr",
	stdlibStrspn:    "strspn",
	stdlibStrcspn:   "strcspn",
	stdlibStrpbrk:   "strpbrk",
	stdlibStrstr:    "strstr",
	stdlibStrlen:    "strlen",
	stdlibStrtok:    "strtok",
	stdlibFree:      "free",
	stdlibMemcpy:    "memcpy",
	stdlibMemmove:   "memmove",
	stdlibMemcmp:    "memcmp",
	stdlibMemchr:    "memchr",
	stdlibMemset:    "memset",
	stdlibTime:      "time",
	stdlibLocaltime: "localtime",
	// Named for what it was watched doing rather than after a C function; see
	// stdlib.go. It is serviced like the rest, so leaving it out of this table
	// made a trace call it `unnamed` and made an import scan count it as a gap.
	stdlibRunFunction: "runFunction",
}
