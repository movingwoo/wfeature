package backend

import (
	"io"
	"log/slog"
)

// BuildProfile is the user-facing build distinction. It is deliberately not
// tied to a development or production runtime environment.
func BuildProfile() string {
	if debugBuild {
		return "debug"
	}
	return "release"
}

// DebugBuild reports whether this binary carries the debug profile's detailed
// logs and diagnostics. Hosts use it to size diagnostic collection; release
// builds keep the reduced surface.
func DebugBuild() bool {
	return debugBuild
}

func NewLogger(output io.Writer) *slog.Logger {
	level := slog.LevelInfo
	if debugBuild {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
}
