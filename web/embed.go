// Package web carries the browser client as files embedded in the server
// binary. A release is one executable per operating system — the user
// downloads it, drops game archives beside it and leaves it running — so the
// PWA shell travels inside the binary rather than in a directory that has to
// be unpacked next to it.
//
// The shell is all there is to carry: the emulator runs in this same binary and
// the page draws the frames a session sends it.
package web

import (
	"embed"
	"io/fs"
)

//go:embed *.html *.js *.css *.png *.webmanifest
var files embed.FS

// Client returns the embedded PWA shell, rooted where the page expects it:
// index.html at the top.
func Client() fs.FS { return files }
