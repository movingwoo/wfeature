package conformance

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Every registered backend has to answer the corpus. With one backend
// registered this is a regression net against the architecture; with two it is
// also the differential test, and the second assertion below is what makes it
// one without any further work.
func TestEveryBackendAnswersTheCorpus(t *testing.T) {
	names := armcore.BackendNames()
	if len(names) == 0 {
		t.Fatal("no execution backend is registered")
	}
	cases := Cases()
	if len(cases) == 0 {
		t.Fatal("the corpus is empty")
	}
	for _, probe := range cases {
		t.Run(probe.Name, func(t *testing.T) {
			var oracle Snapshot
			var oracleName string
			for _, name := range names {
				backend, ok := armcore.NewBackend(name)
				if !ok {
					t.Fatalf("%s is listed but does not build", name)
				}
				got, err := Run(backend, probe)
				if err != nil {
					t.Fatalf("%s: run the case: %v", name, err)
				}
				if differences := Differences(probe.Want, got); len(differences) > 0 {
					t.Errorf("%s does not match the architecture.\n%s\n%s",
						name, probe.Rule, format(differences))
				}
				if oracleName == "" {
					oracle, oracleName = got, name
					continue
				}
				// Two backends that are both wrong the same way still have to be
				// reported, so this is asked separately from the one above.
				if differences := Differences(oracle, got); len(differences) > 0 {
					t.Errorf("%s and %s disagree.\n%s", oracleName, name, format(differences))
				}
			}
		})
	}
}

// A backend nothing registered is the interpreter rather than a failure: a
// build that does not carry the strategy a configuration asks for still has to
// run the game.
func TestAnUnregisteredBackendFallsBackToTheInterpreter(t *testing.T) {
	core := armcore.NewCore(armcore.CoreOptions{BackendName: "a-strategy-nothing-registered"})
	if got := core.BackendName(); got != armcore.InterpreterBackend {
		t.Fatalf("backend = %q, want %q", got, armcore.InterpreterBackend)
	}
	if _, ok := armcore.NewBackend("a-strategy-nothing-registered"); ok {
		t.Fatal("an unregistered name resolved to a backend")
	}
	if _, ok := armcore.NewBackend(armcore.InterpreterBackend); !ok {
		t.Fatal("the interpreter is not registered")
	}
}

// The registry is the single injection point, so a backend handed in directly
// has to be the one a core runs through.
func TestAnInjectedBackendIsTheOneTheCoreRuns(t *testing.T) {
	counting := &countingBackend{inner: armcore.Engine{}}
	core := armcore.NewCore(armcore.CoreOptions{Backend: counting})
	if got := core.BackendName(); got != "counting" {
		t.Fatalf("backend = %q, want %q", got, "counting")
	}
	memory := core.Memory()
	if err := memory.Map(CodeBase, CodeSize, armcore.PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(CodeBase, arm(0xe3a00001)); err != nil {
		t.Fatal(err)
	}
	context := armcore.NewContext()
	if err := context.SetPC(CodeBase); err != nil {
		t.Fatal(err)
	}
	summary, err := core.Run(t.Context(), armcore.NewThread(context), CodeBase+4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Steps != 1 {
		t.Fatalf("steps = %d, want 1", summary.Steps)
	}
	if counting.runs == 0 {
		t.Fatal("the core did not run through the backend it was given")
	}
}

type countingBackend struct {
	inner armcore.Backend
	runs  int
}

func (backend *countingBackend) Name() string { return "counting" }

func (backend *countingBackend) Run(
	context *armcore.Context, memory *armcore.Memory, end uint32, count uint32,
) (armcore.RunResult, error) {
	backend.runs++
	return backend.inner.Run(context, memory, end, count)
}

func format(differences []string) string {
	text := ""
	for _, difference := range differences {
		text += "  " + difference + "\n"
	}
	return text
}
