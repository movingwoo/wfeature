package skt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

type authoredSISCompositionPlacement struct {
	x                int
	y                int
	pass             uint
	mirrorVertical   bool
	mirrorHorizontal bool
	rotateLeft       bool
	trailingFlag     bool
	extra            uint
}

type authoredSISCompositionFrame struct {
	invert     bool
	placements []*authoredSISCompositionPlacement
}

func authoredSISCompositionData(canvasWidth, canvasHeight int, headerInvert bool, variant uint, objects []scriptSISLiteralObject, frames []authoredSISCompositionFrame) []byte {
	var bits scriptSISBits
	bits.append(uint(len(frames)), 5)
	bits.append(0, 5)
	bits.append(uint(canvasWidth/8), 5)
	bits.append(uint(canvasHeight/8), 4)
	bits.append(scriptSISBoolBit(headerInvert), 1)
	bits.append(uint(len(objects)-1), 5)
	bits.append(0, 3)
	bits.append(0, 1)
	bits.append(0, 4)
	bits.append(variant, 3)
	bits.append(0, 4)
	for _, object := range objects {
		columns := object.width / 8
		rows := object.height / 8
		bits.append(uint(columns), 5)
		bits.append(uint(rows), 4)
		for range columns * rows {
			bits.append(0, 1)
		}
		for tile := 0; tile < columns*rows; tile++ {
			for encodedPosition := range 64 {
				x, y := scriptSISLiteralPosition(encodedPosition)
				x += tile % columns * 8
				y += tile / columns * 8
				bits.append(scriptSISBoolBit(scriptSISObjectPixel(object, x, y)), 1)
			}
		}
	}
	for _, frame := range frames {
		bits.append(scriptSISBoolBit(frame.invert), 1)
		for _, placement := range frame.placements {
			bits.append(scriptSISBoolBit(placement != nil), 1)
		}
		for _, placement := range frame.placements {
			if placement == nil {
				continue
			}
			if variant != 1 {
				bits.append(placement.pass, 3)
			}
			bits.append(scriptSISTestSignedMagnitude(placement.x, 8), 8)
			bits.append(scriptSISTestSignedMagnitude(placement.y, 7), 7)
			bits.append(scriptSISBoolBit(placement.mirrorVertical), 1)
			bits.append(scriptSISBoolBit(placement.mirrorHorizontal), 1)
			bits.append(scriptSISBoolBit(placement.rotateLeft), 1)
			bits.append(scriptSISBoolBit(placement.trailingFlag), 1)
			if placement.trailingFlag {
				bits.append(placement.extra, 2)
			}
		}
	}
	return bits.bytes()
}

func scriptSISBoolBit(value bool) uint {
	if value {
		return 1
	}
	return 0
}

func authoredSISCompositionObject(width, height int, coordinates ...[2]int) scriptSISLiteralObject {
	object := scriptSISLiteralObject{width: width, height: height, pixels: make([]byte, width/8*height)}
	for _, coordinate := range coordinates {
		x, y := coordinate[0], coordinate[1]
		object.pixels[y*(width/8)+x/8] |= 0x80 >> (x & 7)
	}
	return object
}

func TestScriptSISCompositionTransformsAndClipping(t *testing.T) {
	base := authoredSISCompositionObject(16, 8,
		[2]int{0, 0}, [2]int{1, 0}, [2]int{2, 0}, [2]int{8, 1},
		[2]int{9, 2}, [2]int{0, 3}, [2]int{15, 7},
	)
	for _, tc := range []struct {
		name      string
		placement authoredSISCompositionPlacement
		want      [][2]int
	}{
		{
			"none", authoredSISCompositionPlacement{x: 3, y: 4},
			[][2]int{{3, 4}, {4, 4}, {5, 4}, {11, 5}, {12, 6}, {3, 7}, {18, 11}},
		},
		{
			"vertical", authoredSISCompositionPlacement{x: 3, y: 4, mirrorVertical: true},
			[][2]int{{18, 4}, {3, 8}, {12, 9}, {11, 10}, {3, 11}, {4, 11}, {5, 11}},
		},
		{
			"horizontal", authoredSISCompositionPlacement{x: 3, y: 4, mirrorHorizontal: true},
			[][2]int{{16, 4}, {17, 4}, {18, 4}, {10, 5}, {9, 6}, {18, 7}, {3, 11}},
		},
		{
			"rotate left", authoredSISCompositionPlacement{x: 3, y: 4, rotateLeft: true},
			[][2]int{{10, 4}, {5, 10}, {4, 11}, {3, 17}, {3, 18}, {3, 19}, {6, 19}},
		},
		{
			"rotate and both mirrors", authoredSISCompositionPlacement{x: 3, y: 4, mirrorVertical: true, mirrorHorizontal: true, rotateLeft: true},
			[][2]int{{7, 4}, {10, 4}, {10, 5}, {10, 6}, {9, 12}, {8, 13}, {3, 19}},
		},
		{
			"negative clipped vertical", authoredSISCompositionPlacement{x: -5, y: -4, mirrorVertical: true},
			[][2]int{{4, 1}, {3, 2}},
		},
		{
			"negative clipped horizontal", authoredSISCompositionPlacement{x: -5, y: -4, mirrorHorizontal: true},
			nil,
		},
		{
			"negative clipped rotation", authoredSISCompositionPlacement{x: -5, y: -4, mirrorVertical: true, mirrorHorizontal: true, rotateLeft: true},
			[][2]int{{1, 4}, {0, 5}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			placement := tc.placement
			data := authoredSISCompositionData(24, 24, false, 1, []scriptSISLiteralObject{base}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{&placement}}})
			frame, ok := decodeScriptSISLiteralFrame(data, 0)
			if !ok {
				t.Fatal("composition was rejected")
			}
			assertScriptSISFramePixels(t, frame, tc.want)
		})
	}
}

func TestScriptSISCanvasAndFrameInversion(t *testing.T) {
	object := authoredSISCompositionObject(8, 8, [2]int{0, 0})
	placement := &authoredSISCompositionPlacement{x: 8, y: 8}
	frames := []authoredSISCompositionFrame{
		{placements: []*authoredSISCompositionPlacement{placement}},
		{invert: true, placements: []*authoredSISCompositionPlacement{placement}},
	}
	data := authoredSISCompositionData(16, 16, false, 1, []scriptSISLiteralObject{object}, frames)
	normal, ok := decodeScriptSISLiteralFrame(data, 0)
	if !ok {
		t.Fatal("normal frame was rejected")
	}
	inverted, ok := decodeScriptSISLiteralFrame(data, 1)
	if !ok {
		t.Fatal("inverted frame was rejected")
	}
	for index := range normal.pixels {
		if inverted.pixels[index] != ^normal.pixels[index] {
			t.Fatalf("byte %d was not inverted across the complete canvas", index)
		}
	}

	headerData := authoredSISCompositionData(16, 16, true, 1, []scriptSISLiteralObject{object}, frames)
	headerOnly, ok := decodeScriptSISLiteralFrame(headerData, 0)
	if !ok || !bytes.Equal(headerOnly.pixels, inverted.pixels) {
		t.Fatal("header inversion did not match frame inversion")
	}
	double, ok := decodeScriptSISLiteralFrame(headerData, 1)
	if !ok || !bytes.Equal(double.pixels, normal.pixels) {
		t.Fatal("header and frame inversion did not cancel")
	}

	emptyData := authoredSISCompositionData(16, 16, true, 1, []scriptSISLiteralObject{object}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{nil}}})
	empty, ok := decodeScriptSISLiteralFrame(emptyData, 0)
	if !ok || !bytes.Equal(empty.pixels, bytes.Repeat([]byte{0xff}, len(empty.pixels))) {
		t.Fatal("header inversion did not cover an empty canvas")
	}
}

func TestScriptSISOrderedCompositionPasses(t *testing.T) {
	background := authoredSISCompositionObject(16, 8)
	for index := range background.pixels {
		background.pixels[index] = 0xff
	}
	hollow := authoredSISCompositionObject(16, 8, [2]int{0, 0}, [2]int{15, 0}, [2]int{0, 7}, [2]int{15, 7})
	first := &authoredSISCompositionPlacement{pass: 0}
	second := &authoredSISCompositionPlacement{pass: 1}
	data := authoredSISCompositionData(16, 8, false, 2, []scriptSISLiteralObject{background, hollow}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{first, second}}})
	frame, ok := decodeScriptSISLiteralFrame(data, 0)
	want := append([]byte{0x80, 0x01}, bytes.Repeat([]byte{0xff}, 14)...)
	if !ok || !bytes.Equal(frame.pixels, want) {
		t.Fatalf("later pass output %x valid=%v, want %x", frame.pixels, ok, want)
	}

	second.x = -4
	clippedData := authoredSISCompositionData(16, 8, false, 2, []scriptSISLiteralObject{background, hollow}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{first, second}}})
	clipped, ok := decodeScriptSISLiteralFrame(clippedData, 0)
	clippedWant := append([]byte{0x00, 0x1f}, bytes.Repeat([]byte{0xff}, 14)...)
	if !ok || !bytes.Equal(clipped.pixels, clippedWant) {
		t.Fatalf("clipped later pass output %x valid=%v, want %x", clipped.pixels, ok, clippedWant)
	}

	outside := &authoredSISCompositionPlacement{pass: 2}
	ignoredData := authoredSISCompositionData(16, 8, false, 2, []scriptSISLiteralObject{hollow}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{outside}}})
	ignored, ok := decodeScriptSISLiteralFrame(ignoredData, 0)
	if !ok || !bytes.Equal(ignored.pixels, make([]byte, len(ignored.pixels))) {
		t.Fatal("pass selector outside the declared variant was rendered")
	}
}

func TestScriptSISTrailingCompositionFieldIsConsumed(t *testing.T) {
	object := authoredSISCompositionObject(8, 8, [2]int{0, 0}, [2]int{7, 7})
	var want []byte
	for extra := uint(0); extra < 4; extra++ {
		placement := &authoredSISCompositionPlacement{trailingFlag: true, extra: extra}
		data := authoredSISCompositionData(8, 8, false, 1, []scriptSISLiteralObject{object}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{placement}}})
		frame, ok := decodeScriptSISLiteralFrame(data, 0)
		if !ok {
			t.Fatalf("extra value %d was rejected", extra)
		}
		if extra == 0 {
			want = slices.Clone(frame.pixels)
		} else if !bytes.Equal(frame.pixels, want) {
			t.Fatalf("extra value %d changed output to %x, want %x", extra, frame.pixels, want)
		}
	}
}

func assertScriptSISFramePixels(t *testing.T, frame scriptSISLiteralFrame, want [][2]int) {
	t.Helper()
	wantPixels := make(map[[2]int]bool, len(want))
	for _, coordinate := range want {
		wantPixels[coordinate] = true
	}
	for y := 0; y < frame.height; y++ {
		for x := 0; x < frame.width; x++ {
			coordinate := [2]int{x, y}
			if scriptSISPackedPixel(frame.pixels, frame.width, x, y) != wantPixels[coordinate] {
				t.Fatalf("pixel (%d,%d) differs", x, y)
			}
		}
	}
}

func TestLocalScriptSISCompositionComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_COMPOSITION_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_COMPOSITION_COMPARISON to an authored native-result JSONL file")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type nativeRow struct {
		Name        string `json:"name"`
		FrameIndex  int    `json:"frame_index"`
		Data        []byte `json:"data"`
		Output      []byte `json:"output"`
		Result      int    `json:"result"`
		Finished    bool   `json:"finished"`
		BitPosition int    `json:"bit_position"`
	}
	type expectation struct {
		frameIndex   int
		result       int
		bitPosition  int
		dataLength   int
		outputLength int
		reject       bool
	}
	expected := map[string]expectation{
		"inversion-header0-frame0":          {0, 1, 200, 28, 72, false},
		"inversion-header1-frame0":          {0, 1, 200, 28, 72, false},
		"inversion-header0-frame1":          {0, 1, 200, 28, 72, false},
		"inversion-header1-frame1":          {0, 1, 200, 28, 72, false},
		"inversion-empty-frame":             {0, 1, 181, 26, 72, false},
		"frame-snapshot-normal":             {0, 2, 221, 31, 72, false},
		"frame-snapshot-inverted":           {1, 2, 221, 31, 72, false},
		"transform-000":                     {0, 1, 200, 28, 72, false},
		"transform-001":                     {0, 1, 200, 28, 72, false},
		"transform-010":                     {0, 1, 200, 28, 72, false},
		"transform-011":                     {0, 1, 200, 28, 72, false},
		"transform-100":                     {0, 1, 200, 28, 72, false},
		"transform-101":                     {0, 1, 200, 28, 72, false},
		"transform-110":                     {0, 1, 200, 28, 72, false},
		"transform-111":                     {0, 1, 200, 28, 72, false},
		"negative-clip-vertical":            {0, 1, 200, 28, 72, false},
		"negative-clip-horizontal":          {0, 1, 200, 28, 72, false},
		"negative-clip-rotate-and-both":     {0, 1, 200, 28, 72, false},
		"special-extra-0":                   {0, 1, 361, 49, 16, false},
		"special-extra-1":                   {0, 1, 361, 49, 16, false},
		"special-extra-2":                   {0, 1, 361, 49, 16, false},
		"special-extra-3":                   {0, 1, 361, 49, 16, false},
		"variant-0-pass-0":                  {0, 1, 203, 29, 72, false},
		"variant-0-pass-7":                  {0, 1, 203, 29, 72, false},
		"variant-2-pass-0":                  {0, 1, 203, 29, 72, false},
		"variant-2-pass-1":                  {0, 1, 203, 29, 72, false},
		"variant-2-pass-2":                  {0, 1, 203, 29, 72, false},
		"variant-2-pass-7":                  {0, 1, 203, 29, 72, false},
		"variant-3-pass-0":                  {0, 1, 203, 29, 72, false},
		"variant-3-pass-2":                  {0, 1, 203, 29, 72, false},
		"variant-3-pass-3":                  {0, 1, 203, 29, 72, false},
		"variant-3-pass-7":                  {0, 1, 203, 29, 72, false},
		"variant-7-pass-0":                  {0, 1, 203, 29, 72, false},
		"variant-7-pass-6":                  {0, 1, 203, 29, 72, false},
		"variant-7-pass-7":                  {0, 1, 203, 29, 72, false},
		"later-pass-replaces-shape":         {0, 1, 365, 49, 16, false},
		"later-pass-clears-background":      {0, 1, 365, 49, 16, false},
		"later-pass-without-earlier-object": {0, 1, 343, 46, 16, false},
		"later-pass-masked-write":           {0, 1, 365, 49, 16, false},
		"later-pass-masked-write-clipped":   {0, 1, 365, 49, 16, false},
		"header-bit28":                      {0, 1, 200, 28, 72, true},
		"header-flags-1":                    {0, 1, 200, 28, 72, true},
		"header-flags-2":                    {0, 1, 200, 28, 72, true},
		"header-flags-4":                    {0, 1, 200, 28, 72, true},
		"header-flags-8":                    {0, 1, 200, 28, 72, true},
		"header-flags-15":                   {0, 1, 200, 28, 72, true},
	}
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for rowNumber := 1; scanner.Scan(); rowNumber++ {
		var row nativeRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("row %d: %v", rowNumber, err)
		}
		want, exists := expected[row.Name]
		if !exists || seen[row.Name] {
			t.Fatalf("row %d has unexpected or duplicate case %q", rowNumber, row.Name)
		}
		seen[row.Name] = true
		if !row.Finished || row.FrameIndex != want.frameIndex || row.Result != want.result || row.BitPosition != want.bitPosition || len(row.Data) != want.dataLength || len(row.Output) != want.outputLength {
			t.Fatalf("row %d has invalid native result: %+v", rowNumber, row)
		}
		frame, ok := decodeScriptSISLiteralFrame(row.Data, row.FrameIndex)
		if want.reject {
			if ok {
				t.Fatalf("row %d accepted a deliberately unsupported header field", rowNumber)
			}
			before := []byte("unchanged")
			vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: row.Data}, {Data: slices.Clone(before)}}}, nil)
			vm.Push(1234)
			for _, argument := range []int16{1, 1, 0, 1, int16(row.FrameIndex), 42, 43} {
				vm.Push(argument)
			}
			if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != -1 || vm.Pop() != 1234 {
				t.Fatalf("row %d unsupported dispatch failed: %v", rowNumber, err)
			}
			if !bytes.Equal(vm.Resources[1].Data, before) {
				t.Fatalf("row %d unsupported dispatch mutated the destination", rowNumber)
			}
			continue
		}
		if !ok || !bytes.Equal(frame.pixels, row.Output) {
			t.Fatalf("row %d: Go output %x valid=%v, native output %x", rowNumber, frame.pixels, ok, row.Output)
		}
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: row.Data}, {Data: []byte("old")}}}, nil)
		vm.Push(1234)
		for _, argument := range []int16{1, 1, 0, 1, int16(row.FrameIndex), 42, 43} {
			vm.Push(argument)
		}
		if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != 0 || vm.Pop() != 1234 {
			t.Fatalf("row %d dispatch failed: %v", rowNumber, err)
		}
		wantResource := make([]byte, frame.width*frame.height+1)
		copy(wantResource, row.Output)
		if !bytes.Equal(vm.Resources[1].Data, wantResource) {
			t.Fatalf("row %d dispatch output differs", rowNumber)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(expected) {
		t.Fatalf("compared %d native vectors, want %d", len(seen), len(expected))
	}
}

func TestScriptSISCompositionFailuresAreAtomic(t *testing.T) {
	valid := authoredSISLiteral(authoredSISLiteralOptions{})
	object := authoredSISCompositionObject(8, 8, [2]int{0, 0})
	placement := &authoredSISCompositionPlacement{trailingFlag: true, extra: 3}
	trailing := authoredSISCompositionData(8, 8, false, 1, []scriptSISLiteralObject{object}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{placement}}})
	for name, data := range map[string][]byte{
		"record":         valid[:len(valid)-1],
		"trailing field": trailing[:len(trailing)-1],
	} {
		t.Run(name, func(t *testing.T) {
			before := []byte("unchanged")
			vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
			vm.Push(1234)
			for _, argument := range []int16{1, 1, 0, 1, 0, 42, 43} {
				vm.Push(argument)
			}
			if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != -1 || vm.Pop() != 1234 {
				t.Fatalf("truncated composition dispatch failed: %v", err)
			}
			if !bytes.Equal(vm.Resources[1].Data, before) {
				t.Fatal("truncated composition mutated the destination")
			}
		})
	}
}
