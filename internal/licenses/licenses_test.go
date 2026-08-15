package licenses

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// go:embed cannot reach outside its own package, so the licence the binary
// carries is a copy of the one at the repository root. A copy that drifts is
// worse than no copy: the release would state terms the project no longer
// publishes. This is what stops it drifting.
func TestEmbeddedProjectLicenceMatchesTheRepositoryRoot(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if string(root) != Project {
		t.Error("internal/licenses/LICENSE has drifted from the root LICENSE; copy the root file over it")
	}
}

// Every bundled component that asks for its notice to be passed along has to
// appear here, because the release binary is all a user receives. Each entry
// is named by a string from its own licence text rather than by the component
// name, so dropping the text fails the test even if the heading survives.
func TestNoticesCarryEveryBundledComponent(t *testing.T) {
	for _, required := range []struct {
		component string
		text      string
	}{
		{"the Go project's BSD licence", "Neither the name of Google LLC nor the names of its"},
		{"the Go project's copyright", "The Go Authors"},
		{"the large face's copyright", "Eunbin Jeong"},
		{"the handset face's copyright", "Lee Minseo"},
		{"the fonts' licence", "SIL OPEN FONT LICENSE Version 1.1"},
		{"the fonts' reserved name clause", "Reserved Font Name"},
		{"hqx's copyright", "Christopher Serr"},
	} {
		if !strings.Contains(ThirdParty, required.text) {
			t.Errorf("the notices do not carry %s (looked for %q)", required.component, required.text)
		}
	}
}

// The font's licence is reproduced in the notices, so the copy beside the font
// and the copy in the binary have to say the same thing.
func TestNoticesReproduceTheFontLicenceInFull(t *testing.T) {
	font, err := os.ReadFile(filepath.Join("..", "..", "fonts", "LICENSE-neodgm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ThirdParty, strings.TrimSpace(string(font))) {
		t.Error("the notices no longer reproduce fonts/LICENSE-neodgm in full")
	}
}
