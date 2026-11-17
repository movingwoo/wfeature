package wipicrt

import "encoding/binary"

// A pixel operation's answers are cached by the pair of pixels it was asked
// about (see PixelOpCache), and how long an answer can be kept depends on what
// the operation reads. Most local operations read nothing but their three
// arguments and constants of their own: a colour key, an alpha blend, a keyed
// blend. Their answers hold for as long as the operation and its parameter
// do. A few read the title's own memory as well — one a fade level it steps
// once a frame, another a colour it keeps in a global — and an answer of
// theirs holds only for the frame it was asked in.
//
// Which kind an operation is shows in its code, so it is read once: every
// instruction a call can reach, following both ways of each branch and into
// what it calls with BL. An operation is pure when none of them touches memory
// other than its own stack and its literal pool, calls the platform, or jumps
// somewhere its code does not say. Anything else — another load or store, a
// call through a register, an instruction this reading does not know, more
// code than it reads — makes it impure, which only costs the answers being
// asked for again each frame.

const (
	// maxPixelOpInstructions bounds the instructions one reading visits,
	// callees included. The local operations that read only their arguments
	// take 6 to 93, the division helper one of them calls included.
	maxPixelOpInstructions = 256
	// maxPixelOpCallDepth bounds how deep the reading follows BL.
	maxPixelOpCallDepth = 2
	// maxPixelOpVerdicts bounds how many operations' readings are kept. A
	// title sets a handful; past the bound, an operation is read again at
	// every frame it is used in.
	maxPixelOpVerdicts = 64
)

// pixelOpPure reports whether a pixel operation's answers depend on its
// arguments alone, reading its code the first time it is asked.
func (runtime *Runtime) pixelOpPure(function uint32) bool {
	if pure, known := runtime.pixelOpPurity[function]; known {
		return pure
	}
	reading := pixelOpReading{memory: runtime.Client.core.Memory().Read}
	pure := function&1 == 1 && reading.function(function&^1, 0)
	if runtime.pixelOpPurity == nil {
		runtime.pixelOpPurity = make(map[uint32]bool)
	}
	if len(runtime.pixelOpPurity) < maxPixelOpVerdicts {
		runtime.pixelOpPurity[function] = pure
	}
	return pure
}

// pixelOpReading walks the Thumb code of one operation.
type pixelOpReading struct {
	memory  func(address uint32, data []byte) error
	visited int
	// called holds the functions this reading has found pure, so a helper
	// called once for each colour channel is read once.
	called map[uint32]bool
}

// function reports whether every path from a Thumb entry point returns
// without touching memory beyond the stack and literal pool.
func (reading *pixelOpReading) function(entry uint32, depth int) bool {
	seen := make(map[uint32]bool)
	pending := []uint32{entry}
	for len(pending) > 0 {
		address := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for !seen[address] {
			seen[address] = true
			reading.visited++
			if reading.visited > maxPixelOpInstructions {
				return false
			}
			var code [4]byte
			if reading.memory(address, code[:2]) != nil {
				return false
			}
			halfword := binary.LittleEndian.Uint16(code[:2])
			next, branch, ends, pure := thumbStep(address, halfword)
			if halfword>>11 == 0x1e {
				// The first half of a BL or BLX pair. A BL into code
				// that is itself pure is pure; BLX switches to ARM code,
				// which this does not read.
				if reading.memory(address, code[:]) != nil {
					return false
				}
				second := binary.LittleEndian.Uint16(code[2:])
				if second>>11 != 0x1f || depth >= maxPixelOpCallDepth {
					return false
				}
				offset := int32(uint32(halfword&0x7ff)<<21) >> 9
				offset |= int32(second&0x7ff) << 1
				callee := uint32(int64(address) + 4 + int64(offset))
				if !reading.called[callee] {
					if !reading.function(callee, depth+1) {
						return false
					}
					if reading.called == nil {
						reading.called = make(map[uint32]bool)
					}
					reading.called[callee] = true
				}
				address += 4
				continue
			}
			if !pure {
				return false
			}
			if branch != 0 {
				pending = append(pending, branch)
			}
			if ends {
				break
			}
			address = next
		}
	}
	return true
}

// thumbStep classifies one 16-bit Thumb instruction: where execution goes on,
// a branch target to follow as well, whether this path ends here, and whether
// the instruction is one a pure operation may contain.
func thumbStep(address uint32, halfword uint16) (next, branch uint32, ends, pure bool) {
	next = address + 2
	switch {
	case halfword < 0x4400:
		// Shifts, add and subtract, move, compare and immediate arithmetic,
		// and the register ALU operations.
		return next, 0, false, true
	case halfword == 0x46f7:
		return next, 0, true, true // MOV PC, LR: a return
	case halfword < 0x4700:
		// ADD, CMP and MOV on high registers; writing the PC is a jump
		// this reading cannot follow.
		op := halfword >> 8 & 3
		destination := halfword&7 | halfword>>4&8
		return next, 0, false, op == 1 || destination != 15
	case halfword < 0x4780:
		// BX: a return through LR ends the path; any other is a jump.
		return next, 0, true, halfword>>3&0xf == 14
	case halfword < 0x4800:
		return next, 0, false, false // BLX through a register
	case halfword < 0x5000:
		return next, 0, false, true // LDR from the literal pool
	case halfword < 0x9000:
		return next, 0, false, false // loads and stores through registers
	case halfword < 0xb000:
		// SP-relative loads and stores, and an address formed from the PC
		// or SP: the operation's own frame and constants.
		return next, 0, false, true
	case halfword < 0xb100:
		return next, 0, false, true // ADD or SUB SP
	case halfword&0xfe00 == 0xb400:
		return next, 0, false, true // PUSH
	case halfword&0xfe00 == 0xbc00:
		// POP; one that loads the PC returns.
		return next, 0, halfword&0x100 != 0, true
	case halfword < 0xc000:
		return next, 0, false, false
	case halfword < 0xd000:
		return next, 0, false, false // LDM and STM
	case halfword < 0xde00:
		// A conditional branch goes on both ways.
		offset := int32(int8(halfword)) << 1
		return next, uint32(int64(address) + 4 + int64(offset)), false, true
	case halfword < 0xe000:
		return next, 0, false, false // UDF and SVC
	case halfword < 0xe800:
		// B: the path goes on at the target alone.
		offset := int32(uint32(halfword&0x7ff)<<21) >> 20
		return next, uint32(int64(address) + 4 + int64(offset)), true, true
	}
	// The other 32-bit encodings, which this reading does not know.
	return next, 0, false, false
}
