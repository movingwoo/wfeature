package main

import "testing"

// The scan is only as good as what it believes the runtime answers, so what is
// tested is that belief: a method the library declares, one a built-in native
// answers without a declaration, one inherited from a superclass, and one that
// is genuinely absent have to come back apart from each other.
func TestSurfaceKnowsWhatTheRuntimeAnswers(t *testing.T) {
	shape := newSurface()

	for _, present := range []struct {
		class      string
		name       string
		descriptor string
		why        string
	}{
		{"java/lang/String", "length", "()I", "declared in the core library"},
		{"java/lang/Integer", "parseInt", "(Ljava/lang/String;)I", "a built-in native behind a declaration"},
		{"java/io/DataInputStream", "close", "()V", "inherited from java/io/InputStream"},
		{"javax/microedition/lcdui/Canvas", "repaint", "()V", "declared by MIDP"},
		{"com/xce/io/XFile", "read", "([BII)I", "declared by SKVM"},
		{"java/util/Vector", "hashCode", "()I", "inherited from java/lang/Object"},
	} {
		if !shape.hasMethod(present.class, present.name, present.descriptor) {
			t.Errorf("%s.%s%s reported missing (%s)", present.class, present.name, present.descriptor, present.why)
		}
	}

	for _, absent := range []struct {
		class      string
		name       string
		descriptor string
	}{
		{"java/lang/String", "matches", "(Ljava/lang/String;)Z"},
		{"java/util/Vector", "stream", "()Ljava/util/stream/Stream;"},
		{"com/xce/io/XFile", "rename", "(Ljava/lang/String;)Z"},
	} {
		if shape.hasMethod(absent.class, absent.name, absent.descriptor) {
			t.Errorf("%s.%s%s reported present", absent.class, absent.name, absent.descriptor)
		}
	}

	if !shape.hasClass("java/lang/Throwable") {
		t.Error("java/lang/Throwable reported missing")
	}
	if shape.hasClass("java/util/ArrayList") {
		t.Error("java/util/ArrayList reported present")
	}
	if !shape.hasField("com/xce/lcdui/Toolkit", "FONT_HEIGHT", "I") {
		t.Error("Toolkit.FONT_HEIGHT reported missing")
	}
	if shape.hasField("com/xce/lcdui/Toolkit", "FONT_WIDTH", "I") {
		t.Error("Toolkit.FONT_WIDTH reported present")
	}
}

// A platform's own registrations are not on a bare VM, and the scan takes them
// from a diagnostics report instead. What matters is that a name only that
// report carries stops being reported as a gap.
func TestRegisteredNativesCountAsAnswered(t *testing.T) {
	shape := newSurface()
	const key = "com/example/Handset.vibrate(I)V"
	if shape.hasMethod("com/example/Handset", "vibrate", "(I)V") {
		t.Fatal("the example native is already answered")
	}
	shape.registered[key] = true
	if !shape.hasMethod("com/example/Handset", "vibrate", "(I)V") {
		t.Error("a registered native is still reported as a gap")
	}
	if !shape.hasClass("com/example/Handset") {
		t.Error("a class only a registration names is still reported as a gap")
	}
}

// A KTF client image has no constant pools to read — it is compiled — so the
// scan reads the name pool inside it, which holds the platform classes the
// title names beside a good deal that is not a class name at all. What this
// pins is the telling apart: an entry is kept because it names a class in a
// package this platform publishes, and dropped because it is a resource path,
// the title's own package, or a member entry whose descriptor happens to look
// like one.
func TestKTFNamePoolKeepsPlatformClassesAndDropsTheRest(t *testing.T) {
	pool := []string{
		// Named on its own, the way a class lookup or a `new` names one.
		"org/kwis/msp/lcdui/Image",
		// Named only inside a signature, which is the whole record of a class
		// a title never mentions in any other form.
		"(Lorg/kwis/msp/lcdui/Graphics;II)V+paintOne",
		"[Lorg/kwis/msp/lwc/DialogComponent;+dialogs",
		// A member entry: a descriptor joined to a name. The class in it is a
		// reference; the entry as a whole is not a class name.
		"Lorg/kwis/msp/media/Clip;+backgroundClip",
		// Not classes: a resource path a title opens by name, and its own
		// package, which is in the same pool and in no package of ours.
		"res/ui/menu_pal",
		"rpg/GameJlet",
	}
	var image []byte
	for _, entry := range pool {
		image = append(image, entry...)
		image = append(image, 0)
	}

	found := map[string]bool{}
	for _, name := range ktfPlatformNames(image) {
		found[name] = true
	}
	for _, wanted := range []string{
		"org/kwis/msp/lcdui/Image",
		"org/kwis/msp/lcdui/Graphics",
		"org/kwis/msp/lwc/DialogComponent",
		"org/kwis/msp/media/Clip",
	} {
		if !found[wanted] {
			t.Errorf("%s was not read out of the pool", wanted)
		}
	}
	for _, unwanted := range []string{
		"res/ui/menu_pal", "rpg/GameJlet",
		"org/kwis/msp/media/Clip;+backgroundClip",
		"org/kwis/msp/lwc/DialogComponent;+dialogs",
	} {
		if found[unwanted] {
			t.Errorf("%q was read as a class name", unwanted)
		}
	}
}

// The scan is only as good as what it believes the platform answers, and on
// KTF that is two tables rather than one: the classes the platform publishes
// itself, and the core library underneath them, which is what a title's
// `catch` on a runtime exception resolves through. A gap has to be a real
// hole, and the local corpus names two whole packages' worth of both kinds.
func TestKTFAnsweredCoversBothTablesItIsMadeOf(t *testing.T) {
	answered := ktfAnswered()
	for _, published := range []string{
		"org/kwis/msp/lcdui/Card",
		"org/kwis/msp/db/DataBase",
		"java/util/Timer",
	} {
		if !answered[published] {
			t.Errorf("%s is published to guest code and the scan calls it a gap", published)
		}
	}
	for _, core := range []string{
		"java/lang/Exception",
		"java/lang/NullPointerException",
		"java/io/IOException",
	} {
		if !answered[core] {
			t.Errorf("%s is a core library class and the scan calls it a gap", core)
		}
	}
	if answered["org/kwis/msp/lcdui/PluginJlet"] {
		t.Error("PluginJlet is answered now; the scan's report of it is stale")
	}
}
