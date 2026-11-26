<!-- Ref: .teos/sections/04-command-intelligence.md + side-panels design —
     the right panel's commands view: session-aware command list (context
     manifests for the session's live tool, generic set for a bare shell),
     --help discovery, then a cheat-sheet detail per manifest (ADR-007:
     display + copy only — nothing here runs the command). -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import {
		api,
		ApiError,
		type CommandManifest,
		type CommandSummary,
		type ContextResult
	} from '$lib/api';

	let {
		sessionId = '',
		onclose
	}: {
		sessionId?: string;
		onclose?: () => void;
	} = $props();

	type Phase = 'search' | 'detail';
	let phase = $state<Phase>('search');
	let q = $state('');
	let rows = $state<CommandSummary[]>([]);
	let sel = $state(0);
	let busy = $state(false);
	let err = $state('');
	let manifest = $state<CommandManifest | null>(null);
	let copied = $state(false);
	let discBusy = $state(false);
	let inputEl: HTMLInputElement | undefined = $state();
	let listEl: HTMLDivElement | undefined = $state();
	let debounce: ReturnType<typeof setTimeout> | null = null;
	let seq = 0;

	// Session context — the live tool/procs/cwd behind the armed pane;
	// empty-query rows are its manifest list, and the ctx header shows
	// what's bound. Refetched when the armed session changes.
	let ctx = $state<ContextResult | null>(null);
	let lastCtx = '';
	async function loadContext(id: string) {
		if (!id) {
			ctx = null;
			return;
		}
		try {
			ctx = await api.commandContext(id);
		} catch {
			ctx = null;
		}
	}
	$effect(() => {
		if (sessionId !== lastCtx) {
			lastCtx = sessionId;
			void loadContext(sessionId);
		}
	});

	// The running tool a discover call should target — adapter-bound
	// name first, live foreground process as the fallback.
	const ctxExe = $derived(ctx?.tool || ctx?.processes?.[0] || '');
	const discoverTarget = $derived(q.trim() || ctxExe);
	// "discover <exe> --help" surfaces when the session's tool has no
	// manifests, or the query names an uncurated binary.
	const canDiscover = $derived(
		(ctxExe !== '' && (ctx?.commands?.length ?? 0) === 0 && q.trim() === '') ||
			(/^[a-z0-9][a-z0-9_.-]*$/.test(q) && !rows.some((r) => r.application === q))
	);

	// Open = the session's context commands without any typing (§04).
	// A bare shell (ctx loaded, no manifests, no bound exe) falls back
	// to the generic curated set.
	async function search() {
		const my = ++seq;
		try {
			let r: CommandSummary[];
			if (q.trim() === '' && ctx) {
				r = ctx.commands.length || ctxExe ? ctx.commands : await api.commands('', sessionId, 20);
			} else {
				r = await api.commands(q, sessionId);
			}
			if (my !== seq) return; // a newer keystroke owns the list
			rows = r;
			sel = 0;
			err = '';
		} catch {
			if (my === seq) {
				rows = [];
				err = 'search failed';
			}
		}
	}
	// ctx arriving async re-runs search so the fallback path applies too.
	$effect(() => {
		if (ctx && q.trim() === '') void search();
	});
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
			else if (canDiscover && !discBusy) void discover();
		} else if (phase === 'detail' && e.key === 'Enter') {
			e.preventDefault();
			void copyArgv();
		}
	}
	function back() {
		if (phase === 'detail') {
			phase = 'search';
			manifest = null;
			copied = false;
			void tick().then(() => inputEl?.focus());
		} else {
			onclose?.();
		}
	}
	function scrollSel() {
		void tick().then(() => {
			listEl?.querySelector('.crow.sel')?.scrollIntoView({ block: 'nearest' });
		});
	}

	// pick → the cheat-sheet detail: the argv template with {param}
	// placeholders intact, plus the param list as reference.
	async function pick(c: CommandSummary) {
		if (busy) return;
		busy = true;
		err = '';
		try {
			manifest = await api.command(c.id);
			copied = false;
			phase = 'detail';
		} catch (e) {
			err = e instanceof Error ? e.message : 'failed to load command';
		} finally {
			busy = false;
		}
	}

	// The cheat-sheet shows the template verbatim — {param} refs stay
	// as placeholders for the human to fill in at the terminal. Flagged
	// params render after the argument templates (the execution
	// ordering: `git commit -m {message}`, never `git -m x commit`).
	function argvTemplate(m: CommandManifest): string[] {
		const out = [m.command.executable, ...m.command.arguments];
		for (const p of m.parameters ?? []) {
			if (!p.flag) continue;
			out.push(p.flag);
			if (p.type !== 'boolean') out.push(`{${p.name}}`);
		}
		return out;
	}
	function argvText(m: CommandManifest): string {
		return argvTemplate(m).join(' ');
	}
	async function copyArgv() {
		if (!manifest) return;
		try {
			await navigator.clipboard.writeText(argvText(manifest));
			copied = true;
		} catch {
			err = 'copy failed — clipboard permission denied';
		}
	}

	// "discover <exe>" — scrapes --help into provisional manifests
	// (broker-gated controlled op, §04 auto-discovery). Targets the
	// session's running tool when the query box is empty.
	async function discover() {
		if (discBusy) return;
		const exe = discoverTarget;
		if (!exe) return;
		discBusy = true;
		err = '';
		try {
			const found = await api.discovered(exe);
			if (found.length === 0) err = `no subcommands found in ${exe} --help`;
			else {
				await loadContext(sessionId);
				await search();
			}
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
		return () => {
			if (debounce) clearTimeout(debounce);
		};
	});
</script>

<!-- svelte-ignore a11y_no_static_element_interactions — panel root -->
<div class="cmdview" onkeydown={keydown} role="presentation">
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
		</div>
		{#if ctx}
			<div class="cctxh">
				{ctx.tool || ctx.processes.join(' ') || 'shell'}{ctx.cwd ? ` · ${ctx.cwd}` : ''}
				{#if ctx.pin_status === 'unverified'}<span class="badge unv">unverified</span>{/if}
			</div>
		{/if}
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
			{#if err}
				<div class="perr" style="padding: 10px 12px">{err}</div>
			{/if}
			{#if canDiscover}
				<button class="crow cdisc" disabled={discBusy} onclick={() => void discover()}>
					{discBusy ? `reading ${discoverTarget} --help…` : `discover "${discoverTarget}" — read --help`}
				</button>
			{/if}
		</div>
		<div class="pfoot">↵ view · ↑↓ pick · esc back{ctx ? ' · session commands' : ''}</div>
	{:else if phase === 'detail' && manifest}
		<div class="phead">
			<button class="pback" onclick={back}>‹</button>
			<span class="ptitle">{manifest.name}</span>
			<span class="capp">{manifest.application}</span>
			{#if manifest.execution.risk !== 'informational'}
				<span class="crisk" class:hot={manifest.execution.risk === 'confirmed'}>{manifest.execution.risk}</span>
			{/if}
		</div>
		<div class="pform">
			{#if manifest.description}<div class="pdesc">{manifest.description}</div>{/if}
			<div class="pargv">
				{#each argvTemplate(manifest) as a, i (i)}<code>{a}</code>{/each}
			</div>
			{#if (manifest.parameters ?? []).length > 0}
				<div class="pparams">
					{#each manifest.parameters ?? [] as p (p.name)}
						<div class="pp">
							<span class="ppn">
								{p.name}{#if p.required}<span class="preq">*</span>{/if}
							</span>
							{#if p.flag}<code class="ppf">{p.flag}</code>{/if}
							<span class="ppt">{p.type}{p.type === 'enum' && p.options ? `: ${p.options.join(' | ')}` : ''}</span>
							{#if p.description}<span class="ppd">{p.description}</span>{/if}
						</div>
					{/each}
				</div>
			{/if}
			{#if manifest.execution.working_dir}
				<div class="pnote">
					runs in {manifest.execution.working_dir === '{session.cwd}'
						? "the session's working directory"
						: manifest.execution.working_dir}
				</div>
			{/if}
			{#if err}<div class="perr">{err}</div>{/if}
			<div class="pcta">
				<button class="pcopy" onclick={() => void copyArgv()}>
					{copied ? 'copied ✓' : 'copy command'}
				</button>
			</div>
		</div>
	{/if}
</div>

<style>
	.cmdview {
		display: flex;
		flex-direction: column;
		height: 100%;
		background: #0d1117;
		overflow: hidden;
	}
	.cctxh {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
		border-bottom: 1px solid #141b26;
		color: #4a5568;
		font: 11px ui-monospace, monospace;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.badge.unv {
		color: #d0a24a;
		border: 1px solid #3d3014;
		border-radius: 5px;
		padding: 1px 6px;
		background: #1a160c;
		font-size: 10px;
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
	.plist {
		flex: 1;
		min-height: 0;
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
		flex: 1;
		min-height: 0;
		overflow-y: auto;
	}
	.preq {
		color: #f0b429;
		margin-left: 3px;
	}
	.pdesc {
		font: 12px ui-monospace, monospace;
		color: #9db2ce;
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
	.pparams {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.pp {
		display: flex;
		align-items: baseline;
		gap: 8px;
		flex-wrap: wrap;
		font: 12px ui-monospace, monospace;
	}
	.ppn {
		color: #9db2ce;
	}
	.ppf {
		color: #cfe3ff;
		background: #141b26;
		border: 1px solid #22304a;
		border-radius: 5px;
		padding: 1px 6px;
		font-size: 11px;
	}
	.ppt {
		color: #4a5568;
		font-size: 11px;
	}
	.ppd {
		color: #3d4d68;
		font-size: 11px;
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
	.pcopy {
		min-height: 42px;
		padding: 0 22px;
		border: 1px solid #1d3550;
		border-radius: 9px;
		background: #0e1a26;
		color: #4cc9f0;
		font: 13px ui-monospace, monospace;
	}
	.pcopy:active {
		background: #1c2a44;
	}
</style>
