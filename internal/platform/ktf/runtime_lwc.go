package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The lwc text components are the WIPI text-input surface. There is no
// on-device input method here — text reaches the emulator through the Host
// keypad — so a component holds the text it was constructed with and reports
// it back. Everything that would require an editing overlay (caret movement,
// per-key composition) is deliberately absent rather than faked.

const (
	runtimeComponentClass          = "org/kwis/msp/lwc/Component"
	runtimeContainerComponentClass = "org/kwis/msp/lwc/ContainerComponent"
	runtimeShellComponentClass     = "org/kwis/msp/lwc/ShellComponent"
	runtimeTextComponentClass      = "org/kwis/msp/lwc/TextComponent"
	runtimeTextFieldComponentClass = "org/kwis/msp/lwc/TextFieldComponent"
	runtimeTextBoxComponentClass   = "org/kwis/msp/lwc/TextBoxComponent"
	runtimeButtonComponentClass    = "org/kwis/msp/lwc/ButtonComponent"
	runtimeEventListenerClass      = "org/kwis/msp/lwc/EventListener"

	componentTextField      = "text:Ljava/lang/String;"
	componentMaxLengthField = "maxLength:I"
	// componentImageField is the picture a button was built with.
	componentImageField = "image:Lorg/kwis/msp/lcdui/Image;"
	// componentInputHandlerField is the automaton a text component owns. The
	// specification declares it protected on TextComponent and a title reads
	// it off the component rather than asking for it, so it is published into
	// the guest payload as well; see textComponentFieldsSize.
	componentInputHandlerField = "imHandler:Lorg/kwis/msp/lcdui/InputMethodHandler;"
)

// textComponentFieldsSize is how many payload bytes TextComponent's own field
// records describe: the one imHandler reference.
const textComponentFieldsSize = 4

func runtimeTextComponentClassDefinition() runtimeJavaClass {
	const class = runtimeTextComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeComponentClass,
		accessFlags: 0x0021,
		// The specification declares imHandler protected, and three local
		// titles read it off the component instead of asking for it — they
		// take the handler and register their own listener on it, which is
		// how a title that draws its own text field gets the characters. The
		// field record alone is not enough for that: the guest loads the word
		// at the offset the record gives it, so the handler is published into
		// the payload too. See fieldSyncs.
		instanceSize: textComponentFieldsSize,
		fields: []runtimeJavaField{
			{name: "imHandler", descriptor: "Lorg/kwis/msp/lcdui/InputMethodHandler;", accessFlags: 0x0004, offset: 0},
		},
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeTextComponentConstructor},
			{class: class, name: "setMaxLength", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeTextComponentSetMaxLength},
			{class: class, name: "getString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeTextComponentGetString},
			// A text component takes the keys its own card hands it; see
			// runtime_lwc_key.go for why this one class overrides the
			// toolkit's rule that a widget answers a key as unconsumed.
			{class: class, name: "keyNotify", descriptor: "(II)Z", accessFlags: 0x0001, implementation: runtimeTextComponentKeyNotify},
		},
	}
}

func runtimeTextFieldComponentClassDefinition() runtimeJavaClass {
	const class = runtimeTextFieldComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeTextComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeTextComponentConstructorWithText},
			// setString replaces the field's text. The specification declares
			// it on the field rather than on TextComponent, which is why it is
			// here and not beside getString above.
			{class: class, name: "setString", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("TextFieldComponent.setString", componentTextField)},
		},
	}
}

// runtimeButtonComponentClassDefinition is the toolkit's button: "버튼은
// 문자열과 이미지 두개로 구성됩니다" — a string and a picture, either of which
// may be absent. Nothing here draws one, so what it does is hold the two and
// answer them back, which is what a title that builds its own menu out of
// buttons reads.
func runtimeButtonComponentClassDefinition() runtimeJavaClass {
	const class = runtimeButtonComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "<init>", descriptor: "(Ljava/lang/String;Lorg/kwis/msp/lcdui/Image;)V", accessFlags: 0x0001, implementation: runtimeButtonConstructor},
			{class: class, name: "setString", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("ButtonComponent.setString", componentTextField)},
			{class: class, name: "getString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeTextComponentGetString},
			{class: class, name: "setActionListener", descriptor: "(Lorg/kwis/msp/lwc/ActionListener;Ljava/lang/Object;)V", accessFlags: 0x0001, implementation: runtimeComponentSetActionListener},
			{class: class, name: "setImage", descriptor: "(Lorg/kwis/msp/lcdui/Image;)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("ButtonComponent.setImage", componentImageField)},
			{class: class, name: "getImage", descriptor: "()Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0001, implementation: runtimeComponentField(componentImageField)},
		},
	}
}

func runtimeButtonConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ButtonComponent constructor", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentTextField] = arguments[1]
	receiver.Fields[componentImageField] = arguments[2]
	return jvm.VoidValue(), nil
}

func runtimeTextBoxComponentClassDefinition() runtimeJavaClass {
	const class = runtimeTextBoxComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeTextComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeTextComponentConstructorWithText},
			// The box declares its own editing calls where the field declares
			// only setString: a box is the multi-line one, and the handset's
			// input method drives it a run of characters at a time. Nothing
			// drives it here, so what they do is edit the text the component
			// holds — which is the half a title that edits its own box uses.
			{class: class, name: "setString", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("TextBoxComponent.setString", componentTextField)},
			{class: class, name: "insert", descriptor: "([CIII)V", accessFlags: 0x0001, implementation: runtimeTextComponentInsert},
			{class: class, name: "delete", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeTextComponentDelete},
			{class: class, name: "focusNotify", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentBooleanField("focused:Z")},
		},
	}
}

// runtimeTextComponentInsert is `insert(char[] data, int offset, int len, int
// position)`: a run of characters put into the text at a position. A position
// past the end appends, which is where a caret at the end of the text is.
func runtimeTextComponentInsert(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextBoxComponent.insert", arguments, 5)
	if err != nil {
		return jvm.VoidValue(), err
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), fmt.Errorf("TextBoxComponent.insert data is null")
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := arguments[3].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return jvm.VoidValue(), fmt.Errorf("TextBoxComponent.insert range [%d, %d) is out of bounds", offset, offset+length)
	}
	inserted := make([]rune, 0, length)
	for _, value := range values[offset : offset+length] {
		unit, err := value.Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		inserted = append(inserted, rune(uint16(unit)))
	}
	position, err := arguments[4].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	symbols := []rune(runtimeComponentText(receiver))
	at := clampTextPosition(position, len(symbols))
	grown := append([]rune{}, symbols[:at]...)
	grown = append(grown, inserted...)
	grown = append(grown, symbols[at:]...)
	if limit := runtimeComponentMaxLength(receiver); limit > 0 && int32(len(grown)) > limit {
		grown = grown[:limit]
	}
	receiver.Fields[componentTextField] = jvm.ReferenceValue(vm.NewString(string(grown)))
	return jvm.VoidValue(), nil
}

// runtimeTextComponentDelete is `delete(int index, int len)`, in characters. A
// range outside the text is clipped rather than refused: the caller is the
// handset's own input method working against a caret this platform does not
// move, so a position it computed is not the title's mistake.
func runtimeTextComponentDelete(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextBoxComponent.delete", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	length, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	symbols := []rune(runtimeComponentText(receiver))
	at := clampTextPosition(index, len(symbols))
	count := int(length)
	if count < 0 {
		count = 0
	}
	if at+count > len(symbols) {
		count = len(symbols) - at
	}
	kept := append(append([]rune{}, symbols[:at]...), symbols[at+count:]...)
	receiver.Fields[componentTextField] = jvm.ReferenceValue(vm.NewString(string(kept)))
	return jvm.VoidValue(), nil
}

func clampTextPosition(position int32, length int) int {
	if position < 0 {
		return 0
	}
	if int(position) > length {
		return length
	}
	return int(position)
}

// runtimeComponentText and runtimeComponentMaxLength read back what a text
// component holds, for the calls that edit it.
func runtimeComponentText(receiver *jvm.Object) string {
	value, ok := receiver.Fields[componentTextField]
	if !ok {
		return ""
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		return ""
	}
	text, _ := jvm.StringText(object)
	return text
}

func runtimeComponentMaxLength(receiver *jvm.Object) int32 {
	value, ok := receiver.Fields[componentMaxLengthField]
	if !ok {
		return 0
	}
	limit, err := value.Int32()
	if err != nil {
		return 0
	}
	return limit
}

func runtimeTextComponentConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextComponent constructor", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentMaxLengthField] = jvm.IntValue(0)
	attachInputMethodHandler(receiver, jvm.IntValue(0))
	return jvm.VoidValue(), nil
}

// attachInputMethodHandler gives a text component the automaton the
// specification says every one of them owns. A handset builds it in the
// component's own constructor and a title reaches it through the protected
// field rather than constructing one, so a component without one hands the
// title a null to register its listener on.
func attachInputMethodHandler(receiver *jvm.Object, constraint jvm.Value) {
	receiver.Fields[componentInputHandlerField] = jvm.ReferenceValue(&jvm.Object{
		ClassName: runtimeInputMethodHandlerClass,
		Fields:    map[string]jvm.Value{"mode:I": constraint},
	})
}

// runtimeTextComponentConstructorWithText keeps the initial text and the input
// constraint of a text field or text box.
func runtimeTextComponentConstructorWithText(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("text component constructor", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentTextField] = arguments[1]
	receiver.Fields["constraint:I"] = arguments[2]
	attachInputMethodHandler(receiver, arguments[2])
	return jvm.VoidValue(), nil
}

func runtimeTextComponentSetMaxLength(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextComponent.setMaxLength", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentMaxLengthField] = arguments[1]
	return jvm.VoidValue(), nil
}

// runtimeTextComponentGetString answers the component's own text, or the empty
// string when it was never given one.
func runtimeTextComponentGetString(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextComponent.getString", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if value, ok := receiver.Fields[componentTextField]; ok {
		if text, err := value.Reference(); err == nil && text != nil {
			return value, nil
		}
	}
	return jvm.ReferenceValue(vm.NewString("")), nil
}

// runtimeComponentReceiver validates the receiver of an lwc component method
// and guarantees it has a field map to record state in.
func runtimeComponentReceiver(method string, arguments []jvm.Value, count int) (*jvm.Object, error) {
	if len(arguments) != count {
		return nil, fmt.Errorf("%s expected %d arguments, got %d", method, count, len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, fmt.Errorf("%s receiver is null", method)
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	return receiver, nil
}

// runtimeComponentAddComponent records a child in its container and answers the
// child's index, which is what the original runtime returns.
func runtimeComponentAddComponent(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.addComponent", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	child, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if child == nil {
		return jvm.VoidValue(), fmt.Errorf("ContainerComponent.addComponent child is null")
	}
	children, _ := receiver.Native.([]*jvm.Object)
	if len(children) >= maxContainerChildren {
		return jvm.VoidValue(), fmt.Errorf("KTF container child count exceeds %d", maxContainerChildren)
	}
	receiver.Native = append(children, child)
	return jvm.IntValue(int32(len(children))), nil
}

const maxContainerChildren = 256

// runtimeComponentRemoveComponent removes a child by index or by identity,
// depending on which overload the guest called.
func runtimeComponentRemoveComponent(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.removeComponent", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	children, _ := receiver.Native.([]*jvm.Object)
	if arguments[1].Kind() == jvm.ValueReference {
		child, err := arguments[1].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		for index, current := range children {
			if current == child {
				receiver.Native = append(children[:index:index], children[index+1:]...)
				return jvm.VoidValue(), nil
			}
		}
		return jvm.VoidValue(), nil
	}
	index, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if index < 0 || int(index) >= len(children) {
		return jvm.VoidValue(), nil
	}
	receiver.Native = append(children[:index:index], children[index+1:]...)
	return jvm.VoidValue(), nil
}

// runtimeComponentConfigure records the bounds a container assigned to a
// component; component geometry has no separate Host surface, so the values
// only have to survive a later read.
func runtimeComponentConfigure(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("Component.configure", arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	for index, name := range []string{"x:I", "y:I", "width:I", "height:I", "flags:I"} {
		receiver.Fields[name] = arguments[index+1]
	}
	return jvm.VoidValue(), nil
}

// runtimeComponentSetWorkComponent records the shell's work component so a
// later query observes the component the guest installed.
func runtimeComponentSetWorkComponent(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ShellComponent.setWorkComponent", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentWorkField] = arguments[1]
	return jvm.VoidValue(), nil
}

// componentWorkField, componentTitleField, componentTitleTextField and
// componentCommandField are the three slots a shell carries beside its
// children: the component it works with, its title, and the command component
// its soft keys operate.
const (
	componentWorkField      = "workComponent:Lorg/kwis/msp/lwc/Component;"
	componentTitleField     = "title:Lorg/kwis/msp/lwc/Component;"
	componentTitleTextField = "titleText:Ljava/lang/String;"
	componentCommandField   = "command:Lorg/kwis/msp/lwc/Component;"
	componentCommandGrabbed = "commandGrab:Z"
)

// runtimeComponentShown records whether a shell is on the screen, which is the
// whole of what showing one does here.
func runtimeComponentShown(shown bool) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver("ShellComponent show", arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		value := int32(0)
		if shown {
			value = 1
		}
		receiver.Fields["shown:Z"] = jvm.IntValue(value)
		return jvm.VoidValue(), nil
	}
}

// runtimeComponentSetCommand keeps the command component and whether the shell
// was asked to grab the keys for it. Nothing grabs anything: no component here
// is drawn, so no soft key is over one.
func runtimeComponentSetCommand(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ShellComponent.setCommand", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentCommandField] = arguments[1]
	receiver.Fields[componentCommandGrabbed] = arguments[2]
	return jvm.VoidValue(), nil
}

// runtimeComponentAddComponentAt puts a child at a position in the stack. An
// index past the end appends, which is what a container with room does with
// one; a negative index is the caller's mistake and is refused.
func runtimeComponentAddComponentAt(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.addComponent", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	child, err := arguments[2].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if child == nil {
		return jvm.VoidValue(), fmt.Errorf("ContainerComponent.addComponent child is null")
	}
	if index < 0 {
		return jvm.VoidValue(), fmt.Errorf("ContainerComponent.addComponent index %d is negative", index)
	}
	children, _ := receiver.Native.([]*jvm.Object)
	if len(children) >= maxContainerChildren {
		return jvm.VoidValue(), fmt.Errorf("KTF container child count exceeds %d", maxContainerChildren)
	}
	at := int(index)
	if at > len(children) {
		at = len(children)
	}
	children = append(children, nil)
	copy(children[at+1:], children[at:])
	children[at] = child
	receiver.Native = children
	return jvm.VoidValue(), nil
}

// runtimeComponentSetComponentAt replaces the child at a position. A position
// nothing occupies is the caller's mistake and is refused, because the
// alternative is a container that silently grew.
func runtimeComponentSetComponentAt(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.setComponent", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	child, err := arguments[2].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	children, _ := receiver.Native.([]*jvm.Object)
	if index < 0 || int(index) >= len(children) {
		return jvm.VoidValue(), fmt.Errorf("ContainerComponent.setComponent index %d is outside %d children", index, len(children))
	}
	children[index] = child
	receiver.Native = children
	return jvm.VoidValue(), nil
}

// runtimeComponentSetFocus records that this component holds focus. Only one
// component can, so the runtime keeps the newest one.
func runtimeComponentSetFocus(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("Component.setFocus", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.runtimeObjects["lwc:focus"] = receiver
	return jvm.VoidValue(), nil
}

// runtimeComponentBooleanField records a boolean notification on the receiver,
// which is all a component can observe without a widget toolkit behind it.
func runtimeComponentBooleanField(key string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver("Component notification", arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		receiver.Fields[key] = arguments[1]
		return jvm.VoidValue(), nil
	}
}

// runtimeComponentSetField records one argument on the receiver under its own
// name. A component's colours are the case: nothing here paints a component,
// so what a title can observe of setting one is reading it back.
func runtimeComponentSetField(method, key string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver(method, arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		receiver.Fields[key] = arguments[1]
		return jvm.VoidValue(), nil
	}
}

// runtimeComponentKeyNotify reports the key as unconsumed, so the card stack
// below a component keeps receiving it — the original runtime's behavior for a
// component that installed no key handling of its own.
func runtimeComponentKeyNotify(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(1), nil
}

// runtimeComponentRemoveAllComponents empties a container. A title clears a
// form before it rebuilds it, and a call that is not there ends the session
// where a redraw was meant to happen.
func runtimeComponentRemoveAllComponents(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.removeAllComponents", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Native = nil
	return jvm.VoidValue(), nil
}

// runtimeComponentChildren is the one reader of what a container keeps, and it
// reads the same type addComponent writes. They were two types once — the adds
// stored objects and the reads asked for values — so a container answered zero
// children however many it had been given, and a title that walked what it had
// just built saw an empty one.
func runtimeComponentChildren(receiver *jvm.Object) []*jvm.Object {
	children, _ := receiver.Native.([]*jvm.Object)
	return children
}

func runtimeComponentCount(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.getNumberOfComponent", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(runtimeComponentChildren(receiver)))), nil
}

func runtimeComponentAt(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.getComponent", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	children := runtimeComponentChildren(receiver)
	if index < 0 || int(index) >= len(children) {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(children[index]), nil
}

func runtimeComponentIndexOf(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ContainerComponent.getIndexOf", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	wanted, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	for index, child := range runtimeComponentChildren(receiver) {
		if child == wanted {
			return jvm.IntValue(int32(index)), nil
		}
	}
	return jvm.IntValue(-1), nil
}

// componentEventListenerField and componentEventDataField are the pair
// Component.setEventListener is given: who to notify and what to hand back
// with the notification.
const (
	componentEventListenerField = "eventListener:Lorg/kwis/msp/lwc/EventListener;"
	componentEventDataField     = "eventData:Ljava/lang/Object;"
	// componentActionListenerField and componentActionDataField are the same
	// pair under the other name the toolkit uses: a button calls its action
	// listener when the select key is released, where a component calls its
	// event listener for everything. They are kept apart because a title sets
	// both on the same object.
	componentActionListenerField = "actionListener:Lorg/kwis/msp/lwc/ActionListener;"
	componentActionDataField     = "actionData:Ljava/lang/Object;"
)

// runtimeComponentSetEventListener keeps both halves. Nothing fires them —
// no component here is drawn, so none is operated — and keeping them is what
// lets a title read back whether it has already wired its own dialog.
func runtimeComponentSetEventListener(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("Component.setEventListener", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentEventListenerField] = arguments[1]
	receiver.Fields[componentEventDataField] = arguments[2]
	return jvm.VoidValue(), nil
}

// runtimeComponentSetActionListener keeps the pair a button is given. Nothing
// fires it for the reason nothing fires the event listener: the specification
// says a button calls its listener when the select key is released on it, and
// no button here is drawn to be pressed. A title that wires one and reads it
// back sees what it wired.
func runtimeComponentSetActionListener(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("Component.setActionListener", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentActionListenerField] = arguments[1]
	receiver.Fields[componentActionDataField] = arguments[2]
	return jvm.VoidValue(), nil
}

// runtimeComponentField answers one reference field, or null when it has never
// been set.
func runtimeComponentField(key string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver("Component field "+key, arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		value, ok := receiver.Fields[key]
		if !ok {
			return jvm.ReferenceValue(nil), nil
		}
		return value, nil
	}
}
