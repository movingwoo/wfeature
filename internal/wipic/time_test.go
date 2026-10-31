package wipic

import (
	"encoding/binary"
	"testing"
	"time"
)

// Both C libraries that answer localtime fill their struct tm from here, so
// the members, their order and the record a guest reads are pinned once.
func TestTimeFieldsAreAStructTm(t *testing.T) {
	moment := time.Date(2007, time.March, 4, 5, 6, 7, 0, time.UTC)
	want := [9]int32{7, 6, 5, 4, 2, 107, 0, 62, 0}
	if got := TimeFields(moment); got != want {
		t.Fatalf("TimeFields = %v, want %v", got, want)
	}
	record := TimeRecord(moment)
	if len(record) != TimeRecordSize {
		t.Fatalf("record is %d bytes, want %d", len(record), TimeRecordSize)
	}
	for index, value := range want {
		if got := int32(binary.LittleEndian.Uint32(record[index*4:])); got != value {
			t.Fatalf("member %d reads %d, want %d", index, got, value)
		}
	}
}
