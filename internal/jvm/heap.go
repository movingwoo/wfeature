package jvm

import (
	"sort"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

// Heap inspection. Everything here reads what a running VM holds without
// running any of it, which is what an attached debugger or cheat engine needs:
// the layout a class declares, the roots a walk of the object graph starts
// from, and field access by the key the interpreter itself uses.
//
// It is separate from the interpreter's own field paths because those take a
// resolved `classfile.Reference` and answer a guest exception. A tool holding
// an object it found by walking has neither, and turning a missing field into
// a guest exception would be an exception raised by nothing the guest did.

// HeapField is one field in a class's layout: where the interpreter keeps it,
// and what type it holds.
type HeapField struct {
	// Class is the class that declared the field, which is part of the key —
	// a subclass may declare a field of the same name.
	Class      string
	Name       string
	Descriptor string
	Type       Type
	// Static reports whether the field belongs to the class rather than to an
	// instance of it.
	Static bool
}

// Key is how the interpreter names this field in an object's field map, and
// how VM.StaticField names it in the class's.
func (field HeapField) Key() string {
	return field.Class + "." + field.Name + ":" + field.Descriptor
}

// InstanceLayout reports the instance fields of className and of every class
// it inherits from, supers first and in declaration order within each class.
//
// It is read from the declarations rather than from an object's field map,
// because that map holds only the fields that have been written: a class with
// an `int` nobody has assigned yet still has the field, and a tool that only
// saw assigned fields would find state appear and move as a game ran.
func (vm *VM) InstanceLayout(className string) []HeapField {
	return vm.layout(className, false)
}

// StaticLayout reports the static fields className declares. Statics are not
// inherited into a subclass's own storage — they belong to the class that
// declared them — so this does not walk the chain.
func (vm *VM) StaticLayout(className string) []HeapField {
	return vm.layout(className, true)
}

// maxLayoutDepth bounds the superclass walk. A class file names its own
// superclass and the loader will follow a cycle forever.
const maxLayoutDepth = 64

func (vm *VM) layout(className string, static bool) []HeapField {
	if vm == nil || className == "" {
		return nil
	}
	var chain []*classfile.Class
	seen := map[string]bool{}
	for name, depth := className, 0; name != "" && depth < maxLayoutDepth; depth++ {
		if seen[name] {
			break
		}
		seen[name] = true
		class, err := vm.loader.Load(name)
		if err != nil || class == nil {
			break
		}
		chain = append(chain, class)
		if static {
			// Only the declaring class's own statics are wanted, and every
			// loaded class is asked separately.
			break
		}
		name = class.SuperName
	}
	var fields []HeapField
	// Supers first, so an object's layout starts where the object's own
	// storage conceptually does and a subclass's fields follow.
	for index := len(chain) - 1; index >= 0; index-- {
		class := chain[index]
		for _, member := range class.Fields {
			if (member.AccessFlags&AccessStatic != 0) != static {
				continue
			}
			fieldType, err := ParseFieldDescriptor(member.Descriptor)
			if err != nil {
				continue
			}
			fields = append(fields, HeapField{
				Class:      class.Name,
				Name:       member.Name,
				Descriptor: member.Descriptor,
				Type:       fieldType,
				Static:     static,
			})
		}
	}
	return fields
}

// StaticFieldValue reads a static field without initializing its class, and
// reports whether the class has ever been given one.
//
// **The initializing read is the wrong one for a tool.** VM.StaticField runs
// `<clinit>` first, because that is what a getstatic does; a walk of the heap
// that touched every loaded class's statics through it would run guest code the
// game never asked to run, from whichever goroutine happened to be inspecting.
// A class that has not initialized has no values to report, and zero is what
// its fields hold.
func (vm *VM) StaticFieldValue(class, name, descriptor string) (Value, bool) {
	if vm == nil {
		return VoidValue(), false
	}
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	value, ok := vm.statics[fieldKey{class: class, name: name, descriptor: descriptor}]
	return value, ok
}

// SetStaticFieldValue writes a static field without initializing its class,
// for the same reason. A class that initializes afterwards runs its `<clinit>`
// over the top, which is what would have happened to a value the game itself
// had written that early.
func (vm *VM) SetStaticFieldValue(class, name, descriptor string, value Value) error {
	if vm == nil {
		return nil
	}
	fieldType, err := ParseFieldDescriptor(descriptor)
	if err != nil {
		return err
	}
	if err := validateValue(value, fieldType); err != nil {
		return err
	}
	vm.mu.Lock()
	vm.statics[fieldKey{class: class, name: name, descriptor: descriptor}] = value
	vm.mu.Unlock()
	return nil
}

// LoadedClasses names every class the loader has resolved, sorted. It is the
// set a tool sweeping statics has to cover, because a static lives in a class
// rather than in anything reachable from an object.
func (vm *VM) LoadedClasses() []string {
	if vm == nil {
		return nil
	}
	loaded, _ := vm.loader.Census()
	return loaded
}

// HeapRoots are the references a walk of the object graph starts from: every
// static field holding one, and every thread object the VM knows. A platform
// adds whatever it holds itself — a MIDlet, the screen it is showing — because
// those are reachable from the platform's Go state rather than from the guest's.
func (vm *VM) HeapRoots() []*Object {
	if vm == nil {
		return nil
	}
	var roots []*Object
	for _, className := range vm.LoadedClasses() {
		for _, field := range vm.StaticLayout(className) {
			if !field.Type.IsReference() {
				continue
			}
			value, ok := vm.StaticFieldValue(field.Class, field.Name, field.Descriptor)
			if !ok {
				continue
			}
			if reference, err := value.Reference(); err == nil && reference != nil {
				roots = append(roots, reference)
			}
		}
	}
	roots = append(roots, vm.ThreadObjects()...)
	return roots
}

// ThreadObjects lists the java.lang.Thread objects the VM is holding state
// for, including the main thread. A thread's own fields are as much game state
// as any other object's, and the objects a running thread holds are reachable
// only through it.
func (vm *VM) ThreadObjects() []*Object {
	if vm == nil {
		return nil
	}
	vm.threadMu.Lock()
	defer vm.threadMu.Unlock()
	objects := make([]*Object, 0, len(vm.threads)+1)
	if vm.mainThread != nil {
		objects = append(objects, vm.mainThread)
	}
	for thread := range vm.threads {
		if thread != vm.mainThread {
			objects = append(objects, thread)
		}
	}
	// A map's order changes between walks, and a tool that lays objects out in
	// the order it met them would move them between passes.
	sort.Slice(objects, func(left, right int) bool {
		return vm.objectIdentity(objects[left]) < vm.objectIdentity(objects[right])
	})
	return objects
}

// Identity is the stable number this VM gave an object, assigned on first ask.
// It is what lets a tool remember an object across passes without holding a
// reference that would keep a dead one alive.
func (vm *VM) Identity(object *Object) uint32 {
	return vm.objectIdentity(object)
}

// FieldValue reads one field by the key the interpreter stores it under. The
// second result reports whether the object has ever been given that field;
// a field that has not been written holds its type's zero, which the caller
// knows from the layout and this cannot answer.
func (object *Object) FieldValue(key string) (Value, bool) {
	if object == nil {
		return VoidValue(), false
	}
	object.fieldMu.RLock()
	defer object.fieldMu.RUnlock()
	value, ok := object.Fields[key]
	return value, ok
}

// SetFieldValue writes one field by that same key. Unlike the interpreter's
// path it does not check the value against a descriptor: the caller reads the
// descriptor from the layout and builds the value for it, and a tool writing
// through here is deliberately outside the game's own type discipline.
func (object *Object) SetFieldValue(key string, value Value) {
	if object == nil {
		return
	}
	object.fieldMu.Lock()
	defer object.fieldMu.Unlock()
	if object.Fields == nil {
		object.Fields = make(map[string]Value)
	}
	object.Fields[key] = value
}

// ArrayRange copies count elements of a guest array starting at offset. It is
// ArraySnapshot for a caller that wants a window rather than the whole array,
// which is what reading a range of a synthetic address space is.
func ArrayRange(object *Object, offset, count int) ([]Value, error) {
	array, err := objectArray(object)
	if err != nil {
		return nil, err
	}
	return array.LoadRange(offset, count)
}

// SetArrayElement writes one element, validated against the component type.
func SetArrayElement(object *Object, index int, value Value) error {
	array, err := objectArray(object)
	if err != nil {
		return err
	}
	if err := validateValue(value, array.Component); err != nil {
		return err
	}
	return array.Store(index, value)
}
