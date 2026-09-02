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
	if !ok && cleaned != name {
		// A relative name that its class's package does not answer is looked
		// up again at the root of the archive, first as the title wrote it and
		// then by its last element alone. The handset resolved a name from the
		// root whether or not it began with a slash: one title in a package of
		// its own asks for `tk/images/Map0.lbm`, which is exactly where the
		// entry is, and the strict reading turns it into `tk/tk/images/...`
		// and answers null — the title catches its own exception and paints
		// the null, so the session ends in `drawImage` several calls away from
		// the read that failed. Three sibling titles need the other form:
		// they load their tables with `Runtime.getRuntime().getClass().
		// getResourceAsStream("table.gft")`, where the strict reading asks
		// java/lang for a file that is plainly at the top of the JAR and the
		// class they ask through is a platform class with no package of its
		// own to hold a game's data. The strict answer still wins when it
		// finds something, so this only replaces a null.
		if data, ok = findEntry(runtime.Archive.Entries, path.Clean(name)); !ok {
			data, ok = findEntry(runtime.Archive.Entries, path.Base(cleaned))
		}
	}
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
