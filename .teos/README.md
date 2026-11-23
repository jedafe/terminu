# Terminu Engineering Operating System (TEOS)

**Product:** Terminu — the control room for your AI agents
**Version:** v0.3.3 (pre-implementation)
**Status:** Planning — corpus seeded from product decisions; no code yet

TEOS is the canonical source of truth for all Terminu technical decisions,
schemas, API contracts, and implementation roadmaps. Every engineering
decision is documented here first, then implemented in the Terminu repo
(`~/websites/terminu`, parent of this directory).

Modelled on JEOS (JapaConnect Engineering Operating System). Same operating
model: roadmap says WHAT and WHEN, section file says HOW, ADR says WHY.

---

## For Engineers (Human)

### First 5 minutes
1. Read `TEOS.md` — master index of everything in this corpus
2. Read `specs/product-brief-v2.md` — what Terminu is and who it's for
3. Read `roadmaps/ROADMAP-V1.md` — the build plan
4. Read the relevant `sections/` file before touching any feature

### When you need to know...
| Question | Go to |
|---|---|
| What a feature owns (schema, routes, logic) | `sections/` — find the owning section |
| What order to run migrations | `schemas/MIGRATION-ORDER.md` |
| What env vars exist | `conventions/env-vars-catalog.md` |
| What API/WS contracts exist | `conventions/api-routes-catalog.md`, `conventions/websocket-message-catalog.md` |
| What session states exist | `conventions/session-state-catalog.md` |
| What a command manifest looks like | `conventions/adapter-manifest-format.md` |
| Why a decision was made | `adr/ADR-NNN-*.md` |
| Full product rationale | `specs/product-brief-v2.md` |
| Adapter implementation detail | `specs/adapter-design.md` + `sections/06-*` |

---

## For AI Agents

**Read `AGENTS.md` before doing anything else.** It contains the rules for how
agents must use this corpus — what to load, what not to invent, and how to
reference sections.

**Quick rule:** Never implement a feature without first reading the section
file that owns it. The section file is the source of truth.

---

## Repository Map

```
terminu/
└── .teos/                     ← You are here
    ├── README.md              ← This file
    ├── TEOS.md                ← Master index (read second)
    ├── AGENTS.md              ← Agent operating rules
    ├── CLAUDE.md              ← Pointer to AGENTS.md
    ├── RULES.md               ← Hard coding/operational rules
    ├── CHANGELOG.md           ← TEOS version history
    │
    ├── .ai/                   ← Machine-readable project config
    │   ├── project.yaml       ← Product metadata
    │   ├── stack.yaml         ← Tech stack with pinned versions
    │   ├── architecture.yaml  ← Principles, layers, package map
    │   ├── glossary.yaml      ← Terminu-specific terms
    │   └── context/           ← Live phase/task tracker
    │
    ├── roadmaps/
    │   └── ROADMAP-V1.md      ← Phase 0–4 task plan + post-1.0 program
    │
    ├── sections/              ← Feature spec files — SOURCE OF TRUTH
    │   └── NN-feature-name.md
    │
    ├── adr/                   ← Architecture Decision Records
    │   └── ADR-NNN-*.md
    │
    ├── conventions/           ← Cross-cutting contracts and catalogs
    ├── schemas/
    │   └── MIGRATION-ORDER.md ← Ordered SQLite migration guide
    ├── skills/                ← Reusable implementation patterns
    ├── specs/                 ← Product brief + deep design specs
    ├── audit/                 ← Corpus/code audit reports
    ├── plans/                 ← Dated implementation plans
    ├── ideas/                 ← Uncommitted product ideas
    ├── documentations/        ← Post-implementation docs (per phase)
    └── diagrams/              ← Architecture diagrams (Mermaid)
```

---

## Product Overview

Terminu is a self-hosted web platform for managing terminals, CLI
applications, and AI coding agents — a management layer on top of real
terminal sessions. The wedge: it watches your agents and tells you when they
need you, and it's built touch-first.

**One server, one web UI:** Go backend owning PTYs server-side (ADR-005);
SvelteKit frontend; SQLite; SSH for remote targets; single container.

See `specs/product-brief-v2.md` for the full positioning, scope, and the
deferred list.
