// App state: session registry + pane layout (views over sessions).
import { writable, derived, get } from 'svelte/store';
import { api, type Alert, type Session, type Workspace } from './api';
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

// §05 ack-able alert history — refreshed on `alert` events and drawer
// opens; only unacknowledged rows surface in the UI.
export const alerts = writable<Alert[]>([]);
export async function refreshAlerts() {
	alerts.set(await api.alerts().catch(() => []));
}
export async function ackAlert(id: number) {
	await api.ackAlert(id).catch(() => {});
	alerts.update((list) => list.filter((a) => a.id !== id));
}

// §05 ordering: waiting-for-input (high conf, then oldest wait), then
// working-ish, then done/error recent — mirrors the server's Agents().
// Low-confidence waits demote out of the top rank ("may be waiting"
// never needs-you — AGENTS.md).
export const orderedSessions = derived([sessions, agentStates], ([$s, $a]) => {
	const conf = (c?: string) => (c === 'high' ? 0 : c === 'medium' ? 1 : 2);
	const rank = (s: Session) => {
		const st = $a[s.id]?.state ?? s.state;
		if (st === 'waiting-for-input' && $a[s.id]?.confidence !== 'low') return 0;
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
	size?: number; // relative flex weight (default 1) — persisted in layout
}
export const panes = writable<Pane[]>([]);
export const activePane = writable<string | null>(null);
export const workspaces = writable<Workspace[]>([]);
export const activeWorkspace = writable<number>(1);
export const drawerOpen = writable<boolean>(false);
// Default 'mouse': taps/clicks drive the TUI when it holds the mouse —
// the "active" touch mode. 'select' (text-select/copy) is the opt-in.
export const touchMode = writable<'select' | 'mouse'>('mouse');

// Sticky keybar modifiers, shared by KeyBar + InputLine: arm one, the
// next keystroke from EITHER surface goes out modified, then disarms —
// that's what makes keybar-Ctrl + keyboard-c produce \x03.
export const kbdMods = writable({ ctrl: false, alt: false, shift: false });

export function disarmMods() {
	kbdMods.set({ ctrl: false, alt: false, shift: false });
}

const SHIFT_MAP: Record<string, string> = {
	'1': '!', '2': '@', '3': '#', '4': '$', '5': '%',
	'6': '^', '7': '&', '8': '*', '9': '(', '0': ')',
	'-': '_', '=': '+', '[': '{', ']': '}', ';': ':',
	"'": '"', '\\': '|', '`': '~', ',': '<', '.': '>', '/': '?'
};

const CTRL_MAP: Record<string, string> = {
	'[': '\x1b', '\\': '\x1c', ']': '\x1d', '^': '\x1e',
	_: '\x1f', '?': '\x7f', '@': '\x00'
};

// modKey encodes armed modifiers into real sequences: CSI-modified
// arrows/function keys (\x1b[1;<mod>X), Shift+Tab → backtab, control
// bytes for Ctrl+<char>, shifted chars, \x1b prefix for Alt.
export function modKey(seq: string): string {
	const m = get(kbdMods);
	if (!m.ctrl && !m.alt && !m.shift) return seq;
	const mod = 1 + (m.shift ? 1 : 0) + (m.alt ? 2 : 0) + (m.ctrl ? 4 : 0);
	let mm: RegExpMatchArray | null;
	if ((mm = seq.match(/^\x1b\[([ABCDHF])$/))) return `\x1b[1;${mod}${mm[1]}`;
	if ((mm = seq.match(/^\x1b\[(\d+)~$/))) return `\x1b[${mm[1]};${mod}~`;
	if (seq === '\t') return m.shift ? '\x1b[Z' : seq;
	if (seq.length === 1) {
		let c = seq;
		if (m.shift) c = (c >= 'a' && c <= 'z') ? c.toUpperCase() : (SHIFT_MAP[c] ?? c);
		if (m.ctrl) {
			const lc = c.toLowerCase();
			c = lc >= 'a' && lc <= 'z' ? String.fromCharCode(lc.charCodeAt(0) - 96) : (CTRL_MAP[c] ?? c);
		}
		return (m.alt ? '\x1b' : '') + c;
	}
	return m.alt ? '\x1b' + seq : seq;
}

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

// movePane reorders a pane to a new index; swapPanes exchanges two
// positions in place — both drive the grid order and persist via
// the caller's saveWorkspace.
export function movePane(id: string, toIndex: number) {
	panes.update((ps) => {
		const from = ps.findIndex((p) => p.id === id);
		if (from < 0) return ps;
		const next = [...ps];
		const [p] = next.splice(from, 1);
		next.splice(Math.max(0, Math.min(toIndex, next.length)), 0, p);
		return next;
	});
}

export function swapPanes(a: string, b: string) {
	panes.update((ps) => {
		const ia = ps.findIndex((p) => p.id === a);
		const ib = ps.findIndex((p) => p.id === b);
		if (ia < 0 || ib < 0) return ps;
		const next = [...ps];
		[next[ia], next[ib]] = [next[ib], next[ia]];
		return next;
	});
}

// sizePair sets two adjacent panes' sizes so they sum to their current
// total — divider drags redistribute, never grow the strip.
export function sizePair(a: string, b: string, sizeA: number) {
	panes.update((ps) => {
		const ia = ps.findIndex((p) => p.id === a);
		const ib = ps.findIndex((p) => p.id === b);
		if (ia < 0 || ib < 0 || ib !== ia + 1) return ps;
		const total = (ps[ia].size ?? 1) + (ps[ib].size ?? 1);
		const min = total * 0.15;
		const a2 = Math.min(total - min, Math.max(min, sizeA));
		const next = [...ps];
		next[ia] = { ...next[ia], size: a2 };
		next[ib] = { ...next[ib], size: total - a2 };
		return next;
	});
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
		const seen = new Set<string>();
		const restored = (layout.panes ?? []).filter(
			(p) => live.has(p.sessionId) && !seen.has(p.id) && (seen.add(p.id), true)
		);
		if (restored.length) {
			panes.set(restored);
			// Seed past the max restored suffix — restored ids keep their
			// saved names (p4, p9…) so counting entries undershoots and
			// the next newPane re-mints a live id (each_key_duplicate).
			paneSeq = restored.reduce((m, p) => Math.max(m, parseInt(p.id.slice(1), 10) || 0), 0);
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
	subscribeEvents(
		(e) => {
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
		if (e.type === 'alert') {
			void refreshAlerts();
			return; // alert frames don't change the session rows
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
	},
		// The socket has no replay channel — a drop silently misses
		// transitions, so every (re)open is a full catch-up point.
		() => {
			void refreshSessions().catch(() => {});
			void refreshAgents().catch(() => {});
			void refreshAlerts().catch(() => {});
		}
	);
}
