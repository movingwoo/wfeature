package classfile

import (
	"fmt"
	"math"
)

const (
	ConstantUTF8               uint8 = 1
	ConstantInteger            uint8 = 3
	ConstantFloat              uint8 = 4
	ConstantLong               uint8 = 5
	ConstantDouble             uint8 = 6
	ConstantClass              uint8 = 7
	ConstantString             uint8 = 8
	ConstantFieldRef           uint8 = 9
	ConstantMethodRef          uint8 = 10
	ConstantInterfaceMethodRef uint8 = 11
	ConstantNameAndType        uint8 = 12
	ConstantMethodHandle       uint8 = 15
	ConstantMethodType         uint8 = 16
	ConstantDynamic            uint8 = 17
	ConstantInvokeDynamic      uint8 = 18
	ConstantModule             uint8 = 19
	ConstantPackage            uint8 = 20
)

type Constant struct {
	Tag     uint8
	UTF8    string
	Integer int32
	Float   float32
	Long    int64
	Double  float64
	Index1  uint16
	Index2  uint16
}

type ConstantPool []Constant

type ReferenceKind uint8

const (
	FieldReference ReferenceKind = iota + 1
	MethodReference
	InterfaceMethodReference
)

type Reference struct {
	Kind       ReferenceKind
	Class      string
	Name       string
	Descriptor string
}

func parseConstantPool(r *reader) (ConstantPool, error) {
	count, err := r.u2()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, invalid(r.offset-2, "constant_pool_count is zero")
	}

	pool := make(ConstantPool, int(count))
	for index := 1; index < int(count); index++ {
		tagOffset := r.offset
		tag, err := r.u1()
		if err != nil {
			return nil, err
		}
		constant := Constant{Tag: tag}

		switch tag {
		case ConstantUTF8:
			length, err := r.u2()
			if err != nil {
				return nil, err
			}
			data, err := r.take(int(length))
			if err != nil {
				return nil, err
			}
			constant.UTF8, err = decodeModifiedUTF8(data)
			if err != nil {
				return nil, invalid(tagOffset, "%v", err)
			}
		case ConstantInteger:
			value, err := r.u4()
			if err != nil {
				return nil, err
			}
			constant.Integer = int32(value)
		case ConstantFloat:
			value, err := r.u4()
			if err != nil {
				return nil, err
			}
			constant.Float = math.Float32frombits(value)
		case ConstantLong:
			if index+1 >= int(count) {
				return nil, invalid(tagOffset, "long constant has no reserved slot")
			}
			value, err := r.u8()
			if err != nil {
				return nil, err
			}
			constant.Long = int64(value)
			pool[index] = constant
			index++
			continue
		case ConstantDouble:
			if index+1 >= int(count) {
				return nil, invalid(tagOffset, "double constant has no reserved slot")
			}
			value, err := r.u8()
			if err != nil {
				return nil, err
			}
			constant.Double = math.Float64frombits(value)
			pool[index] = constant
			index++
			continue
		case ConstantClass, ConstantString, ConstantMethodType, ConstantModule, ConstantPackage:
			constant.Index1, err = r.u2()
			if err != nil {
				return nil, err
			}
		case ConstantFieldRef, ConstantMethodRef, ConstantInterfaceMethodRef,
			ConstantNameAndType, ConstantDynamic, ConstantInvokeDynamic:
			constant.Index1, err = r.u2()
			if err != nil {
				return nil, err
			}
			constant.Index2, err = r.u2()
			if err != nil {
				return nil, err
			}
		case ConstantMethodHandle:
			kind, err := r.u1()
			if err != nil {
				return nil, err
			}
			constant.Index1 = uint16(kind)
			constant.Index2, err = r.u2()
			if err != nil {
				return nil, err
			}
		default:
			return nil, invalid(tagOffset, "unknown constant pool tag %d", tag)
		}

		pool[index] = constant
	}

	if err := pool.validate(); err != nil {
		return nil, err
	}
	return pool, nil
}

func (p ConstantPool) constant(index uint16, tags ...uint8) (Constant, error) {
	entry, err := p.at(index)
	if err != nil {
		return Constant{}, err
	}
	for _, tag := range tags {
		if entry.Tag == tag {
			return *entry, nil
		}
	}
	return Constant{}, wrongTag(index, entry.Tag)
}

// at finds a constant without copying it, which is the difference between
// reading the pool and reading it in a loop.
//
// **The accessors below are on the interpreter's hottest path**: every invoke
// instruction decodes its own operand out of the pool again, and a profile of a
// guest call loop put a quarter of the run in here. `constant` returned the
// entry by value — fifty-six bytes — and took its accepted tags as a variadic,
// so one `ReferenceAt` copied the struct five times and built five tag slices
// to look at four strings that were already in the pool. This returns a
// pointer into a pool that nothing writes to after it is parsed, and the
// accessors that matter check their own tag inline.
func (p ConstantPool) at(index uint16) (*Constant, error) {
	if index == 0 || int(index) >= len(p) || p[index].Tag == 0 {
		return nil, fmt.Errorf("constant pool index %d is invalid", index)
	}
	return &p[index], nil
}

func wrongTag(index uint16, tag uint8) error {
	return fmt.Errorf("constant pool index %d has tag %d", index, tag)
}

func (p ConstantPool) At(index uint16) (Constant, error) {
	return p.constant(index,
		ConstantUTF8,
		ConstantInteger,
		ConstantFloat,
		ConstantLong,
		ConstantDouble,
		ConstantClass,
		ConstantString,
		ConstantFieldRef,
		ConstantMethodRef,
		ConstantInterfaceMethodRef,
		ConstantNameAndType,
		ConstantMethodHandle,
		ConstantMethodType,
		ConstantDynamic,
		ConstantInvokeDynamic,
		ConstantModule,
		ConstantPackage,
	)
}

func (p ConstantPool) UTF8At(index uint16) (string, error) {
	entry, err := p.at(index)
	if err != nil {
		return "", err
	}
	if entry.Tag != ConstantUTF8 {
		return "", wrongTag(index, entry.Tag)
	}
	return entry.UTF8, nil
}

func (p ConstantPool) ClassName(index uint16) (string, error) {
	entry, err := p.at(index)
	if err != nil {
		return "", err
	}
	if entry.Tag != ConstantClass {
		return "", wrongTag(index, entry.Tag)
	}
	return p.UTF8At(entry.Index1)
}

func (p ConstantPool) ReferenceAt(index uint16) (Reference, error) {
	entry, err := p.at(index)
	if err != nil {
		return Reference{}, err
	}
	var kind ReferenceKind
	switch entry.Tag {
	case ConstantFieldRef:
		kind = FieldReference
	case ConstantMethodRef:
		kind = MethodReference
	case ConstantInterfaceMethodRef:
		kind = InterfaceMethodReference
	default:
		return Reference{}, wrongTag(index, entry.Tag)
	}
	className, err := p.ClassName(entry.Index1)
	if err != nil {
		return Reference{}, err
	}
	nameAndType, err := p.at(entry.Index2)
	if err != nil {
		return Reference{}, err
	}
	if nameAndType.Tag != ConstantNameAndType {
		return Reference{}, wrongTag(entry.Index2, nameAndType.Tag)
	}
	name, err := p.UTF8At(nameAndType.Index1)
	if err != nil {
		return Reference{}, err
	}
	descriptor, err := p.UTF8At(nameAndType.Index2)
	if err != nil {
		return Reference{}, err
	}
	return Reference{Kind: kind, Class: className, Name: name, Descriptor: descriptor}, nil
}

func (p ConstantPool) validate() error {
	for index := 1; index < len(p); index++ {
		constant := p[index]
		var err error
		switch constant.Tag {
		case 0, ConstantUTF8, ConstantInteger, ConstantFloat, ConstantLong, ConstantDouble:
			continue
		case ConstantClass, ConstantString, ConstantMethodType, ConstantModule, ConstantPackage:
			_, err = p.constant(constant.Index1, ConstantUTF8)
		case ConstantFieldRef, ConstantMethodRef, ConstantInterfaceMethodRef:
			_, err = p.constant(constant.Index1, ConstantClass)
			if err == nil {
				_, err = p.constant(constant.Index2, ConstantNameAndType)
			}
		case ConstantNameAndType:
			_, err = p.constant(constant.Index1, ConstantUTF8)
			if err == nil {
				_, err = p.constant(constant.Index2, ConstantUTF8)
			}
		case ConstantMethodHandle:
			if constant.Index1 < 1 || constant.Index1 > 9 {
				err = fmt.Errorf("invalid method handle kind %d", constant.Index1)
			} else {
				_, err = p.constant(constant.Index2, ConstantFieldRef, ConstantMethodRef, ConstantInterfaceMethodRef)
			}
		case ConstantDynamic, ConstantInvokeDynamic:
			_, err = p.constant(constant.Index2, ConstantNameAndType)
		default:
			err = fmt.Errorf("unknown tag %d", constant.Tag)
		}
		if err != nil {
			return fmt.Errorf("%w: constant pool entry %d: %v", ErrInvalidFormat, index, err)
		}
	}
	return nil
}
