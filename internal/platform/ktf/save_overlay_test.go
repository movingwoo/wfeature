package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestSavedFileDoesNotBlockPackagedDescendant(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/state", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	for _, restart := range []string{"first", "restart"} {
		t.Run(restart, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			client.saveStore = store
			client.AttachFilesystem(map[string][]byte{"state/scene/asset": []byte("resource")})
			for _, test := range []struct {
				name string
				want int32
			}{
				{"/state/scene/asset", 1},
				{"/state/scene/missing", 0},
			} {
				value, err := runtimeFileSystemExists(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(client.JVM().NewString(test.name))})
				if err != nil {
					t.Fatal(err)
				}
				if got, _ := value.Int32(); got != test.want || runtime.saveReadError != nil {
					t.Fatalf("exists(%s) = %d, save error = %v", test.name, got, runtime.saveReadError)
				}
			}
			if data, exists := runtime.guestFile("/state/scene/asset"); !exists || string(data) != "resource" {
				t.Fatalf("packaged resource = %q, %t", data, exists)
			}
			if err := runtime.storeGuestFile("state", []byte("updated")); err != nil {
				t.Fatalf("save after resource lookup: %v", err)
			}
			if data, exists := runtime.guestFile("/state"); !exists || string(data) != "updated" {
				t.Fatalf("saved file = %q, %t", data, exists)
			}
		})
	}
}

// A title that keeps its saves in a folder and asks whether the folder exists
// is told the same thing on every run. On the first run the folder is not a
// save; on the next the store holds files below it, and reading its name used
// to end the session with a read error.
func TestAFolderOfSavesAnswersAsOnTheFirstRun(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	for _, run := range []string{"first", "second", "third"} {
		t.Run(run, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			client.saveStore = store
			exists := func(name string) int32 {
				t.Helper()
				value, err := runtimeFileSystemExists(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(client.JVM().NewString(name))})
				if err != nil {
					t.Fatal(err)
				}
				got, _ := value.Int32()
				return got
			}
			if got := exists("/saves"); got != 0 || runtime.saveReadError != nil {
				t.Fatalf("exists(/saves) = %d, save error = %v", got, runtime.saveReadError)
			}
			if run == "first" {
				if err := runtime.storeGuestFile("saves/slot", []byte("progress")); err != nil {
					t.Fatal(err)
				}
			}
			if got := exists("/saves/slot"); got != 1 || runtime.saveReadError != nil {
				t.Fatalf("exists(/saves/slot) = %d, save error = %v", got, runtime.saveReadError)
			}
		})
	}
}
