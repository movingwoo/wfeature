package detect

import (
	"archive/zip"
	"bytes"
	"debug/elf"
	"encoding/binary"
	"io"
	"path"
	"strings"

	"github.com/movingwoo/wfeature/internal/zipentry"
)

// MaxLGTSyncJARSize bounds inspection of a nested descriptorless application.
const MaxLGTSyncJARSize = maxScannedEntry

// LGTSyncJAR returns the exporter's JAR when it is the only JAR in the
// package. The name alone is shared by exports for different handsets.
func LGTSyncJAR(names []string, wrapper string) string {
	candidate := ""
	for _, name := range names {
		if !strings.EqualFold(path.Ext(name), ".jar") {
			continue
		}
		relative := name
		if wrapper != "" {
			relative = strings.TrimPrefix(relative, wrapper+"/")
		}
		if candidate != "" || !strings.EqualFold(relative, "WEBSYNC1.jar") {
			return ""
		}
		candidate = name
	}
	return candidate
}

// LGTSyncModule identifies the export's binary.mod as a little-endian ARM
// ELF32 executable. The execution layer validates the full module at load.
func LGTSyncModule(header []byte) bool {
	return len(header) >= 20 && string(header[:7]) == "\x7fELF\x01\x01\x01" &&
		elf.Type(binary.LittleEndian.Uint16(header[16:])) == elf.ET_EXEC &&
		elf.Machine(binary.LittleEndian.Uint16(header[18:])) == elf.EM_ARM
}

func isLGTSyncExport(outer *zip.Reader) bool {
	var names []string
	var candidate *zip.File
	for _, f := range outer.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := entryName(f.Name)
		names = append(names, name)
		if strings.EqualFold(path.Base(name), "WEBSYNC1.jar") {
			candidate = f
		}
	}
	if candidate == nil || LGTSyncJAR(names, zipentry.Directory(names)) == "" || candidate.UncompressedSize64 > MaxLGTSyncJARSize {
		return false
	}
	rc, err := candidate.Open()
	if err != nil {
		return false
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxLGTSyncJARSize+1))
	if err != nil || len(data) > MaxLGTSyncJARSize {
		return false
	}
	jar, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range jar.File {
		if entryName(f.Name) == "binary.mod" {
			rc, err := f.Open()
			if err != nil {
				return false
			}
			defer rc.Close()
			var header [20]byte
			_, err = io.ReadFull(rc, header[:])
			return err == nil && LGTSyncModule(header[:])
		}
	}
	return false
}
