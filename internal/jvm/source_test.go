package jvm

import (
	"bytes"
	"testing"
)

func TestClassSourcesUseFirstMatchingSource(t *testing.T) {
	runtimeClass := []byte("runtime")
	applicationClass := []byte("application")
	sources := ClassSources{
		mapClassSource{"platform/Class": runtimeClass},
		mapClassSource{"platform/Class": applicationClass, "game/Main": applicationClass},
	}

	got, ok := sources.ClassBytes("platform/Class")
	if !ok || !bytes.Equal(got, runtimeClass) {
		t.Fatalf("platform/Class = %q, %v", got, ok)
	}
	got, ok = sources.ClassBytes("game/Main")
	if !ok || !bytes.Equal(got, applicationClass) {
		t.Fatalf("game/Main = %q, %v", got, ok)
	}
}
