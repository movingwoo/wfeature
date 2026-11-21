package ktf

import (
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// saveFileTimeRewrites are, by the digest of the client image as the JAR
// packages it, what ktf.save_file_time changes. A recognized client writes its
// save, asks MC_fsFileAttribute for the file's time, stores that time inside
// the save and writes it again. When it loads the save it asks for the time
// once more and deletes the save, writing its defaults in its place, if the
// two differ by more than 5000 seconds: a save copied from elsewhere carries
// a time its file no longer has. A capture is such a copy. This platform
// answers a time of 0 for every file, so the saves the title writes itself
// pass, and the one its capture carries, which holds the time the original
// handset gave it, never does.
//
// Reporting real file times would not help the capture and would hurt the
// title's own saves: every one written under this platform holds 0, and the
// first load after such a change would delete them. So the comparison is left
// to answer "the same" for this client instead: its delete path branches to
// the exit every matching time takes. Its length check and its decoding of
// the save run as packaged.
// See docs/ktf.md#save-file-time.
var saveFileTimeRewrites = map[string][]codeRewrite{
	"fb39fb636a1311480f71123b411e9641a124486401a99faef0a5989a0a0b210b": {
		// The comparison of the save's stored time (a 64-bit word at 0x5e0 of
		// the loaded save) with the file's time on both of its signs, through
		// the branch back at 0x1497d8, and the start of the delete path at
		// 0x149794, which every difference above the threshold reaches.
		// ldr r3,[sp] there becomes b 0x149536, the exit the matching path
		// takes at 0x1497bc.
		{at: 0x149770, expect: []byte{
			0x2e, 0x4b, 0x53, 0x44, 0x1b, 0x68, 0xbc, 0x20, 0xc0, 0x00, 0x1b, 0x18, 0x1e, 0x68, 0x5f, 0x68,
			0x32, 0x99, 0x00, 0x22, 0x3d, 0x1c, 0x34, 0x1c, 0x64, 0x1a, 0x95, 0x41, 0x00, 0x2d, 0x16, 0xdb,
			0x00, 0x2d, 0x0d, 0xdd, 0x00, 0x9b, 0x53, 0x44, 0x18, 0x68, 0x53, 0x46, 0x43, 0x44, 0x19, 0x68,
			0x19, 0xf0, 0xf2, 0xff, 0x22, 0x4b, 0x53, 0x44, 0x1a, 0x68, 0x01, 0x23, 0x13, 0x60, 0xc2, 0xe6,
			0x00, 0x2d, 0x00, 0xd0, 0xbf, 0xe6, 0x1f, 0x49, 0x8c, 0x42, 0xeb, 0xd8, 0xbb, 0xe6, 0x14, 0x1c,
			0x0b, 0x1c, 0x9b, 0x1b, 0xbc, 0x41, 0x00, 0x2c, 0xe4, 0xdc, 0x00, 0x2c, 0x00, 0xd0, 0xb2, 0xe6,
			0x18, 0x4a, 0x93, 0x42, 0x00, 0xd8, 0xae, 0xe6, 0xdc, 0xe7,
		}, offset: 0x24, replace: []byte{0xcf, 0xe6}},
	},
}

func hasSaveFileTimeCompatibility(image []byte) bool {
	return compatibility.HasFix("ktf", compatibility.NativeModule, clientDigest(image), compatibility.KTFSaveFileTime)
}

// applySaveFileTime makes a recognized client accept the time its save
// records whatever the file's time is now. The handset plays no part.
func applySaveFileTime(memory *armcore.Memory, digest string) error {
	return applyCodeRewrites(memory, saveFileTimeRewrites[digest], "save file time", digest)
}
