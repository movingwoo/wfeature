package lgt

// Slots that titles here reach, that this platform accepts, and whose contract
// is not known. They are separated from the implemented slots because the two
// fail differently: an unimplemented slot stops the game and says so, while one
// of these lets the game believe it was served. That is a real risk, so the
// whole set is listed in one place with the reason each one is on it, the same
// way the other WIPI platform records its fixed-value stubs.
//
// A slot earns a place here only by being reached by a real module. The
// numbering is the WIPI C flat index, so the block a slot sits in is the only
// clue to what it belongs to; neither the specification nor the original
// runtime's symbols name any of them.
//
// **The set is currently empty**, and the three that emptied it are worth
// keeping in view because each left by a different route. The text-entry widget
// slots left when the specification's input-method calls matched them argument
// for argument (wipic_im.go). 0x4c2 left when its (clip, 0-100) shape turned
// out to be MC_mdaSetWaterMark, a function the specification carries in the HAL
// appendix rather than in the ordered list the block otherwise follows
// (wipic_media.go). 0x4ce left when the caller was disassembled rather than
// traced: the trace showed three arguments and no use of the result, and the
// code showed one argument and the result stored as a volume — the other two
// registers were what the preceding call had left in them (wipic_media.go).
//
// The lesson that cost the most is that last one. **A trace reports registers,
// not arguments.** Where a slot's shape is what has to settle its contract, the
// call site is the evidence and the trace is the pointer to it.
var acceptedUnknownSlots = map[uint32]string{}

// unknownSlotAccepted reports whether reaching a slot is recorded above.
func unknownSlotAccepted(slot uint32) bool {
	_, ok := acceptedUnknownSlots[slot]
	return ok
}
