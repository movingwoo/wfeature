package midp

import "github.com/movingwoo/wfeature/internal/jvm"

// java.util.Timer and java.util.TimerTask, which are the profile's rather than
// the configuration's: CLDC has neither, and a MIDlet that wants work later
// gets it from here. They are declared beside the rest of the MIDP surface for
// that reason, and only a VM that installs this profile answers them.
//
// **A task runs on a thread of its own**, which is what the specification says
// and what a title depends on: one local title schedules a task at a fixed
// rate and keeps redrawing while it runs, so a task invoked on the caller's
// thread would stop the frame the schedule was made from.
//
// The thread is a guest thread rather than a goroutine, so it is the same
// thread a title's own `new Thread(...)` gets — it takes the platform's
// scheduler if one is installed, it dies with the session's step budget, and
// it shows up in a report as a thread rather than as something the runtime is
// doing behind the game's back.
const (
	TimerClass = "java/util/Timer"
	// TimerTaskClass is abstract: a task is always a title's own subclass, and
	// the run this invokes virtually is that subclass's.
	TimerTaskClass = "java/util/TimerTask"
	// TimerThreadClass is the runtime's own worker. It lives outside
	// java/util because no game names it — a game holds the Timer — and it
	// extends Thread so that starting it is the one Thread.start every other
	// guest thread goes through.
	TimerThreadClass = "net/wfeature/TimerThread"
)

func timerDefinitions() []jvm.ClassDefinition {
	const (
		task = "Ljava/util/TimerTask;"
		date = "Ljava/util/Date;"
	)
	return []jvm.ClassDefinition{
		{
			Name:      TimerClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "cancelled", Descriptor: "Z", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				{Name: "cancel", Descriptor: "()V", Access: jvm.AccessPublic, Body: timerCancel},
				{Name: "schedule", Descriptor: "(" + task + "J)V", Access: jvm.AccessPublic, Body: timerSchedule(false)},
				{Name: "schedule", Descriptor: "(" + task + "JJ)V", Access: jvm.AccessPublic, Body: timerSchedule(false)},
				{Name: "schedule", Descriptor: "(" + task + date + ")V", Access: jvm.AccessPublic, Body: timerScheduleAt(false)},
				{Name: "schedule", Descriptor: "(" + task + date + "J)V", Access: jvm.AccessPublic, Body: timerScheduleAt(false)},
				{Name: "scheduleAtFixedRate", Descriptor: "(" + task + "JJ)V", Access: jvm.AccessPublic, Body: timerSchedule(true)},
				{Name: "scheduleAtFixedRate", Descriptor: "(" + task + date + "J)V", Access: jvm.AccessPublic, Body: timerScheduleAt(true)},
			},
		},
		{
			Name:       TimerTaskClass,
			SuperName:  "java/lang/Object",
			Interfaces: []string{jvm.RunnableClass},
			Access:     jvm.AccessPublic | jvm.AccessAbstract,
			Fields: []jvm.FieldDefinition{
				{Name: "cancelled", Descriptor: "Z", Access: jvm.AccessPrivate},
				{Name: "scheduled", Descriptor: "J", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessProtected, Body: emptyInit},
				{Name: "run", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "cancel", Descriptor: "()Z", Access: jvm.AccessPublic, Body: timerTaskCancel},
				{Name: "scheduledExecutionTime", Descriptor: "()J", Access: jvm.AccessPublic, Body: timerTaskScheduledTime},
			},
		},
		{
			Name:      TimerThreadClass,
			SuperName: jvm.ThreadClass,
			Access:    jvm.AccessFinal,
			Fields: []jvm.FieldDefinition{
				{Name: "timer", Descriptor: "Ljava/util/Timer;", Access: jvm.AccessPrivate},
				{Name: "task", Descriptor: task, Access: jvm.AccessPrivate},
				{Name: "delay", Descriptor: "J", Access: jvm.AccessPrivate},
				{Name: "period", Descriptor: "J", Access: jvm.AccessPrivate},
				{Name: "fixedRate", Descriptor: "Z", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				{Name: "run", Descriptor: "()V", Access: jvm.AccessPublic, Body: timerThreadRun},
			},
		},
	}
}

// timerSchedule serves the two delay forms of both schedule and
// scheduleAtFixedRate; the period is the fourth argument when there is one.
func timerSchedule(fixedRate bool) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		timer, task, err := scheduleReceivers(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		delay, err := arguments[2].Int64()
		if err != nil {
			return jvm.VoidValue(), err
		}
		period := int64(0)
		if len(arguments) > 3 {
			if period, err = arguments[3].Int64(); err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.VoidValue(), startTimerThread(call, timer, task, delay, period, fixedRate)
	}
}

// timerScheduleAt is the same schedule against a wall-clock time rather than a
// delay. The two are one call underneath: a first time in the past is due now,
// which is what a Date already gone means to a handset that was switched off
// when it passed.
func timerScheduleAt(fixedRate bool) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		timer, task, err := scheduleReceivers(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		when, err := arguments[2].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if when == nil {
			return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "Timer.schedule first time is null")
		}
		firstTime, err := call.InvokeVirtual(when, "getTime", "()J")
		if err != nil {
			return jvm.VoidValue(), err
		}
		at, err := firstTime.Int64()
		if err != nil {
			return jvm.VoidValue(), err
		}
		now, err := guestMillis(call)
		if err != nil {
			return jvm.VoidValue(), err
		}
		delay := max(at-now, 0)
		period := int64(0)
		if len(arguments) > 3 {
			if period, err = arguments[3].Int64(); err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.VoidValue(), startTimerThread(call, timer, task, delay, period, fixedRate)
	}
}

// scheduleReceivers reads the Timer and the task every schedule form starts
// with, and refuses the two states the specification refuses a schedule in.
func scheduleReceivers(arguments []jvm.Value) (*jvm.Object, *jvm.Object, error) {
	if len(arguments) < 3 {
		return nil, nil, jvm.Throw("java/lang/IllegalArgumentException", "Timer.schedule takes a task")
	}
	timer, err := receiver(arguments)
	if err != nil {
		return nil, nil, err
	}
	task, err := arguments[1].Reference()
	if err != nil {
		return nil, nil, err
	}
	if task == nil {
		return nil, nil, jvm.Throw("java/lang/NullPointerException", "Timer.schedule task is null")
	}
	return timer, task, nil
}

// startTimerThread validates the schedule and puts one worker behind it. Each
// schedule gets its own thread, which is what makes a Timer with two repeating
// tasks behave as two: the specification's single timer thread would serialise
// them, and a title that measured that would be measuring an implementation
// rather than a contract.
func startTimerThread(call *jvm.Invocation, timer, task *jvm.Object, delay, period int64, fixedRate bool) error {
	if delay < 0 || period < 0 {
		return jvm.Throw("java/lang/IllegalArgumentException", "Timer.schedule takes a non-negative delay and period")
	}
	cancelled, err := booleanField(call, timer, TimerClass, "cancelled")
	if err != nil {
		return err
	}
	if cancelled {
		return jvm.Throw("java/lang/IllegalStateException", "Timer already cancelled")
	}
	cancelled, err = booleanField(call, task, TimerTaskClass, "cancelled")
	if err != nil {
		return err
	}
	if cancelled {
		return jvm.Throw("java/lang/IllegalStateException", "TimerTask already cancelled")
	}
	worker, err := call.NewObject(TimerThreadClass, "()V")
	if err != nil {
		return err
	}
	machine := call.VM()
	if err := machine.SetField(worker, TimerThreadClass, "timer", "Ljava/util/Timer;", jvm.ReferenceValue(timer)); err != nil {
		return err
	}
	if err := machine.SetField(worker, TimerThreadClass, "task", "Ljava/util/TimerTask;", jvm.ReferenceValue(task)); err != nil {
		return err
	}
	if err := machine.SetField(worker, TimerThreadClass, "delay", "J", jvm.LongValue(delay)); err != nil {
		return err
	}
	if err := machine.SetField(worker, TimerThreadClass, "period", "J", jvm.LongValue(period)); err != nil {
		return err
	}
	if err := setBooleanField(call, worker, TimerThreadClass, "fixedRate", fixedRate); err != nil {
		return err
	}
	_, err = call.InvokeVirtual(worker, "start", "()V")
	return err
}

// timerThreadRun is the worker: wait, run the task, and either stop or wait
// again. It reads both cancelled flags before every run rather than keeping a
// list of live tasks, so a cancel from any thread is seen without a lock.
//
// The difference between the two schedules is what the next wait is measured
// from. A fixed-rate schedule counts from when the run was due, so a run that
// started late is made up for by a shorter wait; a plain schedule counts from
// when the run finished, so a slow task spreads its own runs out. A title that
// animates on a fixed rate depends on the first, and one that polls depends on
// the second not piling work up behind it.
func timerThreadRun(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	worker, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	machine := call.VM()
	timer, err := objectField(call, worker, TimerThreadClass, "timer", "Ljava/util/Timer;")
	if err != nil {
		return jvm.VoidValue(), err
	}
	task, err := objectField(call, worker, TimerThreadClass, "task", "Ljava/util/TimerTask;")
	if err != nil {
		return jvm.VoidValue(), err
	}
	delayValue, err := machine.Field(worker, TimerThreadClass, "delay", "J")
	if err != nil {
		return jvm.VoidValue(), err
	}
	delay, err := delayValue.Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	periodValue, err := machine.Field(worker, TimerThreadClass, "period", "J")
	if err != nil {
		return jvm.VoidValue(), err
	}
	period, err := periodValue.Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	fixedRate, err := booleanField(call, worker, TimerThreadClass, "fixedRate")
	if err != nil {
		return jvm.VoidValue(), err
	}

	now, err := guestMillis(call)
	if err != nil {
		return jvm.VoidValue(), err
	}
	due := now + delay
	for {
		if err := sleepGuest(call, delay); err != nil {
			return jvm.VoidValue(), err
		}
		stop, err := timerStopped(call, timer, task)
		if err != nil || stop {
			return jvm.VoidValue(), err
		}
		if err := machine.SetField(task, TimerTaskClass, "scheduled", "J", jvm.LongValue(due)); err != nil {
			return jvm.VoidValue(), err
		}
		if _, err := call.InvokeVirtual(task, "run", "()V"); err != nil {
			return jvm.VoidValue(), err
		}
		if period == 0 {
			return jvm.VoidValue(), nil
		}
		if now, err = guestMillis(call); err != nil {
			return jvm.VoidValue(), err
		}
		if fixedRate {
			due += period
			delay = max(due-now, 0)
			continue
		}
		due, delay = now+period, period
	}
}

// timerStopped answers whether this worker has been called off, by its Timer
// or by its own task. A task already running is not interrupted, which is what
// the specification says cancel does and does not do.
func timerStopped(call *jvm.Invocation, timer, task *jvm.Object) (bool, error) {
	cancelled, err := booleanField(call, timer, TimerClass, "cancelled")
	if err != nil || cancelled {
		return true, err
	}
	return booleanField(call, task, TimerTaskClass, "cancelled")
}

func timerCancel(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	timer, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), setBooleanField(call, timer, TimerClass, "cancelled", true)
}

// timerTaskCancel answers whether this call is the one that stopped a task
// that was going to run again, which is what the specification's boolean is.
func timerTaskCancel(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	task, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	cancelled, err := booleanField(call, task, TimerTaskClass, "cancelled")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := setBooleanField(call, task, TimerTaskClass, "cancelled", true); err != nil {
		return jvm.VoidValue(), err
	}
	if cancelled {
		return jvm.IntValue(0), nil
	}
	return jvm.IntValue(1), nil
}

func timerTaskScheduledTime(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	task, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return call.VM().Field(task, TimerTaskClass, "scheduled", "J")
}

// sleepGuest waits the way a game waits. Thread.sleep is the platform's:
// it is scaled by the speed the session runs at and it is what a Host's
// scheduler suspends, so a timer keeps time with the title it belongs to
// rather than with the wall.
func sleepGuest(call *jvm.Invocation, milliseconds int64) error {
	if milliseconds <= 0 {
		return nil
	}
	_, err := call.InvokeStatic(jvm.ThreadClass, "sleep", "(J)V", jvm.LongValue(milliseconds))
	return err
}

func guestMillis(call *jvm.Invocation) (int64, error) {
	value, err := call.InvokeStatic(jvm.SystemClass, "currentTimeMillis", "()J")
	if err != nil {
		return 0, err
	}
	return value.Int64()
}

func objectField(call *jvm.Invocation, object *jvm.Object, className, name, descriptor string) (*jvm.Object, error) {
	value, err := call.VM().Field(object, className, name, descriptor)
	if err != nil {
		return nil, err
	}
	return value.Reference()
}

func booleanField(call *jvm.Invocation, object *jvm.Object, className, name string) (bool, error) {
	value, err := call.VM().Field(object, className, name, "Z")
	if err != nil {
		return false, err
	}
	number, err := value.Int32()
	if err != nil {
		return false, err
	}
	return number != 0, nil
}

func setBooleanField(call *jvm.Invocation, object *jvm.Object, className, name string, value bool) error {
	number := int32(0)
	if value {
		number = 1
	}
	return call.VM().SetField(object, className, name, "Z", jvm.IntValue(number))
}
