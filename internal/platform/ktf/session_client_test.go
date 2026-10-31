package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestDetachedSessionClientAttachesArchiveWithoutStartup(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{Runnable: true})
	source.start(t)
	source.client.SaveStore = newCertificateSaveStore(memorySaveStore{}, []byte("changed at capture"))
	saved := captureClientForTest(t, source.client)
	adapters, _, err := captureSaveAdapters(source.client.SaveStore)
	if err != nil {
		t.Fatal(err)
	}
	source.client.StopThreads()
	archive := &Archive{Descriptor: Descriptor{AID: "ABCDEF12", Properties: map[string]string{"APPNAME": "Checkpoint fixture"}},
		JAR: &JAR{Client: source.client.image, Entries: map[string][]byte{"asset.bin": {1, 2, 3}}}}
	clock := wipicrt.NewManualClock(time.Unix(1800000000, 0))
	debugCalls := 0
	client, err := restoreSessionClient(archive, saved, adapters, SessionOptions{MaxSteps: saved.Core.MaxSteps, Clock: clock,
		SaveStore: make(memorySaveStore), Debug: func(core *armcore.Core) {
			debugCalls++
			if core.Steps() != saved.Core.Steps {
				t.Fatal("debug hook observed a temporary core or guest startup")
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.StopThreads)
	if debugCalls != 1 || client.appProperties["APPNAME"] != "Checkpoint fixture" || len(client.Resources["asset.bin"]) != 3 || client.ProgramName != ProgramNameForAID(archive.Descriptor.AID) {
		t.Fatal("restored session attachments differ")
	}
	certificate, _ := client.SaveStore.LoadSave(certificateSaveKey)
	if string(certificate) != "changed at capture" {
		t.Fatal("restoration regenerated the certificate")
	}
	clock.Advance(30 * time.Millisecond)
	if ran, err := client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("detached session continuation = %d, %v", ran, err)
	}
}

func TestDetachedSessionClientRestoresOlderModuleWithoutEntry(t *testing.T) {
	source := loadSyntheticModule(t)
	source.Clock = wipicrt.NewManualClock(time.Unix(1700000000, 0))
	if _, err := source.ExecuteModuleEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	class := writeModuleClass(t, source.runtime, "saved/Module", 0, "tick", "()V", 5)
	source.runtime.moduleClassByName = map[string]uint32{"saved/Module": class}
	if err := source.runtime.linkModuleClasses(); err != nil {
		t.Fatal(err)
	}
	saved := roundTripClientState(t, captureClientForTest(t, source))
	archive := &Archive{JAR: &JAR{Client: source.image}}
	restored, err := restoreSessionClient(archive, saved, saveAdapterState{Version: saveAdapterVersion}, SessionOptions{MaxSteps: saved.Core.MaxSteps, Clock: wipicrt.NewManualClock(time.Unix(1900000000, 0))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.StopThreads)
	if !restored.IsModule() || !restored.initializationStarted || restored.prepared != restored.runtime || restored.argument != source.argument || restored.Thread.Context() != source.Thread.Context() {
		t.Fatal("older module initialization or thread context differs")
	}
	if _, err := restored.ExecuteModuleEntry(t.Context()); err == nil {
		t.Fatal("restored module allowed entry to run twice")
	}
	if got, err := restored.runtime.resolveModuleClass("saved/Module", 0); err != nil || got != class || restored.Core().Steps() != saved.Core.Steps {
		t.Fatalf("restored module lookup changed execution: %#x, %v", got, err)
	}
}
