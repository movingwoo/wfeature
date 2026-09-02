// Command lib is the emulator as a static library, for an app to link.
//
// **iOS does not let an app start a process.** The Android app runs the same
// binary a desktop runs, as a child; here there is no such thing, so the Go
// code is compiled into an archive (`-buildmode=c-archive`), linked into the
// app, and called. These three functions are the whole of the surface, and
// they exist because C is the only language the two sides share.
//
// The `main` below is never called — an archive has no entry point — but a Go
// program that produces one has to be package main all the same.
package main

import "C"

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/movingwoo/wfeature/internal/appserver"
)

// running is the one server this library will start. An app that asked twice
// would otherwise take a second port and leave the first one serving, and the
// second web view would be looking at a different library from the first.
var (
	mu      sync.Mutex
	running *appserver.Server
	failure string
)

// WfeatureStart starts the server under a directory and answers with the port
// it took, or 0 if it could not start. The reason is left where
// WfeatureLastError can read it, because a C function that answers with a port
// has nowhere to put a sentence.
//
//export WfeatureStart
func WfeatureStart(root *C.char) C.int {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		return C.int(running.Port())
	}

	directory := C.GoString(root)
	// The app hands over its own container, and the tree under it is the same
	// one a desktop keeps beside the executable.
	for _, name := range []string{"games", filepath.Join("savedata", "ktf"), "logs"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0o755); err != nil {
			failure = err.Error()
			return 0
		}
	}

	server, err := appserver.Start(appserver.Options{Root: directory})
	if err != nil {
		failure = err.Error()
		return 0
	}
	running = server
	failure = ""
	return C.int(server.Port())
}

// WfeatureStop drains the server. An app calls it when it is torn down; a save
// being written is what the drain is for.
//
//export WfeatureStop
func WfeatureStop() {
	mu.Lock()
	defer mu.Unlock()
	if running == nil {
		return
	}
	_ = running.Close()
	running = nil
}

// WfeatureLastError is why a start answered with 0. The string belongs to Go
// and stays valid until the next call, which is long enough for the caller to
// show it.
//
//export WfeatureLastError
func WfeatureLastError() *C.char {
	mu.Lock()
	defer mu.Unlock()
	if failure == "" {
		return nil
	}
	return C.CString(failure)
}

func main() {}
