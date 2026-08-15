package lgt

import (
	"fmt"
	"sort"
	"strings"
)

// The platform API surface a Java module links against, as the load call hands
// it over. Six tables come in and five go out; see docs/lgt.md, "The Java
// interface table".
//
// The five member tables are pairs of `char *` — a name and a descriptor —
// and they are **one contiguous run of memory** in the argument's own order,
// which is what gives each of them a length: a table ends where the next one
// begins. Only the last table in memory has to be measured by reading it, and
// that one stops at the first entry that is not a pair of names.
//
// The class table names which platform class owns which run of each member
// table. It does not cover the whole of them: the tables carry the
// application's own fields and virtual methods too, because those are the
// members whose position depends on how large the platform made the class the
// application ultimately extends.

// javaMemberRef is one entry of a member table.
type javaMemberRef struct {
	Name       string
	Descriptor string
}

func (member javaMemberRef) String() string { return member.Name + member.Descriptor }

// javaRun is one class's stretch of a member table: a count in the high half
// of the word and a start index in the low half.
type javaRun struct {
	Start uint32
	Count uint32
}

func javaRunOf(word uint32) javaRun {
	return javaRun{Start: word & 0xffff, Count: word >> 16}
}

func (run javaRun) contains(index uint32) bool {
	return run.Count != 0 && index >= run.Start && index < run.Start+run.Count
}

// javaAPIClass is one platform class the module expects this runtime to have.
type javaAPIClass struct {
	Name           string
	StaticFields   javaRun
	VirtualMethods javaRun
	Methods        javaRun
	StaticMethods  javaRun
}

// javaSurface is the whole of what the load call hands over, plus where the
// answers go.
type javaSurface struct {
	Classes []javaAPIClass

	Fields         []javaMemberRef
	StaticFields   []javaMemberRef
	VirtualMethods []javaMemberRef
	Methods        []javaMemberRef
	StaticMethods  []javaMemberRef

	// The output arrays, in the order the call passes them: one per member
	// table, and none for the class table. Four are int16; the static-method
	// one is a table of function pointers.
	FieldsOut         uint32
	StaticFieldsOut   uint32
	VirtualMethodsOut uint32
	MethodsOut        uint32
	StaticMethodsOut  uint32
}

const (
	// A member table longer than this is a misread rather than a title.
	maxJavaMemberTable = 4096
	maxJavaAPIClasses  = 1024
)

// readJavaSurface reads the six input tables. The arguments arrive in the
// order the call makes them.
func (client *Client) readJavaSurface(arguments []uint32) (*javaSurface, error) {
	if len(arguments) != 11 {
		return nil, fmt.Errorf("the java load call takes 11 arguments, not %d", len(arguments))
	}
	classes, tables := arguments[0], arguments[1:6]
	surface := &javaSurface{
		FieldsOut:         arguments[6],
		StaticFieldsOut:   arguments[7],
		VirtualMethodsOut: arguments[8],
		MethodsOut:        arguments[9],
		StaticMethodsOut:  arguments[10],
	}

	// A table ends where the next one starts. The bases are sorted rather than
	// assumed to be in argument order, so a module that lays them out
	// differently is measured rather than misread; the class table bounds
	// whichever member table sits highest.
	bounds := make([]uint32, 0, len(tables)+1)
	bounds = append(bounds, classes)
	bounds = append(bounds, tables...)
	sort.Slice(bounds, func(a, b int) bool { return bounds[a] < bounds[b] })
	limitOf := func(base uint32) uint32 {
		for _, bound := range bounds {
			if bound > base {
				return bound
			}
		}
		return 0
	}

	for index, base := range tables {
		members, err := client.readJavaMemberTable(base, limitOf(base))
		if err != nil {
			return nil, fmt.Errorf("java member table %d at %#x: %w", index, base, err)
		}
		switch index {
		case 0:
			surface.Fields = members
		case 1:
			surface.StaticFields = members
		case 2:
			surface.VirtualMethods = members
		case 3:
			surface.Methods = members
		case 4:
			surface.StaticMethods = members
		}
	}
	if err := client.readJavaAPIClasses(classes, surface); err != nil {
		return nil, err
	}
	return surface, nil
}

// readJavaMemberTable reads pairs of name and descriptor pointers up to the
// next table's base, stopping early at the first entry that is not a pair of
// printable strings. The limit is what keeps a misjudged end from walking the
// heap; the stop is what keeps the padding between two tables out of the list.
func (client *Client) readJavaMemberTable(base, limit uint32) ([]javaMemberRef, error) {
	if base == 0 || base&3 != 0 {
		return nil, fmt.Errorf("table address %#x is null or unaligned", base)
	}
	count := uint32(maxJavaMemberTable)
	if limit > base {
		count = (limit - base) / 8
	}
	if count > maxJavaMemberTable {
		count = maxJavaMemberTable
	}
	members := make([]javaMemberRef, 0, count)
	for index := uint32(0); index < count; index++ {
		name, err := client.readWord(base + index*8)
		if err != nil {
			break
		}
		descriptor, err := client.readWord(base + index*8 + 4)
		if err != nil {
			break
		}
		// A null half is a real entry. Every platform class's run opens with
		// two entries that are null on both sides, and one local title has an
		// entry with a descriptor and no name. Only a pointer that is neither
		// null nor a string ends the table.
		text, nameOK := client.readPrintableString(name)
		descriptorText, descriptorOK := client.readPrintableString(descriptor)
		if (name != 0 && !nameOK) || (descriptor != 0 && !descriptorOK) {
			break
		}
		members = append(members, javaMemberRef{Name: text, Descriptor: descriptorText})
	}
	return members, nil
}

// readJavaAPIClasses reads the class table: a count, then 24-byte entries. The
// runs are checked against the tables they index, because a run that does not
// fit is a misread of the whole call rather than one bad class.
func (client *Client) readJavaAPIClasses(base uint32, surface *javaSurface) error {
	if base == 0 || base&3 != 0 {
		return fmt.Errorf("java class table address %#x is null or unaligned", base)
	}
	count, err := client.readWord(base)
	if err != nil {
		return err
	}
	if count == 0 || count > maxJavaAPIClasses {
		return fmt.Errorf("java class count %#x at %#x is not a count", count, base)
	}
	surface.Classes = make([]javaAPIClass, 0, count)
	for index := uint32(0); index < count; index++ {
		entry := base + 4 + index*24
		words := make([]uint32, 6)
		for word := range words {
			if words[word], err = client.readWord(entry + uint32(word)*4); err != nil {
				return err
			}
		}
		name, ok := client.readPrintableString(words[0])
		if !ok {
			return fmt.Errorf("java class %d at %#x has no name", index, entry)
		}
		class := javaAPIClass{
			Name:           name,
			StaticFields:   javaRunOf(words[2]),
			VirtualMethods: javaRunOf(words[3]),
			Methods:        javaRunOf(words[4]),
			StaticMethods:  javaRunOf(words[5]),
		}
		for _, check := range []struct {
			label string
			run   javaRun
			table []javaMemberRef
		}{
			{"static fields", class.StaticFields, surface.StaticFields},
			{"virtual methods", class.VirtualMethods, surface.VirtualMethods},
			{"methods", class.Methods, surface.Methods},
			{"static methods", class.StaticMethods, surface.StaticMethods},
		} {
			if uint64(check.run.Start)+uint64(check.run.Count) > uint64(len(check.table)) {
				return fmt.Errorf("java class %s claims %s %d..%d of a table of %d",
					name, check.label, check.run.Start,
					check.run.Start+check.run.Count-1, len(check.table))
			}
		}
		surface.Classes = append(surface.Classes, class)
	}
	return nil
}

// ownerOf answers which platform class a member table entry belongs to, and
// whether any does — the entries past the platform's own runs are the
// application's, and those have no class here.
func (surface *javaSurface) ownerOf(pick func(javaAPIClass) javaRun, index uint32) (string, bool) {
	for _, class := range surface.Classes {
		if pick(class).contains(index) {
			return class.Name, true
		}
	}
	return "", false
}

// describeJavaSurface reports what the load call handed over, one line per
// class and a count per table, so a debug run says what a title expects this
// runtime to provide.
func describeJavaSurface(surface *javaSurface) []string {
	lines := make([]string, 0, len(surface.Classes)+1)
	lines = append(lines, fmt.Sprintf(
		"%d classes, %d fields, %d static fields, %d virtual methods, %d methods, %d static methods",
		len(surface.Classes), len(surface.Fields), len(surface.StaticFields),
		len(surface.VirtualMethods), len(surface.Methods), len(surface.StaticMethods)))
	for _, class := range surface.Classes {
		members := make([]string, 0, 8)
		for _, part := range []struct {
			label string
			run   javaRun
			table []javaMemberRef
		}{
			{"static field", class.StaticFields, surface.StaticFields},
			{"virtual", class.VirtualMethods, surface.VirtualMethods},
			{"method", class.Methods, surface.Methods},
			{"static", class.StaticMethods, surface.StaticMethods},
		} {
			for index := part.run.Start; index < part.run.Start+part.run.Count; index++ {
				member := part.table[index]
				if member.Name == "" && member.Descriptor == "" {
					continue
				}
				members = append(members, fmt.Sprintf("%s %d %s", part.label, index, member))
			}
		}
		lines = append(lines, fmt.Sprintf("%s [%s]", class.Name, strings.Join(members, " ")))
	}
	return lines
}
