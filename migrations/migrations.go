// Ref: .teos/conventions/sqlite-migration-naming.md
// Embedded migration files — the FS the db runner reads.
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
