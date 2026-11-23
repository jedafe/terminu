# Section 06 — Screen Interpreter & Adapters

Owns: headless screen model, trigger gate, `Adapter` interface + per-tool
impls, generic fallback tiers, fixture pipeline. Tables `adapter_pins`,
`fixtures`. Implements ADR-001; full spec in `specs/adapter-design.md`.

## Screen model

- One headless VT emulator per session: cell grid + attrs + cursor +
  bounded scrollback + dirty-region tracking.
- Default scan scope: bottom `TU_SCAN_BOTTOM_ROWS` rows + above-cursor
  region; `scan_scope: full` declared per adapter (opencode).

## Trigger gate — the only authorized detection times

`quiescence (TU_QUIESCENCE_MS)` · `bottom-region dirty` ·
`cursor-to-input` · `on-demand (view open)` · `foreground process change`.
No per-frame scans — budget rule.

## Adapter contract (Rule 7)

```go
type Adapter interface {
    // Process binding — which CLI owns this session's foreground
    MatchesProcess(proc string, argv []string) bool
    // Inspect the screen model; nil = "not confident" (never a wrong match)
    Detect(screen *Screen, ctx *SessionCtx) *StateMatch
    // Declared tool-version coverage — mismatch → "unverified" badge
    Pin() AdapterPin
}
```

- Pure functions; `nil` on uncertainty; `Evidence` mandatory; `Pin()`
  declares verified versions → mismatch = `unverified` badge, still runs.
- Launch coverage: devin, claude, codex, gemini, opencode, aider — all six
  ship at v1. Per-tool pattern hypotheses live in `specs/adapter-design.md`
  §7 — **they must be corrected against captured fixtures before use.**

## Generic fallback (unadapted processes)

G1 `(y/n)`/`[y/N]` trailing token → `confirm` · G2 numbered/lettered list
bottom region + cursor → `option` · G3 `?` + quiescence → `freetext` ·
G4 quiescence + cursor only → `waiting{unknown}` (no buttons, demoted).

## Fixture pipeline

- `fixtures/<tool>/<version>/<scenario>.cast` — real transcripts.
- Replay tests feed fixtures through the same screen model → assert
  `Detect()` output. Negative fixtures (busy screens) must return
  `nil`/`working` — a false positive is worse than a miss.
- Pin CI: re-capture on new tool releases (manual gate); mismatch files
  an issue, badge degrades.

## Acceptance

- Replay suite green across all shipped adapters; no adapter lacks
  fixtures; per-frame scans provably absent (gate-only invocations in
  tests); `unknown` waits render no buttons.
