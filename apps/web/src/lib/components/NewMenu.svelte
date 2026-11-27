<!-- Ref: .teos/plans/2026-11-26-side-panels-design.md — the topbar +
     spawn menu: shell quick-spawns, then launch profiles with inline
     variant sub-selection, then "manage profiles…" → the left panel's
     profiles view. A dropdown, not an in-panel block. -->
<script lang="ts">
	import { api, ApiError, type LaunchProfile } from '$lib/api';
	import { profiles, refreshProfiles } from '$lib/stores';

	let {
		onspawn,
		onlaunch,
		onmanage,
		onclose
	}: {
		onspawn: (shell?: string) => Promise<void> | void;
		onlaunch: (id: number, o?: { variant?: string; resume?: boolean }) => Promise<void> | void;
		onmanage: () => void;
		onclose: () => void;
	} = $props();

	let err = $state('');
	// expProf is the profile id whose launch variants are open.
	let expProf = $state(0);
	const expandable = (pr: LaunchProfile) =>
		(pr.variants?.length ?? 0) > 0 || (pr.resume_cmd?.length ?? 0) > 0;
	const argvText = (pr: LaunchProfile) => (pr.argv ?? []).join(' ') || pr.shell;

	// Profiles are lazy — the menu fetches only when opened.
	$effect(() => {
		void refreshProfiles();
	});

	async function pick(fn: () => Promise<void> | void) {
		err = '';
		try {
			await fn();
			onclose();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'launch failed';
		}
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions — click-away scrim -->
<div class="newback" onpointerdown={onclose} oncontextmenu={(e) => e.preventDefault()}></div>
<div class="newmenu" role="menu">
	<div class="ghead"><span>shell</span></div>
	<button class="cx" role="menuitem" onclick={() => void pick(() => onspawn('/bin/bash'))}>bash</button>
	<button class="cx" role="menuitem" onclick={() => void pick(() => onspawn('/bin/zsh'))}>zsh</button>
	<button class="cx" role="menuitem" onclick={() => void pick(() => onspawn('/bin/fish'))}>fish</button>
	<div class="ghead"><span>agents</span></div>
	{#each $profiles as pr (pr.id)}
		{#if expandable(pr)}
			<button class="cx" role="menuitem" onclick={() => (expProf = expProf === pr.id ? 0 : pr.id)}>
				{pr.name} <span class="marg">{argvText(pr)}</span>
				<span class="caret">{expProf === pr.id ? '▾' : '▸'}</span>
			</button>
			{#if expProf === pr.id}
				{#if (pr.argv?.length ?? 0) > 0 || !pr.shell}
					<button class="cx sub" role="menuitem" onclick={() => void pick(() => onlaunch(pr.id))}>
						default <span class="marg">{argvText(pr)}</span>
					</button>
				{:else}
					<button class="cx sub" role="menuitem" onclick={() => void pick(() => onlaunch(pr.id))}>
						shell <span class="marg">{pr.shell}</span>
					</button>
				{/if}
				{#each pr.variants ?? [] as v}
					<button
						class="cx sub"
						role="menuitem"
						onclick={() => void pick(() => onlaunch(pr.id, { variant: v.name }))}
					>
						{v.name} <span class="marg">{v.argv.join(' ')}</span>
					</button>
				{/each}
				{#if pr.resume_cmd?.length}
					<button
						class="cx sub"
						role="menuitem"
						onclick={() => void pick(() => onlaunch(pr.id, { resume: true }))}
					>
						↻ resume <span class="marg">{pr.resume_cmd.join(' ')}</span>
					</button>
				{/if}
			{/if}
		{:else}
			<button class="cx" role="menuitem" onclick={() => void pick(() => onlaunch(pr.id))}>
				{pr.name}
				<span class="marg">
					{argvText(pr)}{pr.variants?.length ? ` · ${pr.variants.length} variants` : ''}
				</span>
			</button>
		{/if}
	{:else}
		<div class="empty">no profiles yet</div>
	{/each}
	<button
		class="cx manage"
		role="menuitem"
		onclick={() => {
			onclose();
			onmanage();
		}}>manage profiles…</button
	>
	{#if err}<div class="merr">{err}</div>{/if}
</div>

<style>
	.newback {
		position: fixed;
		inset: 0;
		z-index: 29;
	}
	.newmenu {
		position: absolute;
		/* under the topbar's pinned + button — same 57px math as .panel */
		top: calc(57px + env(safe-area-inset-top, 0px));
		right: 8px;
		z-index: 30;
		width: min(320px, 88vw);
		max-height: min(70vh, 560px);
		overflow-y: auto;
		background: #0d1117;
		border: 1px solid #22304a;
		border-radius: 10px;
		padding: 6px;
		box-shadow: 0 8px 32px #000c;
	}
	.ghead {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 10px 10px 4px;
		font: 10px ui-monospace, monospace;
		letter-spacing: 0.12em;
		text-transform: uppercase;
		color: #4a5568;
	}
	.cx {
		display: flex;
		align-items: center;
		gap: 6px;
		width: 100%;
		padding: 8px 10px;
		border: 0;
		border-radius: 7px;
		background: transparent;
		color: #9ab;
		font: 12px ui-monospace, monospace;
		text-align: left;
		cursor: pointer;
	}
	.cx:hover,
	.cx:active {
		background: #16203a;
		color: #e6f2ff;
	}
	.cx.sub {
		padding-left: 26px;
		font-size: 11px;
	}
	.cx.manage {
		margin-top: 4px;
		border-top: 1px solid #1a2334;
		border-radius: 0 0 7px 7px;
		color: #7ea4d4;
	}
	.marg {
		margin-left: auto;
		font-size: 10px;
		color: #4a5568;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 55%;
	}
	.caret {
		color: #4a5568;
		font-size: 10px;
	}
	.empty {
		padding: 10px;
		font: 11px ui-monospace, monospace;
		color: #4a5568;
		text-align: center;
	}
	.merr {
		margin: 6px 4px 2px;
		padding: 6px 8px;
		border-radius: 6px;
		background: #2a1420;
		border: 1px solid #5c2438;
		color: #ff8080;
		font: 11px ui-monospace, monospace;
	}
</style>
