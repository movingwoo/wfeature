package ktf

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestContinuationHeapRestoresDynamicObjectsAndGuestArray(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	source.start(t)
	class, err := source.runtime.resolveAOTArrayClass('I')
	if err != nil {
		t.Fatal(err)
	}
	address, err := source.runtime.allocateAOTArrayObject(class, 2)
	if err != nil {
		t.Fatal(err)
	}
	array, _ := source.client.vm.AOTObject(address)
	if err := jvm.SetArrayRange(array, 0, []jvm.Value{jvm.IntValue(41), jvm.IntValue(42)}); err != nil {
		t.Fatal(err)
	}
	source.object.SetFieldValue("array", jvm.ReferenceValue(array))
	source.object.SetFieldValue("self", jvm.ReferenceValue(source.object))
	source.object.SetFieldValue("label", jvm.ReferenceValue(source.client.vm.NewString("saved after startup")))
	saved := source.capture(t)
	wantNext, err := source.runtime.allocate(20)
	if err != nil {
		t.Fatal(err)
	}
	source.client.StopThreads()
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded continuationFixtureState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	fresh := newContinuationRestoreFixture(t, 1800000000, continuationFixtureOptions{})
	fresh.restore(t, decoded)
	if fresh.object == source.object {
		t.Fatal("worker retained its source heap")
	}
	self, _ := fresh.object.Fields["self"].Reference()
	label, _ := fresh.object.Fields["label"].Reference()
	text, _ := jvm.StringText(label)
	if self != fresh.object || text != "saved after startup" {
		t.Fatal("dynamic heap roots were lost")
	}
	restoredArray, _ := fresh.object.Fields["array"].Reference()
	if bound, _ := fresh.client.vm.AOTObjectAt(address); bound != restoredArray {
		t.Fatal("restored array binding differs from its field")
	}
	if err := jvm.SetArrayElement(restoredArray, 0, jvm.IntValue(99)); err != nil {
		t.Fatal(err)
	}
	elements := address + javaInstanceSize + javaInstanceHeader + javaArrayLengthSize
	if got := binary.LittleEndian.Uint32(readTestBytes(t, fresh.client, elements, 4)); got != 99 {
		t.Fatalf("Java store did not reach restored memory: %d", got)
	}
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], 87)
	if err := fresh.client.core.Memory().Write(elements+4, word[:]); err != nil {
		t.Fatal(err)
	}
	values, err := jvm.ArrayRange(restoredArray, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := values[1].Int32(); got != 87 {
		t.Fatalf("guest store did not reach restored array: %d", got)
	}
	gotNext, err := fresh.runtime.allocate(20)
	if err != nil || gotNext != wantNext {
		t.Fatalf("next allocation=%#x, %v; want %#x", gotNext, err, wantNext)
	}
}

func TestArenaStatePreservesFreeListAndRefusesMalformedRanges(t *testing.T) {
	arena := newGuestArena(0x1000, 0x1000)
	a, _ := arena.allocate(32)
	_, _ = arena.allocate(32)
	c, _ := arena.allocate(32)
	arena.release(a, 32)
	arena.release(c, 32)
	saved, err := arena.captureState()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := restoreArenaState(saved, 0x1000, 0x1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []uint64{12, 24, 4, 64} {
		want, ok := arena.allocate(size)
		got, freshOK := fresh.allocate(size)
		if ok != freshOK || got != want {
			t.Fatalf("allocation(%d)=%#x/%v, want %#x/%v", size, got, freshOK, want, ok)
		}
	}
	for _, change := range []func(*arenaState){
		func(s *arenaState) { s.Freed++ }, func(s *arenaState) { s.Cursor++ },
		func(s *arenaState) { s.HighWater = s.Cursor - 4 },
		func(s *arenaState) { s.Free[0].End = s.Cursor },
		func(s *arenaState) { s.Free = append(s.Free, s.Free[0]) },
	} {
		bad := saved
		bad.Free = append([]arenaBlockState(nil), saved.Free...)
		change(&bad)
		if _, err := restoreArenaState(bad, 0x1000, 0x1000); err == nil {
			t.Fatal("malformed arena accepted")
		}
	}
}
