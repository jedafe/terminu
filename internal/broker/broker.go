// Ref: .teos/sections/09-authn-authz-broker.md, .teos/adr/ADR-004-broker-only-execution-path.md
// The ONLY path for privileged work. Every op: identity → policy → audit.
// Phase 1: role-based policy + audit; confirmation tokens land with Phase 2.
package broker

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jedafe/terminu/internal/auth"
	"github.com/jedafe/terminu/internal/db/gen"
)

type Risk string

const (
	RiskInformational Risk = "informational"
	RiskControlled    Risk = "controlled"
	RiskConfirmed     Risk = "confirmed"
	RiskRestricted    Risk = "restricted"
)

// Op is one privileged request.
type Op struct {
	Actor  *auth.Principal
	Action string   // terminal.spawn, session.kill, ...
	Target string   // local | ssh:<id> | docker:<id> | sftp
	Argv   []string // structured — never interpolated into shell strings
	Risk   Risk
}

// ErrRestricted maps to the action_restricted error code.
var ErrRestricted = errors.New("action_restricted")

// Broker gates + audits every privileged action.
type Broker struct {
	q *gen.Queries
}

func New(q *gen.Queries) *Broker { return &Broker{q: q} }

// Check applies identity → policy. Execution happens in the caller so the
// op carries its real result to Audit. Phase 1 policy: viewers get no
// mutations; operators/admin spawn per TU_MAX_SESSIONS (enforced by the
// session manager).
func (b *Broker) Check(op Op) error {
	if op.Actor == nil {
		return ErrRestricted
	}
	switch op.Risk {
	case RiskInformational:
		return nil
	case RiskControlled, RiskConfirmed:
		if op.Actor.Role == "viewer" {
			return ErrRestricted
		}
		return nil
	case RiskRestricted:
		if op.Actor.Role != "admin" {
			return ErrRestricted
		}
		return nil
	}
	return nil
}

// Audit appends the op + result — write failure fails the op (no silent
// privileged actions, ADR-004).
func (b *Broker) Audit(ctx context.Context, op Op, result string) error {
	argv := ""
	for i, a := range op.Argv {
		if i > 0 {
			argv += " "
		}
		argv += a
	}
	var uid sql.NullInt64
	actor := "anonymous"
	if op.Actor != nil {
		actor = op.Actor.Username
		uid = sql.NullInt64{Int64: op.Actor.UserID, Valid: true}
	}
	return b.q.WriteAudit(ctx, gen.WriteAuditParams{
		Actor: actor, ActorID: uid,
		Action: op.Action, Target: op.Target, Argv: argv, Result: result,
	})
}

var _ = sql.ErrNoRows
