//go:build linux

package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Foreground resolves the process group owning the terminal behind ptmx.
// The pgrp leader is preferred; if it has exited (leaderless group), the
// lowest-numbered live member is used — the fg job outlives its leader
// in patterns like `sh -c 'cmd &'`.
func Foreground(ptmx *os.File) (Info, error) {
	pgid, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return Info{}, fmt.Errorf("tcgetpgrp: %w", err)
	}
	return lookup(pgid)
}

// ForegroundPID resolves a known pgid — gate ticks may cache the pgid
// and re-resolve the member without another ioctl.
func ForegroundPID(pgid int) (Info, error) { return lookup(pgid) }

func lookup(pgid int) (Info, error) {
	if info, err := readProc(pgid); err == nil {
		info.PGID = pgid
		return info, nil
	}
	// Leader gone: scan for a live member of the pgrp.
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Info{}, err
	}
	best := -1
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		if statPgid(string(stat)) == pgid && (best < 0 || pid < best) {
			best = pid
		}
	}
	if best < 0 {
		return Info{}, ErrNoProcess
	}
	info, err := readProc(best)
	if err != nil {
		return Info{}, err
	}
	info.PGID = pgid
	return info, nil
}

func readProc(pid int) (Info, error) {
	base := "/proc/" + strconv.Itoa(pid)
	comm, err := os.ReadFile(base + "/comm")
	if err != nil {
		return Info{}, err
	}
	info := Info{PID: pid, Comm: strings.TrimSpace(string(comm))}
	if raw, err := os.ReadFile(base + "/cmdline"); err == nil {
		fields := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if len(fields) == 1 && fields[0] == "" {
			fields = nil
		}
		info.Argv = fields
	}
	return info, nil
}

// statPgid extracts the pgrp field from /proc/<pid>/stat without choking
// on comm values containing spaces or parens — everything after the last
// ')' is field 3 onward; pgrp is field 5 (index 2 in the tail).
func statPgid(stat string) int {
	i := strings.LastIndex(stat, ")")
	if i < 0 {
		return -1
	}
	fields := strings.Fields(stat[i+1:])
	if len(fields) < 3 {
		return -1
	}
	pgid, _ := strconv.Atoi(fields[2])
	return pgid
}
