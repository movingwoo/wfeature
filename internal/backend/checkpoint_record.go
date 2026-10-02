package backend

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"unicode/utf8"
)

type checkpointRecordLimits struct {
	encoded, decoded uint64
	nodes, depth     int
}

func defaultCheckpointRecordLimits() checkpointRecordLimits {
	return checkpointRecordLimits{encoded: checkpointRuntimeLimit, decoded: 256 << 20, nodes: 4 << 20, depth: 64}
}

// Checkpoint records contain only explicit, exported struct fields, scalars,
// arrays, slices and optional pointers. Object graphs use IDs. Dynamic maps,
// interfaces, tags and custom JSON methods are deliberately outside the schema.
type checkpointRecordWalk struct {
	limits checkpointRecordLimits
	bytes  uint64
	nodes  int
	fields map[reflect.Type]map[string]int
}

func (walk *checkpointRecordWalk) charge(size uint64) error {
	if size > walk.limits.decoded-walk.bytes {
		return fmt.Errorf("checkpoint record allocation exceeds limit")
	}
	walk.bytes += size
	return nil
}

func (walk *checkpointRecordWalk) node(depth int) error {
	walk.nodes++
	if depth > walk.limits.depth || walk.nodes > walk.limits.nodes {
		return fmt.Errorf("checkpoint record depth or value count exceeds limit")
	}
	return nil
}

func (walk *checkpointRecordWalk) schema(t reflect.Type) (map[string]int, error) {
	if fields, ok := walk.fields[t]; ok {
		return fields, nil
	}
	for _, method := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[json.Unmarshaler](), reflect.TypeFor[encoding.TextMarshaler](), reflect.TypeFor[encoding.TextUnmarshaler]()} {
		if t.Implements(method) || reflect.PointerTo(t).Implements(method) {
			return nil, fmt.Errorf("checkpoint record has a custom JSON or text method")
		}
	}
	var fields map[string]int
	if t.Kind() == reflect.Struct {
		fields = make(map[string]int, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() || field.Anonymous || field.Tag.Get("json") != "" {
				return nil, fmt.Errorf("checkpoint record has an implicit or tagged field")
			}
			fields[field.Name] = i
		}
	}
	if walk.fields == nil {
		walk.fields = make(map[reflect.Type]map[string]int)
	}
	walk.fields[t] = fields
	return fields, nil
}

// EncodeCheckpointRecord bounds the encoded allocation before json.Marshal
// constructs its buffer. The conservative estimate includes escaped strings.
func EncodeCheckpointRecord(record any) ([]byte, error) {
	limits := defaultCheckpointRecordLimits()
	walk := checkpointRecordWalk{limits: limits}
	walk.limits.decoded = limits.encoded
	if err := walk.estimate(reflect.ValueOf(record), 0); err != nil {
		return nil, err
	}
	return json.Marshal(record)
}

func (walk *checkpointRecordWalk) estimate(value reflect.Value, depth int) error {
	if err := walk.node(depth); err != nil {
		return err
	}
	if !value.IsValid() {
		return fmt.Errorf("checkpoint record has no concrete type")
	}
	if _, err := walk.schema(value.Type()); err != nil {
		return err
	}
	if err := walk.charge(2); err != nil { // delimiters and separators
		return err
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return walk.charge(4)
		}
		return walk.estimate(value.Elem(), depth+1)
	case reflect.Struct:
		if _, err := walk.schema(value.Type()); err != nil {
			return err
		}
		for i := 0; i < value.NumField(); i++ {
			if err := walk.charge(uint64(len(value.Type().Field(i).Name)) + 4); err != nil {
				return err
			}
			if err := walk.estimate(value.Field(i), depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return walk.charge(4)
		}
		if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
			return walk.charge((uint64(value.Len())+2)/3*4 + 2)
		}
		if value.Len() > walk.limits.nodes-walk.nodes {
			return fmt.Errorf("checkpoint record array count exceeds limit")
		}
		for i := 0; i < value.Len(); i++ {
			if err := walk.estimate(value.Index(i), depth+1); err != nil {
				return err
			}
		}
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return fmt.Errorf("checkpoint record string is not UTF-8")
		}
		return walk.charge(uint64(value.Len())*6 + 2)
	case reflect.Bool:
		return walk.charge(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return walk.charge(32)
	default:
		return fmt.Errorf("checkpoint record has unsupported type %s", value.Type())
	}
	return nil
}

// DecodeCheckpointRecord performs a schema-aware token pass before allocating
// typed slices. A short array of empty objects must not allocate a huge backing
// array merely because each destination struct is large. Failed decoding leaves
// the destination untouched. Component validators still check semantic values.
func DecodeCheckpointRecord(data []byte, destination any) error {
	value := reflect.ValueOf(destination)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("checkpoint record destination must be a non-nil pointer")
	}
	t := value.Elem().Type()
	if err := inspectCheckpointRecord(data, t, defaultCheckpointRecordLimits()); err != nil {
		return fmt.Errorf("%w: %v", ErrCheckpointDamaged, err)
	}
	temporary := reflect.New(t)
	if err := json.Unmarshal(data, temporary.Interface()); err != nil {
		return fmt.Errorf("%w: %v", ErrCheckpointDamaged, err)
	}
	value.Elem().Set(temporary.Elem())
	return nil
}

func inspectCheckpointRecord(data []byte, t reflect.Type, limits checkpointRecordLimits) error {
	if uint64(len(data)) > limits.encoded || !utf8.Valid(data) {
		return fmt.Errorf("checkpoint record byte count or UTF-8 is invalid")
	}
	walk := checkpointRecordWalk{limits: limits}
	if err := walk.charge(uint64(t.Size())); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if err := walk.inspect(decoder, token, t, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("checkpoint record has trailing data")
	}
	return nil
}

func (walk *checkpointRecordWalk) inspect(decoder *json.Decoder, token json.Token, t reflect.Type, depth int) error {
	if err := walk.node(depth); err != nil {
		return err
	}
	if _, err := walk.schema(t); err != nil {
		return err
	}
	if t.Kind() == reflect.Pointer {
		if token == nil {
			return nil
		}
		if err := walk.charge(uint64(t.Elem().Size())); err != nil {
			return err
		}
		return walk.inspect(decoder, token, t.Elem(), depth+1)
	}
	invalid := func() error { return fmt.Errorf("checkpoint record value does not match %s", t) }
	switch t.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return invalid()
		}
		fields, err := walk.schema(t)
		if err != nil {
			return err
		}
		seen := make([]bool, t.NumField())
		count := 0
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			index, exists := fields[name]
			if !ok || !exists || seen[index] {
				return fmt.Errorf("checkpoint record has an unknown or repeated field")
			}
			seen[index], count = true, count+1
			item, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := walk.inspect(decoder, item, t.Field(index).Type, depth+1); err != nil {
				return err
			}
		}
		if count != len(fields) {
			return fmt.Errorf("checkpoint record is missing fields")
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return invalid()
		}
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && token == nil {
			return nil
		}
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			encoded, ok := token.(string)
			if !ok {
				return invalid()
			}
			return walk.charge(2 * uint64(base64.StdEncoding.DecodedLen(len(encoded))))
		}
		if token != json.Delim('[') {
			return invalid()
		}
		count := 0
		for decoder.More() {
			if t.Kind() == reflect.Slice {
				// Allow for the standard decoder's growing backing array.
				if err := walk.charge(2 * uint64(t.Elem().Size())); err != nil {
					return err
				}
			} else if count >= t.Len() {
				return invalid()
			}
			count++
			item, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := walk.inspect(decoder, item, t.Elem(), depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') || t.Kind() == reflect.Array && count != t.Len() {
			return invalid()
		}
	case reflect.String:
		value, ok := token.(string)
		if !ok {
			return invalid()
		}
		return walk.charge(2 * uint64(len(value)))
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return invalid()
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		number, ok := token.(json.Number)
		if !ok {
			return invalid()
		}
		var err error
		switch t.Kind() {
		case reflect.Float32, reflect.Float64:
			_, err = strconv.ParseFloat(string(number), t.Bits())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			_, err = strconv.ParseInt(string(number), 10, t.Bits())
		default:
			_, err = strconv.ParseUint(string(number), 10, t.Bits())
		}
		if err != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
