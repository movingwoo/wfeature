// ktfdump makes a KTF game readable to a disassembler.
//
// A game's client.bin is not what a debugger needs. It relocates itself at
// startup, so the bytes on disk are not the bytes that execute, and the
// addresses in a crash report or a profile mean nothing against the file. What
// is needed is the image after relocation — and, more than that, the map from
// address to method name, which the earlier debugging sessions in this project
// had to rebuild by hand every time.
//
//	ktfdump <game.zip> -image client.bin   the relocated image, load at 0x100000
//	ktfdump <game.zip> -symbols out.txt    address → class.method(descriptor)
//	ktfdump <game.zip> -classes            the class table, to stdout
//
// With no output selected it prints a summary of what the archive holds.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage(output io.Writer) {
	fmt.Fprintln(output, "usage: ktfdump <game.zip> [-image out.bin] [-symbols out.txt] [-classes]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "  -image    the client image after self-relocation; load it at 0x100000")
	fmt.Fprintln(output, "  -symbols  every AOT method body address and its name, address order")
	fmt.Fprintln(output, "  -classes  the registered class table with each method's body address")
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	archivePath := args[0]
	imagePath, symbolPath := "", ""
	listClasses := false
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "-image":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-image expects an output path")
				return 2
			}
			imagePath = args[index+1]
			index++
		case "-symbols":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-symbols expects an output path")
				return 2
			}
			symbolPath = args[index+1]
			index++
		case "-classes":
			listClasses = true
		default:
			fmt.Fprintf(stderr, "unknown option %q\n", args[index])
			usage(stderr)
			return 2
		}
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		fmt.Fprintf(stderr, "read archive: %v\n", err)
		return 1
	}

	// The image is dumped from the loaded client alone, before anything else
	// runs: relocation is the entry function's own work and needs no runtime.
	// Symbols need more — a class is only in the table once the game has asked
	// for it — so those come from a session that has reached startApp.
	if imagePath != "" {
		image, err := dumpImage(data)
		if err != nil {
			fmt.Fprintf(stderr, "dump image: %v\n", err)
			return 1
		}
		if err := os.WriteFile(imagePath, image, 0o644); err != nil {
			fmt.Fprintf(stderr, "write image: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s: %d bytes, load at %#x\n", imagePath, len(image), ktf.ImageBase)
	}

	if symbolPath == "" && !listClasses {
		if imagePath == "" {
			return summarize(data, stdout, stderr)
		}
		return 0
	}

	// The manual clock is what a probe wants: the guest's waits cost no real
	// time, so reaching startApp — and therefore the class table — is as fast
	// as the guest can be driven.
	// A start that fails is the case this tool is most often reached for: the
	// title stopped before it drew anything and the question is which class it
	// was in. The classes it did register are still in the registry behind the
	// failed start, so the failure is reported and the dump goes ahead with
	// whatever the title got to.
	session, err := ktf.StartSession(context.Background(), data, ktf.SessionOptions{
		Clock: ktf.NewManualClock(time.Time{}),
	})
	if err != nil {
		fmt.Fprintf(stderr, "start game: %v\n", err)
	}
	if session == nil {
		if session, err = partialSession(data); err != nil {
			fmt.Fprintf(stderr, "load client: %v\n", err)
			return 1
		}
	}
	defer session.Close()

	if symbolPath != "" {
		text := formatSymbols(session)
		if err := os.WriteFile(symbolPath, []byte(text), 0o644); err != nil {
			fmt.Fprintf(stderr, "write symbols: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s: %d symbols\n", symbolPath, strings.Count(text, "\n"))
	}
	if listClasses {
		fmt.Fprint(stdout, formatClasses(session))
	}
	return 0
}

// partialSession is what is left when a start fails: the client, initialized as
// far as the runtime goes, with the archive's main class loaded if it can be.
// It is enough for the class table and the symbols, which is what a stopped
// title is being asked about.
func partialSession(archive []byte) (*ktf.Session, error) {
	opened, err := ktf.Open(archive)
	if err != nil {
		return nil, err
	}
	client, err := ktf.LoadClient(opened.JAR.Client, armcore.CoreOptions{MaxSteps: 100_000_000})
	if err != nil {
		return nil, err
	}
	client.SetProgramName(ktf.ProgramNameForAID(opened.Descriptor.AID))
	client.AttachAppProperties(opened.Descriptor.Properties)
	client.AttachResources(opened.JAR.Entries)
	client.AttachFilesystem(opened.GuestFiles())
	ctx := context.Background()
	entry, err := client.ExecuteEntry(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("run the relocation entry: %w", err)
	}
	if _, err := client.Initialize(ctx, entry.Context.Registers[0]); err != nil {
		return nil, fmt.Errorf("run initialization: %w", err)
	}
	// A main class that will not load is not a reason to give up the table:
	// what is registered already is what the report is about.
	_, _ = client.LoadClass(ctx, opened.Descriptor.MainClass)
	return &ktf.Session{Archive: opened, Client: client}, nil
}

// dumpImage loads the client and runs its entry function, which is the
// self-relocation, then reads back the window it relocated into.
//
// An older relocatable module is already relocated by the time it is loaded —
// the loader does it rather than the guest — so its entry is not run here. It
// is not a relocation at all there, and running it needs the platform its
// first call goes to.
func dumpImage(archive []byte) ([]byte, error) {
	opened, err := ktf.Open(archive)
	if err != nil {
		return nil, err
	}
	client, err := ktf.LoadClient(opened.JAR.Client, armcore.CoreOptions{MaxSteps: 100_000_000})
	if err != nil {
		return nil, err
	}
	if !client.IsModule() {
		if _, err := client.ExecuteEntry(context.Background(), nil); err != nil {
			return nil, fmt.Errorf("run the relocation entry: %w", err)
		}
	}
	return client.ImageBytes()
}

// formatSymbols lists every guest method body by address, which is the table a
// disassembler or a crash address needs. Native bodies are left out: their
// address is one of our own supervisor-call stubs, not guest code, so naming
// one would point an investigation at the Host.
func formatSymbols(session *ktf.Session) string {
	type symbol struct {
		address uint32
		name    string
	}
	var symbols []symbol
	for _, class := range session.Client.JVM().AOTClasses() {
		for _, method := range class.Methods {
			if method.Body == 0 || method.NativeBody != 0 {
				continue
			}
			symbols = append(symbols, symbol{
				address: method.Body &^ 1,
				name:    class.Name + "." + method.Name + method.Descriptor,
			})
		}
	}
	sort.Slice(symbols, func(left, right int) bool {
		if symbols[left].address != symbols[right].address {
			return symbols[left].address < symbols[right].address
		}
		return symbols[left].name < symbols[right].name
	})
	var builder strings.Builder
	for _, current := range symbols {
		fmt.Fprintf(&builder, "%#08x %s\n", current.address, current.name)
	}
	return builder.String()
}

func formatClasses(session *ktf.Session) string {
	classes := session.Client.JVM().AOTClasses()
	sort.Slice(classes, func(left, right int) bool { return classes[left].Name < classes[right].Name })
	var builder strings.Builder
	for _, class := range classes {
		builder.WriteString(class.Name)
		if class.SuperName != "" {
			fmt.Fprintf(&builder, " extends %s", class.SuperName)
		}
		fmt.Fprintf(&builder, "  (%#08x, %d bytes per instance)\n", class.Address, class.InstanceSize)
		for _, method := range class.Methods {
			kind := ""
			if method.NativeBody != 0 {
				kind = "  [native]"
			}
			fmt.Fprintf(&builder, "    %#08x  %s%s%s\n", method.Body&^1, method.Name, method.Descriptor, kind)
		}
		// A field record's word is an offset for an instance field and the
		// value itself for a static one, so the two are labelled rather than
		// printed the same way. The instance offsets are the class's layout,
		// which is what a question about a field a title reads directly comes
		// down to.
		for _, field := range class.Fields {
			if field.AccessFlags&0x0008 != 0 {
				fmt.Fprintf(&builder, "    static    %s:%s = %#x\n", field.Name, field.Descriptor, field.Offset)
				continue
			}
			fmt.Fprintf(&builder, "    at %-6d %s:%s\n", field.Offset, field.Name, field.Descriptor)
		}
	}
	return builder.String()
}

func summarize(archive []byte, stdout, stderr io.Writer) int {
	opened, err := ktf.Open(archive)
	if err != nil {
		fmt.Fprintf(stderr, "open archive: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "aid          %s\n", opened.Descriptor.AID)
	fmt.Fprintf(stdout, "main class   %s\n", opened.Descriptor.MainClass)
	fmt.Fprintf(stdout, "client       %s (%d bytes)\n", opened.JAR.Client.Name, len(opened.JAR.Client.Data))
	fmt.Fprintf(stdout, "jar entries  %d\n", len(opened.JAR.Entries))
	fmt.Fprintf(stdout, "\nnothing selected; pass -image, -symbols, or -classes\n")
	return 0
}
