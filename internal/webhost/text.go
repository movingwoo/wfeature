package webhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// textFile is where every sentence a player reads on the page is written: the
// page's own markup and scripts, and what this server answers the page with.
// It sits beside index.html in the client files, so a development server
// reads the copy in web/ and a release the one embedded in its binary. The
// format is described at the top of the file.
const textFile = "text.properties"

// pageFile is the page; its {{key}} placeholders are filled from textFile.
const pageFile = "index.html"

// textTable is textFile read: the sentence each key names.
type textTable map[string]string

var (
	textKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// textPlaceholder is a {name} in a sentence, filled when it is shown.
	textPlaceholder = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9]*)\}`)
	// pagePlaceholder is a {{key}} in the page. {{@table}} is the whole table,
	// which the page's scripts read; "@" is not a key's character, so no
	// sentence can be named that.
	pagePlaceholder = regexp.MustCompile(`\{\{(@table|[A-Za-z0-9][A-Za-z0-9._-]*)\}\}`)
)

// lineSpace is what is trimmed from a line and from each side of its `=`:
// spaces, tabs and the carriage return of a file saved with CRLF. Not every
// Unicode space, because web/text-fixture.mjs reads the file for the page's
// tests and the two readers must agree on which characters a value keeps.
const lineSpace = " \t\r"

// parseText reads textFile. It is strict because the file is edited by hand: a
// line that is neither a comment nor `key = value`, a key outside the
// characters keys are written in, and a key written twice are each refused
// with the line they are on, so that a mistake stops a server when it starts
// instead of reaching a player as half a page.
func parseText(data []byte) (textTable, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s is not UTF-8", textFile)
	}
	table := textTable{}
	source := strings.TrimPrefix(string(data), "\ufeff")
	for index, line := range strings.Split(source, "\n") {
		line = strings.Trim(line, lineSpace)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key, value = strings.Trim(key, lineSpace), strings.Trim(value, lineSpace)
		switch {
		case !found:
			return nil, fmt.Errorf("%s:%d: %q is neither a comment nor key = value", textFile, index+1, line)
		case !textKeyPattern.MatchString(key):
			return nil, fmt.Errorf("%s:%d: %q is not a key", textFile, index+1, key)
		}
		if _, seen := table[key]; seen {
			return nil, fmt.Errorf("%s:%d: %s is written twice", textFile, index+1, key)
		}
		table[key] = value
	}
	return table, nil
}

// phrase answers the sentence a key names with its placeholders filled.
// values are name and value pairs; a {name} they do not give stays as it is
// written, and a key the table does not have answers itself, which names what
// is missing instead of saying nothing.
func (table textTable) phrase(key string, values ...string) string {
	sentence, ok := table[key]
	if !ok {
		return key
	}
	if len(values) < 2 {
		return sentence
	}
	named := make(map[string]string, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		named[values[index]] = values[index+1]
	}
	return textPlaceholder.ReplaceAllStringFunc(sentence, func(whole string) string {
		if value, ok := named[whole[1:len(whole)-1]]; ok {
			return value
		}
		return whole
	})
}

// pageText is the table, read out of the client files. It reads the file each
// time it is asked and parses it again only when the bytes changed: a
// development server serves web/ from disk, where an edit should show on the
// next reload, and a release reads the same embedded bytes every time.
type pageText struct {
	files fs.FS
	mu    sync.Mutex
	raw   []byte
	table textTable
}

func (text *pageText) load() (textTable, error) {
	raw, err := fs.ReadFile(text.files, textFile)
	if err != nil {
		return nil, err
	}
	text.mu.Lock()
	defer text.mu.Unlock()
	if text.table != nil && bytes.Equal(raw, text.raw) {
		return text.table, nil
	}
	table, err := parseText(raw)
	if err != nil {
		return nil, err
	}
	text.raw, text.table = raw, table
	return table, nil
}

// renderPage fills a page's placeholders from the table: each {{key}} with its
// sentence, escaped for HTML, and {{@table}} with the table as JSON. A key the
// table does not have, and a "{{" that is not a placeholder at all, are
// refused rather than sent, because either would put the placeholder itself in
// front of a player.
func renderPage(page []byte, table textTable) ([]byte, error) {
	if found, placeholders := bytes.Count(page, []byte("{{")), len(pagePlaceholder.FindAllIndex(page, -1)); found != placeholders {
		return nil, fmt.Errorf("%s has %d {{ but %d placeholders", pageFile, found, placeholders)
	}
	// A map encodes in key order, so the same file is the same page and the
	// same validator. The encoder escapes <, > and &, which is what keeps a
	// sentence from closing the script element the table travels in.
	encoded, err := json.Marshal(map[string]string(table))
	if err != nil {
		return nil, err
	}
	var missing []string
	rendered := pagePlaceholder.ReplaceAllFunc(page, func(match []byte) []byte {
		name := string(match[2 : len(match)-2])
		if name == "@table" {
			return encoded
		}
		sentence, ok := table[name]
		if !ok {
			missing = append(missing, name)
			return match
		}
		return []byte(html.EscapeString(sentence))
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s names %s, which %s does not have", pageFile, strings.Join(missing, ", "), textFile)
	}
	return rendered, nil
}

// renderClientPage reads the page and the table out of the client files and
// fills one from the other.
func (s *Server) renderClientPage() ([]byte, error) {
	page, err := fs.ReadFile(s.client, pageFile)
	if err != nil {
		return nil, err
	}
	table, err := s.text.load()
	if err != nil {
		return nil, err
	}
	return renderPage(page, table)
}

// phrase is the sentence the page is shown for key, from textFile; see
// textTable.phrase for the values. A server whose file stopped reading — an
// edit in a development checkout — answers the key and says why in the log.
func (s *Server) phrase(key string, values ...string) string {
	if s == nil || s.text == nil {
		return key
	}
	table, err := s.text.load()
	if err != nil {
		s.logger.Warn("page text unavailable", "key", key, "error", err)
		return key
	}
	return table.phrase(key, values...)
}

// pageRefusal is an error written for the page: key names its sentence in
// textFile and values fill it. Its Error is the key, which is what a log line
// wants; explain is what turns it into the sentence.
type pageRefusal struct {
	key    string
	values []string
}

func (r *pageRefusal) Error() string { return r.key }

// refuse is a pageRefusal for key; see textTable.phrase for the values.
func refuse(key string, values ...string) error { return &pageRefusal{key: key, values: values} }

// explain is what the page is told for err: the sentence a refusal names, and
// any other error's own text, which no player was meant to read and which
// says what went wrong all the same.
func (s *Server) explain(err error) string {
	var refused *pageRefusal
	if errors.As(err, &refused) {
		return s.phrase(refused.key, refused.values...)
	}
	return err.Error()
}
