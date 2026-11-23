// App state: session registry + pane layout (views over sessions).
import { writable, get } from 'svelte/store';
import { api, type Session, type Workspace } from './api';
import { subscribeEvents } from './ws';

export const sessions = writable<Session[]>([]);
export const me = writable<{ username: string; role: string } | null>(null);

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
	subscribeEvents(() => {
		void refreshSessions();
	});
}
