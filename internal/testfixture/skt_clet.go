package testfixture

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
)

// The SKT native fixture is a Clet authored here in Thumb instructions, laid
// out as a native package's executable. It does what a title of that kind does
// at its smallest: reaches the platform through the root table the loader
// hands it, arms a frame timer that arms itself again every frame, draws the
// frame count into a corner of the screen and flushes it, and keeps a text
// field of its own that composes through the input method — handing each key
// it is given to MC_imHandleInput and appending the completed text the input
// method answers with. Two keys write files: one rewrites a save whole, the
// other opens a log and keeps it open, and a third appends the count to it.
//
// The flags word selects how the field behaves, so one executable serves every
// test: whether the field opens at start, whether it takes its keys on the
// frame timer rather than in the event entry, and whether it hands the input
// method the carrier key at all.
const (
	// SKTCletImageBase and sktCletDataLink are the platform's own numbers,
	// spelt out rather than imported because the platform package's tests use
	// this fixture too: the code is placed at the image base and the data at
	// the link distance from it, and the literals below are those addresses.
	SKTCletImageBase uint32 = 0x00100000
	sktCletDataLink  uint32 = 0x01001000
	sktCletRootMark  uint32 = 0xcafecafe

	// SKTCletID is the fixture's program number, which is also its save owner.
	SKTCletID = "0123456789"

	sktCletData = SKTCletImageBase + sktCletDataLink

	sktCletTimer = sktCletData + 0x04
	// SKTCletCounter counts the timer's frames, and is the colour of the
	// corner the frame draws.
	SKTCletCounter = sktCletData + 0x08
	// SKTCletLastKey is the last key the event entry was handed.
	SKTCletLastKey  = sktCletData + 0x0c
	sktCletSaveName = sktCletData + 0x10
	sktCletComplete = sktCletData + 0x20
	sktCletCompSize = sktCletData + 0x30
	sktCletCompose  = sktCletData + 0x34
	sktCletPartSize = sktCletData + 0x3c
	// SKTCletTextLength and SKTCletText are the field's own value: the bytes
	// the input method completed, in the order it completed them.
	SKTCletTextLength = sktCletData + 0x40
	SKTCletText       = sktCletData + 0x44
	SKTCletTextLimit  = 64
	sktCletSaveHandle = sktCletData + 0x84
	sktCletQueuedKey  = sktCletData + 0x88
	sktCletLogHandle  = sktCletData + 0x8c
	sktCletLogName    = sktCletData + 0x90
	sktCletContext    = sktCletData + 0xa0
	sktCletScreen     = sktCletData + 0xd4
	sktCletFlags      = sktCletData + 0xd8
	sktCletDataSize   = 0x100

	// SKTCletSaveKey and SKTCletLogKey are the save keys the two files land
	// under in a session's store.
	SKTCletSaveKey = "db/save.dat"
	SKTCletLogKey  = "db/log.dat"
)

// SKTCletOptions selects the field's behaviour.
type SKTCletOptions struct {
	// FieldAtStart sets the input method's mode in the start entry, which is
	// a field open from the first frame.
	FieldAtStart bool
	// NumericField sets the digit mode rather than the Hangul one, so the
	// input method completes a digit key the player presses.
	NumericField bool
	// TimerKeys queues each key in the event entry and hands it to the input
	// method on the next frame, the way a field that runs on its frame timer
	// takes keys.
	TimerKeys bool
	// IgnoresCarrier never hands the carrier key to the input method: a
	// screen that takes a 0 as the key it is.
	IgnoresCarrier bool
}

const (
	sktCletFlagField = 1 << iota
	sktCletFlagTimerKeys
	sktCletFlagIgnoresCarrier
	sktCletFlagNumeric
)

// SKTCletProgram is the fixture's executable as its regions: code linked at
// the image base, data at the link distance, and entries as odd offsets into
// the code — start, pause, resume, destroy, paint and event.
type SKTCletProgram struct {
	Code, Data []byte
	BSS        uint32
	Entries    [6]uint32
}

// thumb lays out Thumb halfwords, each routine followed by its literal pool.
type thumb struct {
	code     []uint16
	labels   map[string]int
	branches []thumbBranch
	loads    []thumbLoad
}

type thumbBranch struct {
	at    int
	label string
	kind  int
}

// thumbLoad is one `ldr rd, [pc, #offset]` and what its literal holds: a
// value, or the Thumb address of a label.
type thumbLoad struct {
	at    int
	value uint32
	label string
}

const (
	thumbBranchAlways = iota
	thumbBranchEqual
	thumbBranchNotEqual
	thumbBranchLink
)

func (a *thumb) emit(halfwords ...uint16) { a.code = append(a.code, halfwords...) }

func (a *thumb) label(name string) { a.labels[name] = len(a.code) }

// literal emits `ldr rd, =value`.
func (a *thumb) literal(rd int, value uint32) {
	a.loads = append(a.loads, thumbLoad{at: len(a.code), value: value})
	a.emit(0x4800 | uint16(rd)<<8)
}

// address emits `ldr rd, =label`, the label's Thumb code address.
func (a *thumb) address(rd int, label string) {
	a.loads = append(a.loads, thumbLoad{at: len(a.code), label: label})
	a.emit(0x4800 | uint16(rd)<<8)
}

func (a *thumb) branch(kind int, label string) {
	a.branches = append(a.branches, thumbBranch{at: len(a.code), label: label, kind: kind})
	a.emit(0)
	if kind == thumbBranchLink {
		a.emit(0)
	}
}

// assembleThumb lays every routine out with its literal pool after it,
// resolves the pools and the branches, and answers the code and its labels.
func assembleThumb(routines []func(*thumb)) ([]byte, map[string]int, error) {
	a := &thumb{labels: make(map[string]int)}
	type pool struct {
		start int
		loads []thumbLoad
	}
	var pools []pool
	for _, routine := range routines {
		a.loads = nil
		routine(a)
		// A pool is word-aligned, and every routine starts on a word.
		if len(a.code)%2 != 0 {
			a.emit(0x46c0) // nop
		}
		pools = append(pools, pool{start: len(a.code), loads: a.loads})
		for range a.loads {
			a.emit(0, 0)
		}
	}
	for _, branch := range a.branches {
		target, ok := a.labels[branch.label]
		if !ok {
			return nil, nil, fmt.Errorf("thumb label %q is not defined", branch.label)
		}
		// Thumb branches count halfwords from the instruction plus two.
		offset := target - (branch.at + 2)
		switch branch.kind {
		case thumbBranchAlways:
			if offset < -1024 || offset > 1023 {
				return nil, nil, fmt.Errorf("thumb branch to %q is out of range", branch.label)
			}
			a.code[branch.at] = 0xe000 | uint16(offset)&0x7ff
		case thumbBranchEqual, thumbBranchNotEqual:
			if offset < -128 || offset > 127 {
				return nil, nil, fmt.Errorf("thumb conditional branch to %q is out of range", branch.label)
			}
			condition := uint16(0)
			if branch.kind == thumbBranchNotEqual {
				condition = 1
			}
			a.code[branch.at] = 0xd000 | condition<<8 | uint16(offset)&0xff
		case thumbBranchLink:
			delta := int32(offset) * 2
			a.code[branch.at] = 0xf000 | uint16(delta>>12)&0x7ff
			a.code[branch.at+1] = 0xf800 | uint16(delta>>1)&0x7ff
		}
	}
	for _, block := range pools {
		for index, load := range block.loads {
			slot := block.start + index*2
			value := load.value
			if load.label != "" {
				target, ok := a.labels[load.label]
				if !ok {
					return nil, nil, fmt.Errorf("thumb label %q is not defined", load.label)
				}
				value = (SKTCletImageBase + uint32(target)*2) | 1
			}
			a.code[slot], a.code[slot+1] = uint16(value), uint16(value>>16)
			pc := (load.at*2 + 4) &^ 3
			distance := slot*2 - pc
			if distance < 0 || distance > 1020 || distance%4 != 0 {
				return nil, nil, fmt.Errorf("thumb literal at %#x is out of range", load.at*2)
			}
			a.code[load.at] |= uint16(distance / 4)
		}
	}
	code := make([]byte, len(a.code)*2)
	for index, halfword := range a.code {
		binary.LittleEndian.PutUint16(code[index*2:], halfword)
	}
	return code, a.labels, nil
}

// Thumb encodings the fixture uses.
func thumbMovs(rd, value int) uint16 { return 0x2000 | uint16(rd)<<8 | uint16(value)&0xff }
func thumbCmp(rn, value int) uint16  { return 0x2800 | uint16(rn)<<8 | uint16(value)&0xff }
func thumbAdds(rd, value int) uint16 { return 0x3000 | uint16(rd)<<8 | uint16(value)&0xff }
func thumbSubs(rd, value int) uint16 { return 0x3800 | uint16(rd)<<8 | uint16(value)&0xff }
func thumbMov(rd, rn int) uint16     { return 0x1c00 | uint16(rn)<<3 | uint16(rd) }
func thumbTst(rn, rm int) uint16     { return 0x4200 | uint16(rm)<<3 | uint16(rn) }
func thumbBlx(rm int) uint16         { return 0x4780 | uint16(rm)<<3 }
func thumbLdr(rd, rn, offset int) uint16 {
	return 0x6800 | uint16(offset/4)<<6 | uint16(rn)<<3 | uint16(rd)
}
func thumbStr(rd, rn, offset int) uint16 {
	return 0x6000 | uint16(offset/4)<<6 | uint16(rn)<<3 | uint16(rd)
}
func thumbLdrReg(rd, rn, rm int) uint16 { return 0x5800 | uint16(rm)<<6 | uint16(rn)<<3 | uint16(rd) }
func thumbLdrb(rd, rn, offset int) uint16 {
	return 0x7800 | uint16(offset)<<6 | uint16(rn)<<3 | uint16(rd)
}
func thumbStrbReg(rd, rn, rm int) uint16 { return 0x5400 | uint16(rm)<<6 | uint16(rn)<<3 | uint16(rd) }
func thumbStrSP(rd, offset int) uint16   { return 0x9000 | uint16(rd)<<8 | uint16(offset/4) }
func thumbSubSP(size int) uint16         { return 0xb080 | uint16(size/4) }
func thumbAddSP(size int) uint16         { return 0xb000 | uint16(size/4) }

const (
	thumbPushR4LR     = 0xb510 // push {r4, lr}
	thumbPopR4PC      = 0xbd10 // pop  {r4, pc}
	thumbPushR4R5LR   = 0xb530 // push {r4, r5, lr}
	thumbPopR4R5PC    = 0xbd30 // pop  {r4, r5, pc}
	thumbPushR4R6LR   = 0xb570 // push {r4, r5, r6, lr}
	thumbPopR4R6PC    = 0xbd70 // pop  {r4, r5, r6, pc}
	sktCletKernel     = 0      // root slot of the kernel table
	sktCletGraphics   = 1      // root slot of the graphics table
	sktCletFilesystem = 3      // root slot of the filesystem table
)

// platform loads function `function` of root slot `slot` into rd through the
// root table, the way every call in an SKT executable reaches the platform.
func (a *thumb) platform(rd, slot, function int) {
	a.literal(rd, sktCletData)
	a.emit(thumbLdr(rd, rd, 0), thumbLdr(rd, rd, slot*4))
	if function*4 <= 124 {
		a.emit(thumbLdr(rd, rd, function*4))
		return
	}
	scratch := 5
	if rd == 5 {
		scratch = 4
	}
	a.emit(thumbMovs(scratch, function*4), thumbLdrReg(rd, rd, scratch))
}

// SKTCletParts lays the fixture's executable out.
func SKTCletParts(options SKTCletOptions) (SKTCletProgram, error) {
	const (
		defTimer, setTimer                  = 25, 26
		initContext, setContext             = 5, 6
		screenBuffer, fillRect, flushLCD    = 2, 11, 21
		handleInput, setMode                = 37, 38
		fileOpen, fileWrite, fileClose      = 0, 2, 3
		eventPressed                        = 2
		keySave, keyOpenLog, keyAppendLog   = '5', '6', '7'
		modeHangul, modeDigits, carrierKeyV = 2, 3, '0'
	)
	routines := []func(*thumb){
		// start: arm the frame timer, prepare the context and the screen,
		// and open the field when the flags say so.
		func(a *thumb) {
			a.label("start")
			a.emit(thumbPushR4LR)
			a.literal(0, sktCletTimer)
			a.address(1, "frame")
			a.platform(3, sktCletKernel, defTimer)
			a.emit(thumbBlx(3))
			a.branch(thumbBranchLink, "arm")
			a.literal(0, sktCletContext)
			a.platform(3, sktCletGraphics, initContext)
			a.emit(thumbBlx(3))
			a.platform(3, sktCletGraphics, screenBuffer)
			a.emit(thumbBlx(3))
			a.literal(1, sktCletScreen)
			a.emit(thumbStr(0, 1, 0))
			a.literal(0, sktCletFlags)
			a.emit(thumbLdr(0, 0, 0), thumbMovs(1, sktCletFlagField), thumbTst(0, 1))
			a.branch(thumbBranchEqual, "start.done")
			// movs sets the flags too, so the mode is chosen before the test.
			a.literal(2, sktCletFlags)
			a.emit(thumbLdr(2, 2, 0), thumbMovs(0, modeHangul), thumbMovs(1, sktCletFlagNumeric), thumbTst(2, 1))
			a.branch(thumbBranchEqual, "start.mode")
			a.emit(thumbMovs(0, modeDigits))
			a.label("start.mode")
			a.platform(3, sktCletGraphics, setMode)
			a.emit(thumbBlx(3))
			a.label("start.done")
			a.emit(thumbMovs(0, 0), thumbPopR4PC)
		},
		// arm: MC_knlSetTimer(&timer, 20, 0, 0).
		func(a *thumb) {
			a.label("arm")
			a.emit(thumbPushR4LR)
			a.literal(0, sktCletTimer)
			a.emit(thumbMovs(1, 20), thumbMovs(2, 0), thumbMovs(3, 0))
			a.platform(4, sktCletKernel, setTimer)
			a.emit(thumbBlx(4), thumbPopR4PC)
		},
		// frame: count, arm again, fill the corner with the count, flush it,
		// and hand a queued key to the field.
		func(a *thumb) {
			a.label("frame")
			a.emit(thumbPushR4R5LR)
			a.literal(4, SKTCletCounter)
			a.emit(thumbLdr(0, 4, 0), thumbAdds(0, 1), thumbStr(0, 4, 0))
			a.branch(thumbBranchLink, "arm")
			a.literal(0, sktCletContext)
			a.emit(thumbMovs(1, 1))
			a.literal(2, SKTCletCounter)
			a.emit(thumbLdr(2, 2, 0))
			a.platform(3, sktCletGraphics, setContext)
			a.emit(thumbBlx(3))
			a.emit(thumbSubSP(8), thumbMovs(0, 16), thumbStrSP(0, 0))
			a.literal(0, sktCletContext)
			a.emit(thumbStrSP(0, 4))
			a.literal(0, sktCletScreen)
			a.emit(thumbLdr(0, 0, 0), thumbMovs(1, 0), thumbMovs(2, 0), thumbMovs(3, 16))
			a.platform(4, sktCletGraphics, fillRect)
			a.emit(thumbBlx(4))
			a.emit(thumbMovs(0, 16), thumbStrSP(0, 0), thumbStrSP(0, 4), thumbMovs(0, 0))
			a.literal(1, sktCletScreen)
			a.emit(thumbLdr(1, 1, 0), thumbMovs(2, 0), thumbMovs(3, 0))
			a.platform(4, sktCletGraphics, flushLCD)
			a.emit(thumbBlx(4), thumbAddSP(8))
			a.literal(4, sktCletQueuedKey)
			a.emit(thumbLdr(0, 4, 0), thumbCmp(0, 0))
			a.branch(thumbBranchEqual, "frame.done")
			a.emit(thumbMovs(5, 0), thumbStr(5, 4, 0))
			a.branch(thumbBranchLink, "field")
			a.label("frame.done")
			a.emit(thumbPopR4R5PC)
		},
		// field: hand the key in r0 to the input method and append what it
		// completes, up to the field's limit.
		func(a *thumb) {
			a.label("field")
			a.emit(thumbPushR4R6LR)
			a.literal(4, sktCletFlags)
			a.emit(thumbLdr(4, 4, 0), thumbMovs(5, sktCletFlagIgnoresCarrier), thumbTst(4, 5))
			a.branch(thumbBranchEqual, "field.call")
			a.emit(thumbCmp(0, carrierKeyV))
			a.branch(thumbBranchEqual, "field.done")
			a.label("field.call")
			a.literal(4, sktCletCompSize)
			a.emit(thumbMovs(5, 16), thumbStr(5, 4, 0))
			a.literal(4, sktCletPartSize)
			a.emit(thumbMovs(5, 8), thumbStr(5, 4, 0))
			a.emit(thumbSubSP(8))
			a.literal(4, sktCletCompose)
			a.emit(thumbStrSP(4, 0))
			a.literal(4, sktCletPartSize)
			a.emit(thumbStrSP(4, 4))
			a.emit(thumbMovs(1, eventPressed))
			a.literal(2, sktCletComplete)
			a.literal(3, sktCletCompSize)
			a.platform(4, sktCletGraphics, handleInput)
			a.emit(thumbBlx(4), thumbAddSP(8), thumbCmp(0, 0))
			a.branch(thumbBranchEqual, "field.done")
			a.literal(4, sktCletCompSize)
			a.emit(thumbLdr(5, 4, 0), thumbCmp(5, 0))
			a.branch(thumbBranchEqual, "field.done")
			a.literal(4, SKTCletTextLength)
			a.emit(thumbLdr(6, 4, 0))
			a.literal(2, sktCletComplete)
			a.literal(3, SKTCletText)
			a.label("field.copy")
			a.emit(thumbCmp(6, SKTCletTextLimit))
			a.branch(thumbBranchEqual, "field.store")
			a.emit(thumbLdrb(0, 2, 0), thumbStrbReg(0, 3, 6), thumbAdds(2, 1), thumbAdds(6, 1), thumbSubs(5, 1))
			a.branch(thumbBranchNotEqual, "field.copy")
			a.label("field.store")
			a.emit(thumbStr(6, 4, 0))
			a.label("field.done")
			a.emit(thumbPopR4R6PC)
		},
		// event (type, key, unused): a press is recorded, and goes to a file
		// routine or to the field — now, or on the next frame.
		func(a *thumb) {
			a.label("event")
			a.emit(thumbPushR4LR, thumbCmp(0, eventPressed))
			a.branch(thumbBranchNotEqual, "event.done")
			a.literal(4, SKTCletLastKey)
			a.emit(thumbStr(1, 4, 0), thumbCmp(1, keySave))
			a.branch(thumbBranchEqual, "event.save")
			a.emit(thumbCmp(1, keyOpenLog))
			a.branch(thumbBranchEqual, "event.open")
			a.emit(thumbCmp(1, keyAppendLog))
			a.branch(thumbBranchEqual, "event.append")
			a.literal(4, sktCletFlags)
			a.emit(thumbLdr(4, 4, 0), thumbMovs(0, sktCletFlagTimerKeys), thumbTst(4, 0))
			a.branch(thumbBranchEqual, "event.now")
			a.literal(4, sktCletQueuedKey)
			a.emit(thumbStr(1, 4, 0))
			a.branch(thumbBranchAlways, "event.done")
			a.label("event.now")
			a.emit(thumbMov(0, 1))
			a.branch(thumbBranchLink, "field")
			a.branch(thumbBranchAlways, "event.done")
			a.label("event.save")
			a.branch(thumbBranchLink, "save")
			a.branch(thumbBranchAlways, "event.done")
			a.label("event.open")
			a.branch(thumbBranchLink, "open")
			a.branch(thumbBranchAlways, "event.done")
			a.label("event.append")
			a.branch(thumbBranchLink, "append")
			a.label("event.done")
			a.emit(thumbMovs(0, 1), thumbPopR4PC)
		},
		// save: rewrite save.dat with the count, and close it.
		func(a *thumb) {
			a.label("save")
			a.emit(thumbPushR4LR)
			a.literal(0, sktCletSaveName)
			a.emit(thumbMovs(1, 4))
			a.platform(3, sktCletFilesystem, fileOpen)
			a.emit(thumbBlx(3))
			a.literal(4, sktCletSaveHandle)
			a.emit(thumbStr(0, 4, 0))
			a.literal(1, SKTCletCounter)
			a.emit(thumbMovs(2, 4))
			a.platform(3, sktCletFilesystem, fileWrite)
			a.emit(thumbBlx(3))
			a.literal(4, sktCletSaveHandle)
			a.emit(thumbLdr(0, 4, 0))
			a.platform(3, sktCletFilesystem, fileClose)
			a.emit(thumbBlx(3), thumbPopR4PC)
		},
		// open: create log.dat and keep it open.
		func(a *thumb) {
			a.label("open")
			a.emit(thumbPushR4LR)
			a.literal(0, sktCletLogName)
			a.emit(thumbMovs(1, 4))
			a.platform(3, sktCletFilesystem, fileOpen)
			a.emit(thumbBlx(3))
			a.literal(4, sktCletLogHandle)
			a.emit(thumbStr(0, 4, 0), thumbPopR4PC)
		},
		// append: write the count through the open log's cursor.
		func(a *thumb) {
			a.label("append")
			a.emit(thumbPushR4LR)
			a.literal(4, sktCletLogHandle)
			a.emit(thumbLdr(0, 4, 0))
			a.literal(1, SKTCletCounter)
			a.emit(thumbMovs(2, 4))
			a.platform(3, sktCletFilesystem, fileWrite)
			a.emit(thumbBlx(3), thumbPopR4PC)
		},
		// The other four entries have nothing to do.
		func(a *thumb) {
			a.label("idle")
			a.emit(thumbMovs(0, 0), 0x4770) // bx lr
		},
	}
	code, labels, err := assembleThumb(routines)
	if err != nil {
		return SKTCletProgram{}, err
	}
	data := make([]byte, sktCletDataSize)
	binary.LittleEndian.PutUint32(data[0:], sktCletRootMark)
	copy(data[sktCletSaveName-sktCletData:], "save.dat\x00")
	copy(data[sktCletLogName-sktCletData:], "log.dat\x00")
	flags := uint32(0)
	if options.FieldAtStart {
		flags |= sktCletFlagField
	}
	if options.TimerKeys {
		flags |= sktCletFlagTimerKeys
	}
	if options.IgnoresCarrier {
		flags |= sktCletFlagIgnoresCarrier
	}
	if options.NumericField {
		flags |= sktCletFlagNumeric
	}
	binary.LittleEndian.PutUint32(data[sktCletFlags-sktCletData:], flags)
	entry := func(name string) uint32 { return uint32(labels[name])*2 | 1 }
	return SKTCletProgram{Code: code, Data: data, BSS: 0x100,
		Entries: [6]uint32{entry("start"), entry("idle"), entry("idle"), entry("idle"), entry("idle"), entry("event")}}, nil
}

// SKTCletArchive is the fixture as a native package: the program's directory,
// with its executable, descriptor and icons, and the files given.
func SKTCletArchive(options SKTCletOptions, files map[string][]byte) ([]byte, error) {
	program, err := SKTCletParts(options)
	if err != nil {
		return nil, err
	}
	// The relocation table lists nothing: every address in the code and data
	// is already where the loader places it.
	relocation := make([]byte, 8)
	header := make([]byte, 0x70)
	binary.LittleEndian.PutUint32(header[0x00:], 0x20)
	binary.LittleEndian.PutUint32(header[0x28:], 0x50)
	binary.LittleEndian.PutUint32(header[0x3c:], uint32(len(program.Code)))
	binary.LittleEndian.PutUint32(header[0x40:], uint32(len(program.Data)))
	binary.LittleEndian.PutUint32(header[0x44:], program.BSS)
	binary.LittleEndian.PutUint32(header[0x48:], uint32(len(relocation)))
	for index, entry := range program.Entries {
		binary.LittleEndian.PutUint32(header[0x58+4*index:], entry)
	}
	image := append(append(append(header, program.Code...), program.Data...), relocation...)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	add := func(name string, data []byte) error {
		w, err := writer.Create(SKTCletID + "/" + name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := add(SKTCletID+".daf", image); err != nil {
		return nil, err
	}
	if err := add(SKTCletID+".mod", []byte("descriptor")); err != nil {
		return nil, err
	}
	if err := add(SKTCletID+".wmr", []byte("icons")); err != nil {
		return nil, err
	}
	for name, data := range files {
		if err := add(name, data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
