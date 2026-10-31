package ktf

import (
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Remember which visible card owned activation so a later overlay cannot
// inherit an old C editor merely by requesting a fresh Host snapshot.
func (runtime *initializationRuntime) cInputCard() *jvm.Object {
	if len(runtime.displayCards) == 0 {
		return nil
	}
	return runtime.displayCards[len(runtime.displayCards)-1]
}
