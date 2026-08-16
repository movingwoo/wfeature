package skt

import (
	"bytes"
	"fmt"
	stdimage "image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path"
	"strings"
	"sync"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const maxEncodedImageBytes = 16 * 1024 * 1024

const (
	transformNone         int32 = 0
	transformMirrorRot180 int32 = 1
	transformMirror       int32 = 2
	transformRot180       int32 = 3
	transformMirrorRot270 int32 = 4
	transformRot90        int32 = 5
	transformRot270       int32 = 6
	transformMirrorRot90  int32 = 7
)

type imageData struct {
	mu      sync.RWMutex
	width   int
	height  int
	rgba    []byte
	mutable bool
}

func (runtime *Runtime) createMutableImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	width, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := newMIDPImage(int(width), int(height), true, nil)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(image), nil
}

func (runtime *Runtime) createImageCopy(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	source, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	pixels := source.snapshot()
	image, err := newMIDPImage(source.width, source.height, false, pixels)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(image), nil
}

func (runtime *Runtime) createImageRegion(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	source, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	transform, err := intArgument(arguments, 5)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := validateImageRegion(source, x, y, width, height); err != nil {
		return jvm.VoidValue(), err
	}
	if !validTransform(transform) {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid image transform")
	}
	pixels, transformedWidth, transformedHeight := transformImageRegion(source.snapshot(), source.width, int(x), int(y), int(width), int(height), transform)
	image, err := newMIDPImage(transformedWidth, transformedHeight, false, pixels)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(image), nil
}

func (runtime *Runtime) createImageFromResource(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	resourceName, err := imageResourceName(name)
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/io/IOException", err.Error())
	}
	data, ok := runtime.Archive.Resource(resourceName)
	if !ok {
		return jvm.VoidValue(), newGuestException("java/io/IOException", "image resource not found: "+name)
	}
	image, err := decodeMIDPImage(data)
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/io/IOException", err.Error())
	}
	return jvm.ReferenceValue(image), nil
}

func (runtime *Runtime) createImageFromBytes(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, values, err := primitiveArrayArgument(arguments, 0, jvm.TypeByte)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || length <= 0 || int64(offset)+int64(length) > int64(len(values)) {
		return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException", "invalid encoded image range")
	}
	data := make([]byte, int(length))
	for index, value := range values[int(offset):int(offset+length)] {
		raw, valueErr := value.Int32()
		if valueErr != nil {
			return jvm.VoidValue(), fmt.Errorf("encoded image byte %d: %w", index, valueErr)
		}
		data[index] = byte(raw)
	}
	image, err := decodeMIDPImage(data)
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", err.Error())
	}
	return jvm.ReferenceValue(image), nil
}

func (runtime *Runtime) createRGBImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, values, err := primitiveArrayArgument(arguments, 0, jvm.TypeInt)
	if err != nil {
		return jvm.VoidValue(), err
	}
	width, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	processAlpha, err := booleanArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	byteLength, err := backend.RGBAByteLength(int(width), int(height))
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", err.Error())
	}
	pixelCount := byteLength / 4
	if len(values) < pixelCount {
		return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException", "ARGB array is shorter than image dimensions")
	}
	rgba := make([]byte, byteLength)
	for index := 0; index < pixelCount; index++ {
		argb, valueErr := values[index].Int32()
		if valueErr != nil {
			return jvm.VoidValue(), fmt.Errorf("ARGB pixel %d: %w", index, valueErr)
		}
		raw := uint32(argb)
		rgba[index*4] = byte(raw >> 16)
		rgba[index*4+1] = byte(raw >> 8)
		rgba[index*4+2] = byte(raw)
		rgba[index*4+3] = 0xff
		if processAlpha {
			rgba[index*4+3] = byte(raw >> 24)
		}
	}
	image, err := newMIDPImage(int(width), int(height), false, rgba)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(image), nil
}

func (runtime *Runtime) getImageGraphics(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	image, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !image.mutable {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "immutable Image has no Graphics")
	}
	clip := paintRect{maxX: image.width, maxY: image.height}
	context := &graphicsContext{
		pixels:      image.rgba,
		width:       image.width,
		height:      image.height,
		destination: image,
		deviceClip:  clip,
		clip:        clip,
		font:        runtime.fontObject(fontSystem, fontPlain, fontMedium),
		active:      true,
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: midp.GraphicsClass, Fields: make(map[string]jvm.Value), Native: context}), nil
}

func (runtime *Runtime) getImageWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	image, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(image.width)), nil
}

func (runtime *Runtime) getImageHeight(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	image, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(image.height)), nil
}

func (runtime *Runtime) isImageMutable(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	image, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if image.mutable {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) getImageRGB(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	image, err := midpImageArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	arrayObject, values, err := primitiveArrayArgument(arguments, 1, jvm.TypeInt)
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
	if x < 0 || y < 0 || int64(x)+int64(width) > int64(image.width) || int64(y)+int64(height) > int64(image.height) {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "getRGB region exceeds Image bounds")
	}
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), nil
	}
	if abs64(int64(scanlength)) < int64(width) {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "absolute scanlength is smaller than width")
	}
	if err := validateArrayRasterRange(len(values), offset, scanlength, width, height); err != nil {
		return jvm.VoidValue(), err
	}
	pixels := image.snapshot()
	for row := int32(0); row < height; row++ {
		output := int64(offset) + int64(row)*int64(scanlength)
		rowValues := make([]jvm.Value, int(width))
		for column := int32(0); column < width; column++ {
			pixel := ((int(y+row)*image.width + int(x+column)) * 4)
			argb := uint32(pixels[pixel+3])<<24 | uint32(pixels[pixel])<<16 | uint32(pixels[pixel+1])<<8 | uint32(pixels[pixel+2])
			rowValues[column] = jvm.IntValue(int32(argb))
		}
		if err := jvm.SetArrayRange(arrayObject, int(output), rowValues); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

func newMIDPImage(width, height int, mutable bool, rgba []byte) (*jvm.Object, error) {
	byteLength, err := backend.RGBAByteLength(width, height)
	if err != nil {
		return nil, newGuestException("java/lang/IllegalArgumentException", err.Error())
	}
	if rgba == nil {
		rgba = make([]byte, byteLength)
		for index := 0; index < byteLength; index += 4 {
			rgba[index] = 0xff
			rgba[index+1] = 0xff
			rgba[index+2] = 0xff
			rgba[index+3] = 0xff
		}
	} else if len(rgba) != byteLength {
		return nil, fmt.Errorf("image RGBA length is %d, want %d", len(rgba), byteLength)
	} else {
		rgba = append([]byte(nil), rgba...)
	}
	return &jvm.Object{
		ClassName: midp.ImageClass,
		Fields:    make(map[string]jvm.Value),
		Native:    &imageData{width: width, height: height, rgba: rgba, mutable: mutable},
	}, nil
}

func decodeMIDPImage(data []byte) (*jvm.Object, error) {
	if len(data) == 0 || len(data) > maxEncodedImageBytes {
		return nil, fmt.Errorf("encoded image length %d is outside 1..%d", len(data), maxEncodedImageBytes)
	}
	// The handset's own bitmap, which no standard decoder recognises. No local
	// archive for this platform ships one — the format was found in another
	// vendor's — but the decode is shared and the alternative to routing it
	// here is a title that ends on an image the other platforms can read.
	if wipic.IsLBMP(data) {
		decoded, err := wipic.DecodeLBMP(data)
		if err != nil {
			return nil, err
		}
		return midpImageFromDecoded(decoded)
	}
	config, _, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image header: %w", err)
	}
	// The header is checked before the pixels are decoded, so a header naming
	// a size nothing could hold costs a refusal rather than the allocation.
	if _, err := backend.RGBAByteLength(config.Width, config.Height); err != nil {
		return nil, fmt.Errorf("decode image dimensions: %w", err)
	}
	decoded, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image pixels: %w", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return nil, fmt.Errorf("decoded image dimensions changed from %dx%d to %dx%d", config.Width, config.Height, bounds.Dx(), bounds.Dy())
	}
	return midpImageFromDecoded(decoded)
}

// midpImageFromDecoded copies a decoded image into the straight-alpha RGBA an
// Image holds. Every decode path ends here, so a format added to the router
// above is stored the same way as any other.
func midpImageFromDecoded(decoded stdimage.Image) (*jvm.Object, error) {
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	byteLength, err := backend.RGBAByteLength(width, height)
	if err != nil {
		return nil, fmt.Errorf("decode image dimensions: %w", err)
	}
	rgba := make([]byte, byteLength)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := color.NRGBAModel.Convert(decoded.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			index := (y*width + x) * 4
			rgba[index] = pixel.R
			rgba[index+1] = pixel.G
			rgba[index+2] = pixel.B
			rgba[index+3] = pixel.A
		}
	}
	return newMIDPImage(width, height, false, rgba)
}

func (image *imageData) snapshot() []byte {
	image.mu.RLock()
	pixels := append([]byte(nil), image.rgba...)
	image.mu.RUnlock()
	return pixels
}

func midpImageArgument(arguments []jvm.Value, index int) (*imageData, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Image is null")
	}
	image, ok := object.Native.(*imageData)
	if object.ClassName != midp.ImageClass || !ok || image == nil {
		return nil, fmt.Errorf("argument %d is not a MIDP Image", index)
	}
	return image, nil
}

func primitiveArrayArgument(arguments []jvm.Value, index int, kind jvm.TypeKind) (*jvm.Object, []jvm.Value, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, nil, err
	}
	if object == nil {
		return nil, nil, newGuestException("java/lang/NullPointerException", "array is null")
	}
	component, values, err := jvm.ArraySnapshot(object)
	if err != nil {
		return nil, nil, err
	}
	if component.Kind != kind {
		return nil, nil, fmt.Errorf("argument %d has array component %s", index, component.Descriptor())
	}
	return object, values, nil
}

func stringArgument(arguments []jvm.Value, index int) (string, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", newGuestException("java/lang/NullPointerException", "String is null")
	}
	value, ok := object.Native.(string)
	if object.ClassName != "java/lang/String" || !ok {
		return "", fmt.Errorf("argument %d is not a String", index)
	}
	return value, nil
}

func booleanArgument(arguments []jvm.Value, index int) (bool, error) {
	value, err := intArgument(arguments, index)
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

func validateImageRegion(image *imageData, x, y, width, height int32) error {
	if width <= 0 || height <= 0 || x < 0 || y < 0 ||
		int64(x)+int64(width) > int64(image.width) || int64(y)+int64(height) > int64(image.height) {
		return newGuestException("java/lang/IllegalArgumentException", "image region exceeds source bounds")
	}
	return nil
}

func validTransform(transform int32) bool {
	return transform >= transformNone && transform <= transformMirrorRot90
}

func transformImageRegion(source []byte, sourceWidth, sourceX, sourceY, width, height int, transform int32) ([]byte, int, int) {
	destinationWidth, destinationHeight := transformedDimensions(width, height, transform)
	destination := make([]byte, destinationWidth*destinationHeight*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			destinationX, destinationY := transformedPoint(x, y, width, height, transform)
			sourceIndex := ((sourceY+y)*sourceWidth + sourceX + x) * 4
			destinationIndex := (destinationY*destinationWidth + destinationX) * 4
			copy(destination[destinationIndex:destinationIndex+4], source[sourceIndex:sourceIndex+4])
		}
	}
	return destination, destinationWidth, destinationHeight
}

func transformedDimensions(width, height int, transform int32) (int, int) {
	switch transform {
	case transformMirrorRot270, transformRot90, transformRot270, transformMirrorRot90:
		return height, width
	default:
		return width, height
	}
}

func transformedPoint(x, y, width, height int, transform int32) (int, int) {
	switch transform {
	case transformMirrorRot180:
		return x, height - 1 - y
	case transformMirror:
		return width - 1 - x, y
	case transformRot180:
		return width - 1 - x, height - 1 - y
	case transformMirrorRot270:
		return y, x
	case transformRot90:
		return height - 1 - y, x
	case transformRot270:
		return y, width - 1 - x
	case transformMirrorRot90:
		return height - 1 - y, width - 1 - x
	default:
		return x, y
	}
}

func imageResourceName(name string) (string, error) {
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "\\") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("invalid image resource name %q", name)
	}
	return name, nil
}

func validateArrayRasterRange(arrayLength int, offset, scanlength, width, height int32) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	lastRow := int64(height-1) * int64(scanlength)
	minimum := int64(offset) + min(int64(0), lastRow)
	maximum := int64(offset) + max(int64(0), lastRow) + int64(width) - 1
	if minimum < 0 || maximum >= int64(arrayLength) {
		return newGuestException("java/lang/ArrayIndexOutOfBoundsException", "pixel array range")
	}
	return nil
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
