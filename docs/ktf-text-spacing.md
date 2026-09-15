# KTF Java text spacing

The guest draws a white sentence and then a colored string at the same origin
and anchor. The colored pass replaces each uncolored Korean syllable with two
ASCII spaces and each uncolored ASCII character with one space. It does not
calculate a separate emphasis coordinate using `Font.stringWidth`.

A temporary trace captured a white prefix containing six Korean syllables and
two spaces, followed by the emphasized word. The colored pass starts with
fourteen spaces. Both calls use `(11, 274)` and anchor `0`. The handset face
advances Korean syllables by ten pixels but its font asset advances spaces by
four. The white prefix occupied 68 pixels and the colored prefix 56, placing
the emphasis twelve pixels too far left.

An initial five-pixel space correction removed that accumulated displacement.
Follow-up screenshots exposed a smaller fringe after punctuation: the font
asset advances both period and exclamation mark by four pixels, whereas their
replacement space now advanced by five. The traced second dialogue line
contains a period before another emphasized word, explaining its one-pixel
rightward displacement. Narrow Latin letters and proportional digits have the
same mismatch with a one-space replacement.

KTF WIPI Java drawing and font measurement now use five-pixel cells for
printable ASCII (`U+0020..U+007E`). Korean syllables retain their ten-pixel
advance. Glyph shapes, control-character advances, other Unicode advances,
and the shared font asset are unchanged. Native WIPI-C text and other
platforms retain their existing metrics.

The [WIPI Font contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Font.md)
requires width measurement to describe rendered text. It does not prescribe
these exact pixel widths. The cell-spacing requirement comes from the observed
guest overlay algorithm. Character, string and range measurements, anchoring,
and drawing use the same Java advance function.

`TestJavaTextOverlayUsesHalfWidthSpaces` and `TestJavaTextOverlayAfterASCII`
compare overlaid text with independently positioned colored glyphs. The ASCII
regression failed before the follow-up correction for punctuation, digits and
Latin letters and passes afterward. `TestJavaASCIICellMetrics` checks the public
character-width handler for every printable ASCII character and preserves
representative control and non-ASCII advances.

The replay `var/logs/ktf-overlay.route` (`-hold 5 -ticks 10000`) reaches the
instruction dialogue in the first follow-up screenshot. The final capture is
`var/logs/ktf-render-revised/quiz8.png`; the original proportional rendering and
the intermediate space-only correction are retained in
`ktf-overlay-full-before/quiz8.png` and `ktf-overlay-full-after/quiz8.png`.
The other two follow-up scenes were inspected in supplied screenshots, but
were not navigated separately; their punctuation case is covered by the
synthetic pixel regression. The final and intermediate frames differ at 358
pixels within `x=105..182, y=288..309`; the surrounding scene is identical.


## Validation

Pixel regressions and local CLI dialogue replay passed. The isolated KTF branch passed `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...`.
The two later scenes were not replayed individually, and no new Safari check
was performed. Original assets and saves remain outside Git.
