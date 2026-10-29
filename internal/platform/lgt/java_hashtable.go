package lgt

import (
	"context"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/util/Hashtable`, the table a title keys its data by.
//
// What a table holds is kept on this side, keyed by the object, the way a
// Vector's elements are (java_vector.go), and **it is the class library's own
// table, bucket for bucket**: eleven buckets to start with, a key filed under
// `(hashCode & 0x7fffffff) % buckets`, a new entry at the head of its chain,
// and past three quarters full a rehash into `2n + 1` buckets that walks the
// old ones from the last. The specification's page says the load factor is
// always 75% in CLDC. With hashCode the language's own for Strings and the
// numbers (java_string_members.go, java_number.go), a table enumerates in the
// order a handset's does, and a title that walks its keys to draw a list draws
// it in that order.
//
// A key's hashCode and equals are its own, asked through its slots
// (javaCallVirtual), so a title's own key class is filed the way it says.
// The calls are made where the class library makes them: equals only between
// entries whose hashes already match, and asked of the key the table holds.
//
// Its own methods take its slots from 10 in declaration order — size,
// isEmpty, keys, elements, contains, containsKey, get, rehash, put, remove,
// clear — and toString takes Object's 4.

const (
	javaHashtableClass           = "java/util/Hashtable"
	javaHashtableDefaultCapacity = 11
	javaHashtableLoadPercent     = 75
)

// javaHashtable is one table: its buckets, each a chain from its head, and the
// count past which it rehashes.
type javaHashtable struct {
	buckets   [][]javaHashEntry
	count     int
	threshold int
}

type javaHashEntry struct {
	hash       int32
	key, value uint32
}

func newJavaHashtableState(capacity int) *javaHashtable {
	if capacity == 0 {
		capacity = 1
	}
	return &javaHashtable{
		buckets:   make([][]javaHashEntry, capacity),
		threshold: capacity * javaHashtableLoadPercent / 100,
	}
}

func (table *javaHashtable) index(hash int32) int {
	return int(uint32(hash)&0x7fffffff) % len(table.buckets)
}

// rehash is the class library's: `2n + 1` buckets, the old ones walked from
// the last, each entry put at the head of its new chain.
func (table *javaHashtable) rehash() error {
	capacity := 2*len(table.buckets) + 1
	if capacity > maxJavaArrayLength {
		return fmt.Errorf("a hashtable of %d buckets cannot grow: %d is the most an array holds",
			len(table.buckets), maxJavaArrayLength)
	}
	fresh := make([][]javaHashEntry, capacity)
	for bucket := len(table.buckets) - 1; bucket >= 0; bucket-- {
		for _, entry := range table.buckets[bucket] {
			index := int(uint32(entry.hash)&0x7fffffff) % capacity
			fresh[index] = append([]javaHashEntry{entry}, fresh[index]...)
		}
	}
	table.buckets = fresh
	table.threshold = capacity * javaHashtableLoadPercent / 100
	return nil
}

// entries is every entry in the order the class library's enumerator visits
// them: from the last bucket to the first, each chain from its head.
func (table *javaHashtable) entries() []javaHashEntry {
	all := make([]javaHashEntry, 0, table.count)
	for bucket := len(table.buckets) - 1; bucket >= 0; bucket-- {
		all = append(all, table.buckets[bucket]...)
	}
	return all
}

// find answers where the entry for a key the caller already matched sits now.
// The match is made against a copy, because equals is the title's own code and
// may change the table under it; what is changed afterwards is the entry the
// table holds at the moment of the change.
func (table *javaHashtable) find(hash int32, key uint32) (int, int, bool) {
	bucket := table.index(hash)
	for position, entry := range table.buckets[bucket] {
		if entry.hash == hash && entry.key == key {
			return bucket, position, true
		}
	}
	return 0, 0, false
}

func (client *Client) javaHashtableOf(object uint32) (*javaHashtable, error) {
	table, ok := client.javaRuntimeState().hashtables[object]
	if !ok || table == nil {
		return nil, fmt.Errorf("the object at %#x is not a hashtable this platform built", object)
	}
	return table, nil
}

// javaHashtableConstructor is `Hashtable()` and `Hashtable(int)`. A negative
// capacity is the IllegalArgumentException the class library throws, and a
// capacity of zero is one bucket.
func javaHashtableConstructor(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	capacity := int64(javaHashtableDefaultCapacity)
	if len(arguments) > 1 {
		capacity = int64(int32(arguments[1]))
	}
	if capacity < 0 {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass,
			fmt.Sprintf(": Hashtable(%d)", capacity))
	}
	if capacity > maxJavaArrayLength {
		return 0, fmt.Errorf("a hashtable of %d buckets: %d is the most an array holds", capacity, maxJavaArrayLength)
	}
	client.javaRuntimeState().hashtables[arguments[0]] = newJavaHashtableState(int(capacity))
	return 0, nil
}

// javaHashtableKeyed finds the entry a key is filed under: its own hashCode,
// then equals asked of each entry with that hash, as the class library asks
// it. A null key is the NullPointerException its hashCode would be.
func (client *Client) javaHashtableKeyed(
	ctx context.Context, thread *armcore.Thread, object, key uint32,
) (*javaHashtable, int32, uint32, bool, error) {
	if key == 0 {
		return nil, 0, 0, false, client.throwJavaPlatform(thread, javaThrowNullClass, ": a null hashtable key")
	}
	if _, err := client.javaHashtableOf(object); err != nil {
		return nil, 0, 0, false, err
	}
	hash, err := client.javaHashCodeOf(ctx, thread, key)
	if err != nil {
		return nil, 0, 0, false, err
	}
	table, err := client.javaHashtableOf(object)
	if err != nil {
		return nil, 0, 0, false, err
	}
	chain := append([]javaHashEntry(nil), table.buckets[table.index(hash)]...)
	for _, entry := range chain {
		if entry.hash != hash {
			continue
		}
		equal, err := client.javaEqualsOf(ctx, thread, entry.key, key)
		if err != nil {
			return nil, 0, 0, false, err
		}
		if equal {
			table, err = client.javaHashtableOf(object)
			return table, hash, entry.key, err == nil, err
		}
	}
	table, err = client.javaHashtableOf(object)
	return table, hash, 0, false, err
}

// javaHashtableGet is `get(Object)`, slot 16: the value, or null.
func javaHashtableGet(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, hash, key, found, err := client.javaHashtableKeyed(ctx, thread, arguments[0], arguments[1])
	if err != nil || !found {
		return 0, err
	}
	if bucket, position, ok := table.find(hash, key); ok {
		return table.buckets[bucket][position].value, nil
	}
	return 0, nil
}

// javaHashtableContainsKey is `containsKey(Object)`, slot 15.
func javaHashtableContainsKey(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, hash, key, found, err := client.javaHashtableKeyed(ctx, thread, arguments[0], arguments[1])
	if err != nil || !found {
		return javaFalse, err
	}
	_, _, ok := table.find(hash, key)
	return javaBoolean(ok), nil
}

// javaHashtablePut is `put(Object, Object)`, slot 18: the value under the key,
// answering the value it replaced or null. A key already held keeps its own
// object and takes the new value; a null key or value is the
// NullPointerException the class library throws.
func javaHashtablePut(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[2] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": a null hashtable value")
	}
	table, hash, key, found, err := client.javaHashtableKeyed(ctx, thread, arguments[0], arguments[1])
	if err != nil {
		return 0, err
	}
	if found {
		if bucket, position, ok := table.find(hash, key); ok {
			old := table.buckets[bucket][position].value
			table.buckets[bucket][position].value = arguments[2]
			return old, nil
		}
	}
	if table.count >= maxJavaArrayLength {
		return 0, fmt.Errorf("a hashtable of %d entries cannot take another: %d is the most one holds",
			table.count, maxJavaArrayLength)
	}
	if table.count >= table.threshold {
		if err := table.rehash(); err != nil {
			return 0, err
		}
	}
	bucket := table.index(hash)
	table.buckets[bucket] = append([]javaHashEntry{{hash: hash, key: arguments[1], value: arguments[2]}},
		table.buckets[bucket]...)
	table.count++
	return 0, nil
}

// javaHashtableRemove is `remove(Object)`, slot 19: the entry goes, and the
// answer is its value or null.
func javaHashtableRemove(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, hash, key, found, err := client.javaHashtableKeyed(ctx, thread, arguments[0], arguments[1])
	if err != nil || !found {
		return 0, err
	}
	bucket, position, ok := table.find(hash, key)
	if !ok {
		return 0, nil
	}
	chain := table.buckets[bucket]
	value := chain[position].value
	table.buckets[bucket] = append(chain[:position:position], chain[position+1:]...)
	table.count--
	return value, nil
}

// javaHashtableContains is `contains(Object)`, slot 14: whether any entry's
// value equals the argument, asked of each value in enumeration order. A null
// argument is the NullPointerException the class library throws.
func javaHashtableContains(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": contains(null)")
	}
	table, err := client.javaHashtableOf(arguments[0])
	if err != nil {
		return 0, err
	}
	for _, entry := range table.entries() {
		equal, err := client.javaEqualsOf(ctx, thread, entry.value, arguments[1])
		if err != nil {
			return 0, err
		}
		if equal {
			return javaTrue, nil
		}
	}
	return javaFalse, nil
}

// javaHashtableSize is `size()`, slot 10, and javaHashtableEmpty `isEmpty()`,
// slot 11.
func javaHashtableSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, err := client.javaHashtableOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(table.count), nil
}

func javaHashtableEmpty(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, err := client.javaHashtableOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return javaBoolean(table.count == 0), nil
}

// javaHashtableEnumerate is `keys()`, slot 12, and `elements()`, slot 13.
func javaHashtableEnumerate(keys bool) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		table, err := client.javaHashtableOf(arguments[0])
		if err != nil {
			return 0, err
		}
		entries := table.entries()
		items := make([]uint32, len(entries))
		for index, entry := range entries {
			items[index] = entry.value
			if keys {
				items[index] = entry.key
			}
		}
		return client.newJavaEnumeration(javaHashtableEnumeratorClass, &javaEnumeration{items: items})
	}
}

// javaHashtableRehash is `rehash()`, slot 17, which the class library declares
// protected and a subclass may call.
func javaHashtableRehash(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, err := client.javaHashtableOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return 0, table.rehash()
}

// javaHashtableClear is `clear()`, slot 20: every entry goes and the buckets
// stay.
func javaHashtableClear(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, err := client.javaHashtableOf(arguments[0])
	if err != nil {
		return 0, err
	}
	table.buckets = make([][]javaHashEntry, len(table.buckets))
	table.count = 0
	return 0, nil
}

// javaHashtableToString is `toString()`, slot 4: `{key=value, ...}` in
// enumeration order, each side its own toString, as the class library writes
// it.
func javaHashtableToString(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	table, err := client.javaHashtableOf(arguments[0])
	if err != nil {
		return 0, err
	}
	entries := table.entries()
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		key, err := client.javaToStringOf(ctx, thread, entry.key)
		if err != nil {
			return 0, err
		}
		value, err := client.javaToStringOf(ctx, thread, entry.value)
		if err != nil {
			return 0, err
		}
		parts = append(parts, key+"="+value)
	}
	return client.newJavaString("{" + strings.Join(parts, ", ") + "}")
}
