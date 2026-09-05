// Command distcheck opens the archives `make dist` wrote and checks that what
// a user extracts is what this repository meant to publish.
//
// The release workflow used to stop at SHA256SUMS and a count of five, which
// says the files arrived and nothing about what is in them. Everything below
// was a manual check before a tag: that the launcher a double-click reaches is
// still executable, that the Windows README still carries the byte order mark
// that keeps an editor from guessing CP949, that the notices in the archive are
// the notices in the repository, and that no entry in a package can be
// extracted outside the folder it names.
//
// It reads the archives rather than unpacking them, because "what does this
// zip do when extracted" is a question that has to be answered before
// extracting it — an entry named `../../…` is a finding, not an accident to
// discover afterwards in a home directory. The one exception is the archive
// built for the machine running this, whose server is unpacked into a
// temporary directory and asked for its version: a cross-compiled binary
// proves a build, and only a run proves the version was stamped into it.
//
// Usage:
//
//	go run ./internal/tools/distcheck [-dir build/dist] [-root .] [-run=false] [-phones]
//
// `make dist-check` runs it over what `make dist` just wrote, and the release
// workflow runs the same command before it publishes anything.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func main() {
	directory := flag.String("dir", filepath.Join("build", "dist"), "the directory `make dist` wrote")
	root := flag.String("root", "", "the repository the archives were built from (default: the tree this source is in)")
	run := flag.Bool("run", true, "unpack the archive built for this machine and ask the server its version")
	phones := flag.Bool("phones", false, "require the APK and the IPA beside the archives, as a release has them")
	flag.Parse()

	repository := *root
	if repository == "" {
		found, err := repositoryRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, "distcheck:", err)
			os.Exit(2)
		}
		repository = found
	}

	problems, err := check(repository, *directory, *run, *phones)
	if err != nil {
		fmt.Fprintln(os.Stderr, "distcheck:", err)
		os.Exit(2)
	}
	for _, problem := range problems {
		fmt.Println(problem)
	}
	if len(problems) > 0 {
		fmt.Printf("\n%d problem(s) in %s\n", len(problems), *directory)
		os.Exit(1)
	}
	fmt.Printf("%s: the archives carry what they should\n", *directory)
}

// The five platforms `make dist` builds for. A release that is missing one is
// a release somebody cannot download, and the count alone would not say which.
var releasePlatforms = []platform{
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"windows", "amd64"},
}

type platform struct {
	os   string
	arch string
}

// check reads every archive in the directory and reports what is wrong with
// it. It returns an error only when it cannot look at all; a package that is
// wrong comes back as a problem so that one run reports every one of them
// rather than the first.
func check(repository, directory string, run, phones bool) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	packages := map[string]string{} // archive file name -> full path
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") ||
			strings.HasSuffix(name, ".apk") || strings.HasSuffix(name, ".ipa") {
			packages[name] = filepath.Join(directory, name)
		}
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("no archives in %s; run `make dist` first", directory)
	}

	var problems []string
	report := func(format string, arguments ...any) {
		problems = append(problems, fmt.Sprintf(format, arguments...))
	}

	problems = append(problems, checkChecksums(directory, packages)...)

	sources, err := readPackagingSources(repository)
	if err != nil {
		return nil, err
	}

	// Every archive names the version it was stamped with, and a directory
	// holding two versions is a `make dist` that was run twice without the
	// first one being cleared away.
	versions := map[string]bool{}
	seen := map[platform]string{}
	for name, file := range packages {
		if strings.HasSuffix(name, ".apk") || strings.HasSuffix(name, ".ipa") {
			// The phone builds come from `make mobile` and are packaged by
			// the platform's own tooling. They are checksummed with the rest,
			// which is what the block above covers, and their contents are
			// not this command's to judge.
			continue
		}
		stamp, target, ok := describe(name)
		if !ok {
			report("%s: not a name `make dist` writes (wfeature-<version>-<os>-<arch>.<tar.gz|zip>)", name)
			continue
		}
		versions[stamp] = true
		if previous, duplicate := seen[target]; duplicate {
			report("%s and %s are both %s/%s", previous, name, target.os, target.arch)
		}
		seen[target] = name

		items, err := readArchive(file)
		if err != nil {
			report("%s: %v", name, err)
			continue
		}
		problems = append(problems, inspect(name, target, items, sources)...)
	}

	for _, target := range releasePlatforms {
		if _, ok := seen[target]; !ok {
			report("no archive for %s/%s", target.os, target.arch)
		}
	}
	if len(versions) > 1 {
		report("the archives carry %d different versions: %s", len(versions), strings.Join(sortedKeys(versions), ", "))
	}
	problems = append(problems, checkPhones(packages, versions, phones)...)

	if run {
		problems = append(problems, runTheNativeArchive(directory, seen)...)
	}
	return problems, nil
}

// checkChecksums reads SHA256SUMS and hashes every file it names. `sha256sum
// -c` does the same, and is not on every machine that can run `make dist`;
// this also catches the other direction, an archive the file does not list.
func checkChecksums(directory string, packages map[string]string) []string {
	var problems []string
	sums, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		return append(problems, fmt.Sprintf("SHA256SUMS: %v", err))
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			problems = append(problems, fmt.Sprintf("SHA256SUMS: cannot read the line %q", line))
			continue
		}
		want, name := fields[0], strings.TrimPrefix(fields[1], "*")
		listed[name] = true
		file, ok := packages[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("SHA256SUMS names %s, which is not here", name))
			continue
		}
		contents, err := os.ReadFile(file)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		sum := sha256.Sum256(contents)
		if got := hex.EncodeToString(sum[:]); got != want {
			problems = append(problems, fmt.Sprintf("%s: sha256 is %s, SHA256SUMS says %s", name, got, want))
		}
	}
	for name := range packages {
		if !listed[name] {
			problems = append(problems, fmt.Sprintf("%s is not in SHA256SUMS", name))
		}
	}
	return problems
}

// The phone builds `make mobile` writes, named for the same version as the
// archives beside them. Most local builds have none — `make dist` alone is a
// valid thing to check — so their absence is a problem only when this was
// asked to require them, which the release workflow does. That flag is the
// whole point of this check: a release that went out without the APK is a
// thing that happened, and it happened quietly, because SHA256SUMS is written
// over whatever is in the directory and agrees with five files as readily as
// with seven.
//
// One of the pair present and the other missing is reported either way. That
// is not a release built without the phone builds; it is one whose phone
// builds were half done.
func checkPhones(packages map[string]string, versions map[string]bool, required bool) []string {
	var problems []string
	present := map[string]string{} // extension -> file name
	for name := range packages {
		for _, extension := range []string{".apk", ".ipa"} {
			if strings.HasSuffix(name, extension) {
				present[extension] = name
			}
		}
	}
	if !required && len(present) == 0 {
		return nil
	}
	// Which version to expect them at comes from the archives, so a stale APK
	// left behind by an earlier `make mobile` is named as missing rather than
	// counted as the one this release needs.
	if len(versions) != 1 {
		return nil // already reported: there is no one version to name them for
	}
	stamp := sortedKeys(versions)[0]
	for _, want := range []struct{ extension, name string }{
		{".apk", "wfeature-" + stamp + "-android-arm64.apk"},
		{".ipa", "wfeature-" + stamp + "-ios-arm64.ipa"},
	} {
		if _, ok := packages[want.name]; ok {
			continue
		}
		if stray, ok := present[want.extension]; ok {
			problems = append(problems, fmt.Sprintf("%s is here but %s is what this release needs", stray, want.name))
			continue
		}
		problems = append(problems, fmt.Sprintf("no %s; a release carries the phone builds beside the archives", want.name))
	}
	return problems
}

// describe reads an archive's file name back into the version and platform it
// was built for. The version is whatever is between the project name and the
// last two fields, so a pre-release tag — 0.4.0-pre — survives the split.
func describe(name string) (version string, target platform, ok bool) {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".tar.gz"), ".zip")
	rest, found := strings.CutPrefix(base, "wfeature-")
	if !found {
		return "", platform{}, false
	}
	fields := strings.Split(rest, "-")
	if len(fields) < 3 {
		return "", platform{}, false
	}
	target = platform{os: fields[len(fields)-2], arch: fields[len(fields)-1]}
	version = strings.Join(fields[:len(fields)-2], "-")
	if version == "" {
		return "", platform{}, false
	}
	// The container has to match the system: Explorer opens a zip without a
	// tool, and nothing on Windows opens a .tar.gz without one.
	if (target.os == "windows") != strings.HasSuffix(name, ".zip") {
		return version, target, false
	}
	return version, target, true
}

// item is one entry of an archive, read rather than extracted.
type item struct {
	name string // the path inside the archive, always slash-separated
	mode fs.FileMode
	dir  bool
	link bool // a symlink, a hard link, or anything else that is not a plain file
	data []byte
}

func readArchive(file string) ([]item, error) {
	if strings.HasSuffix(file, ".zip") {
		return readZip(file)
	}
	return readTarGzip(file)
}

func readTarGzip(file string) ([]item, error) {
	handle, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	stream, err := gzip.NewReader(handle)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	var items []item
	archive := tar.NewReader(stream)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		current := item{
			name: header.Name,
			mode: fs.FileMode(header.Mode).Perm(),
			dir:  header.Typeflag == tar.TypeDir,
			link: header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg,
		}
		if !current.dir && !current.link {
			data, err := io.ReadAll(archive)
			if err != nil {
				return nil, err
			}
			current.data = data
		}
		items = append(items, current)
	}
	return items, nil
}

func readZip(file string) ([]item, error) {
	archive, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	var items []item
	for _, entry := range archive.File {
		mode := entry.Mode()
		current := item{
			name: entry.Name,
			mode: mode.Perm(),
			dir:  entry.FileInfo().IsDir(),
			link: !mode.IsDir() && !mode.IsRegular(),
		}
		if !current.dir && !current.link {
			handle, err := entry.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(handle)
			handle.Close()
			if err != nil {
				return nil, err
			}
			current.data = data
		}
		items = append(items, current)
	}
	return items, nil
}

// sources are the files in the repository an archive is built out of. What an
// archive carries is compared against these rather than against a description
// of them, so a change to a launcher script is checked by the same run that
// checks the script is there at all.
type sources struct {
	licence  []byte
	notices  []byte
	games    []byte            // packaging/games/README.txt
	readme   map[string][]byte // packaging/<source>/README.txt, by GOOS
	launcher map[string][]byte // "<GOOS>/<script>" -> contents
}

func readPackagingSources(repository string) (sources, error) {
	read := func(parts ...string) ([]byte, error) {
		return os.ReadFile(filepath.Join(append([]string{repository}, parts...)...))
	}
	found := sources{readme: map[string][]byte{}, launcher: map[string][]byte{}}
	var err error
	if found.licence, err = read("LICENSE"); err != nil {
		return found, err
	}
	if found.notices, err = read("internal", "licenses", "THIRD-PARTY-NOTICES.md"); err != nil {
		return found, err
	}
	if found.games, err = read("packaging", "games", "README.txt"); err != nil {
		return found, err
	}
	for goos, directory := range map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"} {
		if found.readme[goos], err = read("packaging", directory, "README.txt"); err != nil {
			return found, err
		}
		for _, script := range launchers(goos) {
			contents, err := read("packaging", directory, script)
			if err != nil {
				return found, err
			}
			found.launcher[goos+"/"+script] = contents
		}
	}
	return found, nil
}

// The launcher scripts each system gets, named the way that system expects to
// be handed something double-clickable.
func launchers(goos string) []string {
	switch goos {
	case "windows":
		return []string{"start.bat", "status.bat", "stop.bat"}
	case "darwin":
		return []string{"start.command", "status.command", "stop.command"}
	default:
		return []string{"start.sh", "status.sh", "stop.sh"}
	}
}

// inspect is the whole of what one archive has to be.
func inspect(name string, target platform, items []item, from sources) []string {
	var problems []string
	report := func(format string, arguments ...any) {
		problems = append(problems, fmt.Sprintf("%s: "+format, append([]any{name}, arguments...)...))
	}

	// The folder every entry has to be inside. Extracting an archive is
	// expected to leave one directory behind rather than scatter files into
	// whatever the current one is.
	top := strings.TrimSuffix(strings.TrimSuffix(name, ".tar.gz"), ".zip")

	files := map[string]item{}
	directories := map[string]bool{}
	for _, entry := range items {
		clean := strings.TrimSuffix(entry.name, "/")
		switch {
		case entry.link:
			report("%s is not a plain file or directory", entry.name)
			continue
		case path.IsAbs(clean) || strings.HasPrefix(clean, "/") || (len(clean) > 1 && clean[1] == ':'):
			report("%s is an absolute path", entry.name)
			continue
		case strings.Contains(entry.name, `\`):
			report("%s has a backslash in its name", entry.name)
			continue
		}
		unsafe := false
		for _, element := range strings.Split(clean, "/") {
			if element == ".." || element == "." || element == "" {
				report("%s escapes the folder it names", entry.name)
				unsafe = true
				break
			}
		}
		if unsafe {
			continue
		}
		if clean != top && !strings.HasPrefix(clean, top+"/") {
			report("%s is outside %s/", entry.name, top)
			continue
		}
		inside := strings.TrimPrefix(strings.TrimPrefix(clean, top), "/")
		if inside == "" {
			continue // the top-level folder itself
		}
		if entry.dir {
			directories[inside] = true
			continue
		}
		if _, twice := files[inside]; twice {
			report("%s appears twice", inside)
		}
		files[inside] = entry
	}

	binary := "wfeature-server"
	if target.os == "windows" {
		binary += ".exe"
	}

	// What has to be in the archive, and what each file has to be. The
	// launchers and the two READMEs are compared against the packaging
	// sources they are made from, which is what checks their line endings and
	// their byte order marks: `make dist` converts them on the way in for
	// Windows and copies them as they are everywhere else.
	expected := map[string][]byte{
		"LICENSE.txt":            from.licence,
		"THIRD-PARTY-NOTICES.md": from.notices,
		"README.txt":             from.readme[target.os],
		"games/README.txt":       from.games,
		binary:                   nil, // checked by running it, not by its bytes
	}
	for _, script := range launchers(target.os) {
		expected[script] = from.launcher[target.os+"/"+script]
	}
	if target.os == "windows" {
		// cmd.exe parses a .bat line by line, so those need CRLF; the two
		// READMEs are Korean and get a byte order mark as well, because an
		// editor that guesses CP949 renders them as mojibake without one.
		// The .bat files stay BOM-free: cmd.exe would echo one.
		expected["README.txt"] = withBOM(toCRLF(from.readme["windows"]))
		expected["games/README.txt"] = withBOM(toCRLF(from.games))
		for _, script := range launchers("windows") {
			expected[script] = toCRLF(from.launcher["windows/"+script])
		}
	}

	for wanted, contents := range expected {
		entry, present := files[wanted]
		if !present {
			report("%s is missing", wanted)
			continue
		}
		if contents != nil && !bytes.Equal(entry.data, contents) {
			report("%s is not the file it is made from in this repository", wanted)
		}
	}
	for present := range files {
		if _, wanted := expected[present]; !wanted {
			report("%s is in the archive and should not be", present)
		}
	}

	// The empty tree the server looks for beside itself. Its presence is what
	// makes a downloaded release choose that folder as its data root rather
	// than whatever directory it was started from.
	for _, wanted := range []string{"games", "games/ktf", "games/lgt", "games/skt"} {
		if !directories[wanted] {
			report("%s/ is missing", wanted)
		}
	}

	// A launcher that is not executable is a download nobody can start by
	// double-clicking it, which is the whole of how a release is meant to be
	// run. Windows decides from the extension and its zip carries no usable
	// mode, so this is the two archive families that do.
	if target.os != "windows" {
		for _, script := range append(launchers(target.os), binary) {
			entry, present := files[script]
			if !present {
				continue
			}
			if entry.mode&0o111 == 0 {
				report("%s is not executable (mode %04o)", script, entry.mode)
			}
		}
		for present, entry := range files {
			if present == binary || strings.HasPrefix(present, "start") ||
				strings.HasPrefix(present, "stop") || strings.HasPrefix(present, "status") {
				continue
			}
			if entry.mode&0o111 != 0 {
				report("%s is executable (mode %04o) and is not a program", present, entry.mode)
			}
		}
	}

	// A text file in a Unix archive with a carriage return in it came from a
	// conversion that ran on the wrong branch. The Windows archive's LICENSE
	// and notices are deliberately left as they are, so this asks only of the
	// files whose contents are not already compared above.
	if target.os != "windows" {
		for present, entry := range files {
			if present == binary {
				continue
			}
			if bytes.Contains(entry.data, []byte("\r\n")) {
				report("%s has Windows line endings", present)
			}
		}
	} else {
		for _, script := range launchers("windows") {
			entry, present := files[script]
			if !present {
				continue
			}
			for _, character := range entry.data {
				if character > 0x7f {
					report("%s is not ASCII; cmd.exe reads it in the console's code page", script)
					break
				}
			}
		}
	}

	if entry, present := files[binary]; present && len(entry.data) == 0 {
		report("%s is empty", binary)
	}
	return problems
}

func toCRLF(text []byte) []byte {
	// The Makefile appends a carriage return to every line, including the
	// last, which is what `sed -e 's/$/\r/'` does.
	var out bytes.Buffer
	for _, line := range bytes.SplitAfter(text, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if line[len(line)-1] == '\n' {
			out.Write(line[:len(line)-1])
			out.WriteString("\r\n")
			continue
		}
		out.Write(line)
		out.WriteString("\r\n")
	}
	return out.Bytes()
}

func withBOM(text []byte) []byte {
	return append([]byte{0xef, 0xbb, 0xbf}, text...)
}

// runTheNativeArchive unpacks the one archive this machine can run and asks
// the server inside it what version it is. Cross-compiling proves a build; the
// version is stamped by a linker flag, and only a run proves it took.
func runTheNativeArchive(directory string, built map[platform]string) []string {
	name, ok := built[platform{os: runtime.GOOS, arch: runtime.GOARCH}]
	if !ok {
		return []string{fmt.Sprintf("no archive for %s/%s, so no binary here could be started", runtime.GOOS, runtime.GOARCH)}
	}
	version, _, _ := describe(name)

	work, err := os.MkdirTemp("", "distcheck")
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", name, err)}
	}
	defer os.RemoveAll(work)

	binary := "wfeature-server"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	top := strings.TrimSuffix(strings.TrimSuffix(name, ".tar.gz"), ".zip")
	items, err := readArchive(filepath.Join(directory, name))
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", name, err)}
	}
	var server []byte
	for _, entry := range items {
		if entry.name == top+"/"+binary {
			server = entry.data
		}
	}
	if server == nil {
		return []string{fmt.Sprintf("%s: no %s to run", name, binary)}
	}
	file := filepath.Join(work, binary)
	if err := os.WriteFile(file, server, 0o755); err != nil {
		return []string{fmt.Sprintf("%s: %v", name, err)}
	}

	command := exec.Command(file, "-version")
	output, err := command.CombinedOutput()
	if err != nil {
		return []string{fmt.Sprintf("%s: %s -version: %v: %s", name, binary, err, output)}
	}
	want := fmt.Sprintf("wfeature-server %s (release)", version)
	if !strings.Contains(string(output), want) {
		return []string{fmt.Sprintf("%s: %s -version says %q, expected %q", name, binary, strings.TrimSpace(string(output)), want)}
	}
	return nil
}

// repositoryRoot walks up from this source file to the directory holding
// go.mod, so the command can be run from anywhere the way `go run` is.
func repositoryRoot() (string, error) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate this source file")
	}
	directory := filepath.Dir(source)
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("no go.mod above %s; pass -root", filepath.Dir(source))
		}
		directory = parent
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
