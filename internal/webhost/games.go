package webhost

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/movingwoo/wfeature/internal/gameroot"
)

// gameExtensions are the archive shapes the engine loads. A KTF or LGT game is
// a zip and a MIDlet is a jar; which platform a file belongs to is the
// engine's answer from the bytes, so the server only decides what is worth
// listing.
var gameExtensions = map[string]bool{".zip": true, ".jar": true}

// The two roots a game can come from, and the prefix each one wears in the
// paths `games.json` hands out. A game either sits in the library beside the
// server under `games/<group>/`, or it arrived through the page's add button
// and sits in the added root under `ext/`.
//
// **The split is what makes removal answerable.** An archive the page wrote
// and one a person dropped into the library are the same bytes in the same
// shape, so a page that deleted "whatever sits loose in the root" would be
// deleting somebody's own library on the strength of a guess. There is no
// registry to consult instead — this listing is a directory read, every time
// it is asked for, which is what keeps it from ever disagreeing with the disk
// — so the answer has to be somewhere a directory read can find it, and where
// the file is is the one place that qualifies.
const (
	libraryPrefix = "games"
	addedPrefix   = "ext"
)

// Game is one entry in the picker.
type Game struct {
	// Group is the platform directory the archive sits in, and is empty for an
	// archive dropped straight into a root.
	Group string `json:"group"`
	// Name is the archive's file name without its extension.
	Name string `json:"name"`
	// Path is the URL the page loads the archive from.
	Path string `json:"path"`
	// Added reports that the archive came in through the page rather than
	// being put beside the server by hand, which is also what makes it the
	// page's to remove.
	Added bool `json:"added,omitempty"`
}

// ListGames describes both roots as the picker consumes them: one entry per
// archive, grouped by the platform directory holding it, then the archives
// sitting in the root itself, and the games the page added last. A missing
// root is an empty list rather than an error — a fresh install has no games
// yet, and the page has to load anyway so the user can see where to put them.
func ListGames(gameRoot, addedRoot string) []Game {
	games := listRoot(gameRoot, libraryPrefix, false)
	// A host with no added root, and one pointed at the library itself, both
	// list once: a second pass over the same directory would offer every game
	// twice, and offer the library as the page's to delete.
	if addedRoot != "" && addedRoot != gameRoot {
		games = append(games, listRoot(addedRoot, addedPrefix, true)...)
	}
	return games
}

// listRoot is one root's worth of that listing, under the prefix its paths
// carry.
func listRoot(gameRoot, prefix string, added bool) []Game {
	// The depth this reads to is the boundary every tool that reasons about
	// the library shares; gameroot.Entries holds it, one group at a time and
	// with the ungrouped archives last.
	// Titles are Korean, so ordering them by byte would put them in an order
	// no reader recognises.
	korean := collate.New(language.Korean)
	games := []Game{}
	group := ""
	sortFrom := 0
	// sortGroup orders the run of archives that came out of one directory.
	// Groups keep the order they were discovered in, so a file dropped
	// straight into the root still lists after the platform groups: it is a
	// game the user meant to play rather than a mistake to hide, but it is
	// the exception.
	sortGroup := func() {
		run := games[sortFrom:]
		sort.SliceStable(run, func(left, right int) bool {
			return korean.CompareString(run[left].Name, run[right].Name) < 0
		})
	}
	for _, entry := range gameroot.Entries(gameRoot) {
		if !gameExtensions[strings.ToLower(filepath.Ext(entry.Name))] {
			continue
		}
		if entry.Group != group || len(games) == 0 {
			sortGroup()
			group = entry.Group
			sortFrom = len(games)
		}
		location := prefix + "/"
		if entry.Group != "" {
			location += url.PathEscape(entry.Group) + "/"
		}
		games = append(games, Game{
			Group: entry.Group,
			Name:  strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name)),
			Path:  location + url.PathEscape(entry.Name),
			Added: added,
		})
	}
	sortGroup()
	return games
}

func (s *Server) serveGameList(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	body, err := json.Marshal(ListGames(s.gameRoot, s.addedRoot))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
}

// errNotAGame is what every road into the two roots answers with when the path
// it was handed does not name a game inside one of them. It says no more than
// that on purpose: which of the reasons it was is worth logging and is not
// worth telling a caller that is trying paths.
var errNotAGame = errors.New("the path does not name a game")

// gameFile resolves a path the picker handed out — `games/<group>/<archive>`
// or `ext/<archive>` — to the file it names, and reports whether it is one the
// page added. Every route that reaches an archive goes through here, so the
// check that a path stays inside a root is written once: the leading component
// chooses the root and cannot be anything else, and pathComponents refuses the
// components after it that would climb back out.
func (s *Server) gameFile(gamePath string) (file string, added bool, err error) {
	components, err := pathComponents(gamePath)
	if err != nil || len(components) < 2 {
		return "", false, errNotAGame
	}
	root := ""
	switch components[0] {
	case libraryPrefix:
		root = s.gameRoot
	case addedPrefix:
		root, added = s.addedRoot, true
	}
	// An unknown prefix leaves the root empty, and so does a host that was
	// given no added root: both would otherwise resolve against the working
	// directory, which is a place no game of ours lives.
	if root == "" {
		return "", false, errNotAGame
	}
	return filepath.Join(append([]string{root}, components[1:]...)...), added, nil
}

// gameFileInQuery is gameFile for the routes that take the path in a query
// rather than in the request path. `games.json` hands out percent-encoded
// paths and the page sends back what it was given, so a Korean name arrives
// escaped — where net/http has already unescaped a request path by the time a
// handler sees it. One helper so that neither road forgets which it is.
func (s *Server) gameFileInQuery(raw string) (file string, added bool, err error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false, errNotAGame
	}
	return s.gameFile(decoded)
}

// serveGameArchive serves `/games/<group>/<archive>` and `/ext/<archive>` out
// of the root each names. The archives are tens of megabytes and never change
// in place, so they are the one response worth revalidating instead of
// re-sending.
func (s *Server) serveGameArchive(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	file, _, err := s.gameFile(strings.TrimPrefix(request.URL.Path, "/"))
	if err != nil {
		writeError(writer, http.StatusForbidden, "Forbidden")
		return
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	archive, err := os.Open(file)
	if err != nil {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	defer archive.Close()

	securityHeaders(writer.Header())
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Content-Type", contentType(file))
	// Size and modification time identify a build of an archive well enough
	// for revalidation, and cost nothing to compute.
	writer.Header().Set("ETag", archiveETag(info.Size(), info.ModTime().UnixMilli()))
	// ServeContent answers the conditional request and any range request
	// against the ETag and time set here.
	http.ServeContent(writer, request, info.Name(), info.ModTime(), archive)
}

func archiveETag(size, modifiedMilliseconds int64) string {
	return `"` + strings.ToLower(strconv.FormatInt(size, 16)) + "-" +
		strings.ToLower(strconv.FormatInt(modifiedMilliseconds, 16)) + `"`
}
