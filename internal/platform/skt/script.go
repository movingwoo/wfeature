package skt

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

type ScriptOptions struct {
	Framebuffer backend.Framebuffer
	SaveStore   backend.SaveStore
	AudioSink   backend.AudioSink
	Logger      *slog.Logger
	Speed       float64
	// UserID is the opaque byte string stored in the SGS script metadata
	// record. Release Hosts currently leave it empty, matching the original
	// PC Host's unconfigured default.
	UserID []byte

	ExternalLaunch func(string)
}

type scriptTimer struct {
	period, due    time.Duration
	repeat, active bool
}

const scriptStandaloneRuntimeMode byte = 2
const scriptStandaloneRole byte = 1
const scriptUserIDMaxBytes = 9

// ScriptSession drives SGS events on the host's session goroutine. Guest
// timers use a virtual clock and cannot create unbounded goroutines.
type ScriptSession struct {
	vibrator        backend.Vibrator
	overlayPolicy   byte
	audio           *backend.Audio
	sound           backend.AudioHandle
	vm              *sgsvm.VM
	graphics        *scriptGraphics
	options         ScriptOptions
	timers          [3]scriptTimer
	clock           time.Duration
	last            time.Time
	paused          bool
	closed          bool
	runtimeMode     byte
	requestID       int16
	requestStatus   int16
	role            byte
	userID          []byte
	random          *rand.Rand
	textInput       *scriptTextInput
	textInputSerial uint64
}

func StartScript(ctx context.Context, archive *Archive, options ScriptOptions) (*ScriptSession, error) {
	if archive == nil || archive.Script == nil {
		return nil, fmt.Errorf("SKT archive contains no SGS program")
	}
	if len(options.UserID) > scriptUserIDMaxBytes {
		return nil, fmt.Errorf("SGS UserID exceeds its %d-byte metadata field", scriptUserIDMaxBytes)
	}
	if bytes.IndexByte(options.UserID, 0) >= 0 {
		return nil, fmt.Errorf("SGS UserID contains NUL")
	}
	options.UserID = bytes.Clone(options.UserID)
	graphics, err := newScriptGraphics(options.Framebuffer)
	if err != nil {
		return nil, err
	}
	s := &ScriptSession{
		graphics: graphics, options: options, last: time.Now(),
		runtimeMode:   scriptStandaloneRuntimeMode,
		requestID:     -1,
		requestStatus: 1,
		role:          scriptStandaloneRole,
		userID:        options.UserID,
		random:        rand.New(rand.NewPCG(1, 2)),
	}
	s.audio = backend.NewAudio(options.AudioSink)
	s.vibrator.SetClock(func() time.Time { return time.Unix(0, int64(s.clock)) })
	s.vm = sgsvm.New(archive.Script, s)
	if len(s.vm.Variables) < 16 {
		return nil, fmt.Errorf("SGS system variable bank is missing")
	}
	for i := 0; i < 16; i++ {
		if len(s.vm.Variables[i].Values) == 0 {
			return nil, fmt.Errorf("SGS system variable %d is empty", i)
		}
	}
	width, height := options.Framebuffer.Dimensions()
	for variable, value := range map[int]int16{0: int16(s.runtimeMode), 1: int16(width), 2: int16(height), 3: 0, 4: 0, 5: 0, 6: 0, 9: 0, 10: 0, 15: 0x3009} {
		if variable < len(s.vm.Variables) && len(s.vm.Variables[variable].Values) > 0 {
			s.vm.Set(variable, 0, value)
		}
	}
	if err := s.event(ctx, 0, int16(s.runtimeMode)); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *ScriptSession) event(ctx context.Context, entry int, value int16) error {
	if len(s.vm.Variables) > 0 && len(s.vm.Variables[0].Values) > 0 {
		s.vm.Set(0, 0, value)
	}
	err := s.vm.Run(ctx, s.vm.Program.Entries[entry])
	if err != nil && s.options.Logger != nil {
		s.options.Logger.Debug("SGS event failed", "event", entry, "parameter", value, "error", err)
	}
	return err
}
func (s *ScriptSession) Vibration() backend.Vibration { return s.vibrator.State() }
func (s *ScriptSession) GuestElapsed() time.Duration  { return s.clock }

func (s *ScriptSession) Exited() bool { return s.vm.Exited() }

func (s *ScriptSession) Tick(ctx context.Context) (time.Duration, error) {
	now := time.Now()
	elapsed := now.Sub(s.last)
	s.last = now
	if elapsed > 250*time.Millisecond {
		elapsed = 250 * time.Millisecond
	}
	if !s.paused && !s.closed {
		return s.Advance(ctx, elapsed)
	}
	return 16 * time.Millisecond, nil
}
func (s *ScriptSession) Advance(ctx context.Context, elapsed time.Duration) (time.Duration, error) {
	if elapsed < 0 || elapsed > 250*time.Millisecond {
		return 0, fmt.Errorf("SGS elapsed tick outside 0..250ms")
	}
	if s.closed || s.paused || s.Exited() || s.textInput != nil {
		return 16 * time.Millisecond, nil
	}
	speed := backend.ClampSpeed(s.options.Speed)
	if math.IsNaN(speed) {
		speed = 1
	}
	if speed <= 0 {
		speed = 1
	}
	defer func() { s.audio.Advance(s.clock) }()
	s.clock += time.Duration(float64(elapsed) * speed)
	for i := range s.timers {
		if s.Exited() {
			break
		}
		timer := &s.timers[i]
		if !timer.active || timer.due > s.clock {
			continue
		}
		if timer.repeat {
			timer.due = s.clock + timer.period
		} else {
			timer.active = false
		}
		if i == 0 && len(s.vm.Variables) > 6 {
			tick := (s.vm.Value(3, 0) + 1) % 5040
			s.vm.Set(3, 0, tick)
			s.vm.Set(4, 0, tick%2)
			s.vm.Set(5, 0, tick%3)
			s.vm.Set(6, 0, tick%6)
		}
		if err := s.event(ctx, 2, int16(i)); err != nil {
			return 0, err
		}
	}
	return time.Duration(float64(16*time.Millisecond) / speed), nil
}
func (s *ScriptSession) SetSpeed(speed float64) { s.options.Speed = speed; s.last = time.Now() }
func (s *ScriptSession) Pause()                 { s.paused = true }
func (s *ScriptSession) Resume()                { s.paused = false; s.last = time.Now() }
func (s *ScriptSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	defer s.audio.StopAll()
	defer s.vibrator.Stop()
	return s.event(context.Background(), 1, 0)
}
func (s *ScriptSession) SendKey(ctx context.Context, action string, code int32) error {
	if s.closed || s.paused || s.Exited() || s.textInput != nil {
		return nil
	}
	if action != "press" {
		return nil
	}
	var key int16
	switch {
	case code >= '1' && code <= '9':
		key = int16(code - '0')
	case code == '0':
		key = 10
	case code == '*':
		key = 11
	case code == '#':
		key = 12
	case code == -1 || code == KeyCodeUp:
		key = 18
	case code == -2 || code == KeyCodeDown:
		key = 19
	case code == -3 || code == KeyCodeLeft:
		key = 16
	case code == -4 || code == KeyCodeRight:
		key = 17
	case code == -5 || code == KeyCodeFire:
		key = 20
	case code == -6 || code == 129: // Host soft key or handset menu key.
		key = 14
	case code == -7 || code == KeyCodeCall:
		key = 15
	case code == -8 || code == KeyCodeClear:
		key = 13
	default:
		return nil
	}
	return s.event(ctx, 3, key)
}

func (s *ScriptSession) Call(op byte, vm *sgsvm.VM) error {
	if n, ok := scriptArguments[op]; ok {
		if err := vm.Require(n); err != nil {
			return err
		}
	}
	if handled, err := s.graphics.call(op, vm); handled || err != nil {
		return err
	}
	switch op {
	case 0xe3, 0xe4, 0xe5, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef, 0xf2:
		// These dispatch entries share the original empty reserved handler.
	case 0xdb:
		vm.Args(2)
	case 0xdc, 0xdd:
		vm.Pop()
	case 0xe0:
		vm.Args(6)
	case 0xe8:
		return scriptImageInfoCall(vm)
	case 0xe1:
		value := vm.Pop()
		result := int16(-1)
		if value == 1 {
			result = 0
		}
		vm.Push(result)
	case 0x51:
		address := vm.Pop()
		w, h := s.graphics.width, s.graphics.height
		class := int16(1)
		if w >= 80 && h >= 120 {
			class = 2
		}
		if w >= 128 && h >= 128 {
			class = 4
		}
		if w >= 176 && h >= 176 {
			class = 8
		}
		return scriptDeviceResult(vm, address, []int16{class, 4, 256, 1})
	case 0x52:
		resource := vm.Resource(int(vm.Pop()))
		if err := vm.Error(); err != nil {
			return err
		}
		ok, err := resizeScriptResource(vm, resource, 1)
		if err != nil || !ok {
			return err
		}
		resource.Data[0] = 0
	case 0x53:
		resource := vm.Resource(int(vm.Pop()))
		if err := vm.Error(); err != nil {
			return err
		}
		ok, err := resizeScriptResource(vm, resource, len(s.userID)+1)
		if err != nil {
			return err
		}
		if ok {
			copy(resource.Data, s.userID)
			resource.Data[len(s.userID)] = 0
		}
		vm.Push(int16(s.role))
	case 0x54:
		address := vm.Pop()
		return scriptDeviceResult(vm, address, []int16{0, 0, 0, 0, 0})
	case 0x79:
		vm.Push(int16(len(vm.Resource(int(vm.Pop())).Data)))
	case 0x7a, 0x7c, 0x7d, 0x7e, 0x7f, 0x80, 0x83, 0x84:
		return scriptResourceCall(op, vm)
	case 0x7b:
		a := vm.Args(2)
		resource := vm.Resource(int(a[0]))
		if vm.Error() != nil {
			return vm.Error()
		}
		size := int(a[1])
		if size < 0 {
			return fmt.Errorf("negative resource allocation %d", size)
		}
		if err := vm.ChargeWork(size/16 + 1); err != nil {
			return err
		}
		ok, err := resizeScriptResource(vm, resource, size)
		if err != nil || !ok {
			return err
		}
		clear(resource.Data[:size])
	case 0x8a, 0x8b, 0x8c, 0x8d, 0x8e:
		return formatScriptResource(vm, int(op-0x89))
	case 0x8f:
		return s.beginTextInput(vm)
	case 0xc5:
		// The script runtime ends this invocation and hands action 2 to the
		// native Host. The inspected PC Host does not dispatch that action,
		// so it is not evidence that the game or shared session has exited.
		vm.Yield()
	case 0x90:
		resource := vm.Resource(int(vm.Pop()))
		data := resource.Data
		if len(data) < 2 {
			return fmt.Errorf("SGS audio wrapper is truncated")
		}
		if s.sound != 0 {
			_ = s.audio.Close(s.sound)
			s.sound = 0
		}
		handle, err := s.audio.Load(data[2:])
		if err != nil {
			return err
		}
		s.sound = handle
		return s.audio.Play(handle, s.clock, false)
	case 0x91:
		s.audio.StopAll()
	case 0x92:
		vm.Resource(int(vm.Pop()))
	case 0x93:
	case 0x94:
		s.vibrator.Vibrate(100, max(0, int(vm.Pop()))*1000)
	case 0x95:
		s.vibrator.Stop()
	case 0x96, 0x97:
		vm.Pop()
	case 0x98, 0x99:
		a := vm.Args(2)
		count := int(a[1])
		if count < 0 || count > 32767 {
			return fmt.Errorf("SGS save size %d words is invalid", count)
		}
		if int(uint16(a[0])&0x3fff)+count > 0x4000 {
			return fmt.Errorf("SGS save crosses a variable bank")
		}
		if err := vm.ChargeWork(count * (1 + len(vm.Variables)/16)); err != nil {
			return err
		}
		for i := 0; i < count; i++ {
			vm.AddressRead(a[0] + int16(i))
		}
		if vm.Error() != nil {
			return vm.Error()
		}
		data := make([]byte, max(64, count*2))
		if op == 0x98 {
			if s.options.SaveStore != nil {
				if saved, ok := s.options.SaveStore.LoadSave("nv/data"); ok {
					copy(data, saved)
				}
			}
			for i := 0; i < count; i++ {
				vm.AddressWrite(a[0]+int16(i), int16(binary.LittleEndian.Uint16(data[i*2:])))
			}
		} else {
			for i := 0; i < count; i++ {
				binary.LittleEndian.PutUint16(data[i*2:], uint16(vm.AddressRead(a[0]+int16(i))))
			}
			if vm.Error() != nil {
				return vm.Error()
			}
			if s.options.SaveStore != nil {
				return s.options.SaveStore.StoreSave("nv/data", data)
			}
		}
	case 0x9a, 0x9b, 0x9c:
		a := vm.Args(2)
		if a[0] >= 10 {
			period := time.Duration(a[0]) * time.Millisecond
			s.timers[op-0x9a] = scriptTimer{period: period, due: s.clock + period, repeat: a[1] != 0, active: true}
		}
	case 0x9d, 0x9e, 0x9f:
		s.timers[op-0x9d].active = false
	case 0x81, 0x82:
		n := 2
		if op == 0x82 {
			n = 3
		}
		a := vm.Args(n)
		if op == 0x81 {
			vm.Push(int16(int8(vm.ResourceByte(int(a[0]), int(a[1])))))
		} else {
			r := vm.Resource(int(a[0]))
			offset := int(a[1])
			if offset < 0 || offset >= len(r.Data) {
				return fmt.Errorf("SGS resource byte offset out of bounds")
			}
			if vm.Error() == nil {
				r.Data[offset] = byte(a[2])
			}
		}
	case 0xc4:
		return s.beginExternalLaunch(vm)
	case 0x89:
		return formatScriptStringResource(vm)
	case 0xf0, 0xf1:
		return s.traceCall(op, vm)
	case 0x87, 0x88:
		return scriptCopyCall(op, vm)
	case 0x85, 0x86:
		return scriptMemoryCall(op, vm)
	case 0xa4, 0xab, 0xac, 0xad, 0xae, 0xb0, 0xb1, 0xb2, 0xb3, 0xb7:
		return scriptMathCall(op, vm)
	case 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa:
		return scriptTrigCall(op, vm)
	case 0xaf:
		a := vm.Args(2)
		vm.Push(min(a[0], a[1]))
	case 0xbe:
		if s.runtimeMode == 3 {
			return fmt.Errorf("unsupported SGS service 0xbe in runtime mode 3")
		}
		vm.Args(2)
		vm.Push(0)
	case 0xbf:
		requestID := vm.Pop()
		result := int16(4)
		if requestID == s.requestID {
			result = s.requestStatus
		}
		vm.Push(result)
	case 0xa3:
		value := vm.Pop()
		if value < 0 {
			value = -value
		}
		vm.Push(value)
	case 0xba:
		s.overlayPolicy = byte(vm.Pop())
	case 0xd2:
		vm.Push(int16(s.runtimeMode))
	case 0xb8, 0xb9:
		return scriptCalendarCall(op, vm, time.Now())
	case 0xa0:
		seed := uint64(uint16(vm.Pop()))
		s.random = rand.New(rand.NewPCG(seed, seed+1))
	case 0xa1:
		a := vm.Args(2)
		lo, hi := int(a[0]), int(a[1])
		if lo > hi {
			lo, hi = hi, lo
		}
		result := lo
		if lo != hi {
			result += s.random.IntN(hi - lo)
		}
		vm.Push(int16(result))
	case 0xa2:
		p := int(vm.Pop())
		if s.random.IntN(100) < p {
			vm.Push(1)
		} else {
			vm.Push(0)
		}
	default:
		return fmt.Errorf("unsupported SGS service 0x%02x", op)
	}
	return nil
}

var scriptArguments = map[byte]int{
	0x89: 3, 0x8f: 2, 0xf0: 4, 0xf1: 2,
	0xcf: 4, 0xd0: 4,
	0xcb: 5, 0xcc: 5,
	0xce: 4,
	0xcd: 4,
	0x87: 4, 0x88: 4,
	0x8e: 7,
	0xc8: 4, 0xca: 2,
	0xa5: 1, 0xa6: 1, 0xa7: 1, 0xa8: 1, 0xa9: 1, 0xaa: 1,
	0x73: 0, 0x74: 5, 0x75: 1,
	0xd2: 0, 0xdb: 2, 0xdc: 1, 0xdd: 1, 0xe0: 6, 0xe1: 1, 0xe8: 7,
	0xb8: 1, 0xa4: 1, 0xab: 2, 0xac: 3, 0xb0: 3, 0xb1: 2, 0xb3: 3,
	0x51: 1, 0x52: 1, 0x53: 1, 0x54: 1, 0x55: 0, 0x56: 0, 0x57: 1, 0x58: 3, 0x59: 1,
	0x5a: 0, 0x5b: 0, 0x5c: 0, 0x5d: 0, 0x5e: 1, 0x5f: 4, 0x60: 3, 0x61: 3, 0x62: 4, 0x63: 4, 0x64: 4, 0x65: 4,
	0x66: 4, 0x67: 1, 0x68: 2, 0x69: 1, 0x6a: 3, 0x6b: 3, 0x6c: 3, 0x6d: 3, 0x6e: 2, 0x6f: 3, 0x70: 4, 0x71: 4, 0x72: 5,
	0x76: 0, 0x77: 0, 0x78: 0, 0x79: 1, 0x7a: 2, 0x7b: 2, 0x7c: 1, 0x7d: 2, 0x7e: 4, 0x7f: 2, 0x80: 2, 0x81: 2, 0x82: 3, 0x83: 1, 0x84: 2, 0x85: 2, 0x86: 3, 0x8a: 3, 0x8b: 4, 0x8c: 5, 0x8d: 6,
	0x90: 1, 0x91: 0, 0x92: 1, 0x93: 0, 0x94: 1, 0x95: 0, 0x96: 1, 0x97: 1, 0x98: 2, 0x99: 2,
	0x9a: 2, 0x9b: 2, 0x9c: 2, 0x9d: 0, 0x9e: 0, 0x9f: 0, 0xa0: 1, 0xa1: 2, 0xa2: 1, 0xa3: 1, 0xad: 2, 0xae: 3, 0xb2: 2, 0xb7: 3, 0xaf: 2, 0xb9: 1, 0xba: 1, 0xbe: 2, 0xbf: 1, 0xc4: 1, 0xc9: 0,
}
