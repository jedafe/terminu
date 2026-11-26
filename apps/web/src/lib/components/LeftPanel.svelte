<!-- Ref: .teos/plans/2026-11-26-side-panels-design.md — left rail:
     sessions nav + alerts + layout presets (sessions view) and profile
     management (profiles view). The +new block expands inline under the
     header — shells, then profiles with launch-variant sub-selection. -->
<script lang="ts">
	import { api, ApiError, type LaunchProfile, type Session } from '$lib/api';
	import {
		orderedSessions,
		sessions,
		alerts,
		ackAlert,
		profiles,
		refreshProfiles,
		leftView,
		grid
	} from '$lib/stores';
	import {
		isLive,
		isFinished,
		needsYou,
		fmtAgo,
		subLabel,
		detachedOf,
		toolBadge,
		pinUnv,
		label
	} from '$lib/sessionfmt';

	let {
		onopen,
		onspawn,
		onlaunch,
		onend,
		onpurge,
		onclearfin,
		onpreset,
		now,
		onrow,
		newopen = $bindable(false)
	}: {
		onopen: (s: Session) => void;
		onspawn: (shell?: string) => Promise<void> | void;
		onlaunch: (id: number, o?: { variant?: string; resume?: boolean }) => Promise<void> | void;
		onend: (id: string, e: Event) => Promise<void> | void;
		onpurge: (id: string, e: Event) => Promise<void> | void;
		onclearfin: (e: Event) => Promise<void> | void;
		onpreset: (shape: number[]) => void;
		now: number;
		onrow?: () => void; // overlay-mode row click → close panel
		newopen?: boolean;
	} = $props();

	// Layout presets — the grid is a fixed arrangement of slots (≤4).
	const PRESETS: { name: string; shape: number[] }[] = [
		{ name: '1', shape: [1] },
		{ name: '1 | 1', shape: [2] },
		{ name: '1 ⁄ 1', shape: [1, 1] },
		{ name: '1 ⁄ 2', shape: [1, 2] },
		{ name: '2 ⁄ 1', shape: [2, 1] },
		{ name: '2 × 2', shape: [2, 2] },
		{ name: '3', shape: [3] },
		{ name: '1 ⁄ 3', shape: [1, 3] },
		{ name: '3 ⁄ 1', shape: [3, 1] },
		{ name: '4', shape: [4] },
		{ name: '1 ⁄ 1 ⁄ 1', shape: [1, 1, 1] }
	];
	// Portrait touch screens fit full-width stacks only — 1, 1⁄1, 1⁄1⁄1.
	const MOBILE_PRESETS = PRESETS.filter((p) => p.shape.every((n) => n === 1));
	const coarsePtr = () =>
		typeof matchMedia !== 'undefined' && matchMedia('(pointer: coarse)').matches;

	const unacked = $derived($alerts.filter((a) => !a.acked_at));
	const needs = $derived($orderedSessions.filter(needsYou));
	const running = $derived($orderedSessions.filter((s) => isLive(s) && !needsYou(s)));
	const finished = $derived($orderedSessions.filter(isFinished));

	// +new expanding block — expProf is the profile id whose launch
	// variants are open; pick() runs a spawn/launch then collapses.
	let err = $state('');
	let expProf = $state(0);
	const expandable = (pr: LaunchProfile) =>
		(pr.variants?.length ?? 0) > 0 || (pr.resume_cmd?.length ?? 0) > 0;
	const argvText = (pr: LaunchProfile) => (pr.argv ?? []).join(' ') || pr.shell;
	function toggleNew() {
		newopen = !newopen;
	}
	// +new can be opened from outside (topbar + / empty-grid button via
	// bind:newopen) — fetch profiles on any transition to open. The
	// profiles view needs them too — direct entry skips +new entirely.
	$effect(() => {
		if (newopen || $leftView === 'profiles') void refreshProfiles();
	});
	async function pick(fn: () => Promise<void> | void) {
		err = '';
		try {
			await fn();
			newopen = false;
			expProf = 0;
			onrow?.();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'launch failed';
		}
	}

	// ---- profiles view: create/edit form + list ----
	let fName = $state(''),
		fShell = $state(''),
		fArgv = $state(''),
		fCwd = $state(''),
		fResume = $state('');
	let fVars = $state<{ name: string; argv: string }[]>([]);
	let fErr = $state(''),
		editId = $state(0);
	const argvOf = (s: string) => s.trim().split(/\s+/).filter(Boolean);
	const dupeVar = $derived.by(() => {
		const names = fVars.map((v) => v.name.trim()).filter(Boolean);
		return new Set(names).size !== names.length;
	});
	const valid = $derived(
		fName.trim() !== '' &&
			!dupeVar &&
			(fArgv.trim() !== '' || fShell !== '' || fVars.some((v) => v.argv.trim() !== ''))
	);
	async function saveProfile() {
		fErr = '';
		const body = {
			name: fName.trim(),
			shell: fShell,
			argv: argvOf(fArgv),
			cwd: fCwd.trim() || undefined,
			resume_cmd: argvOf(fResume),
			variants: fVars
				.filter((v) => v.name.trim() && v.argv.trim())
				.map((v) => ({ name: v.name.trim(), argv: argvOf(v.argv) }))
		};
		try {
			if (editId) await api.patchProfile(editId, body);
			else await api.createProfile(body);
			resetForm();
			await refreshProfiles();
		} catch (e) {
			fErr = e instanceof ApiError ? e.message : 'save failed';
		}
	}
	function editProfile(pr: LaunchProfile) {
		editId = pr.id;
		fName = pr.name;
		fShell = pr.shell ?? '';
		fArgv = (pr.argv ?? []).join(' ');
		fCwd = pr.cwd;
		fResume = (pr.resume_cmd ?? []).join(' ');
		fVars = (pr.variants ?? []).map((v) => ({ name: v.name, argv: v.argv.join(' ') }));
	}
	function resetForm() {
		editId = 0;
		fName = fShell = fArgv = fCwd = fResume = '';
		fVars = [];
	}
	async function delProfile(id: number, e: Event) {
		e.stopPropagation();
		fErr = '';
		try {
			await api.deleteProfile(id);
			if (editId === id) resetForm();
			await refreshProfiles();
		} catch (ex) {
			fErr = ex instanceof ApiError ? ex.message : 'delete failed';
		}
	}
</script>

<div class="pwrap">
	{#if $leftView === 'profiles'}
		<div class="dhead">
			<button class="tb" onclick={() => leftView.set('sessions')}>‹ back</button>
			<span>profiles</span>
		</div>

		<div class="pform">
			<input class="mt" placeholder="name" bind:value={fName} />
			<select class="mt" bind:value={fShell}>
				<option value="">direct exec</option>
				<option value="/bin/bash">bash</option>
				<option value="/bin/zsh">zsh</option>
				<option value="/bin/fish">fish</option>
			</select>
			<input
				class="mt"
				placeholder="argv — e.g. devin --cloud (empty = shell session)"
				autocapitalize="off"
				autocorrect="off"
				spellcheck="false"
				bind:value={fArgv}
			/>
			<input class="mt" placeholder="cwd (optional)" autocapitalize="off" bind:value={fCwd} />
			<input class="mt" placeholder="resume cmd (optional)" bind:value={fResume} />
			<div class="vhead">
				<span>launch variants</span>
				<button class="cx sm" onclick={() => (fVars = [...fVars, { name: '', argv: '' }])}
					>+ variant</button
				>
			</div>
			{#each fVars as v, i}
				<div class="vrow">
					<input class="mt" placeholder="label" bind:value={v.name} />
					<input class="mt" placeholder="argv" bind:value={v.argv} />
					<button class="cx sm" onclick={() => (fVars = fVars.filter((_, j) => j !== i))}>✕</button>
				</div>
			{/each}
			{#if dupeVar}<div class="merr">variant names must be unique</div>{/if}
			{#if fErr}<div class="merr">{fErr}</div>{/if}
			<div class="mcta">
				<button class="msave" disabled={!valid} onclick={() => void saveProfile()}>
					{editId ? 'update' : 'save'}
				</button>
				{#if editId}<button class="mcx" onclick={resetForm}>cancel edit</button>{/if}
			</div>
		</div>
		{#if err}<div class="merr plist-err">{err}</div>{/if}

		{#each $profiles as pr (pr.id)}
			<div class="mrow">
				<button class="cx" onclick={() => void pick(() => onlaunch(pr.id))}>
					{pr.name}
					<span class="marg">
						{argvText(pr)}{pr.variants?.length ? ` · ${pr.variants.length} variants` : ''}
					</span>
				</button>
				{#if pr.resume_cmd?.length}
					<button
						class="cx sm"
						title="resume — {pr.resume_cmd.join(' ')}"
						onclick={() => void pick(() => onlaunch(pr.id, { resume: true }))}>↻</button
					>
				{/if}
				<button class="cx sm" title="edit" onclick={() => editProfile(pr)}>✎</button>
				<button class="cx sm" title="delete" onclick={(e) => void delProfile(pr.id, e)}>✕</button>
			</div>
		{/each}
	{:else}
		<div class="dhead">
			<span>sessions</span>
			<button class="tb" onclick={toggleNew}>+ new</button>
		</div>

		{#if newopen}
			<div class="newgrp">
				<div class="ghead"><span>shell</span></div>
				<button class="cx" onclick={() => void pick(() => onspawn('/bin/bash'))}>bash</button>
				<button class="cx" onclick={() => void pick(() => onspawn('/bin/zsh'))}>zsh</button>
				<button class="cx" onclick={() => void pick(() => onspawn('/bin/fish'))}>fish</button>
				<div class="ghead"><span>agents</span></div>
				{#each $profiles as pr (pr.id)}
					{#if expandable(pr)}
						<button class="cx" onclick={() => (expProf = expProf === pr.id ? 0 : pr.id)}>
							{pr.name} <span class="marg">{argvText(pr)}</span>
							<span class="caret">{expProf === pr.id ? '▾' : '▸'}</span>
						</button>
						{#if expProf === pr.id}
							{#if (pr.argv?.length ?? 0) > 0 || !pr.shell}
								<button class="cx sub" onclick={() => void pick(() => onlaunch(pr.id))}>
									default <span class="marg">{argvText(pr)}</span>
								</button>
							{:else}
								<button class="cx sub" onclick={() => void pick(() => onlaunch(pr.id))}>
									shell <span class="marg">{pr.shell}</span>
								</button>
							{/if}
							{#each pr.variants ?? [] as v (v.name)}
								<button
									class="cx sub"
									onclick={() => void pick(() => onlaunch(pr.id, { variant: v.name }))}
								>
									{v.name} <span class="marg">{v.argv.join(' ')}</span>
								</button>
							{/each}
							{#if pr.resume_cmd?.length}
								<button
									class="cx sub"
									onclick={() => void pick(() => onlaunch(pr.id, { resume: true }))}
								>
									↻ resume <span class="marg">{pr.resume_cmd.join(' ')}</span>
								</button>
							{/if}
						{/if}
					{:else}
						<button class="cx" onclick={() => void pick(() => onlaunch(pr.id))}>
							{pr.name} <span class="marg">{argvText(pr)}</span>
						</button>
					{/if}
				{/each}
				<button
					class="cx dim"
					onclick={() => {
						newopen = false;
						leftView.set('profiles');
					}}>manage profiles…</button
				>
				{#if err}<div class="merr">{err}</div>{/if}
			</div>
		{/if}

		<div class="ghead"><span>layout</span></div>
		<div class="presets">
			{#each coarsePtr() ? MOBILE_PRESETS : PRESETS as pre (pre.name)}
				{@const cur = $grid.map((r) => r.cells.length).join(',') === pre.shape.join(',')}
				<button class="preset" class:cur title={pre.name} onclick={() => onpreset(pre.shape)}>
					<span class="pg">
						{#each pre.shape as n}<span class="pgrow"
								>{#each Array(n) as _}<i></i>{/each}</span
							>{/each}
					</span>
				</button>
			{/each}
		</div>

		{#if unacked.length > 0}
			<div class="ghead"><span>alerts</span></div>
			{#each unacked as a (a.id)}
				<div class="arow">
					<span
						class="dot"
						class:live={a.level === 'action'}
						class:dead={a.level === 'warn'}
						class:needs={a.level === 'action'}
					></span>
					<span class="smain">
						<span class="sname">{a.transition}</span>
						<span class="ssub">{a.process || a.session_id} · {fmtAgo(now, a.at)}</span>
					</span>
					<button
						class="sact hist"
						title="acknowledge alert"
						onclick={(e) => {
							e.stopPropagation();
							void ackAlert(a.id);
						}}>ack</button
					>
				</div>
			{/each}
		{/if}

		{#if needs.length > 0}
			<div class="ghead">needs you</div>
			{#each needs as s (s.id)}
				{@render srow(s)}
			{/each}
		{/if}

		{#if running.length > 0}
			<div class="ghead">running</div>
			{#each running as s (s.id)}
				{@render srow(s)}
			{/each}
		{/if}

		{#if finished.length > 0}
			<div class="ghead">
				<span>finished</span>
				<button class="clearall" onclick={onclearfin}>clear all</button>
			</div>
			{#each finished as s (s.id)}
				{@render srow(s)}
			{/each}
		{/if}

		{#if $sessions.length === 0}
			<div class="empty">no sessions — tap + new</div>
		{/if}
	{/if}
</div>

<!-- one session row — dot, name + tool badge, state/wait subline, and the
     lifecycle action (live → end, finished → del). -->
{#snippet srow(s: Session)}
	<div
		class="srow"
		class:needs={needsYou(s)}
		role="button"
		tabindex="0"
		onclick={() => onopen(s)}
		onkeydown={() => onopen(s)}
	>
		<span class="dot" class:live={isLive(s)} class:dead={isFinished(s)} class:needs={needsYou(s)}
		></span>
		<span class="smain">
			<span class="stop">
				<span class="sname">{label(s)}</span>
				{#if toolBadge(s)}<span class="badge">{toolBadge(s)}</span>{/if}
				{#if pinUnv(s.id)}<span
						class="badge unv"
						title="adapter pin unverified — binary version outside the tested constraint"
						>unverified</span
					>{/if}
			</span>
			<span class="ssub" class:needs={needsYou(s)}>
				{subLabel(s, now)}{#if detachedOf(s)}<span class="det"> · unattended</span>{/if}
			</span>
		</span>
		{#if isLive(s)}
			<button
				class="sact end"
				title="terminate session — keeps the log row"
				onclick={(e) => onend(s.id, e)}>end</button
			>
		{:else}
			<button
				class="sact hist"
				title="purge state history only — keeps row + output"
				onclick={(e) => onpurge(s.id, e)}>hist</button
			>
			<button
				class="sact del"
				title="delete session + log + output"
				onclick={(e) => onend(s.id, e)}>del</button
			>
		{/if}
	</div>
{/snippet}

<style>
	.pwrap {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-y: auto;
		overflow-y: overlay;
		scrollbar-width: auto;
		scrollbar-color: rgba(255, 255, 255, 0.3) transparent;
		overscroll-behavior: contain;
	}
	.pwrap::-webkit-scrollbar {
		width: 12px;
		background: transparent;
	}
	.pwrap::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.3);
		border-radius: 4px;
	}
	.pwrap::-webkit-scrollbar-thumb:hover {
		background: rgba(255, 255, 255, 0.45);
	}
	.pwrap::-webkit-scrollbar-track {
		background: transparent;
	}
	.dhead {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 10px 14px;
		color: #6b7f9e;
		font: 12px ui-monospace, monospace;
		letter-spacing: 0.15em;
		flex-shrink: 0;
	}
	.tb {
		min-width: 44px;
		height: 44px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #141b26;
		color: #9db2ce;
		font: 15px/1 ui-monospace, monospace;
	}
	.tb:active {
		background: #2a3956;
	}
	/* +new inline expanding block */
	.newgrp {
		border-bottom: 1px solid #141b26;
		padding-bottom: 6px;
		flex-shrink: 0;
	}
	.cx {
		display: block;
		width: 100%;
		min-height: 40px;
		padding: 8px 14px;
		border: none;
		border-radius: 7px;
		background: none;
		color: #d7e1f0;
		font: 14px ui-monospace, monospace;
		text-align: left;
	}
	.cx:active,
	.cx:hover {
		background: #1e2a3d;
	}
	.cx.sm {
		flex: 0 0 auto;
		width: 44px;
	}
	.cx.dim {
		color: #5a6f92;
	}
	.cx.sub {
		padding-left: 28px;
		font-size: 13px;
		min-height: 36px;
	}
	.caret {
		float: right;
		color: #4a5568;
	}
	.mrow {
		display: flex;
		align-items: stretch;
	}
	.mrow .cx {
		flex: 1;
		min-width: 0;
	}
	.marg {
		display: block;
		font-size: 10px;
		color: #4a5568;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.srow,
	.arow {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 12px 14px;
		border-top: 1px solid #141b26;
		cursor: pointer;
	}
	.srow:active {
		background: #141b26;
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
	.srow.needs {
		background: #1a160c;
	}
	.smain {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.stop {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}
	.sname {
		font: 14px ui-monospace, monospace;
		color: #e6e6e6;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
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
	.ssub {
		font: 11px ui-monospace, monospace;
		color: #4a5568;
	}
	.ssub.needs {
		color: #f0b429;
	}
	.det {
		color: #7a5aa0;
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
	.presets {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		padding: 4px 10px 6px;
	}
	.preset {
		display: flex;
		align-items: center;
		justify-content: center;
		border: 1px solid #1c2634;
		border-radius: 8px;
		background: #0d1117;
		color: #6b7f9e;
		padding: 6px 9px;
		font: 10px ui-monospace, monospace;
		min-height: 40px;
	}
	.preset.cur {
		border-color: #4cc9f0;
		color: #e6f2ff;
	}
	.preset:active {
		background: #1c2a44;
	}
	.pg {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 26px;
	}
	.pgrow {
		display: flex;
		gap: 2px;
		flex: 1;
	}
	.pgrow i {
		flex: 1;
		height: 9px;
		background: currentColor;
		opacity: 0.55;
		border-radius: 1px;
	}
	.clearall {
		border: 0;
		background: transparent;
		color: #8a4a5a;
		font: 10px ui-monospace, monospace;
		letter-spacing: 0.12em;
		padding: 12px 10px;
		min-height: 44px;
	}
	.clearall:active {
		color: #ff8080;
	}
	.sact {
		min-width: 44px;
		height: 44px;
		border: 1px solid #22304a;
		border-radius: 8px;
		background: #141b26;
		font: 11px ui-monospace, monospace;
		flex-shrink: 0;
	}
	.sact.end {
		color: #d0a24a;
	}
	.sact.end:active {
		background: #2a2410;
	}
	.sact.hist {
		color: #6b7f9e;
	}
	.sact.hist:active {
		background: #1c2a44;
		color: #9db2ce;
	}
	.sact.del {
		color: #8a4a5a;
	}
	.sact.del:active {
		background: #3a1f1f;
		color: #ff8080;
	}
	.empty {
		padding: 24px;
		text-align: center;
		color: #33415c;
		font: 13px ui-monospace, monospace;
	}
	/* profile form + variant editor */
	.pform {
		padding: 4px 14px 10px;
		display: flex;
		flex-direction: column;
		gap: 6px;
		border-bottom: 1px solid #141b26;
	}
	.mt {
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
	select.mt {
		appearance: none;
	}
	.merr {
		font: 11px ui-monospace, monospace;
		color: #ff8080;
		padding: 0 2px;
	}
	.plist-err {
		padding: 6px 14px;
	}
	.mcta {
		display: flex;
		gap: 6px;
		justify-content: flex-end;
	}
	.msave {
		min-height: 38px;
		padding: 0 16px;
		border: 1px solid #1d3550;
		border-radius: 8px;
		background: #0e1a26;
		color: #4cc9f0;
		font: 12px ui-monospace, monospace;
	}
	.msave:disabled {
		opacity: 0.4;
	}
	.mcx {
		min-height: 38px;
		padding: 0 12px;
		border: 0;
		background: transparent;
		color: #6b7f9e;
		font: 12px ui-monospace, monospace;
	}
	.vhead {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 6px 0 2px;
		color: #3d4d68;
		font: 10px ui-monospace, monospace;
		letter-spacing: 0.18em;
	}
	.vhead .cx {
		width: auto;
		min-height: 32px;
		padding: 4px 10px;
	}
	.vrow {
		display: flex;
		gap: 6px;
	}
	.vrow .mt {
		flex: 1;
		min-width: 0;
	}
</style>
