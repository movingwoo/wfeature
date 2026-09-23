# Text input exception audit

## Result and scope

Reviewing SKT, KTF, LGT, the shared session, WebSocket ownership and the browser
dialog found five defect categories, reproduced in eleven new scenarios.
These are authored-fixture reproductions, not measurements of how frequently
real games encounter them. Product code was not changed during this audit.

All five categories were subsequently fixed. The findings below preserve the
original evidence; the [resolution](#resolution) records the implementation and
ordinary regression tests that now enforce it.

The inspected worktree includes the KTF timer and clipping corrections in
PR #192. Existing relevant Go tests passed 189 leaf cases. The browser dialog
and transport Node tests passed 25 tests. Seven additional dialog scenarios
passed in both Chromium and WebKit, for 14 browser checks. The browser checks
use real DOM controls with a controlled session adapter; existing Go tests
exercise the actual WebSocket transport. Synthetic composition events do not
establish physical phone IME behavior.

## Reproduced defects

### 1. A restored target can revive an old edit

SKT validates the current screen, text and constraints by value, without a
lifecycle revision for ordinary MIDP fields. Open a TextBox edit, change its
text, then restore the original string: the old edit is accepted. The same
happens after leaving and returning to the screen, changing and restoring
constraints, or opening and closing the command menu. All four probes fail
the expected stale-edit rejection.

KTF explicit LWC focus has the same gap: focus another component and then the
original component before committing. The old edit replaces the field value.
The shown-shell, KFC and LGT Java paths already have stronger revision checks;
their existence does not protect this explicit-focus path.

Relevant code: `internal/platform/skt/host_text_input.go`,
`internal/platform/ktf/runtime_lwc_host_input.go`, and
`runtimeComponentSetFocus` in `internal/platform/ktf/runtime_lwc.go`.
Fix direction: track target lifecycle and mutation revisions through the native
setters and focus changes, including changes that restore earlier values.

### 2. KTF can offer a hidden explicitly focused field

Create a shell with a text child, show it, explicitly focus the child, and hide
the shell. A new Host request still offers that child as editable. Hiding the
shell updates its visibility and removes the shown-shell reference, but leaves
`lwc:focus` pointing at the child. The explicit-focus branch does not verify
that the field still belongs to an active visible container.

This is a fresh availability error, distinct from submitting an old snapshot.
Relevant code: `Session.TextInput` in
`internal/platform/ktf/runtime_lwc_host_input.go` and
`runtimeComponentShown` in `internal/platform/ktf/runtime_lwc.go`.
Fix direction: validate visibility/ownership for explicit focus without
requiring a shell for legitimate card-drawn components.

### 3. SKT continues insertion after a callback removes the target

An authored TextComponent detaches itself during the first `insert(char)`
callback. Submitting `abc` still invokes all three insertions and returns
success. Canceling the context during that first callback also delivers all
three characters. `commitTextComponent` validates once before entering its
character loop and does not recheck the target, revision or cancellation
between guest callbacks.

Relevant code: `commitTextComponent` in
`internal/platform/skt/host_text_input.go`.
Fix direction: stop at the first ownership change or cancellation, invalidate
the consumed snapshot, and leave any accepted prefix owned by the guest.
Whole-field rollback is unavailable on this append interface.

### 4. LGT permits retry after text delivery followed by a guest fault

An authored Clet delivers `abc` through `MC_imHandleInput`, then faults just
before its callback returns. The completion buffer contains the text, but the
Host receives an error. After restoring the fixture's return instruction, the
same edit accepts `abc` again. Consuming completed text does not advance the
C input revision. The WebSocket handler retains an edit after generic guest
errors, so it cannot distinguish this case from a retryable rejection.

This proves repeated delivery through the same edit after partial failure.
An append widget can consequently duplicate a prefix; field-level duplication
was not measured in a real archive during this audit.

Relevant code: `cTextInput` in `internal/platform/lgt/host_text_input.go`,
`handleInputKey` in `internal/platform/lgt/wipic_im.go`, and
`internal/webhost/textinput.go`.
Fix direction: invalidate snapshots whenever delivery consumed bytes, including
error returns, while preserving retry after a rejection that consumed nothing.

### 5. Host composition and keypad editing disagree on length units

Set a Java field limit to four UTF-16 units. Host composition accepts `AB🙂`,
which occupies exactly four units. Pressing keypad `2` then appends another
character, producing five units. This reproduces in both SKT TextBox and KTF
LWC fields. The Host validator counts UTF-16 units, while the shared keypad
editor receives that limit as a count of Go runes.

Relevant code: `textEditor` in `internal/platform/skt/lcdui_input.go`,
`textEditorFor` in `internal/platform/ktf/runtime_lwc_key.go`, and
`internal/textinput/textinput.go`.
Fix direction: preserve the platform's length unit when switching between
Host composition and keypad edits. Test supplementary characters at the limit
and deletion followed by insertion, without changing C/EUC-KR field policy.

## Supported routes and deliberate limits

A game is not a single text-input contract. Different screens in one game can
use different implementations. Use these routes as the automated test units:

| Route | Primary contract and necessary evidence |
| --- | --- |
| SKT MIDP TextBox | Whole value, UTF-16 limit, screen lifecycle, command overlay |
| SKT Form TextField | Selected child, whole value, item-change notification |
| SKT XTextField | Focused field on the visible canvas, whole value |
| SKT TextComponent | Attached target, append by Java char, per-callback ownership |
| KTF LWC explicit focus | Whole value, visible/active target and focus lifetime |
| KTF LWC shown shell | Sole direct child/work component, shell and child revisions |
| KTF non-modal KFC form | Unique listened field, form lifetime, guest confirmation |
| KTF WIPI-C | EUC-KR append, synchronous or owning-timer delivery, CLR |
| LGT LWC | Focus and shown parent chain, revisions, whole value |
| LGT WIPI-C | EUC-KR append, declared capacity or recognized output-only caller |

An installed composition-delta listener on KTF/LGT LWC fields, ambiguous KTF
shell/form layouts, synchronous KFC modal entry, general KTF guest event-loop
delivery, and arbitrary game-drawn editors remain unsupported boundaries.
Their rejection is not automatically a regression.

Ten additional encoding checks confirm current C-path limits on KTF and LGT:
precomposed Korean and an empty string pass; decomposed Hangul, supplementary
Unicode and newlines are rejected. A decomposed string can look like ordinary
Korean while falling outside strict EUC-KR. This is a declared encoding limit,
not evidence that every Korean IME submission is supported.

## Making acceptance unambiguous

Record an **editor route**, not just an archive launch. Each record needs the
archive hash, isolated save setup, steps to the field, expected adapter, input
and expected guest value, deletion/replacement steps, confirmation, and save
restoration if the guest promises persistence at that point.

Use separate outcomes:

- **Pass:** reached the editor; readback or captured guest output proves the
  value; confirmation and the stated persistence boundary work.
- **Unsupported:** reached the editor and identified a contract outside the
  implemented routes above.
- **Unreached:** the route never established an active editor. A startup scan
  or API reference is only a candidate for a future manual route.
- **Fail:** reached a supported contract and violated a stated invariant.

The current red status combines missing targets, stale edits and invalid text.
Those cases already differ internally, but that single label cannot classify
a manual result. Preserve the underlying error and active adapter in test or
debug evidence. This audit does not change the compact user-facing labels.

For each supported route, share a matrix of empty/normal/maximum/over-limit
input; Korean and supplementary Unicode; newline/control policy; deletion and
replacement; target changes and restoration; callback failure after a prefix;
cancel/reopen; and session parking/resume. Keep callback and timer cases in
authored fixtures, where they are deterministic. Use representative real-game
routes to confirm field detection, display, confirmation and persistence.
Reserve physical-device testing for actual IME, clipboard, keyboard visibility,
and interruption behavior that synthetic events cannot establish.

## Reproduction artifacts

Ignored artifacts are under `var/diagnostics/text-input-audit-20260923/`:

- `overlay.json` and `*-audit.go.overlay`: authored tests appended through a
  Go overlay, preserving product code and the ordinary test suite.
- `baseline.jsonl`: 189 existing Go cases passed.
- `probes.jsonl`: 10 encoding cases passed and 11 new defect scenarios failed.
- `probes-debug.jsonl`: the same 10 passes and 11 failures in the debug profile.
- `browser.mjs`, `browser-results.json`: seven scenarios in two real browser
  engines, all passed, using the actual `web/text-input.js` module.

Run the defect probes from the original worktree with:

```sh
go test -overlay var/diagnostics/text-input-audit-20260923/overlay.json \
  ./internal/platform/skt ./internal/platform/ktf ./internal/platform/lgt \
  -run '^TestAudit' -count=1 -v
```

This overlay documents the original failing state. The regression cases now live
in the ordinary suite; use the command below against the corrected tree. There
are no new real-game or physical-device acceptance claims in this audit.

## Resolution

The five fixes are scoped to the platform adapters and shared keypad editor:

| Defect | Correction and regression coverage |
| --- | --- |
| Restored target | SKT display/menu, field mutation and Form selection/membership revisions; XTextField focus revisions; KTF component and Form focus revisions. Restored state rejects the old edit, while a fresh snapshot commits. |
| Hidden explicit focus | KTF records parent membership, follows a bounded ancestry chain to the shown shell, and rejects hidden, detached or replaced owners. Tests cover direct/nested/work children, a never-shown shell subclass, hide/show and remove/reinsert. Never-attached card-drawn fields retain support. |
| Callback interruption | SKT rechecks attachment, revision, display/queued display, command overlay and context between inserts and before repaint. Tests detach, detach/reattach, request another screen, cancel, and detach on the final character; a consumed snapshot cannot be reused. |
| Partial delivery retry | LGT advances the C edit revision whenever completion bytes were consumed, even on a guest fault. Authored ARM callbacks distinguish faults before delivery, faults after delivery, successful single-use commits and fresh-snapshot recovery. Existing over-capacity rejection remains retryable. |
| Length-unit mismatch | Java adapters use the shared editor's UTF-16 limit mode. SKT TextBox/XTextField and both KTF keypad routes preserve a full `AB🙂` value at four units and allow deletion/reinsertion. Shared tests cover whole-character truncation, caret/cycling, BMP text and unlimited fields; the rune-count API remains available. |

Regression files:

- `internal/platform/skt/host_text_input_regression_test.go`
- `internal/platform/ktf/host_text_input_regression_test.go`
- `internal/platform/lgt/host_text_input_regression_test.go`
- `internal/textinput/textinput_test.go`

Run the focused regression checks with:

```sh
go test ./internal/platform/skt ./internal/platform/ktf ./internal/platform/lgt \
  ./internal/textinput -run 'Test(SKT|KTF|LGT|KnownCEncoding|UTF16)' -count=1
```

Validation passed on the corrected tree: `make test` (including 227 Node tests),
`make test-debug`, `go test -race ./internal/...`, `go vet ./...`, and
`make server server-release`. The seven dialog scenarios passed again in both
Chromium and WebKit, for 14 checks against real DOM controls and the controlled
session adapter described above. Full Go gates also exercise the existing
WebSocket integration tests. Logs are retained under the ignored
`var/diagnostics/text-input-fixes-20260923/` directory.

The fixes do not add new editor contracts or change strict EUC-KR/BMP-only
boundaries. Physical IME behavior and additional real-game editor routes still
need the manual evidence described above.
