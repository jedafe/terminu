<!-- Ref: .teos/sections/04 — the ⌘ surface: a modal inventory of
     detected CLIs (running / curated / discovered / PATH) → picking
     one binds the right panel's commands view to that app. The first
     row returns to the armed session's live context. -->
<script lang="ts">
	import { api, type AppEntry } from '$lib/api';
	import { agentStates } from '$lib/stores';
	import { onMount, tick } from 'svelte';

	let { onpick, onclose }: { onpick: (app: string) => void; onclose: () => void } = $props();

	let q = $state('');
	let apps = $state<AppEntry[]>([]);
	let err = $state('');
	let sel = $state(0);
	let inputEl: HTMLInputElement | undefined = $state();
	let listEl: HTMLDivElement | undefined = $state();

	// Live foreground tools/processes — the "running" group at top.
	const live = $derived(
		new Set(
			Object.values($agentStates).flatMap((a) =>
				[a.tool, a.process].filter((x): x is string => !!x)
			)
		)
	);
	const needle = $derived(q.trim().toLowerCase());
	const filtered = $derived(
		needle ? apps.filter((a) => a.name.toLowerCase().includes(needle)) : apps
	);
	// Group order: what runs now, what's curated, what was scraped,
	// then bare PATH names. Dedup across groups — running wins.
	const groups = $derived.by(() => {
		const seen = new Set<string>();
		const take = (fn: (a: AppEntry) => boolean, label: string) => {
			const rows = filtered.filter((a) => !seen.has(a.name) && fn(a));
			for (const a of rows) seen.add(a.name);
			return { label, rows };
		};
		return [
			take((a) => live.has(a.name), 'running'),
			take((a) => (a.sources ?? []).some((s) => s === 'builtin' || s === 'custom'), 'curated'),
			take((a) => (a.sources ?? []).includes('discovered'), 'discovered'),
			take(() => true, 'on $PATH')
		].filter((g) => g.rows.length > 0);
	});
	// Flat list for ↑↓ navigation — group headers aren't selectable.
	const flat = $derived(groups.flatMap((g) => g.rows));
	$effect(() => {
		sel = Math.min(sel, Math.max(flat.length - 1, 0));
	});

	async function load() {
		err = '';
		try {
			const res = await api.apps();
			apps = res.apps;
		} catch {
			err = 'inventory unavailable';
		}
	}

	function pick(a: AppEntry) {
		onpick(a.name);
	}
	function pickSession() {
		onpick('session');
	}
	function keydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			sel = Math.min(sel + 1, flat.length - 1);
			scrollSel();
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			sel = Math.max(sel - 1, 0);
			scrollSel();
		} else if (e.key === 'Enter') {
			e.preventDefault();
			const a = flat[sel];
			if (a) pick(a);
		}
	}
	function scrollSel() {
		void tick().then(() => {
			listEl?.querySelector('.arow.sel')?.scrollIntoView({ block: 'nearest' });
		});
	}

	onMount(() => {
		void load();
		void tick().then(() => inputEl?.focus());
	});
</script>

<!-- svelte-ignore a11y_no_static_element_interactions — click-away scrim -->
<div class="aback" onpointerdown={onclose} oncontextmenu={(e) => e.preventDefault()}></div>
<div class="amodal" role="dialog" aria-label="applications" tabindex="-1" onkeydown={keydown}>
	<div class="ahead">
		<span class="aglyph">⌘</span>
		<input
			bind:this={inputEl}
			class="aq"
			placeholder="application…"
			autocapitalize="off"
			autocorrect="off"
			spellcheck="false"
			bind:value={q}
		/>
		<button class="ax" aria-label="close" onclick={onclose}>✕</button>
	</div>
	<button class="arow asess" onclick={pickSession}>
		<span class="aname">◈ session</span>
		<span class="ameta">the armed session's live context</span>
	</button>
	<div class="alist" bind:this={listEl}>
		{#each groups as g (g.label)}
			<div class="agroup">{g.label}</div>
			{#each g.rows as a (a.name)}
				{@const i = flat.indexOf(a)}
				<button
					class="arow"
					class:sel={i === sel}
					onpointerenter={() => (sel = i)}
					onclick={() => pick(a)}
				>
					<span class="aname">{a.name}</span>
					<span class="ameta">
						{#if live.has(a.name)}<span class="adot"></span>{/if}
						{a.manifests > 0 ? `${a.manifests} cmds` : a.path || 'registry only'}
					</span>
				</button>
			{/each}
		{/each}
		{#if flat.length === 0 && !err}
			<div class="aempty">no applications match</div>
		{/if}
		{#if err}<div class="aerr">{err}</div>{/if}
	</div>
	<div class="afoot">↵ open · ↑↓ pick · esc close</div>
</div>

<style>
	.aback {
		position: fixed;
		inset: 0;
		z-index: 44;
		background: #060a10cc;
	}
	.amodal {
		position: fixed;
		/* under the topbar, centered — reads like a launcher palette */
		top: calc(64px + env(safe-area-inset-top, 0px));
		left: 50%;
		transform: translateX(-50%);
		z-index: 45;
		width: min(560px, 92vw);
		max-height: min(72vh, 640px);
		display: flex;
		flex-direction: column;
		background: #0d1117;
		border: 1px solid #22304a;
		border-radius: 12px;
		box-shadow: 0 16px 48px #000d;
		overflow: hidden;
	}
	.ahead {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 10px 12px;
		border-bottom: 1px solid #141b26;
	}
	.aglyph {
		color: #4a5568;
		font: 15px ui-monospace, monospace;
	}
	.aq {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: #e6f2ff;
		font: 14px ui-monospace, monospace;
		outline: none;
	}
	.ax {
		border: 0;
		background: none;
		color: #4a5568;
		font: 14px ui-monospace, monospace;
		cursor: pointer;
		padding: 4px;
	}
	.ax:hover {
		color: #e6f2ff;
	}
	.alist {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: 4px;
	}
	.agroup {
		padding: 10px 10px 4px;
		font: 10px ui-monospace, monospace;
		letter-spacing: 0.12em;
		text-transform: uppercase;
		color: #4a5568;
	}
	.arow {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 8px 10px;
		border: 0;
		border-radius: 7px;
		background: transparent;
		color: #9ab;
		font: 13px ui-monospace, monospace;
		text-align: left;
		cursor: pointer;
	}
	.arow:hover,
	.arow.sel {
		background: #16203a;
		color: #e6f2ff;
	}
	.arow.asess {
		margin: 4px;
		width: calc(100% - 8px);
		border-bottom: 1px solid #141b26;
		border-radius: 7px 7px 0 0;
	}
	.aname {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ameta {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 10px;
		color: #4a5568;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 55%;
	}
	.adot {
		display: inline-block;
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: #4ade80;
	}
	.aempty,
	.aerr {
		padding: 16px 12px;
		font: 12px ui-monospace, monospace;
		color: #4a5568;
		text-align: center;
	}
	.aerr {
		color: #ff8080;
	}
	.afoot {
		padding: 8px 12px;
		border-top: 1px solid #141b26;
		font: 10px ui-monospace, monospace;
		color: #33415c;
	}
</style>
