package wipicrt_test

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

// A game is supposed to check what MC_knlGetResourceID answered before it
// passes the value on. One local title walks numbered resources until the
// numbers run out, hands the resulting M_E_NOENT straight to
// MC_knlGetResource as the handle, and expects the second call to refuse it
// too. Reading a negative handle as an address instead faults the guest, which
// ends the run on a memory error rather than on the answer the caller's own
// error path is waiting for.
func TestGetResourceRefusesAHandleThatIsAnErrorCode(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)

	for _, handle := range []uint32{wipicrt.WIPICErrorNotFound, wipicrt.WIPICErrorGeneric, wipicrt.WIPICErrorShortBuf, 0x80000000} {
		callContext := armcore.NewContext()
		callContext.Registers[0] = handle
		callContext.Registers[1] = 0
		callContext.Registers[2] = 0
		result, err := runtime.WIPICGetResource(armcore.NewThread(callContext))
		if err != nil {
			t.Fatalf("MC_knlGetResource(%#x) error = %v, want a refusal", handle, err)
		}
		if result != wipicrt.WIPICErrorGeneric {
			t.Fatalf("MC_knlGetResource(%#x) = %#x, want %#x", handle, result, wipicrt.WIPICErrorGeneric)
		}
	}
}

// MC_knlFree is declared void, so what it leaves in r0 is not part of its
// contract — and a title reads it anyway, ending an initialization step with
// `MC_knlFree(buf)` in tail position and testing the result. Answering zero
// made a step that succeeded report failure; leaving r0 as the caller set it
// is what a routine that never writes its return register does.
func TestFreeLeavesTheCallersRegisterAlone(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)

	block := callocThroughSVC(t, runtime, 64)
	callContext := armcore.NewContext()
	callContext.Registers[0] = block
	result, err := runtime.HandleWIPICCall(armcore.NewThread(callContext), wipicrt.WIPICKernelFree)
	if err != nil {
		t.Fatalf("MC_knlFree(%#x) error = %v", block, err)
	}
	if result != block {
		t.Fatalf("MC_knlFree(%#x) left %#x in r0, want the argument back", block, result)
	}
}

// resourceCall runs MC_knlGetResourceID(name, &size) or
// MC_knlGetResource(id, buf, size) with the given registers.
func resourceCall(t *testing.T, runtime *wipicrt.Runtime, function uint32, registers ...uint32) uint32 {
	t.Helper()
	callContext := armcore.NewContext()
	copy(callContext.Registers[:], registers)
	result, err := runtime.HandleWIPICCall(armcore.NewThread(callContext), function)
	if err != nil {
		t.Fatalf("kernel %d: %v", function, err)
	}
	return result
}

// TestResourceIdentifierIsItsDirectoryPosition pins the identifier a program
// with an ordered resource directory is answered with: the resource's position
// in it, from zero. One SKT title sizes a table by one resource that lists
// every other and indexes it with these identifiers. Without a directory the
// identifier stays the name's address, and an address handed to
// MC_knlGetResource still reads, which is what a quick save from before can
// hold.
func TestResourceIdentifierIsItsDirectoryPosition(t *testing.T) {
	resources := map[string][]byte{"b": []byte("bb"), "a": []byte("aaaa"), "c": []byte("c")}
	read := func(runtime *wipicrt.Runtime, name string) (uint32, string) {
		t.Helper()
		nameAddress := wipicrttest.GuestBytes(t, runtime, append([]byte(name), 0))
		size, err := runtime.AllocateWords([]uint32{0})
		if err != nil {
			t.Fatal(err)
		}
		id := resourceCall(t, runtime, wipicrt.WIPICKernelGetResourceID, nameAddress, size)
		buffer, err := runtime.AllocateWIPIC(16)
		if err != nil {
			t.Fatal(err)
		}
		if code := resourceCall(t, runtime, wipicrt.WIPICKernelGetResource, id, buffer, 16); code != 0 {
			t.Fatalf("MC_knlGetResource(%#x) = %#x", id, code)
		}
		length := wipicrttest.ReadBytes(t, runtime.Client, size, 4)[0]
		return id, string(wipicrttest.ReadBytes(t, runtime.Client, buffer+wipicrt.WIPICAllocationOverhead, int(length)))
	}
	client, ordered := wipicrttest.NewRuntime(t)
	client.AttachResources(resources)
	client.ResourceOrder = []string{"b", "a", "c"}
	for index, name := range client.ResourceOrder {
		if id, data := read(ordered, name); id != uint32(index) || data != string(resources[name]) {
			t.Errorf("%s: id %#x data %q, want %d and %q", name, id, data, index, resources[name])
		}
	}
	legacy := wipicrttest.GuestBytes(t, ordered, []byte("c\x00"))
	buffer, err := ordered.AllocateWIPIC(16)
	if err != nil {
		t.Fatal(err)
	}
	if code := resourceCall(t, ordered, wipicrt.WIPICKernelGetResource, legacy, buffer, 16); code != 0 {
		t.Errorf("MC_knlGetResource of a name's address = %#x, want it read", code)
	}

	client, unordered := wipicrttest.NewRuntime(t)
	client.AttachResources(resources)
	if id, data := read(unordered, "a"); id < wipicrt.PlatformDataBase || data != "aaaa" {
		t.Errorf("unordered id %#x data %q, want an address and %q", id, data, "aaaa")
	}
}
