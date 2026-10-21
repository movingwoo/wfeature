package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// TestNativeNetworkOpensASocketThatRefusesToConnect covers the network as a
// later title meets it. Opening a socket succeeds — one title loads its
// pictures only once it has one — and everything a socket is for is refused
// the way BREW refuses it with no network, so no title reaches one.
func TestNativeNetworkOpensASocketThatRefusesToConnect(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	network := nativeInterfaceSurface(nativeClassNetwork)
	open := servedSlot(t, platform, network, nativeNetworkOpenSocket)
	socket := nativeCall(t, open, 0, 0)
	if socket == 0 {
		t.Fatal("opening a socket answered null")
	}
	if again := nativeCall(t, open, 0, 1); again != socket {
		t.Errorf("a second socket is %#x, want the one object %#x", again, socket)
	}
	if want, err := platform.client.InterfaceObject(nativeClassSocket); err != nil || socket != want {
		t.Errorf("the socket is %#x, want the socket interface's object %#x (%v)", socket, want, err)
	}

	surface := nativeInterfaceSurface(nativeClassSocket)
	for _, offset := range []uint32{nativeSocketConnect, nativeSocketRead, nativeSocketWrite} {
		if got := nativeCall(t, servedSlot(t, platform, surface, offset), socket, 0, 0, 0); got != nativeNetworkFailed {
			t.Errorf("socket offset %#x answered %#x, want AEE_NET_ERROR", offset, got)
		}
	}
	if got := nativeCall(t, servedSlot(t, platform, surface, nativeSocketLastError), socket); got != nativeNetworkDown {
		t.Errorf("the socket's last error is %#x, want AEE_NET_ENETDOWN %#x", got, nativeNetworkDown)
	}

	// A lookup is never answered: it writes no result and calls nothing back.
	result := nativeString(t, platform, "untouched")
	nativeCall(t, servedSlot(t, platform, network, nativeNetworkLookUp), 0, result, nativeString(t, platform, "example.com"), 0)
	if got := string(nativeRead(t, platform, result, len("untouched"))); got != "untouched" {
		t.Errorf("the lookup wrote %q into its result", got)
	}
}

// TestNativeOpenSocketSurvivesAQuickLoad covers the socket as an interface
// object: a checkpoint taken after a title opened one restores it at the same
// address, still answering.
func TestNativeOpenSocketSurvivesAQuickLoad(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: NewManualClock(time.Time{}), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	socket := nativeCall(t, servedSlot(t, source.platform, nativeInterfaceSurface(nativeClassNetwork), nativeNetworkOpenSocket), 0, 0)
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNativeSessionCheckpoint(archive, saved, NativeSessionOptions{Clock: NewManualClock(time.Time{})})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored := prepared.session.platform
	if got, ok := restored.client.interfaces[nativeClassSocket]; !ok || got != socket {
		t.Fatalf("the restored socket is %#x (%v), want %#x", got, ok, socket)
	}
	if got := nativeCall(t, servedSlot(t, restored, nativeInterfaceSurface(nativeClassSocket), nativeSocketConnect), socket); got != nativeNetworkFailed {
		t.Errorf("the restored socket's connect answered %#x, want AEE_NET_ERROR", got)
	}
}
