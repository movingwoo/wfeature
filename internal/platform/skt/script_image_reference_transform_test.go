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

func TestScriptSISReferenceTransforms(t *testing.T) {
	all := bytes.Repeat([]byte{0xff}, 16)
	for _, tc := range []struct {
		name   string
		data   []byte
		pixels []byte
	}{
		{
			"empty width zero",
			[]byte{83, 73, 83, 8, 2, 32, 128, 16, 8, 160, 0, 0, 0, 0, 0, 0, 0, 0, 18, 0, 0, 0},
			[]byte{0x80, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			"replace only tile",
			[]byte{83, 73, 83, 8, 2, 32, 144, 16, 8, 160, 0, 0, 0, 0, 0, 0, 0, 0, 19, 255, 255, 255, 255, 255, 255, 255, 254, 64, 0, 0},
			bytes.Repeat([]byte{0xff}, 8),
		},
		{
			"replace second tile",
			[]byte{83, 73, 83, 8, 4, 32, 160, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 10, 255, 255, 255, 255, 255, 255, 255, 255, 200, 0, 0},
			[]byte{0x80, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff},
		},
		{
			"reverse replacement order",
			[]byte{83, 73, 83, 8, 4, 32, 160, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 10, 0, 0, 0, 0, 0, 0, 0, 0, 31, 255, 255, 255, 255, 255, 255, 255, 249, 0, 0, 0},
			[]byte{0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0},
		},
		{
			"coded replacement",
			[]byte{83, 73, 83, 8, 4, 32, 160, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 11, 131, 242, 0, 0, 0},
			[]byte{0x80, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff},
		},
		{
			"transform chain",
			[]byte{83, 73, 83, 8, 4, 33, 32, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 8, 255, 255, 255, 255, 255, 255, 255, 255, 192, 21, 255, 255, 255, 255, 255, 255, 255, 255, 136, 0, 0},
			all,
		},
		{
			"exact after transform chain",
			[]byte{83, 73, 83, 8, 4, 33, 160, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 8, 255, 255, 255, 255, 255, 255, 255, 255, 192, 21, 255, 255, 255, 255, 255, 255, 255, 255, 128, 1, 0, 0, 0},
			all,
		},
		{
			"duplicate last wins",
			[]byte{83, 73, 83, 8, 2, 32, 160, 16, 8, 160, 0, 0, 0, 0, 0, 0, 0, 0, 17, 255, 255, 255, 255, 255, 255, 255, 254, 0, 0, 0, 0, 0, 0, 0, 0, 50, 0, 0, 0},
			make([]byte, 8),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame, ok := decodeScriptSISLiteralFrame(tc.data, 0)
			if !ok || !bytes.Equal(frame.pixels, tc.pixels) {
				t.Fatalf("decoded %x valid=%v, want %x", frame.pixels, ok, tc.pixels)
			}
		})
	}
}

func TestScriptSISReferenceTransformSnapshots(t *testing.T) {
	data := []byte{83, 73, 83, 24, 4, 33, 32, 16, 16, 144, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 8, 255, 255, 255, 255, 255, 255, 255, 255, 192, 21, 255, 255, 255, 255, 255, 255, 255, 255, 160, 0, 0, 32, 0, 0, 32, 0, 0}
	want := [][]byte{
		{0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0, 0xff, 1},
		bytes.Repeat([]byte{0xff}, 16),
	}
	for frameIndex := range want {
		frame, ok := decodeScriptSISLiteralFrame(data, frameIndex)
		if !ok || !bytes.Equal(frame.pixels, want[frameIndex]) {
			t.Fatalf("frame %d decoded %x valid=%v, want %x", frameIndex, frame.pixels, ok, want[frameIndex])
		}
	}
}

func TestScriptSISReferenceTransformsRejectUnsafeOrMalformedStreams(t *testing.T) {
	outOfRange := []byte{83, 73, 83, 8, 2, 32, 160, 16, 8, 160, 0, 0, 0, 0, 0, 0, 0, 0, 25, 255, 255, 255, 255, 255, 255, 255, 255, 144, 0, 0}
	valid := []byte{83, 73, 83, 8, 2, 32, 144, 16, 8, 160, 0, 0, 0, 0, 0, 0, 0, 0, 19, 255, 255, 255, 255, 255, 255, 255, 254, 64, 0, 0}
	for name, data := range map[string][]byte{
		"out-of-range tile":  outOfRange,
		"truncated index":    valid[:19],
		"truncated payload":  valid[:24],
		"missing terminator": valid[:27],
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
}

func TestScriptImageFrameRepeatedTransformWorkIsChargedBeforeDecode(t *testing.T) {
	const replacements = 65_000
	data := authoredSISRepeatedTransform(replacements)
	frame, ok := decodeScriptSISLiteralFrame(data, 0)
	if !ok || !bytes.Equal(frame.pixels, make([]byte, 8)) {
		t.Fatal("maximum repeated-transform fixture did not decode successfully")
	}
	if len(data) > 65_535 {
		t.Fatalf("fixture exceeds the supported source size: %d", len(data))
	}
	charged := scriptSISLiteralDecodeWork(data)
	if charged < replacements*3 {
		t.Fatalf("work charge %d does not reserve three tile terms for %d replacements", charged, replacements)
	}
	before := []byte("unchanged")
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: data}, {Data: slices.Clone(before)}}}, nil)
	if err := vm.ChargeWork(sgsvm.MaxSteps - charged + 1); err != nil {
		t.Fatal(err)
	}
	for _, argument := range []int16{1, 1, 0, 1, 0, 0, 0} {
		vm.Push(argument)
	}
	if err := scriptImageFrameCall(vm); err == nil {
		t.Fatal("repeated replacements bypassed the work limit")
	}
	if !bytes.Equal(vm.Resources[1].Data, before) {
		t.Fatal("work-limited transform extraction mutated the destination")
	}
}

func authoredSISRepeatedTransform(replacements int) []byte {
	var bits scriptSISBits
	bits.append(1, 5)
	bits.append(0, 5)
	bits.append(1, 5)
	bits.append(1, 4)
	bits.append(0, 1)
	bits.append(1, 5)
	bits.append(1, 3)
	bits.append(0, 1)
	bits.append(0, 4)
	bits.append(1, 3)
	bits.append(0, 4)
	bits.append(1, 5)
	bits.append(1, 4)
	bits.append(0, 1)
	for range 64 {
		bits.append(0, 1)
	}
	bits.append(0, 9)
	bits.append(1, 1)
	for range replacements {
		bits.append(0, 1)
		bits.append(1, 1)
		bits.append(0, 1)
		bits.append(0b11011, 5)
	}
	bits.append(1, 1)
	bits.append(0, 1)
	bits.append(0, 1)
	bits.append(1, 1)
	bits.append(0, 8)
	bits.append(0, 7)
	bits.append(0, 4)
	return bits.bytes()
}

func TestLocalScriptSISReferenceTransformComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_REFERENCE_TRANSFORM_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_REFERENCE_TRANSFORM_COMPARISON to an authored native-result JSONL file")
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
		"empty-width-zero":            {0, 1, 146, 22, 8, false},
		"replace-only-tile":           {0, 1, 213, 30, 8, false},
		"replace-second-tile":         {0, 1, 280, 38, 16, false},
		"replace-both-reverse-order":  {0, 1, 347, 47, 16, false},
		"coded-replacement":           {0, 1, 226, 32, 16, false},
		"transform-chain":             {0, 1, 360, 48, 16, false},
		"exact-after-transform-chain": {0, 1, 371, 50, 16, false},
		"out-of-range-index":          {0, 1, 215, 30, 8, true},
		"snapshot-independent":        {0, 3, 406, 54, 16, false},
		"snapshot-first-transform":    {1, 3, 406, 54, 16, false},
		"snapshot-second-transform":   {2, 3, 406, 54, 16, false},
		"duplicate-last-wins":         {0, 1, 282, 39, 8, false},
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
				t.Fatalf("row %d accepted the native out-of-range write", rowNumber)
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
