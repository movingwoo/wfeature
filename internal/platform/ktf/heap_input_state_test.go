package ktf

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

func TestHeapInputKeepsFocusedEditorAndNativeSharing(t *testing.T) {
	client, source := newTestRuntime(t)
	now := time.Unix(1700000000, 0)
	clock := NewManualClock(now)
	client.clock = clock
	field := &jvm.Object{ClassName: jvm.ObjectClass, Fields: map[string]jvm.Value{componentTextField: jvm.ReferenceValue(client.vm.NewString(""))}}
	client.FocusTextComponent(field)
	if !client.TypeKey('2') {
		t.Fatal("focused editor did not consume key")
	}
	field.Native = client.textEditor
	other := &jvm.Object{ClassName: jvm.ObjectClass, Native: client.textEditor}
	saved, err := source.captureHeapState([]*jvm.Object{field, other})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var parsed runtimeHeapState
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	freshClient, fresh := newTestRuntime(t)
	freshClock := NewManualClock(time.Unix(1900000000, 0))
	freshClient.clock = freshClock
	roots, err := fresh.restoreHeapState(parsed)
	if err != nil {
		t.Fatal(err)
	}
	editor, ok := roots[0].Native.(*textinput.State)
	if !ok || editor != freshClient.textEditor || roots[1].Native != editor || editor == client.textEditor || freshClient.focusedText != roots[0] {
		t.Fatal("focused editor or native sharing changed")
	}
	if !freshClient.TypeKey('2') || componentText(roots[0]) != "b" || componentText(field) != "a" {
		t.Fatal("restored multi-tap did not continue independently")
	}
	if _, err := fresh.captureHeapState(roots); err != nil {
		t.Fatalf("input recapture: %v", err)
	}
}
