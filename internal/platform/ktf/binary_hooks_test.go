package ktf

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

const hookScratch = uint32(0x28000000)

func newHookRuntime(t *testing.T) (*Client, *initializationRuntime) {
	t.Helper()
	client, runtime := newTestRuntime(t)
	if err := client.core.Memory().Map(hookScratch, 1<<16, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	return client, runtime
}

func callHook(t *testing.T, client *Client, runtime *initializationRuntime, kind binaryHookKind, arguments ...uint32) uint32 {
	t.Helper()
	for register, value := range arguments {
		if err := client.thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.handleBinaryHookCall(client.thread, uint32(kind))
	if err != nil {
		t.Fatalf("hook %d error = %v", kind, err)
	}
	return result
}

func readScratch(t *testing.T, client *Client, address uint32, length int) []byte {
	t.Helper()
	buffer := make([]byte, length)
	if err := client.core.Memory().Read(address, buffer); err != nil {
		t.Fatal(err)
	}
	return buffer
}

// TestHookedMemsetFillsAndReturnsDestination pins the C contract. Getting the
// argument order wrong here would not fail loudly — it would quietly fill the
// wrong bytes — so the length, the truncation of the value to a byte, and the
// returned pointer are all checked.
func TestHookedMemsetFillsAndReturnsDestination(t *testing.T) {
	client, runtime := newHookRuntime(t)
	if err := client.core.Memory().Write(hookScratch, bytes.Repeat([]byte{0xaa}, 16)); err != nil {
		t.Fatal(err)
	}
	// memset(scratch+4, 0x1ff, 8): only the low byte is written.
	result := callHook(t, client, runtime, hookMemset, hookScratch+4, 0x1ff, 8)
	if result != hookScratch+4 {
		t.Fatalf("memset returned %#x, want the destination %#x", result, hookScratch+4)
	}
	got := readScratch(t, client, hookScratch, 16)
	want := append(append(bytes.Repeat([]byte{0xaa}, 4), bytes.Repeat([]byte{0xff}, 8)...), 0xaa, 0xaa, 0xaa, 0xaa)
	if !bytes.Equal(got, want) {
		t.Fatalf("memory after memset = %x, want %x", got, want)
	}

	// A zero length writes nothing at all.
	if result := callHook(t, client, runtime, hookMemset, hookScratch, 0, 0); result != hookScratch {
		t.Fatalf("memset with zero length returned %#x", result)
	}
	if got := readScratch(t, client, hookScratch, 1); got[0] != 0xaa {
		t.Fatalf("memset with zero length wrote %#x", got[0])
	}
}

func TestHookedMemcpyCopiesAndReturnsDestination(t *testing.T) {
	client, runtime := newHookRuntime(t)
	source := hookScratch
	destination := hookScratch + 0x100
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := client.core.Memory().Write(source, payload); err != nil {
		t.Fatal(err)
	}
	result := callHook(t, client, runtime, hookMemcpy, destination, source, uint32(len(payload)))
	if result != destination {
		t.Fatalf("memcpy returned %#x, want the destination %#x", result, destination)
	}
	if got := readScratch(t, client, destination, len(payload)); !bytes.Equal(got, payload) {
		t.Fatalf("copied bytes = %x, want %x", got, payload)
	}
}

func TestHookedStrlenStopsAtTheTerminator(t *testing.T) {
	client, runtime := newHookRuntime(t)
	if err := client.core.Memory().Write(hookScratch, append([]byte("hello"), 0, 'x')); err != nil {
		t.Fatal(err)
	}
	if length := callHook(t, client, runtime, hookStrlen, hookScratch); length != 5 {
		t.Fatalf("strlen = %d, want 5", length)
	}
	// An empty string is zero, not the chunk the scan reads at a time.
	if err := client.core.Memory().Write(hookScratch, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if length := callHook(t, client, runtime, hookStrlen, hookScratch); length != 0 {
		t.Fatalf("strlen of an empty string = %d, want 0", length)
	}
	// A string longer than one chunk still resolves.
	long := append(bytes.Repeat([]byte{'a'}, hookStringChunk+7), 0)
	if err := client.core.Memory().Write(hookScratch, long); err != nil {
		t.Fatal(err)
	}
	if length := callHook(t, client, runtime, hookStrlen, hookScratch); length != uint32(hookStringChunk+7) {
		t.Fatalf("strlen across chunks = %d, want %d", length, hookStringChunk+7)
	}
}

// strcpy is the hazard memcpy is: nothing fails loudly if the arguments are
// swapped or the terminator is left off, so the contract is pinned — the
// terminator is copied, the destination comes back, and nothing past the
// terminator is touched.
func TestHookedStrcpyCopiesTheTerminatorAndReturnsTheDestination(t *testing.T) {
	client, runtime := newHookRuntime(t)
	const source = hookScratch
	const destination = hookScratch + 0x100
	if err := client.core.Memory().Write(source, append([]byte("hello"), 0, 'x')); err != nil {
		t.Fatal(err)
	}
	// Pre-fill the destination so a missing terminator would show as leftovers.
	if err := client.core.Memory().Write(destination, bytes.Repeat([]byte{'Z'}, 8)); err != nil {
		t.Fatal(err)
	}
	if got := callHook(t, client, runtime, hookStrcpy, destination, source); got != destination {
		t.Fatalf("strcpy returned %#x, want the destination %#x", got, destination)
	}
	if got := readScratch(t, client, destination, 8); !bytes.Equal(got, append([]byte("hello"), 0, 'Z', 'Z')) {
		t.Fatalf("destination = %q, want \"hello\\x00ZZ\"", got)
	}
	// The source must not have been written through.
	if got := readScratch(t, client, source, 7); !bytes.Equal(got, append([]byte("hello"), 0, 'x')) {
		t.Fatalf("source = %q, want it untouched", got)
	}
}

func TestHookedStrcpyCopiesAnEmptyString(t *testing.T) {
	client, runtime := newHookRuntime(t)
	const source = hookScratch
	const destination = hookScratch + 0x100
	if err := client.core.Memory().Write(source, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(destination, []byte{'Z', 'Z'}); err != nil {
		t.Fatal(err)
	}
	callHook(t, client, runtime, hookStrcpy, destination, source)
	if got := readScratch(t, client, destination, 2); !bytes.Equal(got, []byte{0, 'Z'}) {
		t.Fatalf("destination = %q, want just the terminator written", got)
	}
}

// TestBinaryHooksInstallOverMatchesOnly covers the scan: a pattern present in
// the image is replaced with a stub that traps, and an image without one is
// left exactly as it was.
func TestBinaryHooksInstallOverMatchesOnly(t *testing.T) {
	client, runtime := newHookRuntime(t)
	target := binaryHooks[2]
	raw, err := hex.DecodeString(strings.ReplaceAll(target.pattern, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	image := make([]byte, 512)
	copy(image[64:], raw)
	if err := client.core.Memory().Write(hookScratch, image); err != nil {
		t.Fatal(err)
	}

	installed, err := runtime.installBinaryHooks(image, hookScratch)
	if err != nil {
		t.Fatalf("installBinaryHooks() error = %v", err)
	}
	if installed != 1 {
		t.Fatalf("installed %d hooks over one match, want 1", installed)
	}
	stub := readScratch(t, client, hookScratch+64, hookStubSize)
	if stub[8] != byte(svcCategoryBinaryHook) || stub[9] != 0xdf {
		t.Fatalf("stub does not trap: %x", stub)
	}
	if stub[12] != byte(target.kind) {
		t.Fatalf("stub carries hook id %d, want %d", stub[12], target.kind)
	}
	// Bytes before the match are untouched.
	if got := readScratch(t, client, hookScratch, 64); !bytes.Equal(got, make([]byte, 64)) {
		t.Fatal("installing a hook wrote outside the matched routine")
	}

	blank := make([]byte, 512)
	installed, err = runtime.installBinaryHooks(blank, hookScratch)
	if err != nil {
		t.Fatal(err)
	}
	if installed != 0 {
		t.Fatalf("installed %d hooks over an image with no match", installed)
	}
}
