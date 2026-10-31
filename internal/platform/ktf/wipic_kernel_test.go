package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// kernelCall drives one kernel slot with the arguments a guest would pass.
func kernelCall(t *testing.T, runtime *initializationRuntime, id uint32, arguments ...uint32) uint32 {
	t.Helper()
	context := armcore.NewContext()
	for index, value := range arguments {
		context.Registers[index] = value
	}
	result, err := runtime.HandleWIPICCall(armcore.NewThread(context), id)
	if err != nil {
		t.Fatalf("kernel slot %d error = %v", id, err)
	}
	return result
}

// TestWIPICSetSystemPropertyIsRefused pins slot 30. The handset's identity is
// not the title's to change, so every property answers M_E_INVALID and the
// getter goes on answering what it did.
func TestWIPICSetSystemPropertyIsRefused(t *testing.T) {
	client, runtime := newTestRuntime(t)
	name := guestBytes(t, runtime, []byte("PHONENUMBER\x00"))
	value := guestBytes(t, runtime, []byte("01099999999\x00"))
	if result := kernelCall(t, runtime, wipicrt.WIPICKernelSetSysProperty, name, value); result != wipicrt.WIPICErrorInvalid {
		t.Fatalf("set system property answered %#x, want M_E_INVALID", result)
	}
	out := guestBytes(t, runtime, make([]byte, 32))
	if result := kernelCall(t, runtime, wipicrt.WIPICKernelGetSysProperty, name, out, 32); result != 0 {
		t.Fatalf("get system property answered %#x", result)
	}
	want, _ := client.SystemProperty("PHONENUMBER")
	if got := string(readTestBytes(t, client, out, len(want))); got != want {
		t.Fatalf("PHONENUMBER after a refused set = %q, want %q", got, want)
	}
}

// TestWIPICLoadFindsNoLibrary pins slot 5, MC_knlLoad. Its caller loads a
// library by the name MC_knlGetExecNames gave it and goes on without the
// library on a negative answer; nothing but the running program is installed.
func TestWIPICLoadFindsNoLibrary(t *testing.T) {
	_, runtime := newTestRuntime(t)
	for _, name := range []string{"", "FastLibrary"} {
		address := guestBytes(t, runtime, append([]byte(name), 0))
		if result := kernelCall(t, runtime, wipicrt.WIPICKernelLoad, address, 0); result != wipicrt.WIPICErrorInvalid {
			t.Fatalf("load %q answered %#x, want M_E_INVALID", name, result)
		}
	}
}
