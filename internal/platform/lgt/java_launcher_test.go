package lgt

import "testing"

// The application object a title asks the platform for is the one the launcher
// built. A Jlet reaches for itself from code that was handed nothing — a Card's
// paint, a listener — and both spellings of the question are the same question
// on a handset running one program.
func TestJavaCurrentJletAnswersTheLaunchedApplication(t *testing.T) {
	client := fixtureClient(t)
	for _, key := range []string{
		"org/kwis/msp/lcdui/Jlet.getCurrentJlet()Lorg/kwis/msp/lcdui/Jlet;",
		"org/kwis/msp/lcdui/Jlet.getActiveJlet()Lorg/kwis/msp/lcdui/Jlet;",
	} {
		method, registered := javaPlatformMethods[key]
		if !registered {
			t.Fatalf("%s is not registered", key)
		}
		// Before the launcher has built one there is no application object,
		// and null is what says so.
		before, err := method.Implementat(client, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s before the launcher ran: %v", key, err)
		}
		if before != 0 {
			t.Errorf("%s answered %#x before the launcher ran, want null", key, before)
		}
	}
	const object = 0x30001234
	client.javaRuntimeState().jlet = object
	for _, key := range []string{
		"org/kwis/msp/lcdui/Jlet.getCurrentJlet()Lorg/kwis/msp/lcdui/Jlet;",
		"org/kwis/msp/lcdui/Jlet.getActiveJlet()Lorg/kwis/msp/lcdui/Jlet;",
	} {
		got, err := javaPlatformMethods[key].Implementat(client, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if got != object {
			t.Errorf("%s answered %#x, want %#x", key, got, uint32(object))
		}
	}
}

// getAppProperty answers out of app_info, the descriptor beside the JAR, the
// way KTF answers out of its descriptor: any case, the key trimmed, and null
// for a key the descriptor does not carry or a key that is no string.
func TestJavaJletAppPropertyAnswersFromTheDescriptor(t *testing.T) {
	const getAppProperty = "org/kwis/msp/lcdui/Jlet.getAppProperty(Ljava/lang/String;)Ljava/lang/String;"
	client := fixtureClient(t)
	descriptor, err := ParseDescriptor([]byte("AID=54585449\nPID=PF000001\nMClass=Main\nName=Fixture Title\nVendor: Someone\n"))
	if err != nil {
		t.Fatal(err)
	}
	client.archive.Descriptor = descriptor
	const jlet = 0x30001234
	for key, want := range map[string]string{
		"Name": "Fixture Title", "NAME": "Fixture Title", " name ": "Fixture Title",
		"MClass": "Main", "PID": "PF000001", "Vendor": "Someone",
	} {
		answer := mustJavaCall(t, client, getAppProperty, jlet, newTestString(t, client, key))
		text, ok := client.javaText(answer)
		if !ok || text != want {
			t.Errorf("getAppProperty(%q) = %q (a String: %v), want %q", key, text, ok, want)
		}
	}
	for _, key := range []uint32{newTestString(t, client, "Missing"), newTestString(t, client, ""), 0} {
		if got := mustJavaCall(t, client, getAppProperty, jlet, key); got != 0 {
			t.Errorf("getAppProperty(%#x) = %#x, want null", key, got)
		}
	}
}

// The exact spelling of a key wins, and among other spellings the first in
// byte order does, so a descriptor that carries two of them answers the same
// one every time.
func TestDescriptorPropertyPrefersTheExactSpelling(t *testing.T) {
	descriptor := Descriptor{Fields: map[string]string{"Key": "mixed", "KEY": "upper", "key": "lower"}}
	for name, want := range map[string]string{"Key": "mixed", "KEY": "upper", "key": "lower", "kEy": "upper"} {
		if got, found := descriptor.Property(name); !found || got != want {
			t.Errorf("Property(%q) = %q, %v, want %q", name, got, found, want)
		}
	}
	if _, found := (Descriptor{}).Property("Key"); found {
		t.Error("a descriptor with no fields answered a key")
	}
}
