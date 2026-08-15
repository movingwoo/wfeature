package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func utilCall(t *testing.T, runtime *initializationRuntime, function uint32, arguments ...uint32) uint32 {
	t.Helper()
	callContext := armcore.NewContext()
	for index, value := range arguments {
		callContext.Registers[index] = value
	}
	result, err := runtime.handleWIPICTableCall(armcore.NewThread(callContext), wipicTableUtil, function)
	if err != nil {
		t.Fatalf("utility function %d: %v", function, err)
	}
	return result
}

// The halfword conversions answer in the low sixteen bits, because the caller
// that stops this platform passes a sign-extended halfword and stores the
// answer back with `strh`. Swapping thirty-two bits for it would put the value
// in the half the caller never reads.
func TestUtilityConvertsByteOrderByWidth(t *testing.T) {
	_, runtime := newTestRuntime(t)

	if got := utilCall(t, runtime, wipicUtilHtonl, 0x01020304); got != 0x04030201 {
		t.Fatalf("MC_utilHtonl(0x01020304) = %#x, want 0x04030201", got)
	}
	if got := utilCall(t, runtime, wipicUtilNtohl, 0x04030201); got != 0x01020304 {
		t.Fatalf("MC_utilNtohl(0x04030201) = %#x, want 0x01020304", got)
	}
	// 80 is the port a download prompt would be converting.
	if got := utilCall(t, runtime, wipicUtilHtons, 80); got != 0x5000 {
		t.Fatalf("MC_utilHtons(80) = %#x, want 0x5000", got)
	}
	if got := utilCall(t, runtime, wipicUtilNtohs, 0x5000); got != 80 {
		t.Fatalf("MC_utilNtohs(0x5000) = %#x, want 80", got)
	}
	// A sign-extended argument is the shape the call site actually passes.
	if got := utilCall(t, runtime, wipicUtilHtons, 0xffff8000); got != 0x0080 {
		t.Fatalf("MC_utilHtons(-32768) = %#x, want 0x0080", got)
	}
}

// The address conversions have to be each other's inverse, and the octet order
// has to be the one a socket call takes: the first octet in the lowest byte,
// because the value is in network byte order on a little-endian machine.
func TestUtilityConvertsAnAddressBothWays(t *testing.T) {
	client, runtime := newTestRuntime(t)

	buffer, err := runtime.allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	text := append([]byte("211.234.1.90"), 0)
	if err := client.core.Memory().Write(buffer, text); err != nil {
		t.Fatal(err)
	}
	address := utilCall(t, runtime, wipicUtilInetAddrInt, buffer)
	if address != 0x5a01ead3 {
		t.Fatalf("MC_utilInetAddrInt = %#x, want 0x5a01ead3", address)
	}

	back, err := runtime.allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	utilCall(t, runtime, wipicUtilInetAddrStr, address, back)
	written, err := runtime.readGuestCString(back, maxInetAddrText)
	if err != nil {
		t.Fatal(err)
	}
	if written != "211.234.1.90" {
		t.Fatalf("MC_utilInetAddrStr wrote %q", written)
	}
}

// An address the platform cannot read is refused with the failure the
// specification gives it, because the caller has a documented path for that and
// no path at all for an address it never asked for.
func TestUtilityRefusesAnAddressItCannotRead(t *testing.T) {
	client, runtime := newTestRuntime(t)

	buffer, err := runtime.allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"", "1.2.3", "1.2.3.4.5", "1.2.3.999", "1.2.3.x"} {
		if err := client.core.Memory().Write(buffer, append([]byte(text), 0)); err != nil {
			t.Fatal(err)
		}
		if got := utilCall(t, runtime, wipicUtilInetAddrInt, buffer); got != wipiErrorCode {
			t.Fatalf("MC_utilInetAddrInt(%q) = %#x, want %#x", text, got, wipiErrorCode)
		}
	}
}

// The seventh entry the original runtime carries has no published prototype, so
// it keeps failing with its call site rather than being answered with a number
// the game would believe.
func TestUtilityLeavesTheUnspecifiedEntryFailing(t *testing.T) {
	_, runtime := newTestRuntime(t)

	_, err := runtime.handleWIPICTableCall(armcore.NewThread(armcore.NewContext()), wipicTableUtil, 6)
	if err == nil {
		t.Fatal("utility function 6 answered instead of naming its call site")
	}
}
