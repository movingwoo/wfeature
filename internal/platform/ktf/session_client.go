package ktf

import (
	"fmt"
	"path/filepath"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

func sessionCoreOptions(options SessionOptions) armcore.CoreOptions {
	steps := options.MaxSteps
	if steps == 0 {
		steps = sessionDefaultMaxSteps
	}
	return armcore.CoreOptions{MaxSteps: steps}
}

// newSessionClient attaches the archive and Host boundaries without running
// guest code or creating per-run authentication adapters. Startup and detached
// restoration use the same construction path.
func newSessionClient(archive *Archive, options SessionOptions) (*Client, error) {
	if archive == nil || archive.JAR == nil {
		return nil, fmt.Errorf("KTF session archive is missing")
	}
	client, err := LoadClient(archive.JAR.Client, sessionCoreOptions(options))
	if err != nil {
		return nil, err
	}
	// Watches must be installed before the first guest write, including entry.
	if options.Debug != nil {
		options.Debug(client.core)
	}
	client.clock = options.Clock
	if client.clock == nil {
		client.clock = wallClock{}
	}
	client.SetSpeed(options.Speed)
	client.SetScreen(options.Width, options.Height)
	client.SetDiagnostics(options.TraceLimit, options.Logger)
	client.audio = backend.NewAudioWithClock(options.AudioSink, client.now)
	client.frameSink = options.FrameSink
	client.threadSliceSteps = options.ThreadSliceSteps
	client.serviceSteps, client.serviceWait = options.ServiceSteps, options.ServiceWait
	client.SetProgramName(ProgramNameForAID(archive.Descriptor.AID))
	client.AttachAppProperties(archive.Descriptor.Properties)
	client.AttachResources(archive.JAR.Entries)
	client.AttachFilesystem(archive.GuestFiles())
	if options.SaveStore != nil {
		client.AttachSaveStore(options.SaveStore)
	} else if options.SaveRoot != "" {
		client.AttachSaveStore(NewDirectorySaveStore(filepath.Join(options.SaveRoot, SaveOwner(archive.Descriptor))))
	}
	return client, nil
}

// The caller supplies an isolated save store and owns the admission barrier.
// Archive identity, the durable save transaction and live adoption are separate.
func restoreSessionClient(archive *Archive, saved clientState, adapters saveAdapterState, options SessionOptions) (*Client, error) {
	return restoreSessionClientForActivation(archive, saved, adapters, options, nil)
}

func restoreSessionClientForActivation(archive *Archive, saved clientState, adapters saveAdapterState, options SessionOptions, activation *clientActivation) (*Client, error) {
	// A restored core replaces the initially mapped one. Install diagnostics on
	// that final core once, after every record has passed validation.
	debug := options.Debug
	options.Debug = nil
	client, err := newSessionClient(archive, options)
	if err != nil {
		return nil, err
	}
	store, err := restoreSaveAdapters(adapters, client.saveStore)
	if err != nil {
		return nil, err
	}
	client.saveStore = store
	client.runtime, err = newInitializationRuntime(client)
	if err != nil {
		return nil, err
	}
	if err := client.restoreClientStateForActivation(saved, sessionCoreOptions(options), options.AudioSink, activation); err != nil {
		return nil, err
	}
	if debug != nil {
		debug(client.core)
	}
	return client, nil
}
