package skt

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

type Descriptor struct {
	Name       string
	MainClass  string
	Properties map[string]string
}

func ParseDescriptor(data []byte) (Descriptor, error) {
	properties := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	// A logical line is built in one buffer: joining each continuation onto a
	// string copied the whole line every time, and a manifest of short
	// continuation lines then cost time in the square of its size.
	var logicalLine strings.Builder
	seenMainSection := false
	flush := func() error {
		if logicalLine.Len() == 0 {
			return nil
		}
		line := logicalLine.String()
		logicalLine.Reset()
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) == "" {
			return fmt.Errorf("invalid SKT descriptor line %q", line)
		}
		properties[strings.TrimSpace(key)] = strings.TrimSpace(value)
		seenMainSection = true
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, " ") {
			if logicalLine.Len() == 0 {
				return Descriptor{}, fmt.Errorf("descriptor continuation has no preceding line")
			}
			logicalLine.WriteString(line[1:])
			continue
		}
		if err := flush(); err != nil {
			return Descriptor{}, err
		}
		if line == "" {
			if seenMainSection {
				break
			}
			continue
		}
		logicalLine.WriteString(line)
	}
	if err := scanner.Err(); err != nil {
		return Descriptor{}, fmt.Errorf("read SKT descriptor: %w", err)
	}
	if err := flush(); err != nil {
		return Descriptor{}, err
	}

	descriptor := Descriptor{Properties: properties}
	descriptor.Name = property(properties, "MIDlet-Name")
	midlet := property(properties, "MIDlet-1")
	parts := strings.Split(midlet, ",")
	if len(parts) < 3 || strings.TrimSpace(parts[2]) == "" {
		return Descriptor{}, fmt.Errorf("MIDlet-1 does not name a main class")
	}
	descriptor.MainClass = strings.ReplaceAll(strings.TrimSpace(parts[2]), ".", "/")
	return descriptor, nil
}

func property(properties map[string]string, name string) string {
	value, _ := Descriptor{Properties: properties}.Property(name)
	return value
}

// Property answers a descriptor key regardless of its case. The exact
// spelling wins, and among other spellings the first in byte order does:
// ranging the map returned whichever came first, so a descriptor carrying two
// spellings of DD-ProgName named a different save owner from launch to launch.
func (descriptor Descriptor) Property(name string) (string, bool) {
	if value, ok := descriptor.Properties[name]; ok {
		return value, true
	}
	match, found := "", false
	for key := range descriptor.Properties {
		if strings.EqualFold(key, name) && (!found || key < match) {
			match, found = key, true
		}
	}
	if !found {
		return "", false
	}
	return descriptor.Properties[match], true
}
