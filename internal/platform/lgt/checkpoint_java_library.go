package lgt

import (
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/movingwoo/wfeature/internal/backend"
)

// What the class library's core keeps on this side (java_class_library.go):
// hashtables, enumerations, the numbers, exception messages, and the
// capacities Vector and StringBuffer answer.
//
// **It is a record of its own, beside the session record rather than inside
// it.** The checkpoint codec reads a strict schema — every field present, none
// unknown — so a field added to the session or Java record would make every
// quick save an earlier build took unreadable. Version 4 wraps the version-2
// session record and the version-3 media record with this one, the way
// version 3 wrapped the session record; a record with nothing here is written
// in the shape an earlier build reads, and every earlier record still loads,
// with these tables empty, which is what a build that could not make them left.
type sessionLibraryCheckpointState struct {
	Version uint32
	Session sessionCheckpointState
	Media   *javaMediaState
	Library javaLibraryState
}

const sessionLibraryCheckpointVersion = 4

type javaLibraryState struct {
	VectorShapes []javaVectorShapeState
	// BufferCapacities is pairs of words: a StringBuffer, and its capacity.
	BufferCapacities []byte
	Numbers          []javaNumberState
	Hashtables       []javaHashtableState
	Enumerations     []javaEnumerationState
	Messages         []javaTextState
}

type javaVectorShapeState struct {
	Object              uint32
	Capacity, Increment int32
}

type javaNumberState struct {
	Object uint32
	Value  int64
}

// javaHashtableState is one table: how many buckets it has, the count past
// which it rehashes, and every entry with the bucket it is filed in, each
// bucket's chain from its head.
type javaHashtableState struct {
	Object    uint32
	Buckets   uint32
	Threshold int
	Entries   []javaHashEntryState
}

type javaHashEntryState struct {
	Bucket     uint32
	Hash       int32
	Key, Value uint32
}

// javaEnumerationState is one enumeration: the vector it walks, or the items it
// was given, and how far it has got.
type javaEnumerationState struct {
	Object, Vector uint32
	Items          []byte
	Next           int
}

// captureJavaLibraryState records the tables, or answers nil when every one is
// empty, which is what keeps such a session's record in the earlier shape.
func (client *Client) captureJavaLibraryState() (*javaLibraryState, error) {
	runtime := client.javaRun
	if runtime == nil {
		return nil, nil
	}
	budget := &stateBudget{}
	saved := &javaLibraryState{}
	for _, object := range slices.Sorted(maps.Keys(runtime.vectorShapes)) {
		shape := runtime.vectorShapes[object]
		saved.VectorShapes = append(saved.VectorShapes, javaVectorShapeState{object, shape.capacity, shape.increment})
	}
	capacities := make(map[uint32]uint32, len(runtime.bufferCapacities))
	for object, capacity := range runtime.bufferCapacities {
		capacities[object] = uint32(capacity)
	}
	saved.BufferCapacities = packPairs(capacities)
	for _, object := range slices.Sorted(maps.Keys(runtime.numbers)) {
		saved.Numbers = append(saved.Numbers, javaNumberState{object, runtime.numbers[object]})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.hashtables)) {
		table := runtime.hashtables[object]
		if table == nil || len(table.buckets) == 0 {
			return nil, fmt.Errorf("LGT checkpoint has a malformed hashtable")
		}
		if err := budget.charge(16 * table.count); err != nil {
			return nil, err
		}
		record := javaHashtableState{Object: object, Buckets: uint32(len(table.buckets)), Threshold: table.threshold}
		for bucket, chain := range table.buckets {
			for _, entry := range chain {
				record.Entries = append(record.Entries, javaHashEntryState{uint32(bucket), entry.hash, entry.key, entry.value})
			}
		}
		saved.Hashtables = append(saved.Hashtables, record)
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.enumerations)) {
		enumeration := runtime.enumerations[object]
		if enumeration == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing enumeration")
		}
		if err := budget.charge(4 * len(enumeration.items)); err != nil {
			return nil, err
		}
		saved.Enumerations = append(saved.Enumerations, javaEnumerationState{
			Object: object, Vector: enumeration.vector, Items: packWords(enumeration.items), Next: enumeration.next,
		})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.messages)) {
		message := runtime.messages[object]
		if err := budget.charge(len(message)); err != nil {
			return nil, err
		}
		saved.Messages = append(saved.Messages, javaTextState{object, []byte(message)})
	}
	if len(saved.VectorShapes) == 0 && len(saved.BufferCapacities) == 0 && len(saved.Numbers) == 0 &&
		len(saved.Hashtables) == 0 && len(saved.Enumerations) == 0 && len(saved.Messages) == 0 {
		return nil, nil
	}
	if err := saved.validate(); err != nil {
		return nil, err
	}
	return saved, nil
}

// validate checks a record's tables before any of it is restored.
func (saved *javaLibraryState) validate() error {
	invalid := func(what string) error { return fmt.Errorf("LGT checkpoint class library has %s", what) }
	for _, count := range []int{len(saved.VectorShapes), len(saved.Numbers), len(saved.Hashtables),
		len(saved.Enumerations), len(saved.Messages)} {
		if count > maxStateRecords {
			return invalid("a table past its limit")
		}
	}
	for index, shape := range saved.VectorShapes {
		if index > 0 && saved.VectorShapes[index-1].Object >= shape.Object ||
			shape.Capacity < 0 || shape.Capacity > maxJavaArrayLength {
			return invalid("an invalid vector shape")
		}
	}
	for index, number := range saved.Numbers {
		if index > 0 && saved.Numbers[index-1].Object >= number.Object {
			return invalid("numbers out of order")
		}
	}
	for index, table := range saved.Hashtables {
		// A table is restored bucket for bucket, so every entry has to sit in
		// the bucket its hash files it under, and the counts have to be ones
		// the table itself could have reached.
		if index > 0 && saved.Hashtables[index-1].Object >= table.Object || table.Buckets == 0 ||
			table.Buckets > maxJavaArrayLength || len(table.Entries) > maxJavaArrayLength ||
			table.Threshold < 0 || table.Threshold > int(table.Buckets) {
			return invalid("an invalid hashtable")
		}
		for _, entry := range table.Entries {
			if entry.Bucket >= table.Buckets || (uint32(entry.Hash)&0x7fffffff)%table.Buckets != entry.Bucket {
				return invalid("a hashtable entry outside its bucket")
			}
		}
	}
	for index, enumeration := range saved.Enumerations {
		if index > 0 && saved.Enumerations[index-1].Object >= enumeration.Object || len(enumeration.Items)%4 != 0 ||
			len(enumeration.Items)/4 > maxJavaArrayLength || enumeration.Vector != 0 && len(enumeration.Items) != 0 ||
			enumeration.Next < 0 || enumeration.Vector == 0 && enumeration.Next > len(enumeration.Items)/4 {
			return invalid("an invalid enumeration")
		}
	}
	for index, message := range saved.Messages {
		if index > 0 && saved.Messages[index-1].Object >= message.Object {
			return invalid("messages out of order")
		}
	}
	return nil
}

// restoreJavaLibraryState fills the tables of a restored runtime. A record
// without these tables restores none, and so does one from a Clet.
func (client *Client) restoreJavaLibraryState(saved *javaLibraryState) error {
	if saved == nil {
		return nil
	}
	if err := saved.validate(); err != nil {
		return err
	}
	runtime := client.javaRun
	if runtime == nil {
		return fmt.Errorf("LGT checkpoint has class library tables and no Java runtime")
	}
	capacities, err := unpackPairs(saved.BufferCapacities, "buffer capacity")
	if err != nil {
		return err
	}
	for object, capacity := range capacities {
		if capacity > math.MaxInt32 {
			return fmt.Errorf("LGT checkpoint buffer capacity table is malformed")
		}
		runtime.bufferCapacities[object] = int32(capacity)
	}
	for _, record := range saved.VectorShapes {
		runtime.vectorShapes[record.Object] = javaVectorShape{capacity: record.Capacity, increment: record.Increment}
	}
	for _, record := range saved.Numbers {
		runtime.numbers[record.Object] = record.Value
	}
	for _, record := range saved.Hashtables {
		table := &javaHashtable{buckets: make([][]javaHashEntry, record.Buckets), threshold: record.Threshold}
		for _, entry := range record.Entries {
			table.buckets[entry.Bucket] = append(table.buckets[entry.Bucket], javaHashEntry{entry.Hash, entry.Key, entry.Value})
		}
		table.count = len(record.Entries)
		runtime.hashtables[record.Object] = table
	}
	for _, record := range saved.Enumerations {
		enumeration := &javaEnumeration{vector: record.Vector, next: record.Next}
		if len(record.Items) != 0 {
			enumeration.items = unpackWords(record.Items)
		}
		runtime.enumerations[record.Object] = enumeration
	}
	for _, record := range saved.Messages {
		runtime.messages[record.Object] = string(record.Text)
	}
	return nil
}

// encodeLGTSessionRecord writes a session record in the oldest shape that holds
// it: the session record alone, the media wrapper, or this one.
func encodeLGTSessionRecord(saved sessionCheckpointState, media *javaMediaState, library *javaLibraryState) ([]byte, error) {
	switch {
	case library != nil:
		return backend.EncodeCheckpointRecord(sessionLibraryCheckpointState{
			Version: sessionLibraryCheckpointVersion, Session: saved, Media: media, Library: *library,
		})
	case media != nil:
		return backend.EncodeCheckpointRecord(sessionMediaCheckpointState{
			Version: sessionMediaCheckpointVersion, Session: saved, Media: *media,
		})
	}
	return backend.EncodeCheckpointRecord(saved)
}
