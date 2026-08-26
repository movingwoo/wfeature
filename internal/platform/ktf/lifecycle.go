package ktf

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// ClassLoadSummary records the bounded guest call used to resolve one AOT
// class through ExeInterface.GetClass. Metadata is the validated JVM registry
// snapshot produced by the call.
type ClassLoadSummary struct {
	Run       armcore.RunSummary
	Metadata  jvm.AOTClassMetadata
	Callbacks InitializationCallbacks
}

// AOTCallSummary records every bounded ARM run needed to complete one Java
// method. More than one run is possible when a Java exception re-enters the
// guest's native stack-unwind restore function.
type AOTCallSummary struct {
	Method jvm.AOTMethodMetadata
	Runs   []armcore.RunSummary
	Result jvm.Value
}

// LoadClass calls the initialized client's real GetClass entry point. The
// guest may construct vtables and register metadata through SVC callbacks;
// the returned address is then independently parsed and checked against name.
func (client *Client) LoadClass(ctx context.Context, name string) (ClassLoadSummary, error) {
	if client == nil || client.core == nil || client.thread == nil || client.vm == nil {
		return ClassLoadSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	if err := validateClassName(name); err != nil {
		return ClassLoadSummary{}, err
	}

	client.run.Lock()
	defer client.run.Unlock()
	return client.loadClassLocked(ctx, name)
}

func (client *Client) loadClassLocked(ctx context.Context, name string) (ClassLoadSummary, error) {
	if client.runtime == nil {
		return ClassLoadSummary{}, fmt.Errorf("KTF client initialization has not completed")
	}
	// An older module publishes its classes rather than answering for them,
	// so the table this side already linked is the answer and there is no
	// guest function to call. See module_link.go.
	if client.module {
		address := client.runtime.moduleClassByName[name]
		if address == 0 {
			return ClassLoadSummary{}, fmt.Errorf("KTF AOT class not found: %s", name)
		}
		metadata, ok := client.vm.AOTClassAt(address)
		if !ok {
			return ClassLoadSummary{}, fmt.Errorf("KTF module class %s at %#x is not registered", name, address)
		}
		return ClassLoadSummary{Metadata: metadata, Callbacks: client.runtime.callbacks}, nil
	}
	if client.executable.Interface.Functions.GetClass == 0 {
		return ClassLoadSummary{}, fmt.Errorf("KTF client initialization has not completed")
	}
	if address := client.runtime.loadedClasses[name]; address != 0 {
		metadata, ok := client.vm.AOTClassAt(address)
		if !ok {
			return ClassLoadSummary{}, fmt.Errorf("cached KTF AOT class %s at %#x is not registered", name, address)
		}
		return ClassLoadSummary{Metadata: metadata, Callbacks: client.runtime.callbacks}, nil
	}

	nameAddress, err := client.runtime.allocateBytes(append([]byte(name), 0))
	if err != nil {
		return ClassLoadSummary{}, fmt.Errorf("allocate KTF class name %q: %w", name, err)
	}
	summary, err := client.core.Call(
		ctx,
		client.thread,
		client.executable.Interface.Functions.GetClass,
		ReturnAddress,
		[]uint32{nameAddress},
		client.runtime.handleSupervisorCall,
	)
	result := ClassLoadSummary{Run: summary, Callbacks: client.runtime.callbacks}
	if err != nil {
		return result, fmt.Errorf("load KTF AOT class %s: %w", name, err)
	}
	address := summary.Context.Registers[0]
	if address == 0 {
		return result, fmt.Errorf("KTF AOT class not found: %s", name)
	}
	metadata, err := client.runtime.resolveAOTClass(address)
	if err != nil {
		return result, fmt.Errorf("resolve loaded KTF AOT class %s at %#x: %w", name, address, err)
	}
	if metadata.Name != name {
		return result, fmt.Errorf("KTF GetClass(%s) returned %s at %#x", name, metadata.Name, address)
	}
	client.runtime.classes[name] = address
	client.runtime.loadedClasses[name] = address
	result.Metadata = metadata
	result.Callbacks = client.runtime.callbacks
	return result, nil
}

// NewObject allocates the paired guest/JVM representation of an AOT class and
// invokes its native-image constructor through the same bridge used by later
// lifecycle calls.
func (client *Client) NewObject(ctx context.Context, className, descriptor string, arguments ...jvm.Value) (*jvm.Object, AOTCallSummary, error) {
	if client == nil || client.core == nil || client.thread == nil || client.vm == nil {
		return nil, AOTCallSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	methodType, err := validateAOTArguments(descriptor, arguments)
	if err != nil {
		return nil, AOTCallSummary{}, fmt.Errorf("construct KTF AOT class %s%s: %w", className, descriptor, err)
	}
	if methodType.Return.Kind != jvm.TypeVoid {
		return nil, AOTCallSummary{}, fmt.Errorf("KTF AOT constructor descriptor must return void: %s", descriptor)
	}

	client.run.Lock()
	defer client.run.Unlock()
	loaded, err := client.loadClassLocked(ctx, className)
	if err != nil {
		return nil, AOTCallSummary{}, err
	}
	method, found, err := client.vm.FindAOTMethod(loaded.Metadata.Address, "<init>", descriptor)
	if err != nil {
		return nil, AOTCallSummary{}, fmt.Errorf("resolve KTF AOT constructor %s%s: %w", className, descriptor, err)
	}
	if !found || method.AccessFlags&0x0008 != 0 {
		return nil, AOTCallSummary{}, fmt.Errorf("KTF AOT constructor not found: %s%s", className, descriptor)
	}
	address, object, err := client.runtime.allocateAOTInstance(loaded.Metadata.Address)
	if err != nil {
		return nil, AOTCallSummary{}, fmt.Errorf("allocate KTF AOT object %s: %w", className, err)
	}
	raw, err := client.aotArguments(append([]jvm.Value{jvm.ReferenceValue(object)}, arguments...), append([]jvm.Type{{Kind: jvm.TypeReference, ClassName: className}}, methodType.Parameters...))
	if err != nil {
		return nil, AOTCallSummary{}, err
	}
	if bound, ok := client.vm.AOTObject(address); !ok || bound != object {
		return nil, AOTCallSummary{}, fmt.Errorf("KTF AOT object %s at %#x lost its JVM binding", className, address)
	}
	summary, err := client.invokeAOTMethodLocked(ctx, method, methodType, raw)
	if err != nil {
		return nil, summary, fmt.Errorf("construct KTF AOT class %s%s: %w", className, descriptor, err)
	}
	return object, summary, nil
}

// InvokeVirtual invokes a non-static AOT method on an object previously bound
// by this client.
func (client *Client) InvokeVirtual(ctx context.Context, receiver *jvm.Object, name, descriptor string, arguments ...jvm.Value) (AOTCallSummary, error) {
	if client == nil || client.core == nil || client.thread == nil || client.vm == nil {
		return AOTCallSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	if receiver == nil {
		return AOTCallSummary{}, fmt.Errorf("invoke KTF AOT %s%s on null reference", name, descriptor)
	}
	methodType, err := validateAOTArguments(descriptor, arguments)
	if err != nil {
		return AOTCallSummary{}, err
	}

	client.run.Lock()
	defer client.run.Unlock()
	metadata, ok := client.vm.AOTClass(receiver.ClassName)
	if !ok {
		loaded, loadErr := client.loadClassLocked(ctx, receiver.ClassName)
		if loadErr != nil {
			return AOTCallSummary{}, loadErr
		}
		metadata = loaded.Metadata
	}
	method, found, err := client.vm.FindAOTMethod(metadata.Address, name, descriptor)
	if err != nil {
		return AOTCallSummary{}, err
	}
	if !found || method.AccessFlags&0x0008 != 0 {
		return AOTCallSummary{}, fmt.Errorf("KTF AOT instance method not found: %s.%s%s", receiver.ClassName, name, descriptor)
	}
	raw, err := client.aotArguments(append([]jvm.Value{jvm.ReferenceValue(receiver)}, arguments...), append([]jvm.Type{{Kind: jvm.TypeReference, ClassName: receiver.ClassName}}, methodType.Parameters...))
	if err != nil {
		return AOTCallSummary{}, err
	}
	summary, err := client.invokeAOTMethodLocked(ctx, method, methodType, raw)
	if err != nil {
		return summary, fmt.Errorf("invoke KTF AOT %s.%s%s: %w", receiver.ClassName, name, descriptor, err)
	}
	return summary, nil
}

// InvokeStatic invokes a static method declared in a loaded AOT class.
func (client *Client) InvokeStatic(ctx context.Context, className, name, descriptor string, arguments ...jvm.Value) (AOTCallSummary, error) {
	if client == nil || client.core == nil || client.thread == nil || client.vm == nil {
		return AOTCallSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	methodType, err := validateAOTArguments(descriptor, arguments)
	if err != nil {
		return AOTCallSummary{}, err
	}
	client.run.Lock()
	defer client.run.Unlock()
	loaded, err := client.loadClassLocked(ctx, className)
	if err != nil {
		return AOTCallSummary{}, err
	}
	method, found, err := client.vm.FindAOTMethod(loaded.Metadata.Address, name, descriptor)
	if err != nil {
		return AOTCallSummary{}, err
	}
	if !found || method.AccessFlags&0x0008 == 0 {
		return AOTCallSummary{}, fmt.Errorf("KTF AOT static method not found: %s.%s%s", className, name, descriptor)
	}
	raw, err := client.aotArguments(arguments, methodType.Parameters)
	if err != nil {
		return AOTCallSummary{}, err
	}
	summary, err := client.invokeAOTMethodLocked(ctx, method, methodType, raw)
	if err != nil {
		return summary, fmt.Errorf("invoke KTF AOT %s.%s%s: %w", className, name, descriptor, err)
	}
	return summary, nil
}

func validateAOTArguments(descriptor string, arguments []jvm.Value) (jvm.MethodDescriptor, error) {
	methodType, err := jvm.ParseMethodDescriptor(descriptor)
	if err != nil {
		return jvm.MethodDescriptor{}, err
	}
	if len(arguments) != len(methodType.Parameters) {
		return jvm.MethodDescriptor{}, fmt.Errorf("expected %d arguments, got %d", len(methodType.Parameters), len(arguments))
	}
	for index, typeInfo := range methodType.Parameters {
		if _, err := aotValueWords(arguments[index], typeInfo, nil); err != nil {
			return jvm.MethodDescriptor{}, fmt.Errorf("argument %d: %w", index, err)
		}
	}
	return methodType, nil
}

func (client *Client) aotArguments(values []jvm.Value, types []jvm.Type) ([]uint32, error) {
	if len(values) != len(types) {
		return nil, fmt.Errorf("KTF AOT argument type count %d does not match value count %d", len(types), len(values))
	}
	words := make([]uint32, 0, len(values)+1)
	for index, typeInfo := range types {
		valueWords, err := aotValueWords(values[index], typeInfo, client.vm)
		if err != nil {
			return nil, fmt.Errorf("KTF AOT argument %d: %w", index, err)
		}
		words = append(words, valueWords...)
	}
	return words, nil
}

func aotValueWords(value jvm.Value, typeInfo jvm.Type, vm *jvm.VM) ([]uint32, error) {
	switch typeInfo.Kind {
	case jvm.TypeVoid:
		if value.Kind() != jvm.ValueVoid {
			return nil, fmt.Errorf("value is %s, not void", value.Kind())
		}
		return nil, nil
	case jvm.TypeBoolean, jvm.TypeByte, jvm.TypeChar, jvm.TypeShort, jvm.TypeInt:
		integer, err := value.Int32()
		if err != nil {
			return nil, err
		}
		return []uint32{uint32(integer)}, nil
	case jvm.TypeLong:
		integer, err := value.Int64()
		if err != nil {
			return nil, err
		}
		return []uint32{uint32(integer), uint32(uint64(integer) >> 32)}, nil
	case jvm.TypeFloat:
		floating, err := value.Float32()
		if err != nil {
			return nil, err
		}
		return []uint32{math.Float32bits(floating)}, nil
	case jvm.TypeDouble:
		floating, err := value.Float64()
		if err != nil {
			return nil, err
		}
		bits := math.Float64bits(floating)
		return []uint32{uint32(bits), uint32(bits >> 32)}, nil
	case jvm.TypeReference, jvm.TypeArray:
		object, err := value.Reference()
		if err != nil {
			return nil, err
		}
		if object == nil {
			return []uint32{0}, nil
		}
		if vm == nil {
			return []uint32{1}, nil
		}
		address, ok := vm.AOTAddress(object)
		if !ok {
			return nil, fmt.Errorf("reference %s has no KTF guest address", object.ClassName)
		}
		return []uint32{address}, nil
	default:
		return nil, fmt.Errorf("invalid KTF AOT argument type %s", typeInfo.Descriptor())
	}
}

func (client *Client) invokeAOTMethodLocked(ctx context.Context, method jvm.AOTMethodMetadata, methodType jvm.MethodDescriptor, rawArguments []uint32) (AOTCallSummary, error) {
	summary := AOTCallSummary{Method: method, Result: jvm.VoidValue()}
	if client.runtime == nil {
		return summary, fmt.Errorf("KTF client initialization has not completed")
	}
	result, runs, err := client.runtime.runAOTMethod(ctx, client.thread, method, methodType, rawArguments)
	summary.Runs = runs
	if err != nil {
		return summary, err
	}
	summary.Result = result
	return summary, nil
}

// runAOTMethod executes one AOT method body on the supplied guest thread,
// entering the normal or native-container form, re-entering restore stubs for
// caught exceptions, and restoring the handler head after an uncaught one.
func (runtime *initializationRuntime) runAOTMethod(ctx context.Context, thread *armcore.Thread, method jvm.AOTMethodMetadata, methodType jvm.MethodDescriptor, rawArguments []uint32) (jvm.Value, []armcore.RunSummary, error) {
	var runs []armcore.RunSummary
	if err := runtime.enterAOTCall(); err != nil {
		return jvm.VoidValue(), runs, err
	}
	defer runtime.leaveAOTCall()

	headAddress := runtime.exceptionHead()
	entryHandler, err := runtime.client.core.ThreadLocalWord(thread, headAddress)
	if err != nil {
		return jvm.VoidValue(), runs, fmt.Errorf("read KTF AOT entry exception handler: %w", err)
	}
	body := method.Body
	arguments := append([]uint32{0}, rawArguments...)
	runtime.traceAOTArgumentShape(method, methodType, arguments)
	if method.AccessFlags&0x0100 != 0 {
		body = method.NativeBody
		if body == 0 {
			return jvm.VoidValue(), runs, fmt.Errorf("KTF AOT native method body is null")
		}
		container, allocationErr := runtime.allocateWords(rawArguments)
		if allocationErr != nil {
			return jvm.VoidValue(), runs, fmt.Errorf("allocate KTF AOT native argument container: %w", allocationErr)
		}
		arguments = []uint32{0, container}
	} else if body == 0 {
		return jvm.VoidValue(), runs, fmt.Errorf("KTF AOT method body is null")
	}

	for {
		run, callErr := runtime.client.core.Call(ctx, thread, body, ReturnAddress, arguments, runtime.handleSupervisorCall)
		runs = append(runs, run)
		if callErr == nil {
			result, resultErr := aotResultValue(methodType.Return, run.Context.Registers[0], run.Context.Registers[1], runtime.client.vm)
			return result, runs, resultErr
		}
		var unwind *aotExceptionUnwind
		if errors.As(callErr, &unwind) && runtime.ownsUnwind(thread, unwind) {
			body = unwind.nextPC
			arguments = []uint32{unwind.contextBase, unwind.target}
			continue
		}
		var uncaught *UncaughtAOTException
		if errors.As(callErr, &uncaught) {
			if restoreErr := runtime.client.core.SetThreadLocalWord(thread, headAddress, entryHandler); restoreErr != nil {
				return jvm.VoidValue(), runs, fmt.Errorf("restore KTF AOT entry exception handler after %w: %v", callErr, restoreErr)
			}
		}
		// A guest fault is where a session ends, and the address it names is
		// never the answer on its own. The registers at the faulting
		// instruction are, so they travel with the error and into the
		// diagnostics — the same evidence a guest throw already carries.
		if fault, isFault := guestFault(callErr); isFault {
			if report := runtime.describeGuestFault(run.Context, fault); report != "" {
				runtime.countDiagnostic("guest fault " + report)
				return jvm.VoidValue(), runs, fmt.Errorf("%w: %s", callErr, report)
			}
		}
		return jvm.VoidValue(), runs, callErr
	}
}

func aotResultValue(typeInfo jvm.Type, low, high uint32, vm *jvm.VM) (jvm.Value, error) {
	switch typeInfo.Kind {
	case jvm.TypeVoid:
		return jvm.VoidValue(), nil
	case jvm.TypeBoolean, jvm.TypeByte, jvm.TypeChar, jvm.TypeShort, jvm.TypeInt:
		return jvm.IntValue(int32(low)), nil
	case jvm.TypeLong:
		return jvm.LongValue(int64(uint64(low) | uint64(high)<<32)), nil
	case jvm.TypeFloat:
		return jvm.FloatValue(math.Float32frombits(low)), nil
	case jvm.TypeDouble:
		return jvm.DoubleValue(math.Float64frombits(uint64(low) | uint64(high)<<32)), nil
	case jvm.TypeReference, jvm.TypeArray:
		if low == 0 {
			return jvm.ReferenceValue(nil), nil
		}
		object, ok := vm.AOTObject(low)
		if !ok {
			return jvm.VoidValue(), fmt.Errorf("KTF AOT method returned unbound reference %#x", low)
		}
		return jvm.ReferenceValue(object), nil
	default:
		return jvm.VoidValue(), fmt.Errorf("invalid KTF AOT return type %s", typeInfo.Descriptor())
	}
}
