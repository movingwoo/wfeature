package ktf

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

type runtimeHeapState struct {
	JVM                jvm.HeapState
	Roots              runtimeHeapRoots
	Opacities          []opacityState
	FramebufferOpacity []framebufferOpacityState
	Databases          []heapDatabaseState
	DatabaseBindings   []heapDatabaseBinding
	Editors            []textinput.StateSnapshot
	Storage            runtimeStorageState
	Control            runtimeControlState
}
type opacityState struct {
	Width, Height int
	Opaque        []byte
}
type framebufferOpacityState struct{ Handle, Opacity uint32 }

// An image and its guest framebuffer can share a mutable transparency mask.
// Keep that ownership outside individual image payloads so a later draw changes
// both views exactly as it does before capture.
type heapNativeContext struct {
	runtime         *initializationRuntime
	opacityIDs      map[*imageOpacity]uint32
	opacities       []opacityState
	opacityBytes    uint64
	restored        []*imageOpacity
	databaseIDs     map[*runtimeDataBaseStore]uint32
	databases       []heapDatabaseState
	restoredDBs     []*runtimeDataBaseStore
	storageBytes    uint64
	now             time.Time
	editorIDs       map[*textinput.State]uint32
	editors         []textinput.StateSnapshot
	editorBytes     uint64
	restoredEditors []*textinput.State
}

func (runtime *initializationRuntime) captureHeapState(roots []*jvm.Object) (runtimeHeapState, error) {
	return runtime.captureHeapStateAt(roots, runtime.client.now())
}

func (runtime *initializationRuntime) captureHeapStateAt(roots []*jvm.Object, now time.Time) (runtimeHeapState, error) {
	if len(runtime.framebufferOpacity) > 1<<18 {
		return runtimeHeapState{}, fmt.Errorf("KTF framebuffer opacity bindings exceed limit")
	}
	context := &heapNativeContext{runtime: runtime, opacityIDs: make(map[*imageOpacity]uint32), now: now}
	codec := runtime.heapCodec()
	codec.CaptureNative = context.captureNative
	saved := runtimeHeapState{}
	var err error
	saved.Control, err = runtime.captureControlState(context.now)
	if err != nil {
		return runtimeHeapState{}, err
	}
	saved.Storage, err = runtime.captureStorageState()
	if err != nil {
		return runtimeHeapState{}, err
	}
	saved.Roots, roots, err = runtime.capturePlatformRoots(roots, context)
	if err != nil {
		return runtimeHeapState{}, err
	}
	saved.DatabaseBindings, err = context.captureDatabaseBindings()
	if err != nil {
		return runtimeHeapState{}, err
	}
	handles := make([]uint32, 0, len(runtime.framebufferOpacity))
	for handle := range runtime.framebufferOpacity {
		handles = append(handles, handle)
	}
	slices.Sort(handles)
	for _, handle := range handles {
		id, err := context.captureOpacity(runtime.framebufferOpacity[handle])
		if err != nil {
			return runtimeHeapState{}, err
		}
		saved.FramebufferOpacity = append(saved.FramebufferOpacity, framebufferOpacityState{Handle: handle, Opacity: id})
	}
	saved.JVM, err = runtime.client.vm.CaptureHeapState(roots, codec)
	if err != nil {
		return runtimeHeapState{}, err
	}
	saved.Opacities = context.opacities
	saved.Databases = context.databases
	saved.Editors = context.editors
	return saved, nil
}

func (runtime *initializationRuntime) restoreHeapState(saved runtimeHeapState) ([]*jvm.Object, error) {
	return runtime.restoreHeapStateAt(saved, runtime.client.now())
}

func (runtime *initializationRuntime) restoreHeapStateAt(saved runtimeHeapState, now time.Time) ([]*jvm.Object, error) {
	return runtime.restoreHeapWithContext(saved, &heapNativeContext{runtime: runtime, now: now})
}

func (runtime *initializationRuntime) restoreHeapWithContext(saved runtimeHeapState, context *heapNativeContext) ([]*jvm.Object, error) {
	control, err := runtime.decodeControlState(saved.Control)
	if err != nil {
		return nil, err
	}
	storage, err := restoreStorageState(saved.Storage)
	if err != nil {
		return nil, err
	}
	if err := saved.Roots.validate(saved.JVM.Roots); err != nil {
		return nil, err
	}
	if err := validateRelayHeap(saved); err != nil {
		return nil, err
	}
	for _, surface := range saved.Roots.ImageSurfaces {
		if err := runtime.client.core.Memory().ValidateRange(surface.Handle, 4, armcore.PermissionRead); err != nil {
			return nil, err
		}
	}
	if len(saved.Opacities) > 1<<18 || len(saved.FramebufferOpacity) > 1<<18 {
		return nil, fmt.Errorf("KTF heap opacity records exceed limit")
	}
	context.restored = make([]*imageOpacity, len(saved.Opacities)+1)
	if uint64(saved.Roots.FocusedEditor) > uint64(len(saved.Editors)) {
		return nil, fmt.Errorf("KTF focused editor reference is invalid")
	}
	if err := context.restoreEditors(saved.Editors); err != nil {
		return nil, err
	}
	databases, err := context.restoreDatabases(saved.Databases, saved.DatabaseBindings)
	if err != nil {
		return nil, err
	}
	for _, mask := range saved.Opacities {
		if !validHeapImageSize(mask.Width, mask.Height) || uint64(len(mask.Opaque)) != uint64(mask.Width)*uint64(mask.Height) {
			return nil, fmt.Errorf("KTF heap opacity shape is invalid")
		}
		context.opacityBytes += uint64(len(mask.Opaque))
		if context.opacityBytes > 64<<20 {
			return nil, fmt.Errorf("KTF heap opacity data exceeds limit")
		}
		for _, value := range mask.Opaque {
			if value > 1 {
				return nil, fmt.Errorf("KTF heap opacity value is invalid")
			}
		}
	}
	for i, mask := range saved.Opacities {
		opacity := &imageOpacity{width: mask.Width, height: mask.Height, opaque: make([]bool, len(mask.Opaque))}
		for index, value := range mask.Opaque {
			opacity.opaque[index] = value != 0
		}
		context.restored[i+1] = opacity
	}
	framebuffers := make(map[uint32]*imageOpacity, len(saved.FramebufferOpacity))
	for i, binding := range saved.FramebufferOpacity {
		if binding.Handle == 0 || uint64(binding.Opacity) >= uint64(len(context.restored)) || i > 0 && saved.FramebufferOpacity[i-1].Handle >= binding.Handle {
			return nil, fmt.Errorf("KTF framebuffer opacity binding is invalid")
		}
		if err := runtime.client.core.Memory().ValidateRange(binding.Handle, 4, armcore.PermissionRead); err != nil {
			return nil, err
		}
		framebuffers[binding.Handle] = context.restored[binding.Opacity]
	}
	codec := runtime.heapCodec()
	codec.RestoreNative = context.restoreNative
	roots, err := runtime.client.vm.RestoreHeapState(saved.JVM, codec)
	if err != nil {
		return nil, err
	}
	runtime.framebufferOpacity = framebuffers
	runtime.databases = databases
	runtime.adoptPlatformRoots(saved.Roots, roots, context)
	control.adopt(runtime, context.now)
	storage.adopt(runtime)
	// A view of roots would also retain the hidden platform roots in its
	// backing array, turning weak clip/image owners into strong references.
	return append([]*jvm.Object(nil), roots[:saved.Roots.CallerCount]...), nil
}

func validHeapImageSize(width, height int) bool {
	return width > 0 && height > 0 && width <= maxWIPICFramebufferSide && height <= maxWIPICFramebufferSide && uint64(width)*uint64(height) <= maxWIPICFramebufferBytes/2
}

func (context *heapNativeContext) captureOpacity(mask *imageOpacity) (uint32, error) {
	if mask == nil {
		return 0, nil
	}
	if id := context.opacityIDs[mask]; id != 0 {
		return id, nil
	}
	if !validHeapImageSize(mask.width, mask.height) || uint64(len(mask.opaque)) != uint64(mask.width)*uint64(mask.height) || len(context.opacities) >= 1<<18 || context.opacityBytes+uint64(len(mask.opaque)) > 64<<20 {
		return 0, fmt.Errorf("KTF heap opacity exceeds shape or size limits")
	}
	context.opacityBytes += uint64(len(mask.opaque))
	record := opacityState{Width: mask.width, Height: mask.height, Opaque: make([]byte, len(mask.opaque))}
	for i, value := range mask.opaque {
		if value {
			record.Opaque[i] = 1
		}
	}
	id := uint32(len(context.opacities) + 1)
	context.opacityIDs[mask] = id
	context.opacities = append(context.opacities, record)
	return id, nil
}

func (context *heapNativeContext) captureNative(native any) (jvm.HeapExternalPayload, error) {
	switch value := native.(type) {
	case *relaySocket:
		return captureHeapRelay(value)
	case *textinput.State:
		id, err := context.captureEditor(value)
		data := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, id)
		return jvm.HeapExternalPayload{Kind: "ktf-editor-v1", Data: data}, err
	case *runtimeDataBaseStore:
		id, err := context.captureDatabase(value)
		data := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, id)
		return jvm.HeapExternalPayload{Kind: "ktf-database-v1", Data: data}, err
	case *runtimeGuestFile:
		data, err := captureHeapFile(value)
		return jvm.HeapExternalPayload{Kind: "ktf-file-v1", Data: data}, err
	case *runtimeGraphicsState:
		words := []uint32{value.target.width, value.target.height, value.target.bpl, value.target.bpp, value.target.buffer, value.target.pixels,
			uint32(value.color), value.rgb, uint32(value.clipX), uint32(value.clipY), uint32(value.clipWidth), uint32(value.clipHeight),
			uint32(value.translateX), uint32(value.translateY), uint32(value.strokeStyle), 0, uint32(value.alpha)}
		if value.xorMode {
			words[15] = 1
		}
		data := make([]byte, len(words)*4)
		for i, word := range words {
			binary.LittleEndian.PutUint32(data[4*i:], word)
		}
		if _, err := context.restoreGraphics(data); err != nil {
			return jvm.HeapExternalPayload{}, err
		}
		return jvm.HeapExternalPayload{Kind: "ktf-graphics-v1", Data: data}, nil
	case image.Image:
		data, err := context.captureImage(value)
		return jvm.HeapExternalPayload{Kind: "ktf-image-v1", Data: data}, err
	default:
		return jvm.HeapExternalPayload{}, fmt.Errorf("KTF heap has an unsupported native payload %T", native)
	}
}

func (context *heapNativeContext) restoreNative(payload jvm.HeapExternalPayload) (any, error) {
	if payload.Kind == "ktf-relay-v1" {
		return restoreHeapRelay(payload)
	}
	if len(payload.References) != 0 {
		return nil, fmt.Errorf("KTF native payload has unexpected object references")
	}
	switch payload.Kind {
	case "ktf-editor-v1":
		if len(payload.Data) != 4 {
			return nil, fmt.Errorf("KTF editor handle size is invalid")
		}
		id := binary.LittleEndian.Uint32(payload.Data)
		if id == 0 || uint64(id) >= uint64(len(context.restoredEditors)) {
			return nil, fmt.Errorf("KTF editor handle is invalid")
		}
		return context.restoredEditors[id], nil
	case "ktf-database-v1":
		if len(payload.Data) != 4 {
			return nil, fmt.Errorf("KTF database handle size is invalid")
		}
		id := binary.LittleEndian.Uint32(payload.Data)
		if id == 0 || uint64(id) >= uint64(len(context.restoredDBs)) {
			return nil, fmt.Errorf("KTF database handle is invalid")
		}
		return context.restoredDBs[id], nil
	case "ktf-file-v1":
		return restoreHeapFile(payload.Data)
	case "ktf-graphics-v1":
		return context.restoreGraphics(payload.Data)
	case "ktf-image-v1":
		return context.restoreImage(payload.Data)
	default:
		return nil, fmt.Errorf("KTF heap native payload kind is unsupported")
	}
}

func (context *heapNativeContext) restoreGraphics(data []byte) (*runtimeGraphicsState, error) {
	if len(data) != 17*4 {
		return nil, fmt.Errorf("KTF graphics state size is invalid")
	}
	var w [17]uint32
	for i := range w {
		w[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	target := wipicFramebuffer{width: w[0], height: w[1], bpl: w[2], bpp: w[3], buffer: w[4], pixels: w[5]}
	if target.width == 0 || target.height == 0 || target.width > maxWIPICFramebufferSide || target.height > maxWIPICFramebufferSide || target.bpp != 16 || uint64(target.bpl) < uint64(target.width)*2 || uint64(target.bpl)*uint64(target.height) > maxWIPICFramebufferBytes || w[6] > 65535 || w[15] > 1 {
		return nil, fmt.Errorf("KTF graphics state has an invalid surface or flag")
	}
	if err := context.runtime.client.core.Memory().ValidateRange(target.pixels, uint64(target.bpl)*uint64(target.height), armcore.PermissionReadWrite); err != nil {
		return nil, err
	}
	return &runtimeGraphicsState{target: target, color: uint16(w[6]), rgb: w[7], clipX: int32(w[8]), clipY: int32(w[9]), clipWidth: int32(w[10]), clipHeight: int32(w[11]), translateX: int32(w[12]), translateY: int32(w[13]), strokeStyle: int32(w[14]), xorMode: w[15] != 0, alpha: int32(w[16])}, nil
}

// heapImage stores the two pixel views used by KTF: premultiplied RGBA and
// unassociated NRGBA. NRGBA's hidden RGB at alpha zero must survive; converting
// every pixel through RGBA would erase a guest's transparent color key.
type heapImage struct {
	rect image.Rectangle
	data []byte
}

func (image *heapImage) Bounds() image.Rectangle { return image.rect }
func (*heapImage) ColorModel() color.Model       { return color.RGBA64Model }
func (image *heapImage) At(x, y int) color.Color {
	if x < image.rect.Min.X || x >= image.rect.Max.X || y < image.rect.Min.Y || y >= image.rect.Max.Y {
		return color.RGBA64{}
	}
	pixel := image.data[((y-image.rect.Min.Y)*image.rect.Dx()+x-image.rect.Min.X)*9:]
	r, g, b, a := binary.LittleEndian.Uint16(pixel[1:]), binary.LittleEndian.Uint16(pixel[3:]), binary.LittleEndian.Uint16(pixel[5:]), binary.LittleEndian.Uint16(pixel[7:])
	switch pixel[0] {
	case 1:
		return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}
	case 2:
		return color.NRGBA64{R: r, G: g, B: b, A: a}
	default:
		return color.RGBA64{R: r, G: g, B: b, A: a}
	}
}

func (context *heapNativeContext) captureImage(source image.Image) ([]byte, error) {
	bounds := source.Bounds()
	if !validHeapImageSize(bounds.Dx(), bounds.Dy()) || int64(bounds.Min.X) < -1<<31 || int64(bounds.Min.Y) < -1<<31 || int64(bounds.Max.X) > 1<<31-1 || int64(bounds.Max.Y) > 1<<31-1 {
		return nil, fmt.Errorf("KTF heap image bounds exceed limits")
	}
	mask, err := context.captureOpacity(imageOpacityOf(source))
	if err != nil {
		return nil, err
	}
	data := make([]byte, 20+bounds.Dx()*bounds.Dy()*9)
	for i, value := range []uint32{uint32(int32(bounds.Min.X)), uint32(int32(bounds.Min.Y)), uint32(bounds.Dx()), uint32(bounds.Dy()), mask} {
		binary.LittleEndian.PutUint32(data[4*i:], value)
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := source.At(x, y)
			kind := byte(0)
			r, g, b, a := pixel.RGBA()
			switch value := pixel.(type) {
			case color.NRGBA:
				kind, r, g, b, a = 1, uint32(value.R), uint32(value.G), uint32(value.B), uint32(value.A)
			case color.NRGBA64:
				kind, r, g, b, a = 2, uint32(value.R), uint32(value.G), uint32(value.B), uint32(value.A)
			}
			if r > 65535 || g > 65535 || b > 65535 || a > 65535 {
				return nil, fmt.Errorf("KTF heap image color exceeds component width")
			}
			offset := 20 + ((y-bounds.Min.Y)*bounds.Dx()+x-bounds.Min.X)*9
			data[offset] = kind
			for i, word := range []uint32{r, g, b, a} {
				binary.LittleEndian.PutUint16(data[offset+1+2*i:], uint16(word))
			}
		}
	}
	return data, nil
}

func (context *heapNativeContext) restoreImage(data []byte) (image.Image, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("KTF heap image is truncated")
	}
	x, y := int64(int32(binary.LittleEndian.Uint32(data))), int64(int32(binary.LittleEndian.Uint32(data[4:])))
	width, height, mask := binary.LittleEndian.Uint32(data[8:]), binary.LittleEndian.Uint32(data[12:]), binary.LittleEndian.Uint32(data[16:])
	if !validHeapImageSize(int(width), int(height)) || x+int64(width) > 1<<31-1 || y+int64(height) > 1<<31-1 || uint64(len(data)) != 20+uint64(width)*uint64(height)*9 || uint64(mask) >= uint64(len(context.restored)) {
		return nil, fmt.Errorf("KTF heap image shape or opacity is invalid")
	}
	for offset := 20; offset < len(data); offset += 9 {
		if data[offset] > 2 {
			return nil, fmt.Errorf("KTF heap image color model is invalid")
		}
		if data[offset] == 1 {
			for i := 0; i < 4; i++ {
				if binary.LittleEndian.Uint16(data[offset+1+2*i:]) > 255 {
					return nil, fmt.Errorf("KTF heap image byte color exceeds width")
				}
			}
		}
	}
	result := &heapImage{rect: image.Rect(int(x), int(y), int(x+int64(width)), int(y+int64(height))), data: append([]byte(nil), data[20:]...)}
	if mask != 0 {
		return decodedImage{Image: result, opacity: context.restored[mask]}, nil
	}
	return result, nil
}
