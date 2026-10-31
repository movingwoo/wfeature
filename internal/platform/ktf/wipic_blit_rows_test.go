package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The copies read and write a row at a time. These tests hold them to the
// pixel-at-a-time walk they replaced, kept below exactly as it was: every byte
// a copy leaves in guest memory, every transparency mark, every guest pixel
// operation call with its arguments and order, the operation cache and the
// error, compared on randomized copies and on the edges a row has.

// referenceBlitClipped is wipicBlitClipped as it was before copies were drawn
// a row at a time: one source read, one operation and one write per pixel.
func referenceBlitClipped(runtime *initializationRuntime, destination wipicrt.WIPICFramebuffer, clip wipicrt.WIPICClip, op wipicrt.WIPICPixelOp, destinationX, destinationY, width, height int32, source wipicrt.WIPICFramebuffer, sourceX, sourceY int32, opacity wipicrt.BlitOpacity) error {
	destinationX, destinationY, width, height, sourceX, sourceY, ok := wipicrt.ClipCopy(destination, source, destinationX, destinationY, width, height, sourceX, sourceY)
	if !ok {
		return nil
	}
	memory := runtime.client.Core().Memory()
	snapshot, err := runtime.BlitSourceSnapshot(destination, destinationX, destinationY, width, height, source, sourceX, sourceY)
	if err != nil {
		return err
	}
	for line := int32(0); line < height; line++ {
		fromY := sourceY + line
		toY := destinationY + line
		if fromY < 0 || toY < 0 || uint32(fromY) >= source.Height || uint32(toY) >= destination.Height {
			continue
		}
		for column := int32(0); column < width; column++ {
			fromX := sourceX + column
			toX := destinationX + column
			if fromX < 0 || toX < 0 || uint32(fromX) >= source.Width || uint32(toX) >= destination.Width {
				continue
			}
			if !clip.Contains(toX, toY) {
				continue
			}
			if !opacity.Source.OpaqueAt(int(fromX), int(fromY)) {
				continue
			}
			var data [2]byte
			if snapshot != nil {
				copy(data[:], snapshot[(int(line)*int(width)+int(column))*2:])
			} else if err := memory.Read(source.Pixels+uint32(fromY)*source.Bpl+uint32(fromX)*2, data[:]); err != nil {
				return err
			}
			address := destination.Pixels + uint32(toY)*destination.Bpl + uint32(toX)*2
			if err := referenceBlendPixel(runtime, op, address, binary.LittleEndian.Uint16(data[:])); err != nil {
				return err
			}
			opacity.Destination.MarkOpaque(int(toX), int(toY))
		}
	}
	return nil
}

// referenceCopyFramebuffer is wipicCopyFramebuffer as it was before copies
// were drawn a row at a time.
func referenceCopyFramebuffer(runtime *initializationRuntime, thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 9)
	for index := range registers {
		value, err := runtime.WIPICArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	destination, ok, err := runtime.ReadWIPICDrawSurface(registers[0], "copy frame buffer destination")
	if err != nil || !ok {
		return 0, err
	}
	source, ok, err := runtime.ReadWIPICDrawSurface(registers[5], "copy frame buffer source")
	if err != nil || !ok {
		return 0, err
	}
	clip, err := runtime.WIPICReadContextClip(registers[8])
	if err != nil {
		return 0, err
	}
	op, err := runtime.WIPICReadContextPixelOp(registers[8])
	if err != nil {
		return 0, err
	}
	destinationX, destinationY, width, height, sourceX, sourceY, ok := wipicrt.ClipCopy(destination, source,
		int32(registers[1]), int32(registers[2]), int32(registers[3]), int32(registers[4]), int32(registers[6]), int32(registers[7]))
	if !ok {
		return 0, nil
	}
	memory := runtime.client.Core().Memory()
	for line := int32(0); line < height; line++ {
		fromY := sourceY + line
		toY := destinationY + line
		if fromY < 0 || toY < 0 || uint32(fromY) >= source.Height || uint32(toY) >= destination.Height {
			continue
		}
		for column := int32(0); column < width; column++ {
			fromX := sourceX + column
			toX := destinationX + column
			if fromX < 0 || toX < 0 || uint32(fromX) >= source.Width || uint32(toX) >= destination.Width {
				continue
			}
			if !clip.Contains(toX, toY) {
				continue
			}
			var data [2]byte
			if err := memory.Read(source.Pixels+uint32(fromY)*source.Bpl+uint32(fromX)*2, data[:]); err != nil {
				return 0, err
			}
			address := destination.Pixels + uint32(toY)*destination.Bpl + uint32(toX)*2
			if err := referenceBlendPixel(runtime, op, address, binary.LittleEndian.Uint16(data[:])); err != nil {
				return 0, err
			}
		}
	}
	return 0, nil
}

// referenceBlendPixel and referenceApplyPixelOp are blendPixel and
// applyPixelOp as the walk called them, so the reference does not move with a
// later change to either.
func referenceBlendPixel(runtime *initializationRuntime, op wipicrt.WIPICPixelOp, address uint32, source uint16) error {
	memory := runtime.client.Core().Memory()
	value := source
	if op.Active() {
		var existing [2]byte
		if err := memory.Read(address, existing[:]); err != nil {
			return err
		}
		result, err := referenceApplyPixelOp(runtime, op, source, uint16(existing[0])|uint16(existing[1])<<8)
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

func referenceApplyPixelOp(runtime *initializationRuntime, op wipicrt.WIPICPixelOp, source, destination uint16) (uint16, error) {
	if op.Xor {
		return source ^ destination, nil
	}
	if !op.Active() {
		return source, nil
	}
	key := uint32(source)<<16 | uint32(destination)
	cache := runtime.PixelOps
	if cache == nil || cache.Function != op.Function || cache.Param != op.Param {
		cache = &wipicrt.PixelOpCache{Function: op.Function, Param: op.Param, Results: make(map[uint32]uint16)}
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
	summary, err := runtime.client.Core().Call(
		runtime.CurrentContext,
		thread,
		op.Function,
		wipicrt.ReturnAddress,
		[]uint32{uint32(source), uint32(destination), op.Param},
		runtime.handleSupervisorCall,
	)
	if err != nil {
		return 0, fmt.Errorf("run KTF pixel operation at %#x: %w", op.Function, err)
	}
	result := uint16(summary.Context.Registers[0])
	runtime.CountDiagnostic("pixel operation guest call")
	cache.Results[key] = result
	return result, nil
}

// loggingPixelOpWindow is how many bytes the logging operation's record spans:
// a count, then one word per call, the call's index wrapped to the window.
const loggingPixelOpWindow = 4096

// installLoggingPixelOperation writes an operation into the loaded module that
// records every call it gets and answers the sum of its two pixels, so the
// order and arguments of the guest calls a copy makes are left in guest memory
// to compare. The record at log holds the call count in its first word and each
// call's source | destination<<16 in word (count & 1023), so it never reaches
// past loggingPixelOpWindow bytes whatever the count word holds — and a test
// can put it inside a surface the copy is drawing, where the copy's own writes
// and the operation's meet.
//
//	ldr r3, =log; ldr r2, [r3]; adds r2, #1; str r2, [r3]
//	lsls r2, r2, #22; lsrs r2, r2, #20; adds r3, r3, r2
//	lsls r1, r1, #16; orrs r1, r0; str r1, [r3]
//	lsrs r1, r1, #16; adds r0, r0, r1; bx lr
func installLoggingPixelOperation(tb testing.TB, runtime *initializationRuntime, address, log uint32) uint32 {
	tb.Helper()
	body := []uint16{
		0x4b06, // ldr r3, [pc, #24]
		0x681a, // ldr r2, [r3]
		0x3201, // adds r2, #1
		0x601a, // str r2, [r3]
		0x0592, // lsls r2, r2, #22
		0x0d12, // lsrs r2, r2, #20
		0x189b, // adds r3, r3, r2
		0x0409, // lsls r1, r1, #16
		0x4301, // orrs r1, r0
		0x6019, // str r1, [r3]
		0x0c09, // lsrs r1, r1, #16
		0x1840, // adds r0, r0, r1
		0x4770, // bx lr
		0x46c0, // nop
	}
	data := make([]byte, len(body)*2+4)
	for index, instruction := range body {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	binary.LittleEndian.PutUint32(data[len(body)*2:], log)
	if err := runtime.client.Core().Memory().Write(address, data); err != nil {
		tb.Fatalf("write logging pixel operation at %#x: %v", address, err)
	}
	return address | 1
}

// writtenFramebufferRecord builds a frame buffer record by hand, the way a
// guest can: a handle naming a record whose fields name a pixel handle. It is
// how a test makes a view into another surface's pixels, or a line length this
// platform never allocates.
func writtenFramebufferRecord(tb testing.TB, runtime *initializationRuntime, width, height, bpl, pixels uint32) uint32 {
	tb.Helper()
	buffer, err := runtime.AllocateWords([]uint32{pixels - 8})
	if err != nil {
		tb.Fatal(err)
	}
	record, err := runtime.AllocateWords([]uint32{0, 0, width, height, bpl, 16, buffer})
	if err != nil {
		tb.Fatal(err)
	}
	handle, err := runtime.AllocateWords([]uint32{record})
	if err != nil {
		tb.Fatal(err)
	}
	return handle
}

// copyScene is the guest state a copy can change: the memory its surfaces and
// the logging operation's record live in, the transparency masks it was handed,
// and the pixel operation cache.
type copyScene struct {
	runtime *initializationRuntime
	regions [][2]uint32
	masks   []*wipicrt.ImageOpacity
}

type copySceneState struct {
	memory [][]byte
	masks  [][]bool
	cache  *wipicrt.PixelOpCache
	calls  uint32
}

func clonePixelOpCache(cache *wipicrt.PixelOpCache) *wipicrt.PixelOpCache {
	if cache == nil {
		return nil
	}
	return &wipicrt.PixelOpCache{Function: cache.Function, Param: cache.Param, Results: maps.Clone(cache.Results)}
}

func (scene copyScene) capture(tb testing.TB) copySceneState {
	tb.Helper()
	var state copySceneState
	for _, region := range scene.regions {
		data := make([]byte, region[1])
		if err := scene.runtime.client.Core().Memory().Read(region[0], data); err != nil {
			tb.Fatalf("read scene memory at %#x: %v", region[0], err)
		}
		state.memory = append(state.memory, data)
	}
	for _, mask := range scene.masks {
		state.masks = append(state.masks, slices.Clone(mask.Opaque))
	}
	state.cache = clonePixelOpCache(scene.runtime.PixelOps)
	state.calls = scene.runtime.CallCounts[wipicrt.DiagEvent{Text: "pixel operation guest call"}]
	return state
}

// restore puts the scene back. It loads rather than writes, because a fault
// test's scene includes a page the guest may only read.
func (scene copyScene) restore(tb testing.TB, state copySceneState) {
	tb.Helper()
	for index, region := range scene.regions {
		if err := scene.runtime.client.Core().Memory().Load(region[0], state.memory[index]); err != nil {
			tb.Fatalf("restore scene memory at %#x: %v", region[0], err)
		}
	}
	for index, mask := range scene.masks {
		copy(mask.Opaque, state.masks[index])
	}
	scene.runtime.PixelOps = clonePixelOpCache(state.cache)
}

// compare runs one copy through the walk and then, from the same state,
// through the copy under test, and fails on the first thing they left
// differently. It answers how many guest operation calls the walk made and the
// error it ended with, so a test can show which paths it reached.
func (scene copyScene) compare(tb testing.TB, label string, run func(walk bool) error) (uint32, error) {
	tb.Helper()
	initial := scene.capture(tb)
	wantErr := run(true)
	want := scene.capture(tb)
	scene.restore(tb, initial)
	gotErr := run(false)
	got := scene.capture(tb)
	if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		tb.Fatalf("%s: error %v, the walk's %v", label, gotErr, wantErr)
	}
	for index, region := range scene.regions {
		if !bytes.Equal(got.memory[index], want.memory[index]) {
			offset := 0
			for got.memory[index][offset] == want.memory[index][offset] {
				offset++
			}
			tb.Fatalf("%s: memory at %#x+%d is %#x, the walk left %#x", label, region[0], offset, got.memory[index][offset], want.memory[index][offset])
		}
	}
	for index := range want.masks {
		if !slices.Equal(got.masks[index], want.masks[index]) {
			tb.Fatalf("%s: transparency mask %d differs from the walk's", label, index)
		}
	}
	if (got.cache == nil) != (want.cache == nil) ||
		got.cache != nil && (got.cache.Function != want.cache.Function || got.cache.Param != want.cache.Param || !maps.Equal(got.cache.Results, want.cache.Results)) {
		tb.Fatalf("%s: pixel operation cache differs from the walk's", label)
	}
	if got.calls-want.calls != want.calls-initial.calls {
		tb.Fatalf("%s: %d guest operation calls, the walk made %d", label, got.calls-want.calls, want.calls-initial.calls)
	}
	return want.calls - initial.calls, wantErr
}

// copySurface is one surface the copy tests draw between, by handle for
// MC_grpCopyFrameBuffer and as the record it decodes to for the blits.
type copySurface struct {
	handle      uint32
	framebuffer wipicrt.WIPICFramebuffer
}

// copyFixture is a runtime with surfaces of every shape that matters to a row
// — an odd width, rows wider than a span, one-pixel rows, rows that cross a
// page, two views that alias the pixels of a third (one of them at an odd
// address with an odd line length), a padded line — and the two guest
// operations.
type copyFixture struct {
	runtime    *initializationRuntime
	surfaces   []copySurface
	scene      copyScene
	log        uint32
	keyOp      uint32
	logOp      uint32
	guestCalls int
}

// copyFixtureHost is the surface the views alias, and where a test can put
// the logging operation's record.
const copyFixtureHost = 3

func newCopyFixture(t *testing.T, runtime *initializationRuntime) *copyFixture {
	t.Helper()
	fixture := &copyFixture{runtime: runtime, scene: copyScene{runtime: runtime}}
	for _, size := range [][2]uint32{{37, 23}, {300, 6}, {1, 9}, {64, 40}, {17, 1}} {
		handle, framebuffer := gfxReviewSurface(t, runtime, size[0], size[1])
		fixture.surfaces = append(fixture.surfaces, copySurface{handle, framebuffer})
		fixture.scene.regions = append(fixture.scene.regions, [2]uint32{framebuffer.Pixels, framebuffer.Bpl * framebuffer.Height})
	}
	host := fixture.surfaces[copyFixtureHost].framebuffer
	padded, err := runtime.AllocateBytes(make([]byte, 44*11))
	if err != nil {
		t.Fatal(err)
	}
	fixture.scene.regions = append(fixture.scene.regions, [2]uint32{padded, 44 * 11})
	for _, view := range [][4]uint32{
		{30, 20, host.Bpl, host.Pixels + 3*host.Bpl + 10},
		{20, 30, 87, host.Pixels + 1},
		{19, 11, 44, padded},
	} {
		handle := writtenFramebufferRecord(t, runtime, view[0], view[1], view[2], view[3])
		framebuffer, err := runtime.ReadWIPICFramebuffer(handle)
		if err != nil {
			t.Fatal(err)
		}
		fixture.surfaces = append(fixture.surfaces, copySurface{handle, framebuffer})
	}
	fixture.log, err = runtime.AllocateBytes(make([]byte, loggingPixelOpWindow))
	if err != nil {
		t.Fatal(err)
	}
	fixture.scene.regions = append(fixture.scene.regions, [2]uint32{fixture.log, loggingPixelOpWindow})
	fixture.keyOp = installPixelOperation(t, runtime, wipicrt.ImageBase+0x40)
	fixture.logOp = installLoggingPixelOperation(t, runtime, wipicrt.ImageBase+0x60, fixture.log)
	return fixture
}

// fill gives every surface new pixels, drawn from palette when there is one so
// that a cached operation has pairs it has already answered.
func (fixture *copyFixture) fill(t *testing.T, random *rand.Rand, palette []uint16) {
	t.Helper()
	for _, region := range fixture.scene.regions {
		data := make([]byte, region[1])
		for index := 0; index+1 < len(data); index += 2 {
			pixel := uint16(random.Intn(0x10000))
			if palette != nil {
				pixel = palette[random.Intn(len(palette))]
			}
			binary.LittleEndian.PutUint16(data[index:], pixel)
		}
		if err := fixture.runtime.client.Core().Memory().Write(region[0], data); err != nil {
			t.Fatal(err)
		}
	}
}

// placeLog points the logging operation at its own record, or at one inside
// the host surface, and starts its count at zero.
func (fixture *copyFixture) placeLog(t *testing.T, random *rand.Rand, inside bool) {
	t.Helper()
	log := fixture.log
	if inside {
		host := fixture.surfaces[copyFixtureHost].framebuffer
		room := host.Bpl*host.Height - loggingPixelOpWindow
		log = (host.Pixels + uint32(random.Intn(int(room)-3)) + 3) &^ 3
	}
	installLoggingPixelOperation(t, fixture.runtime, wipicrt.ImageBase+0x60, log)
	writeGuestWords(t, fixture.runtime, log, 0)
}

// warmPixelOps hands the operation a cache that has already answered most of
// the pairs a palette can make, with answers of its own: the walk and the rows
// both have to take the cache's word for it.
func warmPixelOps(random *rand.Rand, op wipicrt.WIPICPixelOp, palette []uint16) *wipicrt.PixelOpCache {
	cache := &wipicrt.PixelOpCache{Function: op.Function, Param: op.Param, Results: make(map[uint32]uint16)}
	for _, source := range palette {
		for _, destination := range palette {
			if random.Intn(8) != 0 {
				cache.Results[wipicrt.PixelOpKey(source, destination)] = palette[random.Intn(len(palette))]
			}
		}
	}
	return cache
}

func randomPalette(random *rand.Rand) []uint16 {
	palette := make([]uint16, 3+random.Intn(4))
	for index := range palette {
		palette[index] = uint16(random.Intn(0x10000))
	}
	return palette
}

func randomOpacity(random *rand.Rand, framebuffer wipicrt.WIPICFramebuffer) *wipicrt.ImageOpacity {
	width, height := 1+random.Intn(int(framebuffer.Width)+6), 1+random.Intn(int(framebuffer.Height)+6)
	transparent := []int{1, 5, 9}[random.Intn(3)]
	mask := &wipicrt.ImageOpacity{Width: width, Height: height, Opaque: make([]bool, width*height)}
	for index := range mask.Opaque {
		mask.Opaque[index] = random.Intn(10) >= transparent
	}
	return mask
}

// randomClip answers a clip and the three context words that decode to it:
// none, a rectangle that may stick out of the surface or be inverted, or an
// explicitly empty one.
func randomClip(random *rand.Rand, framebuffer wipicrt.WIPICFramebuffer) (wipicrt.WIPICClip, [3]uint32) {
	width, height := int(framebuffer.Width), int(framebuffer.Height)
	var clip wipicrt.WIPICClip
	switch random.Intn(5) {
	case 0, 1:
		return wipicrt.WIPICClip{}, [3]uint32{}
	case 2, 3:
		left, top := random.Intn(width+8)-4, random.Intn(height+8)-4
		clip = wipicrt.WIPICClip{Enabled: true, Left: int32(left), Top: int32(top),
			Right: int32(left + random.Intn(width+8) - 2), Bottom: int32(top + random.Intn(height+8) - 2)}
	default:
		clip = wipicrt.WIPICClip{Enabled: random.Intn(2) == 0,
			Left: int32(random.Intn(width+4) - 2), Top: int32(random.Intn(height+4) - 2),
			Right: int32(random.Intn(width+4) - 2), Bottom: int32(random.Intn(height+4) - 2)}
	}
	enabled := uint32(0)
	if clip.Enabled {
		enabled = 1
	}
	return clip, [3]uint32{enabled,
		uint32(uint16(clip.Left)) | uint32(uint16(clip.Top))<<16,
		uint32(uint16(clip.Right)) | uint32(uint16(clip.Bottom))<<16}
}

// blit compares one wipicBlitClipped against the walk.
func (fixture *copyFixture) blit(t *testing.T, label string, destination wipicrt.WIPICFramebuffer, clip wipicrt.WIPICClip, op wipicrt.WIPICPixelOp, destinationX, destinationY, width, height int32, source wipicrt.WIPICFramebuffer, sourceX, sourceY int32, opacity wipicrt.BlitOpacity) {
	t.Helper()
	scene := fixture.scene
	for _, mask := range []*wipicrt.ImageOpacity{opacity.Source, opacity.Destination} {
		if mask != nil && !slices.Contains(scene.masks, mask) {
			scene.masks = append(scene.masks, mask)
		}
	}
	runtime := fixture.runtime
	fixture.tally(t, label)(scene.compare(t, label, func(walk bool) error {
		if walk {
			return referenceBlitClipped(runtime, destination, clip, op, destinationX, destinationY, width, height, source, sourceX, sourceY, opacity)
		}
		return runtime.WIPICBlitClipped(destination, clip, op, destinationX, destinationY, width, height, source, sourceX, sourceY, opacity)
	}))
}

// copyFramebuffer compares one MC_grpCopyFrameBuffer against the walk, with the
// clip and operation coming out of a graphics context the way the guest passes
// them.
func (fixture *copyFixture) copyFramebuffer(t *testing.T, label string, destination copySurface, clipWords [3]uint32, op wipicrt.WIPICPixelOp, destinationX, destinationY, width, height int32, source copySurface, sourceX, sourceY int32) {
	t.Helper()
	runtime := fixture.runtime
	context := gfxReviewContext(t, runtime, 0, clipWords)
	writeGuestWords(t, runtime, context+wipicrt.WIPICContextPixelOpOffset, op.Function, op.Param)
	thread := gfxReviewThread(t, runtime, destination.handle, uint32(destinationX), uint32(destinationY), uint32(width), uint32(height),
		source.handle, uint32(sourceX), uint32(sourceY), context)
	fixture.tally(t, label)(fixture.scene.compare(t, label, func(walk bool) error {
		var err error
		if walk {
			_, err = referenceCopyFramebuffer(runtime, thread)
		} else {
			_, err = runtime.WIPICCopyFramebuffer(thread)
		}
		return err
	}))
}

// tally counts the guest operation calls the fixture's copies made. Every
// fixture surface is a valid one, so none of them may fail.
func (fixture *copyFixture) tally(t *testing.T, label string) func(uint32, error) {
	return func(calls uint32, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		fixture.guestCalls += int(calls)
	}
}

// Randomized copies between every pair of the fixture's surfaces and inside
// each one, through both calls, with every kind of operation, cache state,
// transparency and clip.
func TestWIPICCopyRowsMatchThePixelWalk(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
	fixture := newCopyFixture(t, runtime)
	random := rand.New(rand.NewSource(242))
	for trial := 0; trial < 5000; trial++ {
		var palette []uint16
		if random.Intn(5) < 3 {
			palette = randomPalette(random)
		}
		fixture.fill(t, random, palette)
		if palette == nil {
			palette = randomPalette(random)
		}
		destination := fixture.surfaces[random.Intn(len(fixture.surfaces))]
		source := fixture.surfaces[random.Intn(len(fixture.surfaces))]
		if random.Intn(3) == 0 {
			source = destination
		}
		to, from := destination.framebuffer, source.framebuffer
		var destinationX, destinationY, sourceX, sourceY int32
		if random.Intn(2) == 0 {
			// Near the destination, so the two overlap when they are one
			// surface.
			destinationX, destinationY = int32(random.Intn(int(to.Width)+4)-2), int32(random.Intn(int(to.Height)+4)-2)
			sourceX, sourceY = destinationX+int32(random.Intn(9)-4), destinationY+int32(random.Intn(9)-4)
		} else {
			destinationX, destinationY = int32(random.Intn(int(to.Width)+16)-8), int32(random.Intn(int(to.Height)+16)-8)
			sourceX, sourceY = int32(random.Intn(int(from.Width)+16)-8), int32(random.Intn(int(from.Height)+16)-8)
		}
		width := int32(random.Intn(int(max(to.Width, from.Width))+10) - 2)
		height := int32(random.Intn(int(max(to.Height, from.Height))+10) - 2)
		throughContext := random.Intn(3) == 0

		var op wipicrt.WIPICPixelOp
		switch random.Intn(4) {
		case 1:
			if !throughContext {
				op = wipicrt.WIPICPixelOp{Xor: true} // the C context has no way to ask for it
			}
		case 2:
			op = wipicrt.WIPICPixelOp{Function: fixture.keyOp, Param: uint32(palette[random.Intn(len(palette))])}
		case 3:
			op = wipicrt.WIPICPixelOp{Function: fixture.logOp, Param: uint32(random.Intn(0x10000))}
			inside := random.Intn(3) == 0
			fixture.placeLog(t, random, inside)
		}
		switch random.Intn(3) {
		case 0:
			runtime.PixelOps = nil
		case 1:
			runtime.PixelOps = warmPixelOps(random, op, palette)
		default:
			runtime.PixelOps = warmPixelOps(random, wipicrt.WIPICPixelOp{Function: op.Function, Param: op.Param ^ 1}, palette)
		}
		clip, clipWords := randomClip(random, to)
		label := fmt.Sprintf("trial %d: %dx%d from (%d,%d) of %dx%d to (%d,%d) of %dx%d, op %+v, clip %+v",
			trial, width, height, sourceX, sourceY, from.Width, from.Height, destinationX, destinationY, to.Width, to.Height, op, clip)
		if throughContext {
			fixture.copyFramebuffer(t, label, destination, clipWords, op, destinationX, destinationY, width, height, source, sourceX, sourceY)
			continue
		}
		var opacity wipicrt.BlitOpacity
		switch random.Intn(5) {
		case 1:
			opacity.Source = randomOpacity(random, from)
		case 2:
			opacity.Destination = randomOpacity(random, to)
		case 3:
			opacity = wipicrt.BlitOpacity{Source: randomOpacity(random, from), Destination: randomOpacity(random, to)}
		case 4:
			mask := randomOpacity(random, to)
			opacity = wipicrt.BlitOpacity{Source: mask, Destination: mask}
		}
		fixture.blit(t, label, to, clip, op, destinationX, destinationY, width, height, from, sourceX, sourceY, opacity)
	}
	if fixture.guestCalls < 12000 {
		t.Fatalf("the copies asked the guest operations %d times; the trials no longer reach them", fixture.guestCalls)
	}
}

// Every way a copy inside one surface can overlap itself, for each kind of
// operation, with and without the surface's own transparency mask as both of
// the copy's, through both calls: the blit, which reads a snapshot, and
// MC_grpCopyFrameBuffer, which carries along what it has drawn.
func TestWIPICCopyRowsMatchThePixelWalkOnEveryOverlap(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
	fixture := newCopyFixture(t, runtime)
	random := rand.New(rand.NewSource(2420))
	palette := randomPalette(random)
	// The odd width, the rows wider than a span, the misaligned view and the
	// padded line.
	for _, index := range []int{0, 1, 6, 7} {
		surface := fixture.surfaces[index]
		framebuffer := surface.framebuffer
		width, height := int32(framebuffer.Width), int32(framebuffer.Height)
		destinationX, destinationY := int32(3), min(int32(3), height/3)
		for shiftY := int32(-3); shiftY <= 3; shiftY++ {
			for shiftX := int32(-3); shiftX <= 3; shiftX++ {
				for kind := 0; kind < 4; kind++ {
					fixture.fill(t, random, palette)
					var op wipicrt.WIPICPixelOp
					runtime.PixelOps = nil
					switch kind {
					case 1:
						op = wipicrt.WIPICPixelOp{Xor: true}
					case 2:
						op = wipicrt.WIPICPixelOp{Function: fixture.keyOp, Param: uint32(palette[0])}
						runtime.PixelOps = warmPixelOps(random, op, palette)
					case 3:
						op = wipicrt.WIPICPixelOp{Function: fixture.logOp, Param: 7}
						fixture.placeLog(t, random, index == 6)
					}
					label := fmt.Sprintf("surface %d shifted (%d,%d), op %+v", index, shiftX, shiftY, op)
					sourceX, sourceY := destinationX+shiftX, destinationY+shiftY
					copyWidth, copyHeight := width-6, height-2*destinationY
					fixture.blit(t, label, framebuffer, wipicrt.WIPICClip{}, op, destinationX, destinationY, copyWidth, copyHeight,
						framebuffer, sourceX, sourceY, wipicrt.BlitOpacity{})
					mask := randomOpacity(random, framebuffer)
					fixture.blit(t, label+" masked", framebuffer, wipicrt.WIPICClip{}, op, destinationX, destinationY, copyWidth, copyHeight,
						framebuffer, sourceX, sourceY, wipicrt.BlitOpacity{Source: mask, Destination: mask})
					if kind != 1 {
						fixture.copyFramebuffer(t, label+" copy frame buffer", surface, [3]uint32{}, op,
							destinationX, destinationY, copyWidth, copyHeight, surface, sourceX, sourceY)
					}
				}
			}
		}
	}
	if fixture.guestCalls < 30000 {
		t.Fatalf("the copies asked the guest operations %d times; the cases no longer reach them", fixture.guestCalls)
	}
}

// A copy whose surface record points at memory that does not read or write
// back fails, or does not, exactly where the walk did: the pixels before the
// fault are drawn and marked, and the error names the pixel that has it. The
// records are written by hand, the way only a guest can make them.
func TestWIPICCopyRowsMatchThePixelWalkAtFaults(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
	memory := runtime.client.Core().Memory()
	// A writable page, a page the guest may only read, a page that is not
	// there at all, and another writable page.
	const base = uint32(0x50000000)
	for _, mapping := range []struct {
		address    uint32
		permission armcore.Permission
	}{{base, armcore.PermissionReadWrite}, {base + 0x1000, armcore.PermissionRead}, {base + 0x3000, armcore.PermissionReadWrite}} {
		if err := memory.Map(mapping.address, 0x1000, mapping.permission); err != nil {
			t.Fatal(err)
		}
	}
	if memory.Write(base+0x1000, make([]byte, 2)) == nil || memory.Read(base+0x2000, make([]byte, 2)) == nil {
		t.Fatal("the fault pages are not the ones this test needs")
	}
	random := rand.New(rand.NewSource(24))
	pattern := make([]byte, 0x4000)
	random.Read(pattern)
	if err := memory.Load(base, pattern[:0x2000]); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(base+0x3000, pattern[0x3000:]); err != nil {
		t.Fatal(err)
	}
	_, wide := gfxReviewSurface(t, runtime, 0x900, 2)
	writeSurfacePixels(t, runtime, wide, randomSurfacePixels(random, wide.Width*wide.Height))
	keyOp := installPixelOperation(t, runtime, wipicrt.ImageBase+0x40)
	scene := copyScene{runtime: runtime, regions: [][2]uint32{
		{base, 0x2000}, {base + 0x3000, 0x1000}, {wide.Pixels, wide.Bpl * wide.Height},
	}}

	// Twenty pixels of each row's first line are writable and the rest only
	// readable; the same shape over the hole is unreadable for twenty pixels.
	writeFault := wipicrt.WIPICFramebuffer{Width: 64, Height: 2, Bpl: 128, Bpp: 16, Pixels: base + 0x1000 - 40}
	readFault := wipicrt.WIPICFramebuffer{Width: 64, Height: 2, Bpl: 128, Bpp: 16, Pixels: base + 0x3000 - 40}
	// One row that starts on the read-only page, crosses the hole, and ends
	// on the last writable page.
	acrossHole := wipicrt.WIPICFramebuffer{Width: 0x900, Height: 1, Bpl: 0x1200, Bpp: 16, Pixels: base + 0x2000 - 20}
	holeMask := &wipicrt.ImageOpacity{Width: 0x900, Height: 1, Opaque: make([]bool, 0x900)}
	for column := range holeMask.Opaque {
		holeMask.Opaque[column] = column < 10 || column >= 10+0x800
	}
	// With the last page of the address space and the first both mapped, the
	// walk's 32-bit sums carry a row that starts near the top on into the
	// bottom page. Where the copy's other surface lives there the two share
	// pixels, and the walk's order decides what each pixel reads.
	if memory.Read(0, make([]byte, 2)) == nil || memory.Read(0xfffff000, make([]byte, 2)) == nil {
		t.Fatal("the ends of the address space are already mapped")
	}
	for _, address := range []uint32{0, 0xfffff000} {
		if err := memory.Map(address, 0x1000, armcore.PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := memory.Load(address, pattern[:0x1000]); err != nil {
			t.Fatal(err)
		}
		scene.regions = append(scene.regions, [2]uint32{address, 0x1000})
	}
	nearTop := wipicrt.WIPICFramebuffer{Width: 200, Height: 1, Bpl: 400, Bpp: 16, Pixels: 0xffffff00}
	wrapping := wipicrt.WIPICFramebuffer{Width: 64, Height: 1, Bpl: 128, Bpp: 16, Pixels: 0xfffffff0}
	bottom := wipicrt.WIPICFramebuffer{Width: 64, Height: 1, Bpl: 128, Bpp: 16, Pixels: 4}

	// A cache that has answered every pair the first row of a write-fault copy
	// asks, so the row is worked out whole before the store that fails.
	answered := &wipicrt.PixelOpCache{Function: keyOp, Param: 0x2484, Results: make(map[uint32]uint16)}
	for column := uint32(0); column < 64; column++ {
		var sourcePixel, destinationPixel [2]byte
		memory.Read(wide.Pixels+column*2, sourcePixel[:])
		memory.Read(writeFault.Pixels+column*2, destinationPixel[:])
		key := wipicrt.PixelOpKey(binary.LittleEndian.Uint16(sourcePixel[:]), binary.LittleEndian.Uint16(destinationPixel[:]))
		answered.Results[key] = uint16(column * 0x0101)
	}
	marks := &wipicrt.ImageOpacity{Width: 64, Height: 2, Opaque: make([]bool, 128)}

	for _, test := range []struct {
		name                string
		destination, source wipicrt.WIPICFramebuffer
		clip                wipicrt.WIPICClip
		op                  wipicrt.WIPICPixelOp
		cache               *wipicrt.PixelOpCache
		opacity             wipicrt.BlitOpacity
		fails               bool
	}{
		{name: "store refused mid-row", destination: writeFault, source: wide, fails: true},
		{name: "store refused mid-row with XOR", destination: writeFault, source: wide, op: wipicrt.WIPICPixelOp{Xor: true}, fails: true},
		{name: "store refused after cached answers", destination: writeFault, source: wide, op: wipicrt.WIPICPixelOp{Function: keyOp, Param: 0x2484}, cache: answered, fails: true},
		{name: "store refused before any answer", destination: writeFault, source: wide, op: wipicrt.WIPICPixelOp{Function: keyOp, Param: 0x2484}, fails: true},
		{name: "store refused with marks", destination: writeFault, source: wide, opacity: wipicrt.BlitOpacity{Destination: marks}, fails: true},
		{name: "destination unreadable for XOR", destination: readFault, source: wide, op: wipicrt.WIPICPixelOp{Xor: true}, fails: true},
		{name: "destination unwritable", destination: readFault, source: wide, fails: true},
		{name: "unreadable pixels clipped away", destination: readFault, source: wide, op: wipicrt.WIPICPixelOp{Xor: true},
			clip: wipicrt.WIPICClip{Enabled: true, Left: 20, Top: 0, Right: 64, Bottom: 2}},
		{name: "source hole under transparency", destination: wide, source: acrossHole, opacity: wipicrt.BlitOpacity{Source: holeMask}},
		{name: "source hole drawn", destination: wide, source: acrossHole, fails: true},
		{name: "destination wrapping past the top", destination: nearTop, source: wide},
		{name: "source wrapping past the top", destination: wide, source: nearTop},
		{name: "destination wrapping onto its source", destination: wrapping, source: bottom},
		{name: "source wrapping onto its destination", destination: bottom, source: wrapping, op: wipicrt.WIPICPixelOp{Xor: true}},
	} {
		scene := scene
		for _, mask := range []*wipicrt.ImageOpacity{test.opacity.Source, test.opacity.Destination} {
			if mask != nil {
				scene.masks = append(scene.masks, mask)
			}
		}
		runtime.PixelOps = clonePixelOpCache(test.cache)
		_, err := scene.compare(t, test.name, func(walk bool) error {
			if walk {
				return referenceBlitClipped(runtime, test.destination, test.clip, test.op, 0, 0, int32(test.destination.Width), int32(test.destination.Height), test.source, 0, 0, test.opacity)
			}
			return runtime.WIPICBlitClipped(test.destination, test.clip, test.op, 0, 0, int32(test.destination.Width), int32(test.destination.Height), test.source, 0, 0, test.opacity)
		})
		if (err != nil) != test.fails {
			t.Errorf("%s: the walk ended with %v", test.name, err)
		}
	}
}

// A copy stores each run of drawn pixels once, and where reading the row whole
// could differ — an MC_grpCopyFrameBuffer inside one surface that moves right
// — it still stores a pixel at a time. A watched address tells the two apart:
// a host store reports up to four bytes of what it wrote from that address on.
func TestWIPICCopyStoresRunsOfDrawnPixels(t *testing.T) {
	_, runtime := newTestRuntime(t)
	core := runtime.client.Core()
	_, source := gfxReviewSurface(t, runtime, 8, 1)
	destinationHandle, destination := gfxReviewSurface(t, runtime, 8, 1)
	writeSurfacePixels(t, runtime, source, []uint16{1, 2, 3, 4, 5, 6, 7, 8})
	watched := destination.Pixels + 2
	storeWidth := func(t *testing.T, draw func() error) uint8 {
		t.Helper()
		core.ClearWatches()
		core.Watch(watched)
		defer core.ClearWatches()
		if err := draw(); err != nil {
			t.Fatal(err)
		}
		hits := core.WatchHits()
		if len(hits) != 1 || hits[0].Count != 1 || hits[0].Origin != armcore.OriginHost {
			t.Fatalf("watched pixel hits = %+v, want one host store", hits)
		}
		return hits[0].Size
	}
	blit := func(opacity wipicrt.BlitOpacity) func() error {
		return func() error {
			return runtime.WIPICBlitClipped(destination, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{}, 0, 0, 8, 1, source, 0, 0, opacity)
		}
	}
	if size := storeWidth(t, blit(wipicrt.BlitOpacity{})); size != 4 {
		t.Errorf("a plain copy stored the watched pixel %d bytes wide, want one store reaching past it", size)
	}
	gap := &wipicrt.ImageOpacity{Width: 8, Height: 1, Opaque: []bool{true, true, false, true, true, true, true, true}}
	if size := storeWidth(t, blit(wipicrt.BlitOpacity{Source: gap})); size != 2 {
		t.Errorf("a run that ends at the watched pixel stored %d bytes from it, want 2", size)
	}
	context := gfxReviewContext(t, runtime, 0, [3]uint32{})
	copyWithin := func(destinationX, sourceX uint32) func() error {
		return func() error {
			thread := gfxReviewThread(t, runtime, destinationHandle, destinationX, 0, 7, 1, destinationHandle, sourceX, 0, context)
			_, err := runtime.WIPICCopyFramebuffer(thread)
			return err
		}
	}
	if size := storeWidth(t, copyWithin(1, 0)); size != 2 {
		t.Errorf("a copy within one surface moving right stored %d bytes, want the walk's 2", size)
	}
	if size := storeWidth(t, copyWithin(0, 1)); size != 4 {
		t.Errorf("a copy within one surface moving left stored %d bytes, want one store reaching past the pixel", size)
	}
}

// BenchmarkWIPICBlitRows draws the same copies by rows and by the walk they
// replaced (the walk sub-benchmarks run referenceBlitClipped): one 32-pixel
// row of a sprite, a 32x32 sprite, the sprite with a quarter of it
// transparent, the sprite through a guest operation whose answers are all
// cached, and a whole 240x320 screen.
func BenchmarkWIPICBlitRows(b *testing.B) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		b.Fatal(err)
	}
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
	random := rand.New(rand.NewSource(1))
	surface := func(width, height uint32) wipicrt.WIPICFramebuffer {
		handle, err := runtime.NewWIPICFramebufferRecord(width, height)
		if err != nil {
			b.Fatal(err)
		}
		framebuffer, err := runtime.ReadWIPICFramebuffer(handle)
		if err != nil {
			b.Fatal(err)
		}
		pixels := make([]byte, framebuffer.Bpl*framebuffer.Height)
		random.Read(pixels)
		if err := runtime.client.Core().Memory().Write(framebuffer.Pixels, pixels); err != nil {
			b.Fatal(err)
		}
		return framebuffer
	}
	screen, backdrop, sprite := surface(240, 320), surface(240, 320), surface(32, 32)
	// A round sprite: the corners outside a circle are transparent.
	round := &wipicrt.ImageOpacity{Width: 32, Height: 32, Opaque: make([]bool, 32*32)}
	for index := range round.Opaque {
		x, y := index%32-16, index/32-16
		round.Opaque[index] = x*x+y*y < 18*18
	}
	// The colour-key operation, with every pair the sprite and the screen
	// make already answered.
	function := uint32(wipicrt.ImageBase + 0x40)
	for index, instruction := range []uint16{0x4290, 0xd000, 0x4770, 0x1c08, 0x4770} {
		var data [2]byte
		binary.LittleEndian.PutUint16(data[:], instruction)
		if err := runtime.client.Core().Memory().Write(function+uint32(index)*2, data[:]); err != nil {
			b.Fatal(err)
		}
	}
	keyed := wipicrt.WIPICPixelOp{Function: function | 1, Param: 0x07e0}

	for _, shape := range []struct {
		name                string
		destination, source wipicrt.WIPICFramebuffer
		op                  wipicrt.WIPICPixelOp
		opacity             wipicrt.BlitOpacity
		x, y                int32
		width, height       int32
	}{
		{name: "row32", destination: screen, source: sprite, x: 8, y: 8, width: 32, height: 1},
		{name: "sprite32x32", destination: screen, source: sprite, x: 8, y: 8, width: 32, height: 32},
		{name: "sprite32x32-transparent", destination: screen, source: sprite, opacity: wipicrt.BlitOpacity{Source: round}, x: 8, y: 8, width: 32, height: 32},
		{name: "sprite32x32-guest-op", destination: screen, source: sprite, op: keyed, x: 8, y: 8, width: 32, height: 32},
		{name: "screen240x320", destination: screen, source: backdrop, width: 240, height: 320},
	} {
		for _, walk := range []bool{true, false} {
			name := shape.name + "/rows"
			if walk {
				name = shape.name + "/walk"
			}
			b.Run(name, func(b *testing.B) {
				draw := runtime.WIPICBlitClipped
				if walk {
					draw = func(destination wipicrt.WIPICFramebuffer, clip wipicrt.WIPICClip, op wipicrt.WIPICPixelOp, destinationX, destinationY, width, height int32, source wipicrt.WIPICFramebuffer, sourceX, sourceY int32, opacity wipicrt.BlitOpacity) error {
						return referenceBlitClipped(runtime, destination, clip, op, destinationX, destinationY, width, height, source, sourceX, sourceY, opacity)
					}
				}
				// Two rounds settle what the copy leaves on the screen, so
				// every pair the operation is asked about after them is one
				// it has answered and only the copy is measured.
				for range 2 {
					if err := draw(shape.destination, wipicrt.WIPICClip{}, shape.op, shape.x, shape.y, shape.width, shape.height, shape.source, 0, 0, shape.opacity); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; index < b.N; index++ {
					if err := draw(shape.destination, wipicrt.WIPICClip{}, shape.op, shape.x, shape.y, shape.width, shape.height, shape.source, 0, 0, shape.opacity); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(shape.height), "ns/row")
			})
		}
	}
}
