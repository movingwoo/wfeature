package wipicrt

import "fmt"

// A graphics context can carry a pixel operation, and a title that uses one is
// telling the platform how every pixel it draws must be combined with what is
// already there:
//
//	M_Int32 op(M_Int32 srcpxl, M_Int32 orgpxl, M_Int32 param1)
//
// The specification's prose for those parameters is the wrong way round —
// it describes the first as the pixel already in the framebuffer — but the
// names in the prototype are right, and the guest code settles it: both local
// operations return their **first** argument in the ordinary case, which for a
// blit that draws anything at all has to be the source pixel.
//
// This is how one title does transparency. Its images are 8-bit BMPs that
// declare none, and every sprite sheet holds its subject on a flat green
// backdrop; the operation is four instructions long and says exactly what to do
// with it:
//
//	if (srcpxl == param1) return orgpxl;   /* the backdrop: keep the screen */
//	return srcpxl;
//
// Without it the backdrop was painted, which was invisible over that title's
// grass and unmistakable over anything else — the dialogue boxes on its title
// screen each had a green square in every corner. A second title's operation
// keys white instead and answers a colour it keeps in a global, so neither the
// colour nor the rule belongs here: only the call does.
type WIPICPixelOp struct {
	Function uint32
	Param    uint32
	// Xor writes the difference between the pixel a draw wanted and the one
	// already there. It is not a guest function: it is the mode the Java
	// context carries as `Graphics.setXORMode`, travelling with the operation
	// because both are answers to the same question — what a draw must write
	// given what the surface already holds. The C context has a field of its
	// own for it, `MC_GRP_CONTEXT_XOR_MODE_IDX`, at an offset no local title
	// has settled the way the four below were settled — so that half stays
	// unread rather than guessed, and nothing sets this from the C side.
	Xor bool
}

// Active reports whether a write has to read the destination first. Both a
// guest operation and XOR need what is already there; a context with neither
// writes the source pixel.
func (op WIPICPixelOp) Active() bool { return op.Function != 0 || op.Xor }

// The pixel operation and its parameter are the last two words of the record.
// The specification's field order puts them earlier, before the font and the
// style, and the original runtime's structure follows it — but this handset
// disagrees and the title that settles it is the one that writes its context
// **directly** rather than through MC_grpSetContext: the function pointer it
// hands to the API also appears at 0x2c, with the green its operation keys on
// at 0x30, and that green appears nowhere else in the record. So the two pairs
// trade places: the font and style take the words the specification's order
// would have given the operation.
const (
	WIPICContextPixelOpOffset    = 44
	wipicContextPixelParamOffset = 48
	wipicContextFontOffset       = 32
	wipicContextStyleOffset      = 36
)

// WIPICReadContextPixelOp reads the operation a draw call has to run its pixels
// through. A field this platform did not write is not trusted to be a function:
// a title that sets a font through what this handset uses for the operation
// leaves a small integer there, and calling it would execute an address that is
// not code. Only a Thumb address inside the loaded module counts.
func (runtime *Runtime) WIPICReadContextPixelOp(contextAddress uint32) (WIPICPixelOp, error) {
	if contextAddress == 0 {
		return WIPICPixelOp{}, nil
	}
	words, err := runtime.ReadAOTWords(contextAddress+WIPICContextPixelOpOffset, 2, "graphics context pixel operation")
	if err != nil {
		return WIPICPixelOp{}, err
	}
	function := words[0]
	if function&1 == 0 {
		return WIPICPixelOp{}, nil
	}
	imageEnd := uint64(ImageBase) + runtime.Client.imageSize
	if uint64(function&^1) < uint64(ImageBase) || uint64(function&^1) >= imageEnd {
		return WIPICPixelOp{}, nil
	}
	return WIPICPixelOp{Function: function, Param: words[1]}, nil
}

// PixelOpCache remembers what one operation answered. The operation is a pure
// function of its three arguments — both local ones are — and a blit asks it
// once per pixel, so without this a sprite sheet's worth of guest calls happens
// every frame. The cache is keyed by the pair of pixels and thrown away when
// the operation or its parameter changes.
type PixelOpCache struct {
	Function uint32
	Param    uint32
	Results  map[uint32]uint16
}

// PixelOpKey is the pair of pixels a cached answer is filed under.
func PixelOpKey(source, destination uint16) uint32 {
	return uint32(source)<<16 | uint32(destination)
}

// answers reports whether the cache holds what op has said, rather than what
// another operation or another parameter did.
func (cache *PixelOpCache) answers(op WIPICPixelOp) bool {
	return cache != nil && cache.Function == op.Function && cache.Param == op.Param
}

// ApplyPixelOp answers what a draw must write, given what it wanted to write
// and what the framebuffer already holds.
func (runtime *Runtime) ApplyPixelOp(op WIPICPixelOp, source, destination uint16) (uint16, error) {
	// The specification makes the drawing modes exclusive — setting one
	// cancels the other — so XOR answers here rather than compounding with an
	// operation the same context might also carry.
	if op.Xor {
		return source ^ destination, nil
	}
	if !op.Active() {
		return source, nil
	}
	key := PixelOpKey(source, destination)
	cache := runtime.PixelOps
	if !cache.answers(op) {
		cache = &PixelOpCache{Function: op.Function, Param: op.Param, Results: make(map[uint32]uint16)}
		runtime.PixelOps = cache
	}
	if result, ok := cache.Results[key]; ok {
		return result, nil
	}
	thread := runtime.CurrentThread
	if thread == nil {
		return source, nil
	}
	if err := runtime.EnterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.LeaveAOTCall(runtime.AOTCallOwner())
	summary, err := runtime.Client.core.Call(
		runtime.CurrentContext,
		thread,
		op.Function,
		ReturnAddress,
		[]uint32{uint32(source), uint32(destination), op.Param},
		runtime.supervisor,
	)
	if err != nil {
		return 0, fmt.Errorf("run %s pixel operation at %#x: %w", runtime.platformName(), op.Function, err)
	}
	result := uint16(summary.Context.Registers[0])
	// One count per pair the operation has never been asked about, which is
	// what the cache is worth: a run that draws the same sprites over the same
	// scene asks the guest a few thousand times and reads the map after that.
	runtime.CountDiagnostic("pixel operation guest call")
	cache.Results[key] = result
	return result, nil
}

// cachedPixelOp answers what applyPixelOp would without running any guest
// code, and reports false when the answer needs the guest. XOR and a context
// without an operation are arithmetic; a guest operation answers here only
// from what it has already said. Nothing changes either way, the cache
// included, which is what lets a copy work out a whole row before it writes
// any of it — see copyRow.
func (runtime *Runtime) cachedPixelOp(op WIPICPixelOp, source, destination uint16) (uint16, bool) {
	if op.Xor {
		return source ^ destination, true
	}
	if !op.Active() {
		return source, true
	}
	if !runtime.PixelOps.answers(op) {
		return 0, false
	}
	result, ok := runtime.PixelOps.Results[PixelOpKey(source, destination)]
	return result, ok
}

// blendPixel is the write half of every drawing call that honours a context a
// pixel at a time: read what is there, ask the operation, and write its
// answer. A context without an operation writes the source pixel, which is
// what every draw did before there was one. A copy works a row at a time and
// comes here only for the pixels it walks; see drawFramebufferCopy.
func (runtime *Runtime) blendPixel(op WIPICPixelOp, address uint32, source uint16) error {
	memory := runtime.Client.core.Memory()
	value := source
	if op.Active() {
		var existing [2]byte
		if err := memory.Read(address, existing[:]); err != nil {
			return err
		}
		result, err := runtime.ApplyPixelOp(op, source, uint16(existing[0])|uint16(existing[1])<<8)
		if err != nil {
			return err
		}
		value = result
	}
	var data [2]byte
	data[0] = byte(value)
	data[1] = byte(value >> 8)
	return memory.Write(address, data[:])
}
