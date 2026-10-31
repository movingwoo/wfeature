package ktf

import "github.com/movingwoo/wfeature/internal/platform/wipicrt"

// detachedRuntime is a runtime over a bare engine client and nothing else,
// for a test that drives the engine's storage, clock or diagnostics directly.
func detachedRuntime(engine *wipicrt.Client) *initializationRuntime {
	client := &Client{Client: engine}
	runtime := &initializationRuntime{client: client, Runtime: &wipicrt.Runtime{Client: engine}}
	client.setRuntimes(runtime, nil)
	return runtime
}
