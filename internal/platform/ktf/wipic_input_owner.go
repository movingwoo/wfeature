package ktf

import (
	"encoding/binary"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A bounded Thumb caller stores its controller state before initializing the
// editor. An ignored confirmation leaves that state unchanged; accepting the
// name leaves it. WIPI-C itself exposes no widget identity or visibility API.
// Resolve this caller's live state through its code and static-base table,
// never through an archive name, fixed address or guessed screen pixels.
type cInputOwner struct {
	address, value uint32
}

func (owner cInputOwner) current(runtime *initializationRuntime) bool {
	if owner.address == 0 {
		return false
	}
	value, err := runtime.readWord(owner.address)
	return err == nil && value == owner.value
}

func (runtime *initializationRuntime) rememberCInputOwner(thread *armcore.Thread) {
	if owner, ok := runtime.cInputOwnerFromCaller(thread); ok {
		runtime.cInput.owner = owner
		runtime.countDiagnostic("C input controller lifetime recognized")
	} else if runtime.cInput.card != runtime.cInputCard() || !runtime.cInput.owner.current(runtime) {
		runtime.cInput.owner = cInputOwner{}
	}
}

func (runtime *initializationRuntime) cInputOwnerFromCaller(thread *armcore.Thread) (cInputOwner, bool) {
	none := cInputOwner{}
	lr, _ := thread.Register(armcore.RegisterLR)
	sp, _ := thread.Register(armcore.RegisterSP)
	staticBase, _ := thread.Register(10)
	if lr&1 == 0 || lr < 53 || sp&3 != 0 {
		return none, false
	}
	// The mode wrapper saves LR and the static base, then calls table slot 1.
	wrapper := (lr &^ 1) - 52
	code, ok := runtime.cInputCode(wrapper, 31)
	if !ok || !cInputInstructions(code, []uint16{
		0xb500, 0x469c, 0x4653, 0xb408, 0x4663, 0, 0x469a, 0, 0x44fa,
		0x4453, 0x681b, 0x6018, 0x2804, 0, 0, 0x4453, 0x681b, 0x6819,
		0, 0x4453, 0x681a, 0x0083, 0x5898, 0x684b, 0, 0, 0, 0,
		0xbc08, 0x469a, 0xbd00,
	}) {
		return none, false
	}
	stack, err := runtime.readAOTWords(sp, 3, "input controller frames")
	if err != nil || stack[0] != staticBase || stack[1]&1 == 0 || stack[1] < 23 || stack[2]&1 == 0 || stack[2] < 19 {
		return none, false
	}
	// The initializer saves only LR. Check its call to the observed wrapper
	// before using the next saved return address as a controller call site.
	initializer := (stack[1] &^ 1) - 22
	initCode, ok := runtime.cInputCode(initializer, 14)
	if !ok || !cInputInstructions(initCode, []uint16{
		0xb500, 0x2080, 0x0040, 0, 0, 0x2000, 0, 0, 0x2000, 0, 0, 0, 0, 0xbd00,
	}) {
		return none, false
	}
	if target, ok := cInputThumbCall(initializer+18, initCode[9:11]); !ok || target != wrapper {
		return none, false
	}
	caller := (stack[2] &^ 1) - 18
	entry, ok := runtime.cInputCode(caller, 9)
	if !ok || entry[0]&0xff00 != 0x4b00 || entry[1] != 0x4453 || entry[2] != 0x681a ||
		entry[3]&0xff00 != 0x2300 || entry[4] != 0x6013 {
		return none, false
	}
	if _, ok := cInputThumbCall(caller+10, entry[5:7]); !ok {
		return none, false
	}
	if target, ok := cInputThumbCall(caller+14, entry[7:9]); !ok || target != initializer {
		return none, false
	}
	literal := ((uint64(caller) + 4) &^ 3) + uint64(entry[0]&0xff)*4
	if literal > 0xfffffffc {
		return none, false
	}
	offset, err := runtime.readWord(uint32(literal))
	slot := int64(staticBase) + int64(int32(offset))
	if err != nil || slot <= 0 || slot > 0xfffffffc || slot&3 != 0 {
		return none, false
	}
	address, err := runtime.readWord(uint32(slot))
	if err != nil || address == 0 || address&3 != 0 {
		return none, false
	}
	owner := cInputOwner{address: address, value: uint32(entry[3] & 0xff)}
	return owner, owner.current(runtime)
}

func (runtime *initializationRuntime) cInputCode(address uint32, count int) ([]uint16, bool) {
	data := make([]byte, count*2)
	if address&1 != 0 || uint64(address)+uint64(len(data)) > 1<<32 {
		return nil, false
	}
	if err := runtime.client.core.Memory().Read(address, data); err != nil {
		return nil, false
	}
	words := make([]uint16, count)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return words, true
}

func cInputInstructions(code, pattern []uint16) bool {
	for i, word := range pattern {
		if word != 0 && code[i] != word {
			return false
		}
	}
	return true
}

func cInputThumbCall(pc uint32, words []uint16) (uint32, bool) {
	if words[0]&0xf800 != 0xf000 || words[1]&0xf800 != 0xf800 {
		return 0, false
	}
	delta := int32(words[0]&0x7ff)<<12 | int32(words[1]&0x7ff)<<1
	delta = delta << 9 >> 9
	target := int64(pc) + 4 + int64(delta)
	return uint32(target), target >= 0 && target <= 0xfffffffe
}
