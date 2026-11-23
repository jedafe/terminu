# Skill — Screen Adapter Pattern

Canonical recipe for a new `Adapter` (§06). Follow this order — it
bakes in the honesty contract.

## 1. Capture first, code second

Record real `.cast` transcripts of the tool hitting every state you intend
to detect (permission/option/confirm/freetext + busy/negative screens)
into `fixtures/<tool>/<version>/`. **No adapter merges without fixtures**
(Rule 7) — patterns are hypotheses until a transcript proves them.

## 2. Implement the interface

```go
type XAdapter struct{}

func (XAdapter) MatchesProcess(proc string, argv []string) bool { /* binding */ }

func (XAdapter) Detect(screen *Screen, ctx *SessionCtx) *StateMatch {
    // read bottom-K rows + above-cursor region
    // match prompt shape → &StateMatch{State, Confidence,
    //   Prompt: &Prompt{Kind, Title, Options}, Region, Evidence}
    // UNCERTAIN → nil  (false-negative > false-positive)
}

func (XAdapter) Pin() AdapterPin { /* tool, verified versions, date */ }
```

## 3. Extraction rules

- `option.label` = what the screen actually shows; `option.inject` = the
  keystroke that option takes (char / ArrowSelect{down} / Escape).
- `evidence` = the literal line(s) that matched — debugging reskins
  depends on it.
- Scope `bottom` by default; `full` only when the TUI truly needs it
  (opencode) — declare it in the adapter.

## 4. Replay-test the fixtures

`go test ./internal/adapter/...` must assert each scenario → expected
`StateMatch`, and negative fixtures → `nil`/`working`.

## 5. Degrade honestly

Pin mismatch → `unverified` badge, still runs. Detection loss mid-prompt
→ next trigger re-evaluates; never stale buttons.
