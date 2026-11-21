package skt

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/detect"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func openScript(data []byte) (*Archive, error) {
	reader, _, err := openWithin(data, "SKT archive", defaultArchiveLimits)
	if err != nil {
		return nil, err
	}
	if !detect.IsScriptPackage(reader) {
		return nil, nil
	}
	var payload []byte
	var scriptName string
	for _, file := range reader.File {
		if !strings.EqualFold(path.Ext(file.Name), ".sgs") {
			continue
		}
		name, err := safeEntryName(file.Name)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			return nil, fmt.Errorf("SKT script archive contains multiple SGS payloads")
		}
		scriptName = name
		payload, err = readEntryWithin(file, sgsvm.MaxWideSize+32)
		if err != nil {
			return nil, err
		}
	}
	program, err := sgsvm.Parse(payload)
	if err != nil {
		return nil, err
	}
	// Some carrier descriptors have been reused for unrelated games. The
	// executable digest keeps their persistent storage separate.
	identity := sha256.Sum256(program.Data)
	archive := &Archive{Script: program, scriptDigest: hex.EncodeToString(identity[:]), Descriptor: Descriptor{Name: program.Name}, ScriptSaveOwner: fmt.Sprintf("sgs-%x", identity[:16])}
	if program.Wide != nil {
		archive.Entries, err = scriptInstalledFiles(reader, path.Dir(scriptName))
		if err != nil {
			return nil, err
		}
		descriptor := strings.TrimSuffix(scriptName, path.Ext(scriptName)) + ".mod"
		for _, file := range reader.File {
			if strings.EqualFold(path.Clean(strings.ReplaceAll(file.Name, "\\", "/")), descriptor) {
				record, err := readEntryWithin(file, 4096)
				if err != nil {
					return nil, err
				}
				archive.ScriptUserID = scriptInstallUserID(record)
				break
			}
		}
	}
	return archive, err
}

// Script-owned files next to the executable provide the handset's initial
// private storage. A later session write takes precedence over these bytes.
func scriptInstalledFiles(reader *zip.Reader, directory string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	remaining := uint64(16 << 20)
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name, err := safeEntryName(entry.Name)
		if err != nil {
			return nil, err
		}
		if path.Dir(name) != directory {
			continue
		}
		switch strings.ToLower(path.Ext(name)) {
		case ".sgs", ".mod", ".inf", ".res", ".wmr", ".exe":
			continue
		}
		key := strings.ToLower(path.Base(name))
		if _, exists := files[key]; exists {
			return nil, fmt.Errorf("SGS: duplicate installed file %q", key)
		}
		data, err := readEntryWithin(entry, min(remaining, 4<<20))
		if err != nil {
			return nil, err
		}
		files[key] = data
		remaining -= uint64(len(data))
	}
	return files, nil
}

// The download descriptor carries the user ID supplied to this installation.
// Fields following the SGS media type are length-prefixed, including empties.
func scriptInstallUserID(record []byte) string {
	mime := []byte("application/x-gnex-sgs")
	pos := bytes.Index(record, mime)
	if pos < 4 || binary.LittleEndian.Uint32(record[pos-4:pos]) != uint32(len(mime)) {
		return ""
	}
	data := record[pos+len(mime):]
	var field []byte
	for i := 0; i < 4; i++ {
		if len(data) < 4 {
			return ""
		}
		size := binary.LittleEndian.Uint32(data)
		data = data[4:]
		if uint64(size) > uint64(len(data)) {
			return ""
		}
		field, data = data[:size], data[size:]
		if i == 0 && !bytes.EqualFold(field, []byte("sgs")) {
			return ""
		}
	}
	for _, part := range strings.Split(string(field), ";") {
		id, found := strings.CutPrefix(part, "ID:")
		if !found || len(id) != 8 {
			continue
		}
		if _, err := hex.DecodeString(id); err == nil {
			return id
		}
	}
	return ""
}
