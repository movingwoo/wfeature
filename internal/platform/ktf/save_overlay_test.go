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
