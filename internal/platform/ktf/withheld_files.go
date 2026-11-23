package ktf

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// A withheldFile is a private file of a capture kept from the guest: its name
// and the SHA-256 of the copy the capture carries. A file of the same name
// with other content is presented as usual.
type withheldFile struct {
	name   string
	sha256 string
}

// withheldFiles are, by the digest of the client image as the JAR packages
// it, the private files of a capture that ktf.withheld_private_file keeps from
// the guest. A capture is whatever was on the handset when it was taken, and
// one registered capture kept its options record but none of the saves that
// record lists: the title's menu opens on Continue, offers two saves that are
// not there, and starts a character without a level or health when one is
// chosen. Without the record the menu opens on a new game with every slot
// empty, and the title writes its own record when a setting changes. Only that
// record is withheld: another capture of the same client, whose record lists
// saves it does carry, keeps its own.
// See docs/ktf.md#withheld-private-files.
var withheldFiles = map[string][]withheldFile{
	"df6a087ca91b09759b9713aa8e040019228154714b1686012e4f1802f9064fe2": {
		{name: "options.dat", sha256: "70c9fcc7cb0beeb4c7f1b246a71d8bf8ade698252bb911bc98d9f4d6ec62301d"},
	},
}

// withholdPrivateFiles removes a recognized client's withheld files from the
// guest's view of its archive, matched by name whatever its case and by
// content. The archive itself is not changed, and a file the title writes
// under the same name is its own.
func withholdPrivateFiles(files map[string][]byte, digest string) map[string][]byte {
	for name, data := range files {
		sum := sha256.Sum256(data)
		for _, withheld := range withheldFiles[digest] {
			if strings.EqualFold(name, withheld.name) && hex.EncodeToString(sum[:]) == withheld.sha256 {
				delete(files, name)
			}
		}
	}
	return files
}

// sessionGuestFiles is the archive as the session's guest reads it. keyNumber
// is the handset whose key a registered client's capture saves are presented
// under, or "" for none (see saveKeyNumber).
func sessionGuestFiles(archive *Archive, revision compatibility.NativeRevision, keyNumber string) (map[string][]byte, error) {
	files := archive.GuestFiles()
	image := archive.JAR.Client.Data
	if revision.Has(compatibility.KTFWithheldPrivateFile) {
		files = withholdPrivateFiles(files, revision.Digest())
	}
	if keyNumber != "" && revision.Has(compatibility.KTFSaveSubscriberKey) {
		return rekeySaves(files, image, revision.Digest(), keyNumber)
	}
	return files, nil
}

// saveKeyNumber is the handset a starting session presents a registered
// client's capture saves for: the implicit handset with compatibility
// enabled. A configured handset and -no-auth read them as packaged. A restored
// session takes the choice its record was started with instead.
func saveKeyNumber(options SessionOptions) string {
	if options.DisableAuthentication || wipic.ConfiguredSubscriberNumber() != "" {
		return ""
	}
	return wipic.SubscriberNumber()
}
