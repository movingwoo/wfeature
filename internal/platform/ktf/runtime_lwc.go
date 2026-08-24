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
	runtimeEventListenerClass      = "org/kwis/msp/lwc/EventListener"

	componentTextField      = "text:Ljava/lang/String;"
	componentMaxLengthField = "maxLength:I"
)

func runtimeTextComponentClassDefinition() runtimeJavaClass {
	const class = runtimeTextComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeTextComponentConstructor},
			{class: class, name: "setMaxLength", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeTextComponentSetMaxLength},
			{class: class, name: "getString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeTextComponentGetString},
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
		},
	}
}

func runtimeTextBoxComponentClassDefinition() runtimeJavaClass {
	const class = runtimeTextBoxComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeTextComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeTextComponentConstructorWithText},
		},
	}
}

func runtimeTextComponentConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("TextComponent constructor", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentMaxLengthField] = jvm.IntValue(0)
	return jvm.VoidValue(), nil
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
	receiver.Fields["workComponent:Lorg/kwis/msp/lwc/Component;"] = arguments[1]
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

func runtimeComponentChildren(receiver *jvm.Object) []jvm.Value {
	children, _ := receiver.Native.([]jvm.Value)
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
	return children[index], nil
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
		if object, referenceErr := child.Reference(); referenceErr == nil && object == wanted {
			return jvm.IntValue(int32(index)), nil
		}
	}
	return jvm.IntValue(-1), nil
}
