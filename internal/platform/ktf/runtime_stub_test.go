package ktf

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// An unimplemented method is loud: the guest call fails and the failure names
// it. A stub is silent — it answers a fixed value the game then believes. That
// makes stubs the harder half of the API surface to keep honest, so the ones
// this runtime has are inventoried here and reviewed as a list rather than
// discovered one crash at a time.
//
// testdata/wipi_java_stubs.txt is that inventory. Adding a stub means adding a
// line; making a method real means removing one.

const stubInventoryPath = "testdata/wipi_java_stubs.txt"

// fixedValueImplementations are the shared bodies that answer without
// consulting any runtime state.
func fixedValueImplementations() map[uintptr]string {
	return map[uintptr]string{
		implementationIdentity(runtimeComponentZero):  "zero",
		implementationIdentity(runtimeDisplayTrue):    "true",
		implementationIdentity(runtimeComponentNoop):  "noop",
		implementationIdentity(runtimeBackLightNoop):  "noop",
		implementationIdentity(runtimeNetworkConnect): "refuse",
	}
}

func implementationIdentity(implementation runtimeJavaImplementation) uintptr {
	return reflect.ValueOf(implementation).Pointer()
}

func TestFixedValueStubInventory(t *testing.T) {
	fixed := fixedValueImplementations()
	var found []string
	for name, definition := range runtimeJavaClasses {
		for _, method := range definition.methods {
			if method.implementation == nil {
				continue
			}
			kind, ok := fixed[implementationIdentity(method.implementation)]
			if !ok {
				continue
			}
			found = append(found, fmt.Sprintf("%s %s %s %s", name, method.name, method.descriptor, kind))
		}
	}
	sort.Strings(found)

	data, err := os.ReadFile(stubInventoryPath)
	if err != nil {
		t.Fatalf("read %s: %v", stubInventoryPath, err)
	}
	var recorded []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		recorded = append(recorded, line)
	}

	recordedSet := make(map[string]bool, len(recorded))
	for _, entry := range recorded {
		recordedSet[entry] = true
	}
	foundSet := make(map[string]bool, len(found))
	for _, entry := range found {
		foundSet[entry] = true
	}
	for _, entry := range found {
		if !recordedSet[entry] {
			t.Errorf("new fixed-value stub is not recorded in %s: %s", stubInventoryPath, entry)
		}
	}
	for _, entry := range recorded {
		if !foundSet[entry] {
			t.Errorf("%s records %s, which is no longer a fixed-value stub; remove the line", stubInventoryPath, entry)
		}
	}
	t.Logf("fixed-value stubs: %d", len(found))
}

// findRuntimeMethod locates one published method in the runtime metadata table.
func findRuntimeMethod(t *testing.T, class, name, descriptor string) runtimeJavaMethod {
	t.Helper()
	definition, ok := runtimeJavaClasses[class]
	if !ok {
		t.Fatalf("%s is not a runtime Java class", class)
	}
	for _, method := range definition.methods {
		if method.name == name && method.descriptor == descriptor {
			return method
		}
	}
	t.Fatalf("%s.%s%s is not published to guest AOT code", class, name, descriptor)
	return runtimeJavaMethod{}
}

func TestObjectPublishesTheMonitorMethodsAGameWaitsOn(t *testing.T) {
	// A game's loader thread parks on Object.wait while another thread reads
	// its data. Leaving these out of the metadata failed the guest's method
	// lookup and killed the session before the title screen.
	for _, descriptor := range []string{"()V", "(J)V", "(JI)V"} {
		method := findRuntimeMethod(t, "java/lang/Object", "wait", descriptor)
		if method.implementation == nil {
			// The JVM builtin checks a monitor ownership KTF never
			// establishes, so wait has to be answered here.
			t.Errorf("Object.wait%s delegates to the JVM monitor path", descriptor)
		}
	}
	for _, name := range []string{"notify", "notifyAll"} {
		findRuntimeMethod(t, "java/lang/Object", name, "()V")
	}
}

func TestInputStreamPublishesTheAbstractSingleByteRead(t *testing.T) {
	method := findRuntimeMethod(t, "java/io/InputStream", "read", "()I")
	if method.accessFlags&0x0400 == 0 {
		t.Errorf("InputStream.read()I access flags %#x do not mark it abstract", method.accessFlags)
	}
	// Every concrete stream has to declare the same name and descriptor, or it
	// takes a vtable slot of its own instead of overriding the one a caller
	// resolving through InputStream dispatches to.
	for _, class := range []string{"java/io/ByteArrayInputStream", "java/io/DataInputStream"} {
		findRuntimeMethod(t, class, "read", "()I")
	}
}

// A published method with no implementation of its own is a promise that the
// JVM already owns one. Nothing checks that promise at registration, so a
// descriptor that does not match the registered builtin — or a class published
// on the strength of a builtin that was never written — only fails when a game
// calls it, which is somewhere deep in a menu rather than at startup.
func TestDelegatingRuntimeMethodsHaveAJVMBody(t *testing.T) {
	client, _ := newTestRuntime(t)
	vm := client.JVM()
	for class, definition := range runtimeJavaClasses {
		for _, method := range definition.methods {
			if method.implementation != nil || method.accessFlags&0x0400 != 0 {
				// An own body answers for itself, and an abstract
				// declaration is answered by the override.
				continue
			}
			if !vm.HasMethodBody(method.class, method.name, method.descriptor) {
				t.Errorf("%s publishes %s.%s%s with no JVM body behind it", class, method.class, method.name, method.descriptor)
			}
		}
	}
}

// java/lang/Integer was missing from the table entirely, and because an unknown
// class still resolves to an empty record, the omission surfaced as a failed
// method lookup the first time a game opened a menu that reads a number. This
// walks the same path the guest does: resolve the class, then look the method
// up through it.
func TestIntegerResolvesTheParseAGuestMenuCalls(t *testing.T) {
	client, runtime := newTestRuntime(t)
	classAddress, err := runtime.ensureJavaClass("java/lang/Integer")
	if err != nil {
		t.Fatal(err)
	}
	method, found, err := client.JVM().FindAOTMethod(classAddress, "parseInt", "(Ljava/lang/String;)I")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Integer.parseInt(Ljava/lang/String;)I does not resolve from the guest's class record")
	}
	if method.Body == 0 {
		t.Fatal("Integer.parseInt(Ljava/lang/String;)I resolves to a null body")
	}
}

func TestAbstractRuntimeMethodsDispatchOnTheReceiver(t *testing.T) {
	// An abstract declaration has no body, so a non-virtual invoke of it fails
	// with "method has no code" rather than reaching the override.
	for class, definition := range runtimeJavaClasses {
		for _, method := range definition.methods {
			if method.accessFlags&0x0400 == 0 || method.implementation != nil {
				continue
			}
			if method.accessFlags&0x0008 != 0 {
				t.Errorf("%s.%s%s is both abstract and static", class, method.name, method.descriptor)
			}
		}
	}
}
