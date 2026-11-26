// Shared session-list formatting — used by +page rows and the panels.
// Store reads via get() stay reactive when invoked from templates.
import { get } from 'svelte/store';
import type { Session } from './api';
import type { Pane } from './stores';
import { agentStates, sessions, LIVE_STATES } from './stores';

export function isLive(s: Session) {
	return LIVE_STATES.has(s.state);
}
export function isFinished(s: Session) {
	return !isLive(s);
}
export function sessionOf(id: string): Session | undefined {
	return get(sessions).find((s) => s.id === id);
}
// §05: a low-confidence wait ("may be waiting") is never promoted to
// the needs-you rank — AGENTS.md: unknown shows the hint only. And a
// finished session is never "needs you": its agent state freezes at
// the last detection, which may still read waiting-for-input.
export function needsYouId(id: string) {
	const a = get(agentStates)[id];
	const s = sessionOf(id);
	return !!s && isLive(s) && a?.state === 'waiting-for-input' && a.confidence !== 'low';
}
export function needsYou(s: Session) {
	return needsYouId(s.id);
}

export function fmtAgo(now: number, iso?: string) {
	if (!iso) return '';
	const sec = Math.max(0, (now - Date.parse(iso)) / 1000);
	if (sec < 10) return 'now';
	if (sec < 60) return `${Math.floor(sec)}s`;
	if (sec < 3600) return `${Math.floor(sec / 60)}m`;
	if (sec < 86400) return `${Math.floor(sec / 3600)}h`;
	return `${Math.floor(sec / 86400)}d`;
}

// §05 row subline: waiting → how long it's been waiting; finished →
// terminal state + when it ended; live → foreground process/state.
export function subLabel(s: Session, now: number) {
	const a = get(agentStates)[s.id];
	// Lifecycle beats detection: a session that exited while waiting
	// shows its terminal state, not a stale "waiting".
	if (isFinished(s)) {
		const ago = fmtAgo(now, s.ended_at);
		return s.state + (ago ? ` ${ago}` : '');
	}
	if (needsYou(s)) return `waiting ${fmtAgo(now, a?.since)}`;
	// A low-confidence wait is only ever a hint — "may be waiting",
	// never the confident "waiting <t>" phrasing (session-state
	// catalog: unknown carries no options).
	if (a?.state === 'waiting-for-input' && a.confidence === 'low') {
		return 'may be waiting';
	}
	return a?.process ?? s.state;
}
// The detached overlay flag: a live session nobody has watched for
// longer than TU_RECONNECT_WINDOW_S.
export function detachedOf(s: Session) {
	return s.detached && isLive(s);
}
export function toolBadge(s: Session) {
	return get(agentStates)[s.id]?.tool ?? '';
}
// §06 pin badge — only `unverified` surfaces (a probed binary that
// failed the adapter's version constraint); unknown = still probing.
export function pinUnv(id: string) {
	return get(agentStates)[id]?.pin_status === 'unverified';
}

export function label(s: Session | undefined): string {
	if (!s) return '?';
	return s.name || s.shell.split('/').pop() || s.id;
}

// Tab auto-name: an explicit name always wins; otherwise show the
// app actually running — curated tool name when an adapter matched,
// else the live foreground process; shell basename as the floor.
export function tabLabel(p: Pane): string {
	const s = sessionOf(p.sessionId);
	if (!s) return '?';
	if (s.name) return s.name;
	const a = get(agentStates)[p.sessionId];
	return a?.tool || a?.process || s.shell.split('/').pop() || s.id;
}
