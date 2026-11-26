<!-- Ref: .teos/sections/11-web-ui.md — TerminalSurface: xterm.js surface,
     one per pane; touch layer resolves TUI-mouse vs browser-selection. -->
<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { get } from 'svelte/store';
	import { Terminal } from '@xterm/xterm';
	import { FitAddon } from '@xterm/addon-fit';
	import { SearchAddon } from '@xterm/addon-search';
	import { Unicode11Addon } from '@xterm/addon-unicode11';
	import { SessionClient, type PromptFrame } from '$lib/ws';
	import { touchMode, kbdMods } from '$lib/stores';
	import '@xterm/xterm/css/xterm.css';

	let {
		sessionId,
		active = false,
		onrequestinput,
		onpull,
		onpullrelease
	}: {
		sessionId: string;
		active?: boolean;
		onrequestinput?: () => void;
		onpull?: (dy: number) => void;
		onpullrelease?: (dy: number) => void;
	} = $props();

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

	// Custom scroll thumb — the native viewport scrollbar paints under the
	// .xterm-screen canvas on every UA (canvas is a positioned sibling), so
	// we hide it and render our own track/thumb above the canvas.
	let vpEl: HTMLElement | null = null;
	let vshow = $state(false);
	let vtop = $state(0);
	let vheight = $state(0);

	function updateThumb() {
		const b = term?.buffer.active;
		if (!b || !vpEl) return;
		const total = b.length;
		const vis = term.rows;
		if (total <= vis) {
			vshow = false;
			return;
		}
		const track = vpEl.clientHeight;
		const h = Math.max(24, (track * vis) / total);
		const frac = b.viewportY / (total - vis);
		vheight = h;
		vtop = Math.min(track - h, Math.max(0, frac * (track - h)));
		vshow = true;
	}

	function vPointerDown(e: PointerEvent) {
		const track = e.currentTarget as HTMLElement;
		e.preventDefault();
		e.stopPropagation();
		try {
			track.setPointerCapture(e.pointerId);
		} catch {
			// synthetic/stale pointer — capture is a nicety, not a requirement
		}
		const jump = (ev: PointerEvent) => {
			const r = track.getBoundingClientRect();
			if (!vpEl) return;
			const frac = Math.min(1, Math.max(0, (ev.clientY - r.top - vheight / 2) / (r.height - vheight)));
			vpEl.scrollTop = frac * (vpEl.scrollHeight - vpEl.clientHeight);
		};
		jump(e);
		const move = (ev: PointerEvent) => jump(ev);
		const end = () => {
			track.removeEventListener('pointermove', move);
			track.removeEventListener('pointerup', end);
			track.removeEventListener('pointercancel', end);
		};
		track.addEventListener('pointermove', move);
		track.addEventListener('pointerup', end);
		track.addEventListener('pointercancel', end);
	}

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

		// Color-report queries (OSC 4/5/10/11/12 '?') get no answer —
		// xterm's reply goes to the PTY as input, and TUIs that re-query
		// on resize/reattach paste it mid-redraw. Returning true marks
		// the sequence consumed; set-forms fall through to xterm.
		for (const id of [4, 5, 10, 11, 12])
			term.parser.registerOscHandler(id, (data: string) => data.includes('?'));

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

		// Scroll thumb follows buffer growth and viewport position.
		vpEl = el.querySelector('.xterm-viewport');
		term.onScroll(() => updateThumb());
		term.onRender(() => updateThumb());

		client = new SessionClient(sessionId, {
			data: (p) => term.write(p),
			open: () => {
				status = 'live';
				sendResize();
			},
			eof: (code) => {
				status = 'dead';
				dead = true;
				prompt = null; // no card over an exited session
				term.write(`\r\n\x1b[2m[process exited ${code}] — close or restart from the session list\x1b[0m\r\n`);
			},
			error: (code, msg) => {
				if (code !== 'resync_overflow') toast(`${code}: ${msg}`);
			},
			close: () => {
				if (status !== 'dead') status = 'reconnecting';
			},
			// Ref: .teos/sections/05-agent-session-management.md — prompt
			// card (read-only per ADR-007; answers happen in the terminal).
			// Low-confidence waits get no visual at all.
			prompt: (p) => {
				prompt = p;
			},
			promptClear: () => {
				prompt = null;
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
		// An armed select episode is scoped to this pane's lifetime —
		// never let it leak into whatever mounts next.
		if (get(touchMode) === 'select') touchMode.set('mouse');
	});

	// --- touch layer (§02/§11): the pointer contract on mobile ---
	// default + TUI holding the mouse: taps/long-press/contextmenu
	//   re-dispatch real button reports the TUI understands.
	// select armed (ctx-menu "select" or a sticky-Shift keybar tap):
	//   the pointer stream drives a synthetic mousedown/mousemove/mouseup
	//   sequence — compat mouse events can't be trusted on touch because
	//   the scroll claim suppresses them mid-drag. Inside a TUI the fake
	//   events carry shiftKey → xterm's force-select path; in a plain
	//   shell they don't (the plain-click path anchors the highlight).
	//   Synthesis only starts once the finger moves past the tap
	//   threshold — a tap or long-press stays a no-op so an existing
	//   highlight survives to the copy step. Shift-arm is per-gesture;
	//   menu-armed 'select' holds until a menu action or "cancel
	//   select" ends the episode.
	const LP_MS = 450;
	const LP_MOVE = 12;
	let lpTimer: ReturnType<typeof setTimeout> | undefined;
	let lpPt: { x: number; y: number } | null = null;
	let lpAt = 0;
	let lastGesture = 0;
	// touchSel: this gesture is an armed select — drive the synthetic
	// sequence on moves. selDown keeps the press point for the anchor;
	// selLive flips when synthesis actually began (past LP_MOVE).
	let touchSel = false;
	let selDown: { x: number; y: number } | null = null;
	let selLive = false;
	// The armed-select path exists for touch — a mouse already has
	// Shift+drag. Gate the menu item on a real touch capability.
	const canTouch =
		navigator.maxTouchPoints > 0 || matchMedia('(pointer: coarse)').matches;

	function fireSelMouse(type: 'mousedown' | 'mousemove' | 'mouseup', x: number, y: number) {
		const scr = el.querySelector('.xterm-screen') ?? el;
		// Edge dead-zone: xterm auto-scrolls whenever the pointer sits
		// outside the canvas — even 1px — and snaps the selection to
		// full line width. On touch that's where precise edge-row
		// selection dies. Report the finger as pinned to the edge for
		// one row-height outside the bounds; past that, the real offset
		// carries through and edge-scroll engages normally.
		if (type !== 'mouseup' && term) {
			const r = scr.getBoundingClientRect();
			const dead = r.height / Math.max(1, term.rows);
			if (y > r.bottom && y <= r.bottom + dead) y = r.bottom;
			else if (y < r.top && y >= r.top - dead) y = r.top;
		}
		const ev = new MouseEvent(type, {
			bubbles: true,
			cancelable: true,
			// shift = xterm's force-select escape — only needed (and only
			// correct) while a TUI owns the mouse; in a plain shell the
			// unmodified press anchors natively.
			shiftKey: tuiMouse(),
			button: 0,
			buttons: type === 'mouseup' ? 0 : 1,
			// the selection service only anchors on detail === 1
			detail: type === 'mousedown' ? 1 : 0,
			clientX: x,
			clientY: y
		});
		(ev as unknown as { __tsynth: boolean }).__tsynth = true;
		scr.dispatchEvent(ev);
	}

	const tuiMouse = () => (term?.modes?.mouseTrackingMode ?? 'none') !== 'none';

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
		lastGesture = Date.now();
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
		lastGesture = Date.now(); // dedupes Android's contextmenu echo
		openCtx(p.x, p.y);
	}

	// Context menu — the system-menu analogue for a canvas terminal.
	// Always the gesture target; "Send right-click" is an item when a
	// TUI holds the mouse, so the app path stays reachable.
	let ctx: { x: number; y: number; row: number; sel: string; tui: boolean } | null =
		$state(null);

	function openCtx(x: number, y: number) {
		ctx = {
			x: Math.min(x, innerWidth - 196),
			y: Math.min(y, innerHeight - 240),
			row: rowAt(y),
			sel: term.hasSelection() ? term.getSelection() : '',
			tui: get(touchMode) === 'mouse' && tuiMouse()
		};
	}

	// Any ctx action completes the interaction — an armed 'select'
	// episode ends with it. ctxSelect is the one item that manages the
	// arming itself (toggle), so it skips this.
	function endCtx() {
		ctx = null;
		touchMode.set('mouse');
	}

	function ctxCopy() {
		if (ctx?.sel) void navigator.clipboard?.writeText(ctx.sel);
		endCtx();
	}
	function ctxCopyLine() {
		if (!ctx) return;
		term.selectLines(ctx.row, ctx.row);
		const text = term.getSelection();
		if (text) void navigator.clipboard?.writeText(text).then(() => toast('line copied'));
		endCtx();
	}
	async function ctxPaste() {
		endCtx();
		try {
			const t = await navigator.clipboard.readText();
			if (t) pasteText(t);
		} catch {
			/* clipboard perm denied */
		}
	}
	function ctxSelectAll() {
		term.selectAll();
		endCtx();
	}
	function ctxRightClick() {
		if (ctx) rightClickAt(ctx.x, ctx.y);
		endCtx();
	}
	// Armed select: the next drag force-highlights (synthesized mouse
	// stream) even while a TUI owns the mouse. The arm has to survive
	// to the copy step — a plain press would clobber the highlight —
	// so it holds until a menu action or this same item cancels it.
	function ctxSelect() {
		const armed = get(touchMode) === 'select';
		ctx = null;
		if (armed) {
			touchMode.set('mouse');
		} else {
			touchMode.set('select');
			toast('select armed — drag to highlight; long-press to cancel');
		}
	}

	// Short tap: a TUI holding the mouse owns the click (xterm's native
	// touch path already reported it). A selection gesture — armed
	// select or armed Shift — is never an input request either: taps
	// stay no-ops so they can't clobber a highlight, only drags
	// synthesize. Otherwise the tap asks for the input line — the
	// surface is the on-ramp to typing.
	function tap() {
		if (gestureSel || tuiMouse()) return;
		onrequestinput?.();
	}

	// Pull-to-refresh inside the terminal: armed only when the viewport
	// already sits at the oldest scrollback line AND no TUI holds the
	// mouse — otherwise the drag keeps its normal job (scroll/TUI
	// wheel). A selection gesture is never a pull.
	let pullStart = -1;
	let pullX = -1;
	let gestureSel = false;
	function armPull(e: PointerEvent) {
		pullStart = -1;
		if (!onpull && !onpullrelease) return;
		if (e.pointerType !== 'touch') return;
		if (gestureSel) return;
		if ((term?.modes?.mouseTrackingMode ?? 'none') !== 'none') return;
		if (term.buffer.active.viewportY !== 0) return;
		pullStart = e.clientY;
		pullX = e.clientX;
	}

	function armTouchLayer() {
		el.addEventListener('pointerdown', (e) => {
			if (e.pointerType !== 'touch') return;
			// Decided here because compat mouse events arrive late —
			// pointerdown precedes the mousedown that consumes an armed
			// Shift, so tap()/armPull can't rely on the store.
			gestureSel = get(touchMode) === 'select' || get(kbdMods).shift;
			if (gestureSel) {
				touchSel = true;
				selDown = { x: e.clientX, y: e.clientY };
				selLive = false;
				// Consume only shift — ctrl/alt keep waiting for a keystroke.
				if (get(kbdMods).shift) kbdMods.update((m) => ({ ...m, shift: false }));
			}
			lpPt = { x: e.clientX, y: e.clientY };
			lpAt = Date.now();
			lpTimer = setTimeout(lpFire, LP_MS);
			armPull(e);
		});
		// While a select gesture is armed, own the whole gesture: no
		// scroll claim, no compat events — the synthetic stream below
		// is the only driver. Same trick the pull path uses.
		el.addEventListener(
			'touchstart',
			(e) => {
				if (touchSel && e.cancelable) e.preventDefault();
			},
			{ passive: false }
		);
		// Same scroll-claim guard as the shell: while a pull is armed,
		// cancel the browser's claim or the drag dies as pointercancel.
		el.addEventListener(
			'touchmove',
			(e) => {
				if ((pullStart >= 0 || touchSel) && e.cancelable) e.preventDefault();
			},
			{ passive: false }
		);
		el.addEventListener('pointermove', (e) => {
			if (lpPt && Math.hypot(e.clientX - lpPt.x, e.clientY - lpPt.y) > LP_MOVE) cancelLp();
			if (pullStart >= 0) {
				const dy = e.clientY - pullStart;
				const dx = Math.abs(e.clientX - pullX);
				// reversed or horizontal-dominant → select/scroll gesture
				if (dy < -6 || (dx > 12 && dx > Math.abs(dy))) pullStart = -1;
				else onpull?.(dy > 0 ? dy : 0);
			}
			if (touchSel && selDown && e.pointerType === 'touch') {
				if (!selLive) {
					if (Math.hypot(e.clientX - selDown.x, e.clientY - selDown.y) <= LP_MOVE)
						return;
					// Past the tap threshold — anchor at the press point,
					// then this move becomes the first extension.
					fireSelMouse('mousedown', selDown.x, selDown.y);
					selLive = true;
				}
				fireSelMouse('mousemove', e.clientX, e.clientY);
			}
		});
		// lpPt surviving to pointerup = press shorter than LP_MS with no
		// move (long-press and scroll both clear it first).
		el.addEventListener('pointerup', (e) => {
			if (e.pointerType === 'touch' && lpPt && Date.now() - lpAt < LP_MS) tap();
			if (pullStart >= 0) {
				onpullrelease?.(e.clientY - pullStart);
				pullStart = -1;
			}
			if (touchSel) {
				// Only a drag ever synthesized — xterm still needs the
				// mouseup to drop its document-level drag listeners.
				if (selLive) fireSelMouse('mouseup', e.clientX, e.clientY);
				touchSel = false;
				selLive = false;
				selDown = null;
			}
			gestureSel = false;
			cancelLp();
		});
		el.addEventListener('pointercancel', (e) => {
			if (pullStart >= 0) {
				onpullrelease?.(0);
				pullStart = -1;
			}
			if (touchSel) {
				if (selLive) fireSelMouse('mouseup', e.clientX, e.clientY);
				touchSel = false;
				selLive = false;
				selDown = null;
			}
			gestureSel = false;
			cancelLp();
		});

		el.addEventListener('contextmenu', (e) => {
			e.preventDefault();
			// Android fires contextmenu for the same long-press lpFire already
			// handled — the window dedupes; desktop right-clicks pass through.
			if (Date.now() - lastGesture < 600) return;
			openCtx(e.clientX, e.clientY);
		});

		// Capture beats xterm's own bubble listener: while a TUI holds the
		// mouse, a plain press is a mouse report — stop the real event and
		// re-dispatch a shift-marked copy so xterm takes its force-select
		// path (the desktop Shift+drag escape). detail must carry over:
		// the selection service only anchors on detail === 1. When no TUI
		// claims the mouse a plain drag already selects — synthesizing
		// shift there would hit xterm's extend-only incremental path and
		// anchor nothing, so the real event passes through.
		// Triggers on an armed 'select' episode OR an armed keybar Shift —
		// either way the arm is consumed for this gesture.
		el.addEventListener(
			'mousedown',
			(e) => {
				if (e.button !== 0) return;
				const shiftArmed = get(kbdMods).shift;
				if (get(touchMode) !== 'select' && !shiftArmed) return;
				if ((e as unknown as { __tsynth?: boolean }).__tsynth) return;
				// Consume only shift — an armed ctrl/alt keeps waiting for a keystroke.
				if (shiftArmed) kbdMods.update((m) => ({ ...m, shift: false }));
				if (!tuiMouse()) return;
				e.stopPropagation();
				const ev = new MouseEvent('mousedown', {
					bubbles: true,
					cancelable: true,
					shiftKey: true,
					button: 0,
					detail: e.detail || 1,
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
			updateThumb();
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

	// Drop-and-reattach the session socket — server replays missed
	// frames; also the visible "refresh did something" signal.
	export function resync() {
		client?.resync();
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

<div
	class="tsurface"
	class:inactive={!active}
	class:select-mode={$touchMode === 'select'}
	class:shiftarmed={$kbdMods.shift}
>
	<div bind:this={el} class="term"></div>

	{#if vshow}
		<!-- svelte-ignore a11y_no_static_element_interactions — scrollbar chrome -->
		<div
			class="vscroll"
			onpointerdown={vPointerDown}
			oncontextmenu={(e) => {
				e.preventDefault();
				e.stopPropagation();
			}}
		>
			<div class="vthumb" style:top="{vtop}px" style:height="{vheight}px"></div>
		</div>
	{/if}

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
		<!-- prompt card — read-only (ADR-007): the detected question and
		     its options; answering happens in the terminal, not here. -->
		<div class="poverlay">
			<div class="ptitle">
				{prompt.title}
				<button class="px" aria-label="dismiss" onclick={() => (prompt = null)}>✕</button>
			</div>
			{#if (prompt.options?.length ?? 0) > 0}
				<div class="prow">
					{#each prompt.options as o}
						<span class="popt" class:def={o.default}>
							{o.label}{#if o.hint}<kbd class="phint">{o.hint}</kbd>{/if}
						</span>
					{/each}
				</div>
			{/if}
		</div>
	{/if}

	{#if ctx}
		<!-- svelte-ignore a11y_no_static_element_interactions — backdrop only -->
		<div class="ctxshade" onpointerdown={() => (ctx = null)} oncontextmenu={(e) => e.preventDefault()}></div>
		<div class="ctx" style:left="{ctx.x}px" style:top="{ctx.y}px" role="menu">
			{#if ctx.sel}
				<button class="cx" role="menuitem" onclick={ctxCopy}>copy</button>
			{/if}
			<button class="cx" role="menuitem" onclick={ctxCopyLine}>copy line</button>
			<button class="cx" role="menuitem" onclick={ctxPaste}>paste</button>
			{#if canTouch}
				<button class="cx" role="menuitem" onclick={ctxSelect}
					>{$touchMode === 'select' ? 'cancel select' : 'select'}</button
				>
			{/if}
			<button class="cx" role="menuitem" onclick={ctxSelectAll}>select all</button>
			{#if ctx.tui}
				<button class="cx" role="menuitem" onclick={ctxRightClick}>send right-click</button>
			{/if}
		</div>
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
	/* Armed select (menu episode or a sticky-Shift keybar tap): own the
	   gesture outright. preventDefault can't veto a compositor scroll
	   claim — touch-action can, and it must be set before the gesture
	   starts, which is exactly what the armed classes guarantee. */
	.select-mode .term,
	.shiftarmed .term {
		touch-action: none;
	}
	/* The native viewport scrollbar paints under the .xterm-screen canvas —
	   hide it; .vscroll renders the thumb above the canvas instead. */
	.tsurface :global(.xterm-viewport) {
		scrollbar-width: none;
	}
	.tsurface :global(.xterm-viewport::-webkit-scrollbar) {
		display: none;
	}
	.vscroll {
		position: absolute;
		top: 0;
		right: 0;
		bottom: 0;
		width: 12px;
		z-index: 4;
		touch-action: none;
	}
	.vthumb {
		position: absolute;
		left: 2px;
		right: 2px;
		border-radius: 4px;
		background: rgba(255, 255, 255, 0.3);
	}
	.vthumb:hover {
		background: rgba(255, 255, 255, 0.45);
	}
	.ctxshade {
		position: fixed;
		inset: 0;
		z-index: 55;
	}
	.ctx {
		position: fixed;
		z-index: 56;
		min-width: 168px;
		background: #11161f;
		border: 1px solid #2b3b55;
		border-radius: 10px;
		padding: 4px;
		box-shadow: 0 8px 28px rgb(0 0 0 / 0.5);
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
	/* §05 prompt card — read-only display row above the term (ADR-007) */
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
	.tools {
		position: absolute;
		z-index: 1;
		top: 6px;
		right: 20px; /* clear of the 12px viewport scrollbar */
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
