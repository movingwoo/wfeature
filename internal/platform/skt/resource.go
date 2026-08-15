package skt

import (
	"fmt"
	"path"
	"strings"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func (runtime *Runtime) getResourceAsStream(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	classObject, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if classObject == nil || classObject.ClassName != jvm.ClassClass {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Class receiver is null")
	}
	className, ok := classObject.Native.(string)
	if !ok || className == "" {
		return jvm.VoidValue(), fmt.Errorf("Class object has no guest class name")
	}
	name, err := javaString(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "resource name is null")
	}
	resourceName := name
	if strings.HasPrefix(resourceName, "/") {
		resourceName = strings.TrimPrefix(resourceName, "/")
	} else if packageEnd := strings.LastIndex(className, "/"); packageEnd >= 0 {
		resourceName = className[:packageEnd+1] + resourceName
	}
	cleaned := path.Clean(resourceName)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return jvm.ReferenceValue(nil), nil
	}
	data, ok := findEntry(runtime.Archive.Entries, cleaned)
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	array := jvm.NewByteArray(data)
	stream, err := vm.NewObject(jvm.ByteArrayInputStreamClass, "([B)V", jvm.ReferenceValue(array))
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("create resource stream for %q: %w", cleaned, err)
	}
	return jvm.ReferenceValue(stream), nil
}
