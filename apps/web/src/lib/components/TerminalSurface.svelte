<!-- Ref: .teos/sections/11-web-ui.md — TerminalSurface: xterm.js surface,
     one per pane; touch layer resolves TUI-mouse vs browser-selection. -->
<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { Terminal } from '@xterm/xterm';
	import { FitAddon } from '@xterm/addon-fit';
	import { SearchAddon } from '@xterm/addon-search';
	import { SessionClient } from '$lib/ws';
	import { touchMode } from '$lib/stores';
	import '@xterm/xterm/css/xterm.css';

	let { sessionId, active = false }: { sessionId: string; active?: boolean } = $props();

	let el: HTMLDivElement;
	let term: Terminal;
	let fit: FitAddon;
	let client: SessionClient;
	let search: SearchAddon;
	let dead = $state(false);
	let status = $state<'connecting' | 'live' | 'reconnecting' | 'dead'>('connecting');
	let searchOpen = $state(false);
	let searchQ = $state('');

	const encoder = new TextEncoder();

	onMount(() => {
		term = new Terminal({
			fontFamily: 'ui-monospace, "JetBrains Mono", Menlo, monospace',
			fontSize: 14,
			cursorBlink: true,
			allowProposedApi: true,
			scrollback: 2000,
			theme: {
				background: '#0a0e14',
				foreground: '#e6e6e6',
				cursor: '#4cc9f0',
				selectionBackground: '#264f78'
			}
		});
		fit = new FitAddon();
		search = new SearchAddon();
		term.loadAddon(fit);
		term.loadAddon(search);
		term.open(el);

		// WebGL renderer — graceful fallback to canvas.
		void import('@xterm/addon-webgl')
			.then(({ WebglAddon }) => term.loadAddon(new WebglAddon()))
			.catch(() => {
				/* canvas fallback is fine */
			});

		// OSC 52 → system clipboard (the bridge that makes remote copy work).
		term.parser.registerOscHandler(52, (data: string) => {
			const payload = data.split(';').pop() ?? '';
			if (payload && payload !== '?') {
				try {
					const text = atob(payload);
					void navigator.clipboard?.writeText(text);
					toast('copied to clipboard');
				} catch {
					/* not base64 — ignore */
				}
			}
			return true;
		});

		// Physical keys and key-bar bytes share one input path.
		term.onData((d) => client?.send(d));

		client = new SessionClient(sessionId, {
			data: (p) => term.write(p),
			open: () => {
				status = 'live';
				sendResize();
			},
			eof: (code) => {
				status = 'dead';
				dead = true;
				term.write(`\r\n\x1b[2m[process exited ${code}] — close or restart from the session list\x1b[0m\r\n`);
			},
			error: (code, msg) => {
				if (code !== 'resync_overflow') toast(`${code}: ${msg}`);
			},
			close: () => {
				if (status !== 'dead') status = 'reconnecting';
			}
		});
		client.connect();

		const ro = new ResizeObserver(() => sendResize());
		ro.observe(el);
		return () => ro.disconnect();
	});

	onDestroy(() => {
		client?.detach();
		term?.dispose();
	});

	function sendResize() {
		try {
			fit?.fit();
			if (term?.cols && term?.rows) client?.resize(term.cols, term.rows);
		} catch {
			/* hidden pane — fit on next show */
		}
	}

	// Called by KeyBar — same path as physical input.
	export function inject(seq: string) {
		client?.send(seq);
		term?.focus();
	}

	export function pasteText(t: string) {
		// Bracketed paste so CLIs don't execute on newlines.
		client?.send(`\x1b[200~${t}\x1b[201~`);
	}

	export function focus() {
		term?.focus();
		sendResize();
	}

	$effect(() => {
		if (active) focus();
	});

	function doSearch() {
		search.findNext(searchQ, { caseSensitive: false, incremental: true });
	}

	let toastMsg = $state('');
	let toastTimer: ReturnType<typeof setTimeout> | null = null;
	function toast(m: string) {
		toastMsg = m;
		if (toastTimer) clearTimeout(toastTimer);
		toastTimer = setTimeout(() => (toastMsg = ''), 1800);
	}
</script>

<div class="tsurface" class:inactive={!active} class:select-mode={$touchMode === 'select'}>
	<div bind:this={el} class="term"></div>

	{#if status === 'reconnecting'}
		<div class="banner">reconnecting…</div>
	{/if}
	{#if dead}
		<div class="banner dead">exited</div>
	{/if}
	{#if toastMsg}
		<div class="toast">{toastMsg}</div>
	{/if}

	<div class="tools" class:open={searchOpen}>
		<button
			class="tool"
			aria-label="search"
			onclick={() => {
				searchOpen = !searchOpen;
				if (!searchOpen) search?.clearDecorations?.();
			}}>⌕</button
		>
		{#if searchOpen}
			<input
				class="searchbox"
				bind:value={searchQ}
				oninput={doSearch}
				placeholder="search buffer"
				autocapitalize="off"
				autocorrect="off"
			/>
		{/if}
	</div>
</div>

<style>
	.tsurface {
		position: relative;
		height: 100%;
		min-height: 0;
		background: #0a0e14;
		overflow: hidden;
	}
	.term {
		position: absolute;
		inset: 0;
		padding: 4px 6px;
	}
	.inactive .term {
		opacity: 0.92;
	}
	/* select mode: text selection wins over synthetic mouse reporting */
	.select-mode :global(.xterm) {
		-webkit-user-select: text;
		user-select: text;
	}
	.banner {
		position: absolute;
		top: 8px;
		right: 8px;
		background: #4a3419;
		color: #ffd479;
		font: 12px/1 ui-monospace, monospace;
		padding: 6px 10px;
		border-radius: 6px;
		pointer-events: none;
	}
	.banner.dead {
		background: #3a1f1f;
		color: #ff8080;
	}
	.toast {
		position: absolute;
		bottom: 12px;
		left: 50%;
		transform: translateX(-50%);
		background: #1f6f43;
		color: #c9f7dd;
		font: 12px/1 ui-monospace, monospace;
		padding: 7px 12px;
		border-radius: 7px;
		pointer-events: none;
	}
	.tools {
		position: absolute;
		top: 6px;
		right: 6px;
		display: flex;
		gap: 6px;
		align-items: center;
	}
	.tool {
		min-width: 36px;
		min-height: 36px;
		border: 0;
		border-radius: 8px;
		background: rgba(28, 36, 48, 0.75);
		color: #9db2ce;
		font-size: 17px;
	}
	.tool:active {
		background: #33415c;
	}
	.searchbox {
		height: 36px;
		width: 44vw;
		max-width: 220px;
		border: 1px solid #33415c;
		border-radius: 8px;
		background: #11161f;
		color: #e6e6e6;
		padding: 0 10px;
		font: 13px ui-monospace, monospace;
	}
</style>
