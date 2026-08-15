package lgt

import "testing"

// The component block hands out no components. A title that asks for one has
// to be told so: a handle answered for a widget this platform never draws
// leaves it waiting for text from something invisible.
func TestComponentBlockRefusesToCreateAComponent(t *testing.T) {
	client := fixtureClient(t)
	name := writeGuest(t, client, append([]byte("TextComponent"), 0))

	if got := callSlot(t, client, slotUicCreateApplicationContext); got == 0 {
		t.Fatal("MC_uicCreateApplicationContext answered no context at all")
	}
	if got := int32(callSlot(t, client, slotUicGetClass, name)); got != wipiError {
		t.Fatalf("MC_uicGetClass = %d, want %d", got, wipiError)
	}
	if got := int32(callSlot(t, client, slotUicCreate, uicApplicationContext, 0)); got != wipiError {
		t.Fatalf("MC_uicCreate = %d, want %d", got, wipiError)
	}
	// The rest of the block is accepted rather than fatal, so a title that
	// configures the component it was refused still runs.
	if got := int32(callSlot(t, client, slotUicLast)); got != wipiSuccess {
		t.Fatalf("the block's last slot = %d, want %d", got, wipiSuccess)
	}
}
