// Package fonts embeds the runtime's font assets.
//
// NeoDGM (Neo둥근모) is the pixel-grid revival of the classic Korean handset
// bitmap font, licensed under the SIL Open Font License 1.1 (see
// LICENSE-neodgm). The original handset runtime renders text with the same
// face, so glyph shapes stay comparable against reference screenshots. It is
// drawn on a 16-unit em and is exact at 16 pixels, where its Korean glyphs
// stand 11 pixels above the baseline.
//
// Galmuri9 covers the smaller screens, where NeoDGM cannot: three quarters of a
// 16-unit grid puts every stroke on a half pixel, and the 176x220 titles need a
// face that is small by design rather than by scaling. It is exact at 10
// pixels, where its Korean glyphs stand 9 above the baseline — the same ink
// NeoDGM has when scaled to 12, which is the size those titles lay out for.
// Licensed under the SIL Open Font License 1.1 (see LICENSE-galmuri).
//
// The number in a Galmuri name is the ink height, not the em: Galmuri11 draws
// 11-pixel glyphs on a 12-pixel em, which is as tall as NeoDGM at 16 and only
// narrower. Galmuri9 is the one that is actually smaller.
//
// Galmuri is not the face those handsets carried. It is a modern pixel font
// whose shapes come from the Nintendo DS rather than from any Korean phone, and
// it is here because it is the one small Korean face on hand that is exact at
// its design size and free to redistribute. Text drawn with it is legible and
// correctly sized; it is not evidence about what the original looked like, and
// screenshots of the 176x220 titles are not comparable against a reference the
// way the 240x320 ones are.
package fonts

import _ "embed"

//go:embed neodgm.ttf
var NeoDGM []byte

//go:embed galmuri9.ttf
var Galmuri9 []byte
