package lgt

import (
	"context"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The socket factory is refused rather than absent. The difference is the
// whole point: an unimplemented slot stops the session with a host error that
// no guest `catch` can see, and one local title's own error path is exactly
// what has to run for it to get past a server that is not there.
func TestASocketIsRefusedWithTheFailureTheSpecificationNames(t *testing.T) {
	const find = "org/kwis/msf/io/URL.find(Ljava/lang/String;)Lorg/kwis/msf/io/Socket;"
	if _, ok := javaPlatformMethods[find]; !ok {
		t.Fatalf("%s is not served, so a title that dials stops the session", find)
	}

	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	// Nothing is armed, so the throw comes back as the failure that stops the
	// run — which is where its text can be read.
	_, err := javaURLFind(client, context.Background(), thread, []uint32{0x1234})
	if err == nil {
		t.Fatal("a refused socket answered instead of throwing")
	}
	if !strings.Contains(err.Error(), javaSchemeNotFoundClass) {
		t.Errorf("the refusal is %q, want it to name %s", err, javaSchemeNotFoundClass)
	}
	if !strings.Contains(err.Error(), "0x1234") {
		t.Errorf("the refusal is %q, want it to name what was dialled", err)
	}
}

// A refusal only helps if the title's `catch` matches it, and what decides
// that is the chain the type check walks. The specification roots
// `SchemeNotFoundException` at `IOException`, so a connection attempt wrapped
// in `catch (IOException)` — or the `catch (Exception)` the local title
// actually uses — has to match.
func TestARefusedSocketIsCaughtAsAnIOException(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaSchemeNotFoundClass)
	if err != nil {
		t.Fatalf("preparePlatformJavaClass(%s) error = %v", javaSchemeNotFoundClass, err)
	}
	chain := []string{}
	for step := class; step != nil; step = client.javaSuperOf(step) {
		chain = append(chain, step.Name)
	}
	for _, want := range []string{
		javaSchemeNotFoundClass, "java/io/IOException",
		"java/lang/Exception", "java/lang/Throwable",
	} {
		found := false
		for _, name := range chain {
			found = found || name == want
		}
		if !found {
			t.Errorf("the chain is %v, want %s in it", chain, want)
		}
	}
}
