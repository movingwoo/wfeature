package webhost

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A read over HTTP never lands inside a batch: the listing and the export hold
// the directory's transaction lock for the whole walk, so each sees the two
// keys of one batch or of the next, never one of each. A leftover of an earlier
// build that cannot be settled is an error, not an empty save tree.
func TestSaveHTTPReadsNeverSplitABatch(t *testing.T) {
	root, archive := checkpointServerFiles(t)
	server := checkpointServer(t, root)
	directory := server.saveDirectory("ktf", "P0001")
	store := backend.NewDirectorySaveStore(directory)
	batch := func(label string) map[string][]byte {
		return map[string][]byte{"index": []byte(label), "progress/current": []byte("payload " + label)}
	}
	if err := store.StoreSaves(batch("initial")); err != nil {
		t.Fatal(err)
	}
	if ok, reason := server.claimSaveDirectory(directory, "fixture"); !ok {
		t.Fatal(reason)
	}
	defer server.releaseSaveDirectory(directory)
	host := httptest.NewServer(server)
	defer host.Close()
	done := make(chan error, 2)
	for _, endpoint := range []string{"/api/saves/ktf/P0001", "/api/savepack?game=games/ktf/checkpoint.zip"} {
		go func() {
			for round := 0; round < 16; round++ {
				response, err := host.Client().Get(host.URL + endpoint)
				if err != nil {
					done <- err
					return
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != http.StatusOK {
					done <- fmt.Errorf("save read status=%d: %v: %s", response.StatusCode, err, body)
					return
				}
				values := map[string]string{}
				if strings.HasPrefix(endpoint, "/api/savepack") {
					pack, decodeErr := backend.DecodeSavePack(body)
					if decodeErr != nil || pack.Identity != backend.SaveIdentity(archive) {
						done <- fmt.Errorf("export identity or format: %v", decodeErr)
						return
					}
					for _, entry := range pack.Entries {
						values[entry.Key] = string(entry.Data)
					}
				} else {
					var listing saveResponse
					if err := json.Unmarshal(body, &listing); err != nil {
						done <- err
						return
					}
					for key, encoded := range listing.Saves {
						data, err := base64.StdEncoding.DecodeString(encoded)
						if err != nil {
							done <- err
							return
						}
						values[key] = string(data)
					}
				}
				label := values["index"]
				if len(values) != 2 || label == "" || values["progress/current"] != "payload "+label {
					done <- fmt.Errorf("HTTP read split a batch: %+v", values)
					return
				}
			}
			done <- nil
		}()
	}
	for round := 0; round < 16; round++ {
		if err := store.StoreSaves(batch(fmt.Sprintf("round%d", round))); err != nil {
			t.Error(err)
			break
		}
	}
	for reader := 0; reader < 2; reader++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	// A recovery error must not be presented as a successful empty save tree.
	intent := filepath.Join(root, "saves", "ktf", ".wfeature-quicksave", "owners", "P0001", "intent")
	if err := os.WriteFile(intent, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/saves/ktf/P0001", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unreadable tree reported as success: %d: %s", recorder.Code, recorder.Body.String())
	}
}
