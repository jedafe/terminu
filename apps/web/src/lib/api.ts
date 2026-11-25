// Ref: .teos/conventions/api-response-format.md — {data} | {error:{code,message,status}}
export class ApiError extends Error {
	constructor(
		public code: string,
		public status: number,
		msg: string,
		// Structured error payload — e.g. confirmation_required carries
		// {confirm_token, argv, expires_in_s} (§04/§09).
		public detail?: unknown
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
		throw new ApiError(e.code ?? 'internal', res.status, e.message ?? res.statusText, e.detail);
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
	agentPrompt: (id: string) => req<AgentPrompt>(`/api/agents/${id}/prompt`),
	alerts: () => req<Alert[]>('/api/alerts'),
	ackAlert: (id: number) => req(`/api/alerts/${id}/ack`, { method: 'POST' }),
	// §04 command intelligence — palette search (context-first when
	// session_id is given), manifest detail, broker-gated execute.
	commands: (q = '', sessionId = '', limit = 50) => {
		const p = new URLSearchParams({ q, limit: String(limit) });
		if (sessionId) p.set('session_id', sessionId);
		return req<CommandSummary[]>(`/api/commands?${p}`);
	},
	command: (id: string) => req<CommandManifest>(`/api/commands/${encodeURIComponent(id)}`),
	executeCommand: (id: string, body: ExecInput) =>
		req<ExecResult>(`/api/commands/${encodeURIComponent(id)}/execute`, {
			method: 'POST',
			body: JSON.stringify(body)
		}),
	commandContext: (sessionId: string) =>
		req<CommandContext>(`/api/commands/context/${sessionId}`),
	discovered: (exe?: string) =>
		req<CommandSummary[]>(exe ? `/api/discovered?exe=${encodeURIComponent(exe)}` : '/api/discovered'),
	adapters: () => req<AdapterPin[]>('/api/adapters'),
	// §05 launch profiles — saved argv+env configs, launched via broker.
	profiles: () => req<LaunchProfile[]>('/api/agents/profiles'),
	createProfile: (p: ProfileInput) =>
		req<LaunchProfile>('/api/agents/profiles', { method: 'POST', body: JSON.stringify(p) }),
	patchProfile: (id: number, p: Partial<ProfileInput>) =>
		req<{ status: string }>(`/api/agents/profiles/${id}`, {
			method: 'PATCH',
			body: JSON.stringify(p)
		}),
	deleteProfile: (id: number) =>
		req<{ status: string }>(`/api/agents/profiles/${id}`, { method: 'DELETE' }),
	launchProfile: (profileId: number, opts: LaunchOpts = {}) =>
		req<LaunchResult>('/api/agents/launch', {
			method: 'POST',
			body: JSON.stringify({ profile_id: profileId, ...opts })
		})
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
	// overlay flag — live session unattended past the reconnect window
	detached?: boolean;
}

export interface SpawnRequest {
	target?: string;
	cwd?: string;
	shell?: string;
	name?: string;
	cols?: number;
	rows?: number;
	argv?: string[];
	env?: Record<string, string>;
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
	pin_status?: string;
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

// §05 ack-able alert row — unacknowledged sort first server-side.
export interface Alert {
	id: number;
	session_id: string;
	level: string;
	transition: string;
	process?: string;
	kind?: string;
	confidence?: string;
	acked_at?: string;
	at: string;
}

// ---------- §04 command intelligence ----------

// Palette row — the list route's compact shape; context=true means the
// manifest matched the session's live process/files (context-first).
export interface CommandSummary {
	id: string;
	application: string;
	name: string;
	description?: string;
	tags?: string[];
	source: string; // builtin | custom | discovered
	risk: string; // informational | controlled | confirmed | restricted
	confirmation: boolean;
	context?: boolean;
}

// Full manifest — detail route only (adapter-manifest-format.md).
export interface CommandManifest {
	id: string;
	application: string;
	name: string;
	description: string;
	parameters?: CommandParam[];
	command: { executable: string; arguments: string[] };
	execution: { risk: string; confirmation: boolean; timeout_s: number; working_dir: string };
	context: { processes: string[]; files: string[] };
	tags?: string[];
	source: string;
	shadowed_by?: string;
}

export interface CommandParam {
	name: string;
	type: string; // resource | string | number | boolean | enum
	resource?: string;
	required?: boolean;
	flag?: string; // flag form, e.g. "--force"; absent = positional
	options?: string[];
	default?: unknown;
	description?: string;
}

export interface ExecInput {
	session_id?: string;
	params?: Record<string, unknown>;
	cwd?: string;
	name?: string;
	cols?: number;
	rows?: number;
	confirm_token?: string;
}

export interface ExecResult {
	session: Session;
	argv: string[];
	manifest_id: string;
}

// GET /api/commands/context/{session_id} — the live context read the
// context-first ranking uses (§04 Context actions).
export interface CommandContext {
	session_id: string;
	processes: string[];
	tool: string;
	pin_status: string;
	cwd: string;
	commands: CommandSummary[];
}

// confirmation_required (409) detail payload — the token binds to the
// exact argv the server built; resubmit with it to proceed (§09).
export interface ConfirmDetail {
	confirm_token: string;
	argv: string[];
	expires_in_s: number;
}

// ---------- §05 launch profiles ----------

export interface LaunchProfile {
	id: number;
	name: string;
	tool: string;
	argv: string[];
	env: Record<string, string>;
	cwd: string;
	resume_cmd: string[];
}

export interface ProfileInput {
	name: string;
	tool?: string;
	argv: string[];
	env?: Record<string, string>;
	cwd?: string;
	resume_cmd?: string[];
}

export interface LaunchOpts {
	resume?: boolean;
	name?: string;
	cwd?: string;
	cols?: number;
	rows?: number;
}

export interface LaunchResult {
	session: Session;
	profile_id: number;
	argv: string[];
}

// ---------- §06 adapters ----------

// GET /api/adapters row — pin status feeding the unverified badge.
export interface AdapterPin {
	tool: string;
	constraint: string;
	verified_at: string;
	status: string; // verified | unverified | unknown
	seen_version?: string;
}
