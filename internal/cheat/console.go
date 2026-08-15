package cheat

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Results printed by a bare `list` before it starts summarizing.
const consoleListLimit = 20

// A `scan` that leaves more candidates than this prints a count only; the
// listing is not useful until the search has narrowed.
const consoleAutoListLimit = 10

// Console executes the text cheat command language against a session, so the
// native CLI and the browser Host share one command surface.
type Console struct {
	// game names what a saved table was made against.
	game    string
	session *Session
}

// NewConsole wraps session with the command processor.
func NewConsole(session *Session) *Console {
	return &Console{session: session}
}

// SetGame names the game a saved table was made against, so a table read later
// can be placed. It is free text: nothing enforces it.
func (console *Console) SetGame(name string) { console.game = name }

// Session exposes the wrapped session.
func (console *Console) Session() *Session { return console.session }

// Execute runs one command line and returns its printable output. Errors are
// returned as output prefixed with "cheat:" — the console is interactive and
// a bad command should never stop the Host loop.
func (console *Console) Execute(line string) string {
	args := strings.Fields(strings.TrimSpace(line))
	if len(args) == 0 {
		return ""
	}
	output, err := console.execute(args)
	if err != nil {
		return "cheat: " + err.Error()
	}
	return output
}

func (console *Console) execute(args []string) (string, error) {
	switch args[0] {
	case "help", "?":
		return consoleHelp, nil
	case "regions", "r":
		return console.regions()
	case "type":
		return console.setType(args[1:])
	case "align":
		return console.setAlign(args[1:])
	case "scan", "s":
		return console.scan(args[1:])
	case "undo":
		if !console.session.Scanner().Undo() {
			return "", fmt.Errorf("nothing to undo")
		}
		return fmt.Sprintf("%d hit(s)", console.session.Scanner().Len()), nil
	case "reset":
		console.session.Scanner().Reset()
		return "scan reset", nil
	case "list", "l":
		return console.list(args[1:])
	case "read":
		return console.read(args[1:])
	case "set", "write":
		return console.set(args[1:])
	case "dump":
		return console.dump(args[1:])
	case "freeze", "f":
		return console.freeze(args[1:])
	case "unfreeze":
		return console.unfreeze(args[1:])
	case "frozen":
		return console.frozen()
	case "watch", "w":
		return console.watch(args[1:])
	case "unwatch":
		return console.unwatch(args[1:])
	case "hits":
		return console.hits(args[1:])
	case "save":
		return console.save(args[1:])
	case "load":
		return console.load(args[1:])
	default:
		return "", fmt.Errorf("unknown command %q (try `help`)", args[0])
	}
}

const consoleHelp = `commands:
  regions                     committed guest memory
  type [t]                    show or set value type (u8/u16/u32/i8/i16/i32, +be)
  align [n]                   show or set scan stride
  scan [t] <filter>           = N | != N | > N | < N | N..M | unknown |
                              changed | unchanged | inc [N] | dec [N]
  undo / reset                revert last scan / start over
  list [n]                    show surviving candidates with live values
  read <addr> [t]             read one value
  set <addr> <value> [t]      write one value
  dump <addr> [len]           hex dump
  freeze <addr> <value> [t] [label]
  unfreeze <addr>|all
  frozen                      list frozen values
  watch <addr>                record what writes an address
  unwatch <addr>|all
  hits [n]                    instructions that wrote the watched addresses
  save <file>                 write frozen values and watches to a cheat table
  load <file>                 apply a cheat table`

// watch, unwatch, and hits expose the store instrumentation. A scan answers
// where a value is; these answer what changes it, which is the part that
// survives to the next run.
func (console *Console) watch(args []string) (string, error) {
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	if err := console.session.Watch(address); err != nil {
		return "", err
	}
	return fmt.Sprintf("watching %#08x", address), nil
}

func (console *Console) unwatch(args []string) (string, error) {
	if len(args) == 1 && args[0] == "all" {
		if err := console.session.ClearWatches(); err != nil {
			return "", err
		}
		return "all watches cleared", nil
	}
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	if err := console.session.Unwatch(address); err != nil {
		return "", err
	}
	return fmt.Sprintf("stopped watching %#08x", address), nil
}

func (console *Console) hits(args []string) (string, error) {
	limit := 20
	if len(args) >= 1 {
		parsed, ok := parseNumber(args[0])
		if !ok || parsed <= 0 {
			return "", fmt.Errorf("invalid hit count %q", args[0])
		}
		limit = int(parsed)
	}
	hits, overflowed, err := console.session.WatchHits()
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "no writes recorded yet", nil
	}
	var builder strings.Builder
	for index, hit := range hits {
		if index >= limit {
			fmt.Fprintf(&builder, "... %d more\n", len(hits)-limit)
			break
		}
		fmt.Fprintf(&builder, "%#08x written by pc %#08x  %d time(s)  last %#x (%d bytes)\n",
			hit.Address, hit.PC, hit.Count, hit.Value, hit.Size)
	}
	if overflowed {
		builder.WriteString("(the distinct-writer limit was reached; sites are missing)\n")
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func (console *Console) save(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("save expects a file path")
	}
	data, err := MarshalTable(console.session.SaveTable(console.game))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(args[0], data, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("saved %d frozen value(s) to %s", console.session.Freezes().Len(), args[0]), nil
}

func (console *Console) load(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("load expects a file path")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return "", err
	}
	table, err := UnmarshalTable(data)
	if err != nil {
		return "", err
	}
	applied, err := console.session.LoadTable(table)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("applied %d frozen value(s) from %s", applied, args[0]), nil
}

func (console *Console) regions() (string, error) {
	regions := console.session.Regions()
	if len(regions) == 0 {
		return "no committed memory yet", nil
	}
	var builder strings.Builder
	total := uint64(0)
	for _, region := range regions {
		fmt.Fprintf(&builder, "  0x%08x-0x%08x  %8d  %s\n", region.Base, uint64(region.Base)+uint64(region.Size), region.Size, region.Label)
		total += uint64(region.Size)
	}
	fmt.Fprintf(&builder, "%d region(s), %d bytes total", len(regions), total)
	return builder.String(), nil
}

func (console *Console) setType(args []string) (string, error) {
	scanner := console.session.Scanner()
	if len(args) == 0 {
		return fmt.Sprintf("type is %s, align %d", scanner.ValueType(), scanner.Align()), nil
	}
	valueType, ok := ParseValueType(args[0])
	if !ok {
		return "", fmt.Errorf("unknown type %q", args[0])
	}
	scanner.SetValueType(valueType)
	return fmt.Sprintf("type is %s, scan reset", valueType), nil
}

func (console *Console) setAlign(args []string) (string, error) {
	scanner := console.session.Scanner()
	if len(args) == 0 {
		return fmt.Sprintf("align is %d", scanner.Align()), nil
	}
	align, ok := parseNumber(args[0])
	if !ok || align < 1 {
		return "", fmt.Errorf("align must be a number of at least 1")
	}
	scanner.SetAlign(int(align))
	return fmt.Sprintf("align is %d", align), nil
}

func (console *Console) scan(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: scan [type] <= N | != N | > N | < N | N..M | unknown | changed | unchanged | inc [N] | dec [N]>")
	}
	// `scan u32 = 500` sets the type inline; a bare `scan = 400` keeps it.
	scanner := console.session.Scanner()
	prefix := ""
	if valueType, ok := ParseValueType(args[0]); ok {
		if valueType != scanner.ValueType() {
			if scanner.Started() {
				prefix = fmt.Sprintf("type changed to %s, previous scan discarded\n", valueType)
			}
			scanner.SetValueType(valueType)
		}
		args = args[1:]
	}
	filter, err := ParseFilter(args, parseNumber)
	if err != nil {
		return "", err
	}
	count, err := console.session.Scan(filter)
	if err != nil {
		if err == ErrNeedsPreviousValue {
			return "", fmt.Errorf("this filter compares against the previous scan; start with `= N` or `unknown`")
		}
		return "", err
	}
	output := prefix + fmt.Sprintf("%d hit(s)", count)
	if count > 0 && count <= consoleAutoListLimit {
		output += "\n" + console.formatCandidates(count)
	}
	return output, nil
}

func (console *Console) list(args []string) (string, error) {
	limit := consoleListLimit
	if len(args) > 0 {
		parsed, ok := parseNumber(args[0])
		if !ok {
			return "", fmt.Errorf("count must be a number")
		}
		limit = max(int(parsed), 0)
	}
	if !console.session.Scanner().Started() {
		return "", fmt.Errorf("no scan in progress")
	}
	console.session.Refresh()
	return console.formatCandidates(limit), nil
}

func (console *Console) formatCandidates(limit int) string {
	candidates := console.session.Candidates()
	var builder strings.Builder
	for index, candidate := range candidates {
		if index >= limit {
			break
		}
		fmt.Fprintf(&builder, "  0x%08x = %d\n", candidate.Address, candidate.Value)
	}
	if len(candidates) > limit {
		fmt.Fprintf(&builder, "  ... and %d more\n", len(candidates)-limit)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (console *Console) read(args []string) (string, error) {
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	valueType, err := console.valueTypeArg(args, 1)
	if err != nil {
		return "", err
	}
	value, err := console.session.ReadValue(address, valueType)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("0x%08x = %d (%#x) [%s]", address, value, value, valueType), nil
}

func (console *Console) set(args []string) (string, error) {
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	if len(args) < 2 {
		return "", fmt.Errorf("usage: set <addr> <value> [type]")
	}
	value, ok := parseNumber(args[1])
	if !ok {
		return "", fmt.Errorf("value must be a number")
	}
	valueType, err := console.valueTypeArg(args, 2)
	if err != nil {
		return "", err
	}
	if err := console.session.WriteValue(address, valueType, value); err != nil {
		return "", err
	}
	return fmt.Sprintf("0x%08x = %d [%s]", address, value, valueType), nil
}

func (console *Console) dump(args []string) (string, error) {
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	length := 0x40
	if len(args) > 1 {
		parsed, ok := parseNumber(args[1])
		if !ok {
			return "", fmt.Errorf("length must be a number")
		}
		length = int(min(max(parsed, 1), 0x1000))
	}
	bytes, err := console.session.ReadBytes(address, length)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	for row := 0; row*16 < len(bytes); row++ {
		chunk := bytes[row*16 : min(row*16+16, len(bytes))]
		hex := make([]string, len(chunk))
		ascii := make([]byte, len(chunk))
		for index, value := range chunk {
			hex[index] = fmt.Sprintf("%02x", value)
			if value >= 0x20 && value < 0x7f {
				ascii[index] = value
			} else {
				ascii[index] = '.'
			}
		}
		fmt.Fprintf(&builder, "  0x%08x  %-47s  %s\n", uint64(address)+uint64(row*16), strings.Join(hex, " "), ascii)
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func (console *Console) freeze(args []string) (string, error) {
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	if len(args) < 2 {
		return "", fmt.Errorf("usage: freeze <addr> <value> [type] [label]")
	}
	value, ok := parseNumber(args[1])
	if !ok {
		return "", fmt.Errorf("value must be a number")
	}
	valueType, err := console.valueTypeArg(args, 2)
	if err != nil {
		return "", err
	}
	label := ""
	if len(args) > 3 {
		label = args[3]
	}
	replaced, err := console.session.Freeze(address, valueType, value, label)
	if err != nil {
		return "", err
	}
	verb := "froze"
	if replaced {
		verb = "refroze"
	}
	return fmt.Sprintf("%s 0x%08x = %d [%s]", verb, address, value, valueType), nil
}

func (console *Console) unfreeze(args []string) (string, error) {
	if len(args) > 0 && args[0] == "all" {
		console.session.UnfreezeAll()
		return "all freezes cleared", nil
	}
	address, err := parseAddress(args, 0)
	if err != nil {
		return "", err
	}
	if !console.session.Unfreeze(address) {
		return "", fmt.Errorf("0x%08x is not frozen", address)
	}
	return fmt.Sprintf("unfroze 0x%08x", address), nil
}

func (console *Console) frozen() (string, error) {
	entries := console.session.Freezes().Entries()
	if len(entries) == 0 {
		return "nothing frozen", nil
	}
	var builder strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&builder, "  0x%08x = %d [%s] %s\n", entry.Address, entry.Value, entry.ValueType, entry.Label)
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func (console *Console) valueTypeArg(args []string, index int) (ValueType, error) {
	if index >= len(args) {
		return console.session.Scanner().ValueType(), nil
	}
	valueType, ok := ParseValueType(args[index])
	if !ok {
		return ValueType{}, fmt.Errorf("unknown type %q", args[index])
	}
	return valueType, nil
}

func parseAddress(args []string, index int) (uint32, error) {
	if index >= len(args) {
		return 0, fmt.Errorf("missing address")
	}
	value, ok := parseNumber(args[index])
	if !ok || value < 0 || value > 0xffffffff {
		return 0, fmt.Errorf("invalid address %q", args[index])
	}
	return uint32(value), nil
}

// parseNumber accepts decimal, 0x-prefixed hex, and negative values.
func parseNumber(s string) (int64, bool) {
	value, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		// Addresses like ffff0000 without a 0x prefix are still common input.
		raw, hexErr := strconv.ParseUint(s, 16, 32)
		if hexErr != nil {
			return 0, false
		}
		return int64(raw), true
	}
	return value, true
}
