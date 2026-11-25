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

// Pane layout — each pane is a view onto one session id. The pool is
// every open tab; the grid is a fixed preset of slots that panes are
// assigned into. Slot weights (cell.size = width share, row.size =
// height share) belong to the slot, not the pane — swap a tab in and
// the window keeps its proportions.
export interface Pane {
	id: string; // pane id
	sessionId: string;
}
export interface GridCell {
	paneId?: string;
	size?: number;
}
export interface GridRow {
	cells: GridCell[];
	size?: number;
}
export const panes = writable<Pane[]>([]);
export const grid = writable<GridRow[]>([{ cells: [{}], size: 1 }]);
// Which panes currently occupy a slot — drives the tab-strip marker.
export const onGrid = derived(grid, (g) => {
	const s = new Set<string>();
	g.forEach((row) => row.cells.forEach((c) => c.paneId && s.add(c.paneId)));
	return s;
});
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
	// Fill the first empty grid slot, if any — pool order decides which
	// waiting tab lands there.
	grid.update((rows) => {
		const next = [...rows];
		for (let r = 0; r < next.length; r++)
			for (let c = 0; c < next[r].cells.length; c++)
				if (!next[r].cells[c].paneId) {
					setCell(next, r, c, p.id);
					return next;
				}
		return next;
	});
	activePane.set(p.id);
	return p;
}

export function closePane(id: string) {
	panes.update((ps) => ps.filter((p) => p.id !== id));
	grid.update((rows) => {
		const next = [...rows];
		for (let r = 0; r < next.length; r++)
			for (let c = 0; c < next[r].cells.length; c++)
				if (next[r].cells[c].paneId === id) setCell(next, r, c, undefined);
		return next;
	});
	const ps = get(panes);
	if (get(activePane) === id) activePane.set(ps[ps.length - 1]?.id ?? null);
}

// movePane reorders a pane's pool position (tab order); grid slots keep
// their occupants — assignment changes go through assignCell.
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

// swapPanes exchanges two panes' positions — pool order always swaps;
// when both are on the grid their slots swap occupants too, so a
// strip-level swap of two visible panes exchanges their windows.
export function swapPanes(a: string, b: string) {
	panes.update((ps) => {
		const ia = ps.findIndex((p) => p.id === a);
		const ib = ps.findIndex((p) => p.id === b);
		if (ia < 0 || ib < 0) return ps;
		const next = [...ps];
		[next[ia], next[ib]] = [next[ib], next[ia]];
		return next;
	});
	grid.update((rows) => {
		const pa = cellOf(rows, a);
		const pb = cellOf(rows, b);
		if (!pa || !pb) return rows;
		const next = [...rows];
		setCell(next, pa.r, pa.c, b);
		setCell(next, pb.r, pb.c, a);
		return next;
	});
}

// assignCell puts a pane into grid slot (r, c). On-grid panes swap
// slots; pool panes displace the occupant back to the pool — the
// preset's shape never changes, only who sits where.
export function assignCell(paneId: string, r: number, c: number) {
	grid.update((rows) => {
		if (!rows[r]?.cells[c]) return rows;
		const next = [...rows];
		const src = cellOf(next, paneId);
		if (src && (src.r !== r || src.c !== c)) {
			setCell(next, src.r, src.c, next[r].cells[c].paneId);
		}
		setCell(next, r, c, paneId);
		return next;
	});
}

// cellPos returns the "r:c" slot key a pane occupies, or null when pool-only.
export function cellPos(rows: GridRow[], paneId: string): string | null {
	const at = cellOf(rows, paneId);
	return at ? `${at.r}:${at.c}` : null;
}

// setCell writes a slot's occupant immutably — new row + cell objects,
// since in-place mutation leaves Svelte's each/{@const} with stale
// identities (the store changes but the cell body never re-renders).
function setCell(rows: GridRow[], r: number, c: number, paneId: string | undefined) {
	rows[r] = {
		...rows[r],
		cells: rows[r].cells.map((cell, i) => (i === c ? { ...cell, paneId } : cell))
	};
}

// cellOf locates the slot a pane occupies, or null when it's pool-only.
function cellOf(rows: GridRow[], paneId: string): { r: number; c: number } | null {
	for (let r = 0; r < rows.length; r++)
		for (let c = 0; c < rows[r].cells.length; c++)
			if (rows[r].cells[c].paneId === paneId) return { r, c };
	return null;
}

// pairClamp splits two adjacent weights at `want` for the first —
// total preserved, each side floored at 15%.
function pairClamp(total: number, want: number): [number, number] {
	const min = total * 0.15;
	const a = Math.min(total - min, Math.max(min, want));
	return [a, total - a];
}

export function sizeCellPair(r: number, c1: number, c2: number, sizeA: number) {
	grid.update((rows) => {
		const row = rows[r];
		if (!row || c2 !== c1 + 1 || !row.cells[c1] || !row.cells[c2]) return rows;
		const [a, b] = pairClamp((row.cells[c1].size ?? 1) + (row.cells[c2].size ?? 1), sizeA);
		row.cells[c1] = { ...row.cells[c1], size: a };
		row.cells[c2] = { ...row.cells[c2], size: b };
		return rows;
	});
}

export function sizeRowPair(r1: number, r2: number, sizeA: number) {
	grid.update((rows) => {
		if (r2 !== r1 + 1 || !rows[r1] || !rows[r2]) return rows;
		const [a, b] = pairClamp((rows[r1].size ?? 1) + (rows[r2].size ?? 1), sizeA);
		rows[r1] = { ...rows[r1], size: a };
		rows[r2] = { ...rows[r2], size: b };
		return rows;
	});
}

// applyPreset rebuilds the grid as rows of `shape` cell-counts (max 4
// cells). Existing slots keep their occupants row-major where they
// fit; overflow panes return to the pool; remaining empty cells are
// filled with unassigned pool panes in tab order.
export function applyPreset(shape: number[]) {
	const counts = shape.map((n) => Math.max(1, Math.min(4, n | 0))).slice(0, 4);
	if (!counts.length) return;
	grid.update((old) => {
		const carried: (string | undefined)[] = [];
		old.forEach((row) => row.cells.forEach((c) => carried.push(c.paneId)));
		const pool = get(panes)
			.map((p) => p.id)
			.filter((id) => !carried.includes(id));
		return counts.map((n) => ({
			size: 1,
			cells: Array.from({ length: n }, () => ({
				size: 1,
				paneId: carried.shift() ?? pool.shift()
			}))
		}));
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
		grid: get(grid),
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
		const layout = JSON.parse(ws.layout || '{}') as {
			panes?: (Pane & { size?: number })[];
			grid?: GridRow[];
			active?: string;
		};
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
			// Grid: validate saved slots; migrate pre-grid layouts (flat
			// pane list with `size`) into a single side-by-side row.
			const ids = new Set(restored.map((p) => p.id));
			const valid =
				Array.isArray(layout.grid) &&
				layout.grid.length > 0 &&
				layout.grid.every((r) => Array.isArray(r?.cells) && r.cells.length > 0);
			grid.set(
				valid
					? layout.grid!.map((r) => ({
							size: typeof r.size === 'number' && r.size > 0 ? r.size : 1,
							cells: r.cells.map((c) => ({
								paneId: c?.paneId && ids.has(c.paneId) ? c.paneId : undefined,
								size: typeof c?.size === 'number' && c.size > 0 ? c.size : 1
							}))
						}))
					: [{ size: 1, cells: restored.map((p) => ({ paneId: p.id, size: p.size ?? 1 })) }]
			);
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
