package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiSelectsStorageBeforeLooseArchiveAdoption(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	library := filepath.Join("var", "games")
	if err := os.MkdirAll(library, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(library, "loose.zip")
	if err := os.WriteFile(archive, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join("var", "accounts.sqlite")
	if err := os.WriteFile(store, []byte("invalid database"), 0600); err != nil {
		t.Fatal(err)
	}
	stream, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err := run([]string{"-multi", "-games", library}, stream, stream); err == nil || !strings.Contains(err.Error(), "account store") {
		t.Fatalf("startup with damaged shared store: %v", err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatal("shared startup adopted a loose archive", err)
	}
	// An unusable listener stops this personal launch after adoption.
	if err := run([]string{"-games", library, "-ext", filepath.Join("var", "ext"), "-addr", "invalid address"}, stream, stream); err == nil || strings.Contains(err.Error(), "account store") {
		t.Fatalf("personal launch: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join("var", "ext", "loose.zip")); err != nil || string(body) != "fixture" {
		t.Fatal("personal launch did not adopt the archive", err)
	}
	if body, err := os.ReadFile(store); err != nil || string(body) != "invalid database" {
		t.Fatal("personal launch touched the account database", err)
	}
}

func TestSharedAccountFileMustBeOutsideServedRoots(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("var", 0700); err != nil {
		t.Fatal(err)
	}
	unsafeRoots := [][]string{{"-web", "."}, {"-games", "var"}}
	if _, err := os.Stat("VAR"); err == nil {
		unsafeRoots = append(unsafeRoots, []string{"-web", "VAR"})
	}
	for _, flags := range unsafeRoots {
		err := run(append([]string{"-multi", "-addr", "invalid"}, flags...), os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "served directory") {
			t.Fatalf("overlapping root %v: %v", flags, err)
		}
		if _, err := os.Stat("var/accounts.sqlite"); !os.IsNotExist(err) {
			t.Fatal("created a database inside a served directory")
		}
	}
}

func TestPersonalBindingRequiresALoginID(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"-multi", "-bind-personal-saves="}, {"-bind-personal-saves", "player"}} {
		err := run(append(args, "-addr", "invalid"), os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "-bind-personal-saves requires") {
			t.Fatalf("invalid binding %v: %v", args, err)
		}
		if _, err := os.Stat("var/accounts.sqlite"); !os.IsNotExist(err) {
			t.Fatal("invalid setup created the account store")
		}
	}
}
