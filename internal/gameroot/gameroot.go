// Package gameroot names the boundary a Host discovers games within, so that
// every tool which reasons about "the library" reasons about the same files.
//
// The boundary is not a taste: the picker reads the game root and one level of
// platform group below it, and an archive deeper than that is not a game the
// user can start. A tool that walks the whole tree instead reports on archives
// no Host will ever offer — which is what an ignored diagnostic corpus under
// the root is made of.
package gameroot

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// Entry is one file inside the boundary.
type Entry struct {
	// Group is the directory below the root that holds the file, and is
	// empty for a file sitting in the root itself.
	Group string
	// Name is the file name, extension included.
	Name string
	// Path is the file's path on disk, root included.
	Path string
}

// Entries lists the files a Host can discover under root: the files one
// directory below it, group by group, and then the files in the root itself.
// Nothing deeper is returned.
//
// A root that cannot be read is an empty list rather than an error — a fresh
// install has no games yet, and a Host has to keep working so the user can see
// where to put them. A group that cannot be read is skipped for the same
// reason: one unreadable directory is not a reason to report nothing.
func Entries(root string) []Entry {
	return entries(root, os.DirFS(root), false)
}

// ConfinedEntries lists a shared library. An operator may link to archives
// within the library, while links leading out of it grant no access.
func ConfinedEntries(root string) []Entry {
	library, err := os.OpenRoot(root)
	if err != nil {
		return []Entry{}
	}
	defer library.Close()
	return entries(root, library.FS(), true)
}

func entries(root string, files fs.FS, confined bool) []Entry {
	// ReadDir sorts by name, so the groups come out in a stable order without
	// a second pass over them.
	top, err := fs.ReadDir(files, ".")
	if err != nil {
		return []Entry{}
	}
	grouped, loose := []Entry{}, []Entry{}
	for _, item := range top {
		group := item.Name()
		mode := entryMode(files, group, item)
		if !mode.IsDir() {
			if !confined || mode.IsRegular() {
				loose = append(loose, Entry{Name: group, Path: filepath.Join(root, group)})
			}
			continue
		}
		inside, err := fs.ReadDir(files, group)
		if err != nil {
			continue
		}
		for _, file := range inside {
			mode := entryMode(files, path.Join(group, file.Name()), file)
			if mode.IsDir() || (confined && !mode.IsRegular()) {
				continue
			}
			grouped = append(grouped, Entry{Group: group, Name: file.Name(), Path: filepath.Join(root, group, file.Name())})
		}
	}
	// A file dropped straight into the root is a game the user meant to play
	// rather than a mistake to hide. It comes last: an ungrouped archive is
	// the exception.
	return append(grouped, loose...)
}

// A personal library can follow group links to another disk. A shared root's
// FS enforces the same symlink boundary for this listing and archive opens.
func entryMode(files fs.FS, name string, entry fs.DirEntry) fs.FileMode {
	if entry.Type()&fs.ModeSymlink != 0 {
		if info, err := fs.Stat(files, name); err == nil {
			return info.Mode()
		}
	}
	return entry.Type()
}

// Paths is Entries reduced to the paths of the files whose extension is one
// the caller loads, matched without regard to case. It is the form a scanner
// wants, and it is sorted so a report reads the same way twice.
func Paths(root string, extensions ...string) []string {
	wanted := make(map[string]bool, len(extensions))
	for _, extension := range extensions {
		wanted[normalizeExtension(extension)] = true
	}
	var paths []string
	for _, entry := range Entries(root) {
		if !wanted[normalizeExtension(filepath.Ext(entry.Name))] {
			continue
		}
		paths = append(paths, entry.Path)
	}
	sort.Strings(paths)
	return paths
}

func normalizeExtension(extension string) string {
	if extension == "" {
		return ""
	}
	if extension[0] != '.' {
		extension = "." + extension
	}
	return lowerASCII(extension)
}

// lowerASCII lowers an extension without pulling in a locale: an archive
// extension is ASCII, and a Turkish locale lowering "I" is not wanted here.
func lowerASCII(text string) string {
	lowered := []byte(text)
	for index, character := range lowered {
		if character >= 'A' && character <= 'Z' {
			lowered[index] = character + ('a' - 'A')
		}
	}
	return string(lowered)
}
