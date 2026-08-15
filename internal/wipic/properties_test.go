package wipic

import "testing"

// The subscriber number is the one property a host may change, and the two
// titles behind that pull in opposite directions — so what this pins is the
// contract the hosts rely on: the change reaches both names a title can ask
// for, and an answer no handset could give is refused rather than stored.
func TestSubscriberNumberIsSettableAndValidated(t *testing.T) {
	original := SubscriberNumber()
	t.Cleanup(func() {
		if err := SetSubscriberNumber(original); err != nil {
			t.Fatal(err)
		}
	})

	if original != defaultSubscriberNumber {
		t.Fatalf("the default number is %q, want %q", original, defaultSubscriberNumber)
	}

	if err := SetSubscriberNumber("9999"); err != nil {
		t.Fatalf("a short number was refused: %v", err)
	}
	// MIN is the same line as PHONENUMBER, and a title reads whichever of the
	// two it was written against.
	for _, name := range []string{"PHONENUMBER", "MIN"} {
		if got := SystemProperties[name]; got != "9999" {
			t.Fatalf("%s = %q after the change, want %q", name, got, "9999")
		}
	}
	if got := SubscriberNumber(); got != "9999" {
		t.Fatalf("SubscriberNumber() = %q", got)
	}

	for _, refused := range []struct {
		name   string
		number string
	}{
		// An empty number is what a title takes the last four digits off, and
		// then asks to copy minus four bytes.
		{"empty", ""},
		// Longer than the twelve-byte buffer one title reads it into.
		{"too long", "010000000000"},
		{"not digits", "010-1234"},
	} {
		if err := SetSubscriberNumber(refused.number); err == nil {
			t.Fatalf("%s number %q was accepted", refused.name, refused.number)
		}
		if got := SubscriberNumber(); got != "9999" {
			t.Fatalf("a refused number changed the answer to %q", got)
		}
	}
}
