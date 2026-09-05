package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A release directory built the way `make dist` builds one has nothing wrong
// with it. Running the binary is left out: the fixture's server is four bytes,
// and what a real one answers is what the release workflow's smoke job asks.
func TestAWellFormedReleaseDirectoryPasses(t *testing.T) {
	repository, directory := stageRelease(t, nil)
	problems, err := check(repository, directory, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("a good release reported %d problem(s):\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// Every way a package has been wrong by hand, or could be. Each of these was a
// manual check before a tag, which is the reason for the command: a check
// somebody has to remember is a check that is skipped the week it matters.
func TestEachWayAPackageCanBeWrongIsReported(t *testing.T) {
	for _, probe := range []struct {
		name   string
		damage func(target platform, staged []file) []file
		expect string
	}{
		{
			name: "an entry that escapes the folder it names",
			damage: func(target platform, staged []file) []file {
				return append(staged, file{name: "../outside.txt", data: []byte("x")})
			},
			expect: "escapes the folder it names",
		},
		{
			name: "an entry outside the archive's own folder",
			damage: func(target platform, staged []file) []file {
				return append(staged, file{name: "elsewhere/notes.txt", data: []byte("x"), absolute: true})
			},
			expect: "is outside",
		},
		{
			name: "a symbolic link",
			damage: func(target platform, staged []file) []file {
				return append(staged, file{name: "shortcut", link: "/etc/passwd"})
			},
			expect: "not a plain file or directory",
		},
		{
			name: "a launcher that lost its executable bit",
			damage: func(target platform, staged []file) []file {
				return withMode(staged, launchers(target.os)[0], 0o644)
			},
			expect: "is not executable",
		},
		{
			name: "a launcher that is missing",
			damage: func(target platform, staged []file) []file {
				return without(staged, launchers(target.os)[0])
			},
			expect: "is missing",
		},
		{
			name: "notices that have drifted from the repository's",
			damage: func(target platform, staged []file) []file {
				return withData(staged, "THIRD-PARTY-NOTICES.md", []byte("## a component nobody bundled\n"))
			},
			expect: "not the file it is made from",
		},
		{
			name: "a Windows README with no byte order mark",
			damage: func(target platform, staged []file) []file {
				if target.os != "windows" {
					return staged
				}
				for _, entry := range staged {
					if entry.name == "README.txt" {
						return withData(staged, "README.txt", bytes.TrimPrefix(entry.data, []byte{0xef, 0xbb, 0xbf}))
					}
				}
				return staged
			},
			expect: "not the file it is made from",
		},
		{
			name: "a Unix README that went through the Windows conversion",
			damage: func(target platform, staged []file) []file {
				if target.os == "windows" {
					return staged
				}
				return withData(staged, "README.txt", toCRLF(readmeFor(target.os)))
			},
			expect: "Windows line endings",
		},
		{
			name: "the empty games tree the server looks for",
			damage: func(target platform, staged []file) []file {
				return without(staged, "games/skt")
			},
			expect: "games/skt/ is missing",
		},
		{
			name: "something the archive should not carry",
			damage: func(target platform, staged []file) []file {
				return append(staged, file{name: "build.log", data: []byte("x")})
			},
			expect: "should not be",
		},
	} {
		repository, directory := stageRelease(t, probe.damage)
		problems, err := check(repository, directory, false, false)
		if err != nil {
			t.Fatalf("%s: %v", probe.name, err)
		}
		if !reported(problems, probe.expect) {
			t.Errorf("%s: nothing said %q; the run reported:\n%s",
				probe.name, probe.expect, strings.Join(problems, "\n"))
		}
	}
}

// The checksums file and the directory have to agree in both directions: an
// archive whose bytes changed after it was hashed, and an archive that was
// added to the directory afterwards and never hashed at all.
func TestTheChecksumsAndTheDirectoryHaveToAgree(t *testing.T) {
	repository, directory := stageRelease(t, nil)
	archive := filepath.Join(directory, "wfeature-9.9.9-linux-amd64.tar.gz")
	if err := os.WriteFile(archive, []byte("not the archive that was hashed"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := check(repository, directory, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reported(problems, "SHA256SUMS says") {
		t.Errorf("a rewritten archive was not caught:\n%s", strings.Join(problems, "\n"))
	}

	repository, directory = stageRelease(t, nil)
	unlisted := filepath.Join(directory, "wfeature-9.9.9-plan9-386.tar.gz")
	if err := os.WriteFile(unlisted, []byte("an archive nobody hashed"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err = check(repository, directory, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reported(problems, "is not in SHA256SUMS") {
		t.Errorf("an unhashed archive was not caught:\n%s", strings.Join(problems, "\n"))
	}
}

// A release that is missing a platform is a release somebody cannot download,
// and five archives of four platforms is the shape that mistake takes.
func TestAMissingPlatformIsReported(t *testing.T) {
	repository, directory := stageRelease(t, nil)
	if err := os.Remove(filepath.Join(directory, "wfeature-9.9.9-linux-arm64.tar.gz")); err != nil {
		t.Fatal(err)
	}
	problems, err := check(repository, directory, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reported(problems, "no archive for linux/arm64") {
		t.Errorf("the missing platform was not named:\n%s", strings.Join(problems, "\n"))
	}
}

// The phone builds are published beside the archives, and the thing that used
// to go wrong is that they were not: they are built by a different command on
// a different machine, and SHA256SUMS agrees with five files as readily as
// with seven. A release asks for them by name.
func TestTheReleaseNeedsThePhoneBuilds(t *testing.T) {
	apk := "wfeature-" + fixtureVersion + "-android-arm64.apk"
	ipa := "wfeature-" + fixtureVersion + "-ios-arm64.ipa"

	// Nothing asked for them, so a directory `make dist` alone wrote is fine.
	repository, directory := stageRelease(t, nil)
	problems, err := check(repository, directory, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("a desktop-only build reported %d problem(s):\n%s", len(problems), strings.Join(problems, "\n"))
	}

	// A release asked for them and they are not here.
	problems, err = check(repository, directory, false, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{apk, ipa} {
		if !reported(problems, "no "+want) {
			t.Errorf("the missing %s was not named:\n%s", want, strings.Join(problems, "\n"))
		}
	}

	// With both beside the archives it passes.
	addPackage(t, directory, apk, []byte("dex\n"))
	addPackage(t, directory, ipa, []byte("Payload\n"))
	problems, err = check(repository, directory, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("a release with its phone builds reported %d problem(s):\n%s", len(problems), strings.Join(problems, "\n"))
	}

	// An APK left over from an earlier version is not this release's APK, and
	// saying so by name is the difference between a stale file and no file.
	repository, directory = stageRelease(t, nil)
	addPackage(t, directory, "wfeature-9.9.8-android-arm64.apk", []byte("dex\n"))
	addPackage(t, directory, ipa, []byte("Payload\n"))
	problems, err = check(repository, directory, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reported(problems, "wfeature-9.9.8-android-arm64.apk is here but "+apk+" is what this release needs") {
		t.Errorf("the stale APK was not named:\n%s", strings.Join(problems, "\n"))
	}
}

// The name carries the version, and a version can have a dash in it: a
// pre-release tag is v0.4.0-pre, and splitting on the first dash would read
// its platform as "0.4.0".
func TestAPreReleaseVersionSurvivesItsFileName(t *testing.T) {
	for _, probe := range []struct {
		name    string
		version string
		target  platform
		ok      bool
	}{
		{"wfeature-0.4.0-linux-amd64.tar.gz", "0.4.0", platform{"linux", "amd64"}, true},
		{"wfeature-0.4.0-pre-windows-amd64.zip", "0.4.0-pre", platform{"windows", "amd64"}, true},
		{"wfeature-0.5.0-rc1-darwin-arm64.tar.gz", "0.5.0-rc1", platform{"darwin", "arm64"}, true},
		// A Windows package in a tarball is a release nobody on Windows can
		// open without installing something first.
		{"wfeature-0.4.0-windows-amd64.tar.gz", "", platform{}, false},
		{"something-else.zip", "", platform{}, false},
	} {
		version, target, ok := describe(probe.name)
		if ok != probe.ok {
			t.Errorf("%s: accepted = %v, want %v", probe.name, ok, probe.ok)
			continue
		}
		if !ok {
			continue
		}
		if version != probe.version || target != probe.target {
			t.Errorf("%s: read as %s %s/%s, want %s %s/%s",
				probe.name, version, target.os, target.arch,
				probe.version, probe.target.os, probe.target.arch)
		}
	}
}

// Fixtures.

// file is one entry to write into a staged archive.
type file struct {
	name     string
	data     []byte
	mode     fs.FileMode
	dir      bool
	link     string // non-empty makes this a symbolic link
	absolute bool   // write the name as it is rather than under the archive's folder
}

const fixtureVersion = "9.9.9"

// stageRelease writes a repository holding the files an archive is built from
// and a release directory holding the five archives, then hands back both. The
// damage function, when given, is applied to each archive's entries before it
// is written, which is how one defect is introduced into a release that is
// otherwise exactly right.
func stageRelease(t *testing.T, damage func(platform, []file) []file) (repository, directory string) {
	t.Helper()
	repository = t.TempDir()
	directory = filepath.Join(repository, "build", "dist")

	write := func(path string, data []byte) {
		t.Helper()
		full := filepath.Join(repository, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("LICENSE", []byte(fixtureLicence))
	write("internal/licenses/THIRD-PARTY-NOTICES.md", []byte(fixtureNotices))
	write("packaging/games/README.txt", []byte(fixtureGamesReadme))
	for goos, source := range map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"} {
		write("packaging/"+source+"/README.txt", readmeFor(goos))
		for _, script := range launchers(goos) {
			write("packaging/"+source+"/"+script, []byte(fixtureScript(goos, script)))
		}
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	sums := &bytes.Buffer{}
	for _, target := range releasePlatforms {
		name := fmt.Sprintf("wfeature-%s-%s-%s", fixtureVersion, target.os, target.arch)
		staged := stagedFiles(target)
		if damage != nil {
			staged = damage(target, staged)
		}
		var archive []byte
		if target.os == "windows" {
			name += ".zip"
			archive = buildZip(t, strings.TrimSuffix(name, ".zip"), staged)
		} else {
			name += ".tar.gz"
			archive = buildTarGzip(t, strings.TrimSuffix(name, ".tar.gz"), staged)
		}
		if err := os.WriteFile(filepath.Join(directory, name), archive, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(archive)
		fmt.Fprintf(sums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	if err := os.WriteFile(filepath.Join(directory, "SHA256SUMS"), sums.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return repository, directory
}

// stagedFiles is what the Makefile's staging step leaves in a package's folder
// for one platform.
func stagedFiles(target platform) []file {
	binary := "wfeature-server"
	if target.os == "windows" {
		binary += ".exe"
	}
	staged := []file{
		{name: binary, data: []byte("ELF\n"), mode: 0o755},
		{name: "LICENSE.txt", data: []byte(fixtureLicence), mode: 0o644},
		{name: "THIRD-PARTY-NOTICES.md", data: []byte(fixtureNotices), mode: 0o644},
		{name: "games", dir: true, mode: 0o755},
		{name: "games/ktf", dir: true, mode: 0o755},
		{name: "games/lgt", dir: true, mode: 0o755},
		{name: "games/skt", dir: true, mode: 0o755},
	}
	if target.os == "windows" {
		staged = append(staged,
			file{name: "README.txt", data: withBOM(toCRLF(readmeFor("windows"))), mode: 0o644},
			file{name: "games/README.txt", data: withBOM(toCRLF([]byte(fixtureGamesReadme))), mode: 0o644})
		for _, script := range launchers("windows") {
			staged = append(staged, file{name: script, data: toCRLF([]byte(fixtureScript("windows", script))), mode: 0o644})
		}
		return staged
	}
	staged = append(staged,
		file{name: "README.txt", data: readmeFor(target.os), mode: 0o644},
		file{name: "games/README.txt", data: []byte(fixtureGamesReadme), mode: 0o644})
	for _, script := range launchers(target.os) {
		staged = append(staged, file{name: script, data: []byte(fixtureScript(target.os, script)), mode: 0o755})
	}
	return staged
}

func buildTarGzip(t *testing.T, top string, staged []file) []byte {
	t.Helper()
	out := &bytes.Buffer{}
	compressed := gzip.NewWriter(out)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: top + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range staged {
		name := top + "/" + entry.name
		if entry.absolute {
			name = entry.name
		}
		header := &tar.Header{Name: name, Mode: int64(entry.mode)}
		switch {
		case entry.link != "":
			header.Typeflag, header.Linkname = tar.TypeSymlink, entry.link
		case entry.dir:
			header.Typeflag, header.Name = tar.TypeDir, name+"/"
		default:
			header.Typeflag, header.Size = tar.TypeReg, int64(len(entry.data))
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := archive.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func buildZip(t *testing.T, top string, staged []file) []byte {
	t.Helper()
	out := &bytes.Buffer{}
	archive := zip.NewWriter(out)
	for _, entry := range staged {
		name := top + "/" + entry.name
		if entry.absolute {
			name = entry.name
		}
		header := &zip.FileHeader{Name: name}
		switch {
		case entry.link != "":
			header.SetMode(entry.mode | fs.ModeSymlink)
		case entry.dir:
			header.Name = name + "/"
			header.SetMode(entry.mode | fs.ModeDir)
		default:
			header.SetMode(entry.mode)
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case entry.link != "":
			if _, err := writer.Write([]byte(entry.link)); err != nil {
				t.Fatal(err)
			}
		case !entry.dir:
			if _, err := writer.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// addPackage drops a file into the release directory and adds it to
// SHA256SUMS, which is what `make checksums` does for whatever is there.
func addPackage(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(directory, "SHA256SUMS")
	existing, err := os.ReadFile(sums)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name)
	if err := os.WriteFile(sums, append(existing, line...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func without(staged []file, name string) []file {
	kept := make([]file, 0, len(staged))
	for _, entry := range staged {
		if entry.name != name {
			kept = append(kept, entry)
		}
	}
	return kept
}

func withMode(staged []file, name string, mode fs.FileMode) []file {
	changed := append([]file(nil), staged...)
	for index := range changed {
		if changed[index].name == name {
			changed[index].mode = mode
		}
	}
	return changed
}

func withData(staged []file, name string, data []byte) []file {
	changed := append([]file(nil), staged...)
	for index := range changed {
		if changed[index].name == name {
			changed[index].data = data
		}
	}
	return changed
}

func reported(problems []string, want string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return true
		}
	}
	return false
}

// The fixture text. It is not the real packaging text — what matters is that
// the archive carries whatever the repository holds, byte for byte — but it is
// the same shape: Korean prose for the reader, ASCII for the launchers.
const (
	fixtureLicence     = "MIT License\n\nCopyright (c) nobody\n"
	fixtureNotices     = "# Notices\n\n## a bundled component\n\nits licence, in full\n"
	fixtureGamesReadme = "게임 넣는 곳\n\n이 폴더에 파일을 둔다.\n"
)

func readmeFor(goos string) []byte {
	return []byte("W-Feature — 피처폰 게임 에뮬레이터 (" + goos + ")\n\n실행 방법\n")
}

func fixtureScript(goos, script string) string {
	if goos == "windows" {
		return "@echo off\nrem " + script + "\nwfeature-server.exe\n"
	}
	return "#!/bin/sh\n# " + script + "\nexec ./wfeature-server\n"
}
