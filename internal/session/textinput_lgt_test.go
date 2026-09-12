package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestLGTTextInputRoundTripThroughPlatformNeutralSession(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("..", "platform", "lgt", "testdata", "text-input.zip"))
	if err != nil {
		t.Fatal(err)
	}
	running, err := Start(t.Context(), archive, Options{
		DisableAuthentication: true,
		SaveStore:             backend.NewDirectorySaveStore(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(running.Close)

	input, err := running.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if input.Text != "" || input.MaxLength != 16 || input.Multiline || input.Password || input.InputMode != "text" {
		t.Fatalf("initial LGT text input = %+v", input)
	}
	const committed = "한글 이름 😀"
	if err := input.Commit(t.Context(), committed); err != nil {
		t.Fatal(err)
	}
	reopened, err := running.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Text != committed {
		t.Fatalf("reopened LGT text = %q, want %q", reopened.Text, committed)
	}
}
