package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A stubbed table answers zero and is counted, and the count names the guest
// address that called it. Without the address, a "stub table 6 function 3" line
// in a report cannot be investigated at all: the WIPI C interface is an array of
// function pointers the guest indexes, so the table number never appears in the
// guest's own code and cannot be searched for. A stub answers rather than fails,
// so unlike an unimplemented table there is no error message to carry the site —
// the count is the only place it is ever recorded.
func TestStubTableCallCountsItsCallSite(t *testing.T) {
	_, runtime := newTestRuntime(t)

	callContext := armcore.NewContext()
	callContext.Registers[armcore.RegisterLR] = 0x18f2c1
	result, err := runtime.handleWIPICTableCall(armcore.NewThread(callContext), 6, 3)
	if err != nil {
		t.Fatalf("stub table call: %v", err)
	}
	if result != 0 {
		t.Errorf("stub table call answered %#x, want 0", result)
	}

	const want = "wipic stub table 6 function 3 @0x18f2c1"
	if got := runtime.diagnosticCounts()[want]; got != 1 {
		t.Errorf("count for %q is %d, want 1; counts are %v", want, got, runtime.diagnosticCounts())
	}
}

// The site is a suffix the counter knows how to drop, so two calls into the same
// slot from different addresses still add up under the name a report has always
// used, and a caller in a loop cannot spend the name budget on one boundary.
func TestStubTableCountCollapsesToItsSlotName(t *testing.T) {
	const slot = "wipic stub table 6 function 3"
	if got := collapseDiagnosticName(slot + " @0x18f2c1"); got != slot {
		t.Errorf("collapsed to %q, want %q", got, slot)
	}
	// It is also one of the events whose detail survives the name limit, which
	// is what makes carrying the site worth anything on a busy run.
	if !diagnosticKeepsDetail(slot + " @0x18f2c1") {
		t.Errorf("%q lost its detail past the name limit", slot)
	}
}
