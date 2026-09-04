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
		// The Go modules carry one licence text between them, so what proves
		// a component is still named is its own heading. x/sys is here
		// because x/image's rasteriser reaches it on the amd64 targets: it
		// is nobody's import in this repository and is linked into three of
		// the five release archives all the same.
		{"the text module's notice", "## golang.org/x/text"},
		{"the image module's notice", "## golang.org/x/image"},
		{"the system module's notice", "## golang.org/x/sys"},
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

// The bundled components are listed in three places, and the release-facing
// one is the easiest to forget: the notices carry a summary table and a
// section per component, and the README has a table of its own for a reader
// who has not downloaded anything yet. `golang.org/x/sys` was in the notices
// and in the binary and missing from the README's table for as long as it had
// been linked, which nothing here could have failed on. This is what fails on
// the next one.
//
// Only the first column is compared. What each list says about a component is
// written for its own reader — the README's is Korean and says which targets
// link x/sys, the summary's is English — and a test that demanded the same
// words in both would be a test that stops either from being rewritten.
func TestEveryListOfBundledComponentsNamesTheSameOnes(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		what  string
		named []string
	}{
		{"the notices' summary table", componentsInTable(ThirdParty)},
		{"the README's component table", componentsInTable(string(readme))},
	} {
		compareComponents(t, pair.what, pair.named, componentSections(ThirdParty))
	}
}

// compareComponents reports each component one list has and the other does not,
// in both directions, so one run names every drift rather than the first.
func compareComponents(t *testing.T, what string, named, sections []string) {
	t.Helper()
	if len(named) == 0 {
		t.Fatalf("%s: no component table found; has its shape changed?", what)
	}
	held := map[string]bool{}
	for _, component := range sections {
		held[component] = true
	}
	listed := map[string]bool{}
	for _, component := range named {
		listed[component] = true
		if !held[component] {
			t.Errorf("%s names %q, which has no section in the notices", what, component)
		}
	}
	for _, component := range sections {
		if !listed[component] {
			t.Errorf("%s does not name %q, which the notices reproduce a licence for", what, component)
		}
	}
}

// componentSections is what the notices actually reproduce: one `## ` heading
// per component, plus the summary heading, which is not one.
func componentSections(notices string) []string {
	var components []string
	for _, line := range strings.Split(notices, "\n") {
		heading, ok := strings.CutPrefix(line, "## ")
		if !ok || strings.TrimSpace(heading) == "Summary" {
			continue
		}
		components = append(components, strings.TrimSpace(heading))
	}
	return components
}

// componentsInTable reads the first column of the one table in a document whose
// heading row is a component list, in either language. A cell may name more
// than one component — the two Go modules under the same terms share a row in
// the README — and may wrap each in backticks.
func componentsInTable(document string) []string {
	var components []string
	inside := false
	for _, line := range strings.Split(document, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			inside = false
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		first := strings.TrimSpace(cells[0])
		if first == "Component" || first == "구성 요소" {
			inside = true
			continue
		}
		if !inside || strings.HasPrefix(first, "---") {
			continue
		}
		for _, name := range strings.Split(first, ",") {
			name = strings.TrimSpace(strings.Trim(strings.TrimSpace(name), "`"))
			if name != "" {
				components = append(components, name)
			}
		}
	}
	return components
}
