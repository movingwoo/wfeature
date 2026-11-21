package ktf

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A codeRewrite is a window of a recognized client and, when replace is set,
// the bytes written at offset in it. expect is what has to be at `at` once the
// client has relocated itself; a window without replace is only checked.
type codeRewrite struct {
	at      uint32
	expect  []byte
	offset  int
	replace []byte
}

// applyCodeRewrites checks every window against the code that was examined
// and only then writes the replacements, so a client whose code differs is
// refused with nothing changed. The archive is not touched: the writes are to
// the session's memory. A digest a registry fix selects without rewrites is an
// error rather than a session that runs with the original code.
func applyCodeRewrites(memory *armcore.Memory, rewrites []codeRewrite, fix, digest string) error {
	if len(rewrites) == 0 {
		return fmt.Errorf("KTF %s compatibility: no code is known for client %s", fix, digest)
	}
	for _, rewrite := range rewrites {
		code := make([]byte, len(rewrite.expect))
		if err := memory.Read(rewrite.at, code); err != nil {
			return fmt.Errorf("KTF %s compatibility: %w", fix, err)
		}
		if !bytes.Equal(code, rewrite.expect) {
			return fmt.Errorf("KTF %s compatibility: unexpected code at %#x", fix, rewrite.at)
		}
	}
	for _, rewrite := range rewrites {
		if len(rewrite.replace) == 0 {
			continue
		}
		if err := memory.Write(rewrite.at+uint32(rewrite.offset), rewrite.replace); err != nil {
			return err
		}
	}
	return nil
}
