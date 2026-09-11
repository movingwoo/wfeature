package skt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

const authenticationCheckDescriptor = "(Ljavax/microedition/midlet/MIDlet;)Z"

// Fingerprints describe complete license-check bytecode shapes, independent of
// archive names, class obfuscation and constant-pool ordering. They retain branch
// offsets, local slots, method descriptors, platform calls and license constants.
// Only application-owned member names and display strings are anonymized.
var authenticationLicenseShapes = map[string]bool{
	// Two direct-digest compiler layouts, two layouts with a missing-key
	// notice, and the library selecting among four key generations.
	"d05a361e9921ceaaa517a14a90e8350cea33051e3220ac9deab834b59093903d": true,
	"08df607a4d67756cee5044682d133c1d2257dcc09701b806d38c11fcf137a41b": true,
	"19db02bb01a4956db1f26a2664c1298c28792e79929eef39c9a0898f48a66684": true,
	"ea1ae7596a8b605a83493e19d5cae9c0d48169d846d40c94ed20345a210cb3ad": true,
	"521d50e14821448ffd09ad86d456e4dd96d9cf27150543f2a4d19e2391879870": true,
}

type authenticationClasses map[string][]byte

func (classes authenticationClasses) ClassBytes(name string) ([]byte, bool) {
	data, ok := classes[name]
	return data, ok
}

func licenseConstant(pool classfile.ConstantPool, index uint16, self string) (string, bool) {
	constant, err := pool.At(index)
	if err != nil {
		return "", false
	}
	switch constant.Tag {
	case classfile.ConstantString:
		value, err := pool.UTF8At(constant.Index1)
		if err != nil {
			return "", false
		}
		switch value {
		case "MIDlet-Key", "MIDlet-Key2", "MIDlet-Key3", "MIDlet-Key4", "MIDlet-1", "MIDlet-Jar-URL",
			"SERVICE_ID=", "a0a535ef35b", "MIN", "m.MIN", "m.CARRIER", "com.xce.wipi.version":
			return "string:" + value, true
		default:
			return "string", true
		}
	case classfile.ConstantInteger:
		return fmt.Sprintf("int:%d", constant.Integer), true
	case classfile.ConstantClass:
		name, err := pool.ClassName(index)
		if name == self {
			name = "self"
		}
		return "class:" + name, err == nil
	case classfile.ConstantMethodRef, classfile.ConstantFieldRef:
		reference, err := pool.ReferenceAt(index)
		if err != nil {
			return "", false
		}
		if reference.Class == self {
			reference.Class = "self"
			if reference.Name != "<init>" && reference.Name != "<clinit>" {
				reference.Name = "member"
			}
		}
		return fmt.Sprintf("%d:%s.%s%s", constant.Tag, reference.Class, reference.Name, reference.Descriptor), true
	}
	return "", false
}

func licenseShape(class *classfile.Class, method *classfile.Member) string {
	code := method.CodeAttribute()
	if method.AccessFlags&0x0008 == 0 || method.Descriptor != authenticationCheckDescriptor || code == nil ||
		len(code.Bytecode) < 4 || len(code.Bytecode) > 4096 {
		return ""
	}
	last := code.Bytecode[len(code.Bytecode)-4:]
	if last[0] != 0xb6 || last[3] != 0xac {
		return ""
	}
	reference, err := class.ConstantPool.ReferenceAt(binary.BigEndian.Uint16(last[1:3]))
	if err != nil || reference.Class != "java/lang/String" || reference.Name != "equals" || reference.Descriptor != "(Ljava/lang/Object;)Z" {
		return ""
	}
	var normalized strings.Builder
	fmt.Fprintf(&normalized, "%d/%d;", code.MaxStack, code.MaxLocals)
	for pc := 0; pc < len(code.Bytecode); {
		opcode := code.Bytecode[pc]
		length, constant := 1, false
		switch {
		case opcode == 0x12:
			length, constant = 2, true
		case opcode == 0x13 || opcode >= 0xb2 && opcode <= 0xb8 || opcode == 0xbb || opcode == 0xbd || opcode == 0xc0:
			length, constant = 3, true
		case opcode == 0x10 || opcode >= 0x15 && opcode <= 0x19 || opcode >= 0x36 && opcode <= 0x3a || opcode == 0xbc:
			length = 2
		case opcode == 0x11 || opcode == 0x84 || opcode >= 0x99 && opcode <= 0xa7 || opcode == 0xc6 || opcode == 0xc7:
			length = 3
		case opcode == 0xaa || opcode == 0xab || opcode >= 0xc4 || opcode == 0xb9 || opcode == 0xba || opcode == 0x14:
			return "" // no variable-width or unrecognized instruction in these checks
		}
		if pc > len(code.Bytecode)-length {
			return ""
		}
		fmt.Fprintf(&normalized, "%02x", opcode)
		if constant {
			index := uint16(code.Bytecode[pc+1])
			if length == 3 {
				index = binary.BigEndian.Uint16(code.Bytecode[pc+1:])
			}
			value, ok := licenseConstant(class.ConstantPool, index, class.Name)
			if !ok {
				return ""
			}
			fmt.Fprintf(&normalized, "[%s]", value)
		} else {
			fmt.Fprintf(&normalized, "%x", code.Bytecode[pc+1:pc+length])
		}
		normalized.WriteByte(';')
		pc += length
	}
	for _, handler := range code.Exceptions {
		fmt.Fprintf(&normalized, "catch:%d/%d/%d/%s;", handler.StartPC, handler.EndPC, handler.HandlerPC, handler.CatchType)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalized.String())))
}

// Transform only the final license comparison. The guest still obtains its
// properties, initializes library state, calculates its digest, and handles
// exceptions normally. pop2 / iconst_1 / nop has exactly the original call's
// stack effect and length; exception ranges and all branch targets stay valid.
// Source archives and the general String.equals implementation remain intact.
func prepareAuthentication(archive *Archive) authenticationClasses {
	classes := make(authenticationClasses)
	names := make([]string, 0, len(archive.Entries))
	for name := range archive.Entries {
		if strings.HasSuffix(name, ".class") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		original := archive.Entries[name]
		if !bytes.Contains(original, []byte("MIDlet-Key")) || !bytes.Contains(original, []byte("SERVICE_ID=")) ||
			!bytes.Contains(original, []byte("MIN")) {
			continue
		}
		class, err := classfile.Parse(original)
		if err != nil || name != class.Name+".class" {
			continue
		}
		var adapted []byte
		for _, method := range class.Methods {
			if !authenticationLicenseShapes[licenseShape(class, &method)] {
				continue
			}
			for _, attribute := range method.Attributes {
				if attribute.Code == nil || bytes.Count(original, attribute.Info) != 1 {
					continue
				}
				offset := bytes.Index(original, attribute.Info) + 8 + len(attribute.Code.Bytecode) - 4
				if offset < 8 || offset > len(original)-4 || !bytes.Equal(original[offset:offset+4], attribute.Code.Bytecode[len(attribute.Code.Bytecode)-4:]) {
					continue
				}
				if adapted == nil {
					adapted = bytes.Clone(original)
				}
				copy(adapted[offset:offset+3], []byte{0x58, 0x04, 0x00})
			}
		}
		if adapted != nil {
			classes[class.Name] = adapted
		}
	}
	return classes
}

func (runtime *Runtime) Authentication() backend.AuthenticationStatus {
	if runtime.authentication == "" {
		return backend.AuthenticationOff
	}
	return runtime.authentication
}
