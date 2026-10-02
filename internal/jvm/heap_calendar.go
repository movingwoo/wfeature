package jvm

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"time"
)

const maxHeapCalendarTransitions = 1024
const maxHeapCalendarBytes = 1 << 20

type heapCalendarZone struct {
	name   string
	offset int32
	dst    bool
}
type heapCalendarTransition struct {
	at   int64
	zone heapCalendarZone
}

func calendarZone(at time.Time) (heapCalendarZone, error) {
	name, offset := at.Zone()
	zone := heapCalendarZone{name: name, offset: int32(offset), dst: at.IsDST()}
	if offset < -86400 || offset > 86400 || len(name) > 255 || strings.ContainsRune(name, 0) {
		return heapCalendarZone{}, fmt.Errorf("JVM calendar zone exceeds limits")
	}
	return zone, nil
}

// Walk to both unbounded ends. A finite prefix of a recurring DST rule is not
// an equivalent location, so exceeding the transition bound refuses capture.
func captureHeapCalendar(at time.Time) ([]byte, error) {
	name := at.Location().String()
	if len(name) > 65535 {
		return nil, fmt.Errorf("JVM calendar location name exceeds limit")
	}
	var transitions []heapCalendarTransition
	appendTransition := func(seconds int64, zone heapCalendarZone) error {
		if len(transitions) == maxHeapCalendarTransitions {
			return fmt.Errorf("JVM calendar timezone has recurring rules or too many transitions")
		}
		transitions = append(transitions, heapCalendarTransition{at: seconds, zone: zone})
		return nil
	}
	cursor := at
	for {
		start, _ := cursor.ZoneBounds()
		if start == (time.Time{}) {
			break
		}
		seconds := start.Unix()
		if seconds == -1<<63 || len(transitions) != 0 && seconds >= transitions[len(transitions)-1].at {
			return nil, fmt.Errorf("JVM calendar timezone bounds do not move backwards")
		}
		zone, err := calendarZone(cursor)
		if err != nil {
			return nil, err
		}
		if err := appendTransition(seconds, zone); err != nil {
			return nil, err
		}
		cursor = time.Unix(seconds-1, 0).In(at.Location())
	}
	initial, err := calendarZone(cursor)
	if err != nil {
		return nil, err
	}
	slices.Reverse(transitions)
	cursor = at
	for {
		_, end := cursor.ZoneBounds()
		if end == (time.Time{}) {
			break
		}
		seconds := end.Unix()
		if seconds <= cursor.Unix() || len(transitions) != 0 && seconds <= transitions[len(transitions)-1].at {
			return nil, fmt.Errorf("JVM calendar timezone bounds do not move forwards")
		}
		cursor = time.Unix(seconds, 0).In(at.Location())
		zone, err := calendarZone(cursor)
		if err != nil {
			return nil, err
		}
		if err := appendTransition(seconds, zone); err != nil {
			return nil, err
		}
	}
	// Validate the exact location representation before emitting a record.
	if _, err := heapCalendarLocation(name, initial, transitions); err != nil {
		return nil, err
	}
	data := binary.LittleEndian.AppendUint64(nil, uint64(at.Unix()))
	data = binary.LittleEndian.AppendUint32(data, uint32(at.Nanosecond()))
	data = binary.LittleEndian.AppendUint32(data, uint32(len(name)))
	data = binary.LittleEndian.AppendUint32(data, uint32(len(transitions)))
	data = append(data, name...)
	appendZone := func(zone heapCalendarZone) {
		data = binary.LittleEndian.AppendUint32(data, uint32(zone.offset))
		flag := byte(0)
		if zone.dst {
			flag = 1
		}
		data = append(data, flag, byte(len(zone.name)))
		data = append(data, zone.name...)
	}
	appendZone(initial)
	for _, transition := range transitions {
		data = binary.LittleEndian.AppendUint64(data, uint64(transition.at))
		appendZone(transition.zone)
	}
	return data, nil
}

func restoreHeapCalendar(data []byte) (*calendarData, error) {
	if len(data) < 26 || len(data) > maxHeapCalendarBytes {
		return nil, fmt.Errorf("JVM calendar payload size is invalid")
	}
	seconds := int64(binary.LittleEndian.Uint64(data))
	nanos := binary.LittleEndian.Uint32(data[8:])
	nameLength, count := binary.LittleEndian.Uint32(data[12:]), binary.LittleEndian.Uint32(data[16:])
	if nanos >= 1e9 || nameLength > 65535 || uint64(nameLength) > uint64(len(data)-20) || count > maxHeapCalendarTransitions {
		return nil, fmt.Errorf("JVM calendar header is invalid")
	}
	name := string(data[20 : 20+nameLength])
	data = data[20+nameLength:]
	readZone := func() (heapCalendarZone, error) {
		if len(data) < 6 || int(data[5]) > len(data)-6 || data[4] > 1 {
			return heapCalendarZone{}, fmt.Errorf("JVM calendar zone header is invalid")
		}
		zone := heapCalendarZone{offset: int32(binary.LittleEndian.Uint32(data)), dst: data[4] != 0, name: string(data[6 : 6+int(data[5])])}
		if zone.offset < -86400 || zone.offset > 86400 || strings.ContainsRune(zone.name, 0) {
			return heapCalendarZone{}, fmt.Errorf("JVM calendar zone is invalid")
		}
		data = data[6+int(data[5]):]
		return zone, nil
	}
	initial, err := readZone()
	if err != nil {
		return nil, err
	}
	if uint64(count)*14 > uint64(len(data)) {
		return nil, fmt.Errorf("JVM calendar transition data is truncated")
	}
	transitions := make([]heapCalendarTransition, 0, int(count))
	for i := uint32(0); i < count; i++ {
		if len(data) < 8 {
			return nil, fmt.Errorf("JVM calendar transition time is truncated")
		}
		at := int64(binary.LittleEndian.Uint64(data))
		data = data[8:]
		if at == -1<<63 || i > 0 && at <= transitions[i-1].at {
			return nil, fmt.Errorf("JVM calendar transitions are not strictly ordered")
		}
		zone, err := readZone()
		if err != nil {
			return nil, err
		}
		transitions = append(transitions, heapCalendarTransition{at: at, zone: zone})
	}
	if len(data) != 0 {
		return nil, fmt.Errorf("JVM calendar payload has trailing bytes")
	}
	location, err := heapCalendarLocation(name, initial, transitions)
	if err != nil {
		return nil, err
	}
	return &calendarData{time: time.Unix(seconds, int64(nanos)).In(location)}, nil
}

// Encode captured facts as TZif v2 for Go's public location constructor. This
// internal representation has no POSIX recurrence footer: Go extends the last
// explicit zone indefinitely, as required by the captured unbounded interval.
// No timezone database or implementation source is bundled.
func heapCalendarLocation(name string, initial heapCalendarZone, transitions []heapCalendarTransition) (*time.Location, error) {
	if len(transitions) == 0 && !initial.dst {
		// FixedZone names both the location and the zone; use TZif when they differ.
		if name == initial.name {
			return time.FixedZone(name, int(initial.offset)), nil
		}
	}
	// Reserve an unused first type for the initial zone. Go's pre-transition
	// selection then keeps that type even when the initial zone is marked DST.
	zones := []heapCalendarZone{initial}
	indices := make([]byte, len(transitions))
	zoneIDs := make(map[heapCalendarZone]byte)
	for i, transition := range transitions {
		id, exists := zoneIDs[transition.zone]
		if !exists {
			if len(zones) == 256 {
				return nil, fmt.Errorf("JVM calendar zone type count exceeds limit")
			}
			id = byte(len(zones))
			zoneIDs[transition.zone] = id
			zones = append(zones, transition.zone)
		}
		indices[i] = id
	}
	var names, zoneData []byte
	abbreviations := make(map[string]byte)
	for _, zone := range zones {
		index, exists := abbreviations[zone.name]
		if !exists {
			if len(names)+len(zone.name)+1 > 256 {
				return nil, fmt.Errorf("JVM calendar abbreviations exceed limit")
			}
			index = byte(len(names))
			abbreviations[zone.name] = index
			names = append(names, zone.name...)
			names = append(names, 0)
		}
		zoneData = binary.BigEndian.AppendUint32(zoneData, uint32(zone.offset))
		flag := byte(0)
		if zone.dst {
			flag = 1
		}
		zoneData = append(zoneData, flag, index)
	}
	var tzif []byte
	header := func(transitionCount, zoneCount, nameBytes uint32) {
		tzif = append(tzif, 'T', 'Z', 'i', 'f', '2')
		tzif = append(tzif, make([]byte, 15)...)
		for _, value := range []uint32{0, 0, 0, transitionCount, zoneCount, nameBytes} {
			tzif = binary.BigEndian.AppendUint32(tzif, value)
		}
	}
	// A minimal 32-bit block precedes the complete 64-bit block.
	header(0, 1, 1)
	tzif = append(tzif, make([]byte, 7)...)
	header(uint32(len(transitions)), uint32(len(zones)), uint32(len(names)))
	for _, transition := range transitions {
		tzif = binary.BigEndian.AppendUint64(tzif, uint64(transition.at))
	}
	tzif = append(tzif, indices...)
	tzif = append(tzif, zoneData...)
	tzif = append(tzif, names...)
	tzif = append(tzif, '\n', '\n')
	return time.LoadLocationFromTZData(name, tzif)
}
