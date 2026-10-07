package lgt

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"time"
)

// The Go half of an AOT Java title
//
// A Java title's objects are guest memory, and the memory image carries them.
// What stands behind them does not: the text of a String, the bytes of an open
// stream, which surface an Image draws into and which class a vtable belongs
// to are all kept on this side, keyed by the object the module holds. These
// records are those tables, each written out in the order of its keys so the
// same runtime always writes the same record.
//
// Guest threads are in checkpoint_worker.go. Everything here is data.

type javaMemberState struct {
	Name, Descriptor  []byte
	Kind, Index, Body uint32
}

type javaInterfaceState struct {
	Name                []byte
	Handle, Slot, Entry uint32
}

// javaRecordState is a class record as it was read out of the module.
type javaRecordState struct {
	Handle, Header, AccessFlags uint32
	Name, Super                 []byte
	SuperHandle                 uint32
	SuperName                   []byte
	InstanceSize, VTableSize    uint16
	StaticWords                 uint32
	Body                        [3]uint32
	VTable                      uint32
	Fields, Methods             []javaMemberState
	End                         uint32
	Inline                      bool
	Interfaces                  []javaInterfaceState
}

// javaClassState is one class this platform has laid out. Super is the index
// of another class in the same table, or -1.
type javaClassState struct {
	Name                            []byte
	Handle                          uint32
	Record                          javaRecordState
	Object, VTable, Slots, Instance uint32
	Super                           int32
	ElementBytes, StaticWords       uint32
	Measured, Initialized           bool
	DataBlock                       uint32
}

type javaClassKeyState struct {
	Key   uint32
	Class int32
}

type javaClassNameState struct {
	Name  []byte
	Class int32
}

type javaMemberRefState struct{ Name, Descriptor []byte }

type javaAPIClassState struct {
	Name                                                         []byte
	Fields, StaticFields, VirtualMethods, Methods, StaticMethods javaRun
}

type javaSurfaceState struct {
	Classes                                                      []javaAPIClassState
	Fields, StaticFields, VirtualMethods, Methods, StaticMethods []javaMemberRefState
	FieldsOut, StaticFieldsOut, VirtualMethodsOut                uint32
	MethodsOut, StaticMethodsOut                                 uint32
}

type javaSlotState struct {
	Key  []byte
	Slot uint32
}

// javaLayoutClassState is one class's answers to the load call. A nil list is
// a table the runtime never made, which it tells apart from an empty one.
type javaLayoutClassState struct {
	Key, Name, Super         []byte
	InstanceSize, VTableSize uint32
	Virtual, Fields, Statics []javaSlotState
	InstanceWords            uint32
	Application              bool
	StaticWords              uint32
	Measured                 bool
}

type javaLinkState struct {
	Surface   *javaSurfaceState
	HasLayout bool
	Layout    []javaLayoutClassState
}

type javaTextState struct {
	Object uint32
	Text   []byte
}

type javaNamedState struct {
	Name   []byte
	Object uint32
}

type javaRandomState struct {
	Object uint32
	State  guestRandomState
}

// javaStreamState is one open stream. A resource stream's bytes are the
// archive's own, so the record names the entry instead of repeating it: a
// title that never closes its streams holds every resource it has ever read.
type javaStreamState struct {
	Object   uint32
	Name     []byte
	Archive  uint8
	Key      []byte
	Data     []byte
	Read     int
	Closed   bool
	Source   *javaStreamSource
	Markable bool
	Mark     int
}

type javaThreadState struct {
	Object, Runnable uint32
	Priority         int32
	// Worker is the index of this thread's worker among the live ones, or -1.
	// Finished marks a thread whose worker has ended and been dropped from
	// that list, which `isAlive` and a second `start` still have to see.
	Worker   int32
	Finished bool
}

type javaMonitorState struct {
	Object uint32
	// Owner is a live worker's index, or -1 for the platform's own thread.
	Owner int32
	Count int
}

type javaVectorState struct {
	Object uint32
	Items  []byte
}

type javaCalendarFieldsState struct {
	Year, Month, Day, Hour, Minute, Second, Millisecond int
}

type javaCalendarState struct {
	Object  uint32
	Millis  int64
	Pending *javaCalendarFieldsState
}

type javaBytesState struct {
	Object uint32
	Data   []byte
}

type javaKeyedSurfaceState struct {
	Key     []byte
	Surface uint32
}

type javaWidgetState struct {
	Object                 uint32
	Text                   []byte
	MaxLength              int32
	Revision               uint64
	Kind                   uint8
	Mode                   int32
	Listener, InputHandler uint32
	Children               []uint32
	Parent                 uint32
	Shown                  bool
	VisibilityRevision     uint64
	Focused                bool
}

type javaDateState struct {
	Object uint32
	Millis int64
}

type javaDatabaseState struct {
	Object     uint32
	Name       []byte
	RecordSize uint32
	Records    [][]byte
	Deleted    []bool
	Modified   int64
	Closed     bool
}

type javaGraphicsState struct {
	Object, Surface        uint32
	Color                  uint16
	TranslateX, TranslateY int
	ClipX, ClipY           int
	ClipWidth, ClipHeight  int
	ClipSet                bool
	Alpha                  int
	Packed                 uint32
	Xor                    bool
}

// javaState is the whole of javaRuntime, with the platform link beside it.
type javaState struct {
	Link *javaLinkState

	Classes  []javaClassState
	ByHandle []javaClassKeyState
	ByObject []javaClassKeyState
	ByName   []javaClassNameState

	Strings    []javaTextState
	Singletons []javaNamedState
	Random     []javaRandomState
	Streams    []javaStreamState
	// Images, Files, Wrapped, SinkFiles and StreamFiles are pairs of words:
	// the object, and what it stands for.
	Images, Files, Wrapped, SinkFiles, StreamFiles []byte

	Threads      []javaThreadState
	Workers      []javaWorkerState
	ThreadStacks int
	Monitors     []javaMonitorState
	MainThread   uint32
	KeyChecks    uint32

	Vectors       []javaVectorState
	Calendars     []javaCalendarState
	Sinks         []javaBytesState
	DecodedImages []javaKeyedSurfaceState
	Widgets       []javaWidgetState
	FocusedWidget uint32
	WidgetGen     uint64
	Dates         []javaDateState
	Databases     []javaDatabaseState
	Graphics      []javaGraphicsState

	Card           uint32
	CardDirty      bool
	ScreenGraphics uint32
	Serial         []uint32
	Jlet           uint32
	TryBuffers     []uint32

	// Objects is every tracked object: its address, its field block, the
	// block's size and whether the last cycle condemned it, thirteen bytes each.
	Objects         []byte
	CollectAt       uint64
	Condemned       int
	SinceCollection int
	Collected       CollectionStats
	Collections     int
}

const javaObjectStateBytes = 13

func packPairs(table map[uint32]uint32) []byte {
	data := make([]byte, 0, len(table)*8)
	for _, key := range slices.Sorted(maps.Keys(table)) {
		data = binary.LittleEndian.AppendUint32(data, key)
		data = binary.LittleEndian.AppendUint32(data, table[key])
	}
	return data
}

func unpackPairs(data []byte, what string) (map[uint32]uint32, error) {
	if len(data)%8 != 0 || len(data)/8 > maxStatePacked {
		return nil, fmt.Errorf("LGT checkpoint %s table is malformed", what)
	}
	table := make(map[uint32]uint32, len(data)/8)
	for offset := 0; offset < len(data); offset += 8 {
		key := binary.LittleEndian.Uint32(data[offset:])
		if offset > 0 && binary.LittleEndian.Uint32(data[offset-8:]) >= key {
			return nil, fmt.Errorf("LGT checkpoint %s table is out of order", what)
		}
		table[key] = binary.LittleEndian.Uint32(data[offset+4:])
	}
	return table, nil
}

func captureJavaMembers(members []javaMember) []javaMemberState {
	if members == nil {
		return nil
	}
	saved := make([]javaMemberState, 0, len(members))
	for _, member := range members {
		saved = append(saved, javaMemberState{[]byte(member.Name), []byte(member.Descriptor), member.Kind, member.Index, member.Body})
	}
	return saved
}

func restoreJavaMembers(saved []javaMemberState) []javaMember {
	if saved == nil {
		return nil
	}
	members := make([]javaMember, 0, len(saved))
	for _, member := range saved {
		members = append(members, javaMember{string(member.Name), string(member.Descriptor), member.Kind, member.Index, member.Body})
	}
	return members
}

func captureJavaRecord(record javaClass) javaRecordState {
	saved := javaRecordState{
		Handle: record.Handle, Header: record.Header, AccessFlags: record.AccessFlags,
		Name: []byte(record.Name), Super: []byte(record.Super), SuperHandle: record.SuperHandle,
		SuperName: []byte(record.SuperName), InstanceSize: record.InstanceSize, VTableSize: record.VTableSize,
		StaticWords: record.StaticWords, Body: record.Body, VTable: record.VTable,
		Fields: captureJavaMembers(record.Fields), Methods: captureJavaMembers(record.Methods),
		End: record.End, Inline: record.Inline,
	}
	for _, implemented := range record.Interfaces {
		saved.Interfaces = append(saved.Interfaces, javaInterfaceState{[]byte(implemented.Name), implemented.Handle, implemented.Slot, implemented.Entry})
	}
	return saved
}

func restoreJavaRecord(saved javaRecordState) javaClass {
	record := javaClass{
		Handle: saved.Handle, Header: saved.Header, AccessFlags: saved.AccessFlags,
		Name: string(saved.Name), Super: string(saved.Super), SuperHandle: saved.SuperHandle,
		SuperName: string(saved.SuperName), InstanceSize: saved.InstanceSize, VTableSize: saved.VTableSize,
		StaticWords: saved.StaticWords, Body: saved.Body, VTable: saved.VTable,
		Fields: restoreJavaMembers(saved.Fields), Methods: restoreJavaMembers(saved.Methods),
		End: saved.End, Inline: saved.Inline,
	}
	for _, implemented := range saved.Interfaces {
		record.Interfaces = append(record.Interfaces, javaInterface{string(implemented.Name), implemented.Handle, implemented.Slot, implemented.Entry})
	}
	return record
}

func captureJavaRefs(members []javaMemberRef) []javaMemberRefState {
	if members == nil {
		return nil
	}
	saved := make([]javaMemberRefState, 0, len(members))
	for _, member := range members {
		saved = append(saved, javaMemberRefState{[]byte(member.Name), []byte(member.Descriptor)})
	}
	return saved
}

func restoreJavaRefs(saved []javaMemberRefState) []javaMemberRef {
	if saved == nil {
		return nil
	}
	members := make([]javaMemberRef, 0, len(saved))
	for _, member := range saved {
		members = append(members, javaMemberRef{string(member.Name), string(member.Descriptor)})
	}
	return members
}

func captureJavaSlots(table map[string]uint32) []javaSlotState {
	if table == nil {
		return nil
	}
	saved := make([]javaSlotState, 0, len(table))
	for _, key := range slices.Sorted(maps.Keys(table)) {
		saved = append(saved, javaSlotState{[]byte(key), table[key]})
	}
	return saved
}

func restoreJavaSlots(saved []javaSlotState) map[string]uint32 {
	if saved == nil {
		return nil
	}
	table := make(map[string]uint32, len(saved))
	for _, entry := range saved {
		table[string(entry.Key)] = entry.Slot
	}
	return table
}

func captureJavaLink(link *javaLink) *javaLinkState {
	if link == nil {
		return nil
	}
	saved := &javaLinkState{HasLayout: link.layout != nil}
	if surface := link.surface; surface != nil {
		record := &javaSurfaceState{
			Fields: captureJavaRefs(surface.Fields), StaticFields: captureJavaRefs(surface.StaticFields),
			VirtualMethods: captureJavaRefs(surface.VirtualMethods), Methods: captureJavaRefs(surface.Methods),
			StaticMethods: captureJavaRefs(surface.StaticMethods),
			FieldsOut:     surface.FieldsOut, StaticFieldsOut: surface.StaticFieldsOut,
			VirtualMethodsOut: surface.VirtualMethodsOut, MethodsOut: surface.MethodsOut,
			StaticMethodsOut: surface.StaticMethodsOut,
		}
		for _, class := range surface.Classes {
			record.Classes = append(record.Classes, javaAPIClassState{[]byte(class.Name), class.Fields, class.StaticFields, class.VirtualMethods, class.Methods, class.StaticMethods})
		}
		saved.Surface = record
	}
	if link.layout != nil {
		for _, key := range slices.Sorted(maps.Keys(link.layout.classes)) {
			class := link.layout.classes[key]
			if class == nil {
				continue
			}
			saved.Layout = append(saved.Layout, javaLayoutClassState{
				Key: []byte(key), Name: []byte(class.Name), Super: []byte(class.Super),
				InstanceSize: class.InstanceSize, VTableSize: class.VTableSize,
				Virtual: captureJavaSlots(class.Virtual), Fields: captureJavaSlots(class.Fields), Statics: captureJavaSlots(class.Statics),
				InstanceWords: class.InstanceWords, Application: class.Application,
				StaticWords: class.StaticWords, Measured: class.Measured,
			})
		}
	}
	return saved
}

// maxJavaLayoutWords bounds the sizes a laid-out class may state. A class the
// module declares states its own in sixteen bits, and a platform class's are
// counted from tables of a few thousand members, so nothing a session lays out
// reaches it. **The sizes are allocated from**: a platform class reached for
// the first time after a load is given a class object with room for the
// statics its layout names, and a record free to name four thousand million of
// them would have that allocation asked for on the Host's side.
const maxJavaLayoutWords = 1 << 16

// validate checks the link a record brings before anything is built from it.
// It is run at capture as well, so a layout a load would refuse is refused
// where the person can still see it.
func (saved *javaLinkState) validate() error {
	if saved == nil {
		return nil
	}
	if record := saved.Surface; record != nil {
		if len(record.Classes) > maxStateRecords || len(record.StaticMethods) > maxJavaStaticMethods {
			return fmt.Errorf("LGT checkpoint platform class table exceeds its limits")
		}
		for _, table := range [][]javaMemberRefState{record.Fields, record.StaticFields, record.VirtualMethods, record.Methods} {
			if len(table) > maxStateRecords {
				return fmt.Errorf("LGT checkpoint platform member table exceeds its limits")
			}
		}
	}
	if !saved.HasLayout {
		if len(saved.Layout) != 0 {
			return fmt.Errorf("LGT checkpoint has a class layout it says it has not")
		}
		return nil
	}
	if len(saved.Layout) > maxStateRecords {
		return fmt.Errorf("LGT checkpoint class layout exceeds its limits")
	}
	supers := make(map[string]string, len(saved.Layout))
	for index, class := range saved.Layout {
		if index > 0 && bytes.Compare(saved.Layout[index-1].Key, class.Key) >= 0 {
			return fmt.Errorf("LGT checkpoint class layout is out of order")
		}
		if class.InstanceSize > maxJavaLayoutWords || class.VTableSize > maxJavaLayoutWords ||
			class.InstanceWords > maxJavaLayoutWords || class.StaticWords > maxJavaLayoutWords {
			return fmt.Errorf("LGT checkpoint class layout states an impossible size")
		}
		supers[string(class.Key)] = string(class.Super)
	}
	// A layout is walked upwards by name — for the slot an override shares,
	// and for the size a subclass starts from — through the classes it holds
	// and, past those, through the specification's own hierarchy. A chain that
	// comes back to a class it has passed is walked for ever by the first, the
	// next time a class beneath it is prepared; the second goes up by
	// recursion, and would not come back from one at all.
	settled := make(map[string]bool, len(supers))
	for start := range supers {
		var path []string
		walking := map[string]bool{}
		for name := start; name != "" && !settled[name]; {
			if walking[name] {
				return fmt.Errorf("LGT checkpoint class layout has a superclass chain that comes back to itself")
			}
			walking[name] = true
			path = append(path, name)
			if super, laid := supers[name]; laid {
				name = super
			} else {
				name = javaPlatformSuper(name)
			}
		}
		for _, name := range path {
			settled[name] = true
		}
	}
	return nil
}

func restoreJavaLink(saved *javaLinkState) (*javaLink, error) {
	if saved == nil {
		return nil, nil
	}
	if err := saved.validate(); err != nil {
		return nil, err
	}
	link := &javaLink{}
	if record := saved.Surface; record != nil {
		surface := &javaSurface{
			Fields: restoreJavaRefs(record.Fields), StaticFields: restoreJavaRefs(record.StaticFields),
			VirtualMethods: restoreJavaRefs(record.VirtualMethods), Methods: restoreJavaRefs(record.Methods),
			StaticMethods: restoreJavaRefs(record.StaticMethods),
			FieldsOut:     record.FieldsOut, StaticFieldsOut: record.StaticFieldsOut,
			VirtualMethodsOut: record.VirtualMethodsOut, MethodsOut: record.MethodsOut,
			StaticMethodsOut: record.StaticMethodsOut,
		}
		for _, class := range record.Classes {
			surface.Classes = append(surface.Classes, javaAPIClass{string(class.Name), class.Fields, class.StaticFields, class.VirtualMethods, class.Methods, class.StaticMethods})
		}
		link.surface = surface
	}
	if saved.HasLayout {
		layout := &javaLayout{classes: make(map[string]*javaLayoutClass, len(saved.Layout))}
		for _, class := range saved.Layout {
			layout.classes[string(class.Key)] = &javaLayoutClass{
				Name: string(class.Name), Super: string(class.Super),
				InstanceSize: class.InstanceSize, VTableSize: class.VTableSize,
				Virtual: restoreJavaSlots(class.Virtual), Fields: restoreJavaSlots(class.Fields), Statics: restoreJavaSlots(class.Statics),
				InstanceWords: class.InstanceWords, Application: class.Application,
				StaticWords: class.StaticWords, Measured: class.Measured,
			}
		}
		link.layout = layout
	}
	return link, nil
}

// archiveResourceIndex answers, for the first byte of a packaged entry, which
// table and key name it. A stream whose bytes start there and are as long is
// reading the archive's own copy.
type archiveResourceRef struct {
	table  uint8
	key    string
	length int
}

func (archive *Archive) resourceIndex() map[*byte]archiveResourceRef {
	index := map[*byte]archiveResourceRef{}
	for table, files := range archive.resourceTables() {
		for _, key := range slices.Sorted(maps.Keys(files)) {
			data := files[key]
			if len(data) == 0 {
				continue
			}
			if _, seen := index[&data[0]]; !seen {
				index[&data[0]] = archiveResourceRef{uint8(table + 1), key, len(data)}
			}
		}
	}
	return index
}

func (archive *Archive) resourceTables() []map[string][]byte {
	return []map[string][]byte{archive.Resources, archive.Packaged, archive.supplemental}
}

// captureJavaState copies the Java runtime's tables. The caller holds the
// client's lock and has established the boundary.
func (client *Client) captureJavaState(budget *stateBudget) (*javaState, error) {
	runtime := client.javaRun
	saved := &javaState{
		Link:       captureJavaLink(client.javaLink),
		MainThread: runtime.mainThread, KeyChecks: runtime.keyChecks, ThreadStacks: runtime.threadStacks,
		FocusedWidget: runtime.focusedWidget, WidgetGen: runtime.widgetGeneration,
		Card: runtime.card, CardDirty: runtime.cardDirty, ScreenGraphics: runtime.screenGraphics,
		Serial: slices.Clone(runtime.serial), Jlet: runtime.jlet, TryBuffers: slices.Clone(client.javaTryBuffers),
		CollectAt: runtime.collectAt, Condemned: runtime.condemned, SinceCollection: runtime.sinceCollection,
		Collected: runtime.collected, Collections: runtime.collections,
		Images: packPairs(runtime.images), Files: packPairs(runtime.files), Wrapped: packPairs(runtime.wrapped),
		SinkFiles: packPairs(runtime.sinkFiles), StreamFiles: packPairs(runtime.streamFiles),
	}

	// Classes are named from three tables, and one class is in more than one.
	// Each is written once, in an order that does not depend on how a map was
	// walked, and the tables name it by position.
	set := map[*javaRuntimeClass]bool{}
	var classes []*javaRuntimeClass
	add := func(class *javaRuntimeClass) {
		for step := 0; class != nil && !set[class] && step < maxStateRecords; step++ {
			set[class] = true
			classes = append(classes, class)
			class = class.Super
		}
	}
	for _, class := range runtime.byHandle {
		add(class)
	}
	for _, class := range runtime.byObject {
		add(class)
	}
	for _, class := range runtime.byName {
		add(class)
	}
	slices.SortFunc(classes, func(a, b *javaRuntimeClass) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Handle, b.Handle), cmp.Compare(a.Object, b.Object))
	})
	if len(classes) > maxStateRecords {
		return nil, fmt.Errorf("LGT checkpoint has too many Java classes")
	}
	position := make(map[*javaRuntimeClass]int32, len(classes))
	for index, class := range classes {
		position[class] = int32(index)
	}
	for _, class := range classes {
		super := int32(-1)
		if class.Super != nil {
			super = position[class.Super]
		}
		saved.Classes = append(saved.Classes, javaClassState{
			Name: []byte(class.Name), Handle: class.Handle, Record: captureJavaRecord(class.Record),
			Object: class.Object, VTable: class.VTable, Slots: class.Slots, Instance: class.Instance,
			Super: super, ElementBytes: class.ElementBytes, StaticWords: class.StaticWords,
			Measured: class.Measured, Initialized: class.initialized, DataBlock: class.dataBlock,
		})
	}
	for _, key := range slices.Sorted(maps.Keys(runtime.byHandle)) {
		saved.ByHandle = append(saved.ByHandle, javaClassKeyState{key, position[runtime.byHandle[key]]})
	}
	for _, key := range slices.Sorted(maps.Keys(runtime.byObject)) {
		saved.ByObject = append(saved.ByObject, javaClassKeyState{key, position[runtime.byObject[key]]})
	}
	for _, key := range slices.Sorted(maps.Keys(runtime.byName)) {
		saved.ByName = append(saved.ByName, javaClassNameState{[]byte(key), position[runtime.byName[key]]})
	}

	if len(runtime.strings) > maxStatePacked || len(runtime.objects) > maxStatePacked {
		return nil, fmt.Errorf("LGT checkpoint has too many Java strings or objects")
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.strings)) {
		text := runtime.strings[object]
		if err := budget.charge(len(text)); err != nil {
			return nil, err
		}
		saved.Strings = append(saved.Strings, javaTextState{object, []byte(text)})
	}
	for _, name := range slices.Sorted(maps.Keys(runtime.singletons)) {
		saved.Singletons = append(saved.Singletons, javaNamedState{[]byte(name), runtime.singletons[name]})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.random)) {
		state, err := runtime.random[object].captureState()
		if err != nil {
			return nil, err
		}
		if err := budget.charge(len(state.History)); err != nil {
			return nil, err
		}
		saved.Random = append(saved.Random, javaRandomState{object, state})
	}
	packaged := client.archive.resourceIndex()
	for _, object := range slices.Sorted(maps.Keys(runtime.streams)) {
		stream := runtime.streams[object]
		if stream == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing stream")
		}
		record := javaStreamState{Object: object, Name: []byte(stream.Name), Read: stream.Read, Closed: stream.Closed, Markable: stream.Markable, Mark: stream.Mark}
		if stream.Source != nil {
			if stream.Source.Pulling {
				return nil, ErrCheckpointBusy
			}
			source := *stream.Source
			record.Source = &source
		}
		if len(stream.Data) != 0 {
			if reference, found := packaged[&stream.Data[0]]; found && reference.length == len(stream.Data) {
				record.Archive, record.Key = reference.table, []byte(reference.key)
			}
		}
		if record.Archive == 0 {
			if err := budget.charge(len(stream.Data)); err != nil {
				return nil, err
			}
			record.Data = bytes.Clone(stream.Data)
		}
		saved.Streams = append(saved.Streams, record)
	}

	// Threads name their workers by position among the live ones.
	live := make(map[*javaWorker]int32, len(runtime.workers))
	for index, worker := range runtime.workers {
		if worker == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing guest thread")
		}
		live[worker] = int32(index)
		record, err := client.captureJavaWorker(worker)
		if err != nil {
			return nil, err
		}
		saved.Workers = append(saved.Workers, record)
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.threads)) {
		thread := runtime.threads[object]
		if thread == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing thread")
		}
		record := javaThreadState{Object: thread.object, Runnable: thread.runnable, Priority: thread.priority, Worker: -1}
		if thread.worker != nil {
			index, listed := live[thread.worker]
			switch {
			case listed:
				record.Worker = index
			case thread.worker.done:
				record.Finished = true
			default:
				return nil, fmt.Errorf("LGT checkpoint has a running guest thread outside the worker list")
			}
		}
		saved.Threads = append(saved.Threads, record)
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.monitors)) {
		monitor := runtime.monitors[object]
		if monitor == nil || monitor.count == 0 {
			continue
		}
		record := javaMonitorState{Object: object, Owner: -1, Count: monitor.count}
		if !monitor.platform {
			index, listed := live[monitor.owner]
			if !listed {
				return nil, fmt.Errorf("LGT checkpoint has a monitor held by a thread that is not running")
			}
			record.Owner = index
		}
		saved.Monitors = append(saved.Monitors, record)
	}

	for _, object := range slices.Sorted(maps.Keys(runtime.vectors)) {
		items := runtime.vectors[object]
		if err := budget.charge(len(items) * 4); err != nil {
			return nil, err
		}
		saved.Vectors = append(saved.Vectors, javaVectorState{object, packWords(items)})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.calendars)) {
		calendar := runtime.calendars[object]
		record := javaCalendarState{Object: object, Millis: calendar.millis}
		if fields := calendar.pending; fields != nil {
			// The zone is the Host's own, the same one every date call reads
			// in, so it is not a part of the calendar to keep.
			if fields.zone != time.Local {
				return nil, fmt.Errorf("LGT checkpoint has a calendar in a zone this platform does not set")
			}
			record.Pending = &javaCalendarFieldsState{fields.year, fields.month, fields.day, fields.hour, fields.minute, fields.second, fields.millisecond}
		}
		saved.Calendars = append(saved.Calendars, record)
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.sinks)) {
		data := runtime.sinks[object]
		if err := budget.charge(len(data)); err != nil {
			return nil, err
		}
		// A sink that was closed keeps no bytes and one that is open and empty
		// keeps none either; only the second is still a sink.
		if data == nil {
			saved.Sinks = append(saved.Sinks, javaBytesState{object, nil})
			continue
		}
		saved.Sinks = append(saved.Sinks, javaBytesState{object, append([]byte{}, data...)})
	}
	for _, key := range slices.Sorted(maps.Keys(runtime.decodedImages)) {
		saved.DecodedImages = append(saved.DecodedImages, javaKeyedSurfaceState{[]byte(key), runtime.decodedImages[key]})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.widgets)) {
		widget := runtime.widgets[object]
		if widget == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing widget")
		}
		if err := budget.charge(len(widget.text)); err != nil {
			return nil, err
		}
		saved.Widgets = append(saved.Widgets, javaWidgetState{
			Object: object, Text: []byte(widget.text), MaxLength: widget.maxLength, Revision: widget.revision,
			Kind: uint8(widget.kind), Mode: widget.mode, Listener: widget.listener, InputHandler: widget.inputHandler,
			Children: slices.Clone(widget.children), Parent: widget.parent, Shown: widget.shown,
			VisibilityRevision: widget.visibilityRevision, Focused: widget.focused,
		})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.dates)) {
		saved.Dates = append(saved.Dates, javaDateState{object, runtime.dates[object]})
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.databases)) {
		database := runtime.databases[object]
		if database == nil || len(database.deleted) != len(database.records) {
			return nil, fmt.Errorf("LGT checkpoint has a malformed database")
		}
		record := javaDatabaseState{
			Object: object, Name: []byte(database.name), RecordSize: database.recordSize,
			Deleted: slices.Clone(database.deleted), Modified: database.modified, Closed: database.closed,
		}
		for _, data := range database.records {
			if err := budget.charge(len(data)); err != nil {
				return nil, err
			}
			if data == nil {
				record.Records = append(record.Records, nil)
				continue
			}
			record.Records = append(record.Records, append([]byte{}, data...))
		}
		saved.Databases = append(saved.Databases, record)
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.graphics)) {
		state := runtime.graphics[object]
		if state == nil {
			return nil, fmt.Errorf("LGT checkpoint has a missing Graphics")
		}
		saved.Graphics = append(saved.Graphics, javaGraphicsState{
			Object: object, Surface: state.surface, Color: state.color,
			TranslateX: state.translateX, TranslateY: state.translateY,
			ClipX: state.clipX, ClipY: state.clipY, ClipWidth: state.clipWidth, ClipHeight: state.clipHeight,
			ClipSet: state.clipSet, Alpha: state.alpha, Packed: state.packed, Xor: state.xor,
		})
	}
	saved.Objects = make([]byte, 0, len(runtime.objects)*javaObjectStateBytes)
	for _, object := range slices.Sorted(maps.Keys(runtime.objects)) {
		record := runtime.objects[object]
		saved.Objects = binary.LittleEndian.AppendUint32(saved.Objects, object)
		saved.Objects = binary.LittleEndian.AppendUint32(saved.Objects, record.block)
		saved.Objects = binary.LittleEndian.AppendUint32(saved.Objects, record.blockSize)
		condemned := byte(0)
		if record.condemned {
			condemned = 1
		}
		saved.Objects = append(saved.Objects, condemned)
	}
	if err := saved.Link.validate(); err != nil {
		return nil, err
	}
	if err := saved.validate(); err != nil {
		return nil, err
	}
	if err := validateJavaObjects(saved.Objects, client.arena); err != nil {
		return nil, err
	}
	for _, worker := range saved.Workers {
		if err := client.validateJavaWorker(worker, saved.ThreadStacks); err != nil {
			return nil, err
		}
	}
	return saved, nil
}

// validate checks the tables against each other. What depends on the restored
// dispatch tables — whether a parked thread's platform call is one that can be
// finished — is checked once those exist.
func (saved *javaState) validate() error {
	invalid := func(what string) error { return fmt.Errorf("LGT checkpoint Java runtime has %s", what) }
	for _, count := range []int{len(saved.Classes), len(saved.ByHandle), len(saved.ByObject), len(saved.ByName),
		len(saved.Singletons), len(saved.Random), len(saved.Streams), len(saved.Threads), len(saved.Monitors),
		len(saved.Vectors), len(saved.Calendars), len(saved.Sinks), len(saved.DecodedImages), len(saved.Widgets),
		len(saved.Dates), len(saved.Databases), len(saved.Graphics)} {
		if count > maxStateRecords {
			return invalid("a table past its limit")
		}
	}
	if len(saved.Strings) > maxStatePacked || len(saved.Objects)%javaObjectStateBytes != 0 || len(saved.Objects)/javaObjectStateBytes > maxStatePacked {
		return invalid("a string or object table past its limit")
	}
	if saved.ThreadStacks < 0 || saved.ThreadStacks > maxJavaThreads || len(saved.Workers) > saved.ThreadStacks ||
		len(saved.Serial) > maxJavaSerialCalls || len(saved.TryBuffers) > maxJavaTryDepth ||
		saved.Condemned < 0 || saved.SinceCollection < 0 || saved.Collections < 0 {
		return invalid("thread, queue or collector counts outside their limits")
	}
	classIndex := func(index int32) bool { return index >= 0 && int(index) < len(saved.Classes) }
	for _, class := range saved.Classes {
		if class.Super != -1 && !classIndex(class.Super) {
			return invalid("a class with a superclass outside the table")
		}
		if class.Slots > maxJavaVTableSlots || class.Instance > maxJavaInstanceSize {
			return invalid("a class with an impossible size")
		}
	}
	for index, entry := range saved.ByHandle {
		if index > 0 && saved.ByHandle[index-1].Key >= entry.Key || !classIndex(entry.Class) {
			return invalid("an invalid class handle table")
		}
	}
	for index, entry := range saved.ByObject {
		if index > 0 && saved.ByObject[index-1].Key >= entry.Key || !classIndex(entry.Class) {
			return invalid("an invalid class object table")
		}
	}
	for index, entry := range saved.ByName {
		if index > 0 && bytes.Compare(saved.ByName[index-1].Name, entry.Name) >= 0 || !classIndex(entry.Class) {
			return invalid("an invalid class name table")
		}
	}
	if !saved.classChainsEnd() {
		return invalid("a class whose superclass chain comes back to itself")
	}
	for index, entry := range saved.Strings {
		if index > 0 && saved.Strings[index-1].Object >= entry.Object {
			return invalid("strings out of order")
		}
	}
	for index, entry := range saved.Singletons {
		if index > 0 && bytes.Compare(saved.Singletons[index-1].Name, entry.Name) >= 0 {
			return invalid("singletons out of order")
		}
	}
	for index, entry := range saved.Random {
		if index > 0 && saved.Random[index-1].Object >= entry.Object {
			return invalid("generators out of order")
		}
		if err := entry.State.validate(); err != nil {
			return err
		}
	}
	for index, stream := range saved.Streams {
		if index > 0 && saved.Streams[index-1].Object >= stream.Object || stream.Archive > 3 ||
			(stream.Archive != 0) != (stream.Key != nil) || stream.Archive != 0 && stream.Data != nil ||
			stream.Read < 0 || stream.Mark < 0 || stream.Source != nil && stream.Source.Pulling {
			return invalid("an invalid stream")
		}
		// The cursors are checked against the bytes once those are in hand: a
		// resource stream's are the archive's.
	}
	for index, thread := range saved.Threads {
		if index > 0 && saved.Threads[index-1].Object >= thread.Object || thread.Worker < -1 || int(thread.Worker) >= len(saved.Workers) ||
			thread.Finished && thread.Worker != -1 {
			return invalid("an invalid thread")
		}
	}
	held := make([]int, len(saved.Workers))
	lockOwner := make(map[uint32]int32, len(saved.Monitors))
	for index, monitor := range saved.Monitors {
		if index > 0 && saved.Monitors[index-1].Object >= monitor.Object || monitor.Count < 1 || monitor.Count > maxJavaMonitorDepth ||
			monitor.Owner < -1 || int(monitor.Owner) >= len(saved.Workers) {
			return invalid("an invalid monitor")
		}
		if monitor.Owner >= 0 {
			held[monitor.Owner] += monitor.Count
		}
		lockOwner[monitor.Object] = monitor.Owner
	}
	owners := make([]int, len(saved.Workers))
	for _, thread := range saved.Threads {
		if thread.Worker >= 0 {
			owners[thread.Worker]++
		}
	}
	stacks := map[uint32]bool{}
	for index, worker := range saved.Workers {
		// A thread is parked according to how many locks it holds, so the
		// count has to be the locks it is recorded as holding.
		if worker.Monitors != held[index] {
			return invalid("a thread whose lock count disagrees with the monitors it holds")
		}
		// A thread gives every lock back as it ends, so one that has ended
		// holds none: a lock recorded under it would never be released, and
		// everything that wanted it afterwards would wait for nobody.
		if worker.Done && worker.Monitors != 0 {
			return invalid("a finished thread that still holds a lock")
		}
		// A thread parked for a lock — entering it, or taking it back after a
		// wait — is not the thread that holds it. A wait gives the lock up
		// before it parks, and entering a lock one already holds does not park.
		if len(worker.Calls) != 0 {
			parked := worker.Calls[len(worker.Calls)-1]
			remainder := javaRemainder(parked.Remainder)
			if owner, locked := lockOwner[parked.Object]; locked && owner == int32(index) &&
				(remainder == javaRemainderWait || remainder == javaRemainderMonitor) {
				return invalid("a thread waiting for a lock it holds")
			}
		}
		if owners[index] > 1 || !worker.Done && owners[index] == 0 && len(worker.Calls) == 0 {
			return invalid("a guest thread with no Thread object to run")
		}
		if !worker.Done && stacks[worker.StackBase] {
			return invalid("two guest threads on one stack")
		}
		stacks[worker.StackBase] = true
	}
	for index, vector := range saved.Vectors {
		if index > 0 && saved.Vectors[index-1].Object >= vector.Object || len(vector.Items)%4 != 0 {
			return invalid("an invalid vector")
		}
	}
	for index, calendar := range saved.Calendars {
		if index > 0 && saved.Calendars[index-1].Object >= calendar.Object {
			return invalid("calendars out of order")
		}
	}
	for index, sink := range saved.Sinks {
		if index > 0 && saved.Sinks[index-1].Object >= sink.Object {
			return invalid("sinks out of order")
		}
	}
	for index, image := range saved.DecodedImages {
		if index > 0 && bytes.Compare(saved.DecodedImages[index-1].Key, image.Key) >= 0 {
			return invalid("decoded images out of order")
		}
	}
	for index, widget := range saved.Widgets {
		if index > 0 && saved.Widgets[index-1].Object >= widget.Object || len(widget.Children) > maxWidgetChildren ||
			widget.Kind > uint8(javaWidgetTextBox) {
			return invalid("an invalid widget")
		}
	}
	for index, date := range saved.Dates {
		if index > 0 && saved.Dates[index-1].Object >= date.Object {
			return invalid("dates out of order")
		}
	}
	for index, database := range saved.Databases {
		if index > 0 && saved.Databases[index-1].Object >= database.Object || len(database.Records) != len(database.Deleted) ||
			len(database.Records) > maxStatePacked || len(database.Name) > maxDatabaseName*4 {
			return invalid("an invalid database")
		}
	}
	for index, graphics := range saved.Graphics {
		if index > 0 && saved.Graphics[index-1].Object >= graphics.Object {
			return invalid("Graphics out of order")
		}
	}
	for offset := javaObjectStateBytes; offset < len(saved.Objects); offset += javaObjectStateBytes {
		if binary.LittleEndian.Uint32(saved.Objects[offset-javaObjectStateBytes:]) >= binary.LittleEndian.Uint32(saved.Objects[offset:]) {
			return invalid("objects out of order")
		}
	}
	for offset := 0; offset < len(saved.Objects); offset += javaObjectStateBytes {
		if saved.Objects[offset+12] > 1 {
			return invalid("an object with an invalid collector mark")
		}
	}
	return nil
}

// classChainsEnd reports whether every superclass chain reaches a class with
// none. A chain is followed the way a type check follows it: through the
// superclass a class was laid out with and, for a platform class that has none
// yet, through whichever class its specified superclass's name is registered
// under — the link the first check to pass that way adds (javaSuperOf). A chain
// that comes back to itself by either kind of link is one every later type
// check walks for ever, so both are followed here. The caller has checked that
// every index in the tables names a class.
func (saved *javaState) classChainsEnd() bool {
	named := make(map[string]int32, len(saved.ByName))
	for _, entry := range saved.ByName {
		named[string(entry.Name)] = entry.Class
	}
	next := func(index int32) (int32, bool) {
		class := saved.Classes[index]
		if class.Super != -1 || class.Handle != 0 {
			return class.Super, true
		}
		// The specification's hierarchy is a few levels deep and ends at
		// Object. The bound is here so that this does not depend on it.
		name := string(class.Name)
		for range len(javaPlatformSupers) + 2 {
			if name = javaPlatformSuper(name); name == "" {
				return -1, true
			}
			if registered, known := named[name]; known {
				return registered, true
			}
		}
		return -1, false
	}
	const (
		unvisited = iota
		walking
		settled
	)
	state := make([]uint8, len(saved.Classes))
	var path []int32
	for start := range saved.Classes {
		path = path[:0]
		current, ends := int32(start), true
		for current != -1 && state[current] == unvisited {
			state[current] = walking
			path = append(path, current)
			if current, ends = next(current); !ends {
				return false
			}
		}
		if current != -1 && state[current] == walking {
			return false
		}
		for _, index := range path {
			state[index] = settled
		}
	}
	return true
}

// validateJavaObjects checks the collector's table against the allocator its
// objects came out of. An object and the block its fields live in are two
// blocks of the platform's data region, handed out together and given back
// together by the collector alone, so every record names two blocks that are
// outstanding and no block is named twice. **This is what bounds a
// collection.** A cycle reads every block whole, and a record free to state a
// size could ask the first one for a buffer of four gigabytes, or name one
// block from every object and have it read a million times.
func validateJavaObjects(objects []byte, data *arena) error {
	invalid := fmt.Errorf("LGT checkpoint Java runtime tracks an object outside the blocks the allocator handed out")
	named := make(map[uint32]bool, len(objects)/javaObjectStateBytes*2)
	for offset := 0; offset+javaObjectStateBytes <= len(objects); offset += javaObjectStateBytes {
		object := binary.LittleEndian.Uint32(objects[offset:])
		block := binary.LittleEndian.Uint32(objects[offset+4:])
		size := uint64(binary.LittleEndian.Uint32(objects[offset+8:]))
		if data.sizes[object] < javaObjectBytes || named[object] {
			return invalid
		}
		named[object] = true
		if size == 0 {
			continue
		}
		if data.sizes[block] < size || named[block] {
			return invalid
		}
		named[block] = true
	}
	return nil
}

// restoreJavaState rebuilds the Java runtime on a client whose memory and
// surfaces are already restored. It starts nothing: the guest threads it
// builds are parked records until the caller starts their goroutines.
func (client *Client) restoreJavaState(saved *javaState) error {
	if err := saved.validate(); err != nil {
		return err
	}
	if err := validateJavaObjects(saved.Objects, client.arena); err != nil {
		return err
	}
	link, err := restoreJavaLink(saved.Link)
	if err != nil {
		return err
	}
	client.javaLink = link
	runtime := newJavaRuntime()
	runtime.mainThread, runtime.keyChecks, runtime.threadStacks = saved.MainThread, saved.KeyChecks, saved.ThreadStacks
	runtime.focusedWidget, runtime.widgetGeneration = saved.FocusedWidget, saved.WidgetGen
	runtime.card, runtime.cardDirty, runtime.screenGraphics = saved.Card, saved.CardDirty, saved.ScreenGraphics
	runtime.serial, runtime.jlet = slices.Clone(saved.Serial), saved.Jlet
	runtime.collectAt, runtime.condemned, runtime.sinceCollection = saved.CollectAt, saved.Condemned, saved.SinceCollection
	runtime.collected, runtime.collections = saved.Collected, saved.Collections
	client.javaTryBuffers = slices.Clone(saved.TryBuffers)

	classes := make([]*javaRuntimeClass, len(saved.Classes))
	for index, class := range saved.Classes {
		classes[index] = &javaRuntimeClass{
			Name: string(class.Name), Handle: class.Handle, Record: restoreJavaRecord(class.Record),
			Object: class.Object, VTable: class.VTable, Slots: class.Slots, Instance: class.Instance,
			ElementBytes: class.ElementBytes, StaticWords: class.StaticWords, Measured: class.Measured,
			initialized: class.Initialized, dataBlock: class.DataBlock,
		}
	}
	for index, class := range saved.Classes {
		if class.Super >= 0 {
			classes[index].Super = classes[class.Super]
		}
	}
	for _, entry := range saved.ByHandle {
		runtime.byHandle[entry.Key] = classes[entry.Class]
	}
	for _, entry := range saved.ByObject {
		runtime.byObject[entry.Key] = classes[entry.Class]
	}
	for _, entry := range saved.ByName {
		runtime.byName[string(entry.Name)] = classes[entry.Class]
	}
	for _, entry := range saved.Strings {
		runtime.strings[entry.Object] = string(entry.Text)
	}
	for _, entry := range saved.Singletons {
		runtime.singletons[string(entry.Name)] = entry.Object
	}
	for _, entry := range saved.Random {
		if runtime.random[entry.Object], err = restoreGuestRandom(entry.State); err != nil {
			return err
		}
	}
	tables := client.archive.resourceTables()
	for _, record := range saved.Streams {
		stream := &javaStream{Name: string(record.Name), Read: record.Read, Closed: record.Closed, Markable: record.Markable, Mark: record.Mark}
		if record.Archive != 0 {
			data, found := tables[record.Archive-1][string(record.Key)]
			if !found {
				return fmt.Errorf("LGT checkpoint stream names a resource the archive has not")
			}
			stream.Data = data
		} else if record.Data != nil {
			stream.Data = bytes.Clone(record.Data)
		}
		if record.Read > len(stream.Data) || record.Mark > len(stream.Data) {
			return fmt.Errorf("LGT checkpoint stream is read past its bytes")
		}
		if record.Source != nil {
			source := *record.Source
			stream.Source = &source
		}
		runtime.streams[record.Object] = stream
	}
	if runtime.images, err = unpackPairs(saved.Images, "image"); err != nil {
		return err
	}
	if runtime.files, err = unpackPairs(saved.Files, "file"); err != nil {
		return err
	}
	if runtime.wrapped, err = unpackPairs(saved.Wrapped, "stream wrapper"); err != nil {
		return err
	}
	if runtime.sinkFiles, err = unpackPairs(saved.SinkFiles, "file sink"); err != nil {
		return err
	}
	if runtime.streamFiles, err = unpackPairs(saved.StreamFiles, "file stream"); err != nil {
		return err
	}
	for _, record := range saved.Vectors {
		runtime.vectors[record.Object] = unpackWords(record.Items)
	}
	for _, record := range saved.Calendars {
		calendar := javaCalendar{millis: record.Millis}
		if fields := record.Pending; fields != nil {
			calendar.pending = &javaCalendarFields{fields.Year, fields.Month, fields.Day, fields.Hour, fields.Minute, fields.Second, fields.Millisecond, time.Local}
		}
		runtime.calendars[record.Object] = calendar
	}
	for _, record := range saved.Sinks {
		if record.Data == nil {
			runtime.sinks[record.Object] = nil
			continue
		}
		runtime.sinks[record.Object] = append([]byte{}, record.Data...)
	}
	if len(saved.DecodedImages) != 0 {
		runtime.decodedImages = make(map[string]uint32, len(saved.DecodedImages))
		for _, record := range saved.DecodedImages {
			runtime.decodedImages[string(record.Key)] = record.Surface
		}
	}
	if len(saved.Widgets) != 0 {
		runtime.widgets = make(map[uint32]*javaWidget, len(saved.Widgets))
		for _, record := range saved.Widgets {
			runtime.widgets[record.Object] = &javaWidget{
				text: string(record.Text), maxLength: record.MaxLength, revision: record.Revision,
				kind: javaWidgetKind(record.Kind), mode: record.Mode, listener: record.Listener,
				inputHandler: record.InputHandler, children: slices.Clone(record.Children), parent: record.Parent,
				shown: record.Shown, visibilityRevision: record.VisibilityRevision, focused: record.Focused,
			}
		}
	}
	for _, record := range saved.Dates {
		runtime.dates[record.Object] = record.Millis
	}
	for _, record := range saved.Databases {
		database := &javaDatabase{
			name: string(record.Name), recordSize: record.RecordSize, deleted: slices.Clone(record.Deleted),
			modified: record.Modified, closed: record.Closed,
		}
		for _, data := range record.Records {
			if data == nil {
				database.records = append(database.records, nil)
				continue
			}
			database.records = append(database.records, append([]byte{}, data...))
		}
		runtime.databases[record.Object] = database
	}
	for _, record := range saved.Graphics {
		runtime.graphics[record.Object] = &javaGraphics{
			surface: record.Surface, color: record.Color, translateX: record.TranslateX, translateY: record.TranslateY,
			clipX: record.ClipX, clipY: record.ClipY, clipWidth: record.ClipWidth, clipHeight: record.ClipHeight,
			clipSet: record.ClipSet, alpha: record.Alpha, packed: record.Packed, xor: record.Xor,
		}
	}
	for offset := 0; offset < len(saved.Objects); offset += javaObjectStateBytes {
		runtime.objects[binary.LittleEndian.Uint32(saved.Objects[offset:])] = javaObjectRecord{
			block:     binary.LittleEndian.Uint32(saved.Objects[offset+4:]),
			blockSize: binary.LittleEndian.Uint32(saved.Objects[offset+8:]),
			condemned: saved.Objects[offset+12] == 1,
		}
	}
	client.javaRun = runtime

	// The threads last: what a parked one owes is read from the dispatch
	// tables above, and none of them is started until all of them check out.
	for _, record := range saved.Workers {
		if err := client.validateJavaWorker(record, saved.ThreadStacks); err != nil {
			return err
		}
	}
	for _, record := range saved.Workers {
		worker, err := client.restoreJavaWorker(record)
		if err != nil {
			return err
		}
		runtime.workers = append(runtime.workers, worker)
	}
	for _, record := range saved.Threads {
		thread := &javaThread{object: record.Object, runnable: record.Runnable, priority: record.Priority}
		switch {
		case record.Worker >= 0:
			thread.worker = runtime.workers[record.Worker]
		case record.Finished:
			thread.worker = &javaWorker{done: true}
		}
		runtime.threads[record.Object] = thread
	}
	for _, record := range saved.Monitors {
		monitor := &javaMonitor{count: record.Count, platform: record.Owner < 0}
		if record.Owner >= 0 {
			monitor.owner = runtime.workers[record.Owner]
		}
		runtime.monitors[record.Object] = monitor
	}
	return nil
}

// startRestoredJavaWorkers starts the goroutine each restored thread runs on.
// It is the last step of a restore: until it runs, a restore that fails has
// nothing to stop.
func (client *Client) startRestoredJavaWorkers() error {
	runtime := client.javaRun
	if runtime == nil {
		return nil
	}
	owners := make(map[*javaWorker]*javaThread, len(runtime.threads))
	for _, thread := range runtime.threads {
		if thread.worker != nil {
			owners[thread.worker] = thread
		}
	}
	for _, worker := range runtime.workers {
		if worker.done {
			continue
		}
		if worker.restored == nil && owners[worker] == nil {
			return fmt.Errorf("LGT checkpoint guest thread has no Thread object to run")
		}
	}
	for _, worker := range runtime.workers {
		switch {
		case worker.done:
		case worker.restored != nil:
			go client.resumeJavaWorker(worker)
		default:
			go client.runJavaWorker(owners[worker], worker)
		}
	}
	return nil
}
