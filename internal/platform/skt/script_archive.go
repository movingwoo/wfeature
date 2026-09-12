package skt

import (
	"crypto/sha256"
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
	for _, file := range reader.File {
		if !strings.EqualFold(path.Ext(file.Name), ".sgs") {
			continue
		}
		if _, err := safeEntryName(file.Name); err != nil {
			return nil, err
		}
		if payload != nil {
			return nil, fmt.Errorf("SKT script archive contains multiple SGS payloads")
		}
		payload, err = readEntryWithin(file, 128*1024+32)
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
	return &Archive{Script: program, Descriptor: Descriptor{Name: program.Name}, ScriptSaveOwner: fmt.Sprintf("sgs-%x", identity[:16])}, nil
}
