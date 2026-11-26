# Terminu — placement runbooks

One artifact (`terminu-server`, a single Go binary embedding the web UI)
runs in three placements. Every placement manages the machine it runs on;
reaching another machine is `ssh` inside a session. See
`.teos/sections/12-deployment-operations.md` for the model and ADR-007
for scope (observe-and-inform: detection output is display-only).

Boot needs three envs: `TU_SESSION_SECRET`, `TU_ADMIN_USER`, and
`TU_ADMIN_PASS_HASH` or `TU_ADMIN_PASSWORD`. Everything else defaults
(`TU_BIND:TU_PORT`, `TU_DATA_DIR`).

## local — binary on your own machine

Run the binary where you want terminals:

```sh
terminu-server            # TU_LOCAL_TARGET_MODE=host (real FS/shell)
```

Sessions spawn as the invoking user, in `$HOME`. `local` target is the
machine's real filesystem — full detection on everything that runs.

## host — systemd under a dedicated service user

Reference deployment (shell.japa.to). A `terminu` system user owns the
unit; sessions spawn as the operator user via `sudo -n -iu <user> -- <shell> -l`:

```ini
# /etc/systemd/system/terminu.service
[Unit]
Description=terminu web terminal
After=network.target

[Service]
User=terminu
WorkingDirectory=/home/terminu
EnvironmentFile=/home/terminu/terminu.env
ExecStart=/home/terminu/bin/terminu-server
Restart=always

[Install]
WantedBy=multi-user.target
```

`terminu.env`:

```sh
TU_SESSION_SECRET=<random>
TU_ADMIN_USER=<operator>
TU_ADMIN_PASSWORD=<password>
TU_LOCAL_TARGET_MODE=host
TU_HOST_USER=<operator-user>   # sessions land here
TU_BIND=10.0.25.1              # gateway IP, not a public interface
TU_PORT=7701
```

Requires NOPASSWD sudoers for `terminu → TU_HOST_USER`. Bind to a private
interface behind a TLS terminator (Caddy), never a public one.

## server — container

```sh
docker run -d --name terminu \
  -e TU_SESSION_SECRET=<random> \
  -e TU_ADMIN_USER=<operator> \
  -e TU_ADMIN_PASSWORD=<password> \
  -p 7701:7701 -v terminu-data:/data \
  terminu/terminu
```

`TU_LOCAL_TARGET_MODE=container` (default): `local` target sees only the
container — least privilege, zero extra grants. To reach the real host
instead, either run the binary on the host (placements above — simplest
and honest) or opt into `nsenter` mode (privileged container entering
host namespaces; see §12).

## Reaching another machine

There is no remote-target machinery in v1 — a Terminu instance manages
its own machine. Two patterns cover remote reach:

- **`ssh` in a session** — open a session, `ssh user@host`. Works today;
  detection on the remote app runs generic-tier only (the foreground
  process is `ssh`, so curated adapters can't bind — the screen still
  parses `(y/n)` tokens and numbered lists).
- **Launch profile with `argv=[ssh,…]`** — save
  `argv: ["ssh", "-i", "/path/key", "user@host"]` as a profile; "new
  session → my VPS" becomes one tap. Same generic-tier caveat.

Or run Terminu on the far machine too — each placement is independent.

## Deploy gate (this repo's deployment)

Prod hosts live sessions — never restart it directly.

1. `deploy/shell/update-dev.sh` — builds + deploys to dev.shell.japa.to
   (its own checkout; never touches the prod tree).
2. Verify on `https://dev.shell.japa.to/`.
3. `deploy/shell/promote.sh` — binary-identical promote to prod. Run it
   from a dev session or an out-of-band shell: the restart ends every
   live prod PTY including the caller's.

`deploy/shell/update.sh` is the direct-to-prod hotfix path — it refuses
inside a prod session; prefer the gate.
