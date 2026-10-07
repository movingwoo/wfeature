package skt

import (
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestJavaCheckpointRejectsMalformedScreenContent(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	source.AttachSaveStore(store)
	list := &jvm.Object{ClassName: midp.ListClass}
	source.displayableState(list).screen = &screenData{kind: screenList, choice: &choiceData{kind: choiceMultiple, elements: []choiceElement{{text: "row"}}}}
	form := &jvm.Object{ClassName: midp.FormClass}
	item := &jvm.Object{ClassName: midp.ChoiceGroupClass, Native: &itemData{kind: itemChoice, owner: form, choice: &choiceData{kind: choiceMultiple, elements: []choiceElement{{text: "option"}}}}}
	source.displayableState(form).screen = &screenData{kind: screenForm, items: []*jvm.Object{item}}
	checkpoint, err := source.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(canvasJAR, checkpoint, Options{DisableAuthentication: true})
	if err != nil {
		t.Fatalf("valid screen graph was rejected: %v", err)
	}
	prepared.Discard()
	tests := []struct {
		name   string
		kind   screenKind
		mutate func(*checkpointScreen, *jvm.HeapPayloadState, *javaCheckpointState)
	}{
		{"negative list selection", screenList, func(s *checkpointScreen, _ *jvm.HeapPayloadState, _ *javaCheckpointState) { s.Selection = -1 }},
		{"list selection past end", screenList, func(s *checkpointScreen, _ *jvm.HeapPayloadState, _ *javaCheckpointState) { s.Selection = 1 }},
		{"missing list choice", screenList, func(_ *checkpointScreen, p *jvm.HeapPayloadState, _ *javaCheckpointState) { p.References[0] = 0 }},
		{"negative form selection", screenForm, func(s *checkpointScreen, _ *jvm.HeapPayloadState, _ *javaCheckpointState) { s.Selection = -1 }},
		{"form selection past end", screenForm, func(s *checkpointScreen, _ *jvm.HeapPayloadState, _ *javaCheckpointState) { s.Selection = 1 }},
		{"negative form sub-selection", screenForm, func(s *checkpointScreen, _ *jvm.HeapPayloadState, _ *javaCheckpointState) { s.SubSelection = -1 }},
		{"excessive form sub-selection", screenForm, func(s *checkpointScreen, _ *jvm.HeapPayloadState, _ *javaCheckpointState) {
			s.SubSelection = checkpointListLimit + 1
		}},
		{"null form item", screenForm, func(_ *checkpointScreen, p *jvm.HeapPayloadState, _ *javaCheckpointState) { p.References[6] = 0 }},
		{"non-item form reference", screenForm, func(_ *checkpointScreen, p *jvm.HeapPayloadState, s *javaCheckpointState) {
			p.References[6] = s.Threads.Heap.Roots[s.Platform.MIDlet-1]
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var saved javaCheckpointState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
				t.Fatal(err)
			}
			changed := false
			for i := range saved.Threads.Heap.Payloads {
				payload := &saved.Threads.Heap.Payloads[i]
				if payload.ExternalKind != "skt.screen" {
					continue
				}
				var screen checkpointScreen
				if err := backend.DecodeCheckpointRecord(payload.Data, &screen); err != nil {
					t.Fatal(err)
				}
				if screen.Kind != test.kind {
					continue
				}
				test.mutate(&screen, payload, &saved)
				payload.Data, err = backend.EncodeCheckpointRecord(screen)
				if err != nil {
					t.Fatal(err)
				}
				changed = true
				break
			}
			if !changed {
				t.Fatal("checkpoint has no matching screen payload")
			}
			broken := checkpoint
			broken.Runtime, err = backend.EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareJavaCheckpoint(canvasJAR, broken, Options{DisableAuthentication: true})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("malformed screen graph was accepted")
			}
			if source.State() != StateActive || source.displayableState(form).screen.items[0] != item {
				t.Fatal("failed preparation changed the source runtime")
			}
		})
	}
	if err := source.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatalf("source did not remain usable after rejected preparations: %v", err)
	}
}

func TestJavaCheckpointSerializesInputWithGuestAndPadState(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	source.AttachSaveStore(store)
	if err := source.SendKey(KeyPressed, KeyCodeLeft); err != nil {
		t.Fatal(err)
	}
	beforePad, err := source.pad.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents := invokeFixtureInt(t, source, "CanvasMIDlet", "keyEvents")
	parked, release, err := source.parkCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		done <- source.SendKey(KeyPressed, KeyCodeRight)
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("input passed a held checkpoint barrier: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if code, held := source.HeldKey(); !held || code != KeyCodeLeft {
		t.Fatalf("input mutated the pad before crossing the barrier: %d, %t", code, held)
	}
	saved, _, err := source.captureJavaState(parked)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Platform.Pad, beforePad) {
		t.Fatalf("captured pad includes an undelivered key: %#v", saved.Platform.Pad)
	}
	release()
	release = nil
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input did not resume after the checkpoint barrier")
	}
	if code, held := source.HeldKey(); !held || code != KeyCodeRight || invokeFixtureInt(t, source, "CanvasMIDlet", "lastKeyCode") != KeyCodeRight {
		t.Fatal("resumed input did not update the guest and pad together")
	}
	encoded, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := backend.Checkpoint{Identity: source.Archive.identity, Variant: backend.CheckpointSKTJava, Runtime: encoded}
	prepared, err := PrepareJavaCheckpoint(canvasJAR, checkpoint, Options{DisableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if code, held := prepared.runtime.HeldKey(); !held || code != KeyCodeLeft ||
		invokeFixtureInt(t, prepared.runtime, "CanvasMIDlet", "lastKeyCode") != KeyCodeLeft ||
		invokeFixtureInt(t, prepared.runtime, "CanvasMIDlet", "keyEvents") != beforeEvents {
		t.Fatal("restored guest callbacks and keypad describe different input boundaries")
	}
}

func TestJavaCheckpointRejectsAuthenticationPolicyChanges(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			store, err := backend.NewMemorySaveStore(nil)
			if err != nil {
				t.Fatal(err)
			}
			source := startLicenseFixture(t, licenseFixture(t), enabled, store)
			checkpoint, err := source.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareJavaCheckpoint(licenseJAR, checkpoint, Options{DisableAuthentication: !enabled})
			if err != nil {
				t.Fatalf("matching authentication policy was rejected: %v", err)
			}
			if prepared.runtime.Authentication() != source.Authentication() {
				prepared.Discard()
				t.Fatal("matching restoration changed authentication status")
			}
			prepared.Discard()
			prepared, err = PrepareJavaCheckpoint(licenseJAR, checkpoint, Options{DisableAuthentication: enabled})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("restoration accepted bytecode from a different authentication policy")
			}
			want := int32(0)
			if enabled {
				want = 1
			}
			if source.State() != StateActive || licenseInt(t, source, "accepted", "()Z") != want {
				t.Fatal("rejected restoration changed the source authentication policy")
			}
		})
	}
}
