package skt

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/movingwoo/wfeature/internal/sgsvm"
	"github.com/movingwoo/wfeature/internal/wipic"
)

func (s *ScriptSession) startWide(ctx context.Context) (*ScriptSession, error) {
	screen, err := newWideScreen(s.options.Framebuffer)
	if err != nil {
		return nil, err
	}
	s.screen = screen
	s.wide = sgsvm.NewWide(s.archive.Script, s.wideService)
	if len(s.wide.Variables) < 10 {
		return nil, fmt.Errorf("SGS: missing version 4 system variables")
	}
	for i := 0; i < 10; i++ {
		if len(s.wide.Variables[i]) == 0 {
			return nil, fmt.Errorf("SGS: empty system symbol %d", i)
		}
	}
	w, h := s.options.Framebuffer.Dimensions()
	s.wide.Variables[4][0], s.wide.Variables[5][0] = int32(w), int32(h)
	if err := s.event(ctx, 0, 0); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ScriptSession) sendWideKey(ctx context.Context, action string, code int32) error {
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
	case code == -8 || code == KeyCodeClear:
		key = 13
	case code == KeyCodeCall:
		key = 14
	case code == KeyCodeLeft || code == -3:
		key = 16
	case code == KeyCodeRight || code == -4:
		key = 17
	case code == KeyCodeUp || code == -1:
		key = 18
	case code == KeyCodeDown || code == -2:
		key = 19
	case code == KeyCodeFire || code == -5:
		key = 20
	case code == KeyCodeMenu || code == KeyCodeSoft1:
		key = 21
	case code == KeyCodeSoft2:
		key = 22
	default:
		return nil
	}
	if s.wideEditor != nil {
		if action == "press" && (key == 13 || key == 20 || key == 21 || key == 22) {
			return s.finishWideEditor(ctx)
		}
		return nil
	}
	switch action {
	case "press":
		s.wideHeld = key
		s.wideKeyDue = s.clock + 500*time.Millisecond
	case "release":
		if s.wideHeld == key {
			s.wideHeld = 0
		}
		key = 255
	default:
		return nil
	}
	return s.event(ctx, 3, key)
}

func (s *ScriptSession) wideService(id uint16, vm *sgsvm.WideVM) error {
	if done, err := s.screen.call(id, vm); done || err != nil {
		return err
	}
	if done, err := s.wideStorage(id, vm); done || err != nil {
		return err
	}
	if done, err := wideStrings(id, vm); done || err != nil {
		return err
	}
	if done, err := s.wideMath(id, vm); done || err != nil {
		return err
	}
	switch id {
	case 1, 2:
		a, err := vm.Args(1)
		if err != nil {
			return err
		}
		value := []byte(s.archive.ScriptUserID)
		if id == 1 {
			value = []byte(wipic.SubscriberNumber())
		}
		if err := putWideString(vm, a[0], value); err != nil {
			return err
		}
		if id == 2 {
			return vm.Push(0)
		}
	case 4, 5:
		a, err := vm.Args(1)
		if err != nil {
			return err
		}
		dst, err := vm.Address(a[0], 4)
		if err != nil {
			return err
		}
		now := time.Now()
		if id == 4 {
			dst[0], dst[1], dst[2], dst[3] = int32(now.Year()), int32(now.Month()), int32(now.Day()), int32(now.Weekday())
		} else {
			dst[0], dst[1], dst[2], dst[3] = int32(now.Hour()), int32(now.Minute()), int32(now.Second()), int32(now.Nanosecond()/1e6)
		}
	case 0x6f, 0x70, 0x72, 0x74, 0x78:
		// This handset has no packet-data bearer. Report an ordinary socket
		// failure to the guest so it can offer its offline path.
		n := 1
		if id == 0x72 {
			n = 3
		}
		if id == 0x74 || id == 0x78 {
			n = 4
		}
		if _, err := vm.Args(n); err != nil {
			return err
		}
		return vm.Push(-1)
	case 0x8e:
		a, err := vm.Args(1)
		if err != nil {
			return err
		}
		s.vibrator.Vibrate(100, int(min(int64(max(0, a[0]))*1000, 60000)))
	case 0x8f:
		s.vibrator.Stop()
	case 0x90, 0x91:
		// A handset key tone and backlight have no separate browser output.
		_, err := vm.Args(1)
		return err
	case 0x95, 0x96, 0x97:
		a, err := vm.Args(2)
		if err != nil {
			return err
		}
		if a[0] < 0 || a[1] < 0 || a[1] > 1 {
			return fmt.Errorf("SGS: invalid timer period %d or repeat %d", a[0], a[1])
		}
		period := time.Duration(a[0]) * time.Millisecond
		s.timers[id-0x95] = scriptTimer{period: period, due: s.clock + period, repeat: a[1] != 0, active: period > 0}
	case 0x98, 0x99, 0x9a:
		s.timers[id-0x98].active = false
	case 0xb4:
		return s.openWideEditor(vm)
	case 0xe1:
		vm.Exited = true
	case 7:
		if _, err := vm.Args(1); err != nil {
			return err
		}
		vm.Exited = true
	default:
		return fmt.Errorf("unsupported SGS v4 service %#x", id)
	}
	return nil
}

func (s *ScriptSession) wideMath(id uint16, vm *sgsvm.WideVM) (bool, error) {
	var n int
	switch id {
	case 0xc8, 0xcb, 0xcd, 0xce, 0xcf, 0xd2, 0xd3:
		n = 1
	case 0xc9, 0xd6, 0xd8:
		n = 2
	case 0xdd:
		n = 4
	default:
		return false, nil
	}
	a, err := vm.Args(n)
	if err != nil {
		return true, err
	}
	var result int32
	switch id {
	case 0xc8:
		seed := uint64(uint32(a[0]))
		s.randomSource.Seed(seed, seed+1)
		return true, nil
	case 0xc9:
		lo, hi := min(a[0], a[1]), max(a[0], a[1])
		result = lo
		if hi != lo {
			span := uint64(int64(hi) - int64(lo))
			result = int32(int64(lo) + int64(s.random.Uint64N(span)))
		}
	case 0xcb:
		result = a[0]
		if result < 0 {
			result = -result
		}
	case 0xcd, 0xce, 0xcf:
		result = int32(scriptTrig(byte(0xa5+id-0xcd), int16(a[0]%360)))
	case 0xd2:
		result = int32(math.Round(math.Atan(float64(a[0])/100) * 180 / math.Pi))
	case 0xd3:
		result = int32(math.Sqrt(float64(max(0, a[0]))))
	case 0xd6:
		result = max(a[0], a[1])
	case 0xd8:
		result = min(a[0], a[1])
	case 0xdd:
		values, err := vm.Address(a[0], int(a[2]))
		if err != nil {
			return true, err
		}
		if err := vm.Charge(len(values)); err != nil {
			return true, err
		}
		if a[3] < 0 || a[3] > 11 || ((a[3] == 4 || a[3] == 5) && a[1] == 0) {
			return true, fmt.Errorf("SGS: invalid array operation %d with operand %d", a[3], a[1])
		}
		for i, v := range values {
			switch a[3] {
			case 0:
				v = a[1]
			case 1:
				v += a[1]
			case 2:
				v -= a[1]
			case 3:
				v *= a[1]
			case 4:
				v /= a[1]
			case 5:
				v %= a[1]
			case 6:
				v &= a[1]
			case 7:
				v |= a[1]
			case 8:
				v = ^a[1]
			case 9:
				v ^= a[1]
			case 10:
				v >>= uint32(a[1]) & 31
			case 11:
				v <<= uint32(a[1]) & 31
			}
			values[i] = v
		}
		return true, nil
	}
	return true, vm.Push(result)
}
