//go:build linux

package proc

import (
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestForegroundDirectChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer func() { _ = ptmx.Close(); _ = cmd.Process.Kill() }()

	info, err := Foreground(ptmx)
	if err != nil {
		t.Fatalf("foreground: %v", err)
	}
	if info.PID != cmd.Process.Pid {
		t.Fatalf("pid = %d want %d", info.PID, cmd.Process.Pid)
	}
	if info.Comm != "sleep" {
		t.Fatalf("comm = %q", info.Comm)
	}
	if info.Name() != "sleep" || len(info.Argv) != 2 || info.Argv[1] != "30" {
		t.Fatalf("argv = %v name = %q", info.Argv, info.Name())
	}
}

func TestForegroundJobChange(t *testing.T) {
	// An interactive bash on a PTY: fg pgrp is the shell, then becomes
	// the job — the binder must report each transition, which is what
	// the gate's process-change trigger watches.
	cmd := exec.Command("bash", "--norc", "-i")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer func() { _ = ptmx.Close(); _ = cmd.Process.Kill() }()

	var info Info
	waitFor(t, func() bool {
		info, err = Foreground(ptmx)
		return err == nil && info.Comm == "bash"
	})
	if info.Name() != "bash" {
		t.Fatalf("shell name = %q", info.Name())
	}

	if _, err := ptmx.WriteString("sleep 30\n"); err != nil {
		t.Fatalf("pty write: %v", err)
	}
	waitFor(t, func() bool {
		info, err = Foreground(ptmx)
		return err == nil && info.Comm == "sleep"
	})
	if info.PGID == info.PID && info.Name() != "sleep" {
		t.Fatalf("fg job resolution = %+v", info)
	}
}

func TestForegroundPIDMatches(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer func() { _ = ptmx.Close(); _ = cmd.Process.Kill() }()

	a, err := Foreground(ptmx)
	if err != nil {
		t.Fatalf("foreground: %v", err)
	}
	b, err := ForegroundPID(a.PGID)
	if err != nil {
		t.Fatalf("foregroundPID: %v", err)
	}
	if a.PID != b.PID || a.Comm != b.Comm {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 1s")
}
