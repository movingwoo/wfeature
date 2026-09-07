package webhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

// Taking a save off this machine, and putting one back.
//
// The save tree is on the server and the page never sees it. On a desktop that
// is merely opaque; on a phone it is unreachable — the same reason a game has
// to arrive over the socket (see upload.go) applies to a save that wants to
// leave. A player who reinstalls, changes machines, or moves from a phone to a
// desktop has no way to carry their progress with them, and no way to keep a
// copy of it anywhere.
//
// So one route in each direction, over the same socket everything else uses,
// carrying `internal/backend`'s container. What the container is for and why it
// refuses what it refuses is written there; what is decided here is who may
// read and write the tree while a game is running.
//
// **An export is not arbitrated. An import is, exactly as a save API write is.**
//
// The claim in saveclaim.go exists for one defect: two writers on one directory,
// where a session reads the files when the guest asks and writes them back
// whole, so the second one to write wins and the first player's progress is
// gone with nothing reported. An export writes nothing, so that defect cannot
// reach it — and refusing an export would refuse the page against its own
// running game, which is the sequence the two second grace was added to
// prevent, with no grace that could help here because the game is deliberately
// still running. What an unarbitrated export can catch is a set of files that
// spans one commit: every individual file is whole, because StoreSave replaces
// by rename, so the worst case is one entry from before a commit beside one
// from after. That is a save the player can export again a moment later, and it
// is a smaller loss than not being able to back up the game they are playing.
//
// An import writes, so it takes the claim the save API takes —
// `holdSaveDirectory`, for the length of the write. A restore landing under a
// running game would be erased at that game's next commit with nothing
// reported anywhere, which is the original defect arriving by a third road, so
// a live holder refuses it. A **parked** holder is closed and taken over, the
// way starting the game already does: parking is what survives a reload, so
// refusing it was a refusal no reload could clear, on the one screen where the
// game's own parked session is the likeliest thing holding the directory.

// savePackQuery names the game whose saves are being carried. It is the same
// string `games.json` handed the page, percent-encoding and all, checked here
// the way every other route checks it: this is not a way around the game root.
const savePackQuery = "game"

// savePackExtension is what an exported file is called. It is not `.zip` or
// `.bin` on purpose — the file has one meaning and a name that says so keeps
// somebody from unpacking it, editing it, and finding the checksum refuses it.
const savePackExtension = ".wfs"

// maxSavePackUpload bounds an imported container. Saves of this era are
// kilobytes; this is the same kind of generous ceiling maxGameUpload is, and it
// is here so a phone cannot be asked to hold an arbitrary body in memory.
const maxSavePackUpload = 64 << 20

// serveSavePack answers both directions:
//
//	GET  /api/savepack?game=<path>   -> the container for that game's saves
//	POST /api/savepack?game=<path>   <- a container restores them
func (s *Server) serveSavePack(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		s.exportSavePack(writer, request)
	case http.MethodPost:
		s.importSavePack(writer, request)
	default:
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
	}
}

// savePackTarget resolves the query into everything both directions need: the
// archive's identity, the directory its saves live in, and a name for the file.
// Every refusal it answers is written for the page to show.
func (s *Server) savePackTarget(request *http.Request) (identity [32]byte, directory, label string, err error) {
	game := request.URL.Query().Get(savePackQuery)
	if strings.TrimSpace(game) == "" {
		return identity, "", "", errors.New("게임을 고르지 않았습니다.")
	}
	archive, label, readErr := s.readGameArchive(game)
	if readErr != nil {
		return identity, "", "", errors.New("게임 파일을 찾을 수 없습니다.")
	}
	summary, inspectErr := session.Inspect(archive)
	if inspectErr != nil {
		// A damaged archive still names its platform, but it cannot name the
		// directory its saves belong in, which is the whole of what is needed
		// here.
		return identity, "", "", errors.New("이 게임 파일을 읽을 수 없어 세이브 위치를 알 수 없습니다.")
	}
	directory = s.saveDirectory(summary.Platform, summary.SaveOwner)
	if directory == "" {
		return identity, "", "", errors.New("이 게임은 세이브를 따로 두지 않습니다.")
	}
	return backend.SaveIdentity(archive), directory, label, nil
}

func (s *Server) exportSavePack(writer http.ResponseWriter, request *http.Request) {
	identity, directory, label, err := s.savePackTarget(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := backend.ReadSaveTree(directory)
	if err != nil {
		s.logger.Error("could not read a save tree for export", "directory", directory, "error", err)
		writeError(writer, http.StatusInternalServerError, "세이브를 읽지 못했습니다.")
		return
	}
	// An empty backup is worse than no backup: it is a file the person keeps,
	// believing their progress is in it. A game that has never saved is told
	// so instead.
	if len(entries) == 0 {
		writeError(writer, http.StatusNotFound, "이 게임에는 아직 저장된 데이터가 없습니다.")
		return
	}
	container, err := backend.EncodeSavePack(backend.SavePack{Identity: identity, Entries: entries})
	if err != nil {
		s.logger.Error("could not encode a save backup", "directory", directory, "error", err)
		writeError(writer, http.StatusInternalServerError, "세이브를 내보내지 못했습니다.")
		return
	}

	securityHeaders(writer.Header())
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", savePackDisposition(label))
	writer.Header().Set("Content-Length", fmt.Sprint(len(container)))
	s.logger.Info("a save was exported", "game", label, "entries", len(entries), "bytes", len(container))
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write(container); err != nil {
		s.logger.Warn("a save export did not finish sending", "game", label, "error", err)
	}
}

// savePackResult is what an import answers. The counts are there so the page
// can say what happened rather than only that it worked: a restore that wrote
// three entries and removed four is a different event from one that wrote
// three and removed none, and only the person looking at it knows whether that
// was what they wanted.
type savePackResult struct {
	Written int `json:"written"`
	Removed int `json:"removed"`
}

func (s *Server) importSavePack(writer http.ResponseWriter, request *http.Request) {
	identity, directory, label, err := s.savePackTarget(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	body, err := readBody(request, maxSavePackUpload)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			writeError(writer, http.StatusRequestEntityTooLarge, "세이브 파일이 너무 큽니다.")
			return
		}
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}

	pack, err := backend.DecodeSavePack(body)
	if err != nil {
		// Each of these is a different thing for the person holding the file to
		// do, which is why the container keeps them apart and why they are said
		// apart here.
		switch {
		case errors.Is(err, backend.ErrNotSavePack):
			writeError(writer, http.StatusBadRequest,
				"이 파일은 세이브 백업 파일이 아닙니다. 내보내기로 만든 "+savePackExtension+" 파일을 고르세요.")
		case errors.Is(err, backend.ErrSavePackVersion):
			writeError(writer, http.StatusBadRequest,
				"이 백업은 더 새로운 버전에서 만들어져 이 빌드가 읽을 수 없습니다.")
		default:
			writeError(writer, http.StatusUnprocessableEntity,
				"백업 파일이 손상되었습니다. 옮기는 중에 파일이 깨진 것이니 다시 내보내거나 다시 옮겨 오세요.")
		}
		s.logger.Warn("refused a save import", "game", label, "reason", err)
		return
	}
	if pack.Identity != identity {
		// The bytes are fine and the file is somebody's real backup — of
		// another game. Saying that is the difference between looking for the
		// right file and looking for a second copy of this one.
		s.logger.Warn("refused a save import for another game", "game", label,
			"backup", shortIdentity(pack.Identity), "expected", shortIdentity(identity))
		writeError(writer, http.StatusConflict, fmt.Sprintf(
			"이 백업은 다른 게임의 것입니다. 백업 %s, 지금 게임 %s.",
			shortIdentity(pack.Identity), shortIdentity(identity)))
		return
	}

	// An import writes the tree whole, so it takes the same claim one save API
	// write takes, and for the same reason. Unlike that write it takes a parked
	// holder over — a person chose this file and this game, and the parked game
	// it would otherwise be refused by is this game.
	held, holder := s.holdSaveDirectory(directory, "세이브 가져오기", true)
	if !held {
		s.logger.Warn("refused a save import into a directory a game holds",
			"game", label, "holder", holder)
		writeError(writer, http.StatusConflict,
			"이 게임이 실행 중입니다("+holder+"). 그 창에서 게임을 멈춘 뒤 다시 가져오세요.")
		return
	}
	defer s.releaseSaveDirectory(directory)

	written, removed, err := backend.WriteSaveTree(directory, pack.Entries)
	if err != nil {
		s.logger.Error("a save import failed partway", "game", label,
			"written", written, "error", err)
		writeError(writer, http.StatusInternalServerError, "세이브를 복원하지 못했습니다.")
		return
	}
	s.logger.Info("a save was imported", "game", label, "written", written, "removed", removed)
	body, err = json.Marshal(savePackResult{Written: written, Removed: removed})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
}

// shortIdentity is how an identity is shown to a person. The whole hash says
// nothing more than its first bytes do for the one question being asked — is
// this the same input — and a line of 64 hex characters in a message box is
// read by nobody.
func shortIdentity(identity [32]byte) string {
	return fmt.Sprintf("%x", identity[:4])
}

// savePackDisposition names the downloaded file. These names are Korean, and a
// Content-Disposition filename is Latin-1, so the name travels in the RFC 5987
// form every browser has read for a decade; the plain parameter carries a
// fallback for anything that does not.
func savePackDisposition(label string) string {
	name := label + savePackExtension
	return fmt.Sprintf(`attachment; filename="save%s"; filename*=UTF-8''%s`,
		savePackExtension, url.PathEscape(name))
}
