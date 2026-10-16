package webhost

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// listSaves answers the save API's listing of one owner, decoded.
func listSaves(t *testing.T, server *Server, target string) map[string]string {
	t.Helper()
	recorder := get(t, server, target)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", target, recorder.Code, recorder.Body.String())
	}
	var response saveResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	saves := make(map[string]string, len(response.Saves))
	for key, encoded := range response.Saves {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		saves[key] = string(data)
	}
	return saves
}

func wantListing(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the listing is %q, want %q", got, want)
	}
	for key, data := range want {
		if got[key] != data {
			t.Fatalf("the listing has %s = %q, want %q (all: %q)", key, got[key], data, got)
		}
	}
}

// The save API takes and lists keys, never the names on disk. Two keys that
// differ only by case are two saves on every host, and a key with an
// upper-case letter or a '#' is spelled out only in the file name.
func TestSaveAPIKeepsKeysThatDifferOnlyByCaseApart(t *testing.T) {
	saveRoot := filepath.Join(t.TempDir(), "ktf")
	server := newTestServer(t, Options{SaveRoot: saveRoot})
	for _, write := range []struct{ target, body string }{
		{"/api/saves/P0001/rms/Save", "upper"},
		{"/api/saves/P0001/rms/save", "lower"},
		{"/api/saves/P0001/rms/save%231", "with a hash"},
	} {
		if code := put(t, server, write.target, []byte(write.body)); code != http.StatusNoContent {
			t.Fatalf("PUT %s = %d", write.target, code)
		}
	}
	wantListing(t, listSaves(t, server, "/api/saves/P0001"), map[string]string{
		"rms/Save": "upper", "rms/save": "lower", "rms/save#1": "with a hash",
	})
	for path, want := range map[string]string{"rms/#Save": "upper", "rms/save": "lower", "rms/save##1": "with a hash"} {
		if data, err := os.ReadFile(filepath.Join(saveRoot, "P0001", filepath.FromSlash(path))); err != nil || string(data) != want {
			t.Fatalf("%s on disk = %q, %v; want %q", path, data, err, want)
		}
	}
}

// A save folder an earlier build wrote, each key its own path, lists under
// the keys it was written with, and is converted on the way.
func TestSaveAPIListsAnEarlierFolderUnderItsKeys(t *testing.T) {
	saveRoot := filepath.Join(t.TempDir(), "ktf")
	earlier := map[string]string{"rms/Option": "settings", "fs/Data/Slot1.sav": "nested", "db/scores": "lower case"}
	for key, data := range earlier {
		path := filepath.Join(saveRoot, "P0002", filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := newTestServer(t, Options{SaveRoot: saveRoot})
	wantListing(t, listSaves(t, server, "/api/saves/P0002"), earlier)
	for _, path := range []string{"rms/#Option", "fs/#Data/#Slot1.sav", "db/scores"} {
		if _, err := os.Stat(filepath.Join(saveRoot, "P0002", filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s is not where the conversion puts it: %v", path, err)
		}
	}
	// A write under a key that differs from one of these only by case is a
	// save of its own.
	if code := put(t, server, "/api/saves/P0002/rms/option", []byte("another store")); code != http.StatusNoContent {
		t.Fatalf("PUT = %d", code)
	}
	earlier["rms/option"] = "another store"
	wantListing(t, listSaves(t, server, "/api/saves/P0002"), earlier)
}
