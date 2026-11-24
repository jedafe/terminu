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
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/gate"
	"github.com/jedafe/terminu/internal/proc"
	"github.com/jedafe/terminu/internal/screen"
)

// Client is one attached consumer of PTY output. Slow clients drop on a
// full channel — they resync via attach{last_seq} (bounded-buffer contract).
type Client struct {
	C chan []byte
}

// killGrace is how long a deliberate terminate lets the TERM'd child
// clean up (shell history, aider state) before escalating to SIGKILL —
// §03 terminate semantics: TERM → grace → KILL.
const killGrace = 3 * time.Second

// ptySession owns one PTY child until EOF — server-side persistence means
// browser disconnect never reaches the process.
type ptySession struct {
	rec      *domain.Session
	cmd      *exec.Cmd
	ptmx     *os.File
	buf      *ring
	mu       sync.Mutex
	conns    map[*Client]struct{}
	done     chan struct{} // closed once the session is fully torn down
	readDone chan struct{} // readLoop drained + persisted; close waits on it
	once     sync.Once
	mgr      *Manager

	exitCode int  // set by waitLoop before done closes — read by all clients
	exited   bool // true once cmd.Wait() has returned

	killed bool           // deliberate terminate → onExit lands `ended`, not `error`
	scr    *screen.Screen // §06 headless model — fed in readLoop
	gt     *gate.Gate     // trigger gate — emits scan events on s.done

	stateUp chan StateUpdate // detection transitions → session-WS frames
}

// StateUpdate is one deduped detection transition for the session socket:
// State always set; Match is the detection result (nil for a bare
// working/undetectable transition or process exit).
// Ref: .teos/conventions/websocket-message-catalog.md (state/prompt/prompt_clear)
type StateUpdate struct {
	State domain.SessionState
	Match *adapter.StateMatch
	At    time.Time // when this state began — wait-age ordering per §05
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
		user := orDefault(m.cfg.HostUser, "japa")
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
		if u := m.cfg.HostUser; u != "" && u != currentUser() {
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
		sudoing := m.cfg.LocalTargetMode == "host" && m.cfg.HostUser != "" && m.cfg.HostUser != currentUser()
		if !sudoing {
			if rec.Cwd == "" {
				rec.Cwd, _ = os.UserHomeDir()
			}
			cmd.Dir = rec.Cwd
		}
	}
	// Child env: daemon config stays daemon-side — TU_* vars are our
	// secrets (admin password, session secret, LLM key); spawning with
	// the full environ would expose them via `env` in every session.
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TU_") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(rec.Cols), Rows: uint16(rec.Rows),
	})
	if err != nil {
		return nil, err
	}
	s := &ptySession{
		rec:      rec,
		cmd:      cmd,
		ptmx:     ptmx,
		buf:      newRing(m.cfg.SessionBufferKB * 1024),
		conns:    map[*Client]struct{}{},
		done:     make(chan struct{}),
		readDone: make(chan struct{}),
		stateUp:  make(chan StateUpdate, 8),
		mgr:      m,
		scr:      screen.New(rec.Cols, rec.Rows, m.cfg.ScreenScrollback),
		gt:       gate.New(time.Duration(m.cfg.QuiescenceMS)*time.Millisecond, m.cfg.ScanBottomRows),
	}
	return s, nil
}

// start launches the session goroutines. Spawn calls it AFTER the live-map
// insert — if waitLoop ran before the insert, a fast-exiting process would
// delete a not-yet-present key and Spawn would then insert a zombie.
func (s *ptySession) start() {
	go s.readLoop()
	go s.waitLoop()
	go s.gt.Run(s.done, s)
}

// readLoop is the single PTY reader: bytes → ring (seq++) → fan-out →
// screen model + gate notify (the §06 detection feed; parsing is not
// detection — adapters only run on gate emits). On exit it persists the
// ring's tail so finished rows keep their output (§03 scrollback).
func (s *ptySession) readLoop() {
	defer close(s.readDone)
	defer s.persistScrollback()
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			seq := s.buf.append(buf[:n])
			frame := make([]byte, 8+n)
			putU64(frame[:8], seq)
			copy(frame[8:], buf[:n])
			s.broadcast(frame)
			_, _ = s.scr.Write(buf[:n])
			s.gt.NotifyFrame()
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
// Order matters: the exit code is stored BEFORE close() drops clients so
// every attached socket (split panes included) can read it for eof —
// a consumable channel would hand the code to only the first reader.
// close() also runs before onExit so the registry's done-check in
// bindingFor makes re-binding impossible — a queued gate event consumed
// in that window can't resurrect the bound entry unwatch clears.
func (s *ptySession) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	s.mu.Lock()
	s.exited, s.exitCode = true, code
	s.mu.Unlock()
	s.close()
	s.mgr.onExit(s, code)
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
	if s.rec.DetachedAt != nil {
		// Someone's watching again — the detached overlay flag clears.
		s.rec.DetachedAt = nil
		go s.mgr.clearDetachedDB(s.rec.ID)
	}
	s.conns[c] = struct{}{}
	s.gt.Request() // a view opening is an authorized on-demand scan
	if lastSeq == 0 {
		return s.buf.snapshot()
	}
	chunks, next, ok := s.buf.replay(lastSeq)
	if !ok {
		return s.buf.snapshot()
	}
	return chunks, next
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
		s.mgr.onDetached(s)
	}
}

// Write is client input → PTY (broker-exempt: authorized session bytes).
func (s *ptySession) Write(p []byte) error {
	_, err := s.ptmx.Write(p)
	return err
}

// Resize propagates SIGWINCH to the PTY child and the screen model.
func (s *ptySession) Resize(cols, rows int) {
	s.mu.Lock()
	s.rec.Cols, s.rec.Rows = cols, rows
	s.mu.Unlock()
	s.scr.Resize(cols, rows)
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	// Layout changed → the prompt may sit outside the old scan scope.
	// A resize is view-driven → authorized on-demand scan.
	s.gt.Request()
}

// Rec returns a copy of the session record — the struct mutates under
// s.mu (state/cols/rows/exit), so callers never see it mid-write.
func (s *ptySession) Rec() *domain.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *s.rec
	return &c
}

func (s *ptySession) setRecState(st domain.SessionState) {
	s.mu.Lock()
	s.rec.State = st
	s.mu.Unlock()
}

func (s *ptySession) setRecDone(st domain.SessionState, code int) {
	s.mu.Lock()
	s.rec.State = st
	s.rec.ExitCode = &code
	s.mu.Unlock()
}

// Foreground resolves the process owning this PTY's foreground group —
// the adapter-binding routing key (spec §2). Read on gate ticks only.
func (s *ptySession) Foreground() (proc.Info, error) {
	return proc.Foreground(s.ptmx)
}

// --- gate.Source implementation (read on gate ticks only) ---

// BottomText returns the bottom `rows` rows of the screen model.
func (s *ptySession) BottomText(rows int) string {
	_, h := s.scr.Size()
	y0 := h - rows
	if y0 < 0 {
		y0 = 0
	}
	return s.scr.Text(y0, h)
}

// Cursor returns the model cursor for the cursor-to-input trigger.
func (s *ptySession) Cursor() (x, y int, visible bool) { return s.scr.Cursor() }

// Rows returns the model height.
func (s *ptySession) Rows() int { _, r := s.scr.Size(); return r }

// Screen exposes the headless screen model for detection reads.
func (s *ptySession) Screen() *screen.Screen { return s.scr }

// GateEvents exposes the trigger-gate channel for the adapter runner.
func (s *ptySession) GateEvents() <-chan gate.Event { return s.gt.Events }

// StateUpdates is the session-WS feed of deduped detection transitions.
func (s *ptySession) StateUpdates() <-chan StateUpdate { return s.stateUp }

// CurrentState is the attach-time resync value for the state/prompt
// frames: the registry's last transition, else the record's state.
func (s *ptySession) CurrentState() StateUpdate {
	if u := s.mgr.reg.current(s.rec.ID); u.State != "" {
		return u
	}
	return StateUpdate{State: s.rec.State}
}

// notifyState pushes a transition to attached session sockets — never
// blocks (an overflow drops to the next transition, client resyncs on
// reattach anyway via the `state` frame cadence).
func (s *ptySession) notifyState(u StateUpdate) {
	select {
	case s.stateUp <- u:
	default:
	}
}

// ExitCode reports the process exit code once EOF has fired — readable
// by every attached client, not a consumable (splits all get eof).
func (s *ptySession) ExitCode() *int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exited {
		return nil
	}
	c := s.exitCode
	return &c
}

// terminate runs the §03 shutdown: SIGTERM, a grace window for the child
// to save state, then SIGKILL via the timer goroutine.
func (s *ptySession) terminate() {
	s.mu.Lock()
	s.killed = true
	s.mu.Unlock()
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Already gone — a hard kill is the honest fallback path.
		_ = s.cmd.Process.Kill()
		return
	}
	go func() {
		select {
		case <-s.done: // exited within the grace window
		case <-time.After(killGrace):
			_ = s.cmd.Process.Kill()
		}
	}()
}

// setDetachedAt stamps when the session lost its last client — the
// `detached` overlay flag derives from this aging past the reconnect
// window (state catalog: overlay, not a process state).
func (s *ptySession) setDetachedAt(t time.Time) {
	s.mu.Lock()
	s.rec.DetachedAt = &t
	s.mu.Unlock()
}

// persistScrollback writes the retained output tail to
// {data_dir}/scrollback/{id} — the read-only replay source for finished
// rows (GET scrollback + dead-session WS attach). Runs at read end so it
// captures the most drained bytes; the file lives as long as the row.
func (s *ptySession) persistScrollback() {
	if s.mgr == nil {
		return // sessions built outside a Manager (tests) persist nothing
	}
	chunks, _ := s.buf.snapshot()
	if len(chunks) == 0 {
		return
	}
	path := s.mgr.scrollbackPath(s.rec.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.Warn("scrollback persist mkdir", "session", s.rec.ID, "err", err)
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		slog.Warn("scrollback persist open", "session", s.rec.ID, "err", err)
		return
	}
	defer func() { _ = f.Close() }()
	for _, c := range chunks {
		if _, err := f.Write(c.data); err != nil {
			slog.Warn("scrollback persist write", "session", s.rec.ID, "err", err)
			return
		}
	}
}

func (s *ptySession) close() {
	s.once.Do(func() {
		close(s.done)
		// Drain first: ptmx.Close would discard bytes the dead child
		// already wrote — closing before readLoop finishes empties the
		// ring (and the persisted scrollback) for fast exits. Bound the
		// wait for a grandchild still holding the slave open.
		select {
		case <-s.readDone:
		case <-time.After(time.Second):
		}
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
