<!-- Ref: .teos/sections/11-web-ui.md — TerminalSurface: xterm.js surface,
     one per pane; touch layer resolves TUI-mouse vs browser-selection. -->
<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { get } from 'svelte/store';
	import { Terminal } from '@xterm/xterm';
	import { FitAddon } from '@xterm/addon-fit';
	import { SearchAddon } from '@xterm/addon-search';
	import { Unicode11Addon } from '@xterm/addon-unicode11';
	import { SessionClient, keystrokeToBytes, type PromptFrame } from '$lib/ws';
	import { touchMode } from '$lib/stores';
	import '@xterm/xterm/css/xterm.css';

	let {
		sessionId,
		active = false,
		onrequestinput
	}: { sessionId: string; active?: boolean; onrequestinput?: () => void } = $props();

	let el: HTMLDivElement;
	let term: Terminal;
	let fit: FitAddon;
	let client: SessionClient;
	let search: SearchAddon;
	let dead = $state(false);
	let status = $state<'connecting' | 'live' | 'reconnecting' | 'dead'>('connecting');
	let searchOpen = $state(false);
	let searchQ = $state('');
	let prompt = $state<PromptFrame | null>(null);
	let maybeWaiting = $state(false);
	let freeReply = $state('');

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
		term.loadAddon(new Unicode11Addon());
		term.unicode.activeVersion = '11';
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
			},
			// Ref: .teos/sections/05-agent-session-management.md — prompt
			// overlay. Low-confidence waits hint only, never buttons.
			state: (m) => {
				maybeWaiting = m.state === 'waiting-for-input' && m.confidence === 'low' && !prompt;
			},
			prompt: (p) => {
				prompt = p;
				maybeWaiting = false;
			},
			promptClear: () => {
				prompt = null;
				maybeWaiting = false;
			}
		});
		client.connect();

		armTouchLayer();

		const ro = new ResizeObserver(() => sendResize());
		ro.observe(el);
		return () => ro.disconnect();
	});

	onDestroy(() => {
		cancelLp();
		client?.detach();
		term?.dispose();
	});

	// --- touch layer (§02/§11): the pointer contract on mobile ---
	// mouse mode: xterm's native path — taps report to the TUI when it
	//   holds the mouse; long-press or contextmenu re-dispatches a
	//   right-button tap so the TUI sees a real right click.
	// select mode: primary mousedown is re-dispatched shift-modified —
	//   xterm then takes its built-in forced-selection path (the desktop
	//   Shift+drag escape) even while a TUI owns the mouse. On touch,
	//   long-press copies the tapped row — canvas/WebGL have no DOM text
	//   for the browser's own selection.
	const LP_MS = 450;
	const LP_MOVE = 12;
	let lpTimer: ReturnType<typeof setTimeout> | undefined;
	let lpPt: { x: number; y: number } | null = null;
	let lpAt = 0;
	let lastRightClick = 0;

	function cancelLp() {
		if (lpTimer) clearTimeout(lpTimer);
		lpTimer = undefined;
		lpPt = null;
	}

	function rightClickAt(x: number, y: number) {
		const t = (document.elementFromPoint(x, y) ?? el) as Element;
		for (const type of ['mousedown', 'mouseup']) {
			const ev = new MouseEvent(type, {
				bubbles: true,
				cancelable: true,
				button: 2,
				clientX: x,
				clientY: y
			});
			(ev as unknown as { __tsynth: boolean }).__tsynth = true;
			t.dispatchEvent(ev);
		}
		lastRightClick = Date.now();
	}

	// Map a viewport Y to a buffer row for select-mode line grabs.
	function rowAt(y: number) {
		const scr = el.querySelector('.xterm-screen') ?? el;
		const r = scr.getBoundingClientRect();
		const cell = r.height / Math.max(1, term.rows);
		const rel = Math.min(term.rows - 1, Math.max(0, Math.floor((y - r.top) / cell)));
		return rel + term.buffer.active.viewportY;
	}

	function lpFire() {
		const p = lpPt;
		cancelLp();
		if (!p) return;
		if (get(touchMode) === 'mouse') {
			rightClickAt(p.x, p.y);
			return;
		}
		const row = rowAt(p.y);
		term.selectLines(row, row);
		const text = term.getSelection();
		if (text) {
			void navigator.clipboard?.writeText(text);
			toast('line copied');
		}
	}

	// Short tap: a TUI holding the mouse owns the click (xterm's native
	// touch path already reported it). Otherwise the tap asks for the
	// input line — the terminal's whole surface is the on-ramp to typing.
	function tap() {
		if (get(touchMode) === 'mouse' && (term?.modes?.mouseTrackingMode ?? 'none') !== 'none') return;
		onrequestinput?.();
	}

	function armTouchLayer() {
		el.addEventListener('pointerdown', (e) => {
			if (e.pointerType !== 'touch') return;
			lpPt = { x: e.clientX, y: e.clientY };
			lpAt = Date.now();
			lpTimer = setTimeout(lpFire, LP_MS);
		});
		el.addEventListener('pointermove', (e) => {
			if (lpPt && Math.hypot(e.clientX - lpPt.x, e.clientY - lpPt.y) > LP_MOVE) cancelLp();
		});
		// lpPt surviving to pointerup = press shorter than LP_MS with no
		// move (long-press and scroll both clear it first).
		el.addEventListener('pointerup', (e) => {
			if (e.pointerType === 'touch' && lpPt && Date.now() - lpAt < LP_MS) tap();
			cancelLp();
		});
		el.addEventListener('pointercancel', cancelLp);

		el.addEventListener('contextmenu', (e) => {
			e.preventDefault();
			// Android fires contextmenu for the same long-press lpFire already
			// handled — the window dedupes; desktop right-clicks pass through.
			if (get(touchMode) === 'mouse' && Date.now() - lastRightClick > 600) {
				rightClickAt(e.clientX, e.clientY);
			}
		});

		// Capture beats xterm's own bubble listener: the real press never
		// reaches it, the shift-marked copy does → forced selection.
		el.addEventListener(
			'mousedown',
			(e) => {
				if (get(touchMode) !== 'select' || e.button !== 0) return;
				if ((e as unknown as { __tsynth?: boolean }).__tsynth) return;
				e.stopPropagation();
				const ev = new MouseEvent('mousedown', {
					bubbles: true,
					cancelable: true,
					shiftKey: true,
					button: 0,
					clientX: e.clientX,
					clientY: e.clientY
				});
				(ev as unknown as { __tsynth: boolean }).__tsynth = true;
				(e.target as Element).dispatchEvent(ev);
			},
			true
		);
	}

	function sendResize() {
		try {
			fit?.fit();
			if (term?.cols && term?.rows) client?.resize(term.cols, term.rows);
		} catch {
			/* hidden pane — fit on next show */
		}
	}

	// Raw input path — no focus steal (the InputLine owns the keyboard).
	export function send(seq: string) {
		client?.send(seq);
	}

	// Called by KeyBar — same path as physical input. On touch devices the
	// InputLine owns the OS keyboard: focusing xterm's hidden textarea
	// would blur it and drop the keyboard after every keybar press.
	export function inject(seq: string) {
		client?.send(seq);
		if (matchMedia('(hover: hover)').matches) term?.focus();
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

	{#if prompt}
		<div class="poverlay">
			<div class="ptitle">
				{prompt.title}
				<button class="px" aria-label="dismiss" onclick={() => (prompt = null)}>✕</button>
			</div>
			{#if prompt.kind === 'freetext' || (prompt.options?.length ?? 0) === 0}
				<form
					class="pfree"
					onsubmit={(e) => {
						e.preventDefault();
						if (freeReply) inject(freeReply + '\r');
						freeReply = '';
						prompt = null;
					}}
				>
					<input
						bind:value={freeReply}
						placeholder="reply…"
						autocapitalize="off"
						autocorrect="off"
						autocomplete="off"
					/>
					<button type="submit">send</button>
				</form>
			{:else}
				<div class="prow">
					{#each prompt.options as o}
						<button
							class="popt"
							class:def={o.default}
							onclick={() => {
								inject(keystrokeToBytes(o.inject));
								prompt = null;
							}}>{o.label}</button
						>
					{/each}
				</div>
			{/if}
		</div>
	{:else if maybeWaiting}
		<div class="maybe">may be waiting — no clear prompt detected</div>
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
		/* Caps the subtree: xterm's canvases carry their own z-indexes and
		   would otherwise stack above the sibling overlays below. */
		z-index: 0;
	}
	.inactive .term {
		opacity: 0.92;
	}
	/* select mode: text selection wins over synthetic mouse reporting */
	.select-mode :global(.xterm) {
		-webkit-user-select: text;
		user-select: text;
	}
	/* xterm's native viewport scrollbar is a hover-cursor affordance —
	   hide it; wheel/drag scrolling is unchanged. */
	.tsurface :global(.xterm-viewport) {
		scrollbar-width: none;
	}
	.tsurface :global(.xterm-viewport::-webkit-scrollbar) {
		display: none;
	}
	.banner {
		position: absolute;
		z-index: 1;
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
		z-index: 1;
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
	/* §05 prompt overlay — touch-first action row above the term */
	.poverlay {
		position: absolute;
		z-index: 1;
		bottom: 10px;
		left: 50%;
		transform: translateX(-50%);
		max-width: min(92%, 560px);
		background: #131a26f2;
		border: 1px solid #2b3b55;
		border-radius: 10px;
		padding: 10px 12px;
		box-shadow: 0 6px 24px rgba(0, 0, 0, 0.5);
	}
	.ptitle {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		color: #cfe0f5;
		font: 13px/1.35 ui-monospace, monospace;
		margin-bottom: 8px;
		white-space: pre-wrap;
	}
	.px {
		flex: none;
		border: 0;
		background: none;
		color: #7a8aa0;
		font-size: 14px;
		min-width: 44px;
		min-height: 44px;
		margin: -8px -4px;
	}
	.prow {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.popt {
		min-height: 44px;
		padding: 0 16px;
		border: 1px solid #33415c;
		border-radius: 8px;
		background: #1b2536;
		color: #dbe7f7;
		font: 14px/1 ui-monospace, monospace;
	}
	.popt.def {
		background: #1f5c8b;
		border-color: #2f7cb3;
	}
	.popt:active {
		background: #33415c;
	}
	.pfree {
		display: flex;
		gap: 8px;
	}
	.pfree input {
		flex: 1;
		height: 44px;
		border: 1px solid #33415c;
		border-radius: 8px;
		background: #0d1420;
		color: #e6e6e6;
		padding: 0 10px;
		font: 14px ui-monospace, monospace;
	}
	.pfree button {
		height: 44px;
		padding: 0 14px;
		border: 1px solid #33415c;
		border-radius: 8px;
		background: #1f5c8b;
		color: #dbe7f7;
		font: 14px ui-monospace, monospace;
	}
	.maybe {
		position: absolute;
		z-index: 1;
		bottom: 10px;
		left: 50%;
		transform: translateX(-50%);
		background: #2a2213f0;
		border: 1px solid #4a3d1e;
		color: #d8bd74;
		font: 12px/1 ui-monospace, monospace;
		padding: 8px 12px;
		border-radius: 8px;
		pointer-events: none;
	}
	.tools {
		position: absolute;
		z-index: 1;
		top: 6px;
		right: 6px;
		display: flex;
		gap: 6px;
		align-items: center;
	}
	/* Desktop: the tools float over scrollback — reveal on hover only.
	   Touch has no hover, so they stay put there. */
	@media (hover: hover) {
		.tools {
			opacity: 0;
			transition: opacity 0.15s;
		}
		.tsurface:hover .tools,
		.tools.open {
			opacity: 1;
		}
	}
	.tool {
		min-width: 44px;
		min-height: 44px;
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
		height: 44px;
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
