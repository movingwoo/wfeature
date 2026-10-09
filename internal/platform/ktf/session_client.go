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
	client.audio.SetLogger(options.Logger)
	_ = client.audio.SetPlaybackRate(0, options.Speed)
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

// restoreSessionClient is the whole restore in one call, for a caller that
// owns the store in its options and adopts nothing over a running session: the
// client comes back bound to that store, under its adapters. A session load
// goes through restoreSessionClientForActivation and binds when it commits.
func restoreSessionClient(archive *Archive, saved clientState, adapters saveAdapterState, options SessionOptions) (*Client, error) {
	client, err := restoreSessionClientForActivation(archive, saved, options, nil)
	if err != nil {
		return nil, err
	}
	if err := client.bindRestoredStorage(adapters, client.saveStore); err != nil {
		client.StopThreads()
		return nil, err
	}
	client.runtime.restoredStorage = nil
	return client, nil
}

// restoreSessionClientForActivation rebuilds a client from its record without
// running any of it and without reading a save: its storage objects hold names
// and no content, and its authentication adapters are not built, until
// Client.bindRestoredStorage connects it to a store.
func restoreSessionClientForActivation(archive *Archive, saved clientState, options SessionOptions, activation *clientActivation) (*Client, error) {
	// A restored core replaces the initially mapped one. Install diagnostics on
	// that final core once, after every record has passed validation.
	debug := options.Debug
	options.Debug = nil
	client, err := newSessionClient(archive, options)
	if err != nil {
		return nil, err
	}
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
