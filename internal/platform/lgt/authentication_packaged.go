package lgt

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"path"
	"strings"
)

// An exported 76-byte certificate has valid application fields but an empty
// subscriber. Mounting that record makes the client reject the handset before
// it offers enrollment. The recognized local service lets the game issue and
// persist its own record. Filter only the known packaged bytes in this
// client's view, including aliases from a supplemental directory.
func (client *Client) omitUnassignedCertificate(digest string) {
	if digest == "" {
		return
	}
	view := *client.archive
	view.Packaged = omitPackagedCertificate(view.Packaged, digest)
	view.supplemental = omitPackagedCertificate(view.supplemental, digest)
	client.archive = &view
}

func omitPackagedCertificate(files map[string][]byte, digest string) map[string][]byte {
	var filtered map[string][]byte
	for name, data := range files {
		if len(data) != 76 || !strings.EqualFold(path.Base(name), "cert.c2s") || certificateDigest(data) != digest {
			continue
		}
		if filtered == nil {
			filtered = maps.Clone(files)
		}
		delete(filtered, name)
	}
	if filtered == nil {
		return files
	}
	return filtered
}

func certificateDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
