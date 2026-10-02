package backend

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func TestCheckpointContainerOwnsBytesAndKeepsSaveGeneration(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	saved := Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Session: []byte("session"), Runtime: []byte("runtime"),
		Saves: []SaveEntry{{Key: "empty", Data: []byte{}}, {Key: "rms/.index", Data: []byte{1, 2}}, {Key: "raw\xff", Data: []byte{3}}}}
	data, err := EncodeCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	first := bytes.Clone(data)
	got, err := DecodeCheckpoint(data, identity)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity != saved.Identity || got.Variant != saved.Variant || !bytes.Equal(got.Session, saved.Session) || !bytes.Equal(got.Runtime, saved.Runtime) || len(got.Saves) != 3 || got.Saves[0].Key != "empty" || len(got.Saves[0].Data) != 0 || got.Saves[1].Key != "raw\xff" || got.Saves[2].Key != "rms/.index" {
		t.Fatalf("decoded checkpoint differs: %+v", got)
	}
	again, err := EncodeCheckpoint(got)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatalf("encoding is not stable: %v", err)
	}
	clear(data)
	if string(got.Session) != "session" || string(got.Runtime) != "runtime" || !reflect.DeepEqual(got.Saves[2].Data, []byte{1, 2}) {
		t.Fatal("decoded checkpoint shares the input buffer")
	}
}

func TestCheckpointContainerRejectsDamageIdentityAndUnsupportedVariants(t *testing.T) {
	identity := SaveIdentity([]byte("authored checkpoint archive"))
	data, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Session: []byte("{}"), Runtime: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func([]byte) []byte
		want error
	}{
		{"magic", func(b []byte) []byte { b[0]++; return b }, ErrNotCheckpoint},
		{"version", func(b []byte) []byte { b[8]++; return b }, ErrCheckpointVersion},
		{"variant", func(b []byte) []byte { b[10] = 255; return b }, ErrCheckpointVersion},
		{"length", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[48:52], ^uint32(0)); return b }, ErrCheckpointDamaged},
		{"digest", func(b []byte) []byte { b[checkpointHeaderSize-1]++; return b }, ErrCheckpointDamaged},
		{"payload", func(b []byte) []byte { b[len(b)-1]++; return b }, ErrCheckpointDamaged},
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }, ErrCheckpointDamaged},
		{"trailing", func(b []byte) []byte { return append(b, 0) }, ErrCheckpointDamaged},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeCheckpoint(test.edit(bytes.Clone(data)), identity); !errors.Is(err, test.want) {
				t.Fatalf("refusal = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := DecodeCheckpoint(data, SaveIdentity([]byte("different archive"))); !errors.Is(err, ErrCheckpointIdentity) {
		t.Fatalf("different archive refusal = %v", err)
	}
	if _, err := DecodeSavePack(data); !errors.Is(err, ErrNotSavePack) {
		t.Fatalf("checkpoint mistaken for ordinary saves: %v", err)
	}
	for _, entries := range [][]SaveEntry{{{Key: "a"}, {Key: "a/child"}}, {{Key: "../outside"}}, {{Key: "a"}, {Key: "a"}}} {
		if _, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Saves: entries}); err == nil {
			t.Fatal("invalid save generation accepted")
		}
	}
}
