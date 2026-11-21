package lgt

import "testing"

// The setting is "1" only for a client whose module was selected, and every
// other property keeps the answer every title gets.
func TestDataServiceLockAnswersOnlyTheSelectedModule(t *testing.T) {
	client := fixtureClient(t)
	if client.dataServiceLock {
		t.Fatal("the fixture module was selected")
	}
	if hasDataServiceLock([]byte("unrecognized module")) {
		t.Fatal("an unknown revision was selected")
	}
	if value, ok := client.systemProperty(dataServiceLockProperty); !ok || value != "0" {
		t.Fatalf("unselected module: DS_LOCK = %q, %v", value, ok)
	}
	client.dataServiceLock = true
	if value, ok := client.systemProperty(dataServiceLockProperty); !ok || value != "1" {
		t.Fatalf("selected module: DS_LOCK = %q, %v", value, ok)
	}
	for _, name := range []string{"ROAMING_AREA", "AIRPLANE_MODE"} {
		if value, ok := client.systemProperty(name); !ok || value != "0" {
			t.Fatalf("selected module: %s = %q, %v", name, value, ok)
		}
	}
}

// A module the registry selects has to hold the code that was examined.
func TestDataServiceLockRejectsUnexpectedCode(t *testing.T) {
	for digest, reader := range dataServiceLockReaders {
		if len(digest) != 64 || len(reader.expect) == 0 || reader.at == 0 {
			t.Fatalf("reader for %s is incomplete", digest)
		}
	}
	client := fixtureClient(t)
	if err := client.selectDataServiceLock(); err != nil || client.dataServiceLock {
		t.Fatalf("unselected module: err=%v selected=%v", err, client.dataServiceLock)
	}
}
