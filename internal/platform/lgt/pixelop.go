package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A graphics context can carry a pixel operation, which is the game telling the
// platform how each pixel it draws has to be combined with the one already
// there. The platform stores it in the context and every draw runs its pixels
// through it:
//
//	M_Int32 op(M_Int32 srcpxl, M_Int32 orgpxl, M_Int32 param1)
//
// **The two arguments are not in the order the names suggest, and this platform
// is not the other one.** The specification's prose is explicit where its names
// are not: `srcpxl` is "프레임 버퍼에 있는 픽셀 값", the pixel already in the
// framebuffer, and the second is the pixel about to be written. Its own alpha
// example settles it — `(srcpxl * (255 - param1) + orgpxl * param1) / 255` with
// an alpha where 255 means fully opaque has to answer the *incoming* pixel at
// 255, so the incoming pixel is the second argument.
//
// Both operations a local title installs agree, and they are the reason this is
// stated so firmly rather than inferred from the prototype. One is four
// instructions:
//
//	if (arg1 == key) return arg0;   /* the colour the art declares transparent */
//	return arg1;
//
// It compares **arg1** against its transparent-colour key, which is only
// meaningful if arg1 is the pixel being drawn — keying on what is already on
// the screen would hide sprites at random. The other adds the two pixels
// channel-wise in RGB565, clamping red and blue at 31 and green at 63, behind
// the same key test: an additive blend, which is what an effect is.
//
// KTF's titles pass them the other way round, and that divergence is real
// rather than a mistake on one side; see `wipic_pixelop.go` there and
// "A pixel operation's arguments are not in the same order on both platforms"
// in `docs/lgt.md`.
type pixelOp struct {
	function uint32
	param    uint32
}

func (op pixelOp) active() bool { return op.function != 0 }

// readContextPixelOp validates the two words MC_grpSetContext writes for the
// operation and its parameter. A field this platform did not write is not
// trusted to be a function: only a Thumb address inside the loaded module
// counts, because a title that stores something else there — KTF has one that
// leaves a font handle in the equivalent word — would otherwise have the
// platform branch to an address that is not code.
//
// **Being inside the module is not enough, because a return address is too.**
// One title's context carried `0xf81f`, which passes that test and is the
// middle of a large routine of its own: run as an operation it walked a
// structure base it had never been given and faulted on a read of `0x98`. The
// second test is therefore whether this platform was ever *handed* that
// address as an operation — `installPixelOp` records every one that arrives
// through `MC_grpSetContext`, and a word that is not among them is a leftover
// rather than a function.
//
// The set is per client rather than per context on purpose. A title that
// copies a context, saves and restores one, or builds a second one beside the
// first carries the operation's address with it and keeps it; only an address
// that was never installed anywhere is refused. `MC_grpInitContext` zeroes the
// record, so a context that has never been given an operation has none, which
// is what makes the absence meaningful rather than a guess.
func (client *Client) readContextPixelOp(function, param uint32) pixelOp {
	if function&1 == 0 {
		return pixelOp{}
	}
	if client.module == nil {
		return pixelOp{}
	}
	low, high := client.module.Span()
	if target := function &^ 1; target < low || target >= high {
		return pixelOp{}
	}
	if !client.installedPixelOps[function] {
		client.countUninstalledPixelOp(function)
		return pixelOp{}
	}
	return pixelOp{function: function, param: param}
}

// installPixelOp records an operation this platform was handed through
// MC_grpSetContext. The bound is the same reasoning as the operation cache's:
// the address comes from guest memory, and a title writing a new one every
// frame must not grow this without limit. Reaching the bound stops recording
// rather than forgetting, so a title that installs a handful keeps them.
func (client *Client) installPixelOp(function uint32) {
	if function&1 == 0 {
		return
	}
	if client.installedPixelOps == nil {
		client.installedPixelOps = make(map[uint32]bool, maxInstalledPixelOps)
	}
	if len(client.installedPixelOps) >= maxInstalledPixelOps && !client.installedPixelOps[function] {
		return
	}
	client.installedPixelOps[function] = true
}

// maxInstalledPixelOps bounds the set above. The titles here install two or
// three.
const maxInstalledPixelOps = 32

// countUninstalledPixelOp records a context word that looked like an operation
// and was not one. It is counted rather than logged per draw: the word is read
// once per drawing call, and a title with a leftover in the field has one on
// every call it makes.
func (client *Client) countUninstalledPixelOp(function uint32) {
	if client.uninstalledPixelOps == nil {
		client.uninstalledPixelOps = make(map[uint32]uint64)
	}
	if len(client.uninstalledPixelOps) > maxInstalledPixelOps {
		return
	}
	client.uninstalledPixelOps[function]++
	if client.uninstalledPixelOps[function] == 1 && client.logger != nil {
		client.logger.Debug("LGT context pixel operation was never installed", "function", function)
	}
}

// pixelOpCache remembers what one operation answered, keyed by the pair of
// pixels it was asked about. The operation is a pure function of its three
// arguments — every local one is — and a draw asks it once per pixel, so
// without this a screen's worth of guest calls happens every frame.
//
// **There is one of these per operation, not one in total.** A title switches
// between operations far more often than is obvious: the one measured here
// alternates between a colour key and an alpha blend about twice a tick, and a
// single cache keyed by "the current operation" was therefore thrown away 3,879
// times over 2,000 ticks — which is most of the value of having one. They are
// held together rather than replaced.
type pixelOpCache struct {
	results map[uint32]uint16
}

const (
	// maxCachedPixelOps bounds how many operations are remembered at once. The
	// titles here install two or three; the bound exists because the function
	// pointer comes from guest memory and a title that wrote a new one every
	// frame would otherwise grow this without limit.
	maxCachedPixelOps = 8
	// maxCachedPixelPairs bounds one operation's answers. Exceeding either
	// bound costs speed and not correctness: the answers are dropped and the
	// guest is asked again.
	maxCachedPixelPairs = 1 << 15
)

// applyPixelOp answers what a draw must write, given what the framebuffer holds
// and what the draw wanted to put there.
func (client *Client) applyPixelOp(
	ctx context.Context, thread *armcore.Thread, op pixelOp, existing, incoming uint16,
) (uint16, error) {
	if !op.active() {
		return incoming, nil
	}
	key := uint32(existing)<<16 | uint32(incoming)
	identity := uint64(op.function)<<32 | uint64(op.param)
	if client.pixelOps == nil {
		client.pixelOps = make(map[uint64]*pixelOpCache)
	}
	cache := client.pixelOps[identity]
	if cache == nil {
		if len(client.pixelOps) >= maxCachedPixelOps {
			client.pixelOps = make(map[uint64]*pixelOpCache)
		}
		cache = &pixelOpCache{results: make(map[uint32]uint16)}
		client.pixelOps[identity] = cache
		// Once per operation the title actually uses, which is two or three
		// lines in a run rather than one per switch.
		if client.logger != nil {
			client.logger.Debug("LGT pixel operation installed",
				"function", op.function, "param", op.param)
		}
	}
	if result, ok := cache.results[key]; ok {
		return result, nil
	}
	if thread == nil {
		return incoming, nil
	}
	// The call is made on the thread that is running rather than the platform's
	// own, because the guest is inside a draw call and its stack pointer is
	// where it left it; see callOn.
	value, err := client.callOn(ctx, thread, op.function, []uint32{uint32(existing), uint32(incoming), op.param})
	if err != nil {
		return 0, fmt.Errorf("run LGT pixel operation at %#x: %w", op.function, err)
	}
	result := uint16(value)
	if len(cache.results) >= maxCachedPixelPairs {
		cache.results = make(map[uint32]uint16)
	}
	cache.results[key] = result
	return result, nil
}
