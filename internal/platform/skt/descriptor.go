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

	var logicalLine string
	seenMainSection := false
	flush := func() error {
		if logicalLine == "" {
			return nil
		}
		key, value, found := strings.Cut(logicalLine, ":")
		if !found || strings.TrimSpace(key) == "" {
			return fmt.Errorf("invalid SKT descriptor line %q", logicalLine)
		}
		properties[strings.TrimSpace(key)] = strings.TrimSpace(value)
		logicalLine = ""
		seenMainSection = true
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, " ") {
			if logicalLine == "" {
				return Descriptor{}, fmt.Errorf("descriptor continuation has no preceding line")
			}
			logicalLine += line[1:]
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
		logicalLine = line
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

func (descriptor Descriptor) Property(name string) (string, bool) {
	for key, value := range descriptor.Properties {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}
