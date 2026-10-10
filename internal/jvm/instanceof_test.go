package jvm

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/InterfaceIdentity*.class
var interfaceIdentityClasses embed.FS

func defineInstanceClasses(t *testing.T, vm *VM, definitions ...ClassDefinition) {
	t.Helper()
	for _, definition := range definitions {
		if err := vm.DefineClass(definition); err != nil {
			t.Fatalf("define %s: %v", definition.Name, err)
		}
	}
}

func TestIsInstanceInterfaceHierarchy(t *testing.T) {
	vm := New(nil, Options{})
	t.Cleanup(vm.Close)
	defineInstanceClasses(t, vm,
		ClassDefinition{Name: "test/Root", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Left", SuperName: ObjectClass, Interfaces: []string{"test/Root"}, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Right", SuperName: ObjectClass, Interfaces: []string{"test/Root"}, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Diamond", SuperName: ObjectClass, Interfaces: []string{"test/Left", "test/Right"}, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Other", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Base", SuperName: ObjectClass, Interfaces: []string{"test/Diamond"}, Access: AccessPublic},
		ClassDefinition{Name: "test/Child", SuperName: "test/Base", Access: AccessPublic},
		ClassDefinition{Name: "test/Unrelated", SuperName: ObjectClass, Access: AccessPublic},
	)
	for _, test := range []struct {
		class, target string
		want          bool
	}{
		{"test/Base", "test/Diamond", true},
		{"test/Base", "test/Left", true},
		{"test/Base", "test/Right", true},
		{"test/Base", "test/Root", true},
		{"test/Child", "test/Diamond", true},
		{"test/Child", "test/Left", true},
		{"test/Child", "test/Root", true},
		{"test/Child", "test/Base", true},
		{"test/Child", ObjectClass, true},
		{"test/Child", "test/Other", false},
		{"test/Child", "test/Unrelated", false},
		{"test/Unrelated", "test/Root", false},
	} {
		t.Run(test.class+" to "+test.target, func(t *testing.T) {
			if got := vm.IsInstance(&Object{ClassName: test.class}, test.target); got != test.want {
				t.Fatalf("IsInstance(%s, %s) = %t, want %t", test.class, test.target, got, test.want)
			}
		})
	}
}

func TestIsInstancePreservesNullAndArrayRules(t *testing.T) {
	vm := New(nil, Options{})
	t.Cleanup(vm.Close)
	for _, target := range []string{ObjectClass, "[B", "java/lang/Cloneable", "java/io/Serializable"} {
		if vm.IsInstance(nil, target) {
			t.Fatalf("null is an instance of %s", target)
		}
		if !vm.IsInstance(NewByteArray(nil), target) {
			t.Fatalf("byte array lost existing instance relation to %s", target)
		}
	}
	for _, target := range []string{"[I", "[Ljava/lang/Object;", "java/lang/Runnable", "missing/Type"} {
		if vm.IsInstance(NewByteArray(nil), target) {
			t.Fatalf("byte array acquired an unrelated instance relation to %s", target)
		}
	}
}

func TestInterfaceIdentityBytecode(t *testing.T) {
	entries, err := interfaceIdentityClasses.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	source := make(mapClassSource)
	for _, entry := range entries {
		data, err := interfaceIdentityClasses.ReadFile("testdata/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source[strings.TrimSuffix(entry.Name(), ".class")] = data
	}
	vm := New(source, Options{})
	t.Cleanup(vm.Close)
	for _, test := range []struct {
		method, descriptor string
		want               int32
	}{
		{"transitiveInstance", "()Z", 1},
		{"inheritedInstance", "()Z", 1},
		{"castAndDispatch", "()I", 44},
		{"inheritedDispatch", "()I", 11},
		{"rejectsUnrelatedCast", "()Z", 1},
		{"nullRelations", "()Z", 1},
	} {
		t.Run(test.method, func(t *testing.T) {
			value, err := vm.InvokeStatic("InterfaceIdentity", test.method, test.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			got, err := value.Int32()
			if err != nil || got != test.want {
				t.Fatalf("%s = %d, %v; want %d", test.method, got, err, test.want)
			}
		})
	}
}

func TestIsInstanceTypeLoadBudget(t *testing.T) {
	for _, limit := range []int{3, 4} {
		vm := New(nil, Options{MaxFrames: limit})
		t.Cleanup(vm.Close)
		defineInstanceClasses(t, vm, ClassDefinition{Name: "test/Entry", SuperName: ObjectClass, Interfaces: []string{"test/Chain0"}, Access: AccessPublic})
		for index := range 4 {
			definition := ClassDefinition{Name: fmt.Sprintf("test/Chain%d", index), SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract}
			if index < 3 {
				definition.Interfaces = []string{fmt.Sprintf("test/Chain%d", index+1)}
			}
			defineInstanceClasses(t, vm, definition)
		}
		// Entry, Chain0, Chain1 and Chain2 must be read to discover Chain3.
		if got := vm.IsInstance(&Object{ClassName: "test/Entry"}, "test/Chain3"); got != (limit == 4) {
			t.Fatalf("type-load limit %d returned %t", limit, got)
		}
	}
}

func TestIsInstanceDiamondGraphDoesNotRevisitTypes(t *testing.T) {
	vm := New(nil, Options{MaxFrames: 26})
	t.Cleanup(vm.Close)
	defineInstanceClasses(t, vm, ClassDefinition{Name: "test/Root", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract})
	parent := "test/Root"
	for level := range 8 {
		left, right, join := fmt.Sprintf("test/Left%d", level), fmt.Sprintf("test/Right%d", level), fmt.Sprintf("test/Join%d", level)
		defineInstanceClasses(t, vm,
			ClassDefinition{Name: left, SuperName: ObjectClass, Interfaces: []string{parent}, Access: AccessPublic | AccessInterface | AccessAbstract},
			ClassDefinition{Name: right, SuperName: ObjectClass, Interfaces: []string{parent}, Access: AccessPublic | AccessInterface | AccessAbstract},
			ClassDefinition{Name: join, SuperName: ObjectClass, Interfaces: []string{left, right}, Access: AccessPublic | AccessInterface | AccessAbstract})
		parent = join
	}
	defineInstanceClasses(t, vm, ClassDefinition{Name: "test/Entry", SuperName: ObjectClass, Interfaces: []string{parent}, Access: AccessPublic})
	if !vm.IsInstance(&Object{ClassName: "test/Entry"}, "test/Root") {
		t.Fatal("26 distinct types were exhausted by revisiting shared diamond ancestors")
	}
}

func TestIsInstanceRelationBudgetCountsRepeatedAndEmptyEdges(t *testing.T) {
	const relationLimit = 1 << 16
	for _, edge := range []string{"test/Leaf", ""} {
		for _, extra := range []int{0, 1} {
			t.Run(fmt.Sprintf("edge=%q/extra=%d", edge, extra), func(t *testing.T) {
				vm := New(nil, Options{MaxFrames: 4})
				t.Cleanup(vm.Close)
				interfaces := make([]string, relationLimit-1+extra)
				for index := range interfaces {
					interfaces[index] = edge
				}
				interfaces[len(interfaces)-1] = "test/Target"
				defineInstanceClasses(t, vm,
					ClassDefinition{Name: "test/Leaf", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
					ClassDefinition{Name: "test/Target", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
					ClassDefinition{Name: "test/Entry", SuperName: ObjectClass, Interfaces: interfaces, Access: AccessPublic})
				// The superclass occupies one relationship slot, even though
				// Object requires no load. Empty/repeated names still cost scans.
				if got := vm.IsInstance(&Object{ClassName: "test/Entry"}, "test/Target"); got != (extra == 0) {
					t.Fatalf("relationship budget with %d slots returned %t", len(interfaces)+1, got)
				}
			})
		}
	}
	for _, extra := range []int{0, 1} {
		vm := New(nil, Options{MaxFrames: 4})
		t.Cleanup(vm.Close)
		first, second := make([]string, relationLimit/2-1), make([]string, relationLimit/2-1+extra)
		for index := range first {
			first[index] = "test/Branch"
		}
		for index := range second {
			second[index] = "test/Leaf"
		}
		second[len(second)-1] = "test/Target"
		defineInstanceClasses(t, vm,
			ClassDefinition{Name: "test/Leaf", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
			ClassDefinition{Name: "test/Target", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
			ClassDefinition{Name: "test/Branch", SuperName: ObjectClass, Interfaces: second, Access: AccessPublic | AccessInterface | AccessAbstract},
			ClassDefinition{Name: "test/Entry", SuperName: ObjectClass, Interfaces: first, Access: AccessPublic})
		if got := vm.IsInstance(&Object{ClassName: "test/Entry"}, "test/Target"); got != (extra == 0) {
			t.Fatalf("aggregate relationship count %d returned %t", len(first)+len(second)+2, got)
		}
	}
}

func TestIsInstanceMissingBranchDoesNotHideKnownAncestor(t *testing.T) {
	vm := New(nil, Options{})
	t.Cleanup(vm.Close)
	defineInstanceClasses(t, vm,
		ClassDefinition{Name: "test/Root", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Branch", SuperName: ObjectClass, Interfaces: []string{"test/Root"}, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Entry", SuperName: ObjectClass, Interfaces: []string{"missing/Type", "test/Branch"}, Access: AccessPublic})
	if !vm.IsInstance(&Object{ClassName: "test/Entry"}, "test/Root") {
		t.Fatal("an unavailable unrelated branch hid a known interface ancestor")
	}
	if vm.IsInstance(&Object{ClassName: "test/Entry"}, "test/Unrelated") || vm.IsInstance(&Object{ClassName: "missing/Type"}, "test/Root") {
		t.Fatal("missing metadata established an unrelated type relation")
	}
}

func TestIsInstanceMalformedCyclesTerminate(t *testing.T) {
	const childMarker = "WFEATURE_TEST_INSTANCE_CYCLE_CHILD"
	if os.Getenv(childMarker) != "1" {
		// A regression to unbounded superclass traversal must fail without
		// leaving a spinning goroutine behind in the rest of the test suite.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIsInstanceMalformedCyclesTerminate$", "-test.count=1")
		command.Env = append(os.Environ(), childMarker+"=1")
		output, err := command.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatal("malformed hierarchy traversal did not terminate")
		}
		if err != nil {
			t.Fatalf("malformed hierarchy subprocess: %v\n%s", err, output)
		}
		return
	}
	vm := New(nil, Options{MaxFrames: 16})
	t.Cleanup(vm.Close)
	defineInstanceClasses(t, vm,
		ClassDefinition{Name: "test/Root", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/CycleA", SuperName: "test/CycleB", Access: AccessPublic},
		ClassDefinition{Name: "test/CycleB", SuperName: "test/CycleA", Interfaces: []string{"test/Root"}, Access: AccessPublic},
		ClassDefinition{Name: "test/InterfaceA", SuperName: ObjectClass, Interfaces: []string{"test/InterfaceB"}, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/InterfaceB", SuperName: ObjectClass, Interfaces: []string{"test/InterfaceA", "test/Root"}, Access: AccessPublic | AccessInterface | AccessAbstract},
		ClassDefinition{Name: "test/Entry", SuperName: ObjectClass, Interfaces: []string{"test/InterfaceA"}, Access: AccessPublic})
	for _, class := range []string{"test/CycleA", "test/Entry"} {
		if !vm.IsInstance(&Object{ClassName: class}, "test/Root") {
			t.Fatalf("cycle traversal lost reachable Root from %s", class)
		}
		if vm.IsInstance(&Object{ClassName: class}, "test/Unrelated") {
			t.Fatalf("cycle traversal invented unrelated ancestry for %s", class)
		}
	}
}
