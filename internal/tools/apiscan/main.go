// Command apiscan reports, per archive, the platform surface a title links
// against that this runtime does not answer — without playing it.
//
// It is worth having beside a `-diag` report because the two answer different
// questions. A report says what a title asked for before it stopped, which is
// one gap per run and a rebuild between each; this says what it would ask for
// if it got that far, so a session can be planned from one pass over a corpus.
// Scanned over a whole vendor's archives at once it also ranks the gaps by how
// many titles want each one, which is the order worth implementing in.
//
//	go run ./internal/tools/apiscan -games var/games/skt
//	go run ./internal/tools/apiscan -games var/games/skt -natives report.json
//	go run ./internal/tools/apiscan -games var/games/ktf
//	go run ./internal/tools/apiscan -games var/games/lgt
//
// Each archive is scanned as the platform it belongs to, so a mixed directory
// is one pass. What "links against" means is not the same on all three, and
// neither is the cost:
//
//   - An SKT title is Java, so every reference it makes is in its own constant
//     pools. Reading them is the whole scan and nothing runs.
//   - A KTF title is AOT-compiled to ARM, and what survives of its references
//     is the name pool inside the client image: the platform classes it names.
//     Nothing runs here either, and the method half of the question is gone —
//     see ktfPlatformNames.
//   - An LGT title is a native module whose imports exist nowhere but the calls
//     it makes while it starts, so this scan starts it and reads back what it
//     resolved. It is one boot per archive rather than a read.
//
// The `-natives` report is any `runskt -diag` output: the SKT platform
// registers part of its surface on a live runtime rather than in a
// declaration, and a bare VM cannot see those registrations. Without it the
// scan over-reports exactly that half.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/movingwoo/wfeature/internal/platform/detect"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/lgt"
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
		gaps, err := scanArchive(shape, path)
		if errors.Is(err, errNoNamePool) {
			fmt.Printf("%-28s not scanned: %v\n", title, err)
			continue
		}
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

// scanArchive scans one archive as the platform it belongs to. Detection is
// the archive's own shape rather than the directory it was found in, so a
// mixed directory scans in one pass and a misfiled archive is scanned
// correctly rather than reported as broken.
func scanArchive(shape *surface, path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	platform, err := detect.Archive(data)
	if err != nil {
		return nil, err
	}
	switch platform {
	case detect.KTF:
		return scanKTF(data)
	case detect.LGT:
		return scanLGT(data)
	case detect.SKT:
		return scan(shape, data)
	}
	return nil, fmt.Errorf("no platform claims this archive")
}

// scan reads every class in the archive's JAR and returns the references that
// name a class outside the archive which this runtime does not answer.
func scan(shape *surface, data []byte) (map[string]string, error) {
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

// scanKTF reports the platform classes a KTF title names that this runtime
// does not publish.
//
// A KTF title is AOT-compiled ARM rather than class files, so there are no
// constant pools to read. What is left of its references is the name pool
// inside the client image — the strings the guest hands to the platform's own
// class lookup — and those are read straight out of the file: relocation moves
// them but does not write them, so nothing has to run.
func scanKTF(data []byte) (map[string]string, error) {
	// The earlier package is the same vendor and the same platform, so
	// detection names it KTF, but it has no client image and therefore no name
	// pool to read: its classes are gone into the compile the way the
	// descriptor package's methods are. There is nothing here to be wrong
	// about, which is not the same as nothing being missing, so it is said
	// rather than counted as a title with no gaps.
	if ktf.IsNativeArchive(data) {
		return nil, errNoNamePool
	}
	archive, err := ktf.Open(data)
	if err != nil {
		return nil, err
	}
	answered := ktfAnswered()
	gaps := map[string]string{}
	for _, name := range ktfPlatformNames(archive.JAR.Client.Data) {
		if !answered[name] {
			gaps[name] = "class"
		}
	}
	return gaps, nil
}

// errNoNamePool says the archive is sound and simply carries nothing this scan
// can read, which is a different answer from both a gap and a failure.
var errNoNamePool = errors.New("the earlier KTF package carries no name pool")

// ktfAnswered is every class name a KTF guest can name and get a real class
// back for: the platform's own published table, and the core library the JVM
// carries underneath it, which is what a title's `catch` and its `new` on an
// exception type resolve through.
func ktfAnswered() map[string]bool {
	answered := map[string]bool{}
	for _, name := range ktf.RuntimeJavaClassNames() {
		answered[name] = true
	}
	for _, definition := range jvm.CoreLibraryDefinitions() {
		answered[definition.Name] = true
	}
	return answered
}

// ktfPlatformNames reads the class names out of a client image's name pool.
//
// The pool is a run of NUL-terminated strings holding everything the image
// names by text: class names on their own, and method entries of the form
// `<descriptor>+<name>`, whose descriptor carries the classes a signature
// mentions. Both are read, because a class a title only ever passes as an
// argument appears in no other form.
//
// **Only the method half of the question is lost.** A pool entry says which
// methods exist by name and descriptor but not which class each one belongs
// to — the pairing is in the class records the image builds at runtime — so a
// missing method on a class this platform does publish is not something this
// scan can see. It is the half a KTF corpus loses by being compiled.
//
// The pool also holds strings that are not class names at all: resource paths,
// which are slash-separated too, and the title's own packages. Both are
// dropped by asking whether the name sits in a package this platform publishes
// classes in, which is exactly the set worth reporting a hole in.
func ktfPlatformNames(image []byte) []string {
	packages := map[string]bool{}
	for name := range ktfAnswered() {
		if index := strings.LastIndex(name, "/"); index > 0 {
			packages[name[:index]] = true
		}
	}
	found := map[string]bool{}
	keep := func(name string) {
		name = strings.TrimLeft(name, "[")
		name = strings.TrimSuffix(strings.TrimPrefix(name, "L"), ";")
		if !plainClassName(name) {
			return
		}
		index := strings.LastIndex(name, "/")
		if index <= 0 || !packages[name[:index]] {
			return
		}
		found[name] = true
	}
	for _, entry := range poolStrings(image) {
		keep(entry)
		// A descriptor names its classes as `Lpackage/Class;`, and the entry
		// holding it is the whole method signature rather than a bare name.
		for rest := entry; ; {
			start := strings.IndexByte(rest, 'L')
			if start < 0 {
				break
			}
			end := strings.IndexByte(rest[start:], ';')
			if end < 0 {
				break
			}
			keep(rest[start : start+end+1])
			rest = rest[start+end+1:]
		}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// poolStrings returns every printable NUL-terminated run in an image that is
// shaped like a name: at least one slash, and nothing in it that a class name
// or a descriptor cannot hold. The shape is what keeps the guest's own text —
// messages, formats, file names — out of the answer.
func poolStrings(image []byte) []string {
	var entries []string
	start := -1
	for index := 0; index <= len(image); index++ {
		if index < len(image) && image[index] >= 0x20 && image[index] < 0x7f {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 && index < len(image) && image[index] == 0 {
			if entry := string(image[start:index]); strings.Contains(entry, "/") && nameShaped(entry) {
				entries = append(entries, entry)
			}
		}
		start = -1
	}
	return entries
}

// plainClassName reports whether what is left after the array and descriptor
// decoration have come off is a class name and nothing else. A pool entry
// joins a descriptor to a member's name with `+`, so the entry as a whole is
// only a class name when it carries none of that.
func plainClassName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '/', character == '_', character == '$':
		default:
			return false
		}
	}
	return true
}

// nameShaped reports whether a string holds only what a class name or a method
// entry can: the characters of an identifier, the separators a package and a
// descriptor use, and the `+` a name entry joins its two halves with.
func nameShaped(entry string) bool {
	for index := 0; index < len(entry); index++ {
		character := entry[index]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9':
		case strings.IndexByte("/_$;()[+<>", character) >= 0:
		default:
			return false
		}
	}
	return true
}

// scanLGT reports the platform slots an LGT module resolves that this runtime
// does not implement.
//
// Unlike the other two this one runs the title — as far as its initializer,
// which is where a module resolves every platform function it might ever call.
// Nothing else in the archive lists them: the ELF carries no dynamic symbols,
// and a slot is a pair of numbers passed to a callback. A startup that fails
// still resolved everything it got to, so what it did resolve is reported
// either way and the failure is noted on stderr.
func scanLGT(data []byte) (map[string]string, error) {
	archive, err := lgt.Open(data)
	if err != nil {
		return nil, err
	}
	client, err := lgt.Load(archive, lgt.Options{})
	if err != nil {
		return nil, err
	}
	if err := client.Start(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "  start: %v\n", err)
	}
	gaps := map[string]string{}
	for _, record := range client.ResolvedImports() {
		if record.Implemented {
			continue
		}
		gaps[record.Describe()] = "slot"
	}
	return gaps, nil
}
