package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// A `-public` run writes its keys down, because two other commands need them
// and neither of them is this process.
//
// `stop` needs the admin key: a server reachable through a tunnel cannot tell
// a local caller from a remote one by address alone, so the polite shutdown
// route asks for a secret that is only on this machine, and the run directory
// is where "only on this machine" is spelled. Without it a stop on Windows
// falls through to killing the process outright, which is the one path that
// can lose a save being written.
//
// `status` needs the access key, for a smaller reason that turns out to matter
// just as much: it is how a user gets the link back after closing the window
// they were shown it in.
//
// The access key is kept between runs. A link that stopped working every time
// the server restarted is a link nobody keeps, and the phone that installed
// the page as an app would be the first to break. The admin key is the
// opposite — it is written fresh on every start, since nothing outlives the
// process it belongs to.

// publicState is the file. It is JSON because it is read by a program, and it
// is small because everything else about a run is discoverable from the port.
type publicState struct {
	// Key is the access key the players' link carries.
	Key string `json:"key"`
	// Admin is this run's admin key, for /api/shutdown.
	Admin string `json:"admin"`
	// Port and PID say which run wrote it, so a stale file from a server that
	// is gone is recognisable rather than believed.
	Port int `json:"port"`
	PID  int `json:"pid"`
}

// publicStatePath is where the file lives: beside the logs and whatever else a
// run leaves behind, under the same data root the games and saves are read
// from. A release keeps it next to the executable, a checkout under var/, and
// in both cases the launcher that will read it was started from the same
// place.
func publicStatePath(root string) string {
	return filepath.Join(root, "run", "public.json")
}

// readPublicState reads the file, and answers with a zero state for every
// reason it might not be there — never started, deleted, unreadable, written
// by a newer build. A missing key is not an error at any call site: it means
// this run has to make one, or that the command asking has nothing to say.
func readPublicState(root string) publicState {
	content, err := os.ReadFile(publicStatePath(root))
	if err != nil {
		return publicState{}
	}
	var state publicState
	if err := json.Unmarshal(content, &state); err != nil {
		return publicState{}
	}
	return state
}

// writePublicState records this run. The file is written 0600 because it holds
// both keys, and the directory is created if this is the first run that needed
// one.
func writePublicState(root string, state publicState) error {
	path := publicStatePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("make the run directory: %w", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the run state: %w", err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// newSecret is 128 bits, spelled in the alphabet that survives being a query
// parameter, a cookie value and a line in a chat message without being escaped
// anywhere. That is 22 characters, which is short enough to read out loud once
// and long enough that reading it out loud is the only way anyone gets it.
func newSecret() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// preparePublicState is what a `-public` start does: keep the access key it
// already had, make one if it had none, and stamp the rest with this run.
func preparePublicState(root string, port int) (publicState, error) {
	state := readPublicState(root)
	if state.Key == "" {
		key, err := newSecret()
		if err != nil {
			return publicState{}, err
		}
		state.Key = key
	}
	admin, err := newSecret()
	if err != nil {
		return publicState{}, err
	}
	state.Admin = admin
	state.Port = port
	state.PID = os.Getpid()
	if err := writePublicState(root, state); err != nil {
		return publicState{}, err
	}
	return state, nil
}

// keyedURL is the link a player is given: an address with the key on it, which
// the page trades for a cookie the moment it is followed.
func keyedURL(base, key string) string {
	if base == "" || key == "" {
		return base
	}
	return base + "/?" + url.Values{accessKeyQuery: {key}}.Encode()
}

// accessKeyQuery is the query parameter webhost reads the key from. It is
// spelled here rather than imported because the two sides of it are a link a
// person pastes and a server that reads it, and both are already written down
// in the documentation.
const accessKeyQuery = "k"

// portOf is the listener's port, or zero for a listener that has none — a Unix
// socket, which no link can name anyway.
func portOf(listener net.Listener) int {
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return address.Port
}

// localURL is this machine's own address for a port, which is what the
// browser on it is sent to.
func localURL(port int) string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
