package gdbstub

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The end-to-end check the protocol tests cannot make: that a breakpoint
// stops a *real* guest, that the registers a client reads are the guest's own
// at that instruction, and that continuing lets it finish.
func TestBreakpointStopsRealGuestExecution(t *testing.T) {
	const base uint32 = 0x1000
	const end uint32 = 0x7fff0000

	core := armcore.NewCore(armcore.CoreOptions{MaxSteps: 1000})
	if err := core.Memory().Map(base, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Map(0x2000, 0x1000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	// A straight-line ARM program: r0 counts up, then returns.
	program := []uint32{
		0xe3a00001, // mov r0, #1
		0xe2800001, // add r0, r0, #1
		0xe2800001, // add r0, r0, #1
		0xe1a0f00e, // mov pc, lr
	}
	code := make([]byte, len(program)*4)
	for index, word := range program {
		binary.LittleEndian.PutUint32(code[index*4:], word)
	}
	if err := core.Memory().Load(base, code); err != nil {
		t.Fatal(err)
	}

	target := NewCoreTarget(core)
	defer target.Detach()
	// Stop on the second add, where r0 must already be 2.
	target.SetBreakpoint(base + 8)

	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = 0x3000
	thread := armcore.NewThread(initial)
	if err := initial.SetPC(base); err != nil {
		t.Fatal(err)
	}

	type result struct {
		summary armcore.RunSummary
		err     error
	}
	done := make(chan result, 1)
	go func() {
		summary, err := core.Call(context.Background(), thread, base, end, nil, nil)
		target.Finished()
		done <- result{summary: summary, err: err}
	}()

	select {
	case stop := <-target.stops:
		if stop != armcore.DebugStopBreakpoint {
			t.Fatalf("stop reason = %v, want a breakpoint", stop)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the breakpoint never stopped the guest")
	}

	registers, _ := target.Registers()
	if registers[armcore.RegisterPC] != base+8 {
		t.Fatalf("stopped at %#x, want the breakpoint at %#x", registers[armcore.RegisterPC], base+8)
	}
	if registers[0] != 2 {
		t.Fatalf("r0 at the breakpoint = %d, want 2 — the guest ran exactly to here", registers[0])
	}

	// Writing a register at the stop changes what the guest computes next,
	// which is the whole point of a debugger.
	if err := target.SetRegister(0, 40); err != nil {
		t.Fatal(err)
	}
	target.ClearBreakpoint(base + 8)
	if _, err := target.Continue(); err != nil {
		t.Fatal(err)
	}

	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatalf("guest run error = %v", outcome.err)
		}
		// Call runs a derived thread, so the run's own summary is where the
		// finished registers are.
		if final := outcome.summary.Context.Registers[0]; final != 41 {
			t.Fatalf("r0 after continue = %d, want 41 from the written value", final)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the guest never finished after continue")
	}
}

func TestKillEndsARunningGuest(t *testing.T) {
	const base uint32 = 0x1000
	const end uint32 = 0x7fff0000

	core := armcore.NewCore(armcore.CoreOptions{MaxSteps: 1 << 20})
	if err := core.Memory().Map(base, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Map(0x2000, 0x1000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	// An endless loop, which is what a game's main loop is.
	code := make([]byte, 4)
	binary.LittleEndian.PutUint32(code, 0xeafffffe) // b .
	if err := core.Memory().Load(base, code); err != nil {
		t.Fatal(err)
	}

	target := NewCoreTarget(core)
	defer target.Detach()
	target.SetBreakpoint(base)

	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = 0x3000
	thread := armcore.NewThread(initial)
	done := make(chan error, 1)
	go func() {
		_, err := core.Call(context.Background(), thread, base, end, nil, nil)
		target.Finished()
		done <- err
	}()

	select {
	case <-target.stops:
	case <-time.After(5 * time.Second):
		t.Fatal("the breakpoint never fired")
	}
	target.Kill()
	select {
	case err := <-done:
		if !errors.Is(err, ErrKilled) {
			t.Fatalf("run error after kill = %v, want ErrKilled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kill did not end the guest")
	}
}

// gdb writes the PC as an address and the instruction set through CPSR. A PC
// write has to leave a stopped Thumb thread in Thumb, and a register-file
// round trip of what 'g' reported has to change nothing.
func TestPCWritesKeepTheThreadsInstructionSet(t *testing.T) {
	core := guestCore(t, true,
		0x2000, // movs r0, #0
		0x3001, // loop: adds r0, #1
		0x28c8, // cmp  r0, #200
		0xd1fc, // bne  loop
		0x4770, // bx   lr
	)
	target := NewCoreTarget(core)
	defer target.Detach()
	target.SetBreakpoint(guestBase + 2)
	done := runGuest(context.Background(), core, target, true)
	select {
	case <-target.stops:
	case <-time.After(5 * time.Second):
		t.Fatal("the breakpoint never stopped the guest")
	}

	thumb := func() bool {
		_, cpsr := target.Registers()
		return cpsr&(1<<5) != 0
	}
	snapshot, _, _ := handle("g", target)
	if reply, _, _ := handle("G"+snapshot, target); reply != "OK" {
		t.Fatalf("G round trip = %q", reply)
	}
	registers, _ := target.Registers()
	if !thumb() || registers[armcore.RegisterPC] != guestBase+2 {
		t.Fatalf("after a G round trip: pc=%#x thumb=%v, want %#x in Thumb", registers[armcore.RegisterPC], thumb(), guestBase+2)
	}
	for _, pc := range []uint32{guestBase, guestBase + 2} {
		if reply, _, _ := handle("Pf="+hexWord(pc), target); reply != "OK" {
			t.Fatalf("P pc=%#x reply = %q", pc, reply)
		}
		registers, _ := target.Registers()
		if !thumb() || registers[armcore.RegisterPC] != pc {
			t.Fatalf("after writing pc=%#x: pc=%#x thumb=%v, want Thumb", pc, registers[armcore.RegisterPC], thumb())
		}
	}
	// Back at the breakpoint and on to the end.
	if reply, _, _ := handle("Pf="+hexWord(guestBase+2), target); reply != "OK" {
		t.Fatalf("P reply = %q", reply)
	}
	target.ClearBreakpoint(guestBase + 2)
	if signal, err := target.Continue(); err != nil || signal != SignalKill {
		t.Fatalf("Continue() = %v, %v, want the run's end", signal, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guest run error = %v", err)
	}
}

// The status word gdb writes changes the flags and, where the PC allows it,
// the instruction set — which is how gdb itself moves a thread between them.
func TestCPSRWritesReachTheStoppedThread(t *testing.T) {
	core := guestCore(t, true,
		0x2000, // movs r0, #0
		0x3001, // loop: adds r0, #1
		0x28c8, // cmp  r0, #200
		0xd1fc, // bne  loop
		0x4770, // bx   lr
	)
	target := NewCoreTarget(core)
	defer func() {
		target.Kill()
		target.Detach()
	}()
	target.SetBreakpoint(guestBase + 2)
	done := runGuest(context.Background(), core, target, true)
	select {
	case <-target.stops:
	case <-time.After(5 * time.Second):
		t.Fatal("the breakpoint never stopped the guest")
	}
	_, cpsr := target.Registers()
	// Flags, and a switch to ARM at a halfword address, which ARM cannot run.
	if reply, _, _ := handle("P19="+hexWord(cpsr&^(1<<5)|0xf0000000), target); reply != "OK" {
		t.Fatalf("P cpsr reply = %q", reply)
	}
	registers, now := target.Registers()
	if now&0xf0000000 != 0xf0000000 || now&(1<<5) == 0 {
		t.Fatalf("cpsr = %#x at pc %#x, want the flags written and Thumb kept", now, registers[armcore.RegisterPC])
	}
	// At a word address the switch is possible, and it happens.
	if reply, _, _ := handle("Pf="+hexWord(guestBase+4), target); reply != "OK" {
		t.Fatal(reply)
	}
	if reply, _, _ := handle("P19="+hexWord(now&^(1<<5)), target); reply != "OK" {
		t.Fatal(reply)
	}
	if _, after := target.Registers(); after&(1<<5) != 0 {
		t.Fatalf("cpsr = %#x, want ARM state after clearing T at a word address", after)
	}
	target.Kill()
	<-done
}
