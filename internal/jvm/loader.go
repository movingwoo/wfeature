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

	mu sync.RWMutex
	// defined holds the classes the runtime declares in Go rather than as
	// class files. They are consulted before the source for the reason the
	// core library precedes application archives: a game that ships its own
	// javax/microedition/lcdui/Canvas must not replace the runtime's.
	defined map[string]*classfile.Class
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
		defined: make(map[string]*classfile.Class),
		classes: make(map[string]*classfile.Class),
		missing: make(map[string]error),
	}
}

// Define installs class metadata the runtime built in Go. Defining a name
// twice is a mistake rather than an override, and so is defining one that has
// already been loaded, because code may already be linked against the class
// the loader handed out.
func (l *Loader) Define(class *classfile.Class) error {
	if class == nil || class.Name == "" {
		return fmt.Errorf("defined class has no name")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.defined[class.Name]; exists {
		return fmt.Errorf("class already defined: %s", class.Name)
	}
	if _, loaded := l.classes[class.Name]; loaded {
		return fmt.Errorf("class already loaded: %s", class.Name)
	}
	l.defined[class.Name] = class
	delete(l.missing, class.Name)
	return nil
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
	defined := l.defined[name]
	l.mu.RUnlock()
	if loaded != nil {
		return loaded, nil
	}
	if defined != nil {
		// Recording the hit keeps Census meaning "what a title actually
		// reached" for defined classes too, which is what says which of them
		// a platform still needs.
		l.mu.Lock()
		if existing := l.classes[name]; existing != nil {
			defined = existing
		} else {
			l.classes[name] = defined
		}
		l.mu.Unlock()
		return defined, nil
	}

	if l.source == nil {
		// A VM may be built with no archive behind it — a platform that only
		// serves its own classes, or a test — and then the definitions above
		// are the whole class surface.
		return nil, l.notFound(name)
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
