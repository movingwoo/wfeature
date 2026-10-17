package jvm

import "testing"

// A waiter that a notify chose after its interrupt had already woken it holds
// that notification, and a notification may not be lost to an interrupt
// (JLS 17.2.4): the wait returns normally and the interrupt stays pending for
// the thread's next wait or sleep. Throwing instead would leave every other
// waiter in the set asleep with nobody notified.
func TestInterruptedWaiterKeepsANotificationItWasGiven(t *testing.T) {
	vm := New(nil, Options{})
	defer vm.Close()
	lock := &Object{ClassName: ObjectClass}
	state := vm.newExecution()
	if err := vm.enterMonitor(state, &lock.monitor); err != nil {
		t.Fatal(err)
	}
	// Where waitMonitor leaves the waiter: in the wait set, the monitor
	// released.
	wait := &bytecodeThreadWait{kind: "wait", object: lock, depth: 1, notification: make(chan struct{})}
	lock.monitor.mu.Lock()
	lock.monitor.waiters = append(lock.monitor.waiters, wait)
	lock.monitor.owner, lock.monitor.depth = 0, 0
	lock.monitor.mu.Unlock()
	// The interrupt wins the waiter's select, and before the waiter takes the
	// monitor's lock to leave the set, a notify picks it.
	wait.interrupted, wait.reenter = true, true
	notifier := vm.newExecution()
	if err := vm.enterMonitor(notifier, &lock.monitor); err != nil {
		t.Fatal(err)
	}
	if err := lock.monitor.notify(notifier.id, false); err != nil {
		t.Fatal(err)
	}
	if err := lock.monitor.exit(notifier.id); err != nil {
		t.Fatal(err)
	}

	if err := vm.continueMonitorWait(state, wait); err != nil {
		t.Fatalf("notified wait error = %v, want a normal return", err)
	}
	if !vm.consumeThreadInterrupt(vm.mainThreadObject()) {
		t.Fatal("the interrupt was dropped instead of left pending")
	}
	if err := lock.monitor.exit(state.id); err != nil {
		t.Fatalf("the waiter does not own the monitor again: %v", err)
	}

	// An interrupted waiter nobody notified still throws.
	if err := vm.enterMonitor(state, &lock.monitor); err != nil {
		t.Fatal(err)
	}
	alone := &bytecodeThreadWait{kind: "wait", object: lock, depth: 1, notification: make(chan struct{})}
	alone.interrupted, alone.reenter = true, true
	lock.monitor.mu.Lock()
	lock.monitor.owner, lock.monitor.depth = 0, 0
	lock.monitor.mu.Unlock()
	if err := vm.continueMonitorWait(state, alone); !vm.IsGuestException(err, "java/lang/InterruptedException") {
		t.Fatalf("interrupted wait error = %v, want InterruptedException", err)
	}
}
