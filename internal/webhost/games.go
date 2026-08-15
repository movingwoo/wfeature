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
	// Group is the platform directory the archive sits in.
	Group string `json:"group"`
	// Name is the archive's file name without its extension.
	Name string `json:"name"`
	// Path is the URL the page loads the archive from.
	Path string `json:"path"`
}

// ListGames describes the game root as the picker consumes it: one entry per
// archive, grouped by the platform directory holding it. A missing root is an
// empty list rather than an error — a fresh install has no games yet, and the
// page has to load anyway so the user can see where to put them.
func ListGames(gameRoot string) []Game {
	// ReadDir sorts by name, so the platform groups come out in a stable
	// order without a second pass over them.
	groups, err := os.ReadDir(gameRoot)
	if err != nil {
		return []Game{}
	}
	// Titles are Korean, so ordering them by byte would put them in an order
	// no reader recognises.
	korean := collate.New(language.Korean)
	games := []Game{}
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(gameRoot, group.Name()))
		if err != nil {
			continue
		}
		found := []Game{}
		for _, entry := range entries {
			if entry.IsDir() || !gameExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
				continue
			}
			found = append(found, Game{
				Group: group.Name(),
				Name:  strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
				Path:  "games/" + url.PathEscape(group.Name()) + "/" + url.PathEscape(entry.Name()),
			})
		}
		sort.SliceStable(found, func(left, right int) bool {
			return korean.CompareString(found[left].Name, found[right].Name) < 0
		})
		games = append(games, found...)
	}
	return games
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
