package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// `com/ktf/kfc` is the vendor's own widget toolkit, and exactly one local
// title asks for it. **What it asks for is a text-entry dialog**, not a
// toolkit in general: the five classes it names are a form, a form with a
// menu bar, a message box, a text field and the listener that hears the field
// change, and the members it resolves around them are the ones a modal
// "type your name" box needs — `doModal`, `setString`, `getString`,
// `setMaxLength`, `getGTextListener`, and the geometry a form is given.
//
// The whole surface was read out of the module's own string pool rather than
// discovered one failure at a time: an AOT module stores every name and
// descriptor it links against verbatim, so a scan for `com/ktf/kfc/` and the
// entries beside it lists the demand in one pass. See docs/ktf.md, "A widget
// toolkit that is one text box".
//
// **These classes answer rather than draw.** Nothing here puts a dialog in
// front of anyone: a form is constructed, its geometry is kept, and a modal
// dialog closes immediately with the field holding the text it was given.
// That is a fixed-value answer and it is recorded as one in
// testdata/wipi_java_stubs.txt. The editing surface the field would need
// already exists for the lwc components (runtime_lwc_input.go), so a Host that
// grows a text overlay can give this one the same one — the reason it is not
// wired here is that nothing draws the form the field would sit in.
const (
	runtimeGFormClass          = "com/ktf/kfc/GForm"
	runtimeGMenubarFormClass   = "com/ktf/kfc/GMenubarForm"
	runtimeGMsgBoxClass        = "com/ktf/kfc/GMsgBox"
	runtimeGTextFieldClass     = "com/ktf/kfc/GTextField"
	runtimeGTextListenerClass  = "com/ktf/kfc/GTextListener"
	runtimeChoiceTextClass     = "com/ktf/kfc/ChoiceText"
	runtimeGFormComponentClass = "com/ktf/kfc/GFormComponent"
	runtimeDMInfoClass         = "wec/DMInfo"

	// componentShownField is whether a form has been shown. `doModal` closes
	// at once, so a form is never shown for longer than the call.
	componentShownField = "shown:Z"
)

// runtimeGFormClassDefinition publishes the form and the two classes built on
// it. A form is a shell: a window with children, which is what
// `ShellComponent` already is here.
func runtimeGFormClassDefinition(class, super string) runtimeJavaClass {
	return runtimeJavaClass{
		name:        class,
		superName:   super,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGFormConstructor},
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lcdui/Display;IIII)V", accessFlags: 0x0001, implementation: runtimeGFormConstructor},
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lcdui/Display;IIIIZ)V", accessFlags: 0x0001, implementation: runtimeGFormConstructor},
			// doModal shows the form and waits for it to be dismissed. There
			// is nothing to show and nobody to dismiss it, so it closes at
			// once and answers zero — which is what a dialog closed without a
			// choice answers.
			{class: class, name: "doModal", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeGFormDoModal},
			{class: class, name: "show", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "hide", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeGFormHide},
			{class: class, name: "isShown", descriptor: "()Z", accessFlags: 0x0001, implementation: runtimeCardIntField(componentShownField, 0)},
			{class: class, name: "showNotify", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "layout", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "move", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "resize", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "setTransparent", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "setPluginLayer", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "getPluginPosition", descriptor: "(I)I", accessFlags: 0x0001, implementation: runtimeComponentZero},
		},
	}
}

// runtimeGTextFieldClassDefinition is the field itself. It is a text component
// of the same kind the lwc classes publish, so the text it holds and the limit
// it was given live in the same two fields and read back through the same
// helpers.
func runtimeGTextFieldClassDefinition() runtimeJavaClass {
	const class = runtimeGTextFieldClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeTextComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(Lcom/ktf/kfc/GMenubarForm;Ljava/lang/String;I)V", accessFlags: 0x0001, implementation: runtimeGTextFieldConstructor},
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lwc/Component;Ljava/lang/String;IIIII)V", accessFlags: 0x0001, implementation: runtimeGTextFieldConstructor},
			{class: class, name: "setString", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeGTextFieldSetString},
			{class: class, name: "getString", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeTextComponentGetString},
			{class: class, name: "setMaxLength", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeTextComponentSetMaxLength},
			{class: class, name: "getGTextListener", descriptor: "()Lcom/ktf/kfc/GTextListener;", accessFlags: 0x0001, implementation: runtimeGTextFieldListener},
			{class: class, name: "setGTextListener", descriptor: "(Lcom/ktf/kfc/GTextListener;)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("GTextField.setGTextListener", componentTextListenerField)},
			{class: class, name: "showNotify", descriptor: "(Z)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "move", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "resize", descriptor: "(II)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

// runtimeGTextListenerClassDefinition is what a field hands back for its
// listener. It is a class rather than an interface here because a title
// operates it directly — it sets the input modes the field should offer
// before the field is ever shown — so the object has to answer those calls
// rather than only exist to be implemented.
func runtimeGTextListenerClassDefinition() runtimeJavaClass {
	const class = runtimeGTextListenerClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			// The input modes a field offers — Hangul, alphabet, digits — as
			// the identifiers the input-method table uses. There is no
			// composing overlay to offer them in, so they are kept and not
			// acted on.
			{class: class, name: "setIMEModes", descriptor: "([I)V", accessFlags: 0x0001, implementation: runtimeComponentSetField("GTextListener.setIMEModes", componentIMEModesField)},
			{class: class, name: "textChanged", descriptor: "(Lcom/ktf/kfc/GTextField;)V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
		},
	}
}

// runtimeGFormComponentClassDefinition is the toolkit's form as a component: a
// shell a title fills with widgets and hangs off its own card. It is the shell
// the lwc classes already publish, plus the one call the vendor added — a child
// with the rectangle it goes in, which the lwc form makes a title compute for
// itself.
//
// **The rectangle is dropped.** Nothing here lays a container out or draws one,
// so where a child was asked to go decides nothing; what a title reads back is
// the order of its children, and that is what the shell already keeps.
func runtimeGFormComponentClassDefinition() runtimeJavaClass {
	const class = runtimeGFormComponentClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeShellComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "<init>", descriptor: "(IIII)V", accessFlags: 0x0001, implementation: runtimeGFormConstructor},
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lcdui/Display;IIII)V", accessFlags: 0x0001, implementation: runtimeGFormConstructor},
			{class: class, name: "addComponent", descriptor: "(Lorg/kwis/msp/lwc/Component;IIII)I", accessFlags: 0x0001, implementation: runtimeGFormAddComponent},
		},
	}
}

// runtimeGFormAddComponent adds the child and drops the rectangle, then answers
// the index the shell's own add would have.
func runtimeGFormAddComponent(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 6 {
		return jvm.VoidValue(), fmt.Errorf("GFormComponent.addComponent expected receiver, child and four bounds, got %d arguments", len(arguments))
	}
	return runtimeComponentAddComponent(runtime, vm, arguments[:2])
}

// runtimeChoiceTextClassDefinition is the toolkit's list-of-strings widget: a
// component constructed from the choices it offers. A second local title puts
// one in a form beside the lwc components, holds it in a field of its own, and
// works it through the component calls the form already answers.
//
// **It holds its choices and its selection and draws neither.** Nothing here
// puts a list in front of anyone, for the same reason the dialog above does not
// draw: there is no toolkit surface to draw it on. What a title can do is ask
// which entry is chosen, and the answer is the first one, which is what a list
// nobody has moved through is showing.
func runtimeChoiceTextClassDefinition() runtimeJavaClass {
	const class = runtimeChoiceTextClass
	return runtimeJavaClass{
		name:        class,
		superName:   runtimeComponentClass,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "([Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeChoiceTextConstructor},
		},
	}
}

// choiceTextChoicesField is the array a choice list was constructed from, and
// choiceTextSelectedField is the entry a title is looking at.
const (
	choiceTextChoicesField  = "choices:[Ljava/lang/String;"
	choiceTextSelectedField = "selected:I"
)

func runtimeChoiceTextConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("ChoiceText constructor", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[choiceTextChoicesField] = arguments[1]
	receiver.Fields[choiceTextSelectedField] = jvm.IntValue(0)
	return jvm.VoidValue(), nil
}

// componentIMEModesField is the mode list a listener was given.
const componentIMEModesField = "imeModes:[I"

// componentTextListenerField is the listener a field was given, handed back by
// getGTextListener. Nothing fires it: the text never changes on its own.
const componentTextListenerField = "listener:Lcom/ktf/kfc/GTextListener;"

// runtimeKFCReceiver takes the receiver of a constructor whose argument count
// varies with the overload, which is every form constructor here: the three
// take four, five and six arguments for the same object.
func runtimeKFCReceiver(method string, arguments []jvm.Value) (*jvm.Object, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("%s expected a receiver", method)
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

func runtimeGFormConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeKFCReceiver("GForm constructor", arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentShownField] = jvm.IntValue(0)
	return jvm.VoidValue(), nil
}

func runtimeGFormHide(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("GForm.hide", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Fields[componentShownField] = jvm.IntValue(0)
	return jvm.VoidValue(), nil
}

func runtimeGFormDoModal(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtimeComponentReceiver("GForm.doModal", arguments, 1); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.countDiagnostic("kfc doModal answered without a dialog")
	return jvm.IntValue(0), nil
}

func runtimeGTextFieldConstructor(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeKFCReceiver("GTextField constructor", arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text := ""
	if len(arguments) > 2 {
		if object, refErr := arguments[2].Reference(); refErr == nil && object != nil {
			text, _ = jvm.StringText(object)
		}
	}
	receiver.Fields[componentTextField] = jvm.ReferenceValue(vm.NewString(text))
	receiver.Fields[componentMaxLengthField] = jvm.IntValue(0)
	attachInputMethodHandler(receiver, jvm.IntValue(0))
	if len(arguments) > 3 {
		if length, intErr := arguments[3].Int32(); intErr == nil && length > 0 {
			receiver.Fields[componentMaxLengthField] = jvm.IntValue(length)
		}
	}
	return jvm.VoidValue(), nil
}

func runtimeGTextFieldSetString(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("GTextField.setString", arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text := ""
	if object, refErr := arguments[1].Reference(); refErr == nil && object != nil {
		text, _ = jvm.StringText(object)
	}
	receiver.Fields[componentTextField] = jvm.ReferenceValue(vm.NewString(text))
	return jvm.VoidValue(), nil
}

// runtimeGTextFieldListener answers the field's listener, building one the
// first time it is asked for. A field always has one on the handset — it is
// how the toolkit reports a change back — and a title that asks for it walks
// into it straight away, so answering null is how one ends up throwing its own
// NullPointerException inside its constructor.
func runtimeGTextFieldListener(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := runtimeComponentReceiver("GTextField.getGTextListener", arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if value, ok := receiver.Fields[componentTextListenerField]; ok {
		if object, refErr := value.Reference(); refErr == nil && object != nil {
			return value, nil
		}
	}
	listener := jvm.ReferenceValue(&jvm.Object{ClassName: runtimeGTextListenerClass, Fields: map[string]jvm.Value{}})
	receiver.Fields[componentTextListenerField] = listener
	runtime.countDiagnostic("kfc text listener created")
	return listener, nil
}

// runtimeDMInfoClassDefinition publishes the one vendor class outside
// `com/ktf/kfc` a local title asks for. It is not the title's own — the archive
// ships no class file for it and the module names it the way it names every
// other class the handset provides — and the only member the module links
// against is the static that answers the instance.
//
// **What it is for is not known and nothing here pretends to know.** The
// instance exists so the call the title makes has something to answer with; a
// member asked of it that is not here fails by name, which is the evidence the
// next round would need.
func runtimeDMInfoClassDefinition() runtimeJavaClass {
	const class = runtimeDMInfoClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeComponentNoop},
			{class: class, name: "getDMInfo", descriptor: "()Lwec/DMInfo;", accessFlags: 0x0009, implementation: runtimeDMInfoInstance},
			// The one thing the local title asks the instance is the handset's
			// MIN — its subscriber number. That is a number this platform
			// already has one answer for, the one `MC_knlGetSysProperty`
			// reports for `PHONENUMBER` and the one a certificate is sealed
			// with, so it is the answer here too: two calls that ask the
			// handset who it is must not disagree. See docs/network.md for
			// where the number comes from and what it costs.
			{class: class, name: "gethandsetMIN", descriptor: "()Ljava/lang/String;", accessFlags: 0x0001, implementation: runtimeDMInfoHandsetNumber},
		},
	}
}

// runtimeDMInfoInstance answers the one instance, built the first time it is
// asked for. A title compares what it got with what it got last time.
func runtimeDMInfoInstance(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	instance := runtime.runtimeObjects[runtimeDMInfoClass]
	if instance == nil {
		instance = &jvm.Object{ClassName: runtimeDMInfoClass, Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects[runtimeDMInfoClass] = instance
	}
	return jvm.ReferenceValue(instance), nil
}

// runtimeDMInfoHandsetNumber answers the subscriber number, which is what a MIN
// is. It is the same number every other question about this handset's identity
// is answered with.
func runtimeDMInfoHandsetNumber(_ *initializationRuntime, vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(vm.NewString(HandsetNumber())), nil
}
