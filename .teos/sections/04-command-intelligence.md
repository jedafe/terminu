# Section 04 — Command Intelligence

Owns: command registry, manifest pipeline, discovery, palette, context
actions. Table `command_manifests`. Differentiator — brief §5.

## Registry

- Sources (priority): `builtin` (curated `registry/` YAML) > `custom`
  (user-authored) > `discovered` (auto-generated). ID collision → higher
  source wins; lower marked shadowed.
- Manifest schema: `conventions/adapter-manifest-format.md` — executable +
  argv templates only.

## Coverage at v1

- AI CLIs: devin, claude, codex, gemini, opencode, aider.
- Developer core: git, docker, go, node/bun, python, postgres, aws,
  kubectl, system (top ~20 commands each — curated, not exhaustive).
- Discovery: `--help`/completion parsing on targets → provisional
  `discovered` manifests; shown badged as such.

## Palette

- `GET /api/commands?q=` — fuzzy search <100 ms (budget); ranked by
  context match, then frequency.
- Command palette UI (⌘K/Ctrl-K) over the same endpoint.

## Context actions

- `GET /api/commands/context/{session_id}` — commands whose `context`
  matches the session's detected process/files. Rides §06 detection —
  a session running `docker` surfaces docker commands.

## Execution

- `POST /api/commands/{id}/execute` — resolves params → structured argv →
  **broker** (ADR-004). `risk` drives confirmation. Never builds a shell
  string (Rule 4).
- Parameter `type: resource` resolves via target providers (container
  list, file picker).

## Audit

- Every execution → `audit_log` (actor, manifest id, argv, target,
  result).

## Acceptance

- Palette returns ranked commands <100 ms; context endpoint reflects the
  session's live process; a `controlled` manifest requires confirmation
  and writes an audit row; a discovered manifest renders the provisional
  badge.
