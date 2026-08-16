// Package curve draws the two shapes a feature-phone platform cannot express
// as rectangles: arcs and rounded rectangles.
//
// It exists because four separate drawing surfaces need exactly this geometry
// and they have nothing else in common — the WIPI-C graphics tables of both
// ARM platforms, and the Java `Graphics` of all three. Written once per
// surface it was written wrong on three of them: two answered an unimplemented
// slot, which is fatal, and one drew a rounded rectangle as a hard rectangle.
//
// The specifications agree on the contract, which is what makes one
// implementation legitimate rather than convenient. WIPI's `MC_grpDrawArc` and
// MIDP's `Graphics.drawArc` both put zero degrees at three o'clock, count
// positive angles counter-clockwise, take the second angle as an *extent*
// rather than an end, and centre the arc in the bounding rectangle. Round
// rectangles take corner **diameters**, not radii.
//
// Nothing here draws. Each function walks its shape and hands out horizontal
// spans, and the caller fills them through whatever clipped primitive it
// already has — which is what keeps the platform's own clip, colour, blend and
// transparency rules in force rather than reimplemented here three times.
package curve

import "math"

// Span is one horizontal run of pixels to fill.
type Span struct {
	X, Y, Width int32
}

// Emit receives each span. Returning an error abandons the shape, so a caller
// whose fill can fail reports the first failure rather than drawing on past it.
type Emit func(Span) error

// RoundRectRadii answers the corner radii a rounded rectangle actually uses.
// A diameter larger than the side it rounds is clamped, which is what makes
// `fillRoundRect(x, y, w, h, w, h)` an ellipse rather than an overflow.
//
// It is exported because a caller with a cheap whole-rectangle fill wants to
// know when the corners have collapsed and the shape is just a rectangle.
func RoundRectRadii(width, height, arcWidth, arcHeight int32) (int32, int32) {
	radiusX, radiusY := arcWidth/2, arcHeight/2
	if radiusX < 0 {
		radiusX = 0
	}
	if radiusY < 0 {
		radiusY = 0
	}
	if radiusX > width/2 {
		radiusX = width / 2
	}
	if radiusY > height/2 {
		radiusY = height / 2
	}
	return radiusX, radiusY
}

// roundRectInset is how far a row is drawn in from the rectangle's edge. Rows
// past the corners are not inset at all; a row inside one is inset by the
// corner ellipse.
func roundRectInset(row, height, radiusX, radiusY int32) int32 {
	if radiusX <= 0 || radiusY <= 0 {
		return 0
	}
	// The distance is measured from the corner ellipse's centre to the row's
	// own centre, so the first and last rows carry the span the curve has
	// there rather than none at all.
	var distance float64
	switch {
	case row < radiusY:
		distance = float64(radiusY-row) - 0.5
	case row >= height-radiusY:
		distance = float64(row-(height-radiusY)) + 0.5
	default:
		return 0
	}
	if distance > float64(radiusY) {
		distance = float64(radiusY)
	}
	ratio := distance / float64(radiusY)
	span := float64(radiusX) * math.Sqrt(math.Max(0, 1-ratio*ratio))
	inset := radiusX - int32(math.Round(span))
	if inset < 0 {
		inset = 0
	}
	return inset
}

// FillRoundRect fills a rectangle whose corners are quarter ellipses, a row at
// a time so every row goes through the caller's own clipped fill.
func FillRoundRect(x, y, width, height, arcWidth, arcHeight int32, emit Emit) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	radiusX, radiusY := RoundRectRadii(width, height, arcWidth, arcHeight)
	for row := int32(0); row < height; row++ {
		inset := roundRectInset(row, height, radiusX, radiusY)
		if span := width - 2*inset; span > 0 {
			if err := emit(Span{X: x + inset, Y: y + row, Width: span}); err != nil {
				return err
			}
		}
	}
	return nil
}

// DrawRoundRect outlines the same shape: the two ends of every row that the
// fill would have covered, which draws the straight edges and the corner
// curves without a second piece of geometry.
func DrawRoundRect(x, y, width, height, arcWidth, arcHeight int32, emit Emit) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	radiusX, radiusY := RoundRectRadii(width, height, arcWidth, arcHeight)
	previous := int32(-1)
	for row := int32(0); row < height; row++ {
		inset := roundRectInset(row, height, radiusX, radiusY)
		// The top and bottom rows are drawn whole; a row whose inset moved
		// draws from the previous inset so the curve has no gaps in it.
		if row == 0 || row == height-1 {
			if span := width - 2*inset; span > 0 {
				if err := emit(Span{X: x + inset, Y: y + row, Width: span}); err != nil {
					return err
				}
			}
			previous = inset
			continue
		}
		thickness := int32(1)
		if previous >= 0 && previous > inset {
			thickness = previous - inset + 1
		}
		if err := emit(Span{X: x + inset, Y: y + row, Width: thickness}); err != nil {
			return err
		}
		if err := emit(Span{X: x + width - inset - thickness, Y: y + row, Width: thickness}); err != nil {
			return err
		}
		previous = inset
	}
	return nil
}

// arcSpan normalises an arc's angles to a start in [0, 360) and a positive
// extent, capped at a full turn.
func arcSpan(start, extent int32) (float64, float64) {
	if extent >= 360 || extent <= -360 {
		return 0, 360
	}
	begin, sweep := float64(start), float64(extent)
	if sweep < 0 {
		begin, sweep = begin+sweep, -sweep
	}
	begin = math.Mod(begin, 360)
	if begin < 0 {
		begin += 360
	}
	return begin, sweep
}

// arcContains reports whether a point offset from the ellipse centre falls
// inside the arc's angular span. Angles run counter-clockwise from three
// o'clock, so the screen's downward y is negated.
func arcContains(offsetX, offsetY, begin, sweep float64) bool {
	if sweep >= 360 {
		return true
	}
	angle := math.Atan2(-offsetY, offsetX) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}
	relative := angle - begin
	if relative < 0 {
		relative += 360
	}
	return relative <= sweep
}

// FillArc fills the pie slice an arc cuts out of its bounding ellipse, which
// is what filling an arc means in both specifications.
func FillArc(x, y, width, height, start, extent int32, emit Emit) error {
	if width <= 0 || height <= 0 || extent == 0 {
		return nil
	}
	begin, sweep := arcSpan(start, extent)
	radiusX, radiusY := float64(width)/2, float64(height)/2
	centreX, centreY := float64(x)+radiusX, float64(y)+radiusY
	for row := int32(0); row < height; row++ {
		pixelY := float64(y+row) + 0.5
		offsetY := pixelY - centreY
		// A run of covered pixels is emitted once rather than a pixel at a
		// time, which keeps a full circle to one span per row.
		runStart := int32(-1)
		for column := int32(0); column <= width; column++ {
			inside := false
			if column < width {
				offsetX := float64(x+column) + 0.5 - centreX
				normalX, normalY := offsetX/radiusX, offsetY/radiusY
				inside = normalX*normalX+normalY*normalY <= 1 && arcContains(offsetX, offsetY, begin, sweep)
			}
			switch {
			case inside && runStart < 0:
				runStart = column
			case !inside && runStart >= 0:
				if err := emit(Span{X: x + runStart, Y: y + row, Width: column - runStart}); err != nil {
					return err
				}
				runStart = -1
			}
		}
	}
	return nil
}

// DrawArc plots the arc's curve itself. Drawing an arc is the curve and not
// the pie's two straight edges, so this walks the angle rather than the
// bounding box.
func DrawArc(x, y, width, height, start, extent int32, emit Emit) error {
	if width <= 0 || height <= 0 || extent == 0 {
		return nil
	}
	begin, sweep := arcSpan(start, extent)
	radiusX, radiusY := float64(width)/2, float64(height)/2
	centreX, centreY := float64(x)+radiusX, float64(y)+radiusY
	// One step per pixel of the longer radius keeps the curve continuous
	// without plotting the same pixel dozens of times on a small arc.
	steps := int(math.Max(8, 2*math.Pi*math.Max(radiusX, radiusY)))
	previousX, previousY := int32(math.MinInt32), int32(math.MinInt32)
	for step := 0; step <= steps; step++ {
		angle := (begin + sweep*float64(step)/float64(steps)) * math.Pi / 180
		pixelX := int32(math.Round(centreX + radiusX*math.Cos(angle) - 0.5))
		pixelY := int32(math.Round(centreY - radiusY*math.Sin(angle) - 0.5))
		if pixelX == previousX && pixelY == previousY {
			continue
		}
		if err := emit(Span{X: pixelX, Y: pixelY, Width: 1}); err != nil {
			return err
		}
		previousX, previousY = pixelX, pixelY
	}
	return nil
}
