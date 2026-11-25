# Terminu — Agent Notes

Project rules and engineering corpus live in `.teos/` — read
`.teos/AGENTS.md` (it self-loads via `TEOS.md`) before implementing.

## Deploying — the dev→prod gate is mandatory

`shell.japa.to` hosts live operator/Devin sessions — a prod restart kills
every PTY. New builds always go through dev first:

1. `cd /home/japa/terminu/repo && bash deploy/shell/update-dev.sh`
   builds + deploys to **dev.shell.japa.to** (its own checkout —
   `/home/japa/terminu/repo` — so it never collides with prod's
   `/home/japa/terminu/terminu` working tree or bounces prod sessions).
2. Verify the change on `https://dev.shell.japa.to/`.
3. `bash deploy/shell/promote.sh` promotes the tested binary to prod —
   binary-identical, atomic swap, deferred restart. **Run it from a dev
   session or an out-of-band shell** — it refuses inside a prod session,
   since the restart kills every prod PTY including the caller's.

`deploy/shell/update.sh` remains the direct-to-prod hotfix path — it too
refuses inside a prod session and warns elsewhere; prefer the gate.
