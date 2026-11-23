# SQLite Migration Order

Canonical ordering — register a new table here before writing its migration
(AGENTS.md Rule 14). Order reflects FK dependencies; a child table never
precedes its parent.

| # | Table | Owns | Section |
|---|-------|------|---------|
| 1 | `users` | accounts, roles (admin/operator/viewer) | §09 |
| 2 | `auth_sessions` | login sessions, tokens, expiry | §09 |
| 3 | `workspaces` | named workspaces, layout, last-open | §03 |
| 4 | `sessions` | terminal session records (target, cwd, shell, state, detached_at) | §03 |
| 5 | `session_state_log` | transition history (state, kind, confidence, evidence, at) | §03 (§05 consumes) |
| 6 | `command_manifests` | registry manifests (source: builtin/discovered/custom) | §04 |
| 7 | `launch_profiles` | saved agent launches (tool, flags, env, cwd, resume_cmd) | §05 |
| 8 | `adapter_pins` | per-tool verified-version records + pin status | §06 |
| 9 | `fixtures` | registered .cast transcripts + replay results | §06 |
| 10 | `audit_log` | every privileged execution (actor, action, target, argv, result, at) | §09 |
| 11 | `ssh_profiles` | saved SSH targets (host refs; secrets in store, not here) | §08 |
| 12 | `file_bookmarks` | per-provider saved paths | §07 |
| 13 | `file_transfers` | upload/download queue records + resume cursors | §07 |
| 14 | `alerts` | emitted state-change notifications (ack state) | §05 |
| 15 | `settings` | runtime-configurable knobs (TU_* defaults, UI prefs) | §12 |
| 16 | `user_public_keys` | per-user public keys for SFTP endpoint auth (ADR-006) | §09 |

Notes:

- Secrets never live in a table column — `ssh_profiles` stores a
  `secret_ref`, resolved through the encrypted secret store at connect time.
- `session_state_log` is append-only — the registry reads latest-per-session;
  "needs you" ordering derives from it.
- `audit_log` is append-only; retention policy is a settings knob, default
  keep-all.
- FK `ON DELETE` behavior: sessions → `session_state_log` cascades;
  users → `auth_sessions` cascades; `audit_log` never cascades (actor id
  is retained as text on user delete — audit integrity outlives accounts).
- **Circular-FK guard:** `sessions.workspace_id → workspaces` is the real
  FK (workspaces precede sessions). `workspaces.last_open` may reference a
  session — store it as a plain column with **no FK constraint** (JEOS
  Rule-13 pattern: never create the cycles workspaces↔sessions).
- `user_public_keys` FK → `users` cascades — keys die with the account.
