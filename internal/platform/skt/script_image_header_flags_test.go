package skt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptSISRemainingHeaderFieldsAreExtractionInert(t *testing.T) {
	asymmetric := authoredSISCompositionObject(16, 8,
		[2]int{0, 0}, [2]int{1, 0}, [2]int{2, 0}, [2]int{8, 1},
		[2]int{9, 2}, [2]int{0, 3}, [2]int{15, 7},
	)
	transformed := &authoredSISCompositionPlacement{pass: 2, x: 3, y: 4, mirrorVertical: true, rotateLeft: true}
	nonsquare := authoredSISCompositionData(24, 24, false, 3, []scriptSISLiteralObject{asymmetric}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{transformed}}})

	background := authoredSISCompositionObject(16, 8)
	for index := range background.pixels {
		background.pixels[index] = 0xff
	}
	hollow := authoredSISCompositionObject(16, 8, [2]int{0, 0}, [2]int{15, 0}, [2]int{0, 7}, [2]int{15, 7})
	first := &authoredSISCompositionPlacement{pass: 0}
	second := &authoredSISCompositionPlacement{pass: 1, x: -4}
	overlap := authoredSISCompositionData(16, 8, false, 2, []scriptSISLiteralObject{background, hollow}, []authoredSISCompositionFrame{{placements: []*authoredSISCompositionPlacement{first, second}}})

	// These are authored streams already covered by the permanent replacement
	// and coded-tile tests. Reusing them keeps this test focused on the header.
	reference := []byte{83, 73, 83, 8, 4, 33, 160, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 8, 255, 255, 255, 255, 255, 255, 255, 255, 192, 21, 255, 255, 255, 255, 255, 255, 255, 255, 128, 1, 0, 0, 0}
	coded := []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 195, 96, 212, 128, 0, 0}

	for name, base := range map[string][]byte{
		"nonsquare transform": nonsquare,
		"masked overlap":      overlap,
		"reference chain":     reference,
		"coded tile":          coded,
	} {
		t.Run(name, func(t *testing.T) {
			want, ok := decodeScriptSISLiteralFrame(base, 0)
			if !ok {
				t.Fatal("baseline was rejected")
			}
			for bit28 := 0; bit28 < 2; bit28++ {
				for flags := 0; flags < 16; flags++ {
					data := scriptSISTestHeaderFields(base, bit28, flags)
					frame, ok := decodeScriptSISLiteralFrame(data, 0)
					if !ok || frame.width != want.width || frame.height != want.height || !bytes.Equal(frame.pixels, want.pixels) {
						t.Fatalf("bit28=%d flags=%x decoded %dx%d %x valid=%v, want %dx%d %x", bit28, flags, frame.width, frame.height, frame.pixels, ok, want.width, want.height, want.pixels)
					}
					vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: []byte("old")}}}, nil)
					vm.Push(1234)
					for _, argument := range []int16{1, 1, 0, 1, 0, 42, 43} {
						vm.Push(argument)
					}
					if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != 0 || vm.Pop() != 1234 {
						t.Fatalf("bit28=%d flags=%x dispatch failed: %v", bit28, flags, err)
					}
					wantResource := make([]byte, want.width*want.height+1)
					copy(wantResource, want.pixels)
					if !bytes.Equal(vm.Resources[1].Data, wantResource) {
						t.Fatalf("bit28=%d flags=%x dispatch output differs", bit28, flags)
					}
				}
			}
		})
	}

	aliased := scriptSISTestHeaderFields(reference, 1, 15)
	want, ok := decodeScriptSISLiteralFrame(aliased, 0)
	if !ok {
		t.Fatal("aliased fixture was rejected")
	}
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: slices.Clone(aliased)}}}, nil)
	for _, argument := range []int16{1, 1, 0, 0, 0, 42, 43} {
		vm.Push(argument)
	}
	if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != 0 {
		t.Fatalf("aliased extraction failed: %v", err)
	}
	if !bytes.Equal(vm.Resources[0].Data[:len(want.pixels)], want.pixels) || !bytes.Equal(vm.Resources[0].Data[len(want.pixels):], make([]byte, len(vm.Resources[0].Data)-len(want.pixels))) {
		t.Fatal("aliased extraction did not retain the decoded pixels and zeroed tail")
	}
}

func scriptSISTestHeaderFields(data []byte, bit28, flags int) []byte {
	result := slices.Clone(data)
	set := func(position, value int) {
		index := 3 + position/8
		mask := byte(0x80 >> (position & 7))
		result[index] &^= mask
		if value != 0 {
			result[index] |= mask
		}
	}
	set(28, bit28)
	for position := 36; position < 40; position++ {
		set(position, flags>>(39-position)&1)
	}
	return result
}

func TestLocalScriptSISRemainingHeaderComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_REMAINING_HEADER_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_REMAINING_HEADER_COMPARISON to an authored native-result JSONL file")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type nativeRow struct {
		Name             string `json:"name"`
		Family           string `json:"family"`
		HeaderBit28      int    `json:"header_bit28"`
		HeaderFlags      int    `json:"header_flags"`
		StoredBit28      int    `json:"stored_bit28"`
		StoredFlags      int    `json:"stored_flags"`
		StoredExportFlag int    `json:"stored_export_flag"`
		Result           int    `json:"result"`
		Finished         bool   `json:"finished"`
		BitPosition      int    `json:"bit_position"`
		Output           []byte `json:"output"`
		Data             []byte `json:"data"`
	}
	type familyShape struct {
		position     int
		dataLength   int
		outputLength int
	}
	families := map[string]familyShape{
		"nonsquare": {203, 29, 72},
		"overlap":   {365, 49, 16},
		"reference": {338, 46, 48},
		"coded":     {95, 15, 32},
	}
	seen := make(map[string]bool, 128)
	scanner := bufio.NewScanner(file)
	for rowNumber := 1; scanner.Scan(); rowNumber++ {
		var row nativeRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("row %d: %v", rowNumber, err)
		}
		shape, exists := families[row.Family]
		key := fmt.Sprintf("%s-%d-%d", row.Family, row.HeaderBit28, row.HeaderFlags)
		if !exists || row.Name != fmt.Sprintf("%s-b%d-f%02x", row.Family, row.HeaderBit28, row.HeaderFlags) || seen[key] || row.HeaderBit28 < 0 || row.HeaderBit28 > 1 || row.HeaderFlags < 0 || row.HeaderFlags > 15 {
			t.Fatalf("row %d is unexpected or duplicated: %+v", rowNumber, row)
		}
		seen[key] = true
		if !row.Finished || row.Result != 1 || row.StoredBit28 != row.HeaderBit28 || row.StoredFlags != row.HeaderFlags || row.StoredExportFlag != row.HeaderFlags>>3 || row.BitPosition != shape.position || len(row.Data) != shape.dataLength || len(row.Output) != shape.outputLength {
			t.Fatalf("row %d has invalid native result: %+v", rowNumber, row)
		}
		frame, ok := decodeScriptSISLiteralFrame(row.Data, 0)
		if !ok || !bytes.Equal(frame.pixels, row.Output) {
			t.Fatalf("row %d: Go output %x valid=%v, native output %x", rowNumber, frame.pixels, ok, row.Output)
		}
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: row.Data}, {Data: []byte("old")}}}, nil)
		vm.Push(1234)
		for _, argument := range []int16{1, 1, 0, 1, 0, 42, 43} {
			vm.Push(argument)
		}
		if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != 0 || vm.Pop() != 1234 {
			t.Fatalf("row %d dispatch failed: %v", rowNumber, err)
		}
		wantResource := make([]byte, len(row.Output)*8+1)
		copy(wantResource, row.Output)
		if !bytes.Equal(vm.Resources[1].Data, wantResource) {
			t.Fatalf("row %d dispatch output differs", rowNumber)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 128 {
		t.Fatalf("compared %d unique native vectors, want 128", len(seen))
	}
}
