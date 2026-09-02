package main

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/korean"
)

// A KTF descriptor's title arrives as EUC-KR and its parser decodes it, so the
// report has a finished UTF-8 string in hand. Reading that as EUC-KR a second
// time is what turned 영웅전설3 into 곸썒꾩꽕3 in every KTF line the tool
// printed, and this is the check that the second read is gone.
func TestAnAlreadyDecodedNameIsNotDecodedTwice(t *testing.T) {
	const title = "영웅전설3"
	if twice := displayName(title); twice == title {
		t.Fatalf("displayName no longer mangles decoded text, so this test guards nothing: %q", twice)
	}
	var out bytes.Buffer
	reportCollision(&out, "KTF", "PD004460", []collisionClaim{
		{path: "var/games/ktf/one.zip", aid: "010356DB", name: title},
	})
	if !strings.Contains(out.String(), title) {
		t.Errorf("report = %q, want it to carry %q unchanged", out.String(), title)
	}
}

// LGT keeps app_info's bytes as they came, so its names are the case
// displayName exists for and must still be decoded.
func TestAnUndecodedNameIsDecodedOnce(t *testing.T) {
	const title = "레이카르나"
	encoded, err := korean.EUCKR.NewEncoder().String(title)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	reportCollision(&out, "LGT", "PD133941", []collisionClaim{
		{path: "var/games/lgt/one.zip", aid: "0002BF2E", name: displayName(encoded)},
	})
	if !strings.Contains(out.String(), title) {
		t.Errorf("report = %q, want it to carry %q", out.String(), title)
	}
}

// An ASCII title passes through either way, which is why the defect stayed
// invisible to anything but a Korean name.
func TestAnASCIINameSurvivesBothWays(t *testing.T) {
	if got := displayName("Ardent 3"); got != "Ardent 3" {
		t.Errorf("displayName(ASCII) = %q", got)
	}
}
