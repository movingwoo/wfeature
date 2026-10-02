package backend

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

type checkpointRecordFixture struct {
	Version uint32
	Name    string
	Bytes   []byte
	Rows    []checkpointRecordRow
}
type checkpointRecordRow struct {
	Number int64
	Bits   [2]uint32
	Child  *checkpointRecordRow
}

type checkpointCustomNumber int

func (checkpointCustomNumber) MarshalJSON() ([]byte, error) { return []byte("0"), nil }

func TestCheckpointRecordRoundTripAndStrictSchema(t *testing.T) {
	saved := checkpointRecordFixture{Version: 1, Name: "checkpoint 🐾", Bytes: []byte{0, 255}, Rows: []checkpointRecordRow{{Number: -3, Bits: [2]uint32{1, 2}}}}
	data, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded checkpointRecordFixture
	if err := DecodeCheckpointRecord(data, &decoded); err != nil || !reflect.DeepEqual(decoded, saved) {
		t.Fatalf("round trip differs: %+v, %v", decoded, err)
	}
	for _, test := range []struct{ name, old, replacement string }{
		{"unknown", `"Version":1`, `"Future":1`},
		{"case", `"Version":1`, `"version":1`},
		{"duplicate", `"Version":1`, `"Version":1,"Version":2`},
		{"missing", `"Version":1,`, ``},
		{"null scalar", `"Version":1`, `"Version":null`},
		{"wrong scalar", `"Version":1`, `"Version":"1"`},
		{"integer overflow", `"Version":1`, `"Version":4294967296`},
		{"wrong array length", `"Bits":[1,2]`, `"Bits":[1]`},
		{"numeric byte array", `"Bytes":"AP8="`, `"Bytes":[0,255]`},
		{"bad base64", `"Bytes":"AP8="`, `"Bytes":"!!!!"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := bytes.Replace(data, []byte(test.old), []byte(test.replacement), 1)
			if bytes.Equal(bad, data) {
				t.Fatal("test did not change its input")
			}
			target := checkpointRecordFixture{Version: 99, Name: "unchanged"}
			if err := DecodeCheckpointRecord(bad, &target); err == nil || target.Version != 99 || target.Name != "unchanged" {
				t.Fatalf("invalid input changed destination: %+v, %v", target, err)
			}
		})
	}
	for _, bad := range [][]byte{append(bytes.Clone(data), []byte(" {}")...), []byte("\xff"), data[:len(data)-1]} {
		if err := DecodeCheckpointRecord(bad, &decoded); err == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
	if _, err := EncodeCheckpointRecord(checkpointRecordFixture{Name: "\xff"}); err == nil {
		t.Fatal("invalid UTF-8 string was silently replaced")
	}
}

func TestCheckpointRecordBudgetsBeforeTypedAllocation(t *testing.T) {
	type wide struct{ Words [1 << 20]uint64 }
	// Eleven input bytes must not allocate several 8 MiB records. The preflight
	// charges backing-array element size before descending into an element.
	var target []wide
	limits := checkpointRecordLimits{encoded: 1024, decoded: 1 << 20, nodes: 100, depth: 8}
	if err := inspectCheckpointRecord([]byte("[{},{}]"), reflect.TypeOf(target), limits); err == nil {
		t.Fatal("small input with large typed allocation was accepted")
	}
	for _, test := range []struct {
		data   string
		typeOf reflect.Type
		limits checkpointRecordLimits
	}{
		{`[0,0,0,0,0]`, reflect.TypeOf([]int{}), checkpointRecordLimits{encoded: 1024, decoded: 1024, nodes: 4, depth: 8}},
		{`"` + strings.Repeat("A", 40) + `"`, reflect.TypeOf([]byte{}), checkpointRecordLimits{encoded: 1024, decoded: 16, nodes: 100, depth: 8}},
		{`[[[]]]`, reflect.TypeOf([][][]int{}), checkpointRecordLimits{encoded: 1024, decoded: 1024, nodes: 100, depth: 1}},
		{`"long"`, reflect.TypeOf(""), checkpointRecordLimits{encoded: 4, decoded: 1024, nodes: 100, depth: 8}},
	} {
		if err := inspectCheckpointRecord([]byte(test.data), test.typeOf, test.limits); err == nil {
			t.Fatalf("budget accepted %s", test.data)
		}
	}
	cycle := &checkpointRecordRow{}
	cycle.Child = cycle
	if _, err := EncodeCheckpointRecord(cycle); err == nil {
		t.Fatal("cyclic Go graph accepted as a data record")
	}
	if _, err := EncodeCheckpointRecord(map[string]any{"dynamic": 1}); err == nil {
		t.Fatal("dynamic schema accepted")
	}
	if _, err := EncodeCheckpointRecord(checkpointCustomNumber(1)); err == nil {
		t.Fatal("custom scalar encoder accepted")
	}
}

func FuzzCheckpointRecord(f *testing.F) {
	seed, err := EncodeCheckpointRecord(checkpointRecordFixture{Version: 1, Name: "authored", Rows: []checkpointRecordRow{{Number: -1}}})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte("null"))
	f.Add([]byte(`{"Version":1,"Version":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		var saved checkpointRecordFixture
		if err := DecodeCheckpointRecord(data, &saved); err != nil {
			return
		}
		encoded, err := EncodeCheckpointRecord(saved)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip checkpointRecordFixture
		if err := DecodeCheckpointRecord(encoded, &roundtrip); err != nil || !reflect.DeepEqual(saved, roundtrip) {
			t.Fatalf("accepted record does not round-trip: %v", err)
		}
	})
}
