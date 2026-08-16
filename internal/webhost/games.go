package webhost

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// gameExtensions are the archive shapes the engine loads. A KTF or LGT game is
// a zip and a MIDlet is a jar; which platform a file belongs to is the
// engine's answer from the bytes, so the server only decides what is worth
// listing.
var gameExtensions = map[string]bool{".zip": true, ".jar": true}

// Game is one entry in the picker.
type Game struct {
	// Group is the platform directory the archive sits in, and is empty for an
	// archive dropped straight into the game root.
	Group string `json:"group"`
	// Name is the archive's file name without its extension.
	Name string `json:"name"`
	// Path is the URL the page loads the archive from.
	Path string `json:"path"`
}

// ListGames describes the game root as the picker consumes it: one entry per
// archive, grouped by the platform directory holding it, and then the archives
// sitting in the root itself. A missing root is an empty list rather than an
// error — a fresh install has no games yet, and the page has to load anyway so
// the user can see where to put them.
func ListGames(gameRoot string) []Game {
	// ReadDir sorts by name, so the platform groups come out in a stable
	// order without a second pass over them.
	entries, err := os.ReadDir(gameRoot)
	if err != nil {
		return []Game{}
	}
	// Titles are Korean, so ordering them by byte would put them in an order
	// no reader recognises.
	korean := collate.New(language.Korean)
	games := []Game{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		inside, err := os.ReadDir(filepath.Join(gameRoot, entry.Name()))
		if err != nil {
			continue
		}
		games = append(games, archives(entry.Name(), inside, korean)...)
	}
	// A file dropped straight into the root is a game the user meant to play
	// rather than a mistake to hide, and which platform loads it is decided by
	// its bytes and not by the directory it was filed under. It lists after
	// the platform groups: an ungrouped archive is the exception.
	return append(games, archives("", entries, korean)...)
}

// archives picks the loadable archives out of one directory listing and orders
// them the way a Korean reader expects. group names the directory the entries
// were read from, and is empty for the game root itself.
func archives(group string, entries []os.DirEntry, korean *collate.Collator) []Game {
	prefix := "games/"
	if group != "" {
		prefix += url.PathEscape(group) + "/"
	}
	found := []Game{}
	for _, entry := range entries {
		if entry.IsDir() || !gameExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		found = append(found, Game{
			Group: group,
			Name:  strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Path:  prefix + url.PathEscape(entry.Name()),
		})
	}
	sort.SliceStable(found, func(left, right int) bool {
		return korean.CompareString(found[left].Name, found[right].Name) < 0
	})
	return found
}

func (s *Server) serveGameList(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	body, err := json.Marshal(ListGames(s.gameRoot))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
}

// serveGameArchive serves /games/<group>/<archive> out of the game root. The
// archives are tens of megabytes and never change in place, so they are the
// one response worth revalidating instead of re-sending.
func (s *Server) serveGameArchive(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	components, err := pathComponents(strings.TrimPrefix(request.URL.Path, "/games"))
	if err != nil {
		writeError(writer, http.StatusForbidden, "Forbidden")
		return
	}
	if len(components) == 0 {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	file := filepath.Join(append([]string{s.gameRoot}, components...)...)
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
