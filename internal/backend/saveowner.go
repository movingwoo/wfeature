package backend

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A game's saves live in one directory below its platform's save root, named
// by the game's save owner: the PID for KTF and LGT, the program number for
// SKT. Archives carry the name, so a repack can carry one that could steer a
// path, cannot be a folder on every system, or hides in a listing. The rule
// for an owner and the mapping of one that breaks it are here, so that every
// platform and both Hosts name a game's directory the same way.

// SaveOwnerMaxBytes is the longest owner a Host accepts.
const SaveOwnerMaxBytes = 64

// saveOwnerRefusedRune is the rule for one character of an owner: the path and
// Windows-illegal characters, controls, and the invisible direction marks that
// make a listing lie.
func saveOwnerRefusedRune(character rune) bool {
	switch {
	case character < 0x20, character == 0x7f:
		return true
	case strings.ContainsRune(`/\:*?"<>|`, character):
		return true
	// The bidirectional overrides and the zero-width joiners are invisible in
	// a file listing, which is exactly what makes a name carrying them worth
	// refusing.
	case character == 0x200e || character == 0x200f || character == 0x061c:
		return true
	case character >= 0x202a && character <= 0x202e:
		return true
	case character >= 0x2066 && character <= 0x2069:
		return true
	case character == 0xfeff:
		return true
	}
	return false
}

// SaveOwnerRefused reports whether an owner cannot name a save directory.
func SaveOwnerRefused(owner string) bool {
	if owner == "" || len(owner) > SaveOwnerMaxBytes || owner == "." || owner == ".." || owner == saveReservedName {
		return true
	}
	for _, character := range owner {
		if saveOwnerRefusedRune(character) {
			return true
		}
	}
	return false
}

// AcceptedSaveOwner maps an owner SaveOwnerRefused refuses to one it accepts,
// and leaves every other owner exactly as it is, because that is where its
// saves already are. A refused character becomes '_', a name too long keeps
// its start, and the result always ends in `~` and a digest of the whole name:
// "A: B" and "A? B" would otherwise share one directory, and so would either
// and a title whose own name is "A_ B". An empty owner stays empty: it names
// a game that keeps no saves.
func AcceptedSaveOwner(owner string) string {
	if owner == "" || !SaveOwnerRefused(owner) {
		return owner
	}
	mapped := strings.Map(func(character rune) rune {
		if saveOwnerRefusedRune(character) {
			return '_'
		}
		return character
	}, owner)
	digest := sha256.Sum256([]byte(owner))
	suffix := fmt.Sprintf("~%x", digest[:4])
	if cut := SaveOwnerMaxBytes - len(suffix); len(mapped) > cut {
		for cut > 0 && !utf8.RuneStart(mapped[cut]) {
			cut--
		}
		mapped = mapped[:cut]
	}
	if mapped = strings.TrimSpace(mapped); mapped == "" {
		mapped = "unnamed"
	}
	return mapped + suffix
}

// saveReservedName is the directory beside the owner directories of one
// platform root where a save store keeps quick-save slots and replacement
// state, in owners/<owner>.
const saveReservedName = ".wfeature-quicksave"

// EarlierSaveOwner answers the owner an earlier release kept a game's saves
// under, when it is not the one AcceptedSaveOwner names now and is a name a
// directory below the root could have: earlier releases joined the owner into
// the root as it was, on the command line, while the browser Host refused it
// and ran without saves. Anything else answers "".
func EarlierSaveOwner(raw string) string {
	if raw == "" || raw == AcceptedSaveOwner(raw) || raw == "." || raw == ".." || raw == saveReservedName ||
		!filepath.IsLocal(raw) || filepath.Base(raw) != raw || strings.ContainsAny(raw, `/\`) {
		return ""
	}
	return raw
}

// MigrateSaveOwner moves a game's saves from the directory an earlier release
// named it by to the one its owner names now, below one platform save root. It
// belongs where a Host joins the owner to its root, before it claims the
// game's save directory; earlier is what EarlierSaveOwner answered.
//
// The quick-save slots and a set-aside generation of saves, which the save
// store keeps in its reserved directory named after the owner, move with them.
// Nothing is merged and nothing is replaced: when the current owner directory
// already exists the old one stays where it is, and so does a slot the current
// owner already has. Both directories are claimed for the move, so a session
// that holds either leaves it for the next start, and no session can make the
// current owner's directory between the look that found it absent and the
// move.
func MigrateSaveOwner(root, earlier, owner string) error {
	if root == "" || earlier == "" || owner == "" || earlier == owner || EarlierSaveOwner(earlier) != earlier || SaveOwnerRefused(owner) {
		return nil
	}
	source, target := filepath.Join(root, earlier), filepath.Join(root, owner)
	if info, err := os.Lstat(source); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("save owner %q: %w", earlier, err)
	} else if !info.IsDir() {
		return nil
	}
	release, err := ClaimSaveDirectory(source)
	if err != nil {
		return fmt.Errorf("save owner %q: %w", earlier, err)
	}
	defer release()
	releaseTarget, err := ClaimSaveDirectory(target)
	if err != nil {
		return fmt.Errorf("save owner %q: %w", owner, err)
	}
	defer releaseTarget()
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("save owner %q: %w", owner, err)
	}
	// An interrupted quick load is settled under the old name first, so what
	// moves is the tree it left rather than half of a replacement.
	if _, err := NewDirectorySaveStore(source).DisplacedGeneration(); err != nil {
		return fmt.Errorf("save owner %q: %w", earlier, err)
	}
	// The reserved entries move before the saves: until the saves have moved,
	// a later start finds the old owner again and finishes the move.
	reserved := filepath.Join(root, saveReservedName, "owners")
	entries, err := os.ReadDir(filepath.Join(reserved, earlier))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("save owner %q: %w", earlier, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		slot := entry.Type().IsRegular() && strings.HasSuffix(name, ".wfq")
		if !slot && !(name == "previous" && entry.IsDir()) {
			continue
		}
		destination := filepath.Join(reserved, owner, name)
		if _, err := os.Lstat(destination); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Join(reserved, owner), 0o700); err != nil {
			return fmt.Errorf("save owner %q: %w", owner, err)
		}
		if err := os.Rename(filepath.Join(reserved, earlier, name), destination); err != nil {
			return fmt.Errorf("move quick save %q from %q to %q: %w", name, earlier, owner, err)
		}
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("move saves from %q to %q: %w", earlier, owner, err)
	}
	return nil
}

// SaveOwnerDirectory answers the directory below a platform root a game's
// saves are read from before MigrateSaveOwner has run: the current owner's,
// unless only the earlier owner's directory is there. It only looks, so a Host
// can answer a question about the saves without moving them.
func SaveOwnerDirectory(root, earlier, owner string) string {
	target := filepath.Join(root, owner)
	if earlier == "" || EarlierSaveOwner(earlier) != earlier {
		return target
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		return target
	}
	source := filepath.Join(root, earlier)
	if info, err := os.Lstat(source); err == nil && info.IsDir() {
		return source
	}
	return target
}
