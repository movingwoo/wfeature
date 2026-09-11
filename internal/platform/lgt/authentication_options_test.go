package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Assemble independent addresses and literal pools around the recognized
// instruction contracts. The executable Clet prefix remains newly authored.
func optionFixture(t *testing.T, delta uint32) (*Module, []byte, []byte, uint32) {
	t.Helper()
	prefix, entry, init, start, event, pause, resume := fixtureModule()
	code, data := make([]byte, 0x2000), make([]byte, 0x400)
	copy(code, prefix)
	copy(data, fixtureData(init, start, event, pause, resume))
	textBase, dataBase := fixtureTextBase+delta, fixtureDataBase+delta
	buffer, name := dataBase+0x200, dataBase+0x280
	copy(data[0x280:], authenticationOptionsName+"\x00")
	module := &Module{Entry: entry + delta, Sections: []Section{
		{Address: textBase, Size: uint32(len(code)), Data: code, Executable: true},
		{Address: dataBase, Size: uint32(len(data)), Data: data},
	}}
	putCall := func(address, target uint32) {
		displacement := target - address - 4
		offset := address - textBase
		binary.LittleEndian.PutUint16(code[offset:], 0xf000|uint16(displacement>>12)&0x7ff)
		binary.LittleEndian.PutUint16(code[offset+2:], 0xf800|uint16(displacement>>1)&0x7ff)
	}
	for _, part := range []struct {
		offset  uint32
		pattern []uint16
	}{
		{0x400, optionReader}, {0x800, optionWriter}, {0xc00, optionReply}, {0x1400, optionGate},
	} {
		for index := 0; index < len(part.pattern); index++ {
			word := part.pattern[index]
			address := textBase + part.offset + uint32(index*2)
			if word == 0 {
				putCall(address, textBase+0x1f00)
				index++
				continue
			}
			binary.LittleEndian.PutUint16(code[part.offset+uint32(index*2):], word)
			if word&0xf800 == 0x4800 {
				pool := ((address + 4) &^ 3) + uint32(word&0xff)*4
				binary.LittleEndian.PutUint32(code[pool-textBase:], dataBase+0x300)
			}
		}
	}
	setLiteral := func(offset, value uint32) {
		word := binary.LittleEndian.Uint16(code[offset:])
		pool := ((textBase + offset + 4) &^ 3) + uint32(word&0xff)*4
		binary.LittleEndian.PutUint32(code[pool-textBase:], value)
	}
	setLiteral(0x402, name)
	setLiteral(0x414, buffer)
	setLiteral(0x808, name)
	setLiteral(0x81a, buffer)
	for _, offset := range []uint32{0xc00 + 90, 0xc00 + 100, 0xc00 + 108, 0x1400} {
		setLiteral(offset, buffer)
	}
	putCall(textBase+0x400+52, textBase+0x800)
	binary.LittleEndian.PutUint16(code[0x1f00:], 0x4718) // bx r3
	return module, code, data, entry
}

func TestAuthenticationOptionsRequiresConnectedCodeContracts(t *testing.T) {
	for _, delta := range []uint32{0, 0x10000, 0x310000} {
		module, code, _, _ := optionFixture(t, delta)
		if !authenticationOptions(module) {
			t.Fatalf("relocated fixture at %#x was not recognized", delta)
		}
		for _, part := range []struct{ offset, words int }{
			{0x400, len(optionReader)}, {0x800, len(optionWriter)}, {0xc00, len(optionReply)}, {0x1400, len(optionGate)},
		} {
			for index := 0; index < part.words; index++ {
				offset := part.offset + index*2
				code[offset+1] ^= 0x80
				matched := authenticationOptions(module)
				code[offset+1] ^= 0x80
				if matched {
					t.Fatalf("changed instruction at %#x was recognized", offset)
				}
			}
		}
	}
	module, _, data, _ := optionFixture(t, 0)
	data[0x280] = 'x'
	if authenticationOptions(module) {
		t.Fatal("unrelated filename was recognized")
	}
	if authenticationOptions(nil) {
		t.Fatal("nil module was recognized")
	}
}

func TestAuthenticationOptionsRejectsTruncationAndDisconnectedPointers(t *testing.T) {
	module, code, _, _ := optionFixture(t, 0)
	for size := 0; size < len(code); size++ {
		module.Sections[0].Data = code[:size]
		// The last required instruction is the trampoline at offset 0x1f00.
		if size < 0x1f02 && authenticationOptions(module) {
			t.Fatalf("truncated executable length %d was recognized", size)
		}
	}
	module.Sections[0].Data = code
	gatePool := uint32(0x1404) + uint32(optionGate[0]&0xff)*4
	for _, pointer := range []uint32{0, 0xffffffff, fixtureDataBase + 0x204, fixtureTextBase + 0x200} {
		binary.LittleEndian.PutUint32(code[gatePool:], pointer)
		if authenticationOptions(module) {
			t.Fatalf("disconnected gate pointer %#x was recognized", pointer)
		}
	}
}

func TestAuthenticationOptionStorePreservesOriginalWordAndOtherWrites(t *testing.T) {
	base := backend.NewDirectorySaveStore(t.TempDir())
	original := make([]byte, 56)
	binary.LittleEndian.PutUint32(original[40:], 7)
	if err := base.StoreSave(authenticationOptionsKey, original); err != nil {
		t.Fatal(err)
	}
	store := newAuthenticationOptionStore(base, nil)
	view, ok := store.LoadSave(authenticationOptionsKey)
	if !ok || binary.LittleEndian.Uint32(view[40:]) != 1 {
		t.Fatal("authentication word was not adapted")
	}
	view[0] = 9
	if err := store.StoreSave(authenticationOptionsKey, view); err != nil {
		t.Fatal(err)
	}
	saved, _ := base.LoadSave(authenticationOptionsKey)
	if saved[0] != 9 || binary.LittleEndian.Uint32(saved[40:]) != 7 || binary.LittleEndian.Uint32(view[40:]) != 1 {
		t.Fatal("ordinary settings, original authentication or caller buffer changed incorrectly")
	}
	for _, key := range []string{"fs/progress.dat", "fs/.removed", "fs/.created"} {
		if err := store.StoreSave(key, []byte("ordinary")); err != nil {
			t.Fatal(err)
		}
		data, ok := base.LoadSave(key)
		if !ok || string(data) != "ordinary" {
			t.Fatalf("ordinary save %s did not persist", key)
		}
	}
	for size := 0; size < 64; size++ {
		data := bytes.Repeat([]byte{0xff}, size)
		if err := store.StoreSave(authenticationOptionsKey, data); err != nil {
			t.Fatal(err)
		}
		view, _ := store.LoadSave(authenticationOptionsKey)
		if len(view) != size || !bytes.Equal(data, bytes.Repeat([]byte{0xff}, size)) {
			t.Fatalf("size %d changed length or caller buffer", size)
		}
	}
}

func TestAuthenticationOptionsSessionUsesGuestFilePersistence(t *testing.T) {
	_, code, data, entry := optionFixture(t, 0)
	archive := zipOf(t, map[string][]byte{
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\n"),
		"0102ABCD.jar": zipOf(t, map[string][]byte{binaryModuleName: fixtureELF(code, data, entry)}),
	})
	base := backend.NewDirectorySaveStore(t.TempDir())
	if err := base.StoreSave(authenticationOptionsKey, make([]byte, 56)); err != nil {
		t.Fatal(err)
	}
	for index, enabled := range []bool{true, false, true} {
		session, err := StartSession(context.Background(), archive, SessionOptions{
			DisableAuthentication: !enabled, SaveStore: base, Width: 16, Height: 8, MaxSteps: 1 << 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close(context.Background()) })
		want := backend.AuthenticationOff
		if enabled {
			want = backend.AuthenticationLGTOptions
		}
		if session.Authentication() != want {
			t.Fatalf("session %d status = %s, want %s", index, session.Authentication(), want)
		}
		client := session.client
		name, err := client.allocateBytes([]byte(authenticationOptionsName + "\x00"))
		if err != nil {
			t.Fatal(err)
		}
		handle := callSlot(t, client, slotFsOpen, name, fileOpenReadOnly)
		buffer, err := client.allocate(56)
		if err != nil {
			t.Fatal(err)
		}
		if read := callSlot(t, client, slotFsRead, handle, buffer, 56); read != 56 {
			t.Fatalf("guest read = %d", read)
		}
		callSlot(t, client, slotFsClose, handle)
		word, err := client.readWord(buffer + 40)
		if err != nil || (word == 1) != enabled {
			t.Fatalf("guest authentication word = %d, enabled %v, error %v", word, enabled, err)
		}
		if index > 0 {
			progress, _ := client.readWord(buffer)
			if progress != 42 {
				t.Fatalf("ordinary settings did not survive restart: %d", progress)
			}
		}
		if index == 0 {
			if err := client.writeWord(buffer, 42); err != nil {
				t.Fatal(err)
			}
			handle = callSlot(t, client, slotFsOpen, name, fileOpenWriteTruncate)
			if written := callSlot(t, client, slotFsWrite, handle, buffer, 56); written != 56 {
				t.Fatalf("guest write = %d", written)
			}
			callSlot(t, client, slotFsClose, handle)
		}
	}
}
