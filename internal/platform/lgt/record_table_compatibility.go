package lgt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// recordTableName is the packaged table the recognized revision searches,
// each time it loads a map, for the record that gives the map's doors their
// destinations. The search reads the table two bytes at a time from its
// start: `23 <map>` is the record, and a pair whose first byte is anything
// else and whose second is 0x24 makes it read a third byte, 0x8a there ending
// the search with nothing found. Nothing bounds it by the table's length.
//
// The table has no record for two maps that carry a door and holds the end
// sequence nowhere the search reads, so loading either map sends the search
// past the buffer until it faults at the end of the heap. With the sequence
// appended the search leaves by its own exit for a missing record, and the
// door keeps the empty destination the title allocated before searching.
// See docs/history/lgt.md#lgt-qa-2026-10-28-unterminated-record-table.
const recordTableName = "scp_port.scp"

// recordTableEnd holds the end sequence twice, one byte apart. A pair whose
// second byte is 0x24 costs the search a third byte, so it can arrive at the
// table's end on either parity; from every position it can reach there, these
// bytes complete the sequence, and none of them is 0x23, which would be read
// as a record.
var recordTableEnd = []byte{0x24, 0x24, 0x8a, 0x24, 0x8a}

func hasRecordTableTerminator(module []byte) bool {
	digest := sha256.Sum256(module)
	return compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTRecordTableTerminator)
}

// packagedResource is a file the archive ships, as the title's file and
// resource calls read it. Only the archive's copy is corrected: a file the
// title wrote under the same name is its own, and readFile answers with that
// before it gets here.
func (client *Client) packagedResource(path string) ([]byte, bool) {
	data, ok := client.archive.Resource(path)
	if !ok || !client.recordTableTerminator || !strings.EqualFold(path, recordTableName) {
		return data, ok
	}
	if client.logger != nil {
		client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTRecordTableTerminator)
	}
	return append(bytes.Clone(data), recordTableEnd...), true
}
