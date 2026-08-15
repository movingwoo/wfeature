package ktf

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// java/util/Timer and java/util/TimerTask are due on the platform's own timer
// service and run on a guest thread.
//
// When a task comes due is the service's decision: a KTF title already paces
// itself with MC_knlSetTimer, the Host already runs those callbacks in due
// order once per round, and a Java task is the same shape of work — a delay, a
// body, and possibly a period.
//
// Where it runs is the specification's: Timer runs its tasks on a background
// thread. That is not a formality — one title's whole battle is a loop inside
// run() that never returns, and invoking it on the client thread left the Host
// inside a single call with no frame collected and no key delivered. See
// Client.startTimerTask and docs/ktf.md.
const (
	runtimeTimerClass     = "java/util/Timer"
	runtimeTimerTaskClass = "java/util/TimerTask"
)

func runtimeTimerClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        runtimeTimerClass,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: runtimeTimerClass, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: runtimeTimerClass, name: "schedule", descriptor: "(Ljava/util/TimerTask;J)V", accessFlags: 0x0001, implementation: runtimeTimerSchedule},
			{class: runtimeTimerClass, name: "schedule", descriptor: "(Ljava/util/TimerTask;JJ)V", accessFlags: 0x0001, implementation: runtimeTimerSchedule},
			// Fixed-rate scheduling differs from the plain kind in how it
			// makes up for a run that started late. Nothing here runs late by
			// more than a service round, so the two schedule the same way.
			{class: runtimeTimerClass, name: "scheduleAtFixedRate", descriptor: "(Ljava/util/TimerTask;JJ)V", accessFlags: 0x0001, implementation: runtimeTimerSchedule},
			{class: runtimeTimerClass, name: "cancel", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeTimerCancel},
		},
	}
}

func runtimeTimerTaskClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        runtimeTimerTaskClass,
		superName:   "java/lang/Object",
		accessFlags: 0x0421,
		methods: []runtimeJavaMethod{
			{class: runtimeTimerTaskClass, name: "<init>", descriptor: "()V", accessFlags: 0x0004, implementation: runtimeComponentNoop},
			// run is the task's own; a subclass always overrides it, and
			// virtual dispatch reaches that override rather than this.
			{class: runtimeTimerTaskClass, name: "run", descriptor: "()V", accessFlags: 0x0401, implementation: runtimeComponentNoop},
			{class: runtimeTimerTaskClass, name: "cancel", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeTimerTaskCancel},
			{class: runtimeTimerTaskClass, name: "scheduledExecutionTime", descriptor: "()J", accessFlags: 0x0001, implementation: runtimeTimerTaskScheduledTime},
		},
	}
}

// runtimeTimerSchedule serves both schedule forms and scheduleAtFixedRate: the
// task, a delay, and for the three-argument forms a repeat period.
func runtimeTimerSchedule(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 3 {
		return jvm.VoidValue(), fmt.Errorf("Timer.schedule expected receiver, task, and delay")
	}
	owner, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	task, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if task == nil {
		return jvm.VoidValue(), fmt.Errorf("Timer.schedule task is null")
	}
	delay, err := arguments[2].Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	var period int64
	if len(arguments) >= 4 {
		if period, err = arguments[3].Int64(); err != nil {
			return jvm.VoidValue(), err
		}
	}
	if len(runtime.pendingTimers) >= maxPendingTimers {
		return jvm.VoidValue(), fmt.Errorf("KTF pending timer count exceeds %d", maxPendingTimers)
	}
	runtime.pendingTimers = append(runtime.pendingTimers, wipicTimer{
		task:   task,
		owner:  owner,
		period: timerDuration(period),
		due:    runtime.client.waitDeadline(timerDuration(delay)),
	})
	return jvm.VoidValue(), nil
}

// timerDuration clamps a Java millisecond delay the same way MC_knlSetTimer's
// is clamped: a negative one is now, and an absurd one is an hour.
func timerDuration(milliseconds int64) time.Duration {
	if milliseconds <= 0 {
		return 0
	}
	return time.Duration(min(milliseconds, maxTimerDelayMillis)) * time.Millisecond
}

// runtimeTimerCancel drops every task this Timer scheduled. A task already
// running is not interrupted, which is what the specification says.
func runtimeTimerCancel(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Timer.cancel expected receiver")
	}
	owner, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.dropTimerTasks(func(timer wipicTimer) bool { return timer.owner == owner })
	return jvm.VoidValue(), nil
}

// runtimeTimerTaskCancel drops this one task and reports whether it had a run
// still to come.
func runtimeTimerTaskCancel(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("TimerTask.cancel expected receiver")
	}
	task, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	dropped := runtime.dropTimerTasks(func(timer wipicTimer) bool { return timer.task == task })
	if dropped == 0 {
		return jvm.IntValue(0), nil
	}
	return jvm.IntValue(1), nil
}

// runtimeTimerTaskScheduledTime answers when the task's most recent run was
// due, in the guest's own epoch milliseconds.
func runtimeTimerTaskScheduledTime(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("TimerTask.scheduledExecutionTime expected receiver")
	}
	return jvm.LongValue(runtime.guestMillis()), nil
}

// dropTimerTasks removes the pending Java timers a predicate selects and
// reports how many went.
func (runtime *initializationRuntime) dropTimerTasks(match func(wipicTimer) bool) int {
	remaining := runtime.pendingTimers[:0]
	dropped := 0
	for _, timer := range runtime.pendingTimers {
		if timer.task != nil && match(timer) {
			dropped++
			continue
		}
		remaining = append(remaining, timer)
	}
	runtime.pendingTimers = remaining
	return dropped
}
