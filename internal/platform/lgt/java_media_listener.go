package lgt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/armcore"
)

const (
	javaPlayListenerClass   = "org/kwis/msp/media/PlayListener"
	javaPlayUpdateSignature = "(Lorg/kwis/msp/media/Clip;II)V"
	maxJavaMediaClasses     = 256
)

type javaMediaClass struct {
	runtime *javaRuntimeClass
	record  javaClass
}

// Media retains guest addresses across calls. A readable vtable alone is not
// ownership: the object must still be an allocation the collector tracks.
func (client *Client) javaMediaClassChain(object uint32) ([]javaMediaClass, error) {
	if client.javaRun == nil {
		return nil, fmt.Errorf("LGT media object %#x has no Java runtime", object)
	}
	if _, issued := client.javaRun.objects[object]; !issued {
		return nil, fmt.Errorf("LGT media object %#x was not issued here", object)
	}
	class, known := client.javaClassOfObject(object)
	if !known {
		return nil, fmt.Errorf("LGT media object %#x has no class", object)
	}
	var chain []javaMediaClass
	seen := map[*javaRuntimeClass]bool{}
	for owner := class; owner != nil; owner = owner.Super {
		if seen[owner] || len(chain) == maxJavaMediaClasses {
			return nil, fmt.Errorf("LGT media class hierarchy is cyclic or too large")
		}
		seen[owner] = true
		record := javaClass{Name: owner.Name}
		if owner.Handle != 0 {
			var err error
			record, err = client.readJavaMediaClass(owner.Handle)
			if err != nil {
				return nil, err
			}
			if record.Name != owner.Name ||
				record.SuperHandle == 0 && record.Super == "" && owner.Super != nil ||
				record.SuperHandle != 0 && (owner.Super == nil || owner.Super.Handle != record.SuperHandle) ||
				record.Super != "" && (owner.Super == nil || owner.Super.Name != record.Super) {
				return nil, fmt.Errorf("LGT media class has a different guest binding")
			}
		}
		chain = append(chain, javaMediaClass{owner, record})
	}
	return chain, nil
}

// Dispatch validation reads the guest records again. Cached checkpoint class
// summaries must not grant a forged interface or hide a malformed member run.
func (client *Client) readJavaMediaClass(handle uint32) (javaClass, error) {
	if handle < javaClassHeader || !client.isJavaClassHandle(handle) {
		return javaClass{}, fmt.Errorf("LGT media class %#x has no valid header", handle)
	}
	memory := client.core.Memory()
	if err := memory.ValidateRange(handle-javaClassHeader, javaClassHeader+12, armcore.PermissionRead); err != nil {
		return javaClass{}, err
	}
	record, err := client.readJavaClass(handle, nil)
	if err != nil || record.Name == "" {
		return javaClass{}, fmt.Errorf("LGT media class %#x has no readable record: %v", handle, err)
	}
	super, err := client.readWord(record.Header + 0x10)
	if err != nil || super != 0 && record.SuperHandle == 0 && record.Super == "" {
		return javaClass{}, fmt.Errorf("LGT media class has an unreadable superclass")
	}
	table, err := client.readWord(record.Header + javaClassInterfaces)
	if err != nil {
		return javaClass{}, err
	}
	if table != 0 {
		count, err := client.readWord(table)
		if err != nil || count == 0 || count > maxJavaInterfaces {
			return javaClass{}, fmt.Errorf("LGT media class has an invalid interface count")
		}
		if err := memory.ValidateRange(table, 4+uint64(count)*4, armcore.PermissionRead); err != nil {
			return javaClass{}, err
		}
		for index := uint32(0); index < count; index++ {
			entry, err := client.readWord(table + 4 + index*4)
			if err != nil || entry == 0 {
				return javaClass{}, fmt.Errorf("LGT media class has a missing interface entry")
			}
			if err := memory.ValidateRange(entry, 8, armcore.PermissionRead); err != nil {
				return javaClass{}, err
			}
		}
		record.Interfaces, err = client.readJavaInterfaces(record.Header)
		if err != nil || len(record.Interfaces) != int(count) {
			return javaClass{}, fmt.Errorf("LGT media class has an unreadable interface table")
		}
		for _, implemented := range record.Interfaces {
			if implemented.Name == "" {
				return javaClass{}, fmt.Errorf("LGT media class has an unnamed interface")
			}
		}
	}
	run, err := client.readWord(record.Header + javaClassMethodRun)
	if err != nil {
		return javaClass{}, err
	}
	if run != 0 {
		count, err := client.readWord(run)
		if err != nil || count > maxJavaMembers {
			return javaClass{}, fmt.Errorf("LGT media class has an invalid method count")
		}
		if err := memory.ValidateRange(run, 4+uint64(count)*javaMethodWords*4, armcore.PermissionRead); err != nil {
			return javaClass{}, err
		}
		record.Methods, _, err = client.readJavaMembers(handle, run, javaMethodWords)
		if err != nil {
			return javaClass{}, err
		}
	}
	return record, nil
}

func (client *Client) validateJavaMediaClip(object uint32) error {
	chain, err := client.javaMediaClassChain(object)
	if err != nil {
		return err
	}
	for _, class := range chain {
		if class.record.Name == javaClipClass {
			return nil
		}
	}
	return fmt.Errorf("LGT media object %#x is not a Clip", object)
}

func (client *Client) javaMediaListenerBody(object uint32) (uint32, error) {
	chain, err := client.javaMediaClassChain(object)
	if err != nil {
		return 0, err
	}
	// Visit the entire bounded graph, including shared parents of derived
	// interfaces. An early match must not conceal a cycle elsewhere in it.
	states := map[uint32]uint8{}
	declared := false
	var visit func(javaInterface) error
	visit = func(implemented javaInterface) error {
		if implemented.Name == javaPlayListenerClass {
			declared = true
		}
		if implemented.Handle == 0 || states[implemented.Handle] == 2 {
			return nil
		}
		if states[implemented.Handle] == 1 || len(states) == maxJavaMediaClasses {
			return fmt.Errorf("LGT media interface graph is cyclic or too large")
		}
		states[implemented.Handle] = 1
		record, err := client.readJavaMediaClass(implemented.Handle)
		if err != nil {
			return err
		}
		for _, parent := range record.Interfaces {
			if err := visit(parent); err != nil {
				return err
			}
		}
		states[implemented.Handle] = 2
		return nil
	}
	for _, class := range chain {
		for _, implemented := range class.record.Interfaces {
			if err := visit(implemented); err != nil {
				return 0, err
			}
		}
	}
	// Validate named signatures before using the one-method interface ABI.
	// Kind is an AOT member kind, not established Java access flags. An
	// inherited named Body must not bypass a stripped receiver's override.
	// Original AOT archives can omit their interface table altogether while
	// retaining the exact callback member. That raw member is also sufficient
	// proof; a stripped callback still needs the declared interface below.
	named := false
	methodOwner, methodBody := -1, uint32(0)
findMethod:
	for index, class := range chain {
		for _, method := range class.record.Methods {
			if method.Name == "playUpdate" {
				named = true
				if method.Descriptor == javaPlayUpdateSignature {
					methodOwner, methodBody = index, method.Body
					break findMethod
				}
			}
		}
	}
	if methodOwner == 0 {
		return client.javaMediaExecutable(methodBody)
	}
	if methodOwner < 0 && !declared {
		return 0, fmt.Errorf("LGT media listener %#x has neither PlayListener metadata nor a named playback callback", object)
	}
	if !named || methodOwner > 0 {
		// Stripped records can still name the direct, one-method interface.
		// Do not infer the method order of an arbitrary derived interface.
		class := chain[0].runtime
		for index, owner := range chain {
			for _, implemented := range owner.record.Interfaces {
				if implemented.Name != javaPlayListenerClass {
					continue
				}
				body := implemented.Slot
				if implemented.Slot < class.Slots && class.VTable != 0 {
					address := uint64(class.VTable) + 4 + uint64(implemented.Slot)*4
					if address > 0xffffffff {
						return 0, fmt.Errorf("LGT media method table overflows")
					}
					body, err = client.readWord(uint32(address))
					if err != nil {
						return 0, err
					}
				} else if methodOwner >= 0 && index > methodOwner {
					// A direct address on a more distant parent does not
					// override a nearer named implementation. A vtable slot,
					// above, always dispatches on the concrete receiver.
					continue
				}
				return client.javaMediaExecutable(body)
			}
		}
	}
	if methodOwner >= 0 {
		return client.javaMediaExecutable(methodBody)
	}
	return 0, fmt.Errorf("LGT media listener %#x has no playUpdate%s", object, javaPlayUpdateSignature)
}

func (client *Client) javaMediaExecutable(body uint32) (uint32, error) {
	width := uint64(4)
	if body&1 != 0 {
		width = 2
	} else if body&3 != 0 {
		return 0, fmt.Errorf("LGT media callback %#x is not aligned", body)
	}
	if body == 0 {
		return 0, fmt.Errorf("LGT media callback has no entry")
	}
	if err := client.core.Memory().ValidateRange(body&^1, width, armcore.PermissionExecute); err != nil {
		return 0, fmt.Errorf("LGT media callback %#x is not executable: %w", body, err)
	}
	return body, nil
}

// Most compilers inline these constants, but an imported static field must
// receive the same value. Only slots requested by this module are populated.
func (client *Client) initializeJavaPlayListenerStatics(class *javaRuntimeClass) error {
	if client.javaLink == nil || client.javaLink.layout == nil {
		return nil
	}
	laid := client.javaLink.layout.classes[class.Name]
	if laid == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value int32
	}{
		{"ERROR", wipi.PlayEventError}, {"END_OF_DATA", wipi.PlayEventEndOfData},
		{"START", wipi.PlayEventStart}, {"STOP", wipi.PlayEventStop},
		{"PAUSE", wipi.PlayEventPause}, {"RESUME", wipi.PlayEventResume},
		{"RECORD", wipi.PlayEventRecord}, {"FULL_OF_DATA", wipi.PlayEventFullOfData},
	} {
		if slot, declared := laid.Statics[field.name+"I"]; declared && slot < class.StaticWords {
			if err := client.writeWord(class.dataBlock+(javaClassDataWords+slot)*4, uint32(field.value)); err != nil {
				return err
			}
		}
	}
	return nil
}
