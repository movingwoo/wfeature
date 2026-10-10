package ktf

import (
	"encoding/binary"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func ktfListenerMediaRecord(t *testing.T, saved *sessionCheckpointState) (*jvm.HeapPayloadState, heapMediaState) {
	t.Helper()
	for index := range saved.Client.Heap.JVM.Payloads {
		payload := &saved.Client.Heap.JVM.Payloads[index]
		if payload.ExternalKind == heapMediaKind {
			var media heapMediaState
			if err := backend.DecodeCheckpointRecord(payload.Data, &media); err != nil {
				t.Fatal(err)
			}
			return payload, media
		}
	}
	t.Fatal("checkpoint has no media carrier")
	return nil, heapMediaState{}
}

func TestKTFWIPIListenerCheckpointRejectsMalformedMediaWithoutChangingSource(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, true)
	if err := fixture.store.StoreSave("progress", []byte("current progress")); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := fixture.session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	audioBefore, err := fixture.client.audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore := slices.Clone(fixture.runtime.mediaEvents)
	word := func(address uint32) uint32 {
		return binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, address, 4))
	}
	listenerClass, _ := fixture.client.JVM().AOTClass(testfixture.KTFListenerClass)
	interfaceName := word(word(word(word(listenerClass.Address+8)+16)) + 8)
	interfaceName = word(interfaceName)
	for _, test := range []struct {
		name   string
		mutate func(*sessionCheckpointState, *jvm.HeapPayloadState, *heapMediaState)
	}{
		{"unknown event", func(_ *sessionCheckpointState, _ *jvm.HeapPayloadState, media *heapMediaState) { media.Events[0] = 0 }},
		{"unsupported recording event", func(_ *sessionCheckpointState, _ *jvm.HeapPayloadState, media *heapMediaState) { media.Events[0] = 6 }},
		{"missing recipient", func(_ *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			payload.References = payload.References[:2]
		}},
		{"null recipient", func(_ *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			payload.References[2] = 0
		}},
		{"recipient is a Clip", func(_ *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			payload.References[2] = payload.References[0]
		}},
		{"unknown event owner", func(_ *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			payload.References[1] = payload.References[2]
		}},
		{"different handle", func(_ *sessionCheckpointState, _ *jvm.HeapPayloadState, media *heapMediaState) {
			media.Clips[0].Handle++
		}},
		{"future completion", func(_ *sessionCheckpointState, _ *jvm.HeapPayloadState, media *heapMediaState) {
			media.Clips[0].Completed++
		}},
		{"unreconciled completion", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			saved.Client.Audio.Sounds[0].Completed++
		}},
		{"unreconciled completion overflow", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			saved.Client.Audio.Sounds[0].Completed += maxClipEvents + 1
		}},
		{"stopped unreconciled completion", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, media *heapMediaState) {
			saved.Client.Audio.Sounds[0].Completed++
			saved.Client.Audio.Sounds[0].Playing = false
			media.Clips[0].Active = false
		}},
		{"missing active root", func(_ *sessionCheckpointState, _ *jvm.HeapPayloadState, media *heapMediaState) {
			media.Clips[0].Active = false
		}},
		{"duplicate handle", func(_ *sessionCheckpointState, payload *jvm.HeapPayloadState, media *heapMediaState) {
			media.Clips = append(media.Clips, media.Clips[0])
			payload.References = slices.Insert(payload.References, 0, payload.References[0])
		}},
		{"too many events", func(_ *sessionCheckpointState, payload *jvm.HeapPayloadState, media *heapMediaState) {
			for len(media.Events) <= maxClipEvents {
				media.Events = append(media.Events, 2)
				payload.References = append(payload.References, payload.References[1:3]...)
			}
		}},
		{"non-Clip owner", func(saved *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			heap := &saved.Client.Heap
			heap.JVM.Roots = append(heap.JVM.Roots, payload.References[2])
			heap.Roots.Clips[0].Owner = uint32(len(heap.JVM.Roots))
			payload.References[0], payload.References[1] = payload.References[2], payload.References[2]
		}},
		{"non-reference listener field", func(saved *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			fields := saved.Client.Heap.JVM.Objects[payload.References[0]-1].Fields
			for index := range fields {
				if fields[index].Name == clipListenerField {
					fields[index].Value = jvm.HeapValueState{Kind: jvm.ValueInt, Bits: 1}
				}
			}
		}},
		{"wrong current listener", func(saved *sessionCheckpointState, payload *jvm.HeapPayloadState, _ *heapMediaState) {
			fields := saved.Client.Heap.JVM.Objects[payload.References[0]-1].Fields
			for index := range fields {
				if fields[index].Name == clipListenerField {
					fields[index].Value.Reference = payload.References[0]
				}
			}
		}},
		{"null carrier root", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			for _, binding := range saved.Client.Heap.Roots.Objects {
				if string(binding.Name) == heapMediaRoot {
					saved.Client.Heap.JVM.Roots[binding.Root-1] = 0
				}
			}
		}},
		{"wrong carrier class", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			for _, binding := range saved.Client.Heap.Roots.Objects {
				if string(binding.Name) == heapMediaRoot {
					id := saved.Client.Heap.JVM.Roots[binding.Root-1]
					saved.Client.Heap.JVM.Objects[id-1].Class = testfixture.KTFListenerClass
				}
			}
		}},
		{"unrooted carrier", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			for index := range saved.Client.Heap.Roots.Objects {
				if string(saved.Client.Heap.Roots.Objects[index].Name) == heapMediaRoot {
					saved.Client.Heap.Roots.Objects[index].Name = []byte("unrelated")
				}
			}
		}},
		{"static callback", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			for _, class := range saved.Client.Heap.JVM.Classes {
				if class.Name == testfixture.KTFListenerClass {
					for index := range class.Methods {
						if class.Methods[index].Name == "playUpdate" {
							class.Methods[index].AccessFlags |= jvm.AccessStatic
						}
					}
				}
			}
		}},
		{"unmapped callback", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			for _, class := range saved.Client.Heap.JVM.Classes {
				if class.Name == testfixture.KTFListenerClass {
					for index := range class.Methods {
						if class.Methods[index].Name == "playUpdate" {
							class.Methods[index].Body = 0xfffffff1
						}
					}
				}
			}
		}},
		{"guest interface differs from cached summary", func(saved *sessionCheckpointState, _ *jvm.HeapPayloadState, _ *heapMediaState) {
			for _, page := range saved.Client.Core.Memory.Pages {
				if interfaceName >= page.Address && uint64(interfaceName-page.Address) < uint64(len(page.Data)) {
					copy(page.Data[interfaceName-page.Address:], []byte("fixture/UnrelatedListener\x00"))
					return
				}
			}
			t.Fatal("guest interface name is not in captured memory")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var saved sessionCheckpointState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
				t.Fatal(err)
			}
			payload, media := ktfListenerMediaRecord(t, &saved)
			test.mutate(&saved, payload, &media)
			payload.Data, err = backend.EncodeCheckpointRecord(media)
			if err != nil {
				t.Fatal(err)
			}
			corrupt := checkpoint
			corrupt.Runtime, err = backend.EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			options := fixture.session.options
			options.Clock, options.AudioSink = NewManualClock(time.Unix(1900000000, 0)), &audioPauseProbe{}
			prepared, err := PrepareSessionCheckpoint(fixture.archive, corrupt, options)
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("malformed media checkpoint was accepted")
			}
			audioAfter, err := fixture.client.audio.CaptureState()
			if err != nil || !reflect.DeepEqual(audioBefore, audioAfter) || !slices.Equal(eventsBefore, fixture.runtime.mediaEvents) {
				t.Fatalf("refused checkpoint changed source playback: %v", err)
			}
			if data, ok := fixture.store.LoadSave("progress"); !ok || string(data) != "current progress" {
				t.Fatal("refused checkpoint changed current progress")
			}
			fixture.wantHistory()
		})
	}
	fixture.drain()
	fixture.advance(fixture.duration())
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1))
}

func TestKTFWIPIListenerRuntimeInterfaceDeclaration(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	address, err := fixture.runtime.ensureJavaClass(runtimePlayListenerClass)
	if err != nil {
		t.Fatal(err)
	}
	method, found, err := fixture.client.JVM().FindAOTMethod(address, "playUpdate", clipUpdateSignature)
	if err != nil || !found || method.AccessFlags&jvm.AccessAbstract == 0 || method.AccessFlags&jvm.AccessStatic != 0 {
		t.Fatalf("PlayListener callback declaration = %+v, found=%v, err=%v", method, found, err)
	}
	for name, value := range map[string]int32{"ERROR": -1, "END_OF_DATA": 1, "START": 2, "STOP": 3, "PAUSE": 4, "RESUME": 5, "RECORD": 6, "FULL_OF_DATA": 7} {
		field, found, err := fixture.client.JVM().FindAOTField(address, name, "I")
		if err != nil || !found || field.AccessFlags&0x0019 != 0x0019 {
			t.Fatalf("PlayListener.%s field = %+v, found=%v, err=%v", name, field, found, err)
		}
		if got := int32(binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, field.Address+12, 4))); got != value {
			t.Fatalf("PlayListener.%s = %d, want %d", name, got, value)
		}
	}
}
