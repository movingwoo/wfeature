package skt

import (
	"bytes"
	"encoding/binary"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// This original revision retains its item-name table when the tutorial returns
// to combat. Clear it at that transition so the ordinary ability loader runs.
// The overwritten phase assignment is redundant: the same block sets phase 5
// before calling another method. Bytecode lengths and branch targets stay fixed.
func prepareTutorialCompatibility(archive *Archive) authenticationClasses {
	if !compatibility.HasFix("skt", compatibility.JavaClassSet, archive.clipCodeDigest(), compatibility.SKTTutorialNameCache) {
		return nil
	}
	data := replaceTutorialPhaseStore(archive.Entries["f.class"], "a", "(Ljavax/microedition/lcdui/Graphics;)V", 1648, "h", "am", "w")
	if data == nil {
		return nil
	}
	return authenticationClasses{"f": data}
}

// replaceTutorialPhaseStore replaces bipush 17 / putfield phase with
// aconst_null / putfield names / nop. The class must already reference owner.
// Callers must gate this transformation on an exact class-set fingerprint.
func replaceTutorialPhaseStore(original []byte, methodName, descriptor string, pc int, owner, phase, names string) []byte {
	class, err := classfile.Parse(original)
	if err != nil || pc < 0 || len(class.ConstantPool) > 65531 {
		return nil
	}
	var ownerIndex uint16
	for i, constant := range class.ConstantPool {
		if constant.Tag == classfile.ConstantClass {
			name, _ := class.ConstantPool.ClassName(uint16(i))
			if name == owner {
				ownerIndex = uint16(i)
				break
			}
		}
	}
	if ownerIndex == 0 {
		return nil
	}
	for _, method := range class.Methods {
		if method.Name != methodName || method.Descriptor != descriptor {
			continue
		}
		for _, attribute := range method.Attributes {
			code := attribute.Code
			if code == nil || pc > len(code.Bytecode)-5 || bytes.Count(original, attribute.Info) != 1 {
				continue
			}
			instruction := code.Bytecode[pc : pc+5]
			if instruction[0] != 0x10 || instruction[1] != 17 || instruction[2] != 0xb5 {
				return nil
			}
			ref, err := class.ConstantPool.ReferenceAt(binary.BigEndian.Uint16(instruction[3:]))
			if err != nil || ref.Kind != classfile.FieldReference || ref.Class != owner || ref.Name != phase || ref.Descriptor != "I" {
				return nil
			}
			offset := bytes.Index(original, attribute.Info) + 8 + pc
			adapted := bytes.Clone(original)
			index := uint16(len(class.ConstantPool))
			copy(adapted[offset:offset+5], []byte{0x01, 0xb5, byte((index + 3) >> 8), byte(index + 3), 0x00})
			// Parse already validated every constant and length. Walk their wire
			// sizes rather than re-encoding modified UTF-8 or existing constants.
			end := 10
			for i := 1; i < len(class.ConstantPool); i++ {
				switch class.ConstantPool[i].Tag {
				case classfile.ConstantUTF8:
					end += 3 + int(binary.BigEndian.Uint16(original[end+1:end+3]))
				case classfile.ConstantLong, classfile.ConstantDouble:
					end += 9
					i++
				case classfile.ConstantClass, classfile.ConstantString, classfile.ConstantMethodType, classfile.ConstantModule, classfile.ConstantPackage:
					end += 3
				case classfile.ConstantMethodHandle:
					end += 4
				default:
					end += 5
				}
			}
			var constants bytes.Buffer
			for _, value := range []string{names, "[[Ljava/lang/String;"} {
				if len(value) > 65535 {
					return nil
				}
				constants.WriteByte(classfile.ConstantUTF8)
				_ = binary.Write(&constants, binary.BigEndian, uint16(len(value)))
				constants.WriteString(value)
			}
			constants.WriteByte(classfile.ConstantNameAndType)
			_ = binary.Write(&constants, binary.BigEndian, []uint16{index, index + 1})
			constants.WriteByte(classfile.ConstantFieldRef)
			_ = binary.Write(&constants, binary.BigEndian, []uint16{ownerIndex, index + 2})
			result := append(bytes.Clone(adapted[:end]), constants.Bytes()...)
			result = append(result, adapted[end:]...)
			binary.BigEndian.PutUint16(result[8:10], index+4)
			if _, err := classfile.Parse(result); err != nil {
				return nil
			}
			return result
		}
	}
	return nil
}
