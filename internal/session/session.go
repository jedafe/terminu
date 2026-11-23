// Ref: .teos/sections/03-session-management.md, .teos/skills/patterns/pty-session-lifecycle.md
// One owned PTY: process, ring buffer, attached client set, exit watch.
package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/proc"
)

// Client is one attached consumer of PTY output. Slow clients drop on a
// full channel — they resync via attach{last_seq} (bounded-buffer contract).
type Client struct {
	C    chan []byte
	Done func()
}

// ptySession owns one PTY child until EOF — server-side persistence means
// browser disconnect never reaches the process.
type ptySession struct {
	rec   *domain.Session
	cmd   *exec.Cmd
	ptmx  *os.File
	buf   *ring
	mu    sync.Mutex
	conns map[*Client]struct{}
	done  chan struct{}
	exit  chan int
	once  sync.Once
	mgr   *Manager
}

// spawnArgv builds the child argv per local-target mode (ADR-006).
// nsenter enters the host's namespaces then execs a login shell as the
// operator user — the documented interim deployment path.
func (m *Manager) spawnArgv(t domain.Target, shell, cwd string) (string, []string, error) {
	if t.Kind != "local" {
		return "", nil, errors.New("target kind unsupported in Phase 1: " + t.Kind)
	}
	switch m.cfg.LocalTargetMode {
	case "nsenter":
		user := hostUser()
		if user == "" {
			user = "japa"
		}
		// Our PTY lives on the container's devpts; after nsenter -m the
		// session's /dev/tty resolves against the HOST devpts and job
		// control breaks ("cannot set terminal process group"). `script`
		// allocates a fresh pty inside the host namespaces and relays
		// I/O — the inner shell gets a real ctty.
		inner := "cd " + shellQuote(orDefault(cwd, "/home/"+user))
		inner += " && exec script -qfc " + shellQuote("exec "+shell+" -l") + " /dev/null"
		return "nsenter", []string{
			"-t", "1", "-m", "-u", "-i", "-n", "-p",
			"su", user, "-c", inner,
		}, nil
	case "host":
		// Running natively on the host: TU_HOST_USER names the account
		// sessions land as (e.g. the operator's dev user). The service
		// user needs NOPASSWD sudo; sudo keeps our host-devpts ctty so
		// job control stays native — no relay needed (ADR-006 option 1).
		if u := hostUser(); u != "" && u != currentUser() {
			return "sudo", []string{"-n", "-iu", u, "--", shell, "-l"}, nil
		}
		return shell, []string{"-l"}, nil
	default: // container — plain spawn as the service user
		return shell, []string{"-l"}, nil
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// hostUser is TU_HOST_USER — the account sessions should land as when the
// service itself runs under a different system user (host/nsenter modes).
func hostUser() string { return os.Getenv("TU_HOST_USER") }

// currentUser resolves the service's own account for the sudo decision.
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func shellQuote(s string) string {
	return "'" + replaceAll(s, "'", "'\\''") + "'"
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// spawn starts the PTY and its read/exit loops. The child is deliberately
// NOT tied to the caller's context — server-owned sessions outlive the
// request that created them.
func (m *Manager) spawn(ctx context.Context, rec *domain.Session) (*ptySession, error) {
	name, argv, err := m.spawnArgv(rec.Target, rec.Shell, rec.Cwd)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(name, argv...)
	if m.cfg.LocalTargetMode != "nsenter" {
		// Unset cwd defaults to the service user's home, not the daemon's
		// WorkingDirectory — shells should open where the user lives.
		// When sudo -iu crosses into another account it owns cwd+env
		// itself; setting Dir to a foreign home would fail the spawn.
		sudoing := m.cfg.LocalTargetMode == "host" && hostUser() != "" && hostUser() != currentUser()
		if !sudoing {
			if rec.Cwd == "" {
				rec.Cwd, _ = os.UserHomeDir()
			}
			cmd.Dir = rec.Cwd
		}
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(rec.Cols), Rows: uint16(rec.Rows),
	})
	if err != nil {
		return nil, err
	}
	s := &ptySession{
		rec:   rec,
		cmd:   cmd,
		ptmx:  ptmx,
		buf:   newRing(m.cfg.SessionBufferKB * 1024),
		conns: map[*Client]struct{}{},
		done:  make(chan struct{}),
		exit:  make(chan int, 1),
		mgr:   m,
	}
	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

// readLoop is the single PTY reader: bytes → ring (seq++) → fan-out.
func (s *ptySession) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			seq := s.buf.append(buf[:n])
			frame := make([]byte, 8+n)
			putU64(frame[:8], seq)
			copy(frame[8:], buf[:n])
			s.broadcast(frame)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				slog.Debug("pty read end", "session", s.rec.ID, "err", err)
			}
			return
		}
	}
}

// waitLoop turns process exit into the done|error state + eof event.
func (s *ptySession) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	s.exit <- code
	s.mgr.onExit(s, code)
	s.close()
}

// broadcast fans a frame out to every attached client. A full client
// channel drops that client (it will resync) — never blocks the reader.
func (s *ptySession) broadcast(frame []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		select {
		case c.C <- frame:
		default:
			// Client is behind; drop it so it reconnects+resyncs.
			s.dropLocked(c)
		}
	}
}

func (s *ptySession) dropLocked(c *Client) {
	delete(s.conns, c)
	close(c.C)
}

// AttachClient registers c and snapshots under one critical section —
// a frame can't fall between snapshot and subscription (dedup by seq is
// the client's contract for boundary overlaps).
func (s *ptySession) AttachClient(c *Client, lastSeq uint64) ([]chunk, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[c] = struct{}{}
	if lastSeq == 0 {
		return s.buf.snapshot()
	}
	chunks, next, ok := s.buf.replay(lastSeq)
	if !ok {
		return s.buf.snapshot()
	}
	return chunks, next
}

// Attach registers a client channel.
func (s *ptySession) Attach(c *Client) {
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
}

// Detach removes a client — the PTY keeps running.
func (s *ptySession) Detach(c *Client) {
	s.mu.Lock()
	if _, ok := s.conns[c]; ok {
		s.dropLocked(c)
	}
	n := len(s.conns)
	s.mu.Unlock()
	if n == 0 {
		s.mgr.onDetached(s.rec.ID)
	}
}

// Write is client input → PTY (broker-exempt: authorized session bytes).
func (s *ptySession) Write(p []byte) error {
	_, err := s.ptmx.Write(p)
	return err
}

// Resize propagates SIGWINCH to the PTY child.
func (s *ptySession) Resize(cols, rows int) {
	s.rec.Cols, s.rec.Rows = cols, rows
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Rec exposes the session record (read-only) for control frames.
func (s *ptySession) Rec() *domain.Session { return s.rec }

// Foreground resolves the process owning this PTY's foreground group —
// the adapter-binding routing key (spec §2). Read on gate ticks only.
func (s *ptySession) Foreground() (proc.Info, error) {
	return proc.Foreground(s.ptmx)
}

// ExitCode reports the process exit code once EOF has fired.
func (s *ptySession) ExitCode() *int {
	select {
	case c := <-s.exit:
		return &c
	default:
		return nil
	}
}

func (s *ptySession) close() {
	s.once.Do(func() {
		close(s.done)
		_ = s.ptmx.Close()
		s.mu.Lock()
		for c := range s.conns {
			s.dropLocked(c)
		}
		s.mu.Unlock()
	})
}

func putU64(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (8 * (7 - i)))
	}
}

var _ = time.Now // silence unused if time not needed later
