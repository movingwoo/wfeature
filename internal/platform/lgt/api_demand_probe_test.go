package lgt

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestLocalAPIDemandProbe lists every platform member a title links against and
// says which of them this platform serves. It is the LGT counterpart of reading
// a KTF module's string pool: a module's own metadata names each member it
// resolved, so the demand is a set rather than a sequence of failures, and
// answering it one run per member is what makes a widget toolkit take a day.
//
// Like the other probes it is a throwaway investigation aid and skips unless an
// archive is supplied:
//
//	WFEATURE_API_DEMAND_ARCHIVE=/abs/path/game.zip \
//	WFEATURE_API_DEMAND_PREFIX=org/kwis/msp/lwc \
//	go test ./internal/platform/lgt -run TestLocalAPIDemandProbe -v
func TestLocalAPIDemandProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_API_DEMAND_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_API_DEMAND_ARCHIVE")
	}
	prefix := os.Getenv("WFEATURE_API_DEMAND_PREFIX")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 240, Height: 320, MaxSteps: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Logf("start: %v", err)
	}
	if client.javaLink == nil || client.javaLink.surface == nil {
		t.Skip("this archive is not a Java title")
	}
	surface := client.javaLink.surface
	lines := []string{}
	report := func(kind string, members []javaMemberRef, run func(javaAPIClass) javaRun) {
		for index, member := range members {
			if member.Name == "" {
				continue
			}
			owner, known := surface.ownerOf(run, uint32(index))
			if !known {
				owner = "?"
			}
			full := owner + "." + member.Name + member.Descriptor
			if prefix != "" && !strings.Contains(full, prefix) {
				continue
			}
			served := javaPlatformMethodFor(full) != nil
			mark := "MISSING"
			if served {
				mark = "served "
			}
			lines = append(lines, fmt.Sprintf("%s %-8s %4d %s", mark, kind, index, full))
		}
	}
	report("static", surface.StaticMethods, func(class javaAPIClass) javaRun { return class.StaticMethods })
	report("virtual", surface.VirtualMethods, func(class javaAPIClass) javaRun { return class.VirtualMethods })
	report("method", surface.Methods, func(class javaAPIClass) javaRun { return class.Methods })
	report("sfield", surface.StaticFields, func(class javaAPIClass) javaRun { return class.StaticFields })
	for index, member := range surface.Fields {
		if member.Name == "" {
			continue
		}
		full := member.Name + member.Descriptor
		if prefix != "" && !strings.Contains(full, prefix) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %-8s %4d %s", "field?  ", "field", index, full))
	}
	sort.Strings(lines)
	for _, line := range lines {
		fmt.Println(line)
	}
}

// javaPlatformMethodFor answers the implementation registered for a member, or
// nil. The tables are separate files; this joins them for the probe.
func javaPlatformMethodFor(full string) *javaPlatformMethod {
	for _, table := range []map[string]javaPlatformMethod{
		javaPlatformMethods, javaDatabaseMethods, javaGraphicsMethods,
	} {
		if method, ok := table[full]; ok {
			return &method
		}
	}
	return nil
}
