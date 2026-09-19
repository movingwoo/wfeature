package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const runtimeKernelClass = "org/kwis/msf/core/Kernel"

func runtimeKernelClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{name: runtimeKernelClass, superName: "java/lang/Object", accessFlags: 0x21, methods: []runtimeJavaMethod{
		{class: runtimeKernelClass, name: "load", descriptor: "(Ljava/lang/String;[Ljava/lang/String;)I", accessFlags: 0x9, implementation: runtimeKernelLoad},
		{class: runtimeKernelClass, name: "getExecNames", descriptor: "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)[Ljava/lang/String;", accessFlags: 0x9, implementation: runtimeKernelGetExecNames},
	}}
}

// Null filters select all installed entries. The local service publishes its
// dependency only when its middleware contract is recognized in this session.
func runtimeKernelGetExecNames(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("Kernel.getExecNames expected three filters")
	}
	type entry struct {
		identifier string
		properties [3]string
	}
	var entries []entry
	if runtime.client.programName != "" {
		entries = append(entries, entry{runtime.client.programName, [3]string{runtime.client.appProperties["NAME"], runtime.client.appProperties["VER"], runtime.client.appProperties["VDR"]}})
	}
	if runtime.hasSlotRelay() {
		entries = append(entries, entry{relayDependency, [3]string{relayDependency, relayDependencyVersion, ""}})
	}
	filters := [3]string{"*", "*", "*"}
	for index, arg := range arguments {
		object, err := arg.Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if object == nil {
			continue
		}
		text, ok := jvm.StringText(object)
		if !ok {
			return jvm.VoidValue(), fmt.Errorf("Kernel.getExecNames filter is not a string")
		}
		filters[index] = fmt.Sprintf("%+q", text)
		matching := entries[:0]
		for _, entry := range entries {
			if entry.properties[index] == text {
				matching = append(matching, entry)
			}
		}
		entries = matching
	}
	runtime.countDiagnostic(fmt.Sprintf("kernel executable lookup name=%s version=%s vendor=%s matches=%d", filters[0], filters[1], filters[2], len(entries)))
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: "java/lang/String"}, int32(len(entries)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	values := make([]jvm.Value, len(entries))
	for i, entry := range entries {
		values[i] = jvm.ReferenceValue(vm.NewString(entry.identifier))
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

// Kernel.load returns a program ID on success and a negative value on failure.
// The local provider has no guest entry point or arguments to execute.
func runtimeKernelLoad(runtime *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	if len(args) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Kernel.load expected two arguments")
	}
	object, err := args[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(object)
	if !ok {
		return jvm.IntValue(-1), nil
	}
	arguments, err := args[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if arguments != nil {
		component, count, ok := jvm.ArrayComponent(arguments)
		if !ok || component.Kind != jvm.TypeReference || component.ClassName != "java/lang/String" || count != 0 {
			return jvm.IntValue(-1), nil
		}
	}
	if name == relayDependency && runtime.hasSlotRelay() {
		runtime.countDiagnostic("local slot relay loaded")
		return jvm.IntValue(2), nil
	}
	return jvm.IntValue(-1), nil
}
