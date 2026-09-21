package ktf

import "github.com/movingwoo/wfeature/internal/armcore"

// A native body in a newer KTF image can publish a 32-bit result in the
// environment instead of r0. It writes the value at 40, then tag 2 at 36.
// Other tags and the older module ABI retain register-return behavior.
const (
	nativeReturnTagOffset   = 36
	nativeReturnValueOffset = 40
	nativeReturnWordTag     = 2
)

type nativeReturnScope struct {
	runtime    *initializationRuntime
	thread     *armcore.Thread
	tag, value uint32
}

func (runtime *initializationRuntime) beginNativeReturn(thread *armcore.Thread) (*nativeReturnScope, error) {
	if runtime.client.module || runtime.exceptionContext == 0 {
		return nil, nil
	}
	core := runtime.client.core
	tag, err := core.ThreadLocalWord(thread, runtime.exceptionContext+nativeReturnTagOffset)
	if err != nil {
		return nil, err
	}
	value, err := core.ThreadLocalWord(thread, runtime.exceptionContext+nativeReturnValueOffset)
	if err != nil {
		return nil, err
	}
	if err := core.SetThreadLocalWord(thread, runtime.exceptionContext+nativeReturnTagOffset, 0); err != nil {
		return nil, err
	}
	return &nativeReturnScope{runtime: runtime, thread: thread, tag: tag, value: value}, nil
}

func (scope *nativeReturnScope) result(fallback uint32) (uint32, error) {
	if scope == nil {
		return fallback, nil
	}
	core, base := scope.runtime.client.core, scope.runtime.exceptionContext
	tag, err := core.ThreadLocalWord(scope.thread, base+nativeReturnTagOffset)
	if err != nil {
		return 0, err
	}
	if tag != nativeReturnWordTag {
		return fallback, nil
	}
	return core.ThreadLocalWord(scope.thread, base+nativeReturnValueOffset)
}

func (scope *nativeReturnScope) restore() {
	if scope == nil {
		return
	}
	// These words are registered, permanently mapped runtime storage. Restore
	// the caller's result even on exceptions; nested calls must not publish
	// their own result as the enclosing native's answer.
	core, base := scope.runtime.client.core, scope.runtime.exceptionContext
	_ = core.SetThreadLocalWord(scope.thread, base+nativeReturnTagOffset, scope.tag)
	_ = core.SetThreadLocalWord(scope.thread, base+nativeReturnValueOffset, scope.value)
}
