// App state: session registry + pane layout (views over sessions).
import { writable, derived, get } from 'svelte/store';
import { api, type Session, type Workspace } from './api';
import { subscribeEvents } from './ws';

export const sessions = writable<Session[]>([]);
export const me = writable<{ username: string; role: string } | null>(null);

// Detection state per session id — fed by /api/agents + agent.state
// events. Ref: .teos/sections/05-agent-session-management.md
export interface AgentInfo {
	state: string;
	kind?: string;
	confidence?: string;
	process?: string;
	tool?: string;
	since?: string;
}
export const agentStates = writable<Record<string, AgentInfo>>({});

// §05 ordering: waiting-for-input (high conf, then oldest wait), then
// working-ish, then done/error recent — mirrors the server's Agents().
export const orderedSessions = derived([sessions, agentStates], ([$s, $a]) => {
	const conf = (c?: string) => (c === 'high' ? 0 : c === 'medium' ? 1 : 2);
	const rank = (s: Session) => {
		const st = $a[s.id]?.state ?? s.state;
		if (st === 'waiting-for-input') return 0;
		if (st === 'done' || st === 'error' || st === 'ended') return 2;
		return 1;
	};
	return [...$s].sort((x, y) => {
		const rx = rank(x);
		const ry = rank(y);
		if (rx !== ry) return rx - ry;
		const ax = $a[x.id];
		const ay = $a[y.id];
		if (rx === 0) {
			const c = conf(ax?.confidence) - conf(ay?.confidence);
			if (c !== 0) return c;
			return (ax?.since ?? '').localeCompare(ay?.since ?? ''); // oldest first
		}
		return (ay?.since ?? '').localeCompare(ax?.since ?? ''); // recent first
	});
});

export async function refreshAgents() {
	const rows = await api.agents();
	const m: Record<string, AgentInfo> = {};
	for (const r of rows) {
		m[r.session.id] = {
			state: r.state, kind: r.kind, confidence: r.confidence,
			process: r.process, tool: r.tool, since: r.since
		};
	}
	agentStates.set(m);
}

// Pane layout — each pane is a view onto one session id.
export interface Pane {
	id: string; // pane id
	sessionId: string;
}
export const panes = writable<Pane[]>([]);
export const activePane = writable<string | null>(null);
export const workspaces = writable<Workspace[]>([]);
export const activeWorkspace = writable<number>(1);
export const drawerOpen = writable<boolean>(false);
export const touchMode = writable<'select' | 'mouse'>('select');

let paneSeq = 0;
export function newPane(sessionId: string) {
	const p: Pane = { id: `p${++paneSeq}`, sessionId };
	panes.update((ps) => [...ps, p]);
	activePane.set(p.id);
	return p;
}

export function closePane(id: string) {
	panes.update((ps) => ps.filter((p) => p.id !== id));
	const ps = get(panes);
	if (get(activePane) === id) activePane.set(ps[ps.length - 1]?.id ?? null);
}

export async function refreshSessions() {
	sessions.set(await api.sessions());
}

export async function loadWorkspaces() {
	workspaces.set(await api.workspaces());
}

// Persist layout → workspaces row (debounced by caller).
export async function saveWorkspace() {
	const layout = JSON.stringify({
		panes: get(panes),
		active: get(activePane)
	});
	await api.putWorkspace(get(activeWorkspace), {
		name: 'default',
		layout,
		last_open_session: get(panes)[0]?.sessionId ?? ''
	});
}

export async function restoreWorkspace() {
	const list = await api.workspaces();
	workspaces.set(list);
	const ws = list.find((w) => w.id === get(activeWorkspace));
	if (!ws) return;
	try {
		const layout = JSON.parse(ws.layout || '{}') as { panes?: Pane[]; active?: string };
		const live = new Set(get(sessions).map((s) => s.id));
		const restored = (layout.panes ?? []).filter((p) => live.has(p.sessionId));
		if (restored.length) {
			panes.set(restored);
			paneSeq = restored.length;
			activePane.set(layout.active ?? restored[0].id);
		}
	} catch {
		/* corrupt layout — start fresh */
	}
}

let eventsWired = false;
export function wireEvents() {
	if (eventsWired) return;
	eventsWired = true;
	subscribeEvents((e) => {
		if (e.type === 'agent.state' && e.session_id) {
			const ev = e as {
				session_id: string;
				state?: string;
				kind?: string;
				confidence?: string;
				process?: string;
			};
			agentStates.update((m) => {
				const prev = m[ev.session_id] ?? {};
				const changed = ev.state && ev.state !== prev.state;
				return {
					...m,
					[ev.session_id]: {
						...prev,
						state: ev.state ?? prev.state ?? '',
						kind: ev.kind,
						confidence: ev.confidence,
						process: ev.process,
						// fresh transition with no server since → now, not oldest
						since: changed ? new Date().toISOString() : prev.since
					}
				};
			});
			return; // state change alone doesn't need a session refetch
		}
		void refreshSessions().then(() => {
			// Prune dead ids + re-sync ordering fields (agent.state events
			// can drop under load — the agents view is the source of truth).
			const live = new Set(get(sessions).map((s) => s.id));
			agentStates.update((m) => {
				const out: Record<string, AgentInfo> = {};
				for (const [k, v] of Object.entries(m)) if (live.has(k)) out[k] = v;
				return out;
			});
			void refreshAgents().catch(() => {});
		});
	});
}
