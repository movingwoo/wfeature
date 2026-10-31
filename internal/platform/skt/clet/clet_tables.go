package clet

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The SKT root table, as both local titles index it (2026-10-30; see
// docs/skt-native.md). Each slot's number is the offset a title loads its
// table pointer from, divided by four; slots 2, 8, 12 and 13 are not reached
// by either title.
const (
	sktRootKernel     = 0
	sktRootGraphics   = 1
	sktRootFilesystem = 3
	sktRootMisc       = 4
	sktRootNet        = 5
	sktRootUtil       = 6
	sktRootMedia      = 7
	sktRootSerial     = 9
	sktRootPhone      = 10
	sktRootTerminal   = 11
	sktRootCLibrary   = 14
)

// sktKernelFunctions are the kernel slots a Clet is answered on. The SKT
// kernel table numbers its functions as the specification lists them, and so
// does this runtime's: every slot either local title calls (0, 1, 7, 9, 14 and
// 20 to 31) carries the arguments the specification gives the function at that
// position. Slot 33 and above are this runtime's own additions for KTF
// libraries and are not offered.
var sktKernelFunctions = map[uint32]bool{
	wipicrt.WIPICKernelPrintk: true, wipicrt.WIPICKernelSprintk: true, wipicrt.WIPICKernelGetExecNames: true,
	wipicrt.WIPICKernelLoad: true, wipicrt.WIPICKernelExit: true, wipicrt.WIPICKernelGetCurProgramID: true,
	wipicrt.WIPICKernelGetAccessLevel: true, wipicrt.WIPICKernelGetProgramName: true,
	wipicrt.WIPICKernelAlloc: true, wipicrt.WIPICKernelCalloc: true, wipicrt.WIPICKernelFree: true,
	wipicrt.WIPICKernelGetTotalMemory: true, wipicrt.WIPICKernelGetFreeMemory: true,
	wipicrt.WIPICKernelDefTimer: true, wipicrt.WIPICKernelSetTimer: true, wipicrt.WIPICKernelUnsetTimer: true,
	wipicrt.WIPICKernelCurrentTime: true, wipicrt.WIPICKernelGetSysProperty: true, wipicrt.WIPICKernelSetSysProperty: true,
	wipicrt.WIPICKernelGetResourceID: true, wipicrt.WIPICKernelGetResource: true,
}

// The SKT graphics table is the specification's graphics section up to
// MC_grpPostEvent at 36, and then not: the input-method functions follow it,
// in the order this runtime's KTF input-method table already carries them,
// and the handset's own additions come after those. Each is named by its
// callers in one title:
//
//	37 (key, type, buf, size, buf, size)  the input method's six arguments,
//	                                     a key and the title's text buffers:
//	                                     MC_imHandleInput
//	38 (mode)                            answered non-zero for success:
//	                                     MC_imSetCurrentMode
//	40 ()                                a count the title loops over the
//	                                     answer of 41 with, comparing each
//	                                     entry with "N123":
//	                                     MC_imGetSupportedModeCount
//	41 ()                                MC_imGetSupportedModes
//	44 (0, 1)                            at start-up, before the title takes
//	                                     a bar's height off its screen
//	                                     height; accepted with nothing to do
//	45 (0, "HEIGHT", buf, 4)             the other title's reading of that
//	                                     height, as text it hands to atoi:
//	                                     there is no bar here, so "0"
//	53 ("굴림", 0, 0)                     a font by its face's name, then
//	                                     (0, 0, 0) if that fails; the answer
//	                                     goes into the context's font slot
//	                                     and to MC_grpGetFontHeight. There is
//	                                     one face here, so it is
//	                                     MC_grpGetFont's answer for the size
//	52 (0, &surface)                     before anything is drawn; the title
//	                                     reads a framebuffer out of the
//	                                     surface's first word and flushes it
//	                                     to the screen: the screen
//	                                     framebuffer, handed back through a
//	                                     pointer
//	78, 79                               a property getter's and setter's
//	                                     cases that turn M_E_INVALID into the
//	                                     title's own "not supported"
//
// 39 sits inside the run 37 to 41 the callers name, so it is the third of the
// input-method table, MC_imGetCurrentMode.
const (
	sktGraphicsRepaint       = 25
	sktGraphicsPostEvent     = 36
	sktGraphicsInputFirst    = 37
	sktGraphicsInputLast     = 41
	sktGraphicsHandsetField  = 44
	sktGraphicsHandsetHeight = 45
	sktGraphicsScreenSurface = 52
	sktGraphicsNamedFont     = 53
	sktGraphicsPropertyGet   = 78
	sktGraphicsPropertySet   = 79
)

// sktKernelLaunch is a launcher call one title makes from its menus, handed
// "NGB", a count of two, "URL" and an address: a browser asked to open a page.
// There is no browser to hand it to, and the title ignores the answer.
//
// sktKernelSetting is handed ("EXITONEND", 0) by the other title at start-up,
// a setting by name for how the program ends. Nothing here ends differently,
// so it is accepted, and the title ignores the answer as well.
const (
	sktKernelSetting = 37
	sktKernelLaunch  = 50
)

// sktFilesystemFunction maps an SKT filesystem slot to this runtime's. The
// table is the specification's order with its last two entries exchanged:
// slot 15 is handed a name and an access area, which is MC_fsIsExist, and slot
// 16 a descriptor, which is MC_fsTell. One title calls both, and the other the
// second, each with those arguments.
func sktFilesystemFunction(function uint32) uint32 {
	switch function {
	case 15:
		return wipicrt.WIPICFileExists
	case 16:
		return wipicrt.WIPICFileTell
	}
	return function
}

// sktMediaFunctions maps the SKT media slots the local titles reach to this
// runtime's. The SKT table is not the specification's order and not the one
// this runtime's KTF table follows, so each is named by its callers, as that
// table's are:
//
//	0  (type, size, callback)  MC_mdaClipCreate, the type a string
//	1  (clip)                  the last call on a clip: MC_mdaClipFree
//	4  (clip, buf, size)       right after the create, and its answer is
//	                           compared with the size: MC_mdaClipPutData
//	10 (clip)                  between 17 and 1: MC_mdaClipClearData
//	13 (clip, level)           a level scaled to 0..100: MC_mdaClipSetVolume
//	14 (clip, repeat)          after the volume, a flag: MC_mdaPlay
//	15, 16 (clip)              the pair a title's pause and resume make:
//	                           MC_mdaPause and MC_mdaResume
//	17 (clip)                  first of the run that ends a clip: MC_mdaStop
//	21 (level, timeout)        (0, 0) to stop: MC_mdaVibrator
//	24 (source, state)         answered -1 or M_E_INVALID as a failure, and a
//	                           title sets source 3 off on its way out:
//	                           MC_mdaSetMuteState
//	25 (source)                beside 24 and compared with 1: MC_mdaGetMuteState
//
// A slot neither title reaches is refused with its caller named.
var sktMediaFunctions = map[uint32]uint32{
	0:  wipicrt.WIPICMediaClipCreate,
	1:  wipicrt.WIPICMediaClipFree,
	4:  wipicrt.WIPICMediaClipPutData,
	10: wipicrt.WIPICMediaClipClearData,
	13: wipicrt.WIPICMediaClipSetVolume,
	14: wipicrt.WIPICMediaPlay,
	15: wipicrt.WIPICMediaPause,
	16: wipicrt.WIPICMediaResume,
	17: wipicrt.WIPICMediaStop,
	21: wipicrt.WIPICMediaVibrator,
	24: wipicrt.WIPICMediaSetMuteState,
	25: wipicrt.WIPICMediaGetMuteState,
}

// handleCletCall routes one call through the SKT root table to the function
// this runtime serves for it.
func (runtime *initializationRuntime) handleCletCall(thread *armcore.Thread, id uint32) (uint32, error) {
	slot, function := id>>16, id&0xffff
	switch slot {
	case sktRootKernel:
		if sktKernelFunctions[function] {
			return runtime.HandleWIPICCall(thread, function)
		}
		if function == sktKernelSetting {
			runtime.CountDiagnostic("SKT kernel setting accepted" + runtime.CallerMark(thread))
			return 0, nil
		}
		if function == sktKernelLaunch {
			runtime.CountDiagnostic("SKT launch refused" + runtime.CallerMark(thread))
			return wipicrt.WIPICErrorInvalid, nil
		}
	case sktRootGraphics:
		return runtime.handleCletGraphicsCall(thread, function)
	case sktRootFilesystem:
		if function <= wipicrt.WIPICFileExists {
			return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableFilesystem<<16|sktFilesystemFunction(function))
		}
	case sktRootMisc:
		// MC_miscBackLight. The rest of the table — the LED calls — no
		// local title reaches.
		if function == 0 {
			return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableMisc<<16|function)
		}
	case sktRootNet:
		// The specification's order, which this runtime's net table follows:
		// connect, close, and the socket calls, all of which are refused here
		// the way they are for a KTF title.
		return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableNet<<16|function)
	case sktRootUtil:
		if function <= wipicrt.WIPICUtilInetAddrStr {
			return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableUtil<<16|function)
		}
	case sktRootMedia:
		if mapped, ok := sktMediaFunctions[function]; ok {
			return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableMedia<<16|mapped)
		}
	case sktRootSerial, sktRootPhone, sktRootTerminal:
		return runtime.handleCletDeviceCall(thread, slot, function)
	case sktRootCLibrary:
		return runtime.handleCletLibraryCall(thread, function)
	}
	return 0, fmt.Errorf("SKT WIPI-C root %d function %d is not implemented%s", slot, function, runtime.CallerSite(thread))
}

// handleCletGraphicsCall serves the SKT graphics table; see the constants above
// for where it leaves the specification's order.
func (runtime *initializationRuntime) handleCletGraphicsCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch {
	case function == sktGraphicsRepaint:
		// The paint entry is called for it on the next round, which is the
		// specification's answer; a card's paint is not this program's.
		runtime.RecordDiagnostic(wipicrt.DiagEvent{Kind: wipicrt.DiagWIPICCall, Nums: [5]uint32{wipicrt.WIPICTableGraphics<<16 | function}})
		runtime.client.clet.repaint = true
		return 0, nil
	case function <= sktGraphicsPostEvent:
		return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableGraphics<<16|function)
	case function >= sktGraphicsInputFirst && function <= sktGraphicsInputLast:
		return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableInputMethod<<16|(function-sktGraphicsInputFirst))
	case function == sktGraphicsHandsetField:
		runtime.CountDiagnostic(fmt.Sprintf("SKT graphics function %d accepted%s", function, runtime.CallerMark(thread)))
		return 0, nil
	case function == sktGraphicsHandsetHeight:
		return runtime.cletHandsetBarProperty(thread)
	case function == sktGraphicsScreenSurface:
		surface, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		screen, err := runtime.WIPICGetScreenFramebuffer()
		if err != nil {
			return 0, err
		}
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], screen)
		if err := runtime.client.Core().Memory().Write(surface, word[:]); err != nil {
			return 0, fmt.Errorf("write SKT screen surface at %#x: %w", surface, err)
		}
		return 0, nil
	case function == sktGraphicsNamedFont:
		// MC_grpGetFont reads its size from the second argument too.
		return runtime.HandleWIPICCall(thread, wipicrt.WIPICTableGraphics<<16|26)
	case function == sktGraphicsPropertyGet || function == sktGraphicsPropertySet:
		runtime.CountDiagnostic(fmt.Sprintf("SKT graphics function %d refused%s", function, runtime.CallerMark(thread)))
		return wipicrt.WIPICErrorInvalid, nil
	}
	return 0, fmt.Errorf("SKT WIPI-C graphics function %d is not implemented%s", function, runtime.CallerSite(thread))
}

// cletHandsetBarProperty answers graphics function 45, (lcd, name, buf, size):
// a property of the handset's bar written as text. The bar's height is the
// one a title asks for, and with no bar it is zero; any other name is not
// one this handset has.
func (runtime *initializationRuntime) cletHandsetBarProperty(thread *armcore.Thread) (uint32, error) {
	var arguments [4]uint32
	for index := range arguments {
		value, err := thread.Register(index)
		if err != nil {
			return 0, err
		}
		arguments[index] = value
	}
	name, err := runtime.ReadCString(arguments[1], 64)
	if err != nil {
		return 0, fmt.Errorf("read SKT handset property name: %w", err)
	}
	runtime.CountDiagnostic("SKT handset property " + name)
	if name != "HEIGHT" {
		return wipicrt.WIPICErrorInvalid, nil
	}
	if arguments[3] < 2 {
		return wipicrt.WIPICErrorShortBuf, nil
	}
	if err := runtime.client.Core().Memory().Write(arguments[2], []byte{'0', 0}); err != nil {
		return 0, fmt.Errorf("write SKT handset property: %w", err)
	}
	return 0, nil
}

// handleCletDeviceCall answers the serial port, the phone and the handset's
// own records — the phonebook — none of which a handset here has. Each opening
// call fails with M_E_ERROR, which is what the specification gives a device
// that is not there, and every other call on what could not be opened fails the
// same way. One title opens the serial port, sends an SMS and reads the
// phonebook from its own menus; the other sends an SMS. Both handle the
// failure themselves.
func (runtime *initializationRuntime) handleCletDeviceCall(thread *armcore.Thread, slot, function uint32) (uint32, error) {
	name := map[uint32]string{sktRootSerial: "serial", sktRootPhone: "phone", sktRootTerminal: "terminal resource"}[slot]
	runtime.CountDiagnostic(fmt.Sprintf("SKT %s function %d refused%s", name, function, runtime.CallerMark(thread)))
	return wipicrt.WipiErrorCode, nil
}
