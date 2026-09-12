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

func TestScriptSISCodedTiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		pixels []byte
	}{
		{
			"zero run 64",
			[]byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 219, 64, 0, 0},
			[]byte{0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			"one run 64",
			[]byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 244, 0, 0, 0},
			[]byte{255, 255, 255, 255, 255, 255, 255, 255},
		},
		{
			"zero 32 then one 32",
			[]byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 195, 96, 212, 128, 0, 0},
			[]byte{0, 1, 3, 7, 31, 63, 127, 255},
		},
		{
			"one 32 then zero 32",
			[]byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 212, 54, 128, 0, 0},
			[]byte{255, 254, 252, 248, 224, 192, 128, 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame, ok := decodeScriptSISLiteralFrame(tc.data, 0)
			if !ok || frame.width != 8 || frame.height != 8 || !bytes.Equal(frame.pixels, tc.pixels) {
				t.Fatalf("decoded %dx%d %x valid=%v, want 8x8 %x", frame.width, frame.height, frame.pixels, ok, tc.pixels)
			}
		})
	}
}

func TestScriptSISCodedTilesRejectOverflowAndTruncation(t *testing.T) {
	overflow := []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 198, 154, 0, 0, 0}
	truncatedCode := []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 192}
	for name, data := range map[string][]byte{
		"run overflow":   overflow,
		"truncated code": truncatedCode,
	} {
		if _, ok := decodeScriptSISLiteralFrame(data, 0); ok {
			t.Fatalf("accepted %s", name)
		}
		before := []byte("unchanged")
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
		vm.Push(1234)
		for _, argument := range []int16{1, 1, 0, 1, 0, 42, 43} {
			vm.Push(argument)
		}
		if err := (&ScriptSession{}).Call(0xe7, vm); err != nil || vm.Pop() != -1 || vm.Pop() != 1234 {
			t.Fatalf("%s dispatch failed: %v", name, err)
		}
		if !bytes.Equal(vm.Resources[1].Data, before) {
			t.Fatalf("%s mutated the destination", name)
		}
	}

	valid := []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 244, 0, 0, 0}
	for length := 0; length < len(valid); length++ {
		if _, ok := decodeScriptSISLiteralFrame(valid[:length], 0); ok {
			t.Fatalf("accepted %d-byte prefix of coded stream", length)
		}
	}
}

func TestScriptImageFrameMaximumCodedReferenceWorkIsChargedBeforeDecode(t *testing.T) {
	data := authoredSISMaximumCodedReferences()
	frame, ok := decodeScriptSISLiteralFrame(data, 0)
	if !ok || frame.width != 31*8 || frame.height != 15*8 || !bytes.Equal(frame.pixels, make([]byte, 31*15*8)) {
		t.Fatal("maximum coded reference fixture did not decode successfully")
	}
	before := []byte("unchanged")
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
	charged := scriptSISLiteralDecodeWork(data)
	if err := vm.ChargeWork(sgsvm.MaxSteps - charged + 1); err != nil {
		t.Fatal(err)
	}
	for _, argument := range []int16{1, 1, 0, 1, 0, 0, 0} {
		vm.Push(argument)
	}
	if err := scriptImageFrameCall(vm); err == nil {
		t.Fatal("maximum coded reference fan-out bypassed the work limit")
	}
	if !bytes.Equal(vm.Resources[1].Data, before) {
		t.Fatal("work-limited coded reference extraction mutated destination")
	}
}

func authoredSISMaximumCodedReferences() []byte {
	var bits scriptSISBits
	bits.append(1, 5)
	bits.append(0, 5)
	bits.append(31, 5)
	bits.append(15, 4)
	bits.append(0, 1)
	bits.append(19, 5)
	bits.append(0, 3)
	bits.append(0, 1)
	bits.append(0, 4)
	bits.append(1, 3)
	bits.append(0, 4)
	bits.append(31, 5)
	bits.append(12, 4)
	for range 31 * 12 {
		bits.append(1, 1)
	}
	for range 31 * 12 {
		bits.append(0, 1)
		bits.append(0b11011, 5)
	}
	for range 19 {
		bits.append(0, 10)
	}
	bits.append(0, 1)
	for range 20 {
		bits.append(1, 1)
	}
	for range 20 {
		bits.append(0, 8)
		bits.append(0, 7)
		bits.append(0, 4)
	}
	return bits.bytes()
}

func TestScriptSISRunCodesArePrefixFree(t *testing.T) {
	for color, codes := range scriptSISRunCodes {
		for run, code := range codes {
			if code.length < 2 || code.length > 12 || code.bits >= uint16(1)<<code.length {
				t.Fatalf("color %d run %d has invalid code %+v", color, run+1, code)
			}
			for otherRun, other := range codes {
				if run == otherRun || code.length > other.length {
					continue
				}
				if other.bits>>(other.length-code.length) == code.bits {
					t.Fatalf("color %d run %d prefixes run %d", color, run+1, otherRun+1)
				}
			}
		}
	}
}

func TestLocalScriptSISCodebookComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_CODEBOOK_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_CODEBOOK_COMPARISON to an authored native-result JSONL file")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type nativeCodeword struct {
		Length      int    `json:"length"`
		Bits        string `json:"bits"`
		Result      int    `json:"result"`
		Run         int    `json:"run"`
		OutputColor int    `json:"output_color"`
	}
	type nativeRow struct {
		Color             int              `json:"color"`
		ValidPrefixes     int              `json:"valid_prefixes"`
		InvalidLookaheads int              `json:"invalid_lookaheads"`
		ConsumedLengths   []int            `json:"consumed_lengths"`
		Results           []int            `json:"results"`
		Codewords         []nativeCodeword `json:"codewords"`
	}
	wantInvalid := [2]int{560, 50}
	wantLengths := [2][]int{{4, 5, 6, 7, 8}, {2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}}
	seenColors := [2]bool{}
	scanner := bufio.NewScanner(file)
	for rowNumber := 1; scanner.Scan(); rowNumber++ {
		var row nativeRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("row %d: %v", rowNumber, err)
		}
		if row.Color < 0 || row.Color > 1 || seenColors[row.Color] {
			t.Fatalf("row %d has unexpected or duplicate color %d", rowNumber, row.Color)
		}
		seenColors[row.Color] = true
		wantResults := make([]int, 64)
		for run := 1; run <= 64; run++ {
			wantResults[run-1] = run<<1 | row.Color
		}
		if row.ValidPrefixes != 64 || row.InvalidLookaheads != wantInvalid[row.Color] || !slices.Equal(row.ConsumedLengths, wantLengths[row.Color]) || !slices.Equal(row.Results, wantResults) || len(row.Codewords) != 64 {
			t.Fatalf("row %d has invalid exhaustive summary: %+v", rowNumber, row)
		}
		seenRuns := make([]bool, 65)
		nativePrefixes := make(map[uint16]int)
		for _, native := range row.Codewords {
			if native.Run < 1 || native.Run > 64 || seenRuns[native.Run] || native.Length != len(native.Bits) || native.Result != native.Run<<1|row.Color || native.OutputColor != row.Color {
				t.Fatalf("row %d has invalid or duplicate codeword %+v", rowNumber, native)
			}
			seenRuns[native.Run] = true
			bits, ok := scriptSISCodedTestBits(native.Bits)
			if !ok {
				t.Fatalf("row %d has non-binary codeword %+v", rowNumber, native)
			}
			want := scriptSISRunCodes[row.Color][native.Run-1]
			if want.length != uint8(native.Length) || want.bits != bits {
				t.Fatalf("color %d run %d code {%b,%d}, native {%s,%d}", row.Color, native.Run, want.bits, want.length, native.Bits, native.Length)
			}
			nativePrefixes[uint16(1)<<native.Length|bits] = native.Run
			reader := scriptSISBitReader{data: scriptSISCodedTestPacked(native.Bits)}
			run, ok := decodeScriptSISRun(&reader, uint(row.Color))
			if !ok || run != native.Run || reader.position != native.Length {
				t.Fatalf("color %d native run %d decoded as %d valid=%v at %d", row.Color, native.Run, run, ok, reader.position)
			}
		}
		for run := 1; run <= 64; run++ {
			if !seenRuns[run] {
				t.Fatalf("row %d omitted run %d", rowNumber, run)
			}
		}
		invalid := 0
		seenPrefixes := make(map[uint16]bool)
		for lookahead := 0; lookahead < 1<<12; lookahead++ {
			reader := scriptSISBitReader{data: []byte{byte(lookahead >> 4), byte(lookahead << 4)}}
			run, ok := decodeScriptSISRun(&reader, uint(row.Color))
			if !ok {
				invalid++
				continue
			}
			prefix := uint16(1)<<reader.position | uint16(lookahead>>(12-reader.position))
			if nativePrefixes[prefix] != run {
				t.Fatalf("color %d lookahead %012b decoded as unexpected run %d at %d bits", row.Color, lookahead, run, reader.position)
			}
			seenPrefixes[prefix] = true
		}
		if invalid != row.InvalidLookaheads || len(seenPrefixes) != row.ValidPrefixes {
			t.Fatalf("color %d exhaustive decode found %d invalid lookaheads and %d prefixes, want %d and %d", row.Color, invalid, len(seenPrefixes), row.InvalidLookaheads, row.ValidPrefixes)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !seenColors[0] || !seenColors[1] {
		t.Fatalf("native comparison covered colors %v", seenColors)
	}
}

func scriptSISCodedTestBits(value string) (uint16, bool) {
	var bits uint16
	for _, bit := range value {
		bits <<= 1
		switch bit {
		case '0':
		case '1':
			bits |= 1
		default:
			return 0, false
		}
	}
	return bits, true
}

func scriptSISCodedTestPacked(value string) []byte {
	var bits scriptSISBits
	for _, bit := range value {
		bits.append(uint(bit-'0'), 1)
	}
	return bits.bytes()[3:]
}

func TestLocalScriptSISCodedComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_CODED_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_CODED_COMPARISON to an authored native-result JSONL file")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type nativeRow struct {
		Name         string   `json:"name"`
		InitialColor int      `json:"initial_color"`
		Codes        []string `json:"codes"`
		Result       int      `json:"result"`
		Finished     bool     `json:"finished"`
		BitPosition  int      `json:"bit_position"`
		Output       []byte   `json:"output"`
		Data         []byte   `json:"data"`
	}
	type expectation struct {
		initialColor int
		codes        []string
		result       int
		bitPosition  int
		data         []byte
		output       []byte
	}
	expected := map[string]expectation{
		"zero-64":        {0, []string{"11011"}, 1, 77, []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 219, 64, 0, 0}, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		"one-64":         {1, []string{"000001111"}, 1, 81, []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 244, 0, 0, 0}, []byte{255, 255, 255, 255, 255, 255, 255, 255}},
		"zero-32-one-32": {0, []string{"00011011", "000001101010"}, 1, 92, []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 195, 96, 212, 128, 0, 0}, []byte{0, 1, 3, 7, 31, 63, 127, 255}},
		"one-32-zero-32": {1, []string{"000001101010", "00011011"}, 1, 92, []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 212, 54, 128, 0, 0}, []byte{255, 254, 252, 248, 224, 192, 128, 0}},
		"run-overflow":   {0, []string{"00110100", "11"}, 0, 59, []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 198, 154, 0, 0, 0}, bytes.Repeat([]byte{0xa5}, 8)},
		"truncated-code": {1, []string{"0000011"}, 0, 51, []byte{83, 73, 83, 8, 2, 32, 0, 16, 8, 224, 192}, bytes.Repeat([]byte{0xa5}, 8)},
	}
	seen := make(map[string]bool)
	unique := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for rowNumber := 1; scanner.Scan(); rowNumber++ {
		var row nativeRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("row %d: %v", rowNumber, err)
		}
		want, exists := expected[row.Name]
		if !exists || seen[row.Name] || unique[string(row.Data)] {
			t.Fatalf("row %d has unexpected or duplicate case %q", rowNumber, row.Name)
		}
		seen[row.Name] = true
		unique[string(row.Data)] = true
		if !row.Finished || row.InitialColor != want.initialColor || !slices.Equal(row.Codes, want.codes) || row.Result != want.result || row.BitPosition != want.bitPosition || !bytes.Equal(row.Data, want.data) || !bytes.Equal(row.Output, want.output) {
			t.Fatalf("row %d has invalid native result: %+v", rowNumber, row)
		}
		frame, ok := decodeScriptSISLiteralFrame(row.Data, 0)
		if row.Result == 1 {
			if !ok || !bytes.Equal(frame.pixels, row.Output) {
				t.Fatalf("row %d: Go output %x valid=%v, native output %x", rowNumber, frame.pixels, ok, row.Output)
			}
		} else if ok {
			t.Fatalf("row %d: Go accepted native failure", rowNumber)
		}

		before := []byte("old")
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: row.Data}, {Data: slices.Clone(before)}}}, nil)
		vm.Push(1234)
		for _, argument := range []int16{1, 1, 0, 1, 0, 42, 43} {
			vm.Push(argument)
		}
		if err := (&ScriptSession{}).Call(0xe7, vm); err != nil {
			t.Fatalf("row %d dispatch failed: %v", rowNumber, err)
		}
		if row.Result == 1 {
			if vm.Pop() != 0 || vm.Pop() != 1234 {
				t.Fatalf("row %d dispatch result or caller stack changed", rowNumber)
			}
			wantResource := make([]byte, frame.width*frame.height+1)
			copy(wantResource, row.Output)
			if !bytes.Equal(vm.Resources[1].Data, wantResource) {
				t.Fatalf("row %d dispatch output differs", rowNumber)
			}
		} else {
			if vm.Pop() != -1 || vm.Pop() != 1234 {
				t.Fatalf("row %d failure result or caller stack changed", rowNumber)
			}
			if !bytes.Equal(vm.Resources[1].Data, before) {
				t.Fatalf("row %d failure mutated destination", rowNumber)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(expected) {
		t.Fatalf("compared %d native vectors, want %d", len(seen), len(expected))
	}
}
