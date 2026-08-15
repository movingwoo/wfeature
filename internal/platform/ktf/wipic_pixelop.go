package ktf

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
type wipicPixelOp struct {
	function uint32
	param    uint32
}

func (op wipicPixelOp) active() bool { return op.function != 0 }

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
	wipicContextPixelOpOffset    = 44
	wipicContextPixelParamOffset = 48
	wipicContextFontOffset       = 32
	wipicContextStyleOffset      = 36
)

// wipicReadContextPixelOp reads the operation a draw call has to run its pixels
// through. A field this platform did not write is not trusted to be a function:
// a title that sets a font through what this handset uses for the operation
// leaves a small integer there, and calling it would execute an address that is
// not code. Only a Thumb address inside the loaded module counts.
func (runtime *initializationRuntime) wipicReadContextPixelOp(contextAddress uint32) (wipicPixelOp, error) {
	if contextAddress == 0 {
		return wipicPixelOp{}, nil
	}
	words, err := runtime.readAOTWords(contextAddress+wipicContextPixelOpOffset, 2, "graphics context pixel operation")
	if err != nil {
		return wipicPixelOp{}, err
	}
	function := words[0]
	if function&1 == 0 {
		return wipicPixelOp{}, nil
	}
	imageEnd := uint64(ImageBase) + runtime.client.image.MappedSize()
	if uint64(function&^1) < uint64(ImageBase) || uint64(function&^1) >= imageEnd {
		return wipicPixelOp{}, nil
	}
	return wipicPixelOp{function: function, param: words[1]}, nil
}

// pixelOpCache remembers what one operation answered. The operation is a pure
// function of its three arguments — both local ones are — and a blit asks it
// once per pixel, so without this a sprite sheet's worth of guest calls happens
// every frame. The cache is keyed by the pair of pixels and thrown away when
// the operation or its parameter changes.
type pixelOpCache struct {
	function uint32
	param    uint32
	results  map[uint32]uint16
}

// applyPixelOp answers what a draw must write, given what it wanted to write
// and what the framebuffer already holds.
func (runtime *initializationRuntime) applyPixelOp(op wipicPixelOp, source, destination uint16) (uint16, error) {
	if !op.active() {
		return source, nil
	}
	key := uint32(source)<<16 | uint32(destination)
	cache := runtime.pixelOps
	if cache == nil || cache.function != op.function || cache.param != op.param {
		cache = &pixelOpCache{function: op.function, param: op.param, results: make(map[uint32]uint16)}
		runtime.pixelOps = cache
	}
	if result, ok := cache.results[key]; ok {
		return result, nil
	}
	thread := runtime.currentThread
	if thread == nil {
		return source, nil
	}
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall()
	summary, err := runtime.client.core.Call(
		runtime.currentContext,
		thread,
		op.function,
		ReturnAddress,
		[]uint32{uint32(source), uint32(destination), op.param},
		runtime.handleSupervisorCall,
	)
	if err != nil {
		return 0, fmt.Errorf("run KTF pixel operation at %#x: %w", op.function, err)
	}
	result := uint16(summary.Context.Registers[0])
	// One count per pair the operation has never been asked about, which is
	// what the cache is worth: a run that draws the same sprites over the same
	// scene asks the guest a few thousand times and reads the map after that.
	runtime.countDiagnostic("pixel operation guest call")
	cache.results[key] = result
	return result, nil
}

// blendPixel is the write half of every drawing call that honours a context:
// read what is there, ask the operation, and write its answer. A context
// without an operation writes the source pixel, which is what every draw did
// before there was one.
func (runtime *initializationRuntime) blendPixel(op wipicPixelOp, address uint32, source uint16) error {
	memory := runtime.client.core.Memory()
	value := source
	if op.active() {
		var existing [2]byte
		if err := memory.Read(address, existing[:]); err != nil {
			return err
		}
		result, err := runtime.applyPixelOp(op, source, uint16(existing[0])|uint16(existing[1])<<8)
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
