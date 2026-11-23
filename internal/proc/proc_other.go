//go:build !linux

package proc

import (
	"errors"
	"os"
)

// Foreground is Linux-only (procfs + tcgetpgrp); other platforms return
// an error — remote targets resolve via `ps` scrape (§08) instead.
func Foreground(ptmx *os.File) (Info, error) {
	return Info{}, errors.New("proc: foreground resolution unsupported on this platform")
}

func ForegroundPID(pgid int) (Info, error) {
	return Info{}, errors.New("proc: foreground resolution unsupported on this platform")
}
