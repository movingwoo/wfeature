package jvm

import (
	"sort"
	"testing"
)

// A body with no declaration is reachable from a native dispatch and from
// nowhere else. Interpreted code resolves the method through the class, and so
// does a platform that links ahead of time — both find nothing and stop the
// title, while every test that calls the method directly keeps passing.
//
// Eleven members were in that state at once when this test was written, four
// of them ones a local archive had already stopped on: String.replace(CC),
// StringBuffer's long, boolean, object and char-array appends, its capacity
// constructor and setLength, String's char-array reads and its encoding-named
// getBytes, and Class.forName. Each had a working body behind it for as long
// as it had been unreachable.
func TestEveryCoreLibraryBodyIsDeclared(t *testing.T) {
	vm := New(nil, Options{})
	declared := make(map[methodKey]bool)
	published := make(map[string]bool)
	for _, definition := range CoreLibraryDefinitions() {
		published[definition.Name] = true
		for _, method := range definition.Methods {
			declared[methodKey{class: definition.Name, name: method.Name, descriptor: method.Descriptor}] = true
		}
	}
	var missing []string
	for key := range vm.builtinNatives {
		if !published[key.class] || declared[key] {
			continue
		}
		missing = append(missing, key.class+"."+key.name+key.descriptor)
	}
	sort.Strings(missing)
	for _, member := range missing {
		t.Errorf("%s has a native body and no declaration, so nothing can resolve it", member)
	}
}

// The other direction, with the one exception that is a design rather than an
// omission: a method the platform running underneath answers. Those two are
// declared here so a game can link them and registered by whichever platform
// is loaded, because the answer is that platform's rather than the library's.
func TestEveryCoreLibraryNativeDeclarationHasABody(t *testing.T) {
	platformAnswered := map[string]bool{
		ClassClass + ".getResourceAsStream(Ljava/lang/String;)Ljava/io/InputStream;": true,
		SystemClass + ".getProperty(Ljava/lang/String;)Ljava/lang/String;":           true,
	}
	vm := New(nil, Options{})
	for _, definition := range CoreLibraryDefinitions() {
		for _, method := range definition.Methods {
			if method.Access&AccessNative == 0 || method.Body != nil {
				continue
			}
			key := methodKey{class: definition.Name, name: method.Name, descriptor: method.Descriptor}
			if _, ok := vm.natives[key]; ok {
				continue
			}
			if _, ok := vm.contextNatives[key]; ok {
				continue
			}
			member := definition.Name + "." + method.Name + method.Descriptor
			if platformAnswered[member] {
				continue
			}
			t.Errorf("%s is declared native with no body and no platform to answer it", member)
		}
	}
}
