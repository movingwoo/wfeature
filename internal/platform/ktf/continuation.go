package ktf

import (
	"context"
	"errors"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// These records cover ARM calls whose pending Host remainder is known. They
// are one component of a checkpoint, not a worker or session state format.
type aotCallCheckpoint struct {
	ARM    armcore.CallFrame
	Native nativeReturnCheckpoint
	Wait   javaWaitCheckpoint
}

type javaWaitCheckpoint struct {
	Active     bool
	Class      string
	Name       string
	Descriptor string
	Receiver   uint32
}

type javaWaitScope struct {
	thread   *armcore.Thread
	method   runtimeJavaMethod
	receiver *jvm.Object
}

type workerAOTEntry struct {
	class        string
	method       jvm.AOTMethodMetadata
	entryHandler uint32
	invocation   *jvm.Invocation
}

type workerAOTCheckpoint struct {
	Class        string
	Method       jvm.AOTMethodMetadata
	EntryHandler uint32
	Execution    jvm.NativeExecutionState
	Calls        []aotCallCheckpoint
}

func (runtime *initializationRuntime) captureWorkerContinuation(worker *guestWorker) (workerAOTCheckpoint, error) {
	if worker == nil || worker.aotEntry == nil || worker.aotEntry.invocation == nil {
		return workerAOTCheckpoint{}, fmt.Errorf("KTF worker has no recorded AOT/JVM entry")
	}
	entry := worker.aotEntry
	if entry.method.Name != "run" || entry.method.Descriptor != "()V" || entry.method.AccessFlags&8 != 0 {
		return workerAOTCheckpoint{}, fmt.Errorf("KTF worker entry has no supported result remainder")
	}
	calls, err := runtime.captureAOTCalls(worker)
	if err != nil {
		return workerAOTCheckpoint{}, err
	}
	represented := 0
	if calls[len(calls)-1].Wait.Active {
		represented = 1
	}
	execution, err := entry.invocation.CaptureNativeState(represented)
	if err != nil {
		return workerAOTCheckpoint{}, err
	}
	return workerAOTCheckpoint{Class: entry.class, Method: entry.method, EntryHandler: entry.entryHandler, Execution: execution, Calls: calls}, nil
}

func (runtime *initializationRuntime) resumeWorkerContinuation(ctx context.Context, worker *guestWorker, saved workerAOTCheckpoint) error {
	if worker == nil || worker.javaThread == nil || worker.armThread == nil || runtime.client.activeWorker != worker || ctx == nil {
		return fmt.Errorf("resume KTF worker without its execution grant")
	}
	if err := runtime.validateWorkerContinuation(saved); err != nil {
		return err
	}
	_, err := runtime.client.vm.ResumeNativeExecution(saved.Execution, func(invocation *jvm.Invocation) (jvm.Value, error) {
		previousContext := runtime.currentContext
		ctx = context.WithValue(ctx, jvmInvocationKey{}, invocation)
		runtime.currentContext = ctx
		defer func() { runtime.currentContext = previousContext }()
		previousEntry := worker.aotEntry
		worker.aotEntry = &workerAOTEntry{class: saved.Class, method: saved.Method, entryHandler: saved.EntryHandler, invocation: invocation}
		defer func() { worker.aotEntry = previousEntry }()
		if err := runtime.enterAOTCall(); err != nil {
			return jvm.VoidValue(), err
		}
		defer runtime.leaveAOTCall(runtime.aotCallOwner())
		run, callErr := runtime.resumeAOTCalls(ctx, worker.armThread, saved.Calls)
		methodType := jvm.MethodDescriptor{Return: jvm.Type{Kind: jvm.TypeVoid}}
		result, _, err := runtime.completeAOTMethod(ctx, worker.armThread, methodType, saved.EntryHandler, run, callErr)
		var uncaught *UncaughtAOTException
		if errors.As(err, &uncaught) && uncaught.Exception != nil {
			return jvm.VoidValue(), uncaught.Exception
		}
		return result, err
	})
	return err
}

func (runtime *initializationRuntime) validateWorkerContinuation(saved workerAOTCheckpoint) error {
	class, ok := runtime.client.vm.AOTClass(saved.Class)
	if !ok {
		return fmt.Errorf("KTF restored worker class is missing")
	}
	method, found, err := runtime.client.vm.FindAOTMethod(class.Address, "run", "()V")
	if err != nil {
		return err
	}
	if !found || method != saved.Method || method.AccessFlags&8 != 0 || saved.Method.Name != "run" || saved.Method.Descriptor != "()V" {
		return fmt.Errorf("KTF restored worker method differs from its entry")
	}
	if err := runtime.validateAOTCalls(saved.Calls); err != nil {
		return err
	}
	return runtime.client.vm.ValidateNativeExecutionState(saved.Execution)
}

func isCheckpointWait(method runtimeJavaMethod) bool {
	return method.class == "java/lang/Thread" && method.name == "sleep" && method.descriptor == "(J)V" && method.accessFlags&8 != 0 ||
		method.class == jvm.ObjectClass && method.name == "wait" && method.accessFlags&8 == 0 &&
			(method.descriptor == "()V" || method.descriptor == "(J)V" || method.descriptor == "(JI)V")
}

// captureAOTCalls requires the caller to hold client.run between worker grants.
// A refused capture does not change the live worker or its pending operation.
func (runtime *initializationRuntime) captureAOTCalls(worker *guestWorker) ([]aotCallCheckpoint, error) {
	if worker == nil || worker.armThread == nil {
		return nil, fmt.Errorf("capture KTF calls without a guest worker")
	}
	var saved []aotCallCheckpoint
	_, err := runtime.client.core.InspectParkedCalls(worker.armThread, func(thread *armcore.Thread, frame armcore.CallFrame) error {
		record := aotCallCheckpoint{ARM: frame}
		id := frame.Context.Registers[12]
		if frame.Stop == armcore.CallStoppedAtSupervisor {
			switch frame.SupervisorCall.Immediate {
			case svcCategoryJavaInterface:
				if id == javaSVCCallNative {
					var err error
					record.Native, err = runtime.captureNativeReturn(thread)
					if err != nil {
						return err
					}
				}
			case svcCategoryRuntimeJava:
				pending := worker.javaWait
				if pending == nil || pending.thread != thread {
					return fmt.Errorf("KTF runtime Java call has no recorded wait remainder")
				}
				record.Wait = javaWaitCheckpoint{Active: true, Class: pending.method.class, Name: pending.method.name, Descriptor: pending.method.descriptor}
				if pending.receiver != nil {
					address, ok := runtime.client.vm.AOTObjectAddress(pending.receiver)
					if !ok {
						return fmt.Errorf("KTF wait receiver has no guest identity")
					}
					record.Wait.Receiver = address
				}
			}
		}
		saved = append(saved, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := runtime.validateAOTCalls(saved); err != nil {
		return nil, err
	}
	return saved, nil
}

func (runtime *initializationRuntime) validateAOTCalls(saved []aotCallCheckpoint) error {
	if len(saved) == 0 || len(saved) > int(maxAOTCallDepth) {
		return fmt.Errorf("invalid KTF captured call count %d", len(saved))
	}
	for index, record := range saved {
		frame := record.ARM
		if err := frame.Validate(); err != nil {
			return err
		}
		if frame.End != ReturnAddress || frame.Budget != saved[0].ARM.Budget {
			return fmt.Errorf("KTF captured calls have incompatible return boundaries or budgets")
		}
		leaf := index == len(saved)-1
		if !leaf && saved[index+1].ARM.EntryStack != frame.Context.Registers[armcore.RegisterSP] {
			return fmt.Errorf("KTF captured child has an incompatible entry stack")
		}
		if frame.Stop == armcore.CallStoppedAtLimit {
			if !leaf || record.Native != (nativeReturnCheckpoint{}) || record.Wait != (javaWaitCheckpoint{}) {
				return fmt.Errorf("KTF instruction boundary has a pending Host remainder")
			}
			continue
		}
		id := frame.Context.Registers[12]
		switch frame.SupervisorCall.Immediate {
		case svcCategoryJavaInterface:
			if leaf || record.Wait != (javaWaitCheckpoint{}) {
				return fmt.Errorf("KTF AOT bridge has no child call")
			}
			switch id {
			case javaSVCJump1, javaSVCJump2, javaSVCJump3:
				if record.Native != (nativeReturnCheckpoint{}) {
					return fmt.Errorf("KTF jump contains a native return scope")
				}
			case javaSVCCallNative:
				expected := !runtime.client.module && runtime.exceptionContext != 0
				if record.Native.Active != expected || !expected && record.Native != (nativeReturnCheckpoint{}) {
					return fmt.Errorf("KTF native return scope is incompatible")
				}
				address := frame.Context.Registers[1]
				if address&3 != 0 {
					return fmt.Errorf("KTF captured native result is not aligned")
				}
				var data [8]byte
				if err := runtime.client.core.Memory().Read(address, data[:]); err != nil {
					return err
				}
			default:
				return fmt.Errorf("KTF Java interface call %d has no continuation", id)
			}
		case svcCategoryRuntimeJava:
			invocation, ok := runtime.nativeMethods[id]
			method, wait := invocation.method, record.Wait
			if !leaf || !ok || !isCheckpointWait(method) || !wait.Active || record.Native != (nativeReturnCheckpoint{}) ||
				wait.Class != method.class || wait.Name != method.name || wait.Descriptor != method.descriptor {
				return fmt.Errorf("KTF runtime Java call has no compatible wait remainder")
			}
			if method.accessFlags&8 != 0 {
				if wait.Receiver != 0 {
					return fmt.Errorf("KTF static wait has a receiver")
				}
			} else if object, ok := runtime.client.vm.AOTObjectAt(wait.Receiver); !ok || object == nil {
				return fmt.Errorf("KTF wait receiver is missing")
			}
		case svcCategoryModuleJump:
			if !runtime.client.module || record.Native != (nativeReturnCheckpoint{}) || record.Wait != (javaWaitCheckpoint{}) {
				return fmt.Errorf("KTF module call contains an incompatible remainder")
			}
			switch id {
			case moduleJumpInvoke, moduleJumpInvokeNative:
				if leaf {
					return fmt.Errorf("KTF module invocation has no child call")
				}
				sp := frame.Context.Registers[armcore.RegisterSP]
				if sp&3 != 0 || uint64(sp)+moduleJumpSpill*4 > uint64(frame.EntryStack) {
					return fmt.Errorf("KTF module invocation has an invalid argument spill")
				}
				if err := runtime.client.core.Memory().ValidateRange(sp, moduleJumpSpill*4, armcore.PermissionReadWrite); err != nil {
					return err
				}
			case moduleJumpWait:
				if !leaf {
					return fmt.Errorf("KTF module wait contains a child call")
				}
			default:
				return fmt.Errorf("KTF module jump %d has no continuation", id)
			}
		default:
			return fmt.Errorf("KTF supervisor category %d has no continuation", frame.SupervisorCall.Immediate)
		}
	}
	return nil
}

// resumeAOTCalls completes only the suspended operations. The caller restores
// memory, TLS and the worker deadline first, and supplies the root JVM/AOT
// entry remainder separately. All records are checked before execution starts.
func (runtime *initializationRuntime) resumeAOTCalls(ctx context.Context, parent *armcore.Thread, saved []aotCallCheckpoint) (armcore.RunSummary, error) {
	if err := runtime.validateAOTCalls(saved); err != nil {
		return armcore.RunSummary{}, err
	}
	return runtime.resumeAOTCall(ctx, parent, saved, 0)
}

func (runtime *initializationRuntime) resumeAOTCall(ctx context.Context, parent *armcore.Thread, saved []aotCallCheckpoint, index int) (armcore.RunSummary, error) {
	record := saved[index]
	complete := func(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
		previousThread, previousContext := runtime.currentThread, runtime.currentContext
		runtime.currentThread, runtime.currentContext = thread, ctx
		defer func() { runtime.currentThread, runtime.currentContext = previousThread, previousContext }()
		var result uint32
		var err error
		switch call.Immediate {
		case svcCategoryRuntimeJava:
			if runtime.saveReadError != nil {
				return runtime.saveReadError
			}
			method := runtime.nativeMethods[record.ARM.Context.Registers[12]].method
			if record.Wait.Receiver != 0 {
				receiver, _ := runtime.client.vm.AOTObject(record.Wait.Receiver)
				err = runtime.publishGuestFields(receiver, method)
			}
			if err == nil {
				result, err = runtime.completeRuntimeJavaResult(thread, method, jvm.Type{Kind: jvm.TypeVoid}, jvm.VoidValue())
			}
		case svcCategoryModuleJump:
			if record.ARM.Context.Registers[12] != moduleJumpWait {
				result, err = runtime.resumeModuleInvocation(ctx, thread, saved, index)
			}
		default:
			result, err = runtime.resumeAOTBridge(ctx, thread, saved, index)
		}
		var unwind *aotExceptionUnwind
		if errors.As(err, &unwind) {
			return runtime.resumeAOTException(thread, unwind)
		}
		if err != nil {
			return err
		}
		return thread.SetRegister(0, result)
	}
	return runtime.client.core.ResumeCall(ctx, parent, record.ARM, complete, runtime.handleSupervisorCall)
}

func (runtime *initializationRuntime) resumeModuleInvocation(ctx context.Context, thread *armcore.Thread, saved []aotCallCheckpoint, index int) (uint32, error) {
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall(runtime.aotCallOwner())
	summary, err := runtime.resumeAOTCall(ctx, thread, saved, index+1)
	if err != nil {
		return 0, err
	}
	return completeModuleMethod(thread, saved[index].ARM.Context.Registers[armcore.RegisterSP], summary)
}

func (runtime *initializationRuntime) resumeAOTBridge(ctx context.Context, thread *armcore.Thread, saved []aotCallCheckpoint, index int) (uint32, error) {
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall(runtime.aotCallOwner())
	record := saved[index]
	var scope *nativeReturnScope
	if record.ARM.Context.Registers[12] == javaSVCCallNative {
		var err error
		scope, err = runtime.resumeNativeReturn(thread, record.Native)
		if err != nil {
			return 0, err
		}
		defer scope.restore()
	}
	summary, err := runtime.resumeAOTCall(ctx, thread, saved, index+1)
	if err != nil {
		return 0, err
	}
	if record.ARM.Context.Registers[12] == javaSVCCallNative {
		return runtime.completeAOTNative(record.ARM.Context.Registers[0], record.ARM.Context.Registers[1], scope, summary)
	}
	return runtime.completeAOTJump(thread, summary)
}
