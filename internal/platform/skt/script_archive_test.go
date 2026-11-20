package skt

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestScriptInstallUserID(t *testing.T) {
	var data []byte
	for _, s := range []string{"application/x-gnex-sgs", "sgs", "36.19.0", "", "IP:127.0.0.1;PORT:1;ID:AB12CD34"} {
		data = binary.LittleEndian.AppendUint32(data, uint32(len(s)))
		data = append(data, s...)
	}
	if got := scriptInstallUserID(data); got != "AB12CD34" {
		t.Fatalf("installer ID = %q", got)
	}
	for _, input := range [][]byte{data[:len(data)-1], bytes.Replace(data, []byte("ID:"), []byte("XX:"), 1), []byte("ID:AB12CD34")} {
		if got := scriptInstallUserID(input); got != "" {
			t.Fatalf("accepted malformed metadata: %q", got)
		}
	}
}
