package jvm

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"
)

// This small, newly authored TZif has a non-standard initial zone, one past
// transition, and one beyond 2038. No host timezone database is needed.
func checkpointCalendarLocation(t *testing.T, recurring bool) *time.Location {
	t.Helper()
	var data []byte
	header := func(transitions, zones, names uint32) {
		data = append(data, 'T', 'Z', 'i', 'f', '2')
		data = append(data, make([]byte, 15)...)
		for _, value := range []uint32{0, 0, 0, transitions, zones, names} {
			data = binary.BigEndian.AppendUint32(data, value)
		}
	}
	header(0, 1, 1)
	data = append(data, make([]byte, 7)...)
	header(2, 3, 13)
	data = binary.BigEndian.AppendUint64(data, uint64(100000000))
	data = binary.BigEndian.AppendUint64(data, uint64(3000000000))
	data = append(data, 1, 2)
	for _, zone := range []struct {
		offset    uint32
		dst, name byte
	}{{1800, 1, 0}, {3600, 0, 4}, {7200, 0, 8}} {
		data = binary.BigEndian.AppendUint32(data, zone.offset)
		data = append(data, zone.dst, zone.name)
	}
	data = append(data, []byte("PRE\x00MID\x00POST\x00")...)
	if recurring {
		data = append(data, []byte("\nSTD-2DST,M3.2.0,M11.1.0\n")...)
	} else {
		data = append(data, '\n', '\n')
	}
	location, err := time.LoadLocationFromTZData("Authored/Calendar", data)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestHeapCalendarPreservesFiniteHistoryAndJavaCalls(t *testing.T) {
	location := checkpointCalendarLocation(t, false)
	for _, instant := range []int64{-100000000, 200000000, 4000000000} {
		vm := New(mapClassSource{"HeapCheckpointProbe": heapCheckpointClass}, Options{})
		if _, err := vm.InvokeStatic("HeapCheckpointProbe", "prepareCalendar", "(J)V", LongValue(instant*1000)); err != nil {
			t.Fatal(err)
		}
		value, err := vm.StaticField("HeapCheckpointProbe", "calendar", "Ljava/util/Calendar;")
		if err != nil {
			t.Fatal(err)
		}
		object, _ := value.Reference()
		original := object.Native.(*calendarData)
		original.time = time.Unix(instant, 123456789).In(location)
		saved, err := vm.CaptureHeapState([]*Object{object}, HeapCodec{})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		var decoded HeapState
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		fresh := New(mapClassSource{"HeapCheckpointProbe": heapCheckpointClass}, Options{})
		roots, err := fresh.RestoreHeapState(decoded, HeapCodec{})
		if err != nil {
			t.Fatal(err)
		}
		restored := roots[0].Native.(*calendarData)
		if !restored.time.Equal(original.time) || restored.time.Location().String() != location.String() {
			t.Fatal("calendar instant or location name changed")
		}
		for _, seconds := range []int64{-100000000, 99999999, 100000000, 100000001, 2999999999, 3000000000, 3000000001, 9000000000} {
			want := time.Unix(seconds, 0).In(location)
			got := time.Unix(seconds, 0).In(restored.time.Location())
			wn, wo := want.Zone()
			gn, goff := got.Zone()
			if wn != gn || wo != goff || want.IsDST() != got.IsDST() {
				t.Fatalf("zone at %d = %q/%d/%v, want %q/%d/%v", seconds, gn, goff, got.IsDST(), wn, wo, want.IsDST())
			}
			value, err := fresh.InvokeStatic("HeapCheckpointProbe", "calendarMinute", "(J)I", LongValue(seconds*1000))
			minute, _ := value.Int32()
			if err != nil || minute != int32(want.Hour()*60+want.Minute()) {
				t.Fatalf("Java calendar minute at %d = %d, %v", seconds, minute, err)
			}
		}
		if _, err := fresh.CaptureHeapState(roots, HeapCodec{}); err != nil {
			t.Fatalf("calendar recapture: %v", err)
		}
	}
}

func TestHeapCalendarRefusesUnboundedRecurringRules(t *testing.T) {
	object := &Object{ClassName: "java/util/Calendar", Native: &calendarData{time: time.Unix(4000000000, 0).In(checkpointCalendarLocation(t, true))}}
	if _, err := New(nil, Options{}).CaptureHeapState([]*Object{object}, HeapCodec{}); err == nil {
		t.Fatal("recurring future timezone rules were silently truncated")
	}
}

func TestHeapCalendarMalformedRecordsPreserveTarget(t *testing.T) {
	vm := New(nil, Options{})
	object := &Object{ClassName: CalendarClass, Native: &calendarData{time: time.Unix(200000000, 0).In(checkpointCalendarLocation(t, false))}}
	saved, err := vm.CaptureHeapState([]*Object{object}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Payloads) != 1 {
		t.Fatal("calendar fixture has unexpected native payloads")
	}
	original := saved.Payloads[0].Data
	zoneStart := 20 + int(binary.LittleEndian.Uint32(original[12:]))
	firstTransition := zoneStart + 6 + int(original[zoneStart+5])
	secondTransition := firstTransition + 8 + 6 + int(original[firstTransition+8+5])
	var invalid [][]byte
	for n := range original {
		invalid = append(invalid, bytes.Clone(original[:n]))
	}
	for _, change := range []func([]byte){
		func(data []byte) { binary.LittleEndian.PutUint32(data[8:], 1e9) },
		func(data []byte) { binary.LittleEndian.PutUint32(data[12:], 65536) },
		func(data []byte) { binary.LittleEndian.PutUint32(data[16:], maxHeapCalendarTransitions+1) },
		func(data []byte) { data[zoneStart+4] = 2 },
		func(data []byte) { binary.LittleEndian.PutUint32(data[zoneStart:], 86401) },
		func(data []byte) {
			copy(data[secondTransition:secondTransition+8], data[firstTransition:firstTransition+8])
		},
	} {
		data := bytes.Clone(original)
		change(data)
		invalid = append(invalid, data)
	}
	invalid = append(invalid, append(bytes.Clone(original), 0))
	fresh := New(nil, Options{})
	marker := fresh.NewString("retained target")
	if err := fresh.BindAOTObject(0x1000, marker); err != nil {
		t.Fatal(err)
	}
	for _, data := range invalid {
		bad := saved
		bad.Payloads = append([]HeapPayloadState(nil), saved.Payloads...)
		bad.Payloads[0].Data = data
		if _, err := fresh.RestoreHeapState(bad, HeapCodec{}); err == nil {
			t.Fatal("malformed calendar accepted")
		}
		if got, _ := fresh.AOTObjectAt(0x1000); got != marker {
			t.Fatal("failed calendar restoration changed the target")
		}
	}
}
