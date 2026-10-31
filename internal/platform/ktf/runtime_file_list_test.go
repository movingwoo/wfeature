package ktf

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// What an earlier session saved is in the save store and nowhere else, so a
// listing that read only memory and the archive told a title with saved slots
// that its save directory was empty.
func TestFileSystemListIncludesEarlierSessionsFiles(t *testing.T) {
	saves := t.TempDir()
	_, first := newTestRuntime(t)
	first.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	for _, name := range []string{"save/one.dat", "save/two.dat"} {
		if err := first.storeGuestFile(name, []byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.markGuestFileRemoved("save/two.dat", true); err != nil {
		t.Fatal(err)
	}

	client, second := newTestRuntime(t)
	second.client.SaveStore = wipicrt.NewDirectorySaveStore(saves)
	if count := listedCount(t, client, second, "save"); count != 1 {
		t.Fatalf("list(save) in a new session = %d entries, want the one saved and not removed", count)
	}
	// A name that is no directory is the specification's IOException.
	if _, err := runtimeFileSystemList(second, client.JVM(), []jvm.Value{jvm.ReferenceValue(client.JVM().NewString("elsewhere"))}); !isGuestException(err, "java/io/IOException") {
		t.Fatalf("list(elsewhere) = %v, want IOException", err)
	}
}

func TestFileSystemTotalSpaceIsNotLessThanAvailable(t *testing.T) {
	var total, available *runtimeJavaMethod
	definition := runtimeFileSystemClassDefinition()
	for index := range definition.methods {
		method := &definition.methods[index]
		switch method.name + method.descriptor {
		case "totalSpace()I":
			total = method
		case "available()I":
			available = method
		}
	}
	if total == nil || available == nil {
		t.Fatal("FileSystem does not declare totalSpace()I and available()I")
	}
	_, runtime := newTestRuntime(t)
	totalValue, err := total.implementation(runtime, nil, []jvm.Value{})
	if err != nil {
		t.Fatal(err)
	}
	availableValue, err := available.implementation(runtime, nil, []jvm.Value{})
	if err != nil {
		t.Fatal(err)
	}
	totalBytes, _ := totalValue.Int32()
	availableBytes, _ := availableValue.Int32()
	if totalBytes <= 0 || availableBytes > totalBytes {
		t.Fatalf("totalSpace %d, available %d", totalBytes, availableBytes)
	}
}

// isGuestException reports whether err is a Java exception of the class.
func isGuestException(err error, class string) bool {
	var guest *jvm.GuestException
	return errors.As(err, &guest) && guest.Object != nil && guest.Object.ClassName == class
}
