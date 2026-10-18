package webhost

import (
	"encoding/json"
	"go/scanner"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

// textFormatCases are the cases both readers of text.properties answer to;
// web/text.test.mjs runs the same file against the page's reader.
type textFormatCases struct {
	Parsed []struct {
		Source string            `json:"source"`
		Table  map[string]string `json:"table"`
	} `json:"parsed"`
	Refused []string `json:"refused"`
	Phrased []struct {
		Sentence string            `json:"sentence"`
		Values   map[string]string `json:"values"`
		Want     string            `json:"want"`
	} `json:"phrased"`
}

// servedText is web/text.properties, the file every test server serves.
func servedText(t *testing.T) textTable {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "web", textFile))
	if err != nil {
		t.Fatal(err)
	}
	table, err := parseText(data)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// pageSentence is key's sentence as a test server answers it, so that a test
// of what the page is told does not repeat the file's wording.
func pageSentence(t *testing.T, key string, values ...string) string {
	t.Helper()
	table := servedText(t)
	if _, ok := table[key]; !ok {
		t.Fatalf("text.properties has no %s", key)
	}
	return table.phrase(key, values...)
}

// saysSentence reports whether message is key's sentence, whatever its
// placeholders were filled with.
func saysSentence(t *testing.T, key, message string) bool {
	t.Helper()
	sentence := pageSentence(t, key)
	parts := textPlaceholder.Split(sentence, -1)
	for index, part := range parts {
		parts[index] = regexp.QuoteMeta(part)
	}
	return regexp.MustCompile(`^` + strings.Join(parts, `[\s\S]*`) + `$`).MatchString(message)
}

func readTextFormatCases(t *testing.T) textFormatCases {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "web", "testdata", "text-format.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases textFormatCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Parsed) == 0 || len(cases.Refused) == 0 || len(cases.Phrased) == 0 {
		t.Fatal("text-format.json has an empty section")
	}
	return cases
}

func TestTextIsReadTheWayThePageTestsReadIt(t *testing.T) {
	cases := readTextFormatCases(t)
	for _, test := range cases.Parsed {
		table, err := parseText([]byte(test.Source))
		if err != nil {
			t.Errorf("parseText(%q): %v", test.Source, err)
			continue
		}
		if !reflect.DeepEqual(map[string]string(table), test.Table) {
			t.Errorf("parseText(%q) = %q, want %q", test.Source, table, test.Table)
		}
	}
	for _, source := range cases.Refused {
		if table, err := parseText([]byte(source)); err == nil {
			t.Errorf("parseText(%q) = %q, want a refusal", source, table)
		}
	}
	for _, test := range cases.Phrased {
		table := textTable{"sentence": test.Sentence}
		names := make([]string, 0, len(test.Values))
		for name := range test.Values {
			names = append(names, name)
		}
		sort.Strings(names)
		var values []string
		for _, name := range names {
			values = append(values, name, test.Values[name])
		}
		if got := table.phrase("sentence", values...); got != test.Want {
			t.Errorf("phrase(%q, %q) = %q, want %q", test.Sentence, values, got, test.Want)
		}
	}
}

func TestARefusalNamesTheLineItIsOn(t *testing.T) {
	_, err := parseText([]byte("# a comment\na = 1\nno separator here\n"))
	if err == nil || !strings.Contains(err.Error(), "text.properties:3:") {
		t.Fatalf("parseText error = %v, want one naming line 3", err)
	}
	if _, err := parseText([]byte("a = \xff\n")); err == nil {
		t.Fatal("parseText took a file that is not UTF-8")
	}
}

func TestAMissingSentenceIsShownAsItsKey(t *testing.T) {
	table := textTable{"present": "here"}
	if got := table.phrase("absent.key", "name", "value"); got != "absent.key" {
		t.Fatalf("phrase of a missing key = %q", got)
	}
	var server *Server
	if got := server.phrase("absent.key"); got != "absent.key" {
		t.Fatalf("phrase on a server without text = %q", got)
	}
}

func TestThePageIsFilledFromTheText(t *testing.T) {
	table, err := parseText([]byte("a.b = <b>&\"x\"</b>\nc = plain\n"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := renderPage([]byte(`<p title="{{a.b}}">{{c}}</p><script>{{@table}}</script>`), table)
	if err != nil {
		t.Fatal(err)
	}
	want := `<p title="&lt;b&gt;&amp;&#34;x&#34;&lt;/b&gt;">plain</p><script>{"a.b":"\u003cb\u003e\u0026\"x\"\u003c/b\u003e","c":"plain"}</script>`
	if string(page) != want {
		t.Fatalf("renderPage =\n%s\nwant\n%s", page, want)
	}
	for _, broken := range []string{"{{missing}}", "{{ c }}", "{{c}", "{{"} {
		if _, err := renderPage([]byte(broken), table); err == nil {
			t.Errorf("renderPage(%q) was sent", broken)
		}
	}
}

func TestTheServedPageCarriesItsWordsAndTheTable(t *testing.T) {
	server := newTestServer(t, Options{})
	table, err := server.text.load()
	if err != nil {
		t.Fatal(err)
	}
	recorder := get(t, server, "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "{{") {
		t.Fatal("the served page still has a placeholder in it")
	}
	if !strings.Contains(body, ">"+table["picker.start"]+"</button>") {
		t.Errorf("the start button does not say %q", table["picker.start"])
	}
	opening := `<script id="page-text" type="application/json">`
	start := strings.Index(body, opening)
	if start < 0 {
		t.Fatal("the served page has no table for its scripts")
	}
	rest := body[start+len(opening):]
	var served map[string]string
	if err := json.Unmarshal([]byte(rest[:strings.Index(rest, "</script>")]), &served); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(served, map[string]string(table)) {
		t.Error("the page's table is not text.properties")
	}
}

func TestAServerWithBrokenTextDoesNotStart(t *testing.T) {
	page := &fstest.MapFile{Data: []byte("<p>{{a}}</p>")}
	for name, files := range map[string]fstest.MapFS{
		"no text file":       {"index.html": page},
		"a broken line":      {"index.html": page, "text.properties": {Data: []byte("a = 1\nbroken\n")}},
		"a key it lacks":     {"index.html": page, "text.properties": {Data: []byte("b = 1\n")}},
		"a key twice":        {"index.html": page, "text.properties": {Data: []byte("a = 1\na = 2\n")}},
		"a stray brace pair": {"index.html": {Data: []byte("<p>{{a}} {{ a }}</p>")}, "text.properties": {Data: []byte("a = 1\n")}},
	} {
		if _, err := New(Options{Client: files}); err == nil {
			t.Errorf("%s: New started", name)
		}
	}
}

// A development server serves web/ from disk, and an edit to the text shows on
// the next request — in the page and in what the server answers it with.
func TestAnEditedTextShowsWithoutARestart(t *testing.T) {
	root := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<p>{{a}}</p>")
	write("text.properties", "a = before\n")
	server := newTestServer(t, Options{Client: os.DirFS(root)})
	write("text.properties", "a = after\n")
	if body := get(t, server, "/").Body.String(); body != "<p>after</p>" {
		t.Fatalf("page = %q", body)
	}
	if got := server.phrase("a"); got != "after" {
		t.Fatalf("phrase = %q", got)
	}
	// A broken edit shows as the key, and the page refuses rather than half
	// renders; the last good table is not served in its place.
	write("text.properties", "a = after\nbroken\n")
	if got := server.phrase("a"); got != "a" {
		t.Fatalf("phrase over a broken file = %q", got)
	}
	if code := get(t, server, "/").Code; code != http.StatusInternalServerError {
		t.Fatalf("page over a broken file: status %d", code)
	}
}

// goSources are the non-test Go files of a package directory.
func goSources(t *testing.T, directory string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		sources[filepath.Join(directory, name)] = data
	}
	return sources
}

// goStrings are the string literals in a Go file, unquoted.
func goStrings(t *testing.T, name string, data []byte) []string {
	t.Helper()
	files := token.NewFileSet()
	var lexer scanner.Scanner
	lexer.Init(files.AddFile(name, -1, len(data)), data, func(position token.Position, message string) {
		t.Fatalf("%s: %s", position, message)
	}, 0)
	var literals []string
	for {
		_, kind, literal := lexer.Scan()
		if kind == token.EOF {
			return literals
		}
		if kind != token.STRING {
			continue
		}
		value, err := strconv.Unquote(literal)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		literals = append(literals, value)
	}
}

// The server asks for its sentences by key, and a key with no sentence would
// put the key in front of a player. Every literal in this package that is
// spelled like a key in one of the file's sections must be one of its keys.
func TestEverySentenceTheServerAsksForIsWritten(t *testing.T) {
	table := servedText(t)
	sections := map[string]bool{}
	for key := range table {
		sections[strings.SplitN(key, ".", 2)[0]] = true
	}
	keyShaped := regexp.MustCompile(`^([a-z][a-z-]*)\.[A-Za-z0-9._-]+$`)
	asked := 0
	for name, data := range goSources(t, ".") {
		for _, literal := range goStrings(t, name, data) {
			match := keyShaped.FindStringSubmatch(literal)
			if match == nil || !sections[match[1]] {
				continue
			}
			asked++
			if _, ok := table[literal]; !ok {
				t.Errorf("%s asks for %q, which text.properties does not have", name, literal)
			}
		}
	}
	if asked == 0 {
		t.Fatal("found no key in the server's sources; the scan is not reading them")
	}
}

// Every sentence a player reads lives in web/text.properties. A Korean string
// written into the server — or into the cheat engine, whose refusals the panel
// shows — is one the file does not have, and the next person changing the
// page's wording would not find it. Comments are not looked at; a platform
// package's guest text is not the page's and is not either.
func TestNoPlayerTextIsWrittenIntoTheServer(t *testing.T) {
	for _, directory := range []string{".", filepath.Join("..", "cheat")} {
		for name, data := range goSources(t, directory) {
			for _, literal := range goStrings(t, name, data) {
				for _, character := range literal {
					if unicode.Is(unicode.Hangul, character) {
						t.Errorf("%s writes %q; it belongs in web/text.properties", name, literal)
						break
					}
				}
			}
		}
	}
}
