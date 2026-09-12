package skt

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

const scriptExternalURLMaxBytes = 2048

// ErrExternalLaunchUnsupported reports a valid guest handoff for which the
// current Host supplied no intentional external-link action.
var ErrExternalLaunchUnsupported = errors.New("SGS external URL launch is unsupported by this host")

func (s *ScriptSession) beginExternalLaunch(vm *sgsvm.VM) error {
	resource := vm.Resource(int(vm.Pop()))
	if err := vm.Error(); err != nil {
		return err
	}
	length, err := scriptResourceStringLength(vm, resource.Data)
	if err != nil {
		return err
	}
	if length > scriptExternalURLMaxBytes {
		return fmt.Errorf("SGS external URL exceeds %d bytes", scriptExternalURLMaxBytes)
	}

	// Script text is EUC-KR. Decode it strictly before applying URL policy;
	// the observed destination is ASCII, which is represented identically.
	decoded, err := decodeScriptInputText(resource.Data[:length])
	if err != nil {
		return fmt.Errorf("SGS external URL: %w", err)
	}
	destination := string(decoded)
	parsed, err := url.Parse(destination)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return fmt.Errorf("SGS external URL must be an absolute HTTP or HTTPS URL")
	}

	// The original action terminates this invocation. A Host acknowledgement
	// belongs only to its notice and must never resume or call back into guest
	// code.
	vm.Yield()
	if s.options.ExternalLaunch == nil {
		return ErrExternalLaunchUnsupported
	}
	s.options.ExternalLaunch(destination)
	return nil
}
