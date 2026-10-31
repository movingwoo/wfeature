package session

import (
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/platform/skt/clet"
)

// The platform sessions behind a Session, for tests that look past it. Each
// answers nil when another kind is running.

func (s *Session) ktfNativeSession() *ktf.NativeSession {
	e, _ := s.engine.(ktfNativeEngine)
	return e.session
}

func (s *Session) cletPlatform() *clet.Session {
	e, _ := s.engine.(cletEngine)
	return e.session
}

func (s *Session) sktRuntime() *skt.Runtime {
	e, _ := s.engine.(sktJavaEngine)
	return e.runtime
}

func (s *Session) sktScript() *skt.ScriptSession {
	e, _ := s.engine.(sktScriptEngine)
	return e.script
}

// captureSurface is the surface a MIDP runtime draws into.
func (s *Session) captureSurface() *captureFramebuffer {
	switch e := s.engine.(type) {
	case sktJavaEngine:
		return e.surface
	case sktScriptEngine:
		return e.surface
	}
	return nil
}
