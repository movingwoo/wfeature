package jvm

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type TypeKind uint8

const (
	TypeVoid TypeKind = iota
	TypeBoolean
	TypeByte
	TypeChar
	TypeShort
	TypeInt
	TypeLong
	TypeFloat
	TypeDouble
	TypeReference
	TypeArray
)

type Type struct {
	Kind      TypeKind
	ClassName string
	Component *Type
}

func (t Type) Slots() int {
	if t.Kind == TypeVoid {
		return 0
	}
	if t.Kind == TypeLong || t.Kind == TypeDouble {
		return 2
	}
	return 1
}

func (t Type) IsReference() bool {
	return t.Kind == TypeReference || t.Kind == TypeArray
}

func (t Type) Descriptor() string {
	switch t.Kind {
	case TypeVoid:
		return "V"
	case TypeBoolean:
		return "Z"
	case TypeByte:
		return "B"
	case TypeChar:
		return "C"
	case TypeShort:
		return "S"
	case TypeInt:
		return "I"
	case TypeLong:
		return "J"
	case TypeFloat:
		return "F"
	case TypeDouble:
		return "D"
	case TypeReference:
		return "L" + t.ClassName + ";"
	case TypeArray:
		if t.Component == nil {
			return "[?"
		}
		return "[" + t.Component.Descriptor()
	default:
		return "?"
	}
}

type MethodDescriptor struct {
	Parameters []Type
	Return     Type
}

func ParseFieldDescriptor(descriptor string) (Type, error) {
	offset := 0
	typeInfo, err := parseType(descriptor, &offset, false)
	if err != nil {
		return Type{}, err
	}
	if offset != len(descriptor) {
		return Type{}, fmt.Errorf("invalid field descriptor %q: trailing input", descriptor)
	}
	return typeInfo, nil
}

// parsedDescriptors memoizes method descriptor parses. Every guest call
// through the AOT and native boundaries parses its own descriptor, and the
// same few hundred strings repeat for the life of a session, so the parse and
// its parameter slice are done once per distinct descriptor instead of once
// per call.
var (
	parsedDescriptors      sync.Map // descriptor string -> parsedDescriptor
	parsedDescriptorsCount atomic.Int64
)

// parsedDescriptorLimit bounds the memo. Descriptors can be read out of guest
// memory, so their distinct count is not bounded by the class files alone; past
// the limit parsing simply happens per call again rather than growing the map.
const parsedDescriptorLimit = 16384

type parsedDescriptor struct {
	result MethodDescriptor
	err    error
}

func ParseMethodDescriptor(descriptor string) (MethodDescriptor, error) {
	if cached, ok := parsedDescriptors.Load(descriptor); ok {
		entry := cached.(parsedDescriptor)
		return entry.result, entry.err
	}
	result, err := parseMethodDescriptor(descriptor)
	// Every later caller receives this same slice, so its spare capacity is
	// clipped: a caller that appends to it then copies instead of writing into
	// what the next caller will read.
	result.Parameters = result.Parameters[:len(result.Parameters):len(result.Parameters)]
	if parsedDescriptorsCount.Load() < parsedDescriptorLimit {
		if _, loaded := parsedDescriptors.LoadOrStore(descriptor, parsedDescriptor{result: result, err: err}); !loaded {
			parsedDescriptorsCount.Add(1)
		}
	}
	return result, err
}

func parseMethodDescriptor(descriptor string) (MethodDescriptor, error) {
	if len(descriptor) == 0 || descriptor[0] != '(' {
		return MethodDescriptor{}, fmt.Errorf("invalid method descriptor %q", descriptor)
	}
	offset := 1
	result := MethodDescriptor{}
	parameterSlots := 0
	for offset < len(descriptor) && descriptor[offset] != ')' {
		parameter, err := parseType(descriptor, &offset, false)
		if err != nil {
			return MethodDescriptor{}, fmt.Errorf("invalid method descriptor %q: %w", descriptor, err)
		}
		parameterSlots += parameter.Slots()
		if parameterSlots > 255 {
			return MethodDescriptor{}, fmt.Errorf("invalid method descriptor %q: more than 255 parameter slots", descriptor)
		}
		result.Parameters = append(result.Parameters, parameter)
	}
	if offset >= len(descriptor) || descriptor[offset] != ')' {
		return MethodDescriptor{}, fmt.Errorf("invalid method descriptor %q: missing )", descriptor)
	}
	offset++
	returnType, err := parseType(descriptor, &offset, true)
	if err != nil {
		return MethodDescriptor{}, fmt.Errorf("invalid method descriptor %q: %w", descriptor, err)
	}
	if offset != len(descriptor) {
		return MethodDescriptor{}, fmt.Errorf("invalid method descriptor %q: trailing input", descriptor)
	}
	result.Return = returnType
	return result, nil
}

func parseType(descriptor string, offset *int, allowVoid bool) (Type, error) {
	if *offset >= len(descriptor) {
		return Type{}, fmt.Errorf("missing type")
	}
	tag := descriptor[*offset]
	*offset++
	switch tag {
	case 'V':
		if !allowVoid {
			return Type{}, fmt.Errorf("void is not allowed here")
		}
		return Type{Kind: TypeVoid}, nil
	case 'Z':
		return Type{Kind: TypeBoolean}, nil
	case 'B':
		return Type{Kind: TypeByte}, nil
	case 'C':
		return Type{Kind: TypeChar}, nil
	case 'S':
		return Type{Kind: TypeShort}, nil
	case 'I':
		return Type{Kind: TypeInt}, nil
	case 'J':
		return Type{Kind: TypeLong}, nil
	case 'F':
		return Type{Kind: TypeFloat}, nil
	case 'D':
		return Type{Kind: TypeDouble}, nil
	case 'L':
		start := *offset
		for *offset < len(descriptor) && descriptor[*offset] != ';' {
			*offset++
		}
		if *offset >= len(descriptor) || *offset == start {
			return Type{}, fmt.Errorf("unterminated or empty reference type")
		}
		name := descriptor[start:*offset]
		*offset++
		return Type{Kind: TypeReference, ClassName: name}, nil
	case '[':
		component, err := parseType(descriptor, offset, false)
		if err != nil {
			return Type{}, fmt.Errorf("invalid array component: %w", err)
		}
		return Type{Kind: TypeArray, Component: &component}, nil
	default:
		return Type{}, fmt.Errorf("unknown type tag %q", tag)
	}
}
