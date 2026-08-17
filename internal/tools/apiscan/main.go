// Command apiscan reads every class in a MIDlet archive and reports the
// library classes, methods and fields the title links against that this
// runtime does not answer.
//
// It is a static read — nothing runs — which is what makes it worth having
// beside `runskt -diag`. A report says what a title asked for before it
// stopped; this says what it would ask for if it got that far, so a session
// can be planned from one pass over the library instead of from a run, a fix,
// and another run each time. A whole vendor's corpus scanned at once also
// ranks the gaps by how many titles want them, which is the order worth
// implementing in.
//
//	go run ./internal/tools/apiscan -games var/games/skt
//	go run ./internal/tools/apiscan -games var/games/skt -natives report.json
//
// The `-natives` report is any `runskt -diag` output: a platform registers
// part of its surface on a live runtime rather than in a declaration, and a
// bare VM cannot see those registrations. Without it the scan over-reports
// exactly that half.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

type surface struct {
	classes    map[string]jvm.ClassDefinition
	vm         *jvm.VM
	registered map[string]bool
}

func newSurface() *surface {
	definitions := append([]jvm.ClassDefinition(nil), jvm.CoreLibraryDefinitions()...)
	definitions = append(definitions, midp.Definitions()...)
	definitions = append(definitions, skvm.Definitions()...)
	classes := make(map[string]jvm.ClassDefinition, len(definitions))
	for _, definition := range definitions {
		classes[definition.Name] = definition
	}
	machine := jvm.New(jvm.ClassSources{}, jvm.Options{})
	_ = midp.Define(machine)
	_ = skvm.Define(machine)
	return &surface{classes: classes, vm: machine, registered: map[string]bool{}}
}

// loadRegistered adds the natives a running platform registers, which a bare
// VM does not carry. They come out of a `runskt -diag` report: its natives map
// is keyed the same way this scan keys a reference.
func (s *surface) loadRegistered(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var report struct {
		Natives map[string]uint64 `json:"natives"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return err
	}
	for key := range report.Natives {
		s.registered[key] = true
	}
	return nil
}

func (s *surface) hasClass(name string) bool {
	if strings.HasPrefix(name, "[") {
		return true
	}
	if name == "java/lang/Object" {
		return true
	}
	if _, ok := s.classes[name]; ok {
		return true
	}
	for key := range s.registered {
		if strings.HasPrefix(key, name+".") {
			return true
		}
	}
	return false
}

func (s *surface) hasMethod(class, name, descriptor string) bool {
	if strings.HasPrefix(class, "[") {
		class = "java/lang/Object"
	}
	for current := class; current != ""; {
		definition, ok := s.classes[current]
		if !ok {
			break
		}
		for _, method := range definition.Methods {
			if method.Name == name && method.Descriptor == descriptor {
				return true
			}
		}
		for _, iface := range definition.Interfaces {
			if s.hasMethod(iface, name, descriptor) {
				return true
			}
		}
		current = definition.SuperName
	}
	if objectMethod(name, descriptor) {
		return true
	}
	if s.registered[class+"."+name+descriptor] {
		return true
	}
	return s.vm.HasMethodBody(class, name, descriptor)
}

func (s *surface) hasField(class, name, descriptor string) bool {
	for current := class; current != ""; {
		definition, ok := s.classes[current]
		if !ok {
			return false
		}
		for _, field := range definition.Fields {
			if field.Name == name && field.Descriptor == descriptor {
				return true
			}
		}
		current = definition.SuperName
	}
	return false
}

// objectMethod covers what every class inherits from java/lang/Object, which
// the runtime answers in the interpreter rather than through a declaration.
func objectMethod(name, descriptor string) bool {
	switch name + descriptor {
	case "<init>()V", "equals(Ljava/lang/Object;)Z", "hashCode()I",
		"toString()Ljava/lang/String;", "getClass()Ljava/lang/Class;",
		"notify()V", "notifyAll()V", "wait()V", "wait(J)V", "wait(JI)V":
		return true
	}
	return false
}

type gap struct {
	kind string // "class", "method", "field"
	key  string
	from map[string]bool
}

func main() {
	games := flag.String("games", "var/games/skt", "directory of archives to scan")
	natives := flag.String("natives", "", "a runskt -diag report whose natives map lists the platform's registrations")
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		entries, err := os.ReadDir(*games)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				paths = append(paths, filepath.Join(*games, entry.Name()))
			}
		}
	}

	shape := newSurface()
	if *natives != "" {
		if err := shape.loadRegistered(*natives); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	total := map[string]*gap{}
	for _, path := range paths {
		title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		gaps, err := scan(shape, path)
		if err != nil {
			fmt.Printf("%-28s ERROR %v\n", title, err)
			continue
		}
		fmt.Printf("%-28s %d gaps\n", title, len(gaps))
		for _, key := range sortedKeys(gaps) {
			if total[key] == nil {
				total[key] = &gap{kind: gaps[key], key: key, from: map[string]bool{}}
			}
			total[key].from[title] = true
		}
	}

	fmt.Println()
	fmt.Println("== every gap, by how many titles ask for it ==")
	all := make([]*gap, 0, len(total))
	for _, entry := range total {
		all = append(all, entry)
	}
	sort.Slice(all, func(i, j int) bool {
		if len(all[i].from) != len(all[j].from) {
			return len(all[i].from) > len(all[j].from)
		}
		return all[i].key < all[j].key
	})
	for _, entry := range all {
		titles := make([]string, 0, len(entry.from))
		for title := range entry.from {
			titles = append(titles, title)
		}
		sort.Strings(titles)
		fmt.Printf("%2d  %-6s %-70s %s\n", len(entry.from), entry.kind, entry.key, strings.Join(titles, " "))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// scan reads every class in the archive's JAR and returns the references that
// name a class outside the archive which this runtime does not answer.
func scan(shape *surface, path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	classes := map[string]*classfile.Class{}
	collect := func(jar []byte) error {
		inner, err := zip.NewReader(bytes.NewReader(jar), int64(len(jar)))
		if err != nil {
			return err
		}
		for _, file := range inner.File {
			if !strings.HasSuffix(file.Name, ".class") {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return err
			}
			body, err := io.ReadAll(opened)
			opened.Close()
			if err != nil {
				return err
			}
			parsed, err := classfile.Parse(body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  parse %s: %v\n", file.Name, err)
				continue
			}
			classes[parsed.Name] = parsed
		}
		return nil
	}
	found := false
	for _, file := range reader.File {
		name := strings.ToLower(file.Name)
		if strings.HasSuffix(name, ".jar") {
			opened, err := file.Open()
			if err != nil {
				return nil, err
			}
			jar, err := io.ReadAll(opened)
			opened.Close()
			if err != nil {
				return nil, err
			}
			if err := collect(jar); err != nil {
				return nil, err
			}
			found = true
		}
	}
	if !found {
		if err := collect(data); err != nil {
			return nil, err
		}
	}
	if len(classes) == 0 {
		return nil, fmt.Errorf("no classes")
	}

	gaps := map[string]string{}
	for _, class := range classes {
		for _, constant := range class.ConstantPool {
			switch constant.Tag {
			case classfile.ConstantClass:
				name, err := class.ConstantPool.ClassName(constant.Index1)
				if err != nil || classes[name] != nil {
					continue
				}
				if !shape.hasClass(name) && !strings.HasPrefix(name, "[") {
					gaps[name] = "class"
				}
			case classfile.ConstantMethodRef, classfile.ConstantInterfaceMethodRef, classfile.ConstantFieldRef:
				reference, err := poolReference(class.ConstantPool, constant)
				if err != nil || classes[reference.Class] != nil {
					continue
				}
				if !shape.hasClass(reference.Class) {
					gaps[reference.Class] = "class"
				}
				key := reference.Class + "." + reference.Name + reference.Descriptor
				if constant.Tag == classfile.ConstantFieldRef {
					if !shape.hasField(reference.Class, reference.Name, reference.Descriptor) {
						gaps[key] = "field"
					}
					continue
				}
				if !shape.hasMethod(reference.Class, reference.Name, reference.Descriptor) {
					gaps[key] = "method"
				}
			}
		}
	}
	return gaps, nil
}

func poolReference(pool classfile.ConstantPool, constant classfile.Constant) (classfile.Reference, error) {
	className, err := pool.ClassName(constant.Index1)
	if err != nil {
		return classfile.Reference{}, err
	}
	nameAndType, err := pool.At(constant.Index2)
	if err != nil {
		return classfile.Reference{}, err
	}
	name, err := pool.UTF8At(nameAndType.Index1)
	if err != nil {
		return classfile.Reference{}, err
	}
	descriptor, err := pool.UTF8At(nameAndType.Index2)
	if err != nil {
		return classfile.Reference{}, err
	}
	return classfile.Reference{Class: className, Name: name, Descriptor: descriptor}, nil
}
