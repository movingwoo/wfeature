package jvm

import (
	"fmt"
	"math"
	"strings"
)

// The java/util half of the core library: the two collections and the
// generator CLDC has. Vector and Hashtable declare nearly every method
// synchronized, and they still are — a Go body of a synchronized method takes
// the receiver's monitor before it runs.

func vectorDefinition() ClassDefinition {
	sync := AccessPublic | AccessSynchronized
	return ClassDefinition{
		Name:      VectorClass,
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "elements", Descriptor: "[Ljava/lang/Object;", Access: AccessPrivate},
			{Name: "size", Descriptor: "I", Access: AccessPrivate},
			{Name: "capacityIncrement", Descriptor: "I", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: vectorInitDefault},
			{Name: "<init>", Descriptor: "(I)V", Access: AccessPublic, Body: vectorInitCapacity},
			{Name: "<init>", Descriptor: "(II)V", Access: AccessPublic, Body: vectorInit},
			{Name: "addElement", Descriptor: "(Ljava/lang/Object;)V", Access: sync, Body: vectorAddElement},
			{Name: "elementAt", Descriptor: "(I)Ljava/lang/Object;", Access: sync, Body: vectorElementAt},
			{Name: "indexOf", Descriptor: "(Ljava/lang/Object;)I", Access: sync, Body: vectorIndexOf},
			{Name: "insertElementAt", Descriptor: "(Ljava/lang/Object;I)V", Access: sync, Body: vectorInsertElementAt},
			{Name: "isEmpty", Descriptor: "()Z", Access: sync, Body: vectorIsEmpty},
			{Name: "contains", Descriptor: "(Ljava/lang/Object;)Z", Access: sync, Body: vectorContains},
			{Name: "lastIndexOf", Descriptor: "(Ljava/lang/Object;)I", Access: sync, Body: vectorLastIndexOf},
			{Name: "copyInto", Descriptor: "([Ljava/lang/Object;)V", Access: sync, Body: vectorCopyInto},
			{Name: "capacity", Descriptor: "()I", Access: sync, Body: vectorCapacity},
			{Name: "ensureCapacity", Descriptor: "(I)V", Access: sync, Body: vectorEnsureCapacity},
			{Name: "trimToSize", Descriptor: "()V", Access: sync, Body: vectorTrimToSize},
			{Name: "setSize", Descriptor: "(I)V", Access: sync, Body: vectorSetSize},
			{Name: "firstElement", Descriptor: "()Ljava/lang/Object;", Access: sync, Body: vectorFirstElement},
			{Name: "lastElement", Descriptor: "()Ljava/lang/Object;", Access: sync, Body: vectorLastElement},
			{Name: "setElementAt", Descriptor: "(Ljava/lang/Object;I)V", Access: sync, Body: vectorSetElementAt},
			{Name: "removeElement", Descriptor: "(Ljava/lang/Object;)Z", Access: sync, Body: vectorRemoveElement},
			{Name: "removeAllElements", Descriptor: "()V", Access: sync, Body: vectorRemoveAllElements},
			{Name: "removeElementAt", Descriptor: "(I)V", Access: sync, Body: vectorRemoveElementAt},
			{Name: "size", Descriptor: "()I", Access: sync, Body: vectorSize},
			// A title walks a vector with an Enumeration where a newer one
			// would use an index — the interface is what CLDC gives it and a
			// Hashtable already hands out the same view over the same class.
			{Name: "elements", Descriptor: "()Ljava/util/Enumeration;", Access: sync, Body: vectorElements},
			// The two searches from an index, and the text a vector makes of
			// itself, which is what String.valueOf and an append of it answer.
			{Name: "indexOf", Descriptor: "(Ljava/lang/Object;I)I", Access: sync, Body: vectorIndexOfFrom},
			{Name: "lastIndexOf", Descriptor: "(Ljava/lang/Object;I)I", Access: sync, Body: vectorLastIndexOfFrom},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: sync, Body: vectorToString},
		},
	}
}

// vectorIndexOfFrom is indexOf(elem, index): where the first element at or
// after index that elem equals sits, or -1. Equal is the argument's own
// equals, null finding a null element, as in indexOf(elem). A negative index
// is the ArrayIndexOutOfBoundsException the class library's loop runs into
// before it compares anything; one at or past the size finds nothing. LGT
// answers the same (internal/platform/lgt).
func vectorIndexOfFrom(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	from, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if from < 0 {
		return VoidValue(), guestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("indexOf from %d", from))
	}
	for index := from; index < size; index++ {
		equal, err := vectorElementEquals(call, elements, index, arguments[1])
		if err != nil {
			return VoidValue(), err
		}
		if equal {
			return IntValue(index), nil
		}
	}
	return IntValue(-1), nil
}

// vectorLastIndexOfFrom is lastIndexOf(elem, index): the same search backwards
// from index. An index at or past the size is the IndexOutOfBoundsException the
// class library throws, and a negative one finds nothing.
func vectorLastIndexOfFrom(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	from, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if from >= size {
		return VoidValue(), guestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("%d >= %d", from, size))
	}
	for index := from; index >= 0; index-- {
		equal, err := vectorElementEquals(call, elements, index, arguments[1])
		if err != nil {
			return VoidValue(), err
		}
		if equal {
			return IntValue(index), nil
		}
	}
	return IntValue(-1), nil
}

// vectorToString is `[a, b, c]`: every element's own text, between brackets
// and after commas, in index order, as the class library writes it. The
// elements are read before any is asked for its text, so a toString that
// edits the vector changes what the next call sees and not this one.
func vectorToString(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	snapshot, err := elements.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	text := call.newContainerText(VectorClass)
	if err := text.open('['); err != nil {
		return VoidValue(), err
	}
	defer text.close()
	for index, element := range snapshot {
		if index > 0 {
			if err := text.write(", "); err != nil {
				return VoidValue(), err
			}
		}
		if err := text.element(element); err != nil {
			return VoidValue(), err
		}
	}
	return text.result(']')
}

// vectorElements is `elements()`: a view over what the vector holds now. It is
// a snapshot the way the Hashtable views beside it are — the specification's
// Enumeration is not required to survive a change to what it came from, and a
// copy is what makes "not required" into "does not".
func vectorElements(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	array, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	snapshot, err := call.vm.newArray(objectArrayType, size)
	if err != nil {
		return VoidValue(), err
	}
	elements, err := array.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	if err := SetArrayRange(snapshot, 0, elements); err != nil {
		return VoidValue(), err
	}
	view, err := call.NewObject(ArrayEnumerationClass, "([Ljava/lang/Object;)V", ReferenceValue(snapshot))
	if err != nil {
		return VoidValue(), err
	}
	return ReferenceValue(view), nil
}

// maxContainerTextNesting bounds how deeply Vector and Hashtable toStrings nest
// on one execution. A vector that holds itself, or two tables that hold each
// other, ask their own toString for ever, and every step of that is Go stack
// rather than guest stack: the class library would run out of stack, and here
// the depth is counted and the call refused, which ends the session the way
// running out of frames does. 256 is far past a structure a title builds — a
// table of vectors of tables — and is the bound LGT holds the same calls to
// (internal/platform/lgt).
const maxContainerTextNesting = 256

// containerText is the text a Vector or Hashtable makes of itself, built
// while that container is counted on its execution's nesting.
type containerText struct {
	call    *Invocation
	class   string
	builder strings.Builder
	units   int
	entered bool
}

func (call *Invocation) newContainerText(class string) *containerText {
	return &containerText{call: call, class: class}
}

// open counts the container and starts its text, or refuses a nesting past
// maxContainerTextNesting. A text that opened is closed by its caller.
func (text *containerText) open(bracket byte) error {
	state := text.call.state
	if state.containerTexts >= maxContainerTextNesting {
		return fmt.Errorf("%s.toString nested %d calls deep, past the stack the class library would have",
			javaClassName(text.class), state.containerTexts)
	}
	state.containerTexts++
	text.entered = true
	return text.write(string(bracket))
}

func (text *containerText) close() {
	if text.entered {
		text.call.state.containerTexts--
		text.entered = false
	}
}

// write adds a piece of text. A String is no longer than an array can be, so
// text past that is the OutOfMemoryError the class library's buffer would
// have raised growing to hold it, before the host holds it all.
func (text *containerText) write(piece string) error {
	for _, character := range piece {
		text.units++
		if character > 0xffff {
			text.units++
		}
	}
	if text.units > text.call.vm.config.MaxArrayLength {
		return guestException("java/lang/OutOfMemoryError",
			fmt.Sprintf("the text of a %s is longer than a String can be", javaClassName(text.class)))
	}
	text.builder.WriteString(piece)
	return nil
}

// element adds what one element's text is: "null" for null, a String's own
// text, and otherwise what the element's own toString answers, asked as a
// virtual call so that a title's class answers for itself — on a platform that
// compiled the title, through the title's own code — and "null" when that
// answers null. It is String.valueOf of the element, which is what the class
// library appends.
func (text *containerText) element(value Value) error {
	object, err := value.Reference()
	if err != nil {
		return err
	}
	if object == nil {
		return text.write("null")
	}
	if held, ok := nativeStringObject(object); ok {
		return text.write(held)
	}
	result, err := text.call.InvokeVirtual(object, "toString", "()Ljava/lang/String;")
	if err != nil {
		return err
	}
	answer, err := result.Reference()
	if err != nil {
		return err
	}
	if answer == nil {
		return text.write("null")
	}
	held, ok := nativeStringObject(answer)
	if !ok {
		return fmt.Errorf("%s.toString answered a %s", object.ClassName, answer.ClassName)
	}
	return text.write(held)
}

// result closes the text with its bracket and answers it as a String.
func (text *containerText) result(bracket byte) (Value, error) {
	if err := text.write(string(bracket)); err != nil {
		return VoidValue(), err
	}
	return text.call.vm.stringResult(text.builder.String())
}

// objectArrayType is the component type of the two arrays these collections
// keep their contents in.
var objectArrayType = Type{Kind: TypeReference, ClassName: ObjectClass}

const (
	vectorElementsField = "elements"
	vectorElementsType  = "[Ljava/lang/Object;"
)

// vectorState reads the backing array and the count. Both fields are private,
// but they live on the object rather than beside it, so a Go body reads them
// the way the bytecode did.
func vectorState(vm *VM, vector *Object) (*Array, int32, error) {
	value, err := vm.Field(vector, VectorClass, vectorElementsField, vectorElementsType)
	if err != nil {
		return nil, 0, err
	}
	object, err := value.Reference()
	if err != nil {
		return nil, 0, err
	}
	array, err := guestArray(object)
	if err != nil {
		return nil, 0, err
	}
	size, err := intField(vm, vector, VectorClass, "size")
	if err != nil {
		return nil, 0, err
	}
	return array, size, nil
}

func vectorInitDefault(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeSpecial(vector, VectorClass, "<init>", "(I)V", IntValue(10))
	return VoidValue(), err
}

func vectorInitCapacity(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	capacity, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeSpecial(vector, VectorClass, "<init>", "(II)V", IntValue(capacity), IntValue(0))
	return VoidValue(), err
}

// vectorInit fixes the growth rule. The capacity increment is not a hint: a
// vector that is full grows by exactly this many slots, and doubles only when
// the increment is zero, which is what decides what capacity() answers
// afterwards. One title walks a vector with capacity() as the bound and
// elementAt as the body, so a vector that grew by more than it was told reads
// past its elements.
func vectorInit(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	capacity, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	increment, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	if capacity < 0 {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "negative vector capacity")
	}
	elements, err := call.vm.newArray(objectArrayType, capacity)
	if err != nil {
		return VoidValue(), err
	}
	if err := call.vm.SetField(vector, VectorClass, vectorElementsField, vectorElementsType, ReferenceValue(elements)); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, vector, VectorClass, "capacityIncrement", increment)
}

func vectorAddElement(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, vector, VectorClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(vector, "insertElementAt", "(Ljava/lang/Object;I)V", arguments[1], IntValue(size))
	return VoidValue(), err
}

func vectorElementAt(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	index, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if err := checkVectorIndex(index, size); err != nil {
		return VoidValue(), err
	}
	return elements.Load(int(index))
}

func vectorIndexOf(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	for index := int32(0); index < size; index++ {
		equal, err := vectorElementEquals(call, elements, index, arguments[1])
		if err != nil {
			return VoidValue(), err
		}
		if equal {
			return IntValue(index), nil
		}
	}
	return IntValue(-1), nil
}

func vectorLastIndexOf(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	for index := size - 1; index >= 0; index-- {
		equal, err := vectorElementEquals(call, elements, index, arguments[1])
		if err != nil {
			return VoidValue(), err
		}
		if equal {
			return IntValue(index), nil
		}
	}
	return IntValue(-1), nil
}

// vectorElementEquals compares a searched-for value with one element. A null
// search matches by identity and anything else asks the value's own equals,
// which for a game's object is the game's method.
func vectorElementEquals(call *Invocation, elements *Array, index int32, wanted Value) (bool, error) {
	element, err := elements.Load(int(index))
	if err != nil {
		return false, err
	}
	target, err := wanted.Reference()
	if err != nil {
		return false, err
	}
	stored, err := element.Reference()
	if err != nil {
		return false, err
	}
	if target == nil {
		return stored == nil, nil
	}
	result, err := call.InvokeVirtual(target, "equals", "(Ljava/lang/Object;)Z", ReferenceValue(stored))
	if err != nil {
		return false, err
	}
	value, err := result.Int32()
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

func vectorInsertElementAt(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	index, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, vector, VectorClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	if index < 0 || index > size {
		return VoidValue(), guestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("insert at %d of %d", index, size))
	}
	elements, err := growVector(call, vector, size+1)
	if err != nil {
		return VoidValue(), err
	}
	if moved := size - index; moved > 0 {
		values, err := elements.LoadRange(int(index), int(moved))
		if err != nil {
			return VoidValue(), err
		}
		if err := elements.StoreRange(int(index+1), values); err != nil {
			return VoidValue(), err
		}
	}
	if err := elements.Store(int(index), arguments[1]); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, vector, VectorClass, "size", size+1)
}

// growVector makes room for minimum elements, by the increment the vector was
// constructed with. It answers the backing array afterwards, grown or not.
func growVector(call *Invocation, vector *Object, minimum int32) (*Array, error) {
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return nil, err
	}
	if minimum <= int32(elements.Length()) {
		return elements, nil
	}
	increment, err := intField(call.vm, vector, VectorClass, "capacityIncrement")
	if err != nil {
		return nil, err
	}
	capacity := int32(elements.Length())*2 + 1
	if increment > 0 {
		capacity = int32(elements.Length()) + increment
	}
	if capacity < minimum {
		capacity = minimum
	}
	grown, err := call.vm.newArray(objectArrayType, capacity)
	if err != nil {
		return nil, err
	}
	values, err := elements.LoadRange(0, int(size))
	if err != nil {
		return nil, err
	}
	if err := SetArrayRange(grown, 0, values); err != nil {
		return nil, err
	}
	if err := call.vm.SetField(vector, VectorClass, vectorElementsField, vectorElementsType, ReferenceValue(grown)); err != nil {
		return nil, err
	}
	return guestArray(grown)
}

func vectorIsEmpty(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, vector, VectorClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	if size == 0 {
		return IntValue(1), nil
	}
	return IntValue(0), nil
}

func vectorContains(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	result, err := call.InvokeVirtual(vector, "indexOf", "(Ljava/lang/Object;)I", arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	index, err := result.Int32()
	if err != nil {
		return VoidValue(), err
	}
	if index >= 0 {
		return IntValue(1), nil
	}
	return IntValue(0), nil
}

func vectorCopyInto(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	destination, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	target, err := guestArray(destination)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if int(size) > target.Length() {
		return VoidValue(), guestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("%d elements into %d", size, target.Length()))
	}
	values, err := elements.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), target.StoreRange(0, values)
}

func vectorCapacity(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, _, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(int32(elements.Length())), nil
}

func vectorEnsureCapacity(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	minimum, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	_, err = growVector(call, vector, minimum)
	return VoidValue(), err
}

func vectorTrimToSize(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if int(size) >= elements.Length() {
		return VoidValue(), nil
	}
	trimmed, err := call.vm.newArray(objectArrayType, size)
	if err != nil {
		return VoidValue(), err
	}
	values, err := elements.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	if err := SetArrayRange(trimmed, 0, values); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(vector, VectorClass, vectorElementsField, vectorElementsType, ReferenceValue(trimmed))
}

func vectorSetSize(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	next, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if next < 0 {
		return VoidValue(), guestException("java/lang/ArrayIndexOutOfBoundsException", "negative size")
	}
	// ensureCapacity is public, so a subclass may have replaced how the vector
	// grows and setSize goes through it the way the bytecode did.
	if _, err := call.InvokeVirtual(vector, "ensureCapacity", "(I)V", IntValue(next)); err != nil {
		return VoidValue(), err
	}
	elements, _, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, vector, VectorClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	for index := next; index < size; index++ {
		if err := elements.Store(int(index), ReferenceValue(nil)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), setIntField(call.vm, vector, VectorClass, "size", next)
}

func vectorFirstElement(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if err := checkVectorIndex(0, size); err != nil {
		return VoidValue(), err
	}
	return elements.Load(0)
}

func vectorLastElement(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if err := checkVectorIndex(size-1, size); err != nil {
		return VoidValue(), err
	}
	return elements.Load(int(size - 1))
}

func vectorSetElementAt(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	index, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if err := checkVectorIndex(index, size); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), elements.Store(int(index), arguments[1])
}

func vectorRemoveElement(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	found, err := call.InvokeVirtual(vector, "indexOf", "(Ljava/lang/Object;)I", arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	index, err := found.Int32()
	if err != nil {
		return VoidValue(), err
	}
	if index < 0 {
		return IntValue(0), nil
	}
	if _, err := call.InvokeVirtual(vector, "removeElementAt", "(I)V", IntValue(index)); err != nil {
		return VoidValue(), err
	}
	return IntValue(1), nil
}

func vectorRemoveAllElements(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	for index := int32(0); index < size; index++ {
		if err := elements.Store(int(index), ReferenceValue(nil)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), setIntField(call.vm, vector, VectorClass, "size", 0)
}

func vectorRemoveElementAt(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	index, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	elements, size, err := vectorState(call.vm, vector)
	if err != nil {
		return VoidValue(), err
	}
	if err := checkVectorIndex(index, size); err != nil {
		return VoidValue(), err
	}
	if moved := size - index - 1; moved > 0 {
		values, err := elements.LoadRange(int(index+1), int(moved))
		if err != nil {
			return VoidValue(), err
		}
		if err := elements.StoreRange(int(index), values); err != nil {
			return VoidValue(), err
		}
	}
	if err := elements.Store(int(size-1), ReferenceValue(nil)); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, vector, VectorClass, "size", size-1)
}

func vectorSize(call *Invocation, arguments []Value) (Value, error) {
	vector, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, vector, VectorClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(size), nil
}

// checkVectorIndex reports an out-of-range access. The index and the size are
// what say whether the caller counted wrongly or the vector was never filled,
// and an exception with neither leaves that unanswerable from a log.
func checkVectorIndex(index, size int32) error {
	if index < 0 || index >= size {
		return guestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("%d of %d", index, size))
	}
	return nil
}

// hashtableDefinition is CLDC's map. It is a pair of parallel arrays searched
// linearly rather than a hash table: the tables a game builds here hold a
// handful of entries, and the guest's own equals decides a match, so a hash
// would buy an extra call per lookup and little else.
func hashtableDefinition() ClassDefinition {
	sync := AccessPublic | AccessSynchronized
	return ClassDefinition{
		Name:      HashtableClass,
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "keys", Descriptor: "[Ljava/lang/Object;", Access: AccessPrivate},
			{Name: "values", Descriptor: "[Ljava/lang/Object;", Access: AccessPrivate},
			{Name: "size", Descriptor: "I", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: hashtableInit},
			// The capacity form is a hint. The two arrays grow on demand, so
			// what the value has to do is be validated and then ignored, the
			// same way Vector's is.
			{Name: "<init>", Descriptor: "(I)V", Access: AccessPublic, Body: hashtableInitCapacity},
			{Name: "containsKey", Descriptor: "(Ljava/lang/Object;)Z", Access: sync, Body: hashtableContainsKey},
			{Name: "get", Descriptor: "(Ljava/lang/Object;)Ljava/lang/Object;", Access: sync, Body: hashtableGet},
			{Name: "isEmpty", Descriptor: "()Z", Access: sync, Body: hashtableIsEmpty},
			{Name: "put", Descriptor: "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", Access: sync, Body: hashtablePut},
			{Name: "remove", Descriptor: "(Ljava/lang/Object;)Ljava/lang/Object;", Access: sync, Body: hashtableRemove},
			{Name: "size", Descriptor: "()I", Access: sync, Body: hashtableSize},
			{Name: "clear", Descriptor: "()V", Access: sync, Body: hashtableClear},
			{Name: "keys", Descriptor: "()Ljava/util/Enumeration;", Access: sync, Body: hashtableView("keys")},
			{Name: "elements", Descriptor: "()Ljava/util/Enumeration;", Access: sync, Body: hashtableView("values")},
			// The search by value, the growth a subclass may ask for, and the
			// text a table makes of itself.
			{Name: "contains", Descriptor: "(Ljava/lang/Object;)Z", Access: sync, Body: hashtableContains},
			{Name: "rehash", Descriptor: "()V", Access: AccessProtected, Body: hashtableRehash},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: sync, Body: hashtableToString},
		},
	}
}

// hashtableContains is contains(value): whether any entry's value equals the
// argument, asked of each value the table holds — `value.equals(argument)`, as
// the class library asks it — in the order elements() hands them out. A null
// argument is the NullPointerException the class library throws. The values
// are read before any is asked, so an equals that edits the table does not
// move the walk.
func hashtableContains(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	wanted, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if wanted == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "Hashtable.contains(null)")
	}
	_, values, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	snapshot, err := values.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	for _, value := range snapshot {
		stored, err := value.Reference()
		if err != nil {
			return VoidValue(), err
		}
		if stored == nil {
			continue
		}
		result, err := call.InvokeVirtual(stored, "equals", "(Ljava/lang/Object;)Z", ReferenceValue(wanted))
		if err != nil {
			return VoidValue(), err
		}
		equal, err := result.Int32()
		if err != nil {
			return VoidValue(), err
		}
		if equal != 0 {
			return IntValue(1), nil
		}
	}
	return IntValue(0), nil
}

// hashtableRehash is rehash(), which the class library declares protected for
// a subclass to call: the contents moved into a table of larger capacity. This
// table is two arrays searched in order rather than buckets, so what it does
// is what an automatic growth does — both arrays become 2n + 1 long, the n the
// length they had — and the entries keep their places: keys() and elements()
// enumerate as they did before. The capacity is nothing a title can read, so
// the only difference a call makes is the memory it takes, refused with
// OutOfMemoryError as any other growth is.
func hashtableRehash(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	keys, _, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	_, _, err = resizeHashtable(call, table, size, int64(keys.Length())*2+1)
	return VoidValue(), err
}

// hashtableToString is `{key=value, key2=value2}`: each entry's key text and
// value text, joined by an equals sign, in the order keys() and elements()
// hand them out, as the class library writes it — the key's text asked before
// its value's. The entries are read before any is asked for its text.
func hashtableToString(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	keys, values, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	keySnapshot, err := keys.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	valueSnapshot, err := values.LoadRange(0, int(size))
	if err != nil {
		return VoidValue(), err
	}
	text := call.newContainerText(HashtableClass)
	if err := text.open('{'); err != nil {
		return VoidValue(), err
	}
	defer text.close()
	for index := range keySnapshot {
		if index > 0 {
			if err := text.write(", "); err != nil {
				return VoidValue(), err
			}
		}
		if err := text.element(keySnapshot[index]); err != nil {
			return VoidValue(), err
		}
		if err := text.write("="); err != nil {
			return VoidValue(), err
		}
		if err := text.element(valueSnapshot[index]); err != nil {
			return VoidValue(), err
		}
	}
	return text.result('}')
}

func hashtableSize(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, table, HashtableClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(size), nil
}

func hashtableClear(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	keys, values, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	for index := int32(0); index < size; index++ {
		for _, array := range []*Array{keys, values} {
			if err := array.Store(int(index), ReferenceValue(nil)); err != nil {
				return VoidValue(), err
			}
		}
	}
	return VoidValue(), setIntField(call.vm, table, HashtableClass, "size", 0)
}

// hashtableView answers an Enumeration over a copy of one of the two arrays.
// A copy rather than a live view because a game walks a table while it edits
// it — removing the entry it just handled is the common shape — and a live
// view would have to decide what that means. The standard leaves it undefined,
// so what is chosen here is the harmless reading.
func hashtableView(field string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		table, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		keys, values, size, err := hashtableState(call.vm, table)
		if err != nil {
			return VoidValue(), err
		}
		source := keys
		if field == "values" {
			source = values
		}
		snapshot, err := call.vm.newArray(objectArrayType, size)
		if err != nil {
			return VoidValue(), err
		}
		elements, err := source.LoadRange(0, int(size))
		if err != nil {
			return VoidValue(), err
		}
		if err := SetArrayRange(snapshot, 0, elements); err != nil {
			return VoidValue(), err
		}
		view, err := call.NewObject(ArrayEnumerationClass, "([Ljava/lang/Object;)V", ReferenceValue(snapshot))
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(view), nil
	}
}

// arrayEnumerationDefinition walks a snapshot. It is the runtime's own class
// rather than one from java/util, because the standard names no implementation
// of Enumeration and a game only ever holds the interface.
func arrayEnumerationDefinition() ClassDefinition {
	return ClassDefinition{
		Name:       ArrayEnumerationClass,
		SuperName:  ObjectClass,
		Interfaces: []string{EnumerationClass},
		Access:     AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "elements", Descriptor: "[Ljava/lang/Object;", Access: AccessPrivate},
			{Name: "index", Descriptor: "I", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "([Ljava/lang/Object;)V", Access: AccessPublic, Body: arrayEnumerationInit},
			{Name: "hasMoreElements", Descriptor: "()Z", Access: AccessPublic, Body: arrayEnumerationHasMore},
			{Name: "nextElement", Descriptor: "()Ljava/lang/Object;", Access: AccessPublic, Body: arrayEnumerationNext},
		},
	}
}

func arrayEnumerationInit(call *Invocation, arguments []Value) (Value, error) {
	view, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(view, ArrayEnumerationClass, "elements", vectorElementsType, arguments[1])
}

// arrayEnumerationElements reads the snapshot and the position together, which
// is what both methods need.
func arrayEnumerationElements(call *Invocation, view *Object) (*Array, int32, error) {
	value, err := call.vm.Field(view, ArrayEnumerationClass, "elements", vectorElementsType)
	if err != nil {
		return nil, 0, err
	}
	object, err := value.Reference()
	if err != nil {
		return nil, 0, err
	}
	elements, err := guestArray(object)
	if err != nil {
		return nil, 0, err
	}
	index, err := intField(call.vm, view, ArrayEnumerationClass, "index")
	if err != nil {
		return nil, 0, err
	}
	return elements, index, nil
}

func arrayEnumerationHasMore(call *Invocation, arguments []Value) (Value, error) {
	view, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, index, err := arrayEnumerationElements(call, view)
	if err != nil {
		return VoidValue(), err
	}
	if int(index) < elements.Length() {
		return IntValue(1), nil
	}
	return IntValue(0), nil
}

func arrayEnumerationNext(call *Invocation, arguments []Value) (Value, error) {
	view, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	elements, index, err := arrayEnumerationElements(call, view)
	if err != nil {
		return VoidValue(), err
	}
	if int(index) >= elements.Length() {
		return VoidValue(), guestException("java/util/NoSuchElementException", "the enumeration is exhausted")
	}
	element, err := elements.Load(int(index))
	if err != nil {
		return VoidValue(), err
	}
	if err := setIntField(call.vm, view, ArrayEnumerationClass, "index", index+1); err != nil {
		return VoidValue(), err
	}
	return element, nil
}

const hashtableInitialCapacity = 8

func hashtableInit(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	// Both arrays are made before either is stored, so a refusal leaves the
	// table as it was rather than with keys and no values.
	var arrays [2]*Object
	for index := range arrays {
		if arrays[index], err = call.vm.newArray(objectArrayType, hashtableInitialCapacity); err != nil {
			return VoidValue(), err
		}
	}
	for index, name := range []string{"keys", "values"} {
		if err := call.vm.SetField(table, HashtableClass, name, vectorElementsType, ReferenceValue(arrays[index])); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}

func hashtableInitCapacity(call *Invocation, arguments []Value) (Value, error) {
	capacity, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if capacity < 0 {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "Hashtable capacity")
	}
	return hashtableInit(call, arguments)
}

// hashtableState reads both arrays and the count together, since every method
// here needs the keys to find an entry and the values to answer with one.
func hashtableState(vm *VM, table *Object) (keys, values *Array, size int32, err error) {
	keyValue, err := vm.Field(table, HashtableClass, "keys", vectorElementsType)
	if err != nil {
		return nil, nil, 0, err
	}
	keyObject, err := keyValue.Reference()
	if err != nil {
		return nil, nil, 0, err
	}
	if keys, err = guestArray(keyObject); err != nil {
		return nil, nil, 0, err
	}
	valueValue, err := vm.Field(table, HashtableClass, "values", vectorElementsType)
	if err != nil {
		return nil, nil, 0, err
	}
	valueObject, err := valueValue.Reference()
	if err != nil {
		return nil, nil, 0, err
	}
	if values, err = guestArray(valueObject); err != nil {
		return nil, nil, 0, err
	}
	if size, err = intField(vm, table, HashtableClass, "size"); err != nil {
		return nil, nil, 0, err
	}
	return keys, values, size, nil
}

// findHashtableKey answers where a key is, or -1. The stored key's equals
// decides, which is the direction the class file called it in.
func findHashtableKey(call *Invocation, keys *Array, size int32, key Value) (int32, error) {
	wanted, err := key.Reference()
	if err != nil {
		return 0, err
	}
	if wanted == nil {
		return 0, guestException("java/lang/NullPointerException", "null key")
	}
	for index := int32(0); index < size; index++ {
		element, err := keys.Load(int(index))
		if err != nil {
			return 0, err
		}
		stored, err := element.Reference()
		if err != nil {
			return 0, err
		}
		if stored == nil {
			continue
		}
		result, err := call.InvokeVirtual(stored, "equals", "(Ljava/lang/Object;)Z", ReferenceValue(wanted))
		if err != nil {
			return 0, err
		}
		equal, err := result.Int32()
		if err != nil {
			return 0, err
		}
		if equal != 0 {
			return index, nil
		}
	}
	return -1, nil
}

func hashtableContainsKey(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	keys, _, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	index, err := findHashtableKey(call, keys, size, arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	if index >= 0 {
		return IntValue(1), nil
	}
	return IntValue(0), nil
}

func hashtableGet(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	keys, values, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	index, err := findHashtableKey(call, keys, size, arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	if index < 0 {
		return ReferenceValue(nil), nil
	}
	return values.Load(int(index))
}

func hashtableIsEmpty(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	size, err := intField(call.vm, table, HashtableClass, "size")
	if err != nil {
		return VoidValue(), err
	}
	if size == 0 {
		return IntValue(1), nil
	}
	return IntValue(0), nil
}

func hashtablePut(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	if _, err := requireObject(arguments, 1); err != nil {
		return VoidValue(), err
	}
	if _, err := requireObject(arguments, 2); err != nil {
		return VoidValue(), err
	}
	keys, values, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	index, err := findHashtableKey(call, keys, size, arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	if index >= 0 {
		previous, err := values.Load(int(index))
		if err != nil {
			return VoidValue(), err
		}
		if err := values.Store(int(index), arguments[2]); err != nil {
			return VoidValue(), err
		}
		return previous, nil
	}
	keys, values, err = growHashtable(call, table, size)
	if err != nil {
		return VoidValue(), err
	}
	if err := keys.Store(int(size), arguments[1]); err != nil {
		return VoidValue(), err
	}
	if err := values.Store(int(size), arguments[2]); err != nil {
		return VoidValue(), err
	}
	if err := setIntField(call.vm, table, HashtableClass, "size", size+1); err != nil {
		return VoidValue(), err
	}
	return ReferenceValue(nil), nil
}

// growHashtable makes room for one more entry, growing both arrays together
// so a key never outlives its value. Both are made before either is stored:
// the second can be refused, and a table left with grown keys and the old
// values would never grow again, its next new key landing past the values'
// end.
func growHashtable(call *Invocation, table *Object, size int32) (*Array, *Array, error) {
	keys, values, _, err := hashtableState(call.vm, table)
	if err != nil {
		return nil, nil, err
	}
	if int(size) < keys.Length() {
		return keys, values, nil
	}
	return resizeHashtable(call, table, size, int64(keys.Length())*2+1)
}

// resizeHashtable moves the first size entries into two arrays of the given
// length, made before either is stored. A length past what an array may be is
// refused by the array it would have made.
func resizeHashtable(call *Invocation, table *Object, size int32, length int64) (*Array, *Array, error) {
	keys, values, _, err := hashtableState(call.vm, table)
	if err != nil {
		return nil, nil, err
	}
	capacity := int32(min(length, math.MaxInt32))
	var made [2]*Object
	for index := range made {
		if made[index], err = call.vm.newArray(objectArrayType, capacity); err != nil {
			return nil, nil, err
		}
	}
	grown := make([]*Array, 0, 2)
	for arrayIndex, source := range []*Array{keys, values} {
		next := made[arrayIndex]
		existing, err := source.LoadRange(0, int(size))
		if err != nil {
			return nil, nil, err
		}
		if err := SetArrayRange(next, 0, existing); err != nil {
			return nil, nil, err
		}
		name := "keys"
		if arrayIndex == 1 {
			name = "values"
		}
		if err := call.vm.SetField(table, HashtableClass, name, vectorElementsType, ReferenceValue(next)); err != nil {
			return nil, nil, err
		}
		array, err := guestArray(next)
		if err != nil {
			return nil, nil, err
		}
		grown = append(grown, array)
	}
	return grown[0], grown[1], nil
}

func hashtableRemove(call *Invocation, arguments []Value) (Value, error) {
	table, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	keys, values, size, err := hashtableState(call.vm, table)
	if err != nil {
		return VoidValue(), err
	}
	index, err := findHashtableKey(call, keys, size, arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	if index < 0 {
		return ReferenceValue(nil), nil
	}
	previous, err := values.Load(int(index))
	if err != nil {
		return VoidValue(), err
	}
	if moved := size - index - 1; moved > 0 {
		for _, array := range []*Array{keys, values} {
			tail, err := array.LoadRange(int(index+1), int(moved))
			if err != nil {
				return VoidValue(), err
			}
			if err := array.StoreRange(int(index), tail); err != nil {
				return VoidValue(), err
			}
		}
	}
	for _, array := range []*Array{keys, values} {
		if err := array.Store(int(size-1), ReferenceValue(nil)); err != nil {
			return VoidValue(), err
		}
	}
	if err := setIntField(call.vm, table, HashtableClass, "size", size-1); err != nil {
		return VoidValue(), err
	}
	return previous, nil
}

// calendarDefinition and dateDefinition publish the clock pair. Their bodies
// are natives in builtins.go because both stand for an instant the Host owns;
// what the declarations add is the field constants a title indexes get() with
// and a class for `new Date()` to resolve.
func calendarDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	constant := AccessPublic | AccessStatic | AccessFinal
	return ClassDefinition{
		Name:      CalendarClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessAbstract,
		Fields: []FieldDefinition{
			{Name: "YEAR", Descriptor: "I", Access: constant, Constant: IntValue(1)},
			{Name: "MONTH", Descriptor: "I", Access: constant, Constant: IntValue(2)},
			{Name: "DATE", Descriptor: "I", Access: constant, Constant: IntValue(5)},
			{Name: "DAY_OF_MONTH", Descriptor: "I", Access: constant, Constant: IntValue(5)},
			{Name: "DAY_OF_WEEK", Descriptor: "I", Access: constant, Constant: IntValue(7)},
			{Name: "HOUR", Descriptor: "I", Access: constant, Constant: IntValue(10)},
			{Name: "HOUR_OF_DAY", Descriptor: "I", Access: constant, Constant: IntValue(11)},
			{Name: "MINUTE", Descriptor: "I", Access: constant, Constant: IntValue(12)},
			{Name: "SECOND", Descriptor: "I", Access: constant, Constant: IntValue(13)},
			{Name: "MILLISECOND", Descriptor: "I", Access: constant, Constant: IntValue(14)},
		},
		Methods: []MethodDefinition{
			{Name: "getInstance", Descriptor: "()Ljava/util/Calendar;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "getInstance", Descriptor: "(Ljava/util/TimeZone;)Ljava/util/Calendar;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "get", Descriptor: "(I)I", Access: native},
			{Name: "set", Descriptor: "(II)V", Access: native},
			{Name: "getTime", Descriptor: "()Ljava/util/Date;", Access: AccessPublic | AccessFinal | AccessNative},
			{Name: "setTime", Descriptor: "(Ljava/util/Date;)V", Access: AccessPublic | AccessFinal | AccessNative},
			{Name: "setTimeZone", Descriptor: "(Ljava/util/TimeZone;)V", Access: native},
		},
	}
}

func dateDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	return ClassDefinition{
		Name:      DateClass,
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: native},
			{Name: "<init>", Descriptor: "(J)V", Access: native},
			{Name: "getTime", Descriptor: "()J", Access: native},
			{Name: "setTime", Descriptor: "(J)V", Access: native},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: native},
			{Name: "hashCode", Descriptor: "()I", Access: native},
		},
	}
}

// enumerationDefinition is the interface Hashtable's two views answer with. A
// game holds one and walks it; nothing here implements it but the two views,
// which are declared beside the table itself.
func enumerationDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      EnumerationClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessInterface | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "hasMoreElements", Descriptor: "()Z", Access: AccessPublic | AccessAbstract},
			{Name: "nextElement", Descriptor: "()Ljava/lang/Object;", Access: AccessPublic | AccessAbstract},
		},
	}
}

// stackDefinition is Vector with the four methods that make it a stack. It is
// a subclass in the standard library too, so a title that stores into it
// through the Vector half sees what it expects.
func stackDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      StackClass,
		SuperName: VectorClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: superInit(VectorClass, "()V")},
			{Name: "push", Descriptor: "(Ljava/lang/Object;)Ljava/lang/Object;", Access: AccessPublic, Body: stackPush},
			{Name: "pop", Descriptor: "()Ljava/lang/Object;", Access: AccessPublic, Body: stackPop},
			{Name: "peek", Descriptor: "()Ljava/lang/Object;", Access: AccessPublic, Body: stackPeek},
			{Name: "empty", Descriptor: "()Z", Access: AccessPublic, Body: stackEmpty},
			{Name: "search", Descriptor: "(Ljava/lang/Object;)I", Access: AccessPublic, Body: stackSearch},
		},
	}
}

func stackPush(call *Invocation, arguments []Value) (Value, error) {
	stack, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	if _, err := call.InvokeVirtual(stack, "addElement", "(Ljava/lang/Object;)V", arguments[1]); err != nil {
		return VoidValue(), err
	}
	return arguments[1], nil
}

// stackTop answers the index of the top element, or the empty-stack exception
// the standard raises rather than an out-of-range read.
func stackTop(call *Invocation, stack *Object) (int32, error) {
	result, err := call.InvokeVirtual(stack, "size", "()I")
	if err != nil {
		return 0, err
	}
	size, err := result.Int32()
	if err != nil {
		return 0, err
	}
	if size == 0 {
		return 0, guestException("java/util/EmptyStackException", "the stack is empty")
	}
	return size - 1, nil
}

func stackPop(call *Invocation, arguments []Value) (Value, error) {
	stack, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	top, err := stackTop(call, stack)
	if err != nil {
		return VoidValue(), err
	}
	element, err := call.InvokeVirtual(stack, "elementAt", "(I)Ljava/lang/Object;", IntValue(top))
	if err != nil {
		return VoidValue(), err
	}
	if _, err := call.InvokeVirtual(stack, "removeElementAt", "(I)V", IntValue(top)); err != nil {
		return VoidValue(), err
	}
	return element, nil
}

func stackPeek(call *Invocation, arguments []Value) (Value, error) {
	stack, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	top, err := stackTop(call, stack)
	if err != nil {
		return VoidValue(), err
	}
	return call.InvokeVirtual(stack, "elementAt", "(I)Ljava/lang/Object;", IntValue(top))
}

func stackEmpty(call *Invocation, arguments []Value) (Value, error) {
	stack, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	return call.InvokeVirtual(stack, "isEmpty", "()Z")
}

// stackSearch counts from the top, where the top is 1, and answers -1 for an
// element the stack does not hold.
func stackSearch(call *Invocation, arguments []Value) (Value, error) {
	stack, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	found, err := call.InvokeVirtual(stack, "lastIndexOf", "(Ljava/lang/Object;)I", arguments[1])
	if err != nil {
		return VoidValue(), err
	}
	index, err := found.Int32()
	if err != nil {
		return VoidValue(), err
	}
	if index < 0 {
		return IntValue(-1), nil
	}
	result, err := call.InvokeVirtual(stack, "size", "()I")
	if err != nil {
		return VoidValue(), err
	}
	size, err := result.Int32()
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(size - index), nil
}

// randomDefinition is the CLDC generator. The sequence is the one the standard
// specifies down to the constants, because a game that seeds it and expects a
// particular series — a map generator, a shuffled deck — gets a different game
// otherwise.
func randomDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      RandomClass,
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "seed", Descriptor: "J", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: randomInitDefault},
			{Name: "<init>", Descriptor: "(J)V", Access: AccessPublic, Body: randomInitSeed},
			{Name: "setSeed", Descriptor: "(J)V", Access: AccessPublic, Body: randomSetSeed},
			{Name: "next", Descriptor: "(I)I", Access: AccessProtected, Body: randomNext},
			{Name: "nextInt", Descriptor: "()I", Access: AccessPublic, Body: randomNextInt},
			{Name: "nextInt", Descriptor: "(I)I", Access: AccessPublic, Body: randomNextIntBound},
			{Name: "nextLong", Descriptor: "()J", Access: AccessPublic, Body: randomNextLong},
		},
	}
}

const (
	randomMultiplier = 0x5deece66d
	randomAddend     = 11
	randomMask       = (1 << 48) - 1
)

func randomInitDefault(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	now, err := call.InvokeStatic(SystemClass, "currentTimeMillis", "()J")
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeSpecial(random, RandomClass, "<init>", "(J)V", now)
	return VoidValue(), err
}

func randomInitSeed(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(random, "setSeed", "(J)V", arguments[1])
	return VoidValue(), err
}

func randomSetSeed(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	seed, err := nativeLong(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(random, RandomClass, "seed", "J", LongValue((seed^randomMultiplier)&randomMask))
}

func randomNext(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	bits, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	value, err := call.vm.Field(random, RandomClass, "seed", "J")
	if err != nil {
		return VoidValue(), err
	}
	seed, err := value.Int64()
	if err != nil {
		return VoidValue(), err
	}
	seed = (seed*randomMultiplier + randomAddend) & randomMask
	if err := call.vm.SetField(random, RandomClass, "seed", "J", LongValue(seed)); err != nil {
		return VoidValue(), err
	}
	return IntValue(int32(uint64(seed) >> uint(48-bits))), nil
}

// randomBits asks for the next bits through the VM, because next is protected
// rather than private: a game may subclass Random to make the sequence its own.
func randomBits(call *Invocation, random *Object, bits int32) (int32, error) {
	result, err := call.InvokeVirtual(random, "next", "(I)I", IntValue(bits))
	if err != nil {
		return 0, err
	}
	return result.Int32()
}

func randomNextInt(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := randomBits(call, random, 32)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(value), nil
}

func randomNextIntBound(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	bound, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if bound <= 0 {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "bound must be positive")
	}
	if bound&-bound == bound {
		bits, err := randomBits(call, random, 31)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32((int64(bound) * int64(bits)) >> 31)), nil
	}
	for {
		bits, err := randomBits(call, random, 31)
		if err != nil {
			return VoidValue(), err
		}
		value := bits % bound
		if bits-value+(bound-1) >= 0 {
			return IntValue(value), nil
		}
	}
}

func randomNextLong(call *Invocation, arguments []Value) (Value, error) {
	random, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	high, err := randomBits(call, random, 32)
	if err != nil {
		return VoidValue(), err
	}
	low, err := randomBits(call, random, 32)
	if err != nil {
		return VoidValue(), err
	}
	return LongValue(int64(high)<<32 + int64(low)), nil
}

// java/util/TimeZone. This runtime knows exactly two zones: GMT, and the one
// the guest clock runs in — which is the Host's, because Calendar and Date read
// that clock as local time and a zone object that disagreed with them would
// make a title's own arithmetic wrong.
//
// So getAvailableIDs answers those two rather than a database. Shipping the
// IANA table would mean embedding tzdata for a cross-compiled release, and no
// title seen here does more with the answer than hand an element straight back
// to getTimeZone. An ID that is neither gets the GMT zone, which is what the
// specification says an unrecognized ID gets.
func timeZoneDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	return ClassDefinition{
		Name:      TimeZoneClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "getDefault", Descriptor: "()Ljava/util/TimeZone;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "getTimeZone", Descriptor: "(Ljava/lang/String;)Ljava/util/TimeZone;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "getAvailableIDs", Descriptor: "()[Ljava/lang/String;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "getID", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "getRawOffset", Descriptor: "()I", Access: native},
			{Name: "useDaylightTime", Descriptor: "()Z", Access: native},
		},
	}
}
