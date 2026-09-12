package skt

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestLocalScriptSISMetadataComparison(t *testing.T) {
	path := os.Getenv("WFEATURE_SGS_SIS_METADATA_COMPARISON")
	if path == "" {
		t.Skip("set WFEATURE_SGS_SIS_METADATA_COMPARISON to an authored native-result JSONL file")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type result struct {
		Data         []byte   `json:"data"`
		NativeResult int16    `json:"native_result"`
		Values       [5]int16 `json:"values"`
		Finished     bool     `json:"finished"`
	}
	scanner := bufio.NewScanner(f)
	cases, typeOne, typeTwo, success, failure := 0, 0, 0, 0, 0
	seen := make(map[string]struct{}, 102)
	for scanner.Scan() {
		var want result
		if err := json.Unmarshal(scanner.Bytes(), &want); err != nil {
			t.Fatalf("case %d: %v", cases, err)
		}
		if !want.Finished {
			t.Fatalf("case %d did not return from the native helper", cases)
		}
		key := string(want.Data)
		if _, ok := seen[key]; ok {
			t.Fatalf("case %d repeats header %x", cases, want.Data)
		}
		seen[key] = struct{}{}
		if len(want.Data) == 8 {
			typeOne++
		} else {
			typeTwo++
		}
		switch want.NativeResult {
		case 0:
			success++
		case -1:
			failure++
		default:
			t.Fatalf("case %d has unexpected native result %d", cases, want.NativeResult)
		}
		vm := sgsvm.New(&sgsvm.Program{
			Variables: []sgsvm.Variable{{Mutable: true, Values: make([]int16, 5)}},
			Resources: []sgsvm.Resource{{Data: want.Data}},
		}, nil)
		vm.Push(71)
		for _, arg := range []int16{1, 1, 0, 0, 0, 42, 43} {
			vm.Push(arg)
		}
		if err := (&ScriptSession{}).Call(0xe8, vm); err != nil {
			t.Fatalf("case %d data=%x: %v", cases, want.Data, err)
		}
		gotResult := vm.Pop()
		if gotResult != want.NativeResult || !slices.Equal(vm.Variables[0].Values, want.Values[:]) {
			t.Errorf("case %d data=%x: got result=%d values=%v, want result=%d values=%v", cases, want.Data, gotResult, vm.Variables[0].Values, want.NativeResult, want.Values)
		}
		if vm.Pop() != 71 {
			t.Fatalf("case %d consumed caller stack", cases)
		}
		cases++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if cases != 102 || len(seen) != 102 || typeOne != 12 || typeTwo != 90 || success != 80 || failure != 22 {
		t.Fatalf("comparison coverage cases=%d unique=%d type1=%d type2=%d success=%d failure=%d", cases, len(seen), typeOne, typeTwo, success, failure)
	}
}
