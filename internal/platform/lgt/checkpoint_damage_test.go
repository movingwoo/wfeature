package lgt

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// javaSleeperFixture is the smallest Java title with something to restore: one
// guest thread that counts and sleeps.
func javaSleeperFixture(t *testing.T) *javaThreadFixture {
	t.Helper()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	lock, err := newTestObject(t, fixture.client, "java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	sleeper := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.increment(a, 0)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	waiter := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "enter", lock)
		fixture.call(a, "wait", lock)
		fixture.increment(a, 1)
		fixture.call(a, "exit", lock)
		fixture.loop(a, top)
	})
	fixture.start("Sleeper", sleeper)
	fixture.start("Waiter", waiter)
	fixture.settle()
	tickSession(t, fixture.session, 3)
	return fixture
}

// damageRecord changes one value somewhere in a record: a number to a boundary
// or to noise, a flag to its opposite, a list to a shorter, longer or empty
// one, an optional part to missing. It answers what it changed, for the report
// of a case that went wrong.
func damageRecord(random *rand.Rand, record any) string {
	type leaf struct {
		path  string
		value reflect.Value
	}
	var leaves []leaf
	var walk func(path string, value reflect.Value, depth int)
	walk = func(path string, value reflect.Value, depth int) {
		if depth > 12 {
			return
		}
		switch value.Kind() {
		case reflect.Struct:
			for index := 0; index < value.NumField(); index++ {
				walk(path+"."+value.Type().Field(index).Name, value.Field(index), depth+1)
			}
		case reflect.Pointer:
			leaves = append(leaves, leaf{path, value})
			if !value.IsNil() {
				walk(path, value.Elem(), depth+1)
			}
		case reflect.Slice:
			leaves = append(leaves, leaf{path, value})
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return
			}
			// One element of a long list is as good as another, and walking
			// every page of the memory image would make it the only thing
			// that is ever damaged.
			for _, index := range []int{0, value.Len() / 2, value.Len() - 1} {
				if index >= 0 && index < value.Len() {
					walk(fmt.Sprintf("%s[%d]", path, index), value.Index(index), depth+1)
				}
			}
		case reflect.Array:
			for index := 0; index < value.Len(); index++ {
				walk(fmt.Sprintf("%s[%d]", path, index), value.Index(index), depth+1)
			}
		case reflect.Bool, reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float64:
			leaves = append(leaves, leaf{path, value})
		}
	}
	walk("record", reflect.ValueOf(record).Elem(), 0)
	chosen := leaves[random.Intn(len(leaves))]
	value := chosen.value
	interesting := []uint64{0, 1, 2, 3, 0x7f, 0x80, 0xff, 0x100, 0xffff, 0x10000, 0x7fffffff, 0x80000000, 0xffffffff,
		0x20000000, 0x30000000, 0x31000000, 0x33000000, 0x40000000, 0x41000000, 0x7fff0000, 1 << 40, 1<<63 - 1, 1 << 63, ^uint64(0)}
	pick := func() uint64 {
		switch random.Intn(3) {
		case 0:
			return interesting[random.Intn(len(interesting))]
		case 1:
			return random.Uint64()
		}
		return uint64(random.Intn(64))
	}
	switch value.Kind() {
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.String:
		value.SetString(value.String() + "x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch random.Intn(4) {
		case 0:
			value.SetInt(value.Int() + 1)
		case 1:
			value.SetInt(value.Int() - 1)
		case 2:
			value.SetInt(-value.Int() - 1)
		default:
			value.SetInt(int64(pick()))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch random.Intn(4) {
		case 0:
			value.SetUint(value.Uint() + 1)
		case 1:
			value.SetUint(value.Uint() - 1)
		case 2:
			value.SetUint(value.Uint() ^ 1<<uint(random.Intn(32)))
		default:
			value.SetUint(pick())
		}
	case reflect.Float64:
		value.SetFloat([]float64{0, -1, 0.05, 17, 1e300}[random.Intn(5)])
	case reflect.Pointer:
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		} else {
			value.Set(reflect.Zero(value.Type()))
		}
	case reflect.Slice:
		switch length := value.Len(); {
		case length == 0:
			value.Set(reflect.MakeSlice(value.Type(), 1+random.Intn(3), 4))
		case random.Intn(4) == 0:
			value.Set(reflect.Zero(value.Type()))
		case random.Intn(3) == 0:
			value.Set(reflect.AppendSlice(value, value.Slice(0, 1+random.Intn(length))))
		case value.Type().Elem().Kind() == reflect.Uint8 && random.Intn(2) == 0:
			copied := reflect.MakeSlice(value.Type(), length, length)
			reflect.Copy(copied, value)
			copied.Index(random.Intn(length)).SetUint(uint64(random.Intn(256)))
			value.Set(copied)
		default:
			value.Set(value.Slice(0, random.Intn(length)))
		}
	}
	return chosen.path
}

// A record is the one input here that did not come out of an archive, and
// nothing about it can be relied on: a checksum says it was not damaged in
// transit, not that it describes a session. So a record with one value changed
// — any value — is either refused or accepted, and never takes the process
// down or holds it: a session built from one runs a tick like any other, with
// whatever the guest then makes of it reported as an error.
//
// The changes are drawn from a fixed seed, so a case that fails names the seed
// that repeats it.
func TestCheckpointSurvivesADamagedRecord(t *testing.T) {
	clet := func(t *testing.T) ([]byte, backend.Checkpoint) {
		archive := fixtureArchive(t)
		store, _ := backend.NewMemorySaveStore(nil)
		session := checkpointFixtureSession(t, archive, store)
		populateCheckpointTables(t, session)
		tickSession(t, session, 3)
		return archive, roundTripCheckpoint(t, archive, session)
	}
	java := func(t *testing.T) ([]byte, backend.Checkpoint) {
		fixture := javaSleeperFixture(t)
		return fixture.archive, roundTripCheckpoint(t, fixture.archive, fixture.session)
	}
	rounds := 250
	if value := os.Getenv("WFEATURE_LGT_CHECKPOINT_DAMAGE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("WFEATURE_LGT_CHECKPOINT_DAMAGE=%q is not a count", value)
		}
		rounds = parsed
	}
	for name, build := range map[string]func(*testing.T) ([]byte, backend.Checkpoint){"clet": clet, "java": java} {
		t.Run(name, func(t *testing.T) {
			archive, checkpoint := build(t)
			accepted, failed := 0, 0
			for round := 0; round < rounds && failed < 20; round++ {
				seed := int64(round)
				random := rand.New(rand.NewSource(seed))
				saved := decodeRuntime(t, checkpoint)
				var changed []string
				for count := 1 + random.Intn(2); count > 0; count-- {
					changed = append(changed, damageRecord(random, &saved))
				}
				record, err := backend.EncodeCheckpointRecord(saved)
				if err != nil {
					continue // not a record the encoder will write at all
				}
				damaged := checkpoint
				damaged.Runtime = record
				func() {
					// A case that does not come back is the other thing this
					// test exists to find, and a test that merely timed out
					// would not say which case it was.
					watchdog := time.AfterFunc(2*time.Minute, func() {
						panic(fmt.Sprintf("seed %d (%v) did not return", seed, changed))
					})
					defer watchdog.Stop()
					defer func() {
						if recovered := recover(); recovered != nil {
							// Every failing case is reported, not only the
							// first: two of them rarely have one cause.
							failed++
							t.Errorf("seed %d (%v): %v", seed, changed, recovered)
						}
					}()
					store, _ := backend.NewMemorySaveStore(nil)
					prepared, err := PrepareSessionCheckpoint(archive, damaged, SessionOptions{SaveStore: store})
					if err != nil {
						return
					}
					accepted++
					restored, err := prepared.Commit(context.Background(), nil, store)
					if err != nil {
						prepared.Discard()
						return
					}
					// What an accepted record does next is the guest's
					// business, bounded the way any tick is. Ten ticks is
					// long enough for what the record left pending — a
					// timer, a dial, a sleeping thread — to come due.
					bounded, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
					for tick := 0; tick < 10; tick++ {
						if restored.Tick(bounded) != nil {
							break
						}
					}
					cancel()
					_ = restored.Close(context.Background())
				}()
			}
			t.Logf("%d of %d damaged records were accepted", accepted, rounds)
		})
	}
}

// A checkpoint of a Java title is restored by a process that never built the
// threads: what each one owes is in the record, and nothing of it was left in
// the process that took it.
func TestCheckpointRestoresJavaThreadsInAnotherProcess(t *testing.T) {
	if root := os.Getenv("WFEATURE_LGT_JAVA_CHECKPOINT_FIXTURE"); root != "" {
		archive, err := os.ReadFile(filepath.Join(root, "archive.zip"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "checkpoint.wfq"))
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
		if err != nil {
			t.Fatal(err)
		}
		store, _ := backend.NewMemorySaveStore(nil)
		prepared, err := PrepareSessionCheckpoint(archive, checkpoint, SessionOptions{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		restored, err := prepared.Commit(context.Background(), nil, store)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close(context.Background())
		tickSession(t, restored, 6)
		report := fmt.Sprintf("%d %d %d %d %s",
			guestWord(t, restored, javaFixtureWords), guestWord(t, restored, javaFixtureWords+4),
			restored.Steps(), restored.GuestElapsed(), memoryDigest(t, restored.client))
		if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte(report), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	fixture := javaSleeperFixture(t)
	checkpoint, err := fixture.session.CaptureCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "archive.zip"), fixture.archive, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "checkpoint.wfq"), data, 0600); err != nil {
		t.Fatal(err)
	}
	tickSession(t, fixture.session, 6)
	want := fmt.Sprintf("%d %d %d %d %s",
		guestWord(t, fixture.session, javaFixtureWords), guestWord(t, fixture.session, javaFixtureWords+4),
		fixture.session.Steps(), fixture.session.GuestElapsed(), memoryDigest(t, fixture.client))
	program := os.Getenv("WFEATURE_LGT_CHECKPOINT_RESTORE_BINARY")
	if program == "" {
		program = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), program, "-test.run=^TestCheckpointRestoresJavaThreadsInAnotherProcess$", "-test.timeout=60s")
	process.Env = append(os.Environ(), "WFEATURE_LGT_JAVA_CHECKPOINT_FIXTURE="+root)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("restore process: %v\n%s", err, output)
	}
	got, err := os.ReadFile(filepath.Join(root, "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("the other process continued to\n%s\nwant\n%s", got, want)
	}
}
