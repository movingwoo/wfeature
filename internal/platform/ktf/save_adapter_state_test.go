package ktf

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

type checkpointStoreProbe struct {
	memorySaveStore
	reads, writes int
}

func (store *checkpointStoreProbe) LoadSave(name string) ([]byte, bool) {
	store.reads++
	return store.memorySaveStore.LoadSave(name)
}
func (store *checkpointStoreProbe) StoreSave(name string, data []byte) error {
	store.writes++
	return store.memorySaveStore.StoreSave(name, data)
}

func TestSaveAdapterCheckpointPreservesMutableCertificate(t *testing.T) {
	sourceBase := memorySaveStore{databaseRemovedKey: joinRemovalList([]string{certificateName, "old"})}
	source := newCertificateSaveStore(sourceBase, []byte("initial certificate"))
	if err := source.StoreSaves(map[string][]byte{certificateSaveKey: []byte("changed certificate"), databaseRemovedKey: joinRemovalList([]string{"later"})}); err != nil {
		t.Fatal(err)
	}
	saved, base, err := captureSaveAdapters(source)
	if err != nil || base == nil || len(saved.Layers) != 1 {
		t.Fatalf("capture adapters: %v", err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded saveAdapterState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	destination := &checkpointStoreProbe{memorySaveStore: memorySaveStore{databaseRemovedKey: []byte("unrelated\n")}}
	restored, err := restoreSaveAdapters(decoded, destination)
	if err != nil || destination.reads != 0 || destination.writes != 0 {
		t.Fatalf("restoration touched the base store: %v", err)
	}
	certificate, present := restored.LoadSave(certificateSaveKey)
	removed, _ := restored.LoadSave(databaseRemovedKey)
	if !present || string(certificate) != "changed certificate" || !bytes.Equal(removed, joinRemovalList([]string{"later"})) {
		t.Fatal("restored run-local certificate state differs")
	}
	if err := restored.StoreSave(databaseRemovedKey, []byte("next\n")); err != nil {
		t.Fatal(err)
	}
	persisted, _ := destination.LoadSave(databaseRemovedKey)
	if !bytes.Equal(persisted, joinRemovalList([]string{"next", certificateName})) {
		t.Fatal("original certificate deletion bit was lost")
	}
	if err := restored.StoreSave(certificateSaveKey, nil); err != nil {
		t.Fatal(err)
	}
	original, _ := source.LoadSave(certificateSaveKey)
	if string(original) != "changed certificate" || string(decoded.Layers[0].Certificate) != "changed certificate" {
		t.Fatal("restored adapter shares mutable source bytes")
	}
}

func TestSaveAdapterCheckpointPreservesSubscriberRecovery(t *testing.T) {
	base, table, files, _ := receiptFixture(t)
	source := newSubscriberReceiptStore(base, "01024681357", table, files)
	saved, unwrapped, err := captureSaveAdapters(source)
	if err != nil || unwrapped != base {
		t.Fatalf("capture subscriber adapter: %v", err)
	}
	original, _ := base.LoadSave("db/prefs")
	destination := &checkpointStoreProbe{memorySaveStore: memorySaveStore{"db/prefs": bytes.Clone(original)}}
	restored, err := restoreSaveAdapters(saved, destination)
	if err != nil || destination.reads != 0 || destination.writes != 0 {
		t.Fatalf("restore subscriber adapter: %v", err)
	}
	want, _, err := backend.ReadSave(source, "db/prefs")
	if err != nil {
		t.Fatal(err)
	}
	got, present, err := backend.ReadSave(restored, "db/prefs")
	if err != nil || !present || !bytes.Equal(got, want) {
		t.Fatalf("restored recovery differs: %v", err)
	}
	if destination.writes != 0 || !bytes.Equal(destination.memorySaveStore["db/prefs"], original) {
		t.Fatal("restored recovery wrote during a read")
	}
	copy := restored.(*subscriberReceiptStore)
	copy.table[0]++
	copy.files[subscriberReceiptFiles[0]][0]++
	if bytes.Equal(copy.table, saved.Layers[0].Table) || bytes.Equal(copy.files[subscriberReceiptFiles[0]], saved.Layers[0].Files[0]) {
		t.Fatal("restored recovery shares checkpoint data")
	}
}

func TestSaveAdapterCheckpointRejectsMalformedRecordsWithoutIO(t *testing.T) {
	for _, saved := range []saveAdapterState{
		{},
		{Version: 1, Layers: []saveAdapterLayer{{Kind: "unknown"}}},
		{Version: 1, Layers: make([]saveAdapterLayer, 4)},
		{Version: 1, Layers: []saveAdapterLayer{{Kind: "certificate", Table: []byte{1}}}},
		{Version: 1, Layers: []saveAdapterLayer{{Kind: "subscriber-receipt", Number: "01024681357", Table: make([]byte, 256)}}},
	} {
		base := &checkpointStoreProbe{memorySaveStore: make(memorySaveStore)}
		if _, err := restoreSaveAdapters(saved, base); err == nil || base.reads != 0 || base.writes != 0 {
			t.Fatal("invalid adapter was accepted or touched its base store")
		}
	}
	broken := &certificateSaveStore{readError: errors.New("fixture read failure")}
	if _, _, err := captureSaveAdapters(broken); err == nil {
		t.Fatal("captured failed certificate read")
	}
	cycle := &certificateSaveStore{}
	cycle.base = cycle
	if _, _, err := captureSaveAdapters(cycle); err == nil {
		t.Fatal("captured cyclic adapter chain")
	}
}
