package lgt

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// The wide ABI has four signed clip words and independent signed translations.
// Select it by verified code revision, never by treating a coordinate as a code
// pointer. Compact contexts remain unchanged. See docs/lgt-qa-2026-09-20.md.
const wideContextSize = 56

func hasWideGraphicsContexts(module []byte) bool {
	digest := sha256.Sum256(module)
	return compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTWideGraphicsContext)
}

func hasWideExclusiveClip(module []byte) bool {
	digest := sha256.Sum256(module)
	return compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTWideExclusiveClip)
}

func (client *Client) initWideContext(pointer uint32) int32 {
	if pointer == 0 {
		return wipiError
	}
	words := [wideContextSize / 4]uint32{0, 0, uint32(client.screen.width - 1), uint32(client.screen.height - 1), 0xffff, 0, 0xff}
	if client.wideExclusiveClip {
		words[2]++
		words[3]++
	}
	for i, word := range words {
		if err := client.writeWord(pointer+uint32(i)*4, word); err != nil {
			return wipiError
		}
	}
	return wipiSuccess
}

func (client *Client) transferWideContext(pointer, field, value, slot uint32) int32 {
	var offset, count uint32
	switch field {
	case grpFieldClip:
		offset, count = 0, 4
	case grpFieldForeground:
		offset, count = 16, 1
	case grpFieldBackground:
		offset, count = 20, 1
	case grpFieldAlpha:
		offset, count = 24, 1
	case grpFieldPixelOp:
		offset, count = 28, 1
	case grpFieldParam1:
		offset, count = 32, 1
	case grpFieldFont:
		offset, count = 36, 1
	case grpFieldStyle:
		offset, count = 40, 1
	case grpFieldXorMode:
		offset, count = 44, 1
	case grpFieldOffset:
		offset, count = 48, 2
	default:
		return wipiSuccess
	}
	for i := uint32(0); i < count; i++ {
		address := pointer + offset + i*4
		if slot == slotGetContext {
			word, err := client.readWord(address)
			if err != nil {
				return wipiError
			}
			if field == grpFieldClip && i >= 2 && !client.wideExclusiveClip {
				word++
			}
			if err := client.writeWord(value+i*4, word); err != nil {
				return wipiError
			}
		} else {
			word := value
			if count > 1 {
				var err error
				word, err = client.readWord(value + i*4)
				if err != nil {
					return wipiError
				}
			}
			if field == grpFieldClip && i >= 2 && !client.wideExclusiveClip {
				word--
			}
			if err := client.writeWord(address, word); err != nil {
				return wipiError
			}
		}
	}
	return wipiSuccess
}

func (client *Client) readWideContext(context *graphicsContext, pointer uint32) (*graphicsContext, error) {
	var words [wideContextSize / 4]uint32
	for i := range words {
		word, err := client.readWord(pointer + uint32(i)*4)
		if err != nil {
			return nil, err
		}
		words[i] = word
	}
	edge := int64(1)
	if client.wideExclusiveClip {
		edge = 0
	}
	context.clipX, context.clipY = int(int32(words[0])), int(int32(words[1]))
	context.clipWidth = int(int64(int32(words[2])) - int64(int32(words[0])) + edge)
	context.clipHeight = int(int64(int32(words[3])) - int64(int32(words[1])) + edge)
	context.foreground, context.background = uint16(words[4]), uint16(words[5])
	context.offsetX, context.offsetY = int(int32(words[12])), int(int32(words[13]))
	context.xor = words[11] != 0
	// These callers install callbacks with direct stores. Their globals can
	// change between draws, so cached pixel pairs must not outlive this draw.
	client.pixelOps = nil
	client.installPixelOp(words[7])
	context.op = client.readContextPixelOp(words[7], words[8])
	return context, nil
}
