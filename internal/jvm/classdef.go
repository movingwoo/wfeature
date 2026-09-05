package jvm

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

// Access flags a definition shares with the class file format. The interpreter
// reads two of them — static decides which invoke path a method takes, and
// synchronized decides whether it holds a monitor — and the rest describe the
// class the way its class file would have, which is what the surface a game
// links against is checked and documented from.
const (
	AccessPublic       uint16 = 0x0001
	AccessPrivate      uint16 = 0x0002
	AccessProtected    uint16 = 0x0004
	AccessStatic       uint16 = 0x0008
	AccessFinal        uint16 = 0x0010
	AccessSynchronized uint16 = 0x0020
	AccessNative       uint16 = 0x0100
	AccessInterface    uint16 = 0x0200
	AccessAbstract     uint16 = 0x0400
)

// ContextMethod is a method body written in Go. It is handed the execution it
// was entered on rather than only the VM, because a library method that calls
// back into guest code has to stay on that execution: the bytecode body it
// replaces did, and monitors and the step budget are counted per execution.
type ContextMethod func(call *Invocation, arguments []Value) (Value, error)

// ClassDefinition declares a runtime-owned class in Go. It carries what a
// class file carried — the name, the chain, the members a game links against —
// plus the Go body of every method the runtime implements itself.
//
// The runtime owns these classes, so nothing is gained by shipping them as
// bytecode the interpreter then has to run: the metadata is the part guest code
// needs and the bodies are faster and easier to debug in Go. A method declared
// without a body is either abstract or is filled in later by a platform through
// RegisterNative, which is how a surface whose behavior depends on the Host —
// a screen, a save directory, a clock — is kept out of this layer.
type ClassDefinition struct {
	Name       string
	SuperName  string
	Interfaces []string
	Access     uint16
	Fields     []FieldDefinition
	Methods    []MethodDefinition
}

// FieldDefinition declares one field. Instance fields need no storage here —
// an object holds them by name — so what this adds is the static ones, which
// the VM has to know about before any code touches them, and the constants a
// game reads straight out of the class.
type FieldDefinition struct {
	Name       string
	Descriptor string
	Access     uint16
	// Constant is what a static field starts at, the class file's
	// ConstantValue. A void value means the field starts at its type's zero.
	// Use StringValue for a string constant.
	Constant Value
}

// StringValue is a string constant for a static final field. It is a template
// rather than the object a game reads: strings are objects, and every VM gets
// its own when the class initializes, because identity, the monitor and any
// binding a platform gives the object all belong to one VM.
func StringValue(text string) Value {
	return ReferenceValue(&Object{ClassName: StringClass, Native: text})
}

// MethodDefinition declares one method. A nil Body means the class publishes
// the signature without implementing it here: an abstract method the game
// overrides, or one a platform registers a native for.
type MethodDefinition struct {
	Name       string
	Descriptor string
	Access     uint16
	// Throws names the checked exceptions the method declares, the class
	// file's Exceptions attribute. Nothing at run time reads it — the VM
	// propagates whatever a body raises — but it is part of the surface a game
	// was compiled against, and it is what lets a fixture be compiled against
	// this runtime at all. See internal/tools/javastub.
	Throws []string
	Body   ContextMethod
}

// Invocation is the running execution a Go method body was entered on. Calling
// guest code back through it keeps that code on the same execution, so a
// monitor the caller already holds is not entered twice and the caller's step
// budget keeps counting down.
type Invocation struct {
	vm    *VM
	state *execution
}

// VM answers the runtime the call is running on, for the services that do not
// depend on the current execution.
func (call *Invocation) VM() *VM { return call.vm }

// WaitAsGuestThread waits out a duration on the guest thread this call is
// running on, and reports whether it waited. It is for a platform call the
// handset answered by not returning until something finished — a sound played
// to its end, say — where the game's own loop is written around the wait.
//
// **A call that is not on a guest thread does not wait and reports false.**
// The Host's pass through a paint, a key or a lifecycle callback is not a
// thread the game owns: blocking it stops the screen, the input and the timers
// together, which is not what the handset's wait did. A caller that gets false
// has to answer without waiting.
//
// The wait ends early on `until`, which a caller closes when what it is waiting
// for is over, and on an interrupt — either way the wait is reported as having
// happened, because the thing being waited for is finished as far as the caller
// is concerned.
func (call *Invocation) WaitAsGuestThread(duration time.Duration, until <-chan struct{}) bool {
	if call == nil || call.state == nil || call.state.thread == nil {
		return false
	}
	state := call.vm.threadState(call.state.thread)
	// A non-positive duration is a wait with no deadline of its own: it ends
	// when `until` says the thing is over, or when the thread is interrupted.
	// A caller with nothing to end it must not ask for one.
	var deadline <-chan time.Time
	if duration > 0 {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case <-deadline:
	case <-until:
	case <-state.wake:
	}
	return true
}

// InvokeVirtual dispatches on the receiver's own class, as invokevirtual does.
func (call *Invocation) InvokeVirtual(receiver *Object, name, descriptor string, arguments ...Value) (Value, error) {
	if receiver == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "invoke "+name+descriptor)
	}
	return call.vm.invokeInstance(call.state, receiver.ClassName, receiver, name, descriptor, arguments)
}

// InvokeSpecial dispatches at the named class rather than the receiver's, as
// invokespecial does for a superclass call.
func (call *Invocation) InvokeSpecial(receiver *Object, className, name, descriptor string, arguments ...Value) (Value, error) {
	return call.vm.invokeInstance(call.state, className, receiver, name, descriptor, arguments)
}

// InvokeStatic calls a static method on the current execution.
func (call *Invocation) InvokeStatic(className, name, descriptor string, arguments ...Value) (Value, error) {
	return call.vm.invokeStatic(call.state, className, name, descriptor, arguments)
}

// SetStaticField writes a static field on the current execution. A class
// initializer has to use this rather than the VM's own method: that one starts
// a fresh execution, which would wait for the initialization this call is
// already inside of.
func (call *Invocation) SetStaticField(className, name, descriptor string, value Value) error {
	return call.vm.setStaticValue(call.state, classfile.Reference{
		Kind:       classfile.FieldReference,
		Class:      className,
		Name:       name,
		Descriptor: descriptor,
	}, value)
}

// StaticField reads a static field on the current execution, the read to
// SetStaticField's write.
func (call *Invocation) StaticField(className, name, descriptor string) (Value, error) {
	return call.vm.staticValue(call.state, classfile.Reference{
		Kind:       classfile.FieldReference,
		Class:      className,
		Name:       name,
		Descriptor: descriptor,
	})
}

// NewObject creates an instance and runs the constructor on the current
// execution.
func (call *Invocation) NewObject(className, descriptor string, arguments ...Value) (*Object, error) {
	object, err := call.vm.newObject(call.state, className)
	if err != nil {
		return nil, err
	}
	if _, err := call.vm.invokeInstance(call.state, className, object, "<init>", descriptor, arguments); err != nil {
		return nil, err
	}
	return object, nil
}

// DefineClass installs a class this runtime declares in Go. It is the class
// file's replacement, so it has to be called before guest code can reach the
// class, and a second definition of the same name is a mistake rather than an
// override.
func (vm *VM) DefineClass(definition ClassDefinition) error {
	return vm.defineClass(definition, false)
}

// defineClass installs a definition, optionally marking its bodies as the
// runtime's own. A built-in body may be replaced by a platform later — KTF
// answers Class.getName from its own class records — while two platform
// registrations of one method stay an error.
func (vm *VM) defineClass(definition ClassDefinition, builtin bool) error {
	class, constants, err := buildDefinedClass(definition)
	if err != nil {
		return err
	}
	if err := vm.loader.Define(class); err != nil {
		return err
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()
	// A class that arrives now can be the one a field reference resolves to,
	// so what was worked out before it existed is thrown away.
	clear(vm.declaringFields)
	if len(constants) > 0 {
		if vm.definedConstants == nil {
			vm.definedConstants = make(map[string][]definedConstant)
		}
		vm.definedConstants[definition.Name] = constants
	}
	for _, method := range definition.Methods {
		if method.Body == nil {
			continue
		}
		key := methodKey{class: definition.Name, name: method.Name, descriptor: method.Descriptor}
		if _, exists := vm.natives[key]; exists {
			return fmt.Errorf("native method already registered: %s.%s%s", key.class, key.name, key.descriptor)
		}
		body := method.Body
		vm.natives[key] = nativeEntry{context: func(vm *VM, state *execution, arguments []Value) (Value, error) {
			return body(&Invocation{vm: vm, state: state}, arguments)
		}}
		if builtin {
			vm.builtinNatives[key] = true
		}
	}
	return nil
}

// definedConstant is one static field's starting value, kept per class because
// a definition has no constant pool for initializeStaticFields to read a
// ConstantValue attribute out of.
type definedConstant struct {
	field fieldKey
	value Value
}

// buildDefinedClass turns a definition into the class metadata the loader
// hands out and the static constants the VM seeds at class initialization.
func buildDefinedClass(definition ClassDefinition) (*classfile.Class, []definedConstant, error) {
	if definition.Name == "" {
		return nil, nil, fmt.Errorf("class definition has no name")
	}
	if definition.SuperName == "" && definition.Name != ObjectClass {
		return nil, nil, fmt.Errorf("class definition %s has no superclass", definition.Name)
	}
	if definition.SuperName == definition.Name {
		return nil, nil, fmt.Errorf("class definition %s extends itself", definition.Name)
	}

	class := &classfile.Class{
		MajorVersion: classfile.MaxSupportedMajorVersion,
		AccessFlags:  definition.Access,
		Name:         definition.Name,
		SuperName:    definition.SuperName,
		Interfaces:   append([]string(nil), definition.Interfaces...),
	}

	var constants []definedConstant
	seenFields := make(map[string]bool, len(definition.Fields))
	for _, field := range definition.Fields {
		if field.Name == "" {
			return nil, nil, fmt.Errorf("class definition %s has an unnamed field", definition.Name)
		}
		fieldType, err := ParseFieldDescriptor(field.Descriptor)
		if err != nil {
			return nil, nil, fmt.Errorf("field %s.%s: %w", definition.Name, field.Name, err)
		}
		if seenFields[field.Name+":"+field.Descriptor] {
			return nil, nil, fmt.Errorf("field declared twice: %s.%s", definition.Name, field.Name)
		}
		seenFields[field.Name+":"+field.Descriptor] = true
		class.Fields = append(class.Fields, classfile.Member{
			AccessFlags: field.Access,
			Name:        field.Name,
			Descriptor:  field.Descriptor,
		})
		if field.Constant.Kind() == ValueVoid {
			continue
		}
		if field.Access&AccessStatic == 0 {
			return nil, nil, fmt.Errorf("instance field has a constant: %s.%s", definition.Name, field.Name)
		}
		if err := validateValue(field.Constant, fieldType); err != nil {
			return nil, nil, fmt.Errorf("constant of %s.%s: %w", definition.Name, field.Name, err)
		}
		constants = append(constants, definedConstant{
			field: fieldKey{class: definition.Name, name: field.Name, descriptor: field.Descriptor},
			value: field.Constant,
		})
	}

	seenMethods := make(map[string]bool, len(definition.Methods))
	for _, method := range definition.Methods {
		if method.Name == "" {
			return nil, nil, fmt.Errorf("class definition %s has an unnamed method", definition.Name)
		}
		if _, err := ParseMethodDescriptor(method.Descriptor); err != nil {
			return nil, nil, fmt.Errorf("method %s.%s: %w", definition.Name, method.Name, err)
		}
		if seenMethods[method.Name+method.Descriptor] {
			return nil, nil, fmt.Errorf("method declared twice: %s.%s%s", definition.Name, method.Name, method.Descriptor)
		}
		seenMethods[method.Name+method.Descriptor] = true
		if method.Body != nil && method.Access&AccessAbstract != 0 {
			return nil, nil, fmt.Errorf("abstract method has a body: %s.%s%s", definition.Name, method.Name, method.Descriptor)
		}
		class.Methods = append(class.Methods, classfile.Member{
			AccessFlags: method.Access,
			Name:        method.Name,
			Descriptor:  method.Descriptor,
		})
	}
	return class, constants, nil
}

// seedDefinedConstants gives a defined class's static finals their values, the
// way initializeStaticFields reads them out of a class file's constant pool.
func (vm *VM) seedDefinedConstants(className string) {
	vm.mu.Lock()
	constants := vm.definedConstants[className]
	vm.mu.Unlock()
	for _, constant := range constants {
		value := constant.value
		if object, err := value.Reference(); err == nil && object != nil {
			if text, ok := StringText(object); ok {
				value = ReferenceValue(vm.NewString(text))
			}
		}
		vm.mu.Lock()
		vm.statics[constant.field] = value
		vm.mu.Unlock()
	}
}
