package lgt

import (
	"context"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `String.intern()`: one String object for each text a title interns.
//
// The method takes slot 35, one past the toCharArray anchor at 34 — the last
// method the class page declares. It answers what the KTF and SKT runtimes
// answer (internal/jvm): the String already pooled under the same text, or,
// for a text nothing has interned yet, the receiver itself, which stays the
// text's String for the life of the title. Their pool is bounded the same way —
// 65,536 texts, 16 MiB of them — and a title past either bound gets the
// OutOfMemoryError theirs does; this one stops at half the texts, because the
// pool is kept in a table a checkpoint holds 65,536 entries of at most.
//
// **The pool is the singletons table**, under a key no class name can take:
// an interned String is one object for the life of the title, answered by
// name, which is what a singleton is, and that table is already a root of the
// collector and part of every checkpoint. So the pool needs no state of its
// own and an earlier checkpoint loads with it empty. Counting the pool against
// its bounds walks the table, a cost only a text not pooled yet pays.
//
// One difference remains: those runtimes pool every string constant as it is
// loaded, and this one does not, because a constant here is built by the
// module's own helper and kept in its own cache (takeJavaStringConstant). A
// title that compares `s.intern() == "text"` is true here only when the
// constant it compares with is the object that was interned first.

const (
	// javaInternKey is the prefix of a pooled text's singleton name. A
	// parenthesis is never part of a class name.
	javaInternKey            = "java/lang/String.intern()"
	maxJavaInternedStrings   = 1 << 15
	maxJavaInternedTextBytes = 16 << 20
)

// javaStringIntern is `intern()`, slot 35.
func javaStringIntern(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	runtime := client.javaRuntimeState()
	key := javaInternKey + text
	if pooled, ok := runtime.singletons[key]; ok {
		return pooled, nil
	}
	count, size := 0, 0
	for name := range runtime.singletons {
		if strings.HasPrefix(name, javaInternKey) {
			count++
			size += len(name) - len(javaInternKey)
		}
	}
	if count >= maxJavaInternedStrings || len(text) > maxJavaInternedTextBytes-size {
		return 0, client.throwJavaPlatform(thread, javaOutOfMemoryClass, ": the interned string pool is full")
	}
	runtime.singletons[key] = arguments[0]
	return arguments[0], nil
}
