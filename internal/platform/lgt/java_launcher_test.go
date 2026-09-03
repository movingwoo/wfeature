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
