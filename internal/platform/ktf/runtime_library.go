package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The class-library surface a sweep of a local archive set asked for and did
// not get. A member the runtime does not publish is a stop rather than a wrong
// answer — the AOT bridge resolves by name and descriptor — so each class here
// exists because a title reached for it and ended the session.
//
// Everything with a nil implementation is metadata over a JVM-owned body; the
// ones with implementations are the platform's own, where the specification
// says what the answer is.

const (
	runtimeLEDClass            = "org/kwis/msp/handset/LED"
	runtimeLabelComponentClass = "org/kwis/msp/lwc/LabelComponent"
	runtimeOEMDeviceClass      = "wec/OEMDevice"
	runtimeSYSThemeClass       = "wec/SYSTheme"
)

// runtimeThrowableClassDefinition publishes the root of the exception
// hierarchy. A title reaches it to print what it caught, and every runtime
// exception class already names it through its parents, so the class being
// absent turned a handled failure into an unhandled one.
func runtimeThrowableClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        "java/lang/Throwable",
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: "java/lang/Throwable", name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeThrowableConstructor},
			{class: "java/lang/Throwable", name: "<init>", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeThrowableConstructor},
			{class: "java/lang/Throwable", name: "getMessage", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeThrowableMessage},
			{class: "java/lang/Throwable", name: "toString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeThrowableText},
			// The stack a trace would print is the guest's own ARM frames,
			// which this runtime does not walk. Discarding the call is what
			// the console printing beside it already does.
			{class: "java/lang/Throwable", name: "printStackTrace", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

func runtimeThrowableMessage(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("Throwable.getMessage", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, ok := receiver.Native.(string)
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(text)), nil
}

func runtimeThrowableText(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("Throwable.toString", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text := receiver.ClassName
	if message, ok := receiver.Native.(string); ok && message != "" {
		text += ": " + message
	}
	return jvm.ReferenceValue(vm.NewString(text)), nil
}

// runtimeLEDClassDefinition is the handset's external indicator lights. There
// are none here, so the count is zero and the bit mask a title sets is kept so
// that reading it back answers what was written — which is all a title can
// observe of a light this Host has no way to show.
func runtimeLEDClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        runtimeLEDClass,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		fields: []runtimeJavaField{
			{name: "leds", descriptor: "I", accessFlags: 0x000a},
		},
		methods: []runtimeJavaMethod{
			{class: runtimeLEDClass, name: "set", descriptor: "(I)V", accessFlags: 0x0009, implementation: runtimeLEDSet},
			{class: runtimeLEDClass, name: "get", descriptor: "()I", accessFlags: 0x0009, implementation: runtimeLEDGet},
			{class: runtimeLEDClass, name: "getCount", descriptor: "()I", accessFlags: 0x0009, implementation: runtimeComponentZero},
		},
	}
}

func runtimeLEDSet(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("LED.set expected one argument, got %d", len(arguments))
	}
	value, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.leds = value
	return jvm.VoidValue(), nil
}

func runtimeLEDGet(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(runtime.leds), nil
}

// runtimeLabelComponentClassDefinition is the lwc component that shows a string
// and an image. There is no widget toolkit behind the component hierarchy here,
// so what it does is keep what it was given and answer with it; a title that
// puts one on screen draws the text itself.
func runtimeLabelComponentClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        runtimeLabelComponentClass,
		superName:   runtimeComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: runtimeLabelComponentClass, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: runtimeLabelComponentClass, name: "<init>", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeLabelConstructor},
			{class: runtimeLabelComponentClass, name: "<init>", descriptor: "(Ljava/lang/String;Lorg/kwis/msp/lcdui/Image;)V", accessFlags: 0x0001, implementation: runtimeLabelConstructor},
			{class: runtimeLabelComponentClass, name: "setLabel", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeLabelSetLabel},
			{class: runtimeLabelComponentClass, name: "getLabel", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeLabelGetLabel},
			{class: runtimeLabelComponentClass, name: "setImage", descriptor: "(Lorg/kwis/msp/lcdui/Image;)V", accessFlags: 0x0001, implementation: runtimeLabelSetObject("m_image:Lorg/kwis/msp/lcdui/Image;")},
			{class: runtimeLabelComponentClass, name: "getImage", descriptor: "()Lorg/kwis/msp/lcdui/Image;", accessFlags: 0x0001, implementation: runtimeLabelGetObject("m_image:Lorg/kwis/msp/lcdui/Image;")},
			{class: runtimeLabelComponentClass, name: "setFont", descriptor: "(Lorg/kwis/msp/lcdui/Font;)V", accessFlags: 0x0001, implementation: runtimeLabelSetObject("m_ft:Lorg/kwis/msp/lcdui/Font;")},
			{class: runtimeLabelComponentClass, name: "getFont", descriptor: "()Lorg/kwis/msp/lcdui/Font;", accessFlags: 0x0001, implementation: runtimeLabelGetObject("m_ft:Lorg/kwis/msp/lcdui/Font;")},
			{class: runtimeLabelComponentClass, name: "setLayout", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeLabelSetLayout},
		},
	}
}

const runtimeLabelTextField = "m_str:Ljava/lang/String;"

func runtimeLabelConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("LabelComponent constructor", arguments, len(arguments))
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) >= 2 {
		receiver.Fields[runtimeLabelTextField] = arguments[1]
	}
	if len(arguments) >= 3 {
		receiver.Fields["m_image:Lorg/kwis/msp/lcdui/Image;"] = arguments[2]
	}
	return jvm.VoidValue(), nil
}

func runtimeLabelSetLabel(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("LabelComponent.setLabel", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[runtimeLabelTextField] = arguments[1]
	return jvm.VoidValue(), nil
}

func runtimeLabelGetLabel(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("LabelComponent.getLabel", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if value, ok := receiver.Fields[runtimeLabelTextField]; ok {
		return value, nil
	}
	return jvm.ReferenceValue(nil), nil
}

func runtimeLabelSetObject(key string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver("LabelComponent setter", arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		receiver.Fields[key] = arguments[1]
		return jvm.VoidValue(), nil
	}
}

func runtimeLabelGetObject(key string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver("LabelComponent getter", arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if value, ok := receiver.Fields[key]; ok {
			return value, nil
		}
		return jvm.ReferenceValue(nil), nil
	}
}

func runtimeLabelSetLayout(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("LabelComponent.setLayout", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields["layout:I"] = arguments[1]
	return jvm.VoidValue(), nil
}

// runtimeOEMDeviceClassDefinition is a vendor class outside the WIPI
// specification that two titles of one publisher's toolkit call before they
// draw anything. What they ask for is a sleep lock and the handset's theme.
//
// There is no reference for what either answers, so the answers are the ones a
// caller can do least with and still proceed: sleep is not disabled, and the
// theme is an object with nothing on it. A title that reads a colour off the
// theme will fail at that call rather than here, which is the point where there
// would be evidence about what the field is.
func runtimeOEMDeviceClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        runtimeOEMDeviceClass,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: runtimeOEMDeviceClass, name: "enableSleep", descriptor: "(Z)Z", accessFlags: 0x0009, implementation: runtimeComponentZero},
			{class: runtimeOEMDeviceClass, name: "getSYSTheme", descriptor: "()Lwec/SYSTheme;", accessFlags: 0x0009, implementation: runtimeOEMDeviceTheme},
		},
	}
}

func runtimeOEMDeviceTheme(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	theme := runtime.runtimeObjects[runtimeSYSThemeClass]
	if theme == nil {
		theme = &jvm.Object{ClassName: runtimeSYSThemeClass, Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects[runtimeSYSThemeClass] = theme
	}
	return jvm.ReferenceValue(theme), nil
}

func runtimeSYSThemeClassDefinition() runtimeJavaClass {
	return runtimeJavaClass{
		name:        runtimeSYSThemeClass,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
	}
}
