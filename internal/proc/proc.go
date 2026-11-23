// Ref: .teos/specs/adapter-design.md §2 — foreground-process check.
// The binder resolves which process owns a session's PTY foreground
// group; its (proc, argv) pair is the routing key for
// Adapter.MatchesProcess — adapters are chosen by process identity,
// never by screen guessing.
//
// Platform notes: the Linux implementation reads tcgetpgrp + procfs.
// Remote targets replace this with a `ps` scrape over the target's
// transport (§08) producing the same Info shape.
package proc

import "path/filepath"

// Info describes the process owning a PTY's foreground process group.
type Info struct {
	PID  int      // foreground process — pgrp leader when it exists
	PGID int      // foreground process group id
	Comm string   // kernel comm name (≤15 chars, may truncate)
	Argv []string // /proc cmdline, NUL-split; may be empty for kernel threads
}

// Name is the adapter-routing proc string: basename of argv[0], falling
// back to comm when argv is unavailable.
func (i Info) Name() string {
	if len(i.Argv) > 0 && i.Argv[0] != "" {
		return filepath.Base(i.Argv[0])
	}
	return i.Comm
}

// Same reports whether two Infos refer to the same process — pid+pgid
// is identity; Argv isn't part of it (and makes Info non-comparable).
func (i Info) Same(o Info) bool { return i.PID == o.PID && i.PGID == o.PGID }

// ErrNoProcess is returned when the foreground group has no live member
// (e.g. between jobs on an idle shell the pgrp resolves to the shell's
// own group — callers should treat a shell leader as "shell", not miss).
var ErrNoProcess = errString("no foreground process")

type errString string

func (e errString) Error() string { return string(e) }
