package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func checkpointAudioFixture() AudioState {
	return AudioState{
		Version: 9, Next: 1, MaxSounds: defaultMaxSounds, Volume: 100,
		Sounds: []AudioSoundState{{Handle: 1, Volume: 100, Length: time.Second, Events: []smaf.Event{
			{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{11, 22, 33, 44}},
			{Time: 1000, Type: smaf.EventEnd},
		}}},
		Output: AudioOutputState{
			Channels: defaultAudioChannels(),
			Sounds:   []AudioSoundOutputState{{Sound: 1, Gain: AudioGainUnity, Channels: defaultAudioChannels()}},
			Waves:    []AudioWaveState{{Sound: 1, Channels: 1, Rate: 4, Samples: []int16{11, 22, 33, 44}, BudgetBytes: 8, FramePhase: 0}},
		},
	}
}

func checkpointAudioDocument(t *testing.T, legacy bool) map[string]any {
	t.Helper()
	version := uint32(9)
	if legacy {
		version = 6
	}
	return checkpointAudioVersionDocument(t, version)
}

func checkpointAudioVersionFixture(version uint32) AudioState {
	state := checkpointAudioFixture()
	state.Version = version
	if version == 6 || version == 7 {
		state.Output.Waves[0].BudgetBytes = 0
	}
	if version == 6 || version == 7 || version == 8 {
		state.Output.Waves[0].FramePhase = 0
	}
	return state
}

func checkpointAudioVersionDocument(t *testing.T, version uint32) map[string]any {
	t.Helper()
	data, err := EncodeCheckpointRecord(checkpointAudioFixture())
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["Version"] = version
	remove := func(object map[string]any, field string) {
		if _, ok := object[field]; !ok {
			t.Fatalf("fixture lacks the new field %s", field)
		}
		delete(object, field)
	}
	output := record["Output"].(map[string]any)
	if version == 6 {
		for _, sound := range record["Sounds"].([]any) {
			for _, event := range sound.(map[string]any)["Events"].([]any) {
				remove(event.(map[string]any), "PCMChannel")
			}
		}
		remove(output, "PCMChannels")
		for _, wave := range output["Waves"].([]any) {
			remove(wave.(map[string]any), "PCMChannel")
		}
	}
	if version == 6 || version == 7 {
		for _, wave := range output["Waves"].([]any) {
			remove(wave.(map[string]any), "BudgetBytes")
		}
	}
	if version == 6 || version == 7 || version == 8 {
		for _, wave := range output["Waves"].([]any) {
			remove(wave.(map[string]any), "FramePhase")
		}
	}
	return record
}

func checkpointAudioJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func checkpointAudioEvent(record map[string]any, index int) map[string]any {
	return record["Sounds"].([]any)[0].(map[string]any)["Events"].([]any)[index].(map[string]any)
}

func checkpointAudioWave(record map[string]any) map[string]any {
	return record["Output"].(map[string]any)["Waves"].([]any)[0].(map[string]any)
}

func TestCheckpointAudioLegacyShapeAndFieldOrder(t *testing.T) {
	for _, shapeVersion := range []uint32{6, 7, 8, 9} {
		record := checkpointAudioVersionDocument(t, shapeVersion)
		version := checkpointAudioJSON(t, record["Version"])
		delete(record, "Version")
		other := checkpointAudioJSON(t, record)
		first := append([]byte(`{"Version":`), version...)
		first = append(append(first, ','), other[1:]...)
		last := append(bytes.Clone(other[:len(other)-1]), []byte(`,"Version":`)...)
		last = append(append(last, version...), '}')
		want := checkpointAudioVersionFixture(shapeVersion)
		for _, data := range [][]byte{first, last} {
			var got AudioState
			if err := DecodeCheckpointRecord(data, &got); err != nil {
				t.Fatalf("version=%d: %v", shapeVersion, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("version=%d changed decoded audio: got %+v, want %+v", shapeVersion, got, want)
			}
			if err := validateAudioState(got); err != nil {
				t.Fatalf("version=%d fixture is semantically invalid: %v", shapeVersion, err)
			}
		}
	}
}

func TestCheckpointAudioCurrentPCMDataPreserved(t *testing.T) {
	for _, version := range []uint32{7, 8, 9} {
		want := checkpointAudioVersionFixture(version)
		want.Sounds[0].Events[0].PCMChannel = 35
		want.Output.Waves[0].PCMChannel = 35
		want.Output.PCMChannels = []AudioPCMChannelState{{Sound: 1, Channel: 35, VolumeSet: true, Volume: 0, Expression: 42, Pan: 64}}
		record := checkpointAudioVersionDocument(t, version)
		checkpointAudioEvent(record, 0)["PCMChannel"] = 35
		checkpointAudioWave(record)["PCMChannel"] = 35
		record["Output"].(map[string]any)["PCMChannels"] = want.Output.PCMChannels
		if version == 9 {
			want.Output.Waves[0].FramePhase = 1
			checkpointAudioWave(record)["FramePhase"] = 1
		}
		var got AudioState
		if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("version=%d PCM fields, admission charge or frame phase did not round-trip: %v", version, err)
		}
	}
}

func TestCheckpointAudioLegacyEmptyListsAndPreflightLimits(t *testing.T) {
	for _, version := range []uint32{6, 7, 8, 9} {
		record := checkpointAudioVersionDocument(t, version)
		record["Sounds"] = nil
		record["Output"].(map[string]any)["Waves"] = nil
		data := checkpointAudioJSON(t, record)
		var got AudioState
		if err := DecodeCheckpointRecord(data, &got); err != nil || got.Version != version || got.Sounds != nil || got.Output.Waves != nil {
			t.Fatalf("version=%d empty audio lists differ: %v", version, err)
		}
		for _, alter := range []func(*checkpointRecordLimits){
			func(limits *checkpointRecordLimits) { limits.decoded = 1 },
			func(limits *checkpointRecordLimits) { limits.nodes = 1 },
			func(limits *checkpointRecordLimits) { limits.depth = 1 },
		} {
			limits := defaultCheckpointRecordLimits()
			alter(&limits)
			if err := inspectCheckpointRecord(data, reflect.TypeFor[AudioState](), limits); err == nil {
				t.Fatalf("version=%d shape bypassed a preflight limit", version)
			}
		}
	}
}

func TestCheckpointAudioRejectsHybridOrMissingFieldsAtomically(t *testing.T) {
	for _, test := range []struct {
		name    string
		version uint32
		change  func(map[string]any)
	}{
		{"v6 event addition", 6, func(r map[string]any) { checkpointAudioEvent(r, 0)["PCMChannel"] = 0 }},
		{"v6 second event addition", 6, func(r map[string]any) { checkpointAudioEvent(r, 1)["PCMChannel"] = 0 }},
		{"v6 wave addition", 6, func(r map[string]any) { checkpointAudioWave(r)["PCMChannel"] = 0 }},
		{"v6 output addition", 6, func(r map[string]any) { r["Output"].(map[string]any)["PCMChannels"] = nil }},
		{"v6 budget addition", 6, func(r map[string]any) { checkpointAudioWave(r)["BudgetBytes"] = 0 }},
		{"v7 budget addition", 7, func(r map[string]any) { checkpointAudioWave(r)["BudgetBytes"] = 0 }},
		{"v6 phase addition", 6, func(r map[string]any) { checkpointAudioWave(r)["FramePhase"] = 0 }},
		{"v7 phase addition", 7, func(r map[string]any) { checkpointAudioWave(r)["FramePhase"] = 0 }},
		{"v8 phase addition", 8, func(r map[string]any) { checkpointAudioWave(r)["FramePhase"] = 0 }},
		{"v6 full v7 shape", 7, func(r map[string]any) { r["Version"] = 6 }},
		{"v6 full v8 shape", 8, func(r map[string]any) { r["Version"] = 6 }},
		{"v7 full v8 shape", 8, func(r map[string]any) { r["Version"] = 7 }},
		{"v6 full current shape", 9, func(r map[string]any) { r["Version"] = 6 }},
		{"v7 full current shape", 9, func(r map[string]any) { r["Version"] = 7 }},
		{"v8 full current shape", 9, func(r map[string]any) { r["Version"] = 8 }},
		{"v7 event omission", 7, func(r map[string]any) { delete(checkpointAudioEvent(r, 0), "PCMChannel") }},
		{"v7 second event omission", 7, func(r map[string]any) { delete(checkpointAudioEvent(r, 1), "PCMChannel") }},
		{"v7 wave omission", 7, func(r map[string]any) { delete(checkpointAudioWave(r), "PCMChannel") }},
		{"v7 output omission", 7, func(r map[string]any) { delete(r["Output"].(map[string]any), "PCMChannels") }},
		{"v8 event omission", 8, func(r map[string]any) { delete(checkpointAudioEvent(r, 0), "PCMChannel") }},
		{"v8 second event omission", 8, func(r map[string]any) { delete(checkpointAudioEvent(r, 1), "PCMChannel") }},
		{"v8 wave omission", 8, func(r map[string]any) { delete(checkpointAudioWave(r), "PCMChannel") }},
		{"v8 output omission", 8, func(r map[string]any) { delete(r["Output"].(map[string]any), "PCMChannels") }},
		{"v8 budget omission", 8, func(r map[string]any) { delete(checkpointAudioWave(r), "BudgetBytes") }},
		{"v8 second wave budget omission", 8, func(r map[string]any) {
			wave := checkpointAudioWave(checkpointAudioVersionDocument(t, 8))
			delete(wave, "BudgetBytes")
			r["Output"].(map[string]any)["Waves"] = []any{checkpointAudioWave(r), wave}
		}},
		{"v7 entire v6 shape", 6, func(r map[string]any) { r["Version"] = 7 }},
		{"v8 entire v6 shape", 6, func(r map[string]any) { r["Version"] = 8 }},
		{"v8 entire v7 shape", 7, func(r map[string]any) { r["Version"] = 8 }},
		{"current entire v6 shape", 6, func(r map[string]any) { r["Version"] = 9 }},
		{"current entire v7 shape", 7, func(r map[string]any) { r["Version"] = 9 }},
		{"current entire v8 shape", 8, func(r map[string]any) { r["Version"] = 9 }},
		{"current event omission", 9, func(r map[string]any) { delete(checkpointAudioEvent(r, 0), "PCMChannel") }},
		{"current second event omission", 9, func(r map[string]any) { delete(checkpointAudioEvent(r, 1), "PCMChannel") }},
		{"current wave omission", 9, func(r map[string]any) { delete(checkpointAudioWave(r), "PCMChannel") }},
		{"current output omission", 9, func(r map[string]any) { delete(r["Output"].(map[string]any), "PCMChannels") }},
		{"current budget omission", 9, func(r map[string]any) { delete(checkpointAudioWave(r), "BudgetBytes") }},
		{"current phase omission", 9, func(r map[string]any) { delete(checkpointAudioWave(r), "FramePhase") }},
		{"current second wave phase omission", 9, func(r map[string]any) {
			wave := checkpointAudioWave(checkpointAudioVersionDocument(t, 8))
			r["Output"].(map[string]any)["Waves"] = []any{checkpointAudioWave(r), wave}
		}},
		{"legacy unknown field", 6, func(r map[string]any) { checkpointAudioEvent(r, 0)["Future"] = 0 }},
		{"legacy wrong version case", 6, func(r map[string]any) { delete(r, "Version"); r["version"] = 6 }},
		{"legacy null version", 6, func(r map[string]any) { r["Version"] = nil }},
		{"legacy version overflow", 6, func(r map[string]any) { r["Version"] = uint64(1) << 32 }},
		{"v8 channel overflow", 8, func(r map[string]any) { checkpointAudioEvent(r, 0)["PCMChannel"] = 65536 }},
		{"v8 null channel", 8, func(r map[string]any) { checkpointAudioWave(r)["PCMChannel"] = nil }},
		{"v8 wrong group shape", 8, func(r map[string]any) { r["Output"].(map[string]any)["PCMChannels"] = map[string]any{} }},
		{"v8 null budget", 8, func(r map[string]any) { checkpointAudioWave(r)["BudgetBytes"] = nil }},
		{"v8 budget overflow", 8, func(r map[string]any) { checkpointAudioWave(r)["BudgetBytes"] = uint64(1) << 32 }},
		{"current null phase", 9, func(r map[string]any) { checkpointAudioWave(r)["FramePhase"] = nil }},
		{"current negative phase", 9, func(r map[string]any) { checkpointAudioWave(r)["FramePhase"] = -1 }},
		{"current phase overflow", 9, func(r map[string]any) { checkpointAudioWave(r)["FramePhase"] = uint64(1) << 32 }},
		{"current unknown wave field", 9, func(r map[string]any) { checkpointAudioWave(r)["FuturePhase"] = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := checkpointAudioVersionDocument(t, test.version)
			test.change(record)
			target, want := checkpointAudioFixture(), checkpointAudioFixture()
			target.Next, want.Next = 99, 99
			if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &target); !errors.Is(err, ErrCheckpointDamaged) {
				t.Fatalf("invalid shape returned %v", err)
			}
			if !reflect.DeepEqual(target, want) {
				t.Fatal("refused shape changed destination")
			}
		})
	}
	for _, version := range []uint32{6, 7, 8, 9} {
		for _, test := range []struct {
			name   string
			change func(map[string]any)
		}{
			{"missing version", func(r map[string]any) { delete(r, "Version") }},
			{"missing old event field", func(r map[string]any) { delete(checkpointAudioEvent(r, 0), "Time") }},
			{"missing old wave field", func(r map[string]any) { delete(checkpointAudioWave(r), "Rate") }},
			{"missing old output field", func(r map[string]any) { delete(r["Output"].(map[string]any), "Notes") }},
		} {
			t.Run(fmt.Sprintf("v%d %s", version, test.name), func(t *testing.T) {
				record := checkpointAudioVersionDocument(t, version)
				test.change(record)
				target, want := checkpointAudioFixture(), checkpointAudioFixture()
				if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &target); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(target, want) {
					t.Fatalf("old required field omission was not rejected atomically: %v", err)
				}
			})
		}
		data := checkpointAudioJSON(t, checkpointAudioVersionDocument(t, version))
		duplicates := [][]byte{bytes.Replace(data, []byte(`"Version":`), []byte(`"Version":6,"Version":`), 1)}
		if version >= 7 {
			duplicates = append(duplicates, bytes.Replace(data, []byte(`"PCMChannel":`), []byte(`"PCMChannel":0,"PCMChannel":`), 1))
		}
		if version >= 8 {
			duplicates = append(duplicates, bytes.Replace(data, []byte(`"BudgetBytes":`), []byte(`"BudgetBytes":0,"BudgetBytes":`), 1))
		}
		if version >= 9 {
			duplicates = append(duplicates, bytes.Replace(data, []byte(`"FramePhase":`), []byte(`"FramePhase":0,"FramePhase":`), 1))
		}
		for _, bad := range duplicates {
			target, want := checkpointAudioFixture(), checkpointAudioFixture()
			if err := DecodeCheckpointRecord(bad, &target); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(target, want) {
				t.Fatalf("version=%d duplicate field was not rejected atomically: %v", version, err)
			}
		}
	}
}

func TestCheckpointAudioVersionContextIsLocal(t *testing.T) {
	type wrapper struct {
		Version uint32
		Legacy6 AudioState
		Legacy7 *AudioState
		Legacy8 AudioState
		Missing *AudioState
		Current *AudioState
		Rows    []AudioState
		Event   smaf.Event
		Wave    AudioWaveState
	}
	current := checkpointAudioFixture()
	legacy6 := checkpointAudioVersionFixture(6)
	legacy7 := checkpointAudioVersionFixture(7)
	legacy8 := checkpointAudioVersionFixture(8)
	want := wrapper{Version: 6, Legacy6: legacy6, Legacy7: &legacy7, Legacy8: legacy8, Current: &current,
		Rows: []AudioState{current, legacy6, legacy7, legacy8, current}, Event: current.Sounds[0].Events[0], Wave: current.Output.Waves[0]}
	record := map[string]any{
		"Version": 6, "Legacy6": checkpointAudioVersionDocument(t, 6), "Legacy7": checkpointAudioVersionDocument(t, 7), "Legacy8": checkpointAudioVersionDocument(t, 8), "Missing": nil,
		"Current": checkpointAudioVersionDocument(t, 9),
		"Rows":    []any{checkpointAudioVersionDocument(t, 9), checkpointAudioVersionDocument(t, 6), checkpointAudioVersionDocument(t, 7), checkpointAudioVersionDocument(t, 8), checkpointAudioVersionDocument(t, 9)},
		"Event":   current.Sounds[0].Events[0], "Wave": current.Output.Waves[0],
	}
	var got wrapper
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("independent audio contexts differ: %v", err)
	}
	delete(checkpointAudioEvent(record["Current"].(map[string]any), 0), "PCMChannel")
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy sibling relaxed current audio or changed destination: %v", err)
	}
	record["Current"] = checkpointAudioVersionDocument(t, 9)
	delete(checkpointAudioWave(record["Current"].(map[string]any)), "BudgetBytes")
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy sibling relaxed current wave charge or changed destination: %v", err)
	}
	record["Current"] = checkpointAudioVersionDocument(t, 9)
	delete(checkpointAudioWave(record["Current"].(map[string]any)), "FramePhase")
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy sibling relaxed current wave phase or changed destination: %v", err)
	}
	record["Current"] = checkpointAudioVersionDocument(t, 9)
	checkpointAudioWave(record["Legacy7"].(map[string]any))["BudgetBytes"] = 0
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("current sibling relaxed legacy wave charge or changed destination: %v", err)
	}
	record["Legacy7"] = checkpointAudioVersionDocument(t, 7)
	checkpointAudioWave(record["Legacy8"].(map[string]any))["FramePhase"] = 0
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("current sibling relaxed legacy wave phase or changed destination: %v", err)
	}
	record["Legacy8"] = checkpointAudioVersionDocument(t, 8)
	// An outer Version=6 does not give unrelated events an audio version.
	event := checkpointAudioEvent(checkpointAudioVersionDocument(t, 6), 0)
	record["Event"] = event
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("unscoped event omission was accepted or changed destination: %v", err)
	}
	record["Event"] = current.Sounds[0].Events[0]
	record["Wave"] = checkpointAudioWave(checkpointAudioVersionDocument(t, 7))
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("unscoped wave charge omission was accepted or changed destination: %v", err)
	}
	record["Wave"] = checkpointAudioWave(checkpointAudioVersionDocument(t, 8))
	if err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got); !errors.Is(err, ErrCheckpointDamaged) || !reflect.DeepEqual(got, want) {
		t.Fatalf("unscoped wave phase omission was accepted or changed destination: %v", err)
	}
}

func TestCheckpointAudioUnknownVersionsKeepStrictShape(t *testing.T) {
	for _, version := range []uint32{0, 5, 10, 1<<32 - 1} {
		for _, shapeVersion := range []uint32{6, 7, 8, 9} {
			record := checkpointAudioVersionDocument(t, shapeVersion)
			record["Version"] = version
			var got AudioState
			err := DecodeCheckpointRecord(checkpointAudioJSON(t, record), &got)
			if shapeVersion != 9 {
				if !errors.Is(err, ErrCheckpointDamaged) {
					t.Fatalf("version %d accepted version %d shape with missing current fields: %v", version, shapeVersion, err)
				}
			} else if err != nil || got.Version != version {
				t.Fatalf("component version should be checked after full-shape decoding: %v", err)
			} else if err := validateAudioState(got); err == nil {
				t.Fatalf("component validation accepted unsupported version %d", version)
			}
		}
	}
}
