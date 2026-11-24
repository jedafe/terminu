// Ref: .teos/conventions/api-response-format.md — {data} | {error:{code,message,status}}
export class ApiError extends Error {
	constructor(
		public code: string,
		public status: number,
		msg: string
	) {
		super(msg);
	}
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(path, {
		credentials: 'same-origin',
		headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
		...init
	});
	const body = await res.json().catch(() => ({}));
	if (!res.ok) {
		const e = body?.error ?? {};
		throw new ApiError(e.code ?? 'internal', res.status, e.message ?? res.statusText);
	}
	return body.data as T;
}

export const api = {
	login: (username: string, password: string) =>
		req<{ status: string }>('/api/auth/login', {
			method: 'POST',
			body: JSON.stringify({ username, password })
		}),
	logout: () => req('/api/auth/logout', { method: 'POST' }),
	me: () => req<{ user_id: number; username: string; role: string }>('/api/auth/me'),
	sessions: () => req<Session[]>('/api/sessions'),
	spawn: (s: SpawnRequest) => req<Session>('/api/sessions', { method: 'POST', body: JSON.stringify(s) }),
	kill: (id: string) => req(`/api/sessions/${id}`, { method: 'DELETE' }),
	clearFinished: () => req<{ cleared: number }>('/api/sessions', { method: 'DELETE' }),
	clearLog: (id: string) => req(`/api/sessions/${id}/log`, { method: 'DELETE' }),
	rename: (id: string, name: string) =>
		req(`/api/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ name }) }),
	workspaces: () => req<Workspace[]>('/api/workspaces'),
	putWorkspace: (id: number, w: { name: string; layout: string; last_open_session?: string }) =>
		req(`/api/workspaces/${id}`, { method: 'PUT', body: JSON.stringify(w) }),
	agents: () => req<AgentView[]>('/api/agents'),
	agentPrompt: (id: string) => req<AgentPrompt>(`/api/agents/${id}/prompt`)
};

export interface Session {
	id: string;
	name: string;
	target: { kind: string; id: string };
	cwd: string;
	shell: string;
	state: string;
	exit_code?: number;
	cols: number;
	rows: number;
	created_at: string;
	detached_at?: string;
	ended_at?: string;
}

export interface SpawnRequest {
	target?: string;
	cwd?: string;
	shell?: string;
	name?: string;
	cols?: number;
	rows?: number;
}

export interface Workspace {
	id: number;
	name: string;
	layout: string;
	last_open_session?: string;
	updated_at: string;
}

// §05 needs-you row — sessions ordered waiting-first by the server.
export interface AgentView {
	session: Session;
	process: string;
	tool?: string;
	state: string;
	kind?: string;
	confidence?: string;
	since: string;
	prompt?: AgentPromptBody;
}
export interface AgentPromptBody {
	kind: string;
	title: string;
	options: { label: string; inject: Keystroke; default?: boolean }[];
}
export interface AgentPrompt {
	state: string;
	kind?: string;
	confidence?: string;
	evidence?: string;
	region?: { y0: number; y1: number };
	prompt: AgentPromptBody | null;
}
export interface Keystroke {
	kind: 'char' | 'text' | 'arrow_select' | 'escape';
	char?: number;
	text?: string;
	arrows?: number;
}
