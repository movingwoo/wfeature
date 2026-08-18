package cheat

import (
	"errors"
	"fmt"
	"strconv"
)

// The panel API. A user interface driving the cheat engine — the browser panel
// over a session socket, or the CLI's console — needs the same dozen operations
// answering the same shapes. Those shapes are the interface, so they live here
// rather than being rebuilt in each Host: two copies of "what a scan answers"
// would be two chances to disagree about it.
//
// Everything here is plain data. How it is encoded, and whether it crosses a
// socket at all, is the Host's business.

// PanelListLimit bounds the candidates one answer carries. A scan wider than
// this is not narrow enough to act on, and no panel can show it anyway.
const PanelListLimit = 200

// PanelHitLimit bounds the write sites one answer carries. The list is ordered
// by how often each site fired, so the head is the part that identifies the
// writer.
const PanelHitLimit = 50

// PanelCandidate is one address the scan still holds.
type PanelCandidate struct {
	Address uint32 `json:"address"`
	Value   int64  `json:"value"`
}

// PanelCandidates is what every scanning operation answers: how many addresses
// survive, and the head of the list.
type PanelCandidates struct {
	Count int              `json:"count"`
	Items []PanelCandidate `json:"items"`
}

// PanelFreeze is one held value.
type PanelFreeze struct {
	Address uint32 `json:"address"`
	Value   int64  `json:"value"`
	Type    string `json:"type"`
}

// PanelHit is one recorded write to a watched address. Origin is "guest" or
// "host"; the page needs it because a host hit's PC is the last guest
// instruction rather than the writer, and showing the two alike would offer an
// address to disassemble that has nothing to do with the write.
type PanelHit struct {
	Address uint32 `json:"address"`
	PC      uint32 `json:"pc"`
	Origin  string `json:"origin"`
	Value   int64  `json:"value"`
	Size    int    `json:"size"`
	Count   int64  `json:"count"`
	// Site names the writer on a platform where a PC cannot; empty otherwise,
	// and the page shows whichever it was given. See cheat.WatchHit.Site.
	Site string `json:"site,omitempty"`
}

// PanelHits is the recorded-write answer.
type PanelHits struct {
	Items      []PanelHit `json:"items"`
	Total      int        `json:"total"`
	Overflowed bool       `json:"overflowed"`
}

// ErrNoPreviousScan explains the one failure a user can act on, in the words
// the panel shows. Comparing against the previous scan needs a previous scan.
var ErrNoPreviousScan = errors.New("이 필터는 이전 검색과 비교합니다. 먼저 `이 값으로 찾기`나 `값 모름`으로 시작하세요.")

// PanelScan runs one scan and answers what survived. The value type and filter
// arrive as the names the panel uses, and are parsed through the same parser
// the text console uses so the two cannot drift.
func PanelScan(session *Session, valueTypeName, filterName string, operand int64) (PanelCandidates, error) {
	if session == nil {
		return PanelCandidates{}, errors.New("no cheat session")
	}
	valueType, ok := ParseValueType(valueTypeName)
	if !ok {
		return PanelCandidates{}, fmt.Errorf("unknown value type %q", valueTypeName)
	}
	if session.Scanner().ValueType() != valueType {
		session.Scanner().SetValueType(valueType)
	}
	filter, err := ParseFilter(panelFilterArguments(filterName, operand), panelNumber)
	if err != nil {
		return PanelCandidates{}, err
	}
	if _, err := session.Scan(filter); err != nil {
		if errors.Is(err, ErrNeedsPreviousValue) {
			return PanelCandidates{}, ErrNoPreviousScan
		}
		return PanelCandidates{}, err
	}
	return PanelCandidateList(session), nil
}

// PanelRefresh re-reads the surviving candidates without narrowing them, which
// is what keeps a panel's values live while a game runs.
func PanelRefresh(session *Session) (PanelCandidates, error) {
	if session == nil {
		return PanelCandidates{}, errors.New("no cheat session")
	}
	session.Refresh()
	return PanelCandidateList(session), nil
}

// PanelUndo steps back one scan.
func PanelUndo(session *Session) (PanelCandidates, error) {
	if session == nil {
		return PanelCandidates{}, errors.New("no cheat session")
	}
	if !session.Scanner().Undo() {
		return PanelCandidates{}, errors.New("되돌릴 검색이 없습니다")
	}
	return PanelCandidateList(session), nil
}

// PanelReset forgets the search and starts over.
func PanelReset(session *Session) (PanelCandidates, error) {
	if session == nil {
		return PanelCandidates{}, errors.New("no cheat session")
	}
	session.Scanner().Reset()
	return PanelCandidateList(session), nil
}

// PanelCandidateList is the current search state, bounded for display.
func PanelCandidateList(session *Session) PanelCandidates {
	candidates := session.Candidates()
	listed := min(len(candidates), PanelListLimit)
	items := make([]PanelCandidate, 0, listed)
	for _, candidate := range candidates[:listed] {
		items = append(items, PanelCandidate{Address: candidate.Address, Value: candidate.Value})
	}
	return PanelCandidates{Count: len(candidates), Items: items}
}

// PanelFreeze holds a value at an address, and answers the whole freeze list
// so a panel never has to ask separately.
func PanelFreezeValue(session *Session, address uint32, value int64, valueTypeName string) ([]PanelFreeze, error) {
	if session == nil {
		return nil, errors.New("no cheat session")
	}
	valueType, ok := ParseValueType(valueTypeName)
	if !ok {
		return nil, fmt.Errorf("unknown value type %q", valueTypeName)
	}
	if _, err := session.Freeze(address, valueType, value, ""); err != nil {
		return nil, err
	}
	return PanelFrozen(session), nil
}

// PanelUnfreeze releases one address.
func PanelUnfreeze(session *Session, address uint32) ([]PanelFreeze, error) {
	if session == nil {
		return nil, errors.New("no cheat session")
	}
	session.Unfreeze(address)
	return PanelFrozen(session), nil
}

// PanelFrozen lists what is held.
func PanelFrozen(session *Session) []PanelFreeze {
	if session == nil {
		return nil
	}
	entries := session.Freezes().Entries()
	items := make([]PanelFreeze, 0, len(entries))
	for _, entry := range entries {
		items = append(items, PanelFreeze{
			Address: entry.Address,
			Value:   entry.Value,
			Type:    entry.ValueType.String(),
		})
	}
	return items
}

// PanelWatch starts recording writes to an address and answers the watch list.
func PanelWatch(session *Session, address uint32) ([]uint32, error) {
	if session == nil {
		return nil, errors.New("no cheat session")
	}
	if err := session.Watch(address); err != nil {
		return nil, err
	}
	return session.Watches()
}

// PanelUnwatch stops recording one address, or every address when all is set.
func PanelUnwatch(session *Session, address uint32, all bool) ([]uint32, error) {
	if session == nil {
		return nil, errors.New("no cheat session")
	}
	if all {
		if err := session.ClearWatches(); err != nil {
			return nil, err
		}
		return session.Watches()
	}
	if err := session.Unwatch(address); err != nil {
		return nil, err
	}
	return session.Watches()
}

// PanelWatchHits answers the recorded writes, bounded for display.
func PanelWatchHits(session *Session) (PanelHits, error) {
	if session == nil {
		return PanelHits{}, errors.New("no cheat session")
	}
	hits, overflowed, err := session.WatchHits()
	if err != nil {
		return PanelHits{}, err
	}
	items := make([]PanelHit, 0, min(len(hits), PanelHitLimit))
	for index, hit := range hits {
		if index >= PanelHitLimit {
			break
		}
		items = append(items, PanelHit{
			Address: hit.Address,
			PC:      hit.PC,
			Origin:  hit.Origin.String(),
			Value:   int64(hit.Value),
			Size:    int(hit.Size),
			Count:   int64(hit.Count),
			Site:    hit.Site,
		})
	}
	return PanelHits{Items: items, Total: len(hits), Overflowed: overflowed}, nil
}

// PanelSaveTable renders the session's freezes and watches as the table text a
// user keeps between sessions.
func PanelSaveTable(session *Session, game string) (string, error) {
	if session == nil {
		return "", errors.New("no cheat session")
	}
	data, err := MarshalTable(session.SaveTable(game))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// PanelLoadTable applies a saved table and reports how many entries took.
func PanelLoadTable(session *Session, text string) (applied int, game string, err error) {
	if session == nil {
		return 0, "", errors.New("no cheat session")
	}
	table, err := UnmarshalTable([]byte(text))
	if err != nil {
		return 0, "", err
	}
	applied, err = session.LoadTable(table)
	if err != nil {
		return 0, "", err
	}
	return applied, table.Game, nil
}

// panelFilterArguments rebuilds the console's filter syntax from the panel's
// operation name, so both surfaces go through one filter parser.
func panelFilterArguments(operation string, operand int64) []string {
	switch operation {
	case "eq", "ne", "gt", "lt":
		return []string{operation, strconv.FormatInt(operand, 10)}
	}
	return []string{operation}
}

func panelNumber(text string) (int64, bool) {
	value, err := strconv.ParseInt(text, 0, 64)
	return value, err == nil
}
