<!-- Ref: .teos/plans/2026-11-26-side-panels-design.md — right rail:
     armed-session detail + prompt card + scrollback preview + rename
     (session view), hosting the session-aware commands view. All
     display-only per ADR-007. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { agentStates, prompts, refreshSessions, rightView } from '$lib/stores';
	import { isLive, needsYou, toolBadge, pinUnv, label, fmtAgo, sessionOf } from '$lib/sessionfmt';
	import { api, ApiError } from '$lib/api';
	import { stripAnsi } from '$lib/ansi';
	import CommandPalette from './CommandPalette.svelte';

	let { sessionId, onclose }: { sessionId: string; onclose: () => void } = $props();

	let sess = $derived(sessionOf(sessionId));
	let agent = $derived($agentStates[sessionId]);
	let prm = $derived($prompts[sessionId]);

	// Prompt fallback — unattached sessions have no live WS feeding
	// the prompts store; the REST view fills the gap once per select.
	let lastPromptFetch = '';
	$effect(() => {
		if (!sessionId || prm !== undefined || lastPromptFetch === sessionId) return;
		lastPromptFetch = sessionId;
		void api
			.agentPrompt(sessionId)
			.then((r) => {
				prompts.update((m) => ({ ...m, [sessionId]: r.prompt ?? null }));
			})
			.catch(() => {});
	});

	// Scrollback preview — tail read on open/session change + refresh.
	let preview = $state(''),
		previewErr = $state(''),
		lastPrevFetch = '';
	async function loadPreview(id: string) {
		previewErr = '';
		try {
			const text = await api.scrollback(id);
			const lines = stripAnsi(text).split('\n');
			preview = lines.slice(-40).join('\n').trimEnd();
		} catch {
			previewErr = 'no scrollback';
			preview = '';
		}
	}
	$effect(() => {
		if (sessionId && lastPrevFetch !== sessionId) {
			lastPrevFetch = sessionId;
			preview = '';
			void loadPreview(sessionId);
		}
	});

	// Rename — PATCH {name}; blank restores the auto label.
	let rn = $state(''),
		rnDirty = $state(false),
		rnErr = $state('');
	let lastRnId = '';
	$effect(() => {
		// A different session discards an unsubmitted rename — showing
		// the old session's draft under a new header would lie.
		if (sessionId !== lastRnId) {
			lastRnId = sessionId;
			rnDirty = false;
			rnErr = '';
		}
		if (!rnDirty) rn = sess?.name ?? '';
	});
	async function saveRename() {
		rnErr = '';
		try {
			await api.rename(sessionId, rn.trim());
			rnDirty = false;
			await refreshSessions();
		} catch (e) {
			rnErr = e instanceof ApiError ? e.message : 'rename failed';
		}
	}
	// Light clock for the "waiting Xm" subline — 15s matches the shell's
	// cadence; runs only while the panel is mounted.
	let now = $state(Date.now());
	onMount(() => {
		const t = setInterval(() => (now = Date.now()), 15000);
		return () => clearInterval(t);
	});
</script>

{#if $rightView === 'session'}
	{#if sess}
		<div class="pwrap">
			<div class="rhead">
				<span class="dot" class:live={isLive(sess)} class:needs={needsYou(sess)} class:dead={!isLive(sess)}></span>
				<span class="rname">{label(sess)}</span>
				{#if toolBadge(sess)}<span class="badge">{toolBadge(sess)}</span>{/if}
				{#if pinUnv(sess.id)}<span class="badge unv">unverified</span>{/if}
				<button class="rx" aria-label="close panel" onclick={onclose}>✕</button>
			</div>
			<div class="rename">
				<input class="mt" placeholder="name" bind:value={rn} oninput={() => (rnDirty = true)} />
				<button
					class="cx sm"
					disabled={!rnDirty || rn.trim() === sess.name}
					onclick={() => void saveRename()}>save</button
				>
			</div>
			{#if rnErr}<div class="merr">{rnErr}</div>{/if}

			<div class="dgrid">
				<span>state</span><span>{sess.state}{agent?.confidence ? ` (${agent.confidence})` : ''}</span>
				<span>process</span><span>{agent?.process ?? '—'}</span>
				{#if agent?.tool}<span>tool</span><span>{agent.tool}</span>{/if}
				<span>cwd</span><span class="trunc" title={sess.cwd}>{sess.cwd || '—'}</span>
				<span>size</span><span>{sess.cols}×{sess.rows}</span>
				<span>created</span><span>{fmtAgo(now, sess.created_at)} ago</span>
				{#if sess.ended_at}<span>ended</span><span
						>{fmtAgo(now, sess.ended_at)} ago ({sess.exit_code ?? '?'})</span
					>{/if}
				{#if sess.detached}<span>watch</span><span>unattended</span>{/if}
				<span>id</span><span class="trunc" title={sess.id}>{sess.id}</span>
			</div>

			{#if prm}
				<div class="ghead"><span>prompt</span></div>
				<div class="pcard">
					<div class="ptitle">{prm.title}</div>
					{#if (prm.options?.length ?? 0) > 0}
						<div class="prow">
							{#each prm.options ?? [] as o}
								<span class="popt" class:def={o.default}
									>{o.label}{#if o.hint}<kbd class="phint">{o.hint}</kbd>{/if}</span
								>
							{/each}
						</div>
					{/if}
				</div>
			{/if}

			<div class="ghead">
				<span>preview</span>
				<button class="clearall" onclick={() => void loadPreview(sessionId)}>↻</button>
			</div>
			{#if previewErr}<div class="empty">{previewErr}</div>{/if}
			{#if preview}<pre class="prev">{preview}</pre>{/if}
		</div>
	{:else}
		<div class="pwrap">
			<div class="rhead">
				<span class="rname">no session selected</span>
				<button class="rx" aria-label="close panel" onclick={onclose}>✕</button>
			</div>
			<div class="empty">tap a session in the left panel</div>
		</div>
	{/if}
{:else}
	<CommandPalette sessionId={sessionId} onclose={onclose} />
{/if}

<style>
	.pwrap {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-y: auto;
		overscroll-behavior: contain;
	}
	.rhead {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 14px;
	}
	.rname {
		flex: 1;
		min-width: 0;
		font: 14px ui-monospace, monospace;
		color: #e6f2ff;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.rx {
		border: 0;
		background: transparent;
		color: #4a5568;
		font-size: 13px;
		min-width: 44px;
		min-height: 44px;
		margin: -10px -8px;
	}
	.rx:active {
		color: #ff8080;
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.dot.live {
		background: #3ddc84;
	}
	.dot.needs {
		background: #f0b429;
		box-shadow: 0 0 6px #f0b42980;
	}
	.dot.dead {
		background: #5a3540;
	}
	.badge {
		flex-shrink: 0;
		font: 10px ui-monospace, monospace;
		color: #4cc9f0;
		border: 1px solid #1d3550;
		border-radius: 5px;
		padding: 1px 6px;
		background: #0e1a26;
	}
	.badge.unv {
		color: #d0a24a;
		border-color: #3d3014;
		background: #1a160c;
	}
	.rename {
		display: flex;
		gap: 6px;
		padding: 0 14px 8px;
	}
	.mt {
		flex: 1;
		min-width: 0;
		background: #0d1117;
		border: 1px solid #22304a;
		border-radius: 7px;
		color: #d7e1f0;
		font: 12px ui-monospace, monospace;
		padding: 8px 10px;
		outline: none;
		min-height: 36px;
	}
	.mt:focus {
		border-color: #4cc9f0;
	}
	.cx.sm {
		border: none;
		border-radius: 7px;
		background: none;
		color: #d7e1f0;
		font: 12px ui-monospace, monospace;
		flex: 0 0 auto;
		width: 52px;
	}
	.cx.sm:disabled {
		opacity: 0.4;
	}
	.cx.sm:active {
		background: #1e2a3d;
	}
	.merr {
		font: 11px ui-monospace, monospace;
		color: #ff8080;
		padding: 0 14px 6px;
	}
	.dgrid {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 4px 12px;
		padding: 8px 14px;
		font: 12px ui-monospace, monospace;
		color: #9db2ce;
	}
	.dgrid > span:nth-child(odd) {
		color: #3d4d68;
	}
	.trunc {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ghead {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 8px 14px 4px;
		color: #3d4d68;
		font: 10px ui-monospace, monospace;
		letter-spacing: 0.18em;
	}
	.clearall {
		border: 0;
		background: transparent;
		color: #6b7f9e;
		font: 12px ui-monospace, monospace;
		padding: 8px 10px;
	}
	.clearall:active {
		color: #e6f2ff;
	}
	/* prompt card — same look as the in-pane overlay card (ADR-007:
	   read-only; answering happens in the terminal). */
	.pcard {
		margin: 4px 14px 8px;
		background: #131a26f2;
		border: 1px solid #2b3b55;
		border-radius: 10px;
		padding: 10px 12px;
	}
	.ptitle {
		color: #cfe0f5;
		font: 13px/1.35 ui-monospace, monospace;
		margin-bottom: 8px;
		white-space: pre-wrap;
	}
	.prow {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.popt {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		min-height: 32px;
		padding: 0 12px;
		border: 1px solid #33415c;
		border-radius: 8px;
		background: #1b2536;
		color: #dbe7f7;
		font: 13px/1 ui-monospace, monospace;
	}
	.popt.def {
		background: #1f5c8b;
		border-color: #2f7cb3;
	}
	.phint {
		font: 10px/1 ui-monospace, monospace;
		color: #9db2ce;
		border: 1px solid #33415c;
		border-bottom-width: 2px;
		border-radius: 4px;
		padding: 2px 5px;
		background: #0d1420;
	}
	.prev {
		font: 11px/1.45 ui-monospace, monospace;
		padding: 8px 14px;
		margin: 0;
		overflow: auto;
		white-space: pre-wrap;
		word-break: break-all;
		color: #9db2ce;
		max-height: 40dvh;
	}
	.empty {
		padding: 24px;
		text-align: center;
		color: #33415c;
		font: 13px ui-monospace, monospace;
	}
</style>
