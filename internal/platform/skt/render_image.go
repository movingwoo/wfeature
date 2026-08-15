package skt

import "github.com/movingwoo/wfeature/internal/jvm"

func (runtime *Runtime) drawGraphicsImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := midpImageArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	y, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	anchor, err := intArgument(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	horizontal, vertical, err := normalizeAnchor(anchor, true)
	if err != nil {
		return jvm.VoidValue(), err
	}
	left, top := anchoredImagePosition(x, y, image.width, image.height, horizontal, vertical, context)
	pixels := image.snapshot()
	context.withDestinationWrite(func() {
		context.drawRGBA(pixels, image.width, image.height, left, top)
	})
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) drawGraphicsRegion(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := midpImageArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	transform, err := intArgument(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	destinationX, err := intArgument(arguments, 7)
	if err != nil {
		return jvm.VoidValue(), err
	}
	destinationY, err := intArgument(arguments, 8)
	if err != nil {
		return jvm.VoidValue(), err
	}
	anchor, err := intArgument(arguments, 9)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if context.destination == image {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "drawRegion source is the Graphics destination")
	}
	if err := validateImageRegion(image, x, y, width, height); err != nil {
		return jvm.VoidValue(), err
	}
	if !validTransform(transform) {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid image transform")
	}
	horizontal, vertical, err := normalizeAnchor(anchor, true)
	if err != nil {
		return jvm.VoidValue(), err
	}
	pixels, transformedWidth, transformedHeight := transformImageRegion(
		image.snapshot(), image.width, int(x), int(y), int(width), int(height), transform,
	)
	left, top := anchoredImagePosition(destinationX, destinationY, transformedWidth, transformedHeight, horizontal, vertical, context)
	context.withDestinationWrite(func() {
		context.drawRGBA(pixels, transformedWidth, transformedHeight, left, top)
	})
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) drawGraphicsRGB(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, values, err := primitiveArrayArgument(arguments, 1, jvm.TypeInt)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	scanlength, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	processAlpha, err := booleanArgument(arguments, 8)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), nil
	}
	if err := validateArrayRasterRange(len(values), offset, scanlength, width, height); err != nil {
		return jvm.VoidValue(), err
	}
	left := int64(x) + int64(context.translateX)
	top := int64(y) + int64(context.translateY)
	context.withDestinationWrite(func() {
		for row := int32(0); row < height; row++ {
			input := int64(offset) + int64(row)*int64(scanlength)
			for column := int32(0); column < width; column++ {
				argb, valueErr := values[int(input+int64(column))].Int32()
				if valueErr != nil {
					continue
				}
				raw := uint32(argb)
				alpha := byte(0xff)
				if processAlpha {
					alpha = byte(raw >> 24)
				}
				context.blendPixel(left+int64(column), top+int64(row), byte(raw>>16), byte(raw>>8), byte(raw), alpha)
			}
		}
	})
	return jvm.VoidValue(), nil
}

func anchoredImagePosition(x, y int32, width, height int, horizontal, vertical int32, context *graphicsContext) (int64, int64) {
	left := int64(x) + int64(context.translateX)
	top := int64(y) + int64(context.translateY)
	switch horizontal {
	case anchorHCenter:
		left -= int64(width / 2)
	case anchorRight:
		left -= int64(width)
	}
	switch vertical {
	case anchorVCenter:
		top -= int64(height / 2)
	case anchorBottom:
		top -= int64(height)
	}
	return left, top
}

func (context *graphicsContext) drawRGBA(source []byte, width, height int, left, top int64) {
	if len(source) != width*height*4 {
		return
	}
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			index := (row*width + column) * 4
			context.blendPixel(
				left+int64(column), top+int64(row),
				source[index], source[index+1], source[index+2], source[index+3],
			)
		}
	}
}

func (context *graphicsContext) blendPixel(x, y int64, red, green, blue, alpha byte) {
	if alpha == 0 || x < int64(context.clip.minX) || x >= int64(context.clip.maxX) ||
		y < int64(context.clip.minY) || y >= int64(context.clip.maxY) {
		return
	}
	index := (int(y)*context.width + int(x)) * 4
	if alpha == 0xff {
		context.pixels[index] = red
		context.pixels[index+1] = green
		context.pixels[index+2] = blue
		context.pixels[index+3] = 0xff
		return
	}
	sourceAlpha := uint32(alpha)
	inverseAlpha := uint32(0xff - alpha)
	context.pixels[index] = byte((uint32(red)*sourceAlpha + uint32(context.pixels[index])*inverseAlpha + 127) / 255)
	context.pixels[index+1] = byte((uint32(green)*sourceAlpha + uint32(context.pixels[index+1])*inverseAlpha + 127) / 255)
	context.pixels[index+2] = byte((uint32(blue)*sourceAlpha + uint32(context.pixels[index+2])*inverseAlpha + 127) / 255)
	context.pixels[index+3] = 0xff
}
