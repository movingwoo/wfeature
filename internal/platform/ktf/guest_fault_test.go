package ktf

import (
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A guest fault comes back as an address and an opcode, and the address is
// never the question: what was being dispatched, on which object, and from
// which Java method are. The registers at the faulting instruction hold all
// three, so the report names them the way every other boundary event does.
func TestGuestFaultReportNamesTheObjectsInTheFrame(t *testing.T) {
	_, runtime := newTestRuntime(t)
	address, _ := newGuestString(t, runtime)

	var context armcore.Context
	context.Registers[2] = address
	context.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize) - 64
	context.Registers[armcore.RegisterLR] = 0x1234
	fault := &armcore.InstructionError{PC: 0x17f086, Instruction: 0x58d4, Thumb: true, Cause: armcore.ErrUnmapped}

	report := runtime.describeGuestFault(context, fault)
	if !strings.Contains(report, "regs=") || !strings.Contains(report, "lr=0x1234") {
		t.Fatalf("report does not carry the registers: %s", report)
	}
	if !strings.Contains(report, "java/lang/String") {
		t.Fatalf("report does not name the bound object in r2: %s", report)
	}
}

// The AOT bridge raises two control-flow errors through the same return as a
// fault — a caught exception unwinding and an uncaught one crossing back — and
// neither is a crash to report registers for.
func TestGuestFaultTellsAFaultFromTheBridgesOwnErrors(t *testing.T) {
	fault := &armcore.InstructionError{PC: 0x100, Cause: armcore.ErrUnmapped}
	if _, ok := guestFault(fault); !ok {
		t.Fatal("an instruction fault was not reported as one")
	}
	if _, ok := guestFault(&aotExceptionUnwind{}); ok {
		t.Fatal("an exception unwind was reported as a fault")
	}
	if _, ok := guestFault(&UncaughtAOTException{}); ok {
		t.Fatal("an uncaught guest exception was reported as a fault")
	}
	if _, ok := guestFault(nil); ok {
		t.Fatal("no error at all was reported as a fault")
	}
}
