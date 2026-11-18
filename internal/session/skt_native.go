package session

import (
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/platform/skt/clet"
)

// sktNativeProgram lays an SKT native package out for the Clet runtime that
// runs it. Both the package and the runtime are SKT's: an SKT native executable
// is a WIPI-C program, and `internal/platform/skt/clet` runs it on the WIPI-C
// engine KTF titles run on too (see docs/skt-native.md). The package's code is
// placed at the engine's image base and its data at the same distance from the
// code it was linked at, which is the placement the executable's own
// relocations describe.
//
// Authentication compatibility can bind the handset identity from a pair of
// packaged certificates or apply a recognized comparison fix before loading.
func sktNativeProgram(archive []byte, disableAuthentication bool) (clet.CletProgram, *skt.NativeArchive, error) {
	opened, err := skt.OpenNative(archive)
	if err != nil {
		return clet.CletProgram{}, nil, err
	}
	executable := opened.Executable
	code, data, err := executable.Load(clet.ImageBase)
	if err != nil {
		return clet.CletProgram{}, nil, err
	}
	authentication := backend.AuthenticationOff
	var subscriber string
	if !disableAuthentication {
		if authentication, err = executable.ApplyIdentityCompatibility(code); err != nil {
			return clet.CletProgram{}, nil, err
		}
		subscriber = opened.PairedCertificateSubscriber()
		if subscriber != "" {
			authentication = backend.AuthenticationSKTNativeCertificate
		}
	}
	dataAddress := clet.ImageBase + skt.NativeDataLink
	entry := func(offset uint32) uint32 {
		if offset == 0 {
			return 0
		}
		return clet.ImageBase + offset
	}
	return clet.CletProgram{
		Name:        opened.ID + ".daf",
		ProgramName: opened.ID,
		Identity:    backend.SaveIdentity(archive),
		SaveOwner:   skt.NativeSaveOwner(opened),
		Code:        code,
		Data:        data,
		DataAddress: dataAddress,
		BSSSize:     executable.BSSSize,
		RootSlot:    dataAddress,
		Entries: clet.CletEntries{
			Start:   entry(executable.Entries.Start),
			Pause:   entry(executable.Entries.Pause),
			Resume:  entry(executable.Entries.Resume),
			Destroy: entry(executable.Entries.Destroy),
			Paint:   entry(executable.Entries.Paint),
			Event:   entry(executable.Entries.Event),
		},
		Resources:        executable.Resources,
		ResourceOrder:    executable.ResourceOrder,
		Files:            opened.ProgramFiles(),
		Authentication:   authentication,
		SubscriberNumber: subscriber,
		ServiceSteps:     executable.ServiceAllowance(),
	}, opened, nil
}
