package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// These newly assembled functions place the contracts at independent addresses.
// No game archive or original resource is part of the fixture.
func certificate58Fixture(t *testing.T, delta uint32) (*Module, []byte, []byte, uint32) {
	t.Helper()
	prefix, entry, init, start, event, pause, resume := fixtureModule()
	code, data := make([]byte, 0x2000), make([]byte, 0x400)
	copy(code, prefix)
	copy(data, fixtureData(init, start, event, pause, resume))
	textBase, dataBase := fixtureTextBase+delta, fixtureDataBase+delta
	copy(data[0x200:], authenticationCertificate58Name+"\x00")
	copy(data[0x240:], "PHONENUMBER\x00")
	module := &Module{Entry: entry + delta, Sections: []Section{
		{Address: textBase, Size: uint32(len(code)), Data: code, Executable: true},
		{Address: dataBase, Size: uint32(len(data)), Data: data},
	}}
	call := func(offset, target uint32) {
		displacement := target - (textBase + offset) - 4
		binary.LittleEndian.PutUint16(code[offset:], 0xf000|uint16(displacement>>12)&0x7ff)
		binary.LittleEndian.PutUint16(code[offset+2:], 0xf800|uint16(displacement>>1)&0x7ff)
	}
	for _, part := range []struct {
		offset  uint32
		pattern []uint16
	}{
		{0x400, certificate58Reader}, {0x800, certificate58Writer}, {0xc00, certificate58Cipher}, {0x1000, certificate58Gate},
	} {
		for index := 0; index < len(part.pattern); index++ {
			word := part.pattern[index]
			offset := part.offset + uint32(index*2)
			if word == 0 {
				call(offset, textBase+0x1f00)
				index++
				continue
			}
			binary.LittleEndian.PutUint16(code[offset:], word)
			if word&0xf800 == 0x4800 {
				pool := ((textBase + offset + 4) &^ 3) + uint32(word&0xff)*4
				binary.LittleEndian.PutUint32(code[pool-textBase:], dataBase+0x300)
			}
		}
	}
	literal := func(offset, value uint32) {
		word := binary.LittleEndian.Uint16(code[offset:])
		pool := ((textBase + offset + 4) &^ 3) + uint32(word&0xff)*4
		binary.LittleEndian.PutUint32(code[pool-textBase:], value)
	}
	for _, offset := range []uint32{0x412, 0x850} {
		literal(offset, dataBase+0x200)
	}
	for _, offset := range []uint32{0x46c, 0x848} {
		literal(offset, 0x21c3)
	}
	literal(0xc0c, 0x343fd)
	literal(0xc0e, 0x269ec3)
	literal(0x472, 0x201)
	literal(0x834, 0x200)
	literal(0x101e, 0x200)
	literal(0x1044, 0x229)
	literal(0x103e, dataBase+0x240)
	call(0x46e, textBase+0xc00)
	call(0x84a, textBase+0xc00)
	call(0x824, textBase+0x1f04)
	call(0x82e, textBase+0x1f04)
	call(0x1014, textBase+0x400)
	binary.LittleEndian.PutUint16(code[0x1f00:], 0x4718)
	binary.LittleEndian.PutUint16(code[0x1f04:], 0x4720)
	return module, code, data, entry
}

func TestCertificate58RequiresConnectedContracts(t *testing.T) {
	for _, delta := range []uint32{0, 0x10000, 0x310000} {
		module, code, data, _ := certificate58Fixture(t, delta)
		if !authenticationCertificate58(module) {
			t.Fatalf("relocated fixture %#x rejected", delta)
		}
		for _, part := range []struct {
			offset  int
			pattern []uint16
		}{
			{0x400, certificate58Reader}, {0x800, certificate58Writer}, {0xc00, certificate58Cipher}, {0x1000, certificate58Gate},
		} {
			for index := range part.pattern {
				offset := part.offset + index*2
				code[offset+1] ^= 0x80
				matched := authenticationCertificate58(module)
				code[offset+1] ^= 0x80
				if matched {
					t.Fatalf("changed instruction %#x accepted", offset)
				}
			}
		}
		for _, offset := range []int{0x200, 0x240} {
			data[offset] ^= 1
			matched := authenticationCertificate58(module)
			data[offset] ^= 1
			if matched {
				t.Fatalf("unrelated string %#x accepted", offset)
			}
		}
		// Alter only linked literals, leaving otherwise valid instructions intact.
		for _, offset := range []uint32{0x46c, 0x472, 0x834, 0x848, 0xc0c, 0xc0e, 0x101e, 0x1044} {
			word := binary.LittleEndian.Uint16(code[offset:])
			pool := ((fixtureTextBase + delta + offset + 4) &^ 3) + uint32(word&0xff)*4 - (fixtureTextBase + delta)
			code[pool] ^= 1
			matched := authenticationCertificate58(module)
			code[pool] ^= 1
			if matched {
				t.Fatalf("disconnected literal %#x accepted", offset)
			}
		}
	}
	if authenticationCertificate58(nil) {
		t.Fatal("nil module accepted")
	}
	module, code, _, _ := certificate58Fixture(t, 0)
	for size := 0; size < 0x1f06; size++ {
		module.Sections[0].Data = code[:size]
		if authenticationCertificate58(module) {
			t.Fatalf("truncated code %d accepted", size)
		}
	}
}

func TestCertificate58StorePreservesCertificateAndLedgerMembership(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, membership := range []bool{false, true} {
			base := backend.NewDirectorySaveStore(t.TempDir())
			original := bytes.Repeat([]byte{0xa5}, 58)
			if existing {
				if err := base.StoreSave(authenticationCertificate58Key, original); err != nil {
					t.Fatal(err)
				}
			}
			ledger := []byte("ordinary.dat")
			if membership {
				ledger = []byte("audio.adt\nordinary.dat")
			}
			for _, key := range []string{fileRemovedKey, fileCreatedKey} {
				if err := base.StoreSave(key, ledger); err != nil {
					t.Fatal(err)
				}
			}
			store, ok := newAuthenticationCertificate58Store(base, nil, "01012345678")
			if !ok {
				t.Fatal("valid certificate rejected")
			}
			got, _ := store.LoadSave(authenticationCertificate58Key)
			cryptCertificate58(got)
			if string(got[40:52]) != "01012345678\x00" {
				t.Fatalf("MIN = %q", got[40:52])
			}
			if existing {
				decoded := bytes.Clone(original)
				cryptCertificate58(decoded)
				if !bytes.Equal(got[:40], decoded[:40]) || !bytes.Equal(got[52:], decoded[52:]) {
					t.Fatal("opaque certificate bytes changed")
				}
			}
			if err := store.StoreSave(authenticationCertificate58Key, []byte("guest rewrite")); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{fileRemovedKey, fileCreatedKey} {
				if err := store.StoreSave(key, []byte("audio.adt\nprogress.dat")); err != nil {
					t.Fatal(err)
				}
				persisted, _ := base.LoadSave(key)
				if !bytes.Equal(persisted, certificate58Ledger([]byte("progress.dat"), membership)) {
					t.Fatalf("ledger %s changed original membership: %q", key, persisted)
				}
			}
			if err := store.StoreSave("fs/progress.dat", []byte("checkpoint")); err != nil {
				t.Fatal(err)
			}
			data, found := base.LoadSave(authenticationCertificate58Key)
			if found != existing || (found && !bytes.Equal(data, original)) {
				t.Fatal("backing certificate changed")
			}
			restarted, ok := newAuthenticationCertificate58Store(base, nil, "12")
			if !ok {
				t.Fatal("restart rejected")
			}
			restored, _ := restarted.LoadSave("fs/progress.dat")
			if string(restored) != "checkpoint" {
				t.Fatal("ordinary progress did not persist")
			}
			fresh, _ := restarted.LoadSave(authenticationCertificate58Key)
			cryptCertificate58(fresh)
			if string(fresh[40:52]) != "12\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" {
				t.Fatalf("fresh identity = %q", fresh[40:52])
			}
			// Returned views must never mutate a later load.
			fresh[0] ^= 1
			again, _ := restarted.LoadSave(authenticationCertificate58Key)
			cryptCertificate58(again)
			if fresh[0] == again[0] {
				t.Fatal("returned certificate aliases private state")
			}
		}
	}
	for _, number := range []string{"", "123456789012", "12a"} {
		if _, ok := newAuthenticationCertificate58Store(nil, nil, number); ok {
			t.Fatalf("invalid identity %q accepted", number)
		}
	}
	for _, size := range []int{1, 57, 59, 60} {
		base := backend.NewDirectorySaveStore(t.TempDir())
		if err := base.StoreSave(authenticationCertificate58Key, make([]byte, size)); err != nil {
			t.Fatal(err)
		}
		if _, ok := newAuthenticationCertificate58Store(base, nil, "12"); ok {
			t.Fatalf("record size %d accepted", size)
		}
	}
}

func TestCertificate58SessionUsesGuestFilesAndIdentitySnapshot(t *testing.T) {
	_, code, data, entry := certificate58Fixture(t, 0)
	archive := zipOf(t, map[string][]byte{
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\n"),
		"0102ABCD.jar": zipOf(t, map[string][]byte{binaryModuleName: fixtureELF(code, data, entry)}),
	})
	base := backend.NewDirectorySaveStore(t.TempDir())
	originalNumber := wipic.SubscriberNumber()
	t.Cleanup(func() { _ = wipic.SetSubscriberNumber(originalNumber) })
	for _, enabled := range []bool{true, false, true} {
		if err := wipic.SetSubscriberNumber("01012345678"); err != nil {
			t.Fatal(err)
		}
		session, err := StartSession(context.Background(), archive, SessionOptions{DisableAuthentication: !enabled, SaveStore: base, Width: 16, Height: 8, MaxSteps: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close(context.Background()) })
		want := backend.AuthenticationOff
		if enabled {
			want = backend.AuthenticationLGTCertificate58
		}
		if session.Authentication() != want {
			t.Fatalf("status = %s, want %s", session.Authentication(), want)
		}
		if err := wipic.SetSubscriberNumber("12"); err != nil {
			t.Fatal(err)
		}
		client := session.client
		for _, property := range []string{"MIN", "PHONENUMBER"} {
			name, _ := client.allocateBytes([]byte(property + "\x00"))
			buffer, _ := client.allocate(12)
			callSlot(t, client, slotGetProperty, name, buffer, 12)
			got, err := client.readCString(buffer)
			if err != nil || got != "01012345678" {
				t.Fatalf("C identity = %q: %v", got, err)
			}
			javaName, _ := client.newJavaString(property)
			result, err := javaSystemProperty(client, context.Background(), client.thread, []uint32{javaName})
			value, _ := client.javaText(result)
			if err != nil || value != got {
				t.Fatalf("Java identity = %q: %v", value, err)
			}
		}
		name, _ := client.allocateBytes([]byte(authenticationCertificate58Name + "\x00"))
		handle := callSlot(t, client, slotFsOpen, name, fileOpenReadOnly)
		if !enabled {
			if int32(handle) >= 0 {
				t.Fatal("disabled session saw a private certificate")
			}
			continue
		}
		buffer, _ := client.allocate(58)
		if callSlot(t, client, slotFsRead, handle, buffer, 58) != 58 {
			t.Fatal("guest could not read certificate")
		}
		callSlot(t, client, slotFsClose, handle)
		certificate := make([]byte, 58)
		if err := client.core.Memory().Read(buffer, certificate); err != nil {
			t.Fatal(err)
		}
		cryptCertificate58(certificate)
		pointer, err := client.allocateWords([]uint32{buffer})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.call(context.Background(), fixtureTextBase+0xc01, []uint32{0, pointer, 58, 0x21c3}); err != nil {
			t.Fatal(err)
		}
		guestDecoded := make([]byte, 58)
		if err := client.core.Memory().Read(buffer, guestDecoded); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(guestDecoded, certificate) {
			t.Fatal("executed guest decoder disagrees with session certificate")
		}
		if string(certificate[40:52]) != "01012345678\x00" {
			t.Fatalf("guest certificate identity = %q", certificate[40:52])
		}
		// The ordinary guest path writes and closes a file through the same store.
		progress, _ := client.allocateBytes([]byte("progress.dat\x00"))
		handle = callSlot(t, client, slotFsOpen, progress, fileOpenWriteTruncate)
		if callSlot(t, client, slotFsWrite, handle, buffer, 4) != 4 {
			t.Fatal("guest progress write failed")
		}
		callSlot(t, client, slotFsClose, handle)
		if saved, ok := base.LoadSave("fs/progress.dat"); !ok || len(saved) != 4 {
			t.Fatal("ordinary guest write did not persist")
		}
		if _, exists := base.LoadSave(authenticationCertificate58Key); exists {
			t.Fatal("private certificate leaked to disk")
		}
	}
}

func TestLGTSubscriberSnapshotsCoexist(t *testing.T) {
	original := wipic.SubscriberNumber()
	t.Cleanup(func() { _ = wipic.SetSubscriberNumber(original) })
	numbers := []string{"01012345678", "01987654321"}
	clients := make([]*Client, len(numbers))
	for index, number := range numbers {
		if err := wipic.SetSubscriberNumber(number); err != nil {
			t.Fatal(err)
		}
		clients[index] = fixtureClient(t)
	}
	if err := wipic.SetSubscriberNumber("12"); err != nil {
		t.Fatal(err)
	}
	for index, client := range clients {
		for _, property := range []string{"MIN", "PHONENUMBER"} {
			name, err := client.allocateBytes([]byte(property + "\x00"))
			if err != nil {
				t.Fatal(err)
			}
			buffer, err := client.allocate(12)
			if err != nil {
				t.Fatal(err)
			}
			callSlot(t, client, slotGetProperty, name, buffer, 12)
			value, err := client.readCString(buffer)
			if err != nil || value != numbers[index] {
				t.Fatalf("client %d C identity = %q: %v", index, value, err)
			}
			argument, err := client.newJavaString(property)
			if err != nil {
				t.Fatal(err)
			}
			result, err := javaSystemProperty(client, context.Background(), client.thread, []uint32{argument})
			value, _ = client.javaText(result)
			if err != nil || value != numbers[index] {
				t.Fatalf("client %d Java identity = %q: %v", index, value, err)
			}
		}
	}
}

func TestCertificate58CaseVariantsStayPrivate(t *testing.T) {
	base := backend.NewDirectorySaveStore(t.TempDir())
	store, ok := newAuthenticationCertificate58Store(base, nil, "12")
	if !ok {
		t.Fatal("private certificate setup failed")
	}
	for _, key := range []string{"fs/AUDIO.ADT", "/fs/./audio.adt", "fs/Audio.Adt"} {
		if err := store.StoreSave(key, []byte("private rewrite")); err != nil {
			t.Fatal(err)
		}
		view, ok := store.LoadSave(authenticationCertificate58Key)
		if !ok || string(view) != "private rewrite" {
			t.Fatalf("case variant %s missed private state", key)
		}
		if _, exists := base.LoadSave(key); exists {
			t.Fatalf("case variant %s persisted", key)
		}
	}
}

func TestCertificate58EmptyFailedAttemptIsPrivateAndRecoverable(t *testing.T) {
	base := backend.NewDirectorySaveStore(t.TempDir())
	if err := base.StoreSave(authenticationCertificate58Key, []byte{}); err != nil {
		t.Fatal(err)
	}
	ledger := []byte("audio.adt\nprogress.dat")
	if err := base.StoreSave(fileCreatedKey, ledger); err != nil {
		t.Fatal(err)
	}
	if err := base.StoreSave("fs/progress.dat", []byte{4, 2}); err != nil {
		t.Fatal(err)
	}
	store, ok := newAuthenticationCertificate58Store(base, nil, "01012345678")
	if !ok {
		t.Fatal("empty certificate from a failed connection rejected")
	}
	data, ok := store.LoadSave(authenticationCertificate58Key)
	if !ok || len(data) != 58 {
		t.Fatalf("replacement certificate length=%d, exists=%v", len(data), ok)
	}
	cryptCertificate58(data)
	if string(data[40:52]) != "01012345678\x00" {
		t.Fatal("replacement identity differs")
	}
	if err := store.StoreSave(authenticationCertificate58Key, []byte("guest rewrite")); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave(fileCreatedKey, ledger); err != nil {
		t.Fatal(err)
	}
	original, exists := base.LoadSave(authenticationCertificate58Key)
	if !exists || len(original) != 0 {
		t.Fatal("backing empty certificate changed")
	}
	saved, _ := base.LoadSave("fs/progress.dat")
	if !bytes.Equal(saved, []byte{4, 2}) {
		t.Fatal("ordinary progress changed")
	}
	saved, _ = base.LoadSave(fileCreatedKey)
	if !bytes.Equal(saved, ledger) {
		t.Fatal("original membership changed")
	}
	if _, ok := newAuthenticationCertificate58Store(base, nil, "01012345678"); !ok {
		t.Fatal("restart rejected preserved empty certificate")
	}
}
