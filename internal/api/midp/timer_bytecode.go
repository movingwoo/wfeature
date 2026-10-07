package midp

import (
	_ "embed"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The independent JVM worker uses bytecode so a parked task retains every
// local and its exact return point. Definitions still supplies the existing Go
// body to platforms whose AOT scheduler owns their workers.
//
//go:embed java/net/wfeature/TimerThread.class
var timerThreadBytecode []byte

func defineBytecodeTimer(vm *jvm.VM) error {
	if err := vm.DefineClassFile(timerThreadBytecode); err != nil {
		return err
	}
	if err := vm.RegisterContextNative(TimerThreadClass, "stopped", "(Ljava/util/Timer;Ljava/util/TimerTask;)Z", func(call *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
		timer, err := args[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		task, err := args[1].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		stopped, err := timerStopped(call, timer, task)
		if stopped {
			return jvm.IntValue(1), err
		}
		return jvm.IntValue(0), err
	}); err != nil {
		return err
	}
	if err := vm.RegisterContextNative(TimerThreadClass, "scheduled", "(Ljava/util/TimerTask;J)V", func(call *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
		task, err := args[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), call.VM().SetField(task, TimerTaskClass, "scheduled", "J", args[1])
	}); err != nil {
		return err
	}
	return vm.RegisterContextNative(TimerThreadClass, "failed", "(Ljava/lang/InterruptedException;)V", func(_ *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
		object, err := args[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), &jvm.GuestException{Object: object}
	})
}
