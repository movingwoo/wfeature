package cheat

// Session is one attached cheat session: a scanner and the frozen values,
// bound to a single emulator client's memory.
type Session struct {
	target  MemoryTarget
	scanner *Scanner
	freezes FreezeList
}

// NewSession attaches a session to target with the default u32 scanner.
func NewSession(target MemoryTarget) *Session {
	return &Session{
		target:  target,
		scanner: NewScanner(ValueType{Kind: KindU32, Endian: Little}),
	}
}

// Scanner exposes the progressive search state.
func (session *Session) Scanner() *Scanner { return session.scanner }

// Freezes exposes the frozen values.
func (session *Session) Freezes() *FreezeList { return &session.freezes }

// Regions reports the target's scannable guest memory.
func (session *Session) Regions() []Region { return session.target.Regions() }

// Scan runs one search pass with filter.
func (session *Session) Scan(filter ScanFilter) (int, error) {
	return session.scanner.Scan(session.target, filter)
}

// Refresh re-reads every candidate's live value.
func (session *Session) Refresh() {
	session.scanner.Refresh(session.target)
}

// Candidates exposes the surviving addresses of the current search.
func (session *Session) Candidates() []Candidate { return session.scanner.Candidates() }

// ReadValue decodes one value of valueType at address.
func (session *Session) ReadValue(address uint32, valueType ValueType) (int64, error) {
	buffer := make([]byte, valueType.Size())
	if err := session.target.ReadMemory(address, buffer); err != nil {
		return 0, err
	}
	value, _ := valueType.Decode(buffer)
	return value, nil
}

// ReadBytes reads length raw bytes at address.
func (session *Session) ReadBytes(address uint32, length int) ([]byte, error) {
	buffer := make([]byte, length)
	if err := session.target.ReadMemory(address, buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

// WriteValue encodes value as valueType and writes it at address.
func (session *Session) WriteValue(address uint32, valueType ValueType, value int64) error {
	bytes, err := valueType.Encode(value)
	if err != nil {
		return err
	}
	return session.target.WriteMemory(address, bytes)
}

// Freeze pins value at address. It writes immediately so the effect is
// visible without waiting for the next tick, and reports whether an existing
// freeze on the same address was replaced.
func (session *Session) Freeze(address uint32, valueType ValueType, value int64, label string) (bool, error) {
	if err := session.WriteValue(address, valueType, value); err != nil {
		return false, err
	}
	return session.freezes.Insert(FreezeEntry{
		Address:   address,
		ValueType: valueType,
		Value:     value,
		Label:     label,
	}), nil
}

// Unfreeze removes the freeze at address.
func (session *Session) Unfreeze(address uint32) bool { return session.freezes.Remove(address) }

// UnfreezeAll removes every freeze.
func (session *Session) UnfreezeAll() { session.freezes.Clear() }

// Tick performs per-tick maintenance: rewrite frozen values so they win over
// whatever the game just wrote. It returns the addresses whose write failed.
func (session *Session) Tick() []uint32 {
	if session.freezes.Len() == 0 {
		return nil
	}
	return session.freezes.Apply(session.target)
}
