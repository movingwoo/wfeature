package ktf

import "testing"

// A class this platform has never published still resolves to a record, so a
// missing one is not a load failure — it is a method lookup that fails the
// first time a guest calls it, which can be an hour into a game. One card
// title ran through its whole opening and several battles before asking a
// java/util/Date for its instant and ending the session there. This walks the
// guest's path: resolve the class, then look the method up through it.
func TestDateResolvesTheInstantAGuestAsksFor(t *testing.T) {
	client, runtime := newTestRuntime(t)
	classAddress, err := runtime.ensureJavaClass("java/util/Date")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []struct{ name, descriptor string }{
		{"<init>", "()V"},
		{"<init>", "(J)V"},
		{"getTime", "()J"},
		{"setTime", "(J)V"},
	} {
		resolved, found, err := client.JVM().FindAOTMethod(classAddress, method.name, method.descriptor)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("Date.%s%s does not resolve from the guest's class record", method.name, method.descriptor)
		}
		if resolved.Body == 0 {
			t.Fatalf("Date.%s%s resolves to a null body", method.name, method.descriptor)
		}
	}
}
