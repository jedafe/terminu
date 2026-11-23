<!-- Ref: .teos/sections/11-web-ui.md — the mobile-first shell:
     session drawer + pane grid + key bar. Panes are views over
     server-owned sessions — closing one never kills the process. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Session } from '$lib/api';
	import {
		sessions,
		panes,
		activePane,
		newPane,
		closePane,
		refreshSessions,
		saveWorkspace,
		restoreWorkspace,
		wireEvents,
		drawerOpen,
		touchMode
	} from '$lib/stores';
	import TerminalSurface from '$lib/components/TerminalSurface.svelte';
	import KeyBar from '$lib/components/KeyBar.svelte';

	let surfaces: Record<string, TerminalSurface | undefined> = {};
	let saveTimer: ReturnType<typeof setTimeout> | null = null;

	onMount(async () => {
		await refreshSessions();
		await restoreWorkspace();
		wireEvents();
		if ($panes.length === 0) {
			const live = $sessions.find((s) => isLive(s));
			if (live) newPane(live.id);
			else await spawn();
		}
	});

	function isLive(s: Session) {
		return ['starting', 'working', 'waiting-for-input', 'undetectable', 'detached'].includes(s.state);
	}

	function scheduleSave() {
		if (saveTimer) clearTimeout(saveTimer);
		saveTimer = setTimeout(() => void saveWorkspace(), 1200);
	}

	async function spawn() {
		const s = await api.spawn({ cols: 120, rows: 30 });
		await refreshSessions();
		newPane(s.id);
		scheduleSave();
	}

	function openSession(s: Session) {
		const existing = $panes.find((p) => p.sessionId === s.id);
		if (existing) {
			activePane.set(existing.id);
		} else {
			newPane(s.id);
		}
		drawerOpen.set(false);
		scheduleSave();
	}

	async function killSession(id: string, e: Event) {
		e.stopPropagation();
		await api.kill(id);
		for (const p of $panes.filter((p) => p.sessionId === id)) closePane(p.id);
		await refreshSessions();
	}

	function inject(seq: string) {
		const ap = $activePane;
		if (ap) surfaces[ap]?.inject(seq);
	}

	async function paste() {
		try {
			const t = await navigator.clipboard.readText();
			if (t) {
				const ap = $activePane;
				if (ap) surfaces[ap]?.pasteText(t);
			}
		} catch {
			/* clipboard perm denied */
		}
	}

	function label(s: Session | undefined): string {
		if (!s) return '?';
		return s.name || s.shell.split('/').pop() || s.id;
	}

	function sessionOf(id: string): Session | undefined {
		return $sessions.find((s) => s.id === id);
	}
</script>

<div class="shell">
	<!-- top bar: sessions drawer + panes + mode toggle -->
	<header class="topbar">
		<button class="tb" aria-label="sessions" onclick={() => drawerOpen.update((v) => !v)}>
			☰
		</button>
		<div class="panes">
			{#each $panes as p (p.id)}
				<button
					class="pane-tab"
					class:active={$activePane === p.id}
					onclick={() => {
						activePane.set(p.id);
						scheduleSave();
					}}
				>
					{label(sessionOf(p.sessionId))}
				</button>
			{/each}
			<button class="tb add" aria-label="new session" onclick={spawn}>+</button>
		</div>
		<button
			class="tb mode"
			class:on={$touchMode === 'mouse'}
			title="touch mode: select = copy text, mouse = drive TUIs"
			onclick={() => touchMode.update((m) => (m === 'select' ? 'mouse' : 'select'))}
		>
			{$touchMode === 'select' ? '⇱' : '🖱'}
		</button>
	</header>

	<!-- session drawer -->
	{#if $drawerOpen}
		<div class="drawer">
			<div class="dhead">
				<span>sessions</span>
				<button class="tb" onclick={spawn}>+ new</button>
			</div>
			{#each $sessions as s (s.id)}
				<div class="srow" role="button" tabindex="0" onclick={() => openSession(s)} onkeydown={() => openSession(s)}>
					<span class="dot" class:live={isLive(s)} class:dead={!isLive(s)}></span>
					<span class="sname">{label(s)}</span>
					<span class="smeta">{s.state}</span>
					<button class="skill" onclick={(e) => killSession(s.id, e)}>✕</button>
				</div>
			{/each}
			{#if $sessions.length === 0}
				<div class="empty">no sessions — tap + new</div>
			{/if}
		</div>
	{/if}

	<!-- terminal grid -->
	<main class="grid" class:single={$panes.length <= 1}>
		{#each $panes as p (p.id)}
			<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
			<section
				class="pane"
				class:focused={$activePane === p.id}
				role="group"
				onclick={() => activePane.set(p.id)}
			>
				{#if $panes.length > 1}
					<div class="ptitle">
						{label(sessionOf(p.sessionId))}
						<button class="pclose" onclick={(e) => { e.stopPropagation(); closePane(p.id); scheduleSave(); }}>✕</button>
					</div>
				{/if}
				<div class="pterm">
					<TerminalSurface
						bind:this={surfaces[p.id]}
						sessionId={p.sessionId}
						active={$activePane === p.id}
					/>
				</div>
			</section>
		{/each}
		{#if $panes.length === 0}
			<div class="empty-main">
				<button class="bigspawn" onclick={spawn}>open a session</button>
			</div>
		{/if}
	</main>

	<KeyBar oninject={inject} onpaste={paste} />
</div>

<style>
	.shell {
		height: 100dvh;
		display: flex;
		flex-direction: column;
		background: #0a0e14;
	}
	.topbar {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 6px 8px;
		padding-top: calc(6px + env(safe-area-inset-top, 0px));
		background: #0d1117;
		border-bottom: 1px solid #1a2334;
		flex-shrink: 0;
	}
	.tb {
		min-width: 44px;
		height: 38px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #141b26;
		color: #9db2ce;
		font: 15px/1 ui-monospace, monospace;
	}
	.tb:active {
		background: #2a3956;
	}
	.panes {
		flex: 1;
		display: flex;
		gap: 4px;
		overflow-x: auto;
		scrollbar-width: none;
	}
	.panes::-webkit-scrollbar {
		display: none;
	}
	.pane-tab {
		height: 38px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #11161f;
		color: #6b7f9e;
		font: 13px ui-monospace, monospace;
		padding: 0 14px;
		white-space: nowrap;
		flex-shrink: 0;
	}
	.pane-tab.active {
		background: #1c2a44;
		border-color: #4cc9f0;
		color: #e6f2ff;
	}
	.mode.on {
		background: #1f4d3a;
		border-color: #2f7d5b;
	}
	.drawer {
		position: absolute;
		top: calc(50px + env(safe-area-inset-top, 0px));
		left: 0;
		right: 0;
		z-index: 20;
		background: #0d1117f2;
		backdrop-filter: blur(8px);
		border-bottom: 1px solid #22304a;
		max-height: 60dvh;
		overflow-y: auto;
	}
	.dhead {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 10px 14px;
		color: #6b7f9e;
		font: 12px ui-monospace, monospace;
		letter-spacing: 0.15em;
	}
	.srow {
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
	.dot.dead {
		background: #5a3540;
	}
	.sname {
		flex: 1;
		font: 14px ui-monospace, monospace;
		color: #e6e6e6;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.smeta {
		font: 11px ui-monospace, monospace;
		color: #4a5568;
	}
	.skill {
		min-width: 36px;
		height: 36px;
		border: 0;
		border-radius: 8px;
		background: transparent;
		color: #5a3540;
		font-size: 14px;
	}
	.skill:active {
		background: #3a1f1f;
		color: #ff8080;
	}
	.empty,
	.empty-main {
		padding: 24px;
		text-align: center;
		color: #33415c;
		font: 13px ui-monospace, monospace;
	}
	.grid {
		flex: 1;
		display: grid;
		grid-auto-rows: 1fr;
		gap: 2px;
		min-height: 0;
		background: #06090d;
	}
	@media (min-width: 900px) {
		.grid {
			grid-auto-flow: column;
			grid-auto-columns: 1fr;
		}
	}
	.pane {
		display: flex;
		flex-direction: column;
		min-height: 0;
		min-width: 0;
		background: #0a0e14;
		border: 1px solid transparent;
	}
	.pane.focused {
		border-color: #22304a;
	}
	.ptitle {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 3px 8px;
		background: #0d1117;
		color: #6b7f9e;
		font: 11px ui-monospace, monospace;
		flex-shrink: 0;
	}
	.pclose {
		border: 0;
		background: transparent;
		color: #5a3540;
		font-size: 12px;
		padding: 4px 8px;
	}
	.pterm {
		flex: 1;
		min-height: 0;
	}
	.bigspawn {
		height: 56px;
		padding: 0 28px;
		border: 1px solid #22304a;
		border-radius: 12px;
		background: #141b26;
		color: #9db2ce;
		font: 15px ui-monospace, monospace;
	}
</style>
