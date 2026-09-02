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

	"github.com/movingwoo/wfeature/internal/gameroot"
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
		prefix := "games/"
		if entry.Group != "" {
			prefix += url.PathEscape(entry.Group) + "/"
		}
		games = append(games, Game{
			Group: entry.Group,
			Name:  strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name)),
			Path:  prefix + url.PathEscape(entry.Name),
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
