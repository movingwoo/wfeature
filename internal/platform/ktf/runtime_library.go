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
	runtimeLEDClass               = "org/kwis/msp/handset/LED"
	runtimeLabelComponentClass    = "org/kwis/msp/lwc/LabelComponent"
	runtimeDialogComponentClass   = "org/kwis/msp/lwc/DialogComponent"
	runtimeFormComponentClass     = "org/kwis/msp/lwc/FormComponent"
	runtimeProgressComponentClass = "org/kwis/msp/lwc/ProgressComponent"
	runtimeOEMDeviceClass         = "wec/OEMDevice"
	runtimeSYSThemeClass          = "wec/SYSTheme"
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

// The dialog's own constants, values and all, from the specification's field
// list: the three types at 0/1/2, the three doModal answers at 10/11/12, the
// two button identifiers at 20/21, and the infinite timeout at -1. A title
// reads them to build the dialog and to read the answer back, so they are as
// much of the contract as the constructors are.
const (
	runtimeDialogTypeNone      int32 = 0
	runtimeDialogTypeOK        int32 = 1
	runtimeDialogTypeOKCancel  int32 = 2
	runtimeDialogTimeout       int32 = 10
	runtimeDialogOK            int32 = 11
	runtimeDialogCancel        int32 = 12
	runtimeDialogOKButton      int32 = 20
	runtimeDialogCancelButton  int32 = 21
	runtimeDialogInfinite      int32 = -1
	runtimeDialogDefaultMillis int32 = 3000
)

const (
	runtimeDialogTypeField    = "type:I"
	runtimeDialogTimeoutField = "timeout:I"
	runtimeDialogActionField  = "actionState:I"
	runtimeDialogTitleField   = "title:Ljava/lang/String;"
	runtimeDialogWorkField    = "cmpWork:Lorg/kwis/msp/lwc/Component;"
)

// runtimeDialogComponentClassDefinition is the lwc modal dialog. There is no
// widget toolkit behind the component hierarchy here, so what a dialog does is
// keep the type, title, timeout and data component it was built with and
// answer with them. doModal is deliberately absent: showing a dialog and
// waiting for a person to choose is the one thing on this class that cannot be
// answered without inventing the choice, and a title that reaches it should
// stop with its name in the message rather than be told a button was pressed.
func runtimeDialogComponentClassDefinition() runtimeJavaClass {
	const class = runtimeDialogComponentClass
	constants := []struct {
		name  string
		value int32
	}{
		{"TYPE_NONE", runtimeDialogTypeNone},
		{"TYPE_OK", runtimeDialogTypeOK},
		{"TYPE_OK_CANCEL", runtimeDialogTypeOKCancel},
		{"DLG_TIMEOUT", runtimeDialogTimeout},
		{"DLG_OK", runtimeDialogOK},
		{"DLG_CANCEL", runtimeDialogCancel},
		{"OK_BUTTON", runtimeDialogOKButton},
		{"CANCEL_BUTTON", runtimeDialogCancelButton},
		{"TIMEOUT_INFINITE", runtimeDialogInfinite},
	}
	fields := make([]runtimeJavaField, 0, len(constants))
	for _, constant := range constants {
		value := uint32(constant.value)
		fields = append(fields, runtimeJavaField{
			name:        constant.name,
			descriptor:  "I",
			accessFlags: 0x0019,
			initializer: func(*initializationRuntime) (uint32, error) { return value, nil },
		})
	}
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeShellComponentClass,
		accessFlags: 0x0021,
		fields:      fields,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeDialogConstructor},
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lwc/Component;Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeDialogConstructor},
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lwc/Component;Ljava/lang/String;IIIII)V", accessFlags: 0x0001, implementation: runtimeDialogConstructor},
			{class: class, name: "setType", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeDialogSetType},
			{class: class, name: "setTimeout", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeDialogSetTimeout},
			{class: class, name: "getTimeout", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDialogGetInt(runtimeDialogTimeoutField)},
			{class: class, name: "getActionState", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDialogGetInt(runtimeDialogActionField)},
			// A button's text is the dialog's to draw and nothing here draws
			// it, but the call has to return: the specification says a
			// TYPE_NONE dialog ignores it outright, so discarding it is a
			// documented answer rather than a silent one.
			{class: class, name: "setButtonString", descriptor: "(ILjava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "doModal", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeDialogDoModal},
			{class: class, name: "layout", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "show", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

// runtimeDialogConstructor records what the dialog was built with. All three
// forms end in the type, and the two longer ones put the data component and
// the title before it; the geometry the seven-argument form adds is the
// toolkit's to use, and there is none.
func runtimeDialogConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("DialogComponent constructor", arguments, len(arguments))
	if err != nil {
		return jvm.VoidValue(), err
	}
	typeIndex := 1
	if len(arguments) >= 4 {
		receiver.Fields[runtimeDialogWorkField] = arguments[1]
		receiver.Fields[runtimeDialogTitleField] = arguments[2]
		typeIndex = 3
	}
	if len(arguments) <= typeIndex {
		return jvm.VoidValue(), fmt.Errorf("DialogComponent constructor expected a type, got %d arguments", len(arguments))
	}
	return runtimeDialogApplyType(receiver, arguments[typeIndex])
}

func runtimeDialogSetType(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("DialogComponent.setType", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtimeDialogApplyType(receiver, arguments[1])
}

// runtimeDialogApplyType refuses a type outside the three the specification
// names, which is what it says happens, and resets the timeout to that type's
// default: three seconds for the one that closes itself, and no limit for the
// two that wait for a person.
func runtimeDialogApplyType(receiver *jvm.Object, value jvm.Value) (jvm.Value, error) {
	kind, err := value.Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if kind != runtimeDialogTypeNone && kind != runtimeDialogTypeOK && kind != runtimeDialogTypeOKCancel {
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: "DialogComponent type"},
			Message: fmt.Sprintf("DialogComponent type %d", kind),
		}
	}
	timeout := runtimeDialogInfinite
	if kind == runtimeDialogTypeNone {
		timeout = runtimeDialogDefaultMillis
	}
	receiver.Fields[runtimeDialogTypeField] = jvm.IntValue(kind)
	receiver.Fields[runtimeDialogTimeoutField] = jvm.IntValue(timeout)
	// Nothing has happened to the dialog yet, and the specification's own
	// resting value for that is -2 rather than zero, which is a button.
	receiver.Fields[runtimeDialogActionField] = jvm.IntValue(-2)
	return jvm.VoidValue(), nil
}

// runtimeDialogSetTimeout takes the milliseconds the dialog stays up. Asking a
// TYPE_NONE dialog to stay for ever turns it into TYPE_OK, which is the one
// place the specification lets a timeout change the type.
func runtimeDialogSetTimeout(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("DialogComponent.setTimeout", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	timeout, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if timeout == runtimeDialogInfinite {
		if kind, ok := receiver.Fields[runtimeDialogTypeField]; ok {
			if value, err := kind.Int32(); err == nil && value == runtimeDialogTypeNone {
				receiver.Fields[runtimeDialogTypeField] = jvm.IntValue(runtimeDialogTypeOK)
			}
		}
	}
	receiver.Fields[runtimeDialogTimeoutField] = jvm.IntValue(timeout)
	return jvm.VoidValue(), nil
}

func runtimeDialogGetInt(field string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := runtimeComponentReceiver("DialogComponent field read", arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if value, ok := receiver.Fields[field]; ok {
			return value, nil
		}
		return jvm.IntValue(0), nil
	}
}

// runtimeDialogDoModal answers the dialog without showing it, because nothing
// here paints a component and so nobody can be shown one. That leaves the
// outcome to be decided from the type, and for two of the three the type
// decides it outright: a TYPE_NONE dialog has no buttons and ends in
// DLG_TIMEOUT by itself, and a TYPE_OK dialog has one, so DLG_OK is not a
// choice made on a person's behalf but the only answer that exists. The
// timeout is not waited out either way: the dialog is not on screen, so the
// wait would cost a title its frames and show nothing.
//
// TYPE_OK_CANCEL is the one that genuinely has two answers, and this takes the
// affirmative. It is a guess and it is recorded as one, in the stub inventory
// and in the run's diagnostics, because unlike everything else here the game
// believes a person answered. Two things argue for OK over CANCEL: it is the
// button a dialog opens focused on, so it is what pressing the confirm key
// without reading gets; and a title that offers a choice usually offers it
// once, where refusing every dialog is what leaves one asking again. What
// would end the guess is a Host boundary that puts the dialog in front of the
// person, the way internal/textinput already does for a text field.
func runtimeDialogDoModal(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("DialogComponent.doModal", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	kind := runtimeDialogTypeOK
	if value, ok := receiver.Fields[runtimeDialogTypeField]; ok {
		if current, err := value.Int32(); err == nil {
			kind = current
		}
	}
	action := runtimeDialogOK
	if kind == runtimeDialogTypeNone {
		action = runtimeDialogTimeout
	}
	runtime.countDiagnostic(fmt.Sprintf("dialog answered unseen type=%d action=%d", kind, action))
	receiver.Fields[runtimeDialogActionField] = jvm.IntValue(action)
	return jvm.IntValue(action), nil
}

// runtimeFormComponentClassDefinition is the container that stacks its children
// in a row or a column. There is no layout here, so what a form does is keep
// the two settings that describe one — the direction it was built with and the
// gap between children — and answer with them; the children themselves are
// ContainerComponent's, which already holds them.
func runtimeFormComponentClassDefinition() runtimeJavaClass {
	const class = runtimeFormComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeContainerComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "<init>", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("FormComponent constructor", "vertical:Z")},
			{class: class, name: "setGab", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("FormComponent.setGab", "gab:I")},
			{class: class, name: "getGab", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("gab:I", 0)},
			{class: class, name: "setPacked", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("FormComponent.setPacked", "packed:Z")},
			{class: class, name: "getPacked", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeCardIntField("packed:Z", 0)},
			{class: class, name: "setFocus", descriptor: "(Lorg/kwis/msp/lwc/Component;)V", accessFlags: 0x0001, implementation: runtimeFormSetFocus},
			{class: class, name: "layout", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

// runtimeFormSetFocus moves the focus to the named child rather than to the
// form, which is the whole difference between this and Component.setFocus.
func runtimeFormSetFocus(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtimeComponentReceiver("FormComponent.setFocus", arguments, 2); err != nil {
		return jvm.VoidValue(), err
	}
	child, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if child != nil {
		runtime.runtimeObjects["lwc:focus"] = child
	}
	return jvm.VoidValue(), nil
}

// runtimeProgressComponentClassDefinition is the progress bar. Nothing paints
// it, so what it does is hold the three numbers that describe one — the
// maximum, the current value and the step they move in — and keep them
// consistent the way the specification says: a value is rounded down to a
// whole number of steps, and a maximum at or below zero is refused.
func runtimeProgressComponentClassDefinition() runtimeJavaClass {
	const class = runtimeProgressComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(ZI)V", accessFlags: 0x0001, implementation: runtimeProgressConstructor},
			{class: class, name: "setMaxValue", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeProgressSetMaxValue},
			{class: class, name: "getMaxValue", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("maxValue:I", 0)},
			{class: class, name: "setValue", descriptor: "(I)I", accessFlags: 0x0001, implementation: runtimeProgressSetValue},
			{class: class, name: "getValue", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("value:I", 0)},
			{class: class, name: "setStep", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeProgressSetStep},
			{class: class, name: "getStep", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeCardIntField("step:I", 0)},
			{class: class, name: "setMargin", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

func runtimeProgressConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ProgressComponent constructor", arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields["interactive:Z"] = arguments[1]
	receiver.Fields["value:I"] = jvm.IntValue(0)
	receiver.Fields["step:I"] = jvm.IntValue(0)
	return runtimeProgressApplyMaxValue(receiver, arguments[2])
}

func runtimeProgressSetMaxValue(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ProgressComponent.setMaxValue", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtimeProgressApplyMaxValue(receiver, arguments[1])
}

func runtimeProgressApplyMaxValue(receiver *jvm.Object, value jvm.Value) (jvm.Value, error) {
	maximum, err := value.Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if maximum <= 0 {
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: "ProgressComponent maximum"},
			Message: fmt.Sprintf("ProgressComponent maximum %d", maximum),
		}
	}
	receiver.Fields["maxValue:I"] = jvm.IntValue(maximum)
	return jvm.VoidValue(), nil
}

// runtimeProgressSetValue clamps into the bar and answers what it settled on,
// which is what the specification's own return value is for: a caller that
// asked for more than the maximum finds out here rather than by reading back.
func runtimeProgressSetValue(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ProgressComponent.setValue", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	value = runtimeProgressRound(receiver, value)
	receiver.Fields["value:I"] = jvm.IntValue(value)
	return jvm.IntValue(value), nil
}

// runtimeProgressSetStep changes the unit the bar moves in, and moves the
// current value onto it — the specification says a value that no longer sits
// on a step boundary follows the new step.
func runtimeProgressSetStep(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ProgressComponent.setStep", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	step, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	maximum := runtimeProgressField(receiver, "maxValue:I")
	if step <= 0 || step > maximum {
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: "ProgressComponent step"},
			Message: fmt.Sprintf("ProgressComponent step %d of %d", step, maximum),
		}
	}
	receiver.Fields["step:I"] = jvm.IntValue(step)
	receiver.Fields["value:I"] = jvm.IntValue(runtimeProgressRound(receiver, runtimeProgressField(receiver, "value:I")))
	return jvm.VoidValue(), nil
}

// runtimeProgressRound holds a value inside the bar and on a step boundary.
func runtimeProgressRound(receiver *jvm.Object, value int32) int32 {
	maximum := runtimeProgressField(receiver, "maxValue:I")
	if value < 0 {
		value = 0
	}
	if value > maximum {
		value = maximum
	}
	if step := runtimeProgressField(receiver, "step:I"); step > 0 {
		value -= value % step
	}
	return value
}

func runtimeProgressField(receiver *jvm.Object, key string) int32 {
	value, ok := receiver.Fields[key]
	if !ok {
		return 0
	}
	number, err := value.Int32()
	if err != nil {
		return 0
	}
	return number
}
