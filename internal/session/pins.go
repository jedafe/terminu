// Ref: .teos/sections/06 — adapter pin records. Declared pins sync at
// boot from the compiled adapters; the version probe runs lazily on
// first bind per tool (a bounded --version exec, detection infra like
// the ps scrape) and persists seen_version + status.
package session

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/db/gen"
)

// pinProbeTimeout bounds one --version scrape.
const pinProbeTimeout = 3 * time.Second

// pinStore tracks per-tool pin status: the declared constraint synced
// from Pin(), plus the last probed binary version. status map is the
// read path (agent.state /api surfaces); the table is its persistence.
type pinStore struct {
	q *gen.Queries

	mu     sync.Mutex
	status map[string]string // tool -> verified|unverified|unknown
	probed map[string]bool   // tool -> probe already attempted this run
	seen   map[string]string // tool -> last seen_version
	cstr   map[string]string // tool -> declared constraint
}

func newPinStore(q *gen.Queries) *pinStore {
	return &pinStore{q: q, status: map[string]string{},
		probed: map[string]bool{}, seen: map[string]string{}, cstr: map[string]string{}}
}

// SyncDeclared upserts every compiled adapter's pin (tool, constraint,
// verified_at) at boot and warms the status map from the last seen
// version recorded in a previous run.
func (p *pinStore) SyncDeclared(ctx context.Context, adapters []adapter.Adapter) error {
	for _, a := range adapters {
		pin := a.Pin()
		if err := p.q.UpsertAdapterPin(ctx, gen.UpsertAdapterPinParams{
			Tool: pin.Tool, PinConstraint: pin.Constraint, VerifiedAt: pin.VerifiedAt,
		}); err != nil {
			return err
		}
		p.mu.Lock()
		p.cstr[pin.Tool] = pin.Constraint
		p.mu.Unlock()
	}
	rows, err := p.q.ListAdapterPins(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	for _, row := range rows {
		p.seen[row.Tool] = row.SeenVersion
		if c := p.cstr[row.Tool]; c != "" {
			p.status[row.Tool] = adapter.PinStatus(c, row.SeenVersion)
		} else {
			p.status[row.Tool] = row.Status
		}
	}
	p.mu.Unlock()
	return nil
}

// Status reports the pin badge for a tool — unknown until a version is
// observed for its constraint.
func (p *pinStore) Status(tool string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.status[tool]; ok {
		return s
	}
	return adapter.PinUnknown
}

// Seen reports the last probed binary version ("" when never probed).
func (p *pinStore) Seen(tool string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[tool]
}

// Probe kicks the one-time-per-run version check for a bound tool —
// async by contract so the detection loop never stalls on exec.
func (p *pinStore) Probe(tool string, done func(string)) {
	p.mu.Lock()
	if p.probed[tool] {
		st := p.status[tool]
		p.mu.Unlock()
		done(st)
		return
	}
	p.probed[tool] = true
	p.mu.Unlock()
	go func() {
		ver := probeVersion(tool)
		st := adapter.PinUnknown
		if ver != "" {
			st = adapter.PinStatus(p.cstr[tool], ver)
		}
		_ = p.q.UpdatePinSeen(context.Background(), gen.UpdatePinSeenParams{
			SeenVersion: ver, Status: st, Tool: tool,
		})
		p.mu.Lock()
		p.seen[tool] = ver
		p.status[tool] = st
		p.mu.Unlock()
		done(st)
	}()
}

// versionRe reads the first X.Y(.Z) in a --version line — "v1.2.3",
// "claude 1.0.5", "aider 0.70.1" all land the same shape.
var versionRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// probeVersion runs `tool --version` with a hard timeout and extracts a
// semver-ish token. Env is scrubbed like the session spawn path —
// TU_* never reaches a child.
func probeVersion(tool string) string {
	ctx, cancel := context.WithTimeout(context.Background(), pinProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "--version")
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TU_") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		// Some tools print the version on stderr or exit non-zero.
		out, _ = exec.CommandContext(ctx, tool, "version").Output()
	}
	return versionRe.FindString(string(out))
}
