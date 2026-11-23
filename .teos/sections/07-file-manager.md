# Section 07 — File Manager

Owns: storage-provider abstraction, providers (local/S3/SFTP), transfers,
bookmarks. Tables `file_bookmarks`, `file_transfers`.

## Provider abstraction

```go
type StorageProvider interface {
    List(ctx context.Context, path string) ([]FileEntry, error)
    Stat(ctx context.Context, path string) (FileMeta, error)
    Read(ctx context.Context, path string) (io.ReadCloser, error)
    Write(ctx context.Context, path string, r io.Reader) error
    Mkdir(ctx context.Context, path string) error
    Rename(ctx context.Context, from, to string) error
    Delete(ctx context.Context, path string, recursive bool) error
}
```

Providers at v1: `local` (workspace roots — server-validated against
traversal/symlink escapes), `s3` (configurable endpoint — SeaweedFS-class
compatible), `sftp` (rides §08 SSH profiles). WebDAV/SCP post-1.0.

## SFTP protocol endpoint (ADR-006 — retires SFTPGo)

Terminu serves **SFTP as a server**, exposing the same providers so
native clients (`sftp` CLI, Cyberduck-class tools, rsync-style flows)
get protocol access — not just the web UI:

- `gliderlabs/ssh` + `pkg/sftp` server on `TU_SFTP_PORT`; host keys from
  the secret store; auth = §09 users (password or public key).
- Every protocol op maps onto `StorageProvider` — `SSH_FXP_*` →
  `List/Stat/Read/Write/Mkdir/Rename/Delete`. A client sees the same
  provider tree the web UI shows.
- Mutations pass the broker + audit like any file op; the SFTP surface
  adds no second security model.
- `TU_SFTP_ENABLED=false` default — the endpoint is gate-friendly:
  start it when protocol access is needed, stop it otherwise.

## Transfers

- Chunked upload/download with resume cursors (`file_transfers` rows);
  `files.progress` WS events; queue UI with cancel/retry.
- Remote edit: open remote file → buffer → save writes via provider —
  or `edit in terminal` which opens a session at that path (the honest
  escape hatch, no embedded editor in v1).

## Security

- Mutating ops (`write/mkdir/rename/delete`) → broker risk-gated; `delete`
  is `confirmed`. Local provider confined to declared roots — never
  arbitrary host FS without an explicit root declaration.

## Bookmarks

- Per-provider saved paths; `/api/files/bookmarks` CRUD.

## Acceptance

- All three providers list/read/write via one UI; an upload resumes after
  a simulated drop; a traversal attempt (`../..`) is rejected server-side;
  delete asks confirmation and writes an audit row.
