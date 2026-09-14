package mobile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mobileSource(t *testing.T, relative string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no test source path")
	}
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(filename), relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

// Web storage is scoped to an origin, including its port. Both app launchers
// must therefore return to the same loopback port after a cold restart.
func TestNativeAppsKeepOneWebStorageOrigin(t *testing.T) {
	android := mobileSource(t, "android/src/com/movingwoo/wfeature/MainActivity.java")
	if !strings.Contains(android, "private static final int SERVER_PORT = 11541;") ||
		!strings.Contains(android, "int port = SERVER_PORT;") {
		t.Error("the Android app does not use the stable native-app port")
	}
	if strings.Contains(android, "new ServerSocket(0)") {
		t.Error("the Android app still chooses a new origin on every launch")
	}

	ios := mobileSource(t, "ios/lib/lib.go")
	if !strings.Contains(ios, "const serverPort = 11541") ||
		!strings.Contains(ios, "Port: serverPort") {
		t.Error("the iOS app does not use the same stable native-app port")
	}
}
