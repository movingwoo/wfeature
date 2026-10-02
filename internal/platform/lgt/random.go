package lgt

import (
	"encoding/binary"
	"fmt"
	"math/rand"
)

// guestRandom is a generator a title seeded, kept in a form a checkpoint can
// hold.
//
// The sequence a seed names is the standard library's, and a title's run
// depends on it: a seed it stored and gave back has to give the same game. But
// the library's generator cannot be asked what state it is in, so one of its
// own could only be restored by seeding it again and drawing as many values as
// the title had — and a title draws for as long as it runs.
//
// What makes it possible to hold the state anyway is what the generator is: an
// additive lagged sequence, in which every value is the sum of the values 607
// and 273 places before it. **Its whole state is therefore its last 607
// values**, and each of those was handed out as a result. So this takes the
// first 607 values from the library, which is the only part a seed decides,
// and produces every later one itself from the ones before. The sequence is
// the library's, value for value, and the state is 607 numbers a record can
// carry and a restore can put straight back.
type guestRandom struct {
	*rand.Rand
	source *laggedSource
}

const (
	// laggedLength and laggedTap are the two lags of the sequence.
	laggedLength = 607
	laggedTap    = 273
)

// laggedSource is the sequence. history holds its most recent 607 values in a
// ring whose oldest is at `at`; primed counts how many of the first 607, which
// came from the library, have not been handed out yet.
type laggedSource struct {
	history [laggedLength]int64
	at      int
	primed  int
}

func (source *laggedSource) Seed(seed int64) {
	seeded := rand.NewSource(seed).(rand.Source64)
	for index := range source.history {
		source.history[index] = int64(seeded.Uint64())
	}
	source.at, source.primed = 0, laggedLength
}

func (source *laggedSource) Uint64() uint64 {
	if source.primed > 0 {
		value := source.history[laggedLength-source.primed]
		source.primed--
		return uint64(value)
	}
	// The oldest value is 607 places back, and the one 273 places back is 334
	// further on in the ring. The new value takes the oldest one's place.
	value := source.history[source.at] + source.history[(source.at+laggedLength-laggedTap)%laggedLength]
	source.history[source.at] = value
	source.at = (source.at + 1) % laggedLength
	return uint64(value)
}

func (source *laggedSource) Int63() int64 { return int64(source.Uint64() &^ (1 << 63)) }

func newGuestRandom(seed int64) *guestRandom {
	source := &laggedSource{}
	source.Seed(seed)
	return &guestRandom{Rand: rand.New(source), source: source}
}

// guestRandomState is one generator in a checkpoint: the ring as bytes, where
// its oldest value is, and how many of its first values are still to come.
type guestRandomState struct {
	History []byte
	At      int
	Primed  int
}

func (random *guestRandom) captureState() (guestRandomState, error) {
	if random == nil || random.source == nil {
		return guestRandomState{}, fmt.Errorf("LGT checkpoint has a generator with no source")
	}
	saved := guestRandomState{History: make([]byte, 0, laggedLength*8), At: random.source.at, Primed: random.source.primed}
	for _, value := range random.source.history {
		saved.History = binary.LittleEndian.AppendUint64(saved.History, uint64(value))
	}
	return saved, saved.validate()
}

func (saved guestRandomState) validate() error {
	if len(saved.History) != laggedLength*8 || saved.At < 0 || saved.At >= laggedLength ||
		saved.Primed < 0 || saved.Primed > laggedLength || saved.Primed != 0 && saved.At != 0 {
		return fmt.Errorf("LGT checkpoint has a generator that is not one")
	}
	return nil
}

func restoreGuestRandom(saved guestRandomState) (*guestRandom, error) {
	if err := saved.validate(); err != nil {
		return nil, err
	}
	source := &laggedSource{at: saved.At, primed: saved.Primed}
	for index := range source.history {
		source.history[index] = int64(binary.LittleEndian.Uint64(saved.History[index*8:]))
	}
	return &guestRandom{Rand: rand.New(source), source: source}, nil
}
