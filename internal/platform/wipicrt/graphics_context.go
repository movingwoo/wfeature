package wipicrt

// The MC_GrpContext record is the title's own memory: MC_grpInitContext and
// MC_grpSetContext fill it, every drawing call that takes a context reads it,
// and a title is free to read or write a field itself through the handset's
// structure. So where each field lives is part of the contract, not a choice
// this platform gets to make.
//
// **On this handset every field is a 32-bit word**, after a leading word that
// says whether a clip is set:
//
//	0x00 clip set              0x24 offset x
//	0x04 clip x1               0x28 offset y
//	0x08 clip y1               0x2c pixel operation
//	0x0c clip x2               0x30 pixel operation parameter
//	0x10 clip y2               0x34 font
//	0x14 foreground pixel      0x38 style
//	0x18 background pixel      0x3c not known
//	0x1c transparent pixel
//	0x20 alpha
//
// That is not the order of the MC_GRP_CONTEXT_*_IDX numbers: the origin,
// index 10, comes straight after the alpha, ahead of the pixel operation and
// its parameter, 5 and 6.
//
// Two titles read it directly, and between them they fix the offsets the
// drawing calls depend on. One writes its context itself: the pixel operation
// it also hands the API is at 0x2c and the colour the operation keys on at
// 0x30. The other copies 0x40 bytes of its own record into a context to call
// MC_grpDrawImage, and when it has no pixel operation it draws the image
// itself, reading the clip as four words from 0x04, the transparent pixel from
// 0x1c and the origin from 0x24 and 0x28 — with the clip MC_grpSetContext
// stored for it. The font and the style are placed after the parameter, as
// their numbers, 7 and 8, follow its; what the two titles do with the record
// does not settle them, nor the word at 0x3c, which nothing here writes but
// MC_grpInitContext's clearing.
//
// The engine used a 52-byte record before it knew this, with the clip and the
// origin as 16-bit pairs and the font and style where the operation would
// otherwise have been. Every field it wrote it also read back, so a title that
// only goes through the API drew the same with either; the one that draws for
// itself read its clip from the wrong words and drew none of its title screen,
// menus or interface text. A session restored from a checkpoint taken before
// the change keeps that record, because the contexts it holds in guest memory
// were written that way: see LegacyGraphicsContext.
type graphicsContextLayout struct {
	// size is how many bytes MC_grpInitContext clears.
	size uint32
	// clip and offset are the first of four and two coordinates; halfwords
	// says they are 16-bit fields rather than words.
	clip, offset uint32
	halfwords    bool
	foreground   uint32
	background   uint32
	transparent  uint32
	alpha        uint32
	font, style  uint32
}

// The pixel operation and its parameter are in the same place in both
// records, so a read of them needs no layout.
const (
	WIPICContextPixelOpOffset    = 0x2c
	wipicContextPixelParamOffset = 0x30
)

// WIPICGraphicsContextSize is the size of the handset's graphics context.
const WIPICGraphicsContextSize = 0x40

var (
	handsetGraphicsContext = graphicsContextLayout{
		size: WIPICGraphicsContextSize,
		clip: 0x04, offset: 0x24,
		foreground: 0x14, background: 0x18, transparent: 0x1c, alpha: 0x20,
		font: 0x34, style: 0x38,
	}
	legacyGraphicsContext = graphicsContextLayout{
		size: 52,
		clip: 4, offset: 28, halfwords: true,
		foreground: 12, background: 16, transparent: 20, alpha: 24,
		font: 32, style: 36,
	}
)

// graphicsContext answers the record layout this session reads and writes.
func (runtime *Runtime) graphicsContext() graphicsContextLayout {
	if runtime.LegacyGraphicsContext {
		return legacyGraphicsContext
	}
	return handsetGraphicsContext
}

// WIPICForegroundOffset is where this session's contexts keep the foreground
// pixel.
func (runtime *Runtime) WIPICForegroundOffset() uint32 {
	return runtime.graphicsContext().foreground
}
