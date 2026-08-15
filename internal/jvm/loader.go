package jvm

import (
	"fmt"
	"sort"
	"sync"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

type ClassSource interface {
	ClassBytes(name string) ([]byte, bool)
}

type Loader struct {
	source ClassSource

	mu      sync.RWMutex
	classes map[string]*classfile.Class
	missing map[string]error
}

// Census names every class this loader has resolved and every one it was asked
// for and could not find, with the reason. It is what a Host's diagnostic
// report is built from: the first list is the surface a title actually used,
// which is what says what may be pruned, and the second is what it asked for
// that is not here, which is what says what to implement next.
func (loader *Loader) Census() (loaded []string, missing map[string]string) {
	loader.mu.RLock()
	defer loader.mu.RUnlock()
	loaded = make([]string, 0, len(loader.classes))
	for name := range loader.classes {
		loaded = append(loaded, name)
	}
	sort.Strings(loaded)
	missing = make(map[string]string, len(loader.missing))
	for name, err := range loader.missing {
		if name == ObjectClass {
			// The root class is answered without a class file, so a lookup
			// that reached the loader for it is not a gap. Reporting it as one
			// puts the same false entry at the top of every report.
			continue
		}
		missing[name] = err.Error()
	}
	return loaded, missing
}

func NewLoader(source ClassSource) *Loader {
	return &Loader{
		source:  source,
		classes: make(map[string]*classfile.Class),
		missing: make(map[string]error),
	}
}

// notFound returns the "class not found" error for a name, reusing the one
// already built for it. Native method resolution walks a class chain on every
// call and most of that chain has no class file, so formatting a fresh error
// per miss was one of the busiest allocations in a running session.
func (l *Loader) notFound(name string) error {
	l.mu.RLock()
	cached := l.missing[name]
	l.mu.RUnlock()
	if cached != nil {
		return cached
	}
	err := fmt.Errorf("class not found: %s", name)
	l.mu.Lock()
	if existing := l.missing[name]; existing != nil {
		err = existing
	} else if len(l.missing) < missingClassLimit {
		l.missing[name] = err
	}
	l.mu.Unlock()
	return err
}

// missingClassLimit bounds the remembered misses. Names can come from guest
// memory, so their distinct count is not bounded by the class files; past the
// limit a miss simply formats its error again.
const missingClassLimit = 8192

func (l *Loader) Load(name string) (*classfile.Class, error) {
	l.mu.RLock()
	loaded := l.classes[name]
	l.mu.RUnlock()
	if loaded != nil {
		return loaded, nil
	}

	data, ok := l.source.ClassBytes(name)
	if !ok {
		return nil, l.notFound(name)
	}
	parsed, err := classfile.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("load class %s: %w", name, err)
	}
	if parsed.Name != name {
		return nil, fmt.Errorf("class source returned %s for %s", parsed.Name, name)
	}

	l.mu.Lock()
	if existing := l.classes[name]; existing != nil {
		parsed = existing
	} else {
		l.classes[name] = parsed
	}
	l.mu.Unlock()
	return parsed, nil
}
