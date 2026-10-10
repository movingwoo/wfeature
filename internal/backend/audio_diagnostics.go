package backend

import "log/slog"

// SetLogger attaches the Host's existing diagnostic boundary to this timeline.
// Logging is optional, independent of the output sink and excluded from saves.
func (audio *Audio) SetLogger(logger *slog.Logger) {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	audio.sink.logger = logger
}

func (output *audioOutput) refuseWave(reason string, samples int) {
	if output.pcmRefusals != ^uint64(0) {
		output.pcmRefusals++
	}
	// Log at powers of two so a repeating overloaded score cannot flood logs.
	// NewLogger hides these detailed diagnostics in the release profile.
	if output.logger != nil && output.pcmRefusals&(output.pcmRefusals-1) == 0 {
		output.logger.Debug("audio PCM admission refused", "reason", reason,
			"sound", output.sound, "waves", len(output.waves), "bytes", output.waveBytes,
			"incoming_bytes", uint64(samples)*2, "refused", output.pcmRefusals)
	}
}
