package ktf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

const dialFailureClient = "a62730b07a2e540619dafd0395ef884f868a3e76446dbd604c8bc12bf3d71349"

// dialFailureFixture loads the recognized client's windows where the fix
// expects them. Everything the dial callback reaches is the test's own: the
// table r10 addresses, the words it names, the call veneer and a print
// routine that only returns. The network state word starts at 1, connecting.
func dialFailureFixture(t *testing.T) *armcore.Memory {
	t.Helper()
	memory := armcore.NewMemory()
	for _, page := range []uint32{0x118000, 0x129000, 0x187000, 0x194000, 0x1ae000, 0x200000} {
		if err := memory.Map(page, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
	}
	for _, rewrite := range dialFailureRewrites[dialFailureClient] {
		if err := memory.Load(rewrite.at, rewrite.expect); err != nil {
			t.Fatal(err)
		}
	}
	word := func(value uint32) []byte { return binary.LittleEndian.AppendUint32(nil, value) }
	// The callback forms r10 = 0x1ae0a0 from its first literal.
	for address, data := range map[uint32][]byte{
		0x1ae0a0 + 0x250: word(0x200010), // cleared on entry
		0x1ae0a0 + 0x1f8: word(0x200014), // cleared on entry
		0x1ae0a0 + 0x1fc: word(0x200018), // the network state word
		0x1ae0a0 + 0x040: word(0x200020), // the object whose first method prints
		0x1ae0a0 + 0x260: word(0x200040), // the text of the failure
		0x200018:         word(1),
		0x200020:         word(0x200024),
		0x200024:         word(0x200101),
		0x200100:         {0x70, 0x47}, // print: bx lr
		0x18774c:         {0x18, 0x47}, // veneer: bx r3
	} {
		if err := memory.Load(address, data); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

// dialCallback runs the callback with one result of the dial and answers the
// network state word it leaves.
func dialCallback(t *testing.T, memory *armcore.Memory, result uint32) uint32 {
	t.Helper()
	state := armcore.NewContext()
	state.Registers[0] = result
	state.Registers[13] = 0x200800
	state.Registers[14] = 0x200201
	if err := state.SetPC(0x118c11); err != nil {
		t.Fatal(err)
	}
	run, err := (armcore.Engine{}).Run(&state, memory, 0x200200, 128)
	if err != nil {
		t.Fatal(err)
	}
	if run.Reason != armcore.StopEnd {
		t.Fatalf("the callback stopped at %#x before returning", state.PC())
	}
	var word [4]byte
	if err := memory.Read(0x200018, word[:]); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(word[:])
}

// The callback as packaged leaves the state at connecting when the dial fails.
// With the fix the same failure stores 100, and an error other than the one a
// failed dial reports still changes nothing.
func TestDialFailureStoresTheNetworkErrorState(t *testing.T) {
	const connecting, failed = 1, 100
	const dialError, otherError = 0xffffffff, 0xfffffffe
	if got := dialCallback(t, dialFailureFixture(t), dialError); got != connecting {
		t.Fatalf("packaged callback: state = %d, want %d", got, connecting)
	}
	memory := dialFailureFixture(t)
	if err := applyDialFailureNotice(memory, dialFailureClient); err != nil {
		t.Fatal(err)
	}
	if got := dialCallback(t, memory, dialError); got != failed {
		t.Fatalf("state = %d, want %d", got, failed)
	}
	memory = dialFailureFixture(t)
	if err := applyDialFailureNotice(memory, dialFailureClient); err != nil {
		t.Fatal(err)
	}
	if got := dialCallback(t, memory, otherError); got != connecting {
		t.Fatalf("another error: state = %d, want %d", got, connecting)
	}
}

// Entry 100 of the saved-file check's table takes the offset of entry 31, and
// nothing outside the two rewritten runs changes.
func TestDialFailureAnswersTheSavedFileCheck(t *testing.T) {
	memory := dialFailureFixture(t)
	if err := applyDialFailureNotice(memory, dialFailureClient); err != nil {
		t.Fatal(err)
	}
	var entry31, entry100 [4]byte
	if err := memory.Read(0x19451c, entry31[:]); err != nil {
		t.Fatal(err)
	}
	if err := memory.Read(0x1944a0+100*4, entry100[:]); err != nil {
		t.Fatal(err)
	}
	if entry100 != entry31 {
		t.Fatalf("entry 100 = %x, entry 31 = %x", entry100, entry31)
	}
	for _, rewrite := range dialFailureRewrites[dialFailureClient] {
		want := bytes.Clone(rewrite.expect)
		copy(want[rewrite.offset:], rewrite.replace)
		got := make([]byte, len(want))
		if err := memory.Read(rewrite.at, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("window at %#x = %x, want %x", rewrite.at, got, want)
		}
	}
}

func TestDialFailureRejectsUnexpectedCode(t *testing.T) {
	if err := applyDialFailureNotice(armcore.NewMemory(), dialFailureClient); err == nil {
		t.Fatal("unmapped code accepted")
	}
	for _, rewrite := range dialFailureRewrites[dialFailureClient] {
		memory := dialFailureFixture(t)
		if err := memory.Write(rewrite.at, []byte{rewrite.expect[0] ^ 0xff}); err != nil {
			t.Fatal(err)
		}
		if err := applyDialFailureNotice(memory, dialFailureClient); err == nil {
			t.Fatalf("changed window at %#x accepted", rewrite.at)
		}
		var written [4]byte
		if err := memory.Read(0x194630, written[:]); err != nil {
			t.Fatal(err)
		}
		if rewrite.at != 0x194620 && written != [4]byte{0x10, 0x56, 0xf9, 0xff} {
			t.Fatalf("a refused start rewrote the table: %x", written)
		}
	}
	if err := applyDialFailureNotice(dialFailureFixture(t), "unknown"); err == nil {
		t.Fatal("client without recorded code accepted")
	}
	if hasDialFailureCompatibility([]byte("unrecognized client")) {
		t.Fatal("unknown revision matched")
	}
}
