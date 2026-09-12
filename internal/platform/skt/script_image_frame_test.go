package skt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

type scriptSISBits []byte

func (b *scriptSISBits) append(value uint, width int) {
	for shift := width - 1; shift >= 0; shift-- {
		*b = append(*b, byte(value>>shift&1))
	}
}

func (b scriptSISBits) bytes() []byte {
	data := make([]byte, 3+(len(b)+7)/8)
	copy(data, "SIS")
	for position, bit := range b {
		data[3+position/8] |= bit << (7 - position%8)
	}
	return data
}

type authoredSISLiteralOptions struct {
	x          int
	y          int
	pixels     [64]byte
	coded      bool
	frameFlag  uint
	transforms uint
	invert     uint
	objects    uint
}

func authoredSISLiteral(options authoredSISLiteralOptions) []byte {
	objects := options.objects
	if objects == 0 {
		objects = 1
	}
	var bits scriptSISBits
	bits.append(1, 5)
	bits.append(0, 5)
	bits.append(2, 5)
	bits.append(2, 4)
	bits.append(options.invert, 1)
	bits.append(objects-1, 5)
	bits.append(0, 3)
	bits.append(0, 1)
	bits.append(0, 4)
	bits.append(1, 3)
	bits.append(0, 4)
	bits.append(1, 5)
	bits.append(1, 4)
	if options.coded {
		bits.append(1, 1)
	} else {
		bits.append(0, 1)
	}
	for _, pixel := range options.pixels {
		bits.append(uint(pixel), 1)
	}
	if objects > 1 {
		bits.append(0, 8) // A reference-object prefix, deliberately unsupported.
	}
	bits.append(options.frameFlag, 1)
	for object := uint(0); object < objects; object++ {
		bits.append(1, 1)
	}
	bits.append(scriptSISTestSignedMagnitude(options.x, 8), 8)
	bits.append(scriptSISTestSignedMagnitude(options.y, 7), 7)
	bits.append(options.transforms, 4)
	if options.transforms&1 != 0 {
		bits.append(0, 2)
	}
	return bits.bytes()
}

func scriptSISTestSignedMagnitude(value, width int) uint {
	if value < 0 {
		return uint(1<<(width-1) | -value)
	}
	return uint(value)
}

func TestScriptSISLiteralDiagonalAndPlacement(t *testing.T) {
	for _, tc := range []struct {
		encoded int
		x       int
		y       int
	}{
		{0, 0, 0}, {1, 1, 0}, {2, 0, 1}, {5, 2, 0},
		{8, 1, 2}, {9, 0, 3}, {63, 7, 7},
	} {
		var pixels [64]byte
		pixels[tc.encoded] = 1
		frame, ok := decodeScriptSISLiteralFrame(authoredSISLiteral(authoredSISLiteralOptions{pixels: pixels}), 0)
		if !ok {
			t.Fatalf("encoded position %d was rejected", tc.encoded)
		}
		for y := 0; y < frame.height; y++ {
			for x := 0; x < frame.width; x++ {
				want := x == tc.x && y == tc.y
				if got := scriptSISTestPixel(frame.pixels, frame.width, x, y); got != want {
					t.Fatalf("encoded position %d pixel (%d,%d): got %v want %v", tc.encoded, x, y, got, want)
				}
			}
		}
	}

	var full [64]byte
	for i := range full {
		full[i] = 1
	}
	for _, offset := range []struct{ x, y int }{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}, {7, 7}, {8, 8}} {
		frame, ok := decodeScriptSISLiteralFrame(authoredSISLiteral(authoredSISLiteralOptions{x: offset.x, y: offset.y, pixels: full}), 0)
		if !ok {
			t.Fatalf("placement (%d,%d) was rejected", offset.x, offset.y)
		}
		for y := 0; y < frame.height; y++ {
			for x := 0; x < frame.width; x++ {
				want := x >= offset.x && x < offset.x+8 && y >= offset.y && y < offset.y+8
				if got := scriptSISTestPixel(frame.pixels, frame.width, x, y); got != want {
					t.Fatalf("placement (%d,%d) pixel (%d,%d): got %v want %v", offset.x, offset.y, x, y, got, want)
				}
			}
		}
	}
}

func TestScriptSISLiteralMultipleTilesObjectsAndFrames(t *testing.T) {
	twoTiles := make([]byte, 48)
	twoTiles[3] = 0x40
	twoTiles[26] = 0x80
	twoObjects := make([]byte, 32)
	twoObjects[0] = 0xe0
	twoFrames := make([]byte, 32)
	for y := 8; y < 16; y++ {
		twoFrames[y*2+1] = 0xff
	}
	for _, tc := range []struct {
		name       string
		data       []byte
		frameIndex int
		width      int
		height     int
		pixels     []byte
	}{
		{
			"two tiles",
			[]byte{83, 73, 83, 8, 6, 64, 0, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 40, 8, 16},
			0, 24, 16, twoTiles,
		},
		{
			"two overlapping objects",
			[]byte{83, 73, 83, 8, 4, 64, 128, 16, 8, 176, 0, 0, 0, 0, 0, 0, 0, 2, 44, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 64, 0},
			0, 16, 16, twoObjects,
		},
		{
			"second of two frames",
			[]byte{83, 73, 83, 16, 4, 64, 0, 16, 8, 191, 255, 255, 255, 255, 255, 255, 255, 208, 0, 0, 132, 8, 0},
			1, 16, 16, twoFrames,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame, ok := decodeScriptSISLiteralFrame(tc.data, tc.frameIndex)
			if !ok || frame.width != tc.width || frame.height != tc.height || !bytes.Equal(frame.pixels, tc.pixels) {
				t.Fatalf("decoded %dx%d %x valid=%v, want %dx%d %x", frame.width, frame.height, frame.pixels, ok, tc.width, tc.height, tc.pixels)
			}
		})
	}
}

func scriptSISTestPixel(pixels []byte, width, x, y int) bool {
	return pixels[y*(width/8)+x/8]&(0x80>>(x&7)) != 0
}

func TestScriptImageFrameInstruction(t *testing.T) {
	var pixels [64]byte
	pixels[0] = 1
	program := &sgsvm.Program{
		Data: []byte{0, 5, 1, 5, 1, 5, 0, 5, 1, 5, 0, 5, 0, 5, 0, 0xe7, 0xff},
		Resources: []sgsvm.Resource{
			{Data: authoredSISLiteral(authoredSISLiteralOptions{pixels: pixels})},
			{},
		},
	}
	vm := sgsvm.New(program, &ScriptSession{})
	if err := vm.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if vm.Pop() != 0 || len(vm.Resources[1].Data) != 256 || vm.Resources[1].Data[0] != 0x80 {
		t.Fatal("frame instruction did not extract the authored pixel")
	}
}

func TestScriptSISLiteralRejectsUnsupportedAndTruncatedStreams(t *testing.T) {
	var onePixel [64]byte
	onePixel[0] = 1
	valid := authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel})
	for length := 0; length < len(valid); length++ {
		if _, ok := decodeScriptSISLiteralFrame(valid[:length], 0); ok {
			t.Fatalf("accepted %d-byte prefix of %d-byte fixture", length, len(valid))
		}
	}
	for name, data := range map[string][]byte{
		"coded tile":       authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel, coded: true}),
		"reference object": authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel, objects: 2}),
		"frame mask":       authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel, frameFlag: 1}),
		"transform":        authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel, transforms: 8}),
		"inversion":        authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel, invert: 1}),
	} {
		if _, ok := decodeScriptSISLiteralFrame(data, 0); ok {
			t.Fatalf("accepted unsupported %s", name)
		}
	}
	if _, ok := decodeScriptSISLiteralFrame(valid, -1); ok {
		t.Fatal("accepted negative frame index")
	}
	if _, ok := decodeScriptSISLiteralFrame(valid, 1); ok {
		t.Fatal("accepted frame index equal to frame count")
	}
}

func TestScriptImageFrameWritesPackedPrefixAndClearsRequestedSpan(t *testing.T) {
	var full [64]byte
	for i := range full {
		full[i] = 1
	}
	source := authoredSISLiteral(authoredSISLiteralOptions{x: 1, y: 1, pixels: full})
	destination := bytes.Repeat([]byte{0xa5}, 16*16+8)
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: source}, {Data: destination}}}, nil)
	vm.Push(71)
	for _, argument := range []int16{1, 1, 0, 1, 0, 91, 92} {
		vm.Push(argument)
	}
	if err := scriptImageFrameCall(vm); err != nil {
		t.Fatal(err)
	}
	if vm.Pop() != 0 || vm.Pop() != 71 {
		t.Fatal("frame extraction changed result or caller stack")
	}
	frame, _ := decodeScriptSISLiteralFrame(source, 0)
	got := vm.Resources[1].Data
	if !bytes.Equal(got[:len(frame.pixels)], frame.pixels) {
		t.Fatalf("packed prefix %x want %x", got[:len(frame.pixels)], frame.pixels)
	}
	if !bytes.Equal(got[len(frame.pixels):16*16], make([]byte, 16*16-len(frame.pixels))) {
		t.Fatal("requested destination span was not cleared")
	}
	if !bytes.Equal(got[16*16:], bytes.Repeat([]byte{0xa5}, 8)) {
		t.Fatal("allocator slack beyond the requested span was overwritten")
	}
}

func TestScriptImageFrameAliasingAndFailuresAreAtomic(t *testing.T) {
	var onePixel [64]byte
	onePixel[0] = 1
	data := authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel})
	want, _ := decodeScriptSISLiteralFrame(data, 0)
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: slices.Clone(data)}}}, nil)
	for _, argument := range []int16{1, 1, 0, 0, 0, 0, 0} {
		vm.Push(argument)
	}
	if err := scriptImageFrameCall(vm); err != nil || vm.Pop() != 0 {
		t.Fatalf("aliased extraction failed: %v", err)
	}
	if len(vm.Resources[0].Data) != 16*16 || !bytes.Equal(vm.Resources[0].Data[:len(want.pixels)], want.pixels) {
		t.Fatal("aliased extraction did not retain decoded pixels")
	}

	for _, tc := range []struct {
		name string
		args []int16
		data []byte
		work bool
	}{
		{"selector", []int16{1, 2, 0, 1, 0, 0, 0}, data, false},
		{"format", []int16{1, 1, 0, 1, 0, 0, 0}, []byte("invalid"), false},
		{"frame", []int16{1, 1, 0, 1, 1, 0, 0}, data, false},
		{"work", []int16{1, 1, 0, 1, 0, 0, 0}, data, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := []byte("unchanged")
			vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: tc.data}, {Data: slices.Clone(before)}}}, nil)
			if tc.work {
				if err := vm.ChargeWork(sgsvm.MaxSteps); err != nil {
					t.Fatal(err)
				}
			}
			vm.Push(71)
			for _, argument := range tc.args {
				vm.Push(argument)
			}
			err := scriptImageFrameCall(vm)
			if tc.work {
				if err == nil {
					t.Fatal("work limit did not reject extraction")
				}
			} else if err != nil || vm.Pop() != -1 || vm.Pop() != 71 {
				t.Fatalf("unsupported extraction result: %v", err)
			}
			if !bytes.Equal(vm.Resources[1].Data, before) {
				t.Fatal("failed extraction mutated destination")
			}
		})
	}
}

func TestScriptImageFrameValidatesOperandsAndResourceRangesBeforeMutation(t *testing.T) {
	var onePixel [64]byte
	onePixel[0] = 1
	data := authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel})
	for operands := 0; operands < 7; operands++ {
		before := []byte("unchanged")
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
		for range operands {
			vm.Push(71)
		}
		if err := scriptImageFrameCall(vm); err == nil {
			t.Fatalf("accepted %d operands", operands)
		}
		if !bytes.Equal(vm.Resources[1].Data, before) {
			t.Fatalf("%d-operand call mutated destination", operands)
		}
	}
	for _, arguments := range [][]int16{
		{1, 1, -1, 1, 0, 0, 0},
		{1, 1, 0, 2, 0, 0, 0},
	} {
		before := []byte("unchanged")
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
		for _, argument := range arguments {
			vm.Push(argument)
		}
		if err := scriptImageFrameCall(vm); err == nil {
			t.Fatalf("accepted resource range %v", arguments)
		}
		if !bytes.Equal(vm.Resources[1].Data, before) {
			t.Fatalf("resource range %v mutated destination", arguments)
		}
	}
}

func TestScriptImageFrameLateFailureConsumesWorkWithoutMutation(t *testing.T) {
	var onePixel [64]byte
	onePixel[63] = 1
	data := authoredSISLiteral(authoredSISLiteralOptions{pixels: onePixel, transforms: 8})
	before := []byte("unchanged")
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
	charged := scriptSISLiteralDecodeWork(data)
	if err := vm.ChargeWork(sgsvm.MaxSteps - 2*charged + 1); err != nil {
		t.Fatal(err)
	}
	arguments := []int16{1, 1, 0, 1, 0, 0, 0}
	for _, argument := range arguments {
		vm.Push(argument)
	}
	if err := scriptImageFrameCall(vm); err != nil || vm.Pop() != -1 {
		t.Fatalf("first late format failure returned %v", err)
	}
	for _, argument := range arguments {
		vm.Push(argument)
	}
	if err := scriptImageFrameCall(vm); err == nil {
		t.Fatal("repeated late format failure bypassed the work limit")
	}
	if !bytes.Equal(vm.Resources[1].Data, before) {
		t.Fatal("work-limited format failures mutated destination")
	}
}

func TestLocalScriptSISLiteralComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_LITERAL_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_LITERAL_COMPARISON to an authored native-result JSONL file")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type nativeRow struct {
		Name        string `json:"name"`
		X           int    `json:"x"`
		Y           int    `json:"y"`
		FrameIndex  int    `json:"frame_index"`
		Data        []byte `json:"data"`
		Output      []byte `json:"output"`
		Result      int    `json:"result"`
		Finished    bool   `json:"finished"`
		BitPosition int    `json:"bit_position"`
	}
	rows := 0
	unique := make(map[string]bool)
	type nativeCase struct {
		name string
		x    int
		y    int
	}
	type nativeExpectation struct {
		frameIndex   int
		result       int
		bitPosition  int
		dataLength   int
		outputLength int
	}
	base := nativeExpectation{result: 1, bitPosition: 135, dataLength: 20, outputLength: 32}
	expected := map[nativeCase]nativeExpectation{
		{"bit-0", 0, 0}:               base,
		{"bit-1", 0, 0}:               base,
		{"bit-2", 0, 0}:               base,
		{"bit-5", 0, 0}:               base,
		{"bit-8", 0, 0}:               base,
		{"bit-9", 0, 0}:               base,
		{"bit-63", 0, 0}:              base,
		{"placement", 0, 0}:           base,
		{"placement", 1, 0}:           base,
		{"placement", -1, 0}:          base,
		{"placement", 0, 1}:           base,
		{"placement", 0, -1}:          base,
		{"placement", 7, 7}:           base,
		{"placement", 8, 8}:           base,
		{"two-tiles", 1, 1}:           {result: 1, bitPosition: 200, dataLength: 28, outputLength: 48},
		{"two-objects-overlap", 1, 0}: {result: 1, bitPosition: 229, dataLength: 32, outputLength: 32},
		{"two-frames", 8, 8}:          {frameIndex: 1, result: 2, bitPosition: 156, dataLength: 23, outputLength: 32},
	}
	seenCases := make(map[nativeCase]bool)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row nativeRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("row %d: %v", rows+1, err)
		}
		if unique[string(row.Data)] {
			t.Fatalf("row %d duplicates an earlier stream", rows+1)
		}
		unique[string(row.Data)] = true
		key := nativeCase{row.Name, row.X, row.Y}
		want, exists := expected[key]
		if !exists || seenCases[key] {
			t.Fatalf("row %d has unexpected or duplicate case %+v", rows+1, key)
		}
		seenCases[key] = true
		if !row.Finished || row.FrameIndex != want.frameIndex || row.Result != want.result || row.BitPosition != want.bitPosition {
			t.Fatalf("row %d has invalid native execution status: %+v", rows+1, row)
		}
		if len(row.Data) != want.dataLength || len(row.Output) != want.outputLength {
			t.Fatalf("row %d has stream/output lengths %d/%d, want %d/%d", rows+1, len(row.Data), len(row.Output), want.dataLength, want.outputLength)
		}
		frame, ok := decodeScriptSISLiteralFrame(row.Data, row.FrameIndex)
		if !ok || !bytes.Equal(frame.pixels, row.Output) {
			t.Fatalf("row %d: Go output %x valid=%v, native output %x", rows+1, frame.pixels, ok, row.Output)
		}
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: row.Data}, {Data: []byte("old")}}}, nil)
		vm.Push(1234)
		for _, argument := range []int16{1, 1, 0, 1, int16(row.FrameIndex), 42, 43} {
			vm.Push(argument)
		}
		if err := (&ScriptSession{}).Call(0xe7, vm); err != nil {
			t.Fatalf("row %d dispatch: %v", rows+1, err)
		}
		if vm.Pop() != 0 || vm.Pop() != 1234 {
			t.Fatalf("row %d dispatch result or caller stack changed", rows+1)
		}
		// Growing the three-byte bank rounds the allocation delta to an even
		// number, retaining one extra byte beyond the requested pixel span.
		wantResource := make([]byte, frame.width*frame.height+1)
		copy(wantResource, row.Output)
		if !bytes.Equal(vm.Resources[1].Data, wantResource) {
			t.Fatalf("row %d dispatch packed prefix or zero tail differs", rows+1)
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows != len(expected) {
		t.Fatalf("compared %d native vectors, want %d", rows, len(expected))
	}
}
