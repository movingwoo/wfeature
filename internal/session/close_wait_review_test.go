package session

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// Closing an LGT session whose destroyClet never returns ends in closeWait:
// the call is cut off there and the close returns. The call used to run under
// a context that never ended, so such a title held a stopping server, and an
// app reopened behind it, for ever.
func TestClosingAnLGTSessionWhoseDestroyNeverReturnsEnds(t *testing.T) {
	wait := closeWait
	closeWait = 200 * time.Millisecond
	t.Cleanup(func() { closeWait = wait })
	archive, err := testfixture.LGTEndlessDestroyArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "fs/progress", Data: []byte("saved")}})
	s, err := Start(t.Context(), archive, Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	lgtCheckpointTicks(t, s, 2)
	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("closing the session still waits on a destroyClet that never returns")
	}
	if data, found, err := backend.ReadSave(store, "fs/progress"); err != nil || !found || string(data) != "saved" {
		t.Fatalf("after the close the save holds %q (found=%t, err=%v)", data, found, err)
	}
}
