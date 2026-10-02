package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

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
	previous   *nativeReturnScope
}

type nativeReturnCheckpoint struct {
	Active     bool
	Tag, Value uint32
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
	return runtime.registerNativeReturn(thread, tag, value), nil
}

func (runtime *initializationRuntime) registerNativeReturn(thread *armcore.Thread, tag, value uint32) *nativeReturnScope {
	if runtime.nativeReturnScopes == nil {
		runtime.nativeReturnScopes = make(map[*armcore.Thread]*nativeReturnScope)
	}
	scope := &nativeReturnScope{runtime: runtime, thread: thread, tag: tag, value: value, previous: runtime.nativeReturnScopes[thread]}
	runtime.nativeReturnScopes[thread] = scope
	return scope
}

func (runtime *initializationRuntime) captureNativeReturn(thread *armcore.Thread) (nativeReturnCheckpoint, error) {
	if runtime.client.module || runtime.exceptionContext == 0 {
		return nativeReturnCheckpoint{}, nil
	}
	scope := runtime.nativeReturnScopes[thread]
	if scope == nil || scope.previous != nil {
		return nativeReturnCheckpoint{}, fmt.Errorf("KTF native return has no single resumable scope")
	}
	return nativeReturnCheckpoint{Active: true, Tag: scope.tag, Value: scope.value}, nil
}

// resumeNativeReturn registers the saved cleanup without clearing the guest's
// current result. beginNativeReturn would erase an answer already published.
func (runtime *initializationRuntime) resumeNativeReturn(thread *armcore.Thread, saved nativeReturnCheckpoint) (*nativeReturnScope, error) {
	expected := !runtime.client.module && runtime.exceptionContext != 0
	if saved.Active != expected || (!saved.Active && (saved.Tag != 0 || saved.Value != 0)) || runtime.nativeReturnScopes[thread] != nil {
		return nil, fmt.Errorf("KTF native return state is incompatible")
	}
	if !saved.Active {
		return nil, nil
	}
	return runtime.registerNativeReturn(thread, saved.Tag, saved.Value), nil
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
	if scope.runtime.nativeReturnScopes[scope.thread] != scope {
		return
	}
	// These words are registered, permanently mapped runtime storage. Restore
	// the caller's result even on exceptions; nested calls must not publish
	// their own result as the enclosing native's answer.
	core, base := scope.runtime.client.core, scope.runtime.exceptionContext
	_ = core.SetThreadLocalWord(scope.thread, base+nativeReturnTagOffset, scope.tag)
	_ = core.SetThreadLocalWord(scope.thread, base+nativeReturnValueOffset, scope.value)
	if scope.previous == nil {
		delete(scope.runtime.nativeReturnScopes, scope.thread)
	} else {
		scope.runtime.nativeReturnScopes[scope.thread] = scope.previous
	}
}
