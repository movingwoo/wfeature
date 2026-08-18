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
