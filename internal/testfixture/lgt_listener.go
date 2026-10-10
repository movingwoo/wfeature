package testfixture

import (
	"fmt"
	"strings"
)

const (
	LGTListenerClass          = "Listener"
	LGTListenerStartupCounter = lgtDataBase + 0x100
	LGTListenerHistoryCount   = lgtDataBase + 0x104
	LGTListenerRestartOnce    = lgtDataBase + 0x108
	LGTListenerRestartResult  = lgtDataBase + 0x10c
	// These three words contain addresses filled in by the authored module.
	LGTListenerPlayAddress     = lgtDataBase + 0x110
	LGTListenerClassHandle     = lgtDataBase + 0x114
	LGTListenerCallbackAddress = lgtDataBase + 0x118
	LGTListenerHistory         = lgtDataBase + 0x140
	LGTListenerHistoryCapacity = 64

	lgtListenerTables = lgtDataBase + 0x600
)

// LGTListenerArchive is an authored AOT application that starts through the
// normal Jlet launcher. Its ARM PlayListener stores receiver/Clip/event/parm
// tuples, and can restart the completed Clip once through the imported Player.
func LGTListenerArchive() ([]byte, error) {
	data := &lgtData{bytes: make([]byte, lgtListenerTables-lgtDataBase), next: lgtListenerTables}
	pool := map[string]uint32{}
	names := func(value string) uint32 {
		if address, ok := pool[value]; ok {
			return address
		}
		address := data.text(value)
		pool[value] = address
		return address
	}
	link := lgtListenerLink(data, names)
	p := newLGTProgram(lgtTextBase)
	global := func(index int) { p.through(lgtGlobals + uint32(index)*4) }

	// The module entry is Thumb; bx pc enters the ARM loader immediately after it.
	entry := p.here()
	p.emit(0x46c04778, lgtPushLR, lgtMovReg(4, 0), lgtLdr(5, 1, 4))
	p.literal(6, lgtImportRequests)
	p.literal(7, lgtGlobals)
	p.label("resolve")
	p.emit(lgtLdrPost(0, 6, 4), lgtCmpImm(0, 0))
	p.branch(lgtEQ, "resolved")
	p.emit(lgtLdrPost(1, 6, 4), lgtMovLRPC, lgtBX(5), lgtStrPost(0, 7, 4))
	p.branch(lgtAL, "resolve")
	p.label("resolved")
	p.literal(0, lgtInitStruct)
	p.emit(lgtStr(0, 4, 512+20), lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	initialise := p.here()
	p.emit(lgtPushLR, lgtSubImm(lgtSP, lgtSP, 28))
	for index := 4; index < len(link.load); index++ {
		p.literal(0, link.load[index])
		p.emit(lgtStr(0, lgtSP, uint32(index-4)*4))
	}
	for index := uint32(0); index < 4; index++ {
		p.literal(index, link.load[index])
	}
	global(lgtJavaGlobalLoadClasses)
	p.emit(lgtAddImm(lgtSP, lgtSP, 28))
	p.literal(0, link.statics["Player.play"])
	p.emit(lgtLdr(0, 0, 0))
	p.literal(1, LGTListenerPlayAddress)
	p.emit(lgtStr(0, 1, 0))
	argv := data.reserve(16)
	data.words(argv, names("ListenerApplication"), names(""), names("true"), names("true"))
	p.literal(0, names("org/kwis/msp/lcdui/Main"))
	p.emit(lgtMovImm(1, 0), lgtMovImm(2, 4))
	p.literal(3, argv)
	global(lgtJavaGlobalLauncher)
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	jletInit := p.here()
	p.emit(lgtPushLR)
	p.through(link.statics["Jlet.<init>"])
	p.emit(lgtPopPC)
	p.pool()

	startApp := p.here()
	p.emit(lgtPushLR)
	p.literal(4, LGTListenerStartupCounter)
	p.emit(lgtLdr(0, 4, 0), lgtAddImm(0, 0, 1), lgtStr(0, 4, 0))
	p.literal(0, LGTListenerClassHandle)
	p.emit(lgtLdr(0, 0, 0))
	global(lgtJavaGlobalPrepareClass)
	p.emit(lgtPopPC)
	p.pool()

	listenerInit := p.here()
	p.emit(lgtBXLR)
	callback := p.here()
	p.emit(lgtPushLR)
	p.literal(4, LGTListenerHistoryCount)
	p.emit(lgtLdr(5, 4, 0), lgtCmpImm(5, LGTListenerHistoryCapacity))
	p.branch(2, "listener return") // unsigned >= the history capacity
	p.literal(6, LGTListenerHistory)
	p.emit(lgtAddShift(6, 6, 5, 4), lgtStr(0, 6, 0), lgtStr(1, 6, 4),
		lgtStr(2, 6, 8), lgtStr(3, 6, 12), lgtAddImm(5, 5, 1), lgtStr(5, 4, 0))
	p.emit(lgtCmpImm(2, 1)) // END_OF_DATA
	p.branch(lgtNE, "listener return")
	p.literal(4, LGTListenerRestartOnce)
	p.emit(lgtLdr(5, 4, 0), lgtCmpImm(5, 0))
	p.branch(lgtEQ, "listener return")
	// Consume the hook before reentering Java media, so the new START remains
	// ordinary queued work and cannot restart this callback recursively.
	p.emit(lgtMovImm(5, 0), lgtStr(5, 4, 0), lgtMovReg(0, 1), lgtMovImm(1, 0))
	p.through(LGTListenerPlayAddress)
	p.literal(4, LGTListenerRestartResult)
	p.emit(lgtStr(0, 4, 0))
	p.label("listener return")
	p.emit(lgtPopPC)
	p.pool()
	code, err := p.finish()
	if err != nil {
		return nil, err
	}
	if lgtTextBase+uint32(len(code)) > lgtDataBase {
		return nil, fmt.Errorf("LGT listener fixture: code reaches the data section")
	}

	lgtJavaClassRecord(data, names, "ListenerApplication", "org/kwis/msp/lcdui/Jlet", 6, 21, []lgtJavaMethod{
		{"<init>", "()V", jletInit}, {"startApp", "([Ljava/lang/String;)V", startApp},
	})
	listener := lgtJavaClassRecord(data, names, LGTListenerClass, "java/lang/Object", 3, 12, []lgtJavaMethod{
		{"<init>", "()V", listenerInit}, {"playUpdate", "(Lorg/kwis/msp/media/Clip;II)V", callback},
	})
	// This is a genuine declared interface entry. Its second word is the
	// concrete method address form accepted by compiled invokeinterface calls.
	interfaces, implementation := data.reserve(8), data.reserve(8)
	data.words(interfaces, 1, implementation)
	data.words(implementation, names("org/kwis/msp/media/PlayListener"), callback)
	data.words(listener-lgtJavaClassHeaderSize+0x14, interfaces)
	data.words(LGTListenerClassHandle, listener)
	data.words(LGTListenerCallbackAddress, callback)
	data.words(lgtInitStruct, 0, initialise, 0)
	data.words(lgtImportRequests,
		lgtImportTableJava, lgtJavaLoadClasses,
		lgtImportTableJava, lgtJavaLauncher,
		lgtImportTableJava, lgtJavaStringConstant,
		lgtImportTableJava, lgtJavaPrepareClass,
		0, 0)
	jar, err := fixtureZIP([]fixtureEntry{{"binary.mod", lgtELF(code, data.bytes, entry)}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{
		{"app_info", []byte("AID=0102LIST\nPID=PF0000LS\nMClass=ListenerApplication\nName=Listener Fixture\n")},
		{"0102LIST.jar", jar},
	})
}

// lgtListenerLink uses the same five-table module format as lgtJavaLink, with
// only the constructors and Player methods this fixture imports.
func lgtListenerLink(data *lgtData, names func(string) uint32) lgtJavaLinkage {
	apis := []lgtJavaAPI{
		{"org/kwis/msp/lcdui/Jlet", []string{"<init>()V"}, nil},
		{"org/kwis/msp/media/Clip", []string{"<init>(Ljava/lang/String;[B)V"}, nil},
		{"org/kwis/msp/media/Player", []string{
			"play(Lorg/kwis/msp/media/Clip;Z)Z", "stop(Lorg/kwis/msp/media/Clip;)Z",
			"pause(Lorg/kwis/msp/media/Clip;)Z", "resume(Lorg/kwis/msp/media/Clip;)Z",
		}, nil},
	}
	count := uint32(0)
	for _, api := range apis {
		count += uint32(len(api.statics) + 2)
	}
	fields, staticFields, virtuals, methods := data.reserve(8), data.reserve(8), data.reserve(8), data.reserve(8)
	statics, classes := data.reserve(count*8), data.reserve(4+uint32(len(apis))*24)
	fieldsOut, staticFieldsOut, virtualOut, methodsOut := data.reserve(2), data.reserve(2), data.reserve(2), data.reserve(2)
	staticOut := data.reserve(count * 4)
	link := lgtJavaLinkage{statics: map[string]uint32{}, virtuals: map[string]uint32{}, classes: map[string]uint32{},
		load: [11]uint32{classes, fields, staticFields, virtuals, methods, statics,
			fieldsOut, staticFieldsOut, virtualOut, methodsOut, staticOut}}
	data.words(classes, uint32(len(apis)))
	start := uint32(0)
	for index, api := range apis {
		data.words(classes+4+uint32(index)*24, names(api.class), 0, 0, 0, 0, uint32(len(api.statics)+2)<<16|start)
		link.classes[lgtJavaMemberKey(api.class, "")] = staticOut + (start+1)*4
		for member, text := range api.statics {
			name, descriptor, _ := strings.Cut(text, "(")
			slot := start + 2 + uint32(member)
			data.words(statics+slot*8, names(name), names("("+descriptor))
			link.statics[lgtJavaMemberKey(api.class, text)] = staticOut + slot*4
		}
		start += uint32(len(api.statics) + 2)
	}
	return link
}
