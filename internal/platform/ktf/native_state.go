package ktf

import (
	"bytes"
	"fmt"
	"image"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

const nativeStateRecordLimit = 1 << 16

// nativeStateVersion is the layout of nativeState. Version 1 carried the bytes
// of every open and written file; a record of that version is refused.
const nativeStateVersion = 2

// nativeStateStorageLimit bounds what the open files of a checkpoint may hold
// between them, where it is taken and where it is loaded, and how far into a
// file a recorded cursor may point.
const nativeStateStorageLimit = 128 << 20

type nativeState struct {
	Version                 uint32
	Core                    armcore.CoreState
	Thread                  armcore.RootThreadState
	Arena                   arenaState
	Blocks                  []nativeBlockState
	Surfaces                []NativeSurface
	Interfaces              []nativeInterfaceState
	Application, FileTable  uint32
	Speed                   float64
	Elapsed, SessionElapsed time.Duration
	Screen                  nativePixelsState
	Presents, Draws, Missed int
	Images                  []nativeImageState
	// Files are the objects the title has open and nothing of what is in
	// them: a load reads each file as the store has it then. What the title
	// wrote this session, what was waiting for the store and the parsed
	// resource files are not state a checkpoint carries at all.
	Files        []nativeFileState
	FileFailure  uint32
	Listeners    []NativeListener
	TimedDue     heapDeadline
	TimedRunning bool
	Frame        *nativeFrameState
	Posted       []nativePostedEvent
	Resumes      []nativeResume
	Colours      []nativeColourState
	Audio        backend.AudioState
	Clip         backend.AudioHandle
	Sounding     bool
}

type nativeBlockState struct {
	Address uint32
	Size    uint64
}
type nativeInterfaceState struct{ Identifier, Address uint32 }
type nativePixelsState struct {
	Width, Height int
	Pixels        []byte
}
type nativeImageState struct {
	Object, Data, Length uint32
	Bytes                []byte
	Frame                nativePixelsState
}
type nativeFileState struct {
	Object    uint32
	Name, Key []byte
	Position  int64
	Writable  bool
	// Truncated says the object's open asked for an empty file, and Length is
	// how many bytes it held when the checkpoint was taken. A load gives such
	// an object at most that much of the file as it is then; see
	// NativePlatform.reopenFiles. Length is zero for any other object.
	Truncated bool
	Length    int64
}
type nativeFrameState struct {
	Interval, Remaining time.Duration
	Function, Context   uint32
}
type nativeColourState struct{ Item, Colour uint32 }

func captureNativePixels(frame *image.RGBA) (nativePixelsState, error) {
	if frame == nil || frame.Rect.Min != (image.Point{}) || frame.Stride != frame.Rect.Dx()*4 {
		return nativePixelsState{}, fmt.Errorf("KTF native checkpoint has an unsupported pixel view")
	}
	saved := nativePixelsState{Width: frame.Rect.Dx(), Height: frame.Rect.Dy(), Pixels: bytes.Clone(frame.Pix)}
	return saved, saved.validate()
}

func (saved nativePixelsState) validate() error {
	if saved.Width <= 0 || saved.Height <= 0 || saved.Width > 4096 || saved.Height > 4096 || len(saved.Pixels) != saved.Width*saved.Height*4 {
		return fmt.Errorf("KTF native checkpoint has invalid pixel dimensions")
	}
	return nil
}

func (saved nativePixelsState) restore() *image.RGBA {
	return &image.RGBA{Pix: bytes.Clone(saved.Pixels), Stride: saved.Width * 4, Rect: image.Rect(0, 0, saved.Width, saved.Height)}
}

// checkpointRefusal names what about a session a checkpoint cannot hold. It
// reads the session and nothing else, so a quick save asks it before it gives
// the store anything.
func (session *NativeSession) checkpointRefusal() error {
	client, platform := session.Client, session.platform
	if client.customBindings || client.tracing || !platform.installed || platform.audio == nil {
		return fmt.Errorf("KTF native checkpoint requires built-in bindings and idle devices")
	}
	if platform.screen == nil {
		return fmt.Errorf("KTF native checkpoint has no screen")
	}
	if held := platform.openFileBytes(); held > nativeStateStorageLimit {
		return fmt.Errorf("KTF native checkpoint: the open files hold %d bytes, more than the %d a checkpoint restores", held, nativeStateStorageLimit)
	}
	return nil
}

func (session *NativeSession) captureNativeState(now time.Time) (nativeState, error) {
	client, platform := session.Client, session.platform
	if err := session.checkpointRefusal(); err != nil {
		return nativeState{}, err
	}
	// A record holds no file bytes, so a write the store has not been given
	// would be in no place at all once this session is gone.
	if len(platform.unsaved) != 0 || len(platform.refused) != 0 {
		return nativeState{}, fmt.Errorf("KTF native checkpoint has save writes the store has not been given")
	}
	saved := nativeState{Version: nativeStateVersion, Application: platform.application, FileTable: platform.fileTable, Speed: platform.Speed(),
		Elapsed: now.Sub(platform.started), SessionElapsed: now.Sub(session.started), FileFailure: platform.fileFailure,
		Listeners: slices.Clone(platform.listeners), TimedDue: captureHeapDeadline(platform.timedDue, now), TimedRunning: platform.timedRunning,
		Posted: slices.Clone(platform.posted), Resumes: slices.Clone(platform.resumes), Clip: platform.clip, Sounding: platform.sounding}
	if !now.Add(-saved.Elapsed).Equal(platform.started) || !now.Add(-saved.SessionElapsed).Equal(session.started) {
		return nativeState{}, fmt.Errorf("KTF native checkpoint clock exceeds its duration range")
	}
	var err error
	if saved.Core, err = client.core.CaptureState(); err != nil {
		return nativeState{}, err
	}
	if saved.Thread, err = client.core.CaptureRootThread(client.thread); err != nil {
		return nativeState{}, err
	}
	if saved.Arena, err = client.arena.captureState(); err != nil {
		return nativeState{}, err
	}
	for _, address := range slices.Sorted(maps.Keys(client.blocks)) {
		saved.Blocks = append(saved.Blocks, nativeBlockState{address, client.blocks[address]})
	}
	for _, surface := range client.surfaces {
		saved.Surfaces = append(saved.Surfaces, surface.name)
	}
	for _, id := range slices.Sorted(maps.Keys(client.interfaces)) {
		saved.Interfaces = append(saved.Interfaces, nativeInterfaceState{id, client.interfaces[id]})
	}
	saved.Screen, err = captureNativePixels(platform.screen.frame)
	if err != nil {
		return nativeState{}, err
	}
	saved.Presents, saved.Draws, saved.Missed = platform.screen.presents, platform.screen.draws, platform.screen.missed
	for _, object := range slices.Sorted(maps.Keys(platform.images)) {
		current := platform.images[object]
		if current == nil {
			return nativeState{}, fmt.Errorf("KTF native checkpoint has a missing image")
		}
		frame, err := captureNativePixels(current.frame)
		if err != nil {
			return nativeState{}, err
		}
		saved.Images = append(saved.Images, nativeImageState{object, current.data, current.length, bytes.Clone(current.bytes), frame})
	}
	for _, object := range slices.Sorted(maps.Keys(platform.files)) {
		file := platform.files[object]
		if file == nil {
			return nativeState{}, fmt.Errorf("KTF native checkpoint has a missing open file")
		}
		record := nativeFileState{Object: object, Name: []byte(file.name), Key: []byte(file.key),
			Position: file.position, Writable: file.writable, Truncated: file.truncated}
		if file.truncated {
			record.Length = int64(len(file.data))
		}
		saved.Files = append(saved.Files, record)
	}
	if platform.frame != nil {
		frame := platform.frame
		saved.Frame = &nativeFrameState{frame.interval, frame.due.Sub(now), frame.function, frame.context}
	}
	for _, item := range slices.Sorted(maps.Keys(platform.colours)) {
		saved.Colours = append(saved.Colours, nativeColourState{item, platform.colours[item]})
	}
	saved.Audio, err = platform.audio.CaptureStateAt(platform.pace.SourceInstant(now))
	if err != nil {
		return nativeState{}, err
	}
	if err := saved.validate(client.core.Memory()); err != nil {
		return nativeState{}, err
	}
	return saved, nil
}

// validate checks Host indexes and views before allocating a replacement. Guest
// pointers are read only through the bounded ARM memory implementation.
func (saved nativeState) validate(memory *armcore.Memory) error {
	invalid := func() error { return fmt.Errorf("KTF native checkpoint has invalid state references or limits") }
	if saved.Version != nativeStateVersion || math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < backend.SpeedFloor || saved.Speed > backend.SpeedCeiling ||
		saved.Elapsed < 0 || saved.SessionElapsed < 0 || saved.SessionElapsed > saved.Elapsed || saved.Presents < 0 || saved.Draws < 0 || saved.Missed < 0 ||
		!saved.TimedDue.Set && saved.TimedDue.Remaining != 0 || saved.TimedDue.Remaining == math.MinInt64 || saved.Thread.StepBudget != 0 || len(saved.Core.Memory.ThreadLocal) != 0 || len(saved.Thread.ThreadLocal) != 0 {
		return invalid()
	}
	for _, length := range []int{len(saved.Blocks), len(saved.Images), len(saved.Files), len(saved.Listeners), len(saved.Colours)} {
		if length > nativeStateRecordLimit {
			return invalid()
		}
	}
	if len(saved.Posted) > maxNativePostedEvents || len(saved.Resumes) > maxNativePostedEvents || len(saved.Interfaces) > maxNativeSurfaces || len(saved.Surfaces) < 3 || len(saved.Surfaces) > maxNativeSurfaces || len(saved.Surfaces) != len(saved.Interfaces)+3 ||
		saved.Surfaces[0] != NativePlatformTable || saved.Surfaces[1] != NativeEntryObject || saved.Surfaces[2] != nativeFileSurface || saved.FileTable != nativeTableBase+2*nativePageSize {
		return invalid()
	}
	if _, err := restoreArenaState(saved.Arena, nativeArenaBase, nativeArenaSize); err != nil {
		return err
	}
	// Allocations and free spans partition the arena's used prefix exactly.
	spans := slices.Clone(saved.Arena.Free)
	for i, block := range saved.Blocks {
		end := uint64(block.Address) + block.Size
		if block.Size == 0 || block.Size > maxPlatformAllocation || block.Address%nativeBlockAlignment != 0 || block.Size%nativeBlockAlignment != 0 || uint64(block.Address) < uint64(nativeArenaBase) || end > saved.Arena.Cursor || i > 0 && saved.Blocks[i-1].Address >= block.Address {
			return invalid()
		}
		spans = append(spans, arenaBlockState{uint64(block.Address), end})
	}
	slices.SortFunc(spans, func(a, b arenaBlockState) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	cursor := uint64(nativeArenaBase)
	for _, span := range spans {
		if span.Start != cursor {
			return invalid()
		}
		cursor = span.End
	}
	if cursor != saved.Arena.Cursor {
		return invalid()
	}
	sp := saved.Thread.Context.Registers[armcore.RegisterSP]
	if sp < ThreadStackBase || uint64(sp) > uint64(ThreadStackBase)+ThreadStackSize || sp&3 != 0 {
		return invalid()
	}
	checkObject := func(address uint32) error {
		if address == 0 || address&3 != 0 {
			return invalid()
		}
		return memory.ValidateRange(address, 4, armcore.PermissionReadWrite)
	}
	if err := checkObject(saved.Application); err != nil {
		return err
	}
	seenSurfaces := map[NativeSurface]bool{}
	for _, name := range saved.Surfaces {
		if seenSurfaces[name] {
			return invalid()
		}
		seenSurfaces[name] = true
	}
	for i, object := range saved.Interfaces {
		if i > 0 && saved.Interfaces[i-1].Identifier >= object.Identifier || !seenSurfaces[nativeInterfaceSurface(object.Identifier)] {
			return invalid()
		}
		if err := checkObject(object.Address); err != nil {
			return err
		}
	}
	if err := saved.Screen.validate(); err != nil {
		return err
	}
	for i, value := range saved.Images {
		if i > 0 && saved.Images[i-1].Object >= value.Object || value.Length == 0 || value.Length > maxNativeBitmap || len(value.Bytes) != int(value.Length) {
			return invalid()
		}
		if err := checkObject(value.Object); err != nil {
			return err
		}
		if err := memory.ValidateRange(value.Data, uint64(value.Length), armcore.PermissionReadWrite); err != nil {
			return err
		}
		if err := value.Frame.validate(); err != nil {
			return err
		}
	}
	// A file is an object the title was handed and a name it was opened by.
	// The name is what a load reads the store with, so it has to be one a
	// title could have opened: a key its name gives and the store accepts.
	for i, file := range saved.Files {
		key := nativeSaveKey(string(file.Key))
		if normalized, err := backend.NormalizeSaveKey(key); err != nil || normalized != key {
			return invalid()
		}
		if i > 0 && saved.Files[i-1].Object >= file.Object || len(file.Name) >= nativeMaxFileName || string(file.Key) != nativeFileKey(string(file.Name)) ||
			file.Position < 0 || file.Position > nativeStateStorageLimit || file.Length < 0 || file.Length > nativeStateStorageLimit || !file.Truncated && file.Length != 0 {
			return invalid()
		}
		if err := checkObject(file.Object); err != nil {
			return err
		}
	}
	function := func(address uint32) error {
		if address == 0 {
			return nil
		}
		width := uint64(4)
		if address&1 != 0 {
			width = 2
		} else if address&3 != 0 {
			return invalid()
		}
		return memory.ValidateRange(address&^1, width, armcore.PermissionExecute)
	}
	for _, listener := range saved.Listeners {
		if listener.Source != nativeInterfaceSound && listener.Source != nativeInterfaceTimed {
			return invalid()
		}
		if err := function(listener.Function); err != nil {
			return err
		}
	}
	for _, resume := range saved.Resumes {
		if resume.Function == 0 {
			return invalid()
		}
		if err := function(resume.Function); err != nil {
			return err
		}
	}
	if saved.Frame != nil {
		if saved.Frame.Interval <= 0 || saved.Frame.Interval > time.Duration(math.MaxUint32)*time.Millisecond || saved.Frame.Remaining == math.MinInt64 || math.Abs(float64(saved.Frame.Remaining))/saved.Speed >= float64(math.MaxInt64) {
			return invalid()
		}
		if err := function(saved.Frame.Function); err != nil {
			return err
		}
	}
	for i, colour := range saved.Colours {
		if i > 0 && saved.Colours[i-1].Item >= colour.Item {
			return invalid()
		}
	}
	// Native playback only starts one-shot clips at nonnegative guest times.
	// Refuse impossible playback state before adoption can displace the
	// running session or leave the next tick replaying an excessive number of
	// repeat cycles.
	for _, sound := range saved.Audio.Sounds {
		if sound.Repeat || sound.StartedAt < 0 {
			return fmt.Errorf("KTF native checkpoint has unsupported audio playback state")
		}
	}
	if saved.Clip != 0 && !slices.ContainsFunc(saved.Audio.Sounds, func(sound backend.AudioSoundState) bool { return sound.Handle == saved.Clip }) {
		return invalid()
	}
	return nil
}

func restoreNativeState(archive *NativeArchive, saved nativeState, options NativeSessionOptions) (*NativeSession, error) {
	if err := saved.Screen.validate(); err != nil {
		return nil, err
	}
	steps := options.MaxSteps
	if steps == 0 {
		steps = nativeSessionDefaultMaxSteps
	}
	client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: steps})
	if err != nil {
		return nil, err
	}
	platform := NewNativePlatform(client, archive, options.Clock)
	platform.SetScreen(saved.Screen.Width, saved.Screen.Height)
	if err := platform.Install(); err != nil {
		return nil, err
	}
	expected, err := client.core.CaptureState()
	if err != nil {
		return nil, err
	}
	if !slices.Equal(saved.Core.Memory.Mappings, expected.Memory.Mappings) {
		return nil, fmt.Errorf("KTF native checkpoint changes memory mapping policy")
	}
	core, err := armcore.NewCoreFromState(saved.Core, armcore.CoreOptions{MaxSteps: steps})
	if err != nil {
		return nil, err
	}
	if err := saved.validate(core.Memory()); err != nil {
		return nil, err
	}
	thread, err := core.RestoreRootThread(saved.Thread, 0)
	if err != nil {
		return nil, err
	}
	arena, err := restoreArenaState(saved.Arena, nativeArenaBase, nativeArenaSize)
	if err != nil {
		return nil, err
	}
	client.core, client.thread, client.arena = core, thread, arena
	client.blocks = make(map[uint32]uint64, len(saved.Blocks))
	for _, block := range saved.Blocks {
		client.blocks[block.Address] = block.Size
	}
	client.surfaces = nil
	for index, name := range saved.Surfaces {
		client.surfaces = append(client.surfaces, nativeSurfaceTable{name, nativeTableBase + uint32(index)*nativePageSize, nativeStubBase + uint32(index)*nativePageSize})
	}
	for _, object := range saved.Interfaces {
		client.interfaces[object.Identifier] = object.Address
		for _, offset := range []uint32{nativeObjectAddRef, nativeObjectRelease} {
			key := nativeSlotKey{nativeInterfaceSurface(object.Identifier), offset / 4}
			if _, exists := client.served[key]; !exists {
				client.served[key] = nativeAnswerOne
			}
		}
	}
	platform.SetSpeed(saved.Speed)
	now := platform.clock.Now()
	platform.started = now.Add(-saved.Elapsed)
	platform.application, platform.fileTable = saved.Application, saved.FileTable
	platform.screen = &nativeScreen{frame: saved.Screen.restore(), presents: saved.Presents, draws: saved.Draws, missed: saved.Missed}
	for _, record := range saved.Images {
		platform.images[record.Object] = &nativeImage{data: record.Data, length: record.Length, bytes: bytes.Clone(record.Bytes), frame: record.Frame.restore()}
	}
	// An open file comes back without its bytes: Commit reads them from the
	// store the session will run over. The table of what the title wrote, the
	// marks for the store and the parsed resource files start empty, as they
	// do in a session that has just started.
	for _, record := range saved.Files {
		platform.files[record.Object] = &nativeOpenFile{name: string(record.Name), key: string(record.Key),
			position: record.Position, writable: record.Writable, truncated: record.Truncated}
	}
	platform.fileFailure = saved.FileFailure
	platform.listeners, platform.posted, platform.resumes = slices.Clone(saved.Listeners), slices.Clone(saved.Posted), slices.Clone(saved.Resumes)
	platform.timedDue, platform.timedRunning = saved.TimedDue.restore(now), saved.TimedRunning
	if frame := saved.Frame; frame != nil {
		platform.frame = &nativeSchedule{frame.Interval, frame.Function, frame.Context, now.Add(frame.Remaining)}
	}
	platform.colours = map[uint32]uint32{}
	for _, colour := range saved.Colours {
		platform.colours[colour.Item] = colour.Colour
	}
	platform.audio, err = backend.NewAudioFromStateWithClock(saved.Audio, nil, platform.source.Now)
	if err != nil {
		return nil, err
	}
	platform.audio.SetLogger(options.Logger)
	if err := platform.audio.RebasePlaybackClock(saved.Elapsed, saved.Speed); err != nil {
		return nil, err
	}
	platform.clip, platform.sounding = saved.Clip, saved.Sounding
	return &NativeSession{Archive: archive, Client: client, platform: platform, clock: platform.clock, source: platform.source,
		started: now.Add(-saved.SessionElapsed), logger: options.Logger, options: options}, nil
}
