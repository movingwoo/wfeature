package classfile

import "fmt"

const MaxSupportedMajorVersion uint16 = 70

type Class struct {
	MinorVersion uint16
	MajorVersion uint16
	ConstantPool ConstantPool
	AccessFlags  uint16
	Name         string
	SuperName    string
	Interfaces   []string
	Fields       []Member
	Methods      []Member
	Attributes   []Attribute
}

type Member struct {
	AccessFlags uint16
	Name        string
	Descriptor  string
	Attributes  []Attribute
}

type Attribute struct {
	Name string
	Info []byte
	Code *Code
}

type Code struct {
	MaxStack   uint16
	MaxLocals  uint16
	Bytecode   []byte
	Exceptions []ExceptionHandler
	Attributes []Attribute
}

type ExceptionHandler struct {
	StartPC   uint16
	EndPC     uint16
	HandlerPC uint16
	CatchType string
}

// FindField locates one field this class declares itself. Resolution through
// the superclass chain is the VM's, because only it can load the next class.
func (c *Class) FindField(name, descriptor string) *Member {
	for index := range c.Fields {
		field := &c.Fields[index]
		if field.Name == name && field.Descriptor == descriptor {
			return field
		}
	}
	return nil
}

func (c *Class) FindMethod(name, descriptor string) *Member {
	for index := range c.Methods {
		method := &c.Methods[index]
		if method.Name == name && method.Descriptor == descriptor {
			return method
		}
	}
	return nil
}

func (m *Member) CodeAttribute() *Code {
	for _, attribute := range m.Attributes {
		if attribute.Code != nil {
			return attribute.Code
		}
	}
	return nil
}

func Parse(data []byte) (*Class, error) {
	r := newReader(data)
	magic, err := r.u4()
	if err != nil {
		return nil, err
	}
	if magic != 0xcafebabe {
		return nil, invalid(0, "bad magic 0x%08x", magic)
	}

	class := &Class{}
	if class.MinorVersion, err = r.u2(); err != nil {
		return nil, err
	}
	if class.MajorVersion, err = r.u2(); err != nil {
		return nil, err
	}
	if class.MajorVersion < 45 {
		return nil, invalid(6, "class version %d is below 45", class.MajorVersion)
	}
	if class.MajorVersion > MaxSupportedMajorVersion {
		return nil, &UnsupportedVersionError{Major: class.MajorVersion}
	}

	if class.ConstantPool, err = parseConstantPool(r); err != nil {
		return nil, err
	}
	if class.AccessFlags, err = r.u2(); err != nil {
		return nil, err
	}
	thisClass, err := r.u2()
	if err != nil {
		return nil, err
	}
	if class.Name, err = class.ConstantPool.ClassName(thisClass); err != nil {
		return nil, invalid(r.offset-2, "invalid this_class: %v", err)
	}
	superClass, err := r.u2()
	if err != nil {
		return nil, err
	}
	if superClass != 0 {
		if class.SuperName, err = class.ConstantPool.ClassName(superClass); err != nil {
			return nil, invalid(r.offset-2, "invalid super_class: %v", err)
		}
	}

	if class.Interfaces, err = parseInterfaces(r, class.ConstantPool); err != nil {
		return nil, err
	}
	if class.Fields, err = parseMembers(r, class.ConstantPool, false); err != nil {
		return nil, err
	}
	if class.Methods, err = parseMembers(r, class.ConstantPool, true); err != nil {
		return nil, err
	}
	if class.Attributes, err = parseAttributes(r, class.ConstantPool, false); err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, invalid(r.offset, "%d trailing bytes", r.remaining())
	}
	return class, nil
}

func parseInterfaces(r *reader, pool ConstantPool) ([]string, error) {
	count, err := r.u2()
	if err != nil {
		return nil, err
	}
	interfaces := make([]string, 0, count)
	for range count {
		index, err := r.u2()
		if err != nil {
			return nil, err
		}
		name, err := pool.ClassName(index)
		if err != nil {
			return nil, invalid(r.offset-2, "invalid interface: %v", err)
		}
		interfaces = append(interfaces, name)
	}
	return interfaces, nil
}

func parseMembers(r *reader, pool ConstantPool, methods bool) ([]Member, error) {
	count, err := r.u2()
	if err != nil {
		return nil, err
	}
	members := make([]Member, 0, count)
	for range count {
		member := Member{}
		if member.AccessFlags, err = r.u2(); err != nil {
			return nil, err
		}
		nameIndex, err := r.u2()
		if err != nil {
			return nil, err
		}
		if member.Name, err = pool.UTF8At(nameIndex); err != nil {
			return nil, invalid(r.offset-2, "invalid member name: %v", err)
		}
		descriptorIndex, err := r.u2()
		if err != nil {
			return nil, err
		}
		if member.Descriptor, err = pool.UTF8At(descriptorIndex); err != nil {
			return nil, invalid(r.offset-2, "invalid member descriptor: %v", err)
		}
		if member.Attributes, err = parseAttributes(r, pool, methods); err != nil {
			return nil, err
		}

		if methods {
			codeCount := 0
			for _, attribute := range member.Attributes {
				if attribute.Code != nil {
					codeCount++
				}
			}
			abstractOrNative := member.AccessFlags&(0x0400|0x0100) != 0
			if (!abstractOrNative && codeCount != 1) || (abstractOrNative && codeCount != 0) {
				return nil, fmt.Errorf("%w: method %s%s has %d Code attributes", ErrInvalidFormat, member.Name, member.Descriptor, codeCount)
			}
		}
		members = append(members, member)
	}
	return members, nil
}

func parseAttributes(r *reader, pool ConstantPool, allowCode bool) ([]Attribute, error) {
	count, err := r.u2()
	if err != nil {
		return nil, err
	}
	attributes := make([]Attribute, 0, count)
	for range count {
		nameIndex, err := r.u2()
		if err != nil {
			return nil, err
		}
		name, err := pool.UTF8At(nameIndex)
		if err != nil {
			return nil, invalid(r.offset-2, "invalid attribute name: %v", err)
		}
		length, err := r.u4()
		if err != nil {
			return nil, err
		}
		info, err := r.sized(length)
		if err != nil {
			return nil, err
		}
		attribute := Attribute{Name: name, Info: append([]byte(nil), info...)}
		if name == "Code" {
			if !allowCode {
				return nil, fmt.Errorf("%w: Code attribute is not attached to a method", ErrInvalidFormat)
			}
			if attribute.Code, err = parseCode(info, pool); err != nil {
				return nil, err
			}
		}
		attributes = append(attributes, attribute)
	}
	return attributes, nil
}

func parseCode(data []byte, pool ConstantPool) (*Code, error) {
	r := newReader(data)
	code := &Code{}
	var err error
	if code.MaxStack, err = r.u2(); err != nil {
		return nil, err
	}
	if code.MaxLocals, err = r.u2(); err != nil {
		return nil, err
	}
	length, err := r.u4()
	if err != nil {
		return nil, err
	}
	bytecode, err := r.sized(length)
	if err != nil {
		return nil, err
	}
	code.Bytecode = append([]byte(nil), bytecode...)

	exceptionCount, err := r.u2()
	if err != nil {
		return nil, err
	}
	code.Exceptions = make([]ExceptionHandler, 0, exceptionCount)
	for range exceptionCount {
		handler := ExceptionHandler{}
		if handler.StartPC, err = r.u2(); err != nil {
			return nil, err
		}
		if handler.EndPC, err = r.u2(); err != nil {
			return nil, err
		}
		if handler.HandlerPC, err = r.u2(); err != nil {
			return nil, err
		}
		catchType, err := r.u2()
		if err != nil {
			return nil, err
		}
		if handler.StartPC >= handler.EndPC || uint32(handler.EndPC) > length || uint32(handler.HandlerPC) >= length {
			return nil, fmt.Errorf("%w: invalid exception handler range", ErrInvalidFormat)
		}
		if catchType != 0 {
			if handler.CatchType, err = pool.ClassName(catchType); err != nil {
				return nil, fmt.Errorf("%w: invalid catch type: %v", ErrInvalidFormat, err)
			}
		}
		code.Exceptions = append(code.Exceptions, handler)
	}
	if code.Attributes, err = parseAttributes(r, pool, false); err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, invalid(r.offset, "%d trailing bytes in Code attribute", r.remaining())
	}
	return code, nil
}
