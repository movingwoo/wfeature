package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A game is supposed to check what MC_knlGetResourceID answered before it
// passes the value on. One local title walks numbered resources until the
// numbers run out, hands the resulting M_E_NOENT straight to
// MC_knlGetResource as the handle, and expects the second call to refuse it
// too. Reading a negative handle as an address instead faults the guest, which
// ends the run on a memory error rather than on the answer the caller's own
// error path is waiting for.
func TestGetResourceRefusesAHandleThatIsAnErrorCode(t *testing.T) {
	_, runtime := newTestRuntime(t)

	for _, handle := range []uint32{wipicErrorNotFound, wipicErrorGeneric, wipicErrorShortBuf, 0x80000000} {
		callContext := armcore.NewContext()
		callContext.Registers[0] = handle
		callContext.Registers[1] = 0
		callContext.Registers[2] = 0
		result, err := runtime.wipicGetResource(armcore.NewThread(callContext))
		if err != nil {
			t.Fatalf("MC_knlGetResource(%#x) error = %v, want a refusal", handle, err)
		}
		if result != wipicErrorGeneric {
			t.Fatalf("MC_knlGetResource(%#x) = %#x, want %#x", handle, result, wipicErrorGeneric)
		}
	}
}

// MC_knlFree is declared void, so what it leaves in r0 is not part of its
// contract — and a title reads it anyway, ending an initialization step with
// `MC_knlFree(buf)` in tail position and testing the result. Answering zero
// made a step that succeeded report failure; leaving r0 as the caller set it
// is what a routine that never writes its return register does.
func TestFreeLeavesTheCallersRegisterAlone(t *testing.T) {
	_, runtime := newTestRuntime(t)

	block := callocThroughSVC(t, runtime, 64)
	callContext := armcore.NewContext()
	callContext.Registers[0] = block
	result, err := runtime.handleWIPICCall(armcore.NewThread(callContext), wipicKernelFree)
	if err != nil {
		t.Fatalf("MC_knlFree(%#x) error = %v", block, err)
	}
	if result != block {
		t.Fatalf("MC_knlFree(%#x) left %#x in r0, want the argument back", block, result)
	}
}
