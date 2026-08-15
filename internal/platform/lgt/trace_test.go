package lgt

import (
	"context"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func tracingClient(t *testing.T, size int) *Client {
	t.Helper()
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8, TraceSVC: size})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// driveSVC routes one call through the dispatcher rather than a slot handler,
// because the dispatcher is where the trace is taken.
func driveSVC(t *testing.T, client *Client, category, slot uint32, arguments ...uint32) error {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range arguments {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(12, slot); err != nil {
		t.Fatal(err)
	}
	return client.handleSupervisorCall(context.Background(), thread,
		armcore.SupervisorCall{Immediate: category})
}

// A fault lands in the game's own code, hundreds of instructions past the
// platform call that caused it. The trace is what connects the two.
func TestSVCTraceRecordsArgumentsAndResults(t *testing.T) {
	client := tracingClient(t, 8)

	if err := driveSVC(t, client, svcCategoryWIPIC, slotFramebufferBpp); err != nil {
		t.Fatal(err)
	}
	calls := client.SVCTrace()
	if len(calls) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(calls))
	}
	call := calls[0]
	if call.Slot != slotFramebufferBpp || call.Name != "framebufferBpp" {
		t.Errorf("recorded slot %#x named %q", call.Slot, call.Name)
	}
	if call.Failed {
		t.Errorf("recorded a failure for a slot that succeeded: %s", call.Error)
	}
	if call.Result != 16 {
		t.Errorf("recorded result %d, want the LCD depth", call.Result)
	}
}

// A slot that fails is the one a trace is read for, so its entry has to be
// filed rather than dropped along with the error.
func TestSVCTraceRecordsAFailedSlot(t *testing.T) {
	client := tracingClient(t, 8)

	if err := driveSVC(t, client, svcCategoryOEM, 0xfffe); err == nil {
		t.Fatal("an unimplemented OEM slot was accepted")
	}
	calls := client.SVCTrace()
	if len(calls) != 1 {
		t.Fatalf("recorded %d calls, want the failed one", len(calls))
	}
	if !calls[0].Failed || calls[0].Error == "" {
		t.Errorf("the failed call was recorded as a success: %+v", calls[0])
	}
	// A slot with no name is the finding, not a gap in the dump.
	if got := calls[0].String(); !strings.Contains(got, "unnamed") {
		t.Errorf("an unnamed slot rendered as %q", got)
	}
}

// The interesting run is millions of instructions long, so the trace keeps
// only its tail and has to keep the newest rather than the first.
func TestSVCTraceKeepsTheMostRecentCalls(t *testing.T) {
	client := tracingClient(t, 3)

	for count := 0; count < 5; count++ {
		if err := driveSVC(t, client, svcCategoryWIPIC, slotFramebufferBpp); err != nil {
			t.Fatal(err)
		}
	}
	calls := client.SVCTrace()
	if len(calls) != 3 {
		t.Fatalf("kept %d calls, want the ring's size", len(calls))
	}
	// Ordering is what makes the dump readable: oldest first, so the last line
	// is the call the fault followed.
	dump := FormatSVCTrace(calls)
	if lines := strings.Count(strings.TrimSpace(dump), "\n") + 1; lines != 3 {
		t.Errorf("the dump has %d lines for 3 calls", lines)
	}
}

// A client asked for no trace pays nothing and reports nothing.
func TestSVCTraceIsOffByDefault(t *testing.T) {
	client := fixtureClient(t)

	if err := driveSVC(t, client, svcCategoryWIPIC, slotFramebufferBpp); err != nil {
		t.Fatal(err)
	}
	if calls := client.SVCTrace(); calls != nil {
		t.Errorf("an untraced client recorded %d calls", len(calls))
	}
}

// The slots worth tracing take names, and a name is what the trace has to
// show. `fsOpen(0x400fff40, 0x1)` names nothing; the whole finding behind the
// removal list was two lines that had to read `fsRemove("Save0.dat")` and
// `fsIsExist("Save0.dat")` to be a finding at all.
func TestSVCTraceNamesTheArgumentsThatAreNames(t *testing.T) {
	client := tracingClient(t, 8)

	name, err := client.allocateBytes(append([]byte("Save0.dat"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := driveSVC(t, client, svcCategoryWIPIC, slotFsOpen, name, fileOpenReadOnly); err != nil {
		t.Fatal(err)
	}
	if err := driveSVC(t, client, svcCategoryWIPIC, slotFsRemove, name); err != nil {
		t.Fatal(err)
	}
	dump := FormatSVCTrace(client.SVCTrace())
	for _, want := range []string{`fsOpen("Save0.dat", read`, `fsRemove("Save0.dat"`} {
		if !strings.Contains(dump, want) {
			t.Errorf("the dump does not contain %q:\n%s", want, dump)
		}
	}
	// The raw registers stay, because an address is what a fault reports.
	if !strings.Contains(dump, "|") {
		t.Errorf("the dump dropped the raw registers:\n%s", dump)
	}
}

// A slot whose arguments are numbers is not read as memory. A title asks for
// the framebuffer width tens of thousands of times a second, and a trace that
// read guest memory on every call would change what it is measuring.
func TestSVCTraceLeavesNumericSlotsAlone(t *testing.T) {
	client := tracingClient(t, 8)

	if err := driveSVC(t, client, svcCategoryWIPIC, slotFramebufferWidth, 0x12345678); err != nil {
		t.Fatal(err)
	}
	calls := client.SVCTrace()
	if len(calls) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(calls))
	}
	if calls[0].Detail != "" {
		t.Fatalf("a numeric slot was given the detail %q", calls[0].Detail)
	}
}

// The live stream answers the question the ring cannot: the call that explains
// a wrong screen is a hundred thousand calls back by the time anything looks
// wrong, so it has to be written down when it happens.
func TestLiveTraceStreamsMatchingCallsAsTheyHappen(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	var stream strings.Builder
	client, err := Load(archive, Options{Width: 16, Height: 8, TraceLive: "fsRemove", TraceOut: &stream})
	if err != nil {
		t.Fatal(err)
	}

	name, err := client.allocateBytes(append([]byte("Save0.dat"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := driveSVC(t, client, svcCategoryWIPIC, slotFsIsExist, name); err != nil {
		t.Fatal(err)
	}
	if err := driveSVC(t, client, svcCategoryWIPIC, slotFsRemove, name); err != nil {
		t.Fatal(err)
	}
	written := stream.String()
	if !strings.Contains(written, `fsRemove("Save0.dat"`) {
		t.Errorf("the matching call was not streamed:\n%s", written)
	}
	if strings.Contains(written, "fsIsExist") {
		t.Errorf("a call the filter excludes was streamed:\n%s", written)
	}
}
