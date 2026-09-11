package detect

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"path"
	"strings"
)

// IsScriptPackage identifies the carrier's SGS packages by a descriptor beside
// a matching payload. The extension alone is not a runtime contract. This
// recognizes packaging, not the validity or executability of the script.
func IsScriptPackage(reader *zip.Reader) bool {
	payloads := make(map[string]bool)
	for _, file := range reader.File {
		name := strings.ToLower(entryName(file.Name))
		if path.Ext(name) == ".sgs" && !file.FileInfo().IsDir() {
			payloads[strings.TrimSuffix(name, ".sgs")] = true
		}
	}
	const maxDescriptor = 4096
	probed := 0
	for _, file := range reader.File {
		name := strings.ToLower(entryName(file.Name))
		ext := path.Ext(name)
		if (ext != ".mod" && ext != ".inf") || !payloads[strings.TrimSuffix(name, ext)] {
			continue
		}
		if probed >= 16 {
			break
		}
		probed++
		if file.UncompressedSize64 > maxDescriptor {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(opened, maxDescriptor+1))
		opened.Close()
		if err != nil || len(data) > maxDescriptor {
			continue
		}
		if ext == ".mod" && sgsDescriptor(data) {
			return true
		}
		if ext == ".inf" && gvmDescriptor(data) {
			return true
		}
	}
	return false
}

func sgsDescriptor(data []byte) bool {
	// MOD descriptors store length-prefixed strings. The
	// content type followed by the payload extension identifies this runtime.
	var marker []byte
	for _, value := range []string{"application/x-gnex-sgs", "sgs"} {
		marker = binary.LittleEndian.AppendUint32(marker, uint32(len(value)))
		marker = append(marker, value...)
	}
	return bytes.Contains(bytes.ToLower(data), marker)
}

func gvmDescriptor(data []byte) bool {
	// INF descriptors end with three length-prefixed fields: the runtime
	// name and two server addresses, each address followed by a 16-bit port.
	// Only the runtime name and the field bounds are relevant to detection.
	for i := 0; i+6 <= len(data); i++ {
		if data[i] != 3 || string(data[i+3:i+6]) != "GVM" {
			continue
		}
		if i+6+int(data[i+1])+2+int(data[i+2])+2 == len(data) {
			return true
		}
	}
	return false
}
