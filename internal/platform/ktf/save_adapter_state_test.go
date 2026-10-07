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

// unreadableStore fails the read of one key, the way a damaged disk does.
type unreadableStore struct {
	checkpointStoreProbe
	key string
}

func (store *unreadableStore) ReadSave(name string) ([]byte, bool, error) {
	store.reads++
	if name == store.key {
		return nil, false, errors.New("fixture read failure")
	}
	data, present := store.memorySaveStore.LoadSave(name)
	return data, present, nil
}

// A checkpoint carries what the certificate adapter made for the run — the
// certificate, and whether the title removed it — and nothing it read from the
// store. Binding it reads the removal list of the store it is bound over,
// once, and writes nothing; the view the title is given and the bit a later
// write leaves alone both follow that store's own list.
func TestSaveAdapterBindFollowsTheLiveRemovalList(t *testing.T) {
	for _, test := range []struct {
		name string
		// guest is the removal list the title wrote in the captured run, and
		// live the list of the store the checkpoint is bound over.
		guest, live       []string
		removedInRun      bool
		view, afterAWrite []string
	}{
		{"the title kept its certificate", []string{"later"}, []string{"unrelated"}, false,
			[]string{"unrelated"}, []string{"next"}},
		{"the store's own list names the certificate", []string{"later"}, []string{certificateName, "unrelated"}, false,
			[]string{"unrelated"}, []string{"next", certificateName}},
		{"the title removed its certificate", []string{certificateName, "later"}, []string{"unrelated"}, true,
			[]string{"unrelated", certificateName}, []string{"next"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceBase := memorySaveStore{databaseRemovedKey: joinRemovalList([]string{certificateName, "old"})}
			source := newCertificateSaveStore(sourceBase, []byte("initial certificate"))
			if err := source.StoreSaves(map[string][]byte{certificateSaveKey: []byte("changed certificate"), databaseRemovedKey: joinRemovalList(test.guest)}); err != nil {
				t.Fatal(err)
			}
			saved, base, err := captureSaveAdapters(source)
			if err != nil || base == nil || len(saved.Layers) != 1 || saved.Layers[0].CertificateRemoved != test.removedInRun {
				t.Fatalf("capture adapters: %+v, %v", saved.Layers, err)
			}
			data, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			// The record names no file the store holds: the lists stay behind.
			for _, name := range []string{"later", "old"} {
				if bytes.Contains(data, []byte(name)) {
					t.Fatalf("the adapter record carries the removal list: %s", data)
				}
			}
			var decoded saveAdapterState
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			destination := &checkpointStoreProbe{memorySaveStore: memorySaveStore{databaseRemovedKey: joinRemovalList(test.live)}}
			restored, err := bindSaveAdapters(decoded, destination)
			if err != nil || destination.reads != 1 || destination.writes != 0 {
				t.Fatalf("binding made %d reads and %d writes: %v", destination.reads, destination.writes, err)
			}
			certificate, present := restored.LoadSave(certificateSaveKey)
			removed, _ := restored.LoadSave(databaseRemovedKey)
			if !present || string(certificate) != "changed certificate" || !bytes.Equal(removed, joinRemovalList(test.view)) {
				t.Fatalf("the bound adapter answers certificate %q and list %q, want the record's certificate and %q", certificate, removed, joinRemovalList(test.view))
			}
			if err := restored.StoreSave(databaseRemovedKey, joinRemovalList([]string{"next"})); err != nil {
				t.Fatal(err)
			}
			persisted, _ := destination.memorySaveStore.LoadSave(databaseRemovedKey)
			if !bytes.Equal(persisted, joinRemovalList(test.afterAWrite)) {
				t.Fatalf("a later write of the list stored %q, want %q", persisted, joinRemovalList(test.afterAWrite))
			}
			if err := restored.StoreSave(certificateSaveKey, nil); err != nil {
				t.Fatal(err)
			}
			original, _ := source.LoadSave(certificateSaveKey)
			if string(original) != "changed certificate" || string(decoded.Layers[0].Certificate) != "changed certificate" {
				t.Fatal("the bound adapter shares mutable source bytes")
			}
		})
	}
}

// A removal list that cannot be read refuses the bind, and nothing is written.
func TestSaveAdapterBindRefusesAFailedRead(t *testing.T) {
	saved := saveAdapterState{Version: saveAdapterVersion, Layers: []saveAdapterLayer{{Kind: "certificate", Certificate: []byte("certificate")}}}
	destination := &unreadableStore{key: databaseRemovedKey, checkpointStoreProbe: checkpointStoreProbe{memorySaveStore: memorySaveStore{}}}
	if _, err := bindSaveAdapters(saved, destination); err == nil || destination.writes != 0 {
		t.Fatalf("a bind over an unreadable removal list = %v after %d writes", err, destination.writes)
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
	restored, err := bindSaveAdapters(saved, destination)
	if err != nil || destination.reads != 0 || destination.writes != 0 {
		t.Fatalf("bind subscriber adapter: %v", err)
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
		{Version: 1, Layers: []saveAdapterLayer{{Kind: "certificate"}}},
		{Version: saveAdapterVersion, Layers: []saveAdapterLayer{{Kind: "unknown"}}},
		{Version: saveAdapterVersion, Layers: make([]saveAdapterLayer, 4)},
		{Version: saveAdapterVersion, Layers: []saveAdapterLayer{{Kind: "certificate", Table: []byte{1}}}},
		{Version: saveAdapterVersion, Layers: []saveAdapterLayer{{Kind: "subscriber-receipt", Number: "01024681357", Table: make([]byte, 256)}}},
		{Version: saveAdapterVersion, Layers: []saveAdapterLayer{{Kind: "subscriber-receipt", Number: "01024681357", Table: make([]byte, 256), CertificateRemoved: true}}},
	} {
		base := &checkpointStoreProbe{memorySaveStore: make(memorySaveStore)}
		if _, err := bindSaveAdapters(saved, base); err == nil || base.reads != 0 || base.writes != 0 {
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
	// Moving a chain onto another store walks it too, and has to come back.
	if rebaseSaveAdapters(cycle, memorySaveStore{}) != SaveStore(cycle) {
		t.Fatal("a cyclic adapter chain was not answered as its own top")
	}
}

// A chain moved onto another store reads and writes that store from then on,
// and a store with no adapter is replaced by the other one.
func TestSaveAdaptersMoveOntoAnotherStore(t *testing.T) {
	first, second := memorySaveStore{"db/slot": []byte("first")}, memorySaveStore{"db/slot": []byte("second")}
	if rebaseSaveAdapters(first, second).(memorySaveStore)["db/slot"] == nil || string(rebaseSaveAdapters(nil, second).(memorySaveStore)["db/slot"]) != "second" {
		t.Fatal("a store with no adapter was not replaced")
	}
	chain := newCertificateSaveStore(first, []byte("certificate"))
	top := rebaseSaveAdapters(chain, second)
	if top != SaveStore(chain) {
		t.Fatal("the chain's top changed")
	}
	if data, _ := top.LoadSave("db/slot"); string(data) != "second" {
		t.Fatalf("the moved chain reads %q", data)
	}
	if err := top.StoreSave("db/slot", []byte("written")); err != nil {
		t.Fatal(err)
	}
	if string(first["db/slot"]) != "first" || string(second["db/slot"]) != "written" {
		t.Fatalf("the moved chain wrote to %q and %q", first["db/slot"], second["db/slot"])
	}
	if data, _ := top.LoadSave(certificateSaveKey); string(data) != "certificate" {
		t.Fatal("the moved chain lost the run's certificate")
	}
}
