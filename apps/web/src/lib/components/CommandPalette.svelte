<!-- Ref: .teos/sections/04-command-intelligence.md — the ⌘K palette:
     fuzzy search over /api/commands with context-first ranking for the
     armed session, a param form per manifest, and a confirm step for
     risky runs (confirmed risk re-POSTs with the broker's argv token). -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import {
		api,
		ApiError,
		type CommandManifest,
		type CommandSummary,
		type ConfirmDetail,
		type Session
	} from '$lib/api';

	let {
		sessionId = '',
		onspawn,
		onclose
	}: {
		sessionId?: string;
		onspawn: (s: Session) => void;
		onclose: () => void;
	} = $props();

	type Phase = 'search' | 'params' | 'confirm';
	let phase = $state<Phase>('search');
	let q = $state('');
	let rows = $state<CommandSummary[]>([]);
	let sel = $state(0);
	let busy = $state(false);
	let err = $state('');
	let manifest = $state<CommandManifest | null>(null);
	let values = $state<Record<string, string | boolean>>({});
	let confirmToken = $state('');
	let confirmArgv = $state<string[]>([]);
	let confirmRisk = $state('');
	let discBusy = $state(false);
	let inputEl: HTMLInputElement | undefined = $state();
	let listEl: HTMLDivElement | undefined = $state();
	let debounce: ReturnType<typeof setTimeout> | null = null;
	let seq = 0;

	// Open = one search against the armed session's context — ctx rows
	// land first without any typing (§04 context actions).
	async function search() {
		const my = ++seq;
		try {
			const r = await api.commands(q, sessionId);
			if (my !== seq) return; // a newer keystroke owns the list
			rows = r;
			sel = 0;
			err = '';
		} catch {
			if (my === seq) rows = [];
		}
	}
	function onQ() {
		if (debounce) clearTimeout(debounce);
		debounce = setTimeout(search, 120);
	}

	// Esc backs out a phase before closing; ↑↓/Enter drive the list.
	function keydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			back();
		} else if (phase === 'search' && e.key === 'ArrowDown') {
			e.preventDefault();
			sel = Math.min(sel + 1, rows.length - 1);
			scrollSel();
		} else if (phase === 'search' && e.key === 'ArrowUp') {
			e.preventDefault();
			sel = Math.max(sel - 1, 0);
			scrollSel();
		} else if (phase === 'search' && e.key === 'Enter') {
			e.preventDefault();
			const c = rows[sel];
			if (c) void pick(c);
		} else if (phase === 'params' && e.key === 'Enter') {
			e.preventDefault();
			void submitParams();
		} else if (phase === 'confirm' && e.key === 'Enter') {
			e.preventDefault();
			if (manifest) void run(manifest);
		}
	}
	function back() {
		if (phase === 'confirm' || phase === 'params') {
			phase = 'search';
			confirmToken = '';
			void tick().then(() => inputEl?.focus());
		} else {
			onclose();
		}
	}
	function scrollSel() {
		void tick().then(() => {
			listEl?.querySelector('.crow.sel')?.scrollIntoView({ block: 'nearest' });
		});
	}

	// pick → detail → params form (if the manifest declares any) or the
	// confirm/run path.
	async function pick(c: CommandSummary) {
		busy = true;
		err = '';
		try {
			const m = await api.command(c.id);
			manifest = m;
			values = {};
			for (const p of m.parameters ?? []) {
				if (p.default !== undefined) values[p.name] = p.default as string | boolean;
				else if (p.type === 'boolean') values[p.name] = false;
				else values[p.name] = '';
			}
			if ((m.parameters ?? []).length > 0) {
				phase = 'params';
			} else {
				await stage(m);
			}
		} catch (e) {
			err = e instanceof Error ? e.message : 'failed to load command';
		} finally {
			busy = false;
		}
	}

	// stage decides between the pre-run confirm gate and an immediate
	// execute. `confirmed` risk skips the preview — only the server's
	// 409 carries the exact argv + bound token (§09).
	async function stage(m: CommandManifest) {
		if (m.execution.confirmation && m.execution.risk !== 'confirmed') {
			confirmArgv = previewArgv(m);
			confirmRisk = m.execution.risk;
			confirmToken = '';
			phase = 'confirm';
			return;
		}
		await run(m);
	}

	// buildParams strips empties — the server only binds declared names;
	// a blank optional param stays out of argv entirely.
	function buildParams(): Record<string, unknown> {
		const out: Record<string, unknown> = {};
		for (const p of manifest?.parameters ?? []) {
			const v = values[p.name];
			if (v === undefined || v === '') continue;
			out[p.name] = v;
		}
		return out;
	}

	// Display-only argv preview for the controlled-risk gate — template
	// refs substitute inline, flagged params append after (mirrors
	// registry.Build ordering; the authoritative argv is always the
	// server's, shown verbatim for `confirmed` risk).
	function previewArgv(m: CommandManifest): string[] {
		const out = [m.command.executable];
		for (const a of m.command.arguments) {
			out.push(
				a.replace(/\{([a-zA-Z0-9_.]+)\}/g, (r, name) =>
					values[name] !== undefined && values[name] !== '' ? String(values[name]) : r
				)
			);
		}
		for (const p of m.parameters ?? []) {
			const v = values[p.name];
			if (!p.flag || v === undefined || v === '' || v === false) continue;
			out.push(p.flag);
			if (v !== true) out.push(String(v));
		}
		return out;
	}

	const missing = $derived(
		(manifest?.parameters ?? []).some(
			(p) => p.required && (values[p.name] === undefined || values[p.name] === '')
		)
	);

	async function submitParams() {
		if (!manifest || missing) return;
		await stage(manifest);
	}

	async function run(m: CommandManifest) {
		busy = true;
		err = '';
		try {
			const res = await api.executeCommand(m.id, {
				session_id: sessionId,
				params: buildParams(),
				cols: 120,
				rows: 30,
				confirm_token: confirmToken || undefined
			});
			onspawn(res.session);
			onclose();
		} catch (e) {
			if (e instanceof ApiError && e.code === 'confirmation_required') {
				const d = e.detail as ConfirmDetail;
				confirmToken = d.confirm_token;
				confirmArgv = d.argv;
				confirmRisk = 'confirmed';
				phase = 'confirm';
			} else {
				err = e instanceof ApiError ? e.message : 'execute failed';
			}
		} finally {
			busy = false;
		}
	}

	// "discover <q>" — scrapes --help into provisional manifests
	// (broker-gated controlled op, §04 auto-discovery).
	const canDiscover = $derived(
		/^[a-z0-9][a-z0-9_.-]*$/.test(q) && !rows.some((r) => r.application === q)
	);
	async function discover() {
		const exe = q;
		discBusy = true;
		err = '';
		try {
			const found = await api.discovered(exe);
			if (found.length === 0) err = `no subcommands found in ${exe} --help`;
			else await search();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'discovery failed';
		} finally {
			discBusy = false;
		}
	}

	// onMount, not $effect — search() reads `q` synchronously, and an
	// effect would re-fire on every keystroke (bypassing the debounce).
	onMount(() => {
		void search();
		void tick().then(() => inputEl?.focus());
	});
</script>

<!-- svelte-ignore a11y_no_static_element_interactions — modal scrim -->
<div class="pshade" onpointerdown={onclose} oncontextmenu={(e) => e.preventDefault()}>
	<!-- svelte-ignore a11y_no_static_element_interactions — panel -->
	<div class="palette" onpointerdown={(e) => e.stopPropagation()} onkeydown={keydown}>
		{#if phase === 'search'}
			<div class="prow">
				<span class="pglyph">⌘</span>
				<input
					bind:this={inputEl}
					class="pq"
					placeholder="command…"
					autocapitalize="off"
					autocorrect="off"
					spellcheck="false"
					bind:value={q}
					oninput={onQ}
				/>
				<button class="px" aria-label="close" onclick={onclose}>✕</button>
			</div>
			<div class="plist" bind:this={listEl}>
				{#each rows as c, i (c.id)}
					<button class="crow" class:sel={i === sel} onpointerenter={() => (sel = i)} onclick={() => void pick(c)}>
						<span class="cmain">
							<span class="cname">
								{c.name}
								{#if c.context}<span class="cctx">ctx</span>{/if}
							</span>
							{#if c.description}<span class="cdesc">{c.description}</span>{/if}
						</span>
						<span class="cside">
							<span class="capp">{c.application}</span>
							{#if c.risk !== 'informational'}<span class="crisk" class:hot={c.risk === 'confirmed'}>{c.risk}</span>{/if}
							{#if c.source === 'discovered'}<span class="csrc">auto</span>{/if}
						</span>
					</button>
				{/each}
				{#if rows.length === 0 && !err}
					<div class="cempty">no commands</div>
				{/if}
				{#if canDiscover}
					<button class="crow cdisc" disabled={discBusy} onclick={() => void discover()}>
						{discBusy ? `reading ${q} --help…` : `discover "${q}" — read --help`}
					</button>
				{/if}
			</div>
			<div class="pfoot">↵ run · ↑↓ pick · esc close{sessionId ? ' · ctx-ranked for this pane' : ''}</div>
		{:else if phase === 'params' && manifest}
			<div class="phead">
				<button class="pback" onclick={back}>‹</button>
				<span class="ptitle">{manifest.name}</span>
				<span class="capp">{manifest.application}</span>
			</div>
			<div class="pform">
				{#each manifest.parameters ?? [] as p (p.name)}
					<label class="pf">
						<span class="pfl">
							{p.name}{#if p.required}<span class="preq">*</span>{/if}
							{#if p.flag}<span class="pflag">{p.flag}</span>{/if}
						</span>
						{#if p.type === 'boolean'}
							<input type="checkbox" bind:checked={values[p.name] as boolean} />
						{:else if p.type === 'enum'}
							<select bind:value={values[p.name]}>
								{#if !p.required}<option value="">—</option>{/if}
								{#each p.options ?? [] as o (o)}
									<option value={o}>{o}</option>
								{/each}
							</select>
						{:else}
							<input
								class="ptext"
								type={p.type === 'number' ? 'number' : 'text'}
								autocapitalize="off"
								autocorrect="off"
								spellcheck="false"
								placeholder={p.description ?? (p.resource ? `${p.resource}…` : '')}
								bind:value={values[p.name] as string}
							/>
						{/if}
					</label>
				{/each}
				{#if err}<div class="perr">{err}</div>{/if}
				<button class="prun" disabled={busy || missing} onclick={() => void submitParams()}>
					{busy ? '…' : manifest.execution.confirmation ? 'review' : 'run'}
				</button>
			</div>
		{:else if phase === 'confirm' && manifest}
			<div class="phead">
				<button class="pback" onclick={back}>‹</button>
				<span class="ptitle">{manifest.name}</span>
				<span class="crisk" class:hot={confirmRisk === 'confirmed'}>{confirmRisk}</span>
			</div>
			<div class="pform">
				<div class="pargv">
					{#each confirmArgv as a, i (i)}<code>{a}</code>{/each}
				</div>
				{#if confirmRisk === 'confirmed'}
					<div class="pnote">this command is marked destructive — argv shown is the exact invocation</div>
				{/if}
				{#if err}<div class="perr">{err}</div>{/if}
				<div class="pcta">
					<button class="pcancel" onclick={back}>cancel</button>
					<button class="prun" disabled={busy} onclick={() => manifest && void run(manifest)}>
						{busy ? '…' : 'run'}
					</button>
				</div>
			</div>
		{/if}
	</div>
</div>

<style>
	.pshade {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: #06090db8;
		display: flex;
		justify-content: center;
		align-items: flex-start;
		padding: 12dvh 10px 0;
	}
	.palette {
		width: min(560px, 94vw);
		background: #0d1117;
		border: 1px solid #22304a;
		border-radius: 12px;
		box-shadow: 0 16px 48px #000c;
		overflow: hidden;
	}
	.prow {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 10px;
		border-bottom: 1px solid #141b26;
	}
	.pglyph {
		color: #4cc9f0;
		font: 16px/1 ui-monospace, monospace;
	}
	.pq {
		flex: 1;
		min-width: 0;
		border: 0;
		background: transparent;
		color: #e6f2ff;
		font: 15px ui-monospace, monospace;
		outline: none;
		padding: 8px 0;
	}
	.px {
		border: 0;
		background: transparent;
		color: #4a5568;
		font-size: 12px;
		min-width: 36px;
		min-height: 36px;
	}
	.px:active {
		color: #ff8080;
	}
	.plist {
		max-height: 52dvh;
		overflow-y: auto;
	}
	.crow {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		border: 0;
		background: transparent;
		padding: 11px 12px;
		text-align: left;
		cursor: pointer;
		border-bottom: 1px solid #0f141d;
	}
	.crow.sel {
		background: #16233a;
	}
	.cmain {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.cname {
		font: 13px ui-monospace, monospace;
		color: #e6e6e6;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.cctx {
		font: 9px ui-monospace, monospace;
		color: #3ddc84;
		border: 1px solid #1d4030;
		border-radius: 4px;
		padding: 1px 5px;
	}
	.cdesc {
		font: 11px ui-monospace, monospace;
		color: #4a5568;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.cside {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-shrink: 0;
	}
	.capp {
		font: 10px ui-monospace, monospace;
		color: #4cc9f0;
		border: 1px solid #1d3550;
		border-radius: 5px;
		padding: 1px 6px;
		background: #0e1a26;
	}
	.crisk {
		font: 10px ui-monospace, monospace;
		color: #d0a24a;
		border: 1px solid #3d3014;
		border-radius: 5px;
		padding: 1px 6px;
	}
	.crisk.hot {
		color: #ff8080;
		border-color: #4a2030;
	}
	.csrc {
		font: 10px ui-monospace, monospace;
		color: #7a5aa0;
	}
	.cdisc {
		color: #6b7f9e;
		font: 12px ui-monospace, monospace;
	}
	.cempty {
		padding: 20px;
		text-align: center;
		color: #33415c;
		font: 12px ui-monospace, monospace;
	}
	.pfoot {
		padding: 7px 12px;
		border-top: 1px solid #141b26;
		color: #3d4d68;
		font: 10px ui-monospace, monospace;
		letter-spacing: 0.08em;
	}
	.phead {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 12px;
		border-bottom: 1px solid #141b26;
	}
	.pback {
		border: 0;
		background: transparent;
		color: #6b7f9e;
		font-size: 18px;
		min-width: 36px;
		min-height: 36px;
	}
	.ptitle {
		flex: 1;
		font: 14px ui-monospace, monospace;
		color: #e6f2ff;
	}
	.pform {
		padding: 10px 12px 14px;
		display: flex;
		flex-direction: column;
		gap: 10px;
		max-height: 60dvh;
		overflow-y: auto;
	}
	.pf {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.pfl {
		width: 130px;
		flex-shrink: 0;
		font: 12px ui-monospace, monospace;
		color: #9db2ce;
	}
	.preq {
		color: #f0b429;
		margin-left: 3px;
	}
	.pflag {
		display: block;
		font-size: 10px;
		color: #4a5568;
	}
	.pf input[type='checkbox'] {
		width: 20px;
		height: 20px;
		accent-color: #4cc9f0;
	}
	.pf select {
		flex: 1;
		background: #141b26;
		color: #d7e1f0;
		border: 1px solid #22304a;
		border-radius: 7px;
		font: 13px ui-monospace, monospace;
		padding: 8px;
	}
	.ptext {
		flex: 1;
		min-width: 0;
		background: #141b26;
		border: 1px solid #22304a;
		border-radius: 7px;
		color: #d7e1f0;
		font: 13px ui-monospace, monospace;
		padding: 8px 10px;
		outline: none;
	}
	.ptext:focus {
		border-color: #4cc9f0;
	}
	.pargv {
		display: flex;
		flex-wrap: wrap;
		gap: 5px;
	}
	.pargv code {
		font: 12px ui-monospace, monospace;
		color: #cfe3ff;
		background: #141b26;
		border: 1px solid #22304a;
		border-radius: 6px;
		padding: 3px 8px;
		word-break: break-all;
	}
	.pnote {
		font: 11px ui-monospace, monospace;
		color: #d0a24a;
	}
	.perr {
		font: 12px ui-monospace, monospace;
		color: #ff8080;
	}
	.pcta {
		display: flex;
		gap: 8px;
		justify-content: flex-end;
	}
	.prun {
		min-height: 42px;
		padding: 0 22px;
		border: 1px solid #1d3550;
		border-radius: 9px;
		background: #0e1a26;
		color: #4cc9f0;
		font: 13px ui-monospace, monospace;
	}
	.prun:disabled {
		opacity: 0.4;
	}
	.prun:active {
		background: #1c2a44;
	}
	.pcancel {
		min-height: 42px;
		padding: 0 16px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: transparent;
		color: #6b7f9e;
		font: 13px ui-monospace, monospace;
	}
</style>
