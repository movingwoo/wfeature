package jvm

import (
	"bytes"
	"fmt"
	"testing"
)

// classImplementing is a class file with no members, the superclass and the
// interfaces the caller names, and the access flags it gives.
func classImplementing(name, super string, flags uint16, interfaces ...string) []byte {
	names := append([]string{name, super}, interfaces...)
	var out bytes.Buffer
	writeTestU2(&out, 0xcafe)
	writeTestU2(&out, 0xbabe)
	writeTestU2(&out, 0)
	writeTestU2(&out, 49)
	// Each name is a Utf8 entry and the Class entry after it.
	writeTestU2(&out, uint16(2*len(names)+1))
	for index, text := range names {
		out.WriteByte(1)
		writeTestU2(&out, uint16(len(text)))
		out.WriteString(text)
		out.WriteByte(7)
		writeTestU2(&out, uint16(2*index+1))
	}
	writeTestU2(&out, flags)
	writeTestU2(&out, 2)
	writeTestU2(&out, 4)
	writeTestU2(&out, uint16(len(interfaces)))
	for index := range interfaces {
		writeTestU2(&out, uint16(2*(index+2)+2))
	}
	for range 3 { // fields, methods, attributes
		writeTestU2(&out, 0)
	}
	return out.Bytes()
}

// isInstanceWithSets is the walk isInstance made with a fresh slice and map
// for every call, kept as the answer the allocation-free walk has to give.
func isInstanceWithSets(vm *VM, object *Object, target string) bool {
	if object.ClassName == target || target == "java/lang/Object" {
		return true
	}
	if len(object.ClassName) > 0 && object.ClassName[0] == '[' {
		return target == "java/lang/Cloneable" || target == "java/io/Serializable"
	}
	pending := []string{object.ClassName}
	seen := map[string]bool{object.ClassName: true}
	relations := 0
	for next := 0; next < len(pending); next++ {
		if next >= vm.config.MaxFrames {
			return false
		}
		class, err := vm.loader.Load(pending[next])
		if err != nil {
			continue
		}
		for index := -1; index < len(class.Interfaces); index++ {
			parent := class.SuperName
			if index >= 0 {
				parent = class.Interfaces[index]
			}
			if relations >= maxInstanceRelations {
				return false
			}
			relations++
			if parent == "" {
				continue
			}
			if parent == target {
				return true
			}
			if parent != ObjectClass && !seen[parent] {
				seen[parent] = true
				pending = append(pending, parent)
			}
		}
	}
	return false
}

// The walk behind instanceof and checkcast answers as it did when it kept its
// classes in a slice and a map made for each call: over a class with more
// interfaces than its buffer holds, interfaces met twice, interfaces that
// extend each other in a ring, a class missing from the archive, and a chain
// longer than the frame bound lets it walk.
func TestTheInstanceWalkAnswersAsItDidWithSets(t *testing.T) {
	const iface = 0x0601 // public, interface, abstract
	source := mapClassSource{}
	var wide []string
	for index := range 25 {
		name := fmt.Sprintf("I%d", index)
		wide = append(wide, name)
		var parents []string
		if index%5 == 4 {
			// Every fifth one extends two before it, so they are met twice.
			parents = []string{fmt.Sprintf("I%d", index-1), fmt.Sprintf("I%d", index-3)}
		}
		source[name] = classImplementing(name, ObjectClass, iface, parents...)
	}
	source["Wide"] = classImplementing("Wide", ObjectClass, 0x0021, wide...)
	source["Ring0"] = classImplementing("Ring0", ObjectClass, iface, "Ring1")
	source["Ring1"] = classImplementing("Ring1", ObjectClass, iface, "Ring0", "Deep")
	source["Ringed"] = classImplementing("Ringed", "Wide", 0x0021, "Ring0", "Missing")
	for index := range 40 {
		super := fmt.Sprintf("Chain%d", index+1)
		if index == 39 {
			super = ObjectClass
		}
		source[fmt.Sprintf("Chain%d", index)] = classImplementing(fmt.Sprintf("Chain%d", index), super, 0x0021, "Deep")
	}
	source["Deep"] = classImplementing("Deep", ObjectClass, iface, "I24")
	targets := append(append([]string{}, wide...), "Wide", "Ring0", "Ring1", "Ringed", "Deep", "Missing",
		"Chain0", "Chain20", "Chain39", "Unrelated", ObjectClass, "java/lang/Cloneable")
	objects := []string{"Wide", "Ringed", "Ring0", "I4", "I24", "Chain0", "Chain30", "Missing", "[LWide;"}
	// The classes load, and the walk reaches what it should.
	vm := New(source, Options{})
	for _, answer := range []struct {
		object, target string
		want           bool
	}{
		{"Wide", "I24", true}, {"Wide", "I17", true}, {"Wide", "Unrelated", false},
		{"Ringed", "Deep", true}, {"Ringed", "Missing", true}, {"Chain0", "Deep", true},
		{"Chain0", "Chain39", true}, {"I24", "I21", true}, {"I24", "I20", false},
	} {
		if got := vm.isInstance(&Object{ClassName: answer.object}, answer.target); got != answer.want {
			t.Fatalf("%s instanceof %s = %t, want %t", answer.object, answer.target, got, answer.want)
		}
	}
	vm.Close()
	for _, frames := range []int{0, 8, 30} {
		vm := New(source, Options{MaxFrames: frames})
		for _, className := range objects {
			object := &Object{ClassName: className}
			for _, target := range targets {
				if got, want := vm.isInstance(object, target), isInstanceWithSets(vm, object, target); got != want {
					t.Fatalf("frames %d: %s instanceof %s = %t, the walk with sets said %t", frames, className, target, got, want)
				}
			}
		}
		vm.Close()
	}
}
