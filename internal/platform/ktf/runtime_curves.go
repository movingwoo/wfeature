package ktf

import "github.com/movingwoo/wfeature/internal/curve"

// Rounded rectangles and arcs, drawn as curves rather than as the bounding
// rectangle they used to be approximated with.
//
// The approximation was visible and one title made it obvious: it draws every
// character's ground shadow as a `fillRoundRect` whose corner diameters are
// the whole width and height, which is an ellipse, and 17,000 times a run it
// came out as a hard rectangle under the character. The same call is how these
// titles round a dialogue box, so the shape is worth having right rather than
// worth special-casing.
//
// The geometry itself lives in `internal/curve`, because this platform's Java
// surface is one of five that need it and it is the same shape in all of them.
// What stays here is the binding: every span goes through the clipped fill the
// rest of this runtime's drawing uses, so the clip, the colour and the blend
// are whatever the graphics state says they are.

// curveEmit fills each span the geometry hands out through the runtime's own
// clipped rectangle fill.
func (runtime *initializationRuntime) curveEmit(state *runtimeGraphicsState) curve.Emit {
	return func(span curve.Span) error {
		return runtime.graphicsFillRect(state, span.X, span.Y, span.Width, 1, state.color)
	}
}

// graphicsFillRoundRect fills a rectangle whose corners are quarter ellipses.
func (runtime *initializationRuntime) graphicsFillRoundRect(state *runtimeGraphicsState, x, y, width, height, arcWidth, arcHeight int32) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	// Corners that collapsed leave a plain rectangle, and one fill is cheaper
	// than one per row for a shape a dialogue box uses at full size.
	if radiusX, radiusY := curve.RoundRectRadii(width, height, arcWidth, arcHeight); radiusX == 0 || radiusY == 0 {
		return runtime.graphicsFillRect(state, x, y, width, height, state.color)
	}
	return curve.FillRoundRect(x, y, width, height, arcWidth, arcHeight, runtime.curveEmit(state))
}

// graphicsDrawRoundRect outlines the same shape.
func (runtime *initializationRuntime) graphicsDrawRoundRect(state *runtimeGraphicsState, x, y, width, height, arcWidth, arcHeight int32) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	if radiusX, radiusY := curve.RoundRectRadii(width, height, arcWidth, arcHeight); radiusX == 0 || radiusY == 0 {
		return runtime.graphicsDrawRectOutline(state, x, y, width, height)
	}
	return curve.DrawRoundRect(x, y, width, height, arcWidth, arcHeight, runtime.curveEmit(state))
}

// graphicsFillArc fills the pie slice an arc cuts out of its bounding ellipse.
func (runtime *initializationRuntime) graphicsFillArc(state *runtimeGraphicsState, x, y, width, height, start, extent int32) error {
	return curve.FillArc(x, y, width, height, start, extent, runtime.curveEmit(state))
}

// graphicsDrawArc plots the arc's curve itself.
func (runtime *initializationRuntime) graphicsDrawArc(state *runtimeGraphicsState, x, y, width, height, start, extent int32) error {
	return curve.DrawArc(x, y, width, height, start, extent, runtime.curveEmit(state))
}
