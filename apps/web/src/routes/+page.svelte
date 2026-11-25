<!-- Ref: .teos/sections/11-web-ui.md — the mobile-first shell:
     session drawer + pane grid + key bar. Panes are views over
     server-owned sessions — closing one never kills the process. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import interact from 'interactjs';
	import { api, type Session } from '$lib/api';
	import {
		sessions,
		panes,
		activePane,
		newPane,
		closePane,
		refreshSessions,
		refreshAgents,
		saveWorkspace,
		restoreWorkspace,
		wireEvents,
		drawerOpen,
		orderedSessions,
		agentStates,
		alerts,
		refreshAlerts,
		ackAlert,
		grid,
		onGrid,
		assignCell,
		cellPos,
		applyPreset,
		sizeCellPair,
		sizeRowPair
	} from '$lib/stores';
	import type { Pane } from '$lib/stores';
	import TerminalSurface from '$lib/components/TerminalSurface.svelte';
	import KeyBar from '$lib/components/KeyBar.svelte';
	import InputLine from '$lib/components/InputLine.svelte';

	let surfaces: Record<string, TerminalSurface | undefined> = {};
	let saveTimer: ReturnType<typeof setTimeout> | null = null;
	let now = $state(Date.now());
	// Touch-first devices start with the input line open — the on-screen
	// surface is their only typing path.
	const coarsePtr = () =>
		typeof matchMedia !== 'undefined' && matchMedia('(pointer: coarse)').matches;
	let kbdOpen = $state(coarsePtr());
	// On touch the field is always ghosted — never rendered — and ⌨
	// toggles whether it holds focus (keyboard up/down). Green = armed.
	let kbdArmed = $state(true);
	let iline: InputLine | undefined = $state();
	// The shell tracks the *visual* viewport, not 100dvh: on mobile the
	// keyboard shrinks the visual viewport without always resizing the
	// layout one (iOS), and without this the keybar sits under the
	// keyboard. offsetTop folds in any pan the browser does to keep the
	// focused field visible.
	let shellH = $state(0);

	// Pull-to-refresh: a downward drag outside the terminal surface
	// refreshes sessions/agents/alerts. Touches inside .pterm belong to
	// xterm's own scroll/click gestures — never hijacked.
	const PTR_TRIP = 64;
	const PTR_MAX = 96;
	let pullStart = -1;
	let pullX = -1;
	let pull = $state(0);
	let ptrBusy = $state(false);
	let shellEl: HTMLDivElement;

	// Layout presets — the grid is a fixed arrangement of slots (≤4);
	// pool tabs swap in/out of slots without changing the shape.
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

	// Pane resize + reorder: dividers redistribute a pair's weight,
	// tab drags swap panes into grid slots. Both persist via scheduleSave.
	let cellCount = $derived($grid.reduce((n, r) => n + r.cells.length, 0));
	// Which frame's session-switcher is open ("r:c"), or null.
	let pickCell = $state<string | null>(null);
	// The armed frame — tab clicks route their session here.
	let focusCell = $state<string | null>(null);
	let armedCell = $derived(focusCell ?? ($activePane ? cellPos($grid, $activePane) : null));
	let gridEl: HTMLElement;
	let rowEls: (HTMLElement | undefined)[] = [];
	let justDragged = false;

	// Generic pair drag: `cells` are the two elements sharing an axis;
	// `apply` writes the first element's new weight. Tap = cycle
	// 1/1 → 2/1 → 1/2.
	function pairDrag(e: PointerEvent, cells: [HTMLElement, HTMLElement], horiz: boolean, sizeA: number, sizeB: number, apply: (a: number) => void) {
		if (e.button !== 0) return;
		e.preventDefault();
		const d = e.currentTarget as HTMLElement;
		try {
			d.setPointerCapture(e.pointerId);
		} catch {
			// synthetic/stale pointer — capture is a nicety, not a requirement
		}
		const ra = cells[0].getBoundingClientRect();
		const rb = cells[1].getBoundingClientRect();
		const pairPx = (horiz ? ra.width + rb.width : ra.height + rb.height) || 1;
		const start = horiz ? ra.left : ra.top;
		const total = sizeA + sizeB;
		let moved = false;
		const move = (ev: PointerEvent) => {
			const size = (((horiz ? ev.clientX : ev.clientY) - start) / pairPx) * total;
			if (!moved && Math.abs(size - sizeA) > total * 0.02) moved = true;
			if (moved) apply(size);
		};
		const up = () => {
			d.removeEventListener('pointermove', move);
			d.removeEventListener('pointerup', up);
			d.removeEventListener('pointercancel', up);
			if (!moved) {
				const ratio = sizeA / total;
				apply(total * (ratio > 0.6 ? 1 / 3 : ratio > 0.4 ? 2 / 3 : 0.5));
			}
			scheduleSave();
		};
		d.addEventListener('pointermove', move);
		d.addEventListener('pointerup', up);
		d.addEventListener('pointercancel', up);
	}

	// Divider between two cells in the same row.
	function divDown(e: PointerEvent, r: number, c: number) {
		const row = rowEls[r];
		if (!row) return;
		const cells = [...row.querySelectorAll<HTMLElement>(':scope > .pane')];
		const a = cells[c - 1];
		const b = cells[c];
		if (!a || !b) return;
		const horiz = getComputedStyle(row).flexDirection === 'row';
		const row_ = $grid[r];
		pairDrag(e, [a, b], horiz, row_.cells[c - 1].size ?? 1, row_.cells[c].size ?? 1, (s) =>
			sizeCellPair(r, c - 1, c, s)
		);
	}

	// Divider between two rows — always a vertical split.
	function rowDivDown(e: PointerEvent, r: number) {
		const a = rowEls[r - 1];
		const b = rowEls[r];
		if (!a || !b) return;
		pairDrag(e, [a, b], false, $grid[r - 1].size ?? 1, $grid[r].size ?? 1, (s) =>
			sizeRowPair(r - 1, r, s)
		);
	}

	// Drag & drop via interact.js — the frame's grab handle (.fgrab)
	// carries its session: drop on another frame to swap occupants, or
	// on a pool tab to trade places (tab fills the frame, session goes
	// to the pool). The handle follows the pointer while dragging.
	function initDnd() {
		// Drag state lives on the dragged element — a second pointer
		// starting mid-drag can't corrupt the first one's offset.
		const pos = new WeakMap<Element, { x: number; y: number; moved: boolean }>();
		// The highlighted target under the pointer is what a drop commits —
		// hint and commit can never disagree, and nothing depends on
		// dropzone event timing.
		let hint: { kind: 'cell'; r: number; c: number } | { kind: 'tab'; pid: string } | null = null;
		let hintT = 0; // when the pointer last resolved a target
		const clearHints = () =>
			document
				.querySelectorAll('.drop-hint')
				.forEach((t) => t.classList.remove('drop-hint', 'hint-swap', 'hint-assign'));
		const hintAt = (x: number, y: number) => {
			clearHints();
			hint = null;
			const hit = document.elementFromPoint(x, y);
			const cell = hit?.closest<HTMLElement>('.pane[data-cell]');
			if (cell?.dataset.cell) {
				const [r, c] = cell.dataset.cell.split(':').map(Number);
				hint = { kind: 'cell', r, c };
				hintT = performance.now();
				cell.classList.add('drop-hint', 'hint-assign');
				return;
			}
			const tab = hit?.closest<HTMLElement>('.pane-tab');
			if (tab?.dataset.pid) {
				hint = { kind: 'tab', pid: tab.dataset.pid };
				hintT = performance.now();
				tab.classList.add('drop-hint', 'hint-swap');
			}
		};

		// Drag source is the frame's grab handle (.fgrab), not the tab —
		// the handle exists only for dragging so it starts instantly on
		// both mouse and touch, no hold-to-arm needed.
		const grabs = interact('.fgrab')
			.draggable({
				manualStart: true,
				listeners: {
					start(ev) {
						hint = null;
						(ev.target as HTMLElement).classList.add('dnd-drag');
						document.body.classList.add('dnd-live');
					},
					move(ev) {
						const t = ev.target as HTMLElement;
						const p = pos.get(t);
						if (p) {
							p.x += ev.dx;
							p.y += ev.dy;
							p.moved = true;
							t.style.transform = `translate(${p.x}px, ${p.y}px)`;
						}
						hintAt(ev.clientX, ev.clientY);
					},
					end(ev) {
						const t = ev.target as HTMLElement;
						const p = pos.get(t);
						t.style.transform = '';
						t.classList.remove('dnd-drag');
						document.body.classList.remove('dnd-live');
						// Re-resolve at release coords. If nothing resolves,
						// a *recent* last-move hint still counts; a stale one
						// (pointer left it >200ms ago) is not a drop target.
						const last = hint;
						hintAt(ev.clientX, ev.clientY);
						if (!hint && last && performance.now() - hintT < 200) hint = last;
						clearHints();
						pos.delete(t);
						if (!p?.moved) return;
						// The synthetic click lands right after pointerup —
						// suppress it, then reset for the next plain click.
						justDragged = true;
						setTimeout(() => (justDragged = false), 0);
						const dragPid = t.dataset.pid;
						if (!hint || !dragPid) return;
						if (hint.kind === 'cell' && cellPos($grid, dragPid) !== `${hint.r}:${hint.c}`) {
							assignCell(dragPid, hint.r, hint.c);
						} else if (hint.kind === 'tab' && hint.pid !== dragPid) {
							// Drop on a pool tab → that tab fills the dragged
							// session's frame; the session returns to the pool.
							const src = cellPos($grid, dragPid);
							if (!src) return;
							const [r, c] = src.split(':').map(Number);
							assignCell(hint.pid, r, c);
						} else return;
						scheduleSave();
					}
				}
			})
			.on('down', (ev) => {
				pos.set(ev.currentTarget, { x: 0, y: 0, moved: false });
				ev.interaction.start({ name: 'drag' }, ev.interactable, ev.currentTarget);
			});

		// pointercancel (e.g. an OS gesture steal) may bypass 'end' —
		// never let the grabbing cursor latch on.
		const cancel = () => document.body.classList.remove('dnd-live');
		window.addEventListener('pointercancel', cancel, true);

		return () => {
			grabs.unset();
			window.removeEventListener('pointercancel', cancel, true);
		};
	}

	function ptrDown(e: PointerEvent) {
		if (e.pointerType !== 'touch' || ptrBusy) return;
		const t = e.target as Element;
		// Pull zone is the top bar only — everywhere else scrolls,
		// drags, or drives keys of its own. The terminal has its own
		// pull at scroll-top.
		if (!t.closest('.topbar')) return;
		pullStart = e.clientY;
		pullX = e.clientX;
	}
	function ptrMove(e: PointerEvent) {
		if (pullStart < 0) return;
		const dy = e.clientY - pullStart;
		const dx = Math.abs(e.clientX - pullX);
		// horizontal-dominant or reversed → hand the gesture back to
		// the pane scroller / normal behavior
		if ((dx > 12 && dx > Math.abs(dy)) || dy < -6) {
			pullStart = -1;
			pull = 0;
			return;
		}
		pull = dy > 6 ? Math.min(PTR_MAX, dy) : 0;
	}
	function ptrEnd() {
		if (pullStart < 0) return;
		pullStart = -1;
		if (pull < PTR_TRIP) {
			pull = 0;
			return;
		}
		pull = 0;
		fireRefresh();
	}

	function fireRefresh() {
		ptrBusy = true;
		// Reattach every open session socket — server replays missed
		// frames (repaints the terminal, repairs a stuck connection).
		for (const s of Object.values(surfaces)) s?.resync();
		void (async () => {
			const t0 = Date.now();
			await refreshSessions();
			await Promise.allSettled([refreshAgents(), refreshAlerts()]);
			// keep the chip visible a beat so the refresh reads as done
			setTimeout(() => (ptrBusy = false), Math.max(0, 600 - (Date.now() - t0)));
		})();
	}

	// Terminal-area pulls arrive as raw drag deltas — same indicator,
	// same threshold.
	function termPull(dy: number) {
		if (!ptrBusy && dy > 6) pull = Math.min(PTR_MAX, dy);
	}
	function termPullEnd(dy: number) {
		const go = pull >= PTR_TRIP || dy >= PTR_TRIP;
		pull = 0;
		if (go) fireRefresh();
	}

	onMount(() => {
		const vv = window.visualViewport;
		const syncVV = () => (shellH = vv ? vv.height + vv.offsetTop : 0);
		syncVV();
		vv?.addEventListener('resize', syncVV);
		vv?.addEventListener('scroll', syncVV);
		// Pull guard: while a pull is armed, cancel the browser's scroll
		// claim on the gesture — otherwise it steals the drag and fires
		// pointercancel before the threshold. passive:false is required
		// for preventDefault to work.
		const guard = (e: TouchEvent) => {
			if (pullStart >= 0 && e.cancelable) e.preventDefault();
		};
		shellEl.addEventListener('touchmove', guard, { passive: false });
		const cleanupDnd = initDnd();
		const tick = setInterval(() => (now = Date.now()), 15000);
		void (async () => {
			await refreshSessions();
			await refreshAgents();
			await refreshAlerts();
			await restoreWorkspace();
			wireEvents();
			if ($panes.length === 0) {
				const live = $sessions.find((s) => isLive(s));
				if (live) newPane(live.id);
				else await spawn();
			}
		})();
		return () => {
			clearInterval(tick);
			cleanupDnd();
			shellEl.removeEventListener('touchmove', guard);
			vv?.removeEventListener('resize', syncVV);
			vv?.removeEventListener('scroll', syncVV);
		};
	});

	function isLive(s: Session) {
		return ['starting', 'working', 'waiting-for-input', 'undetectable', 'detached'].includes(s.state);
	}
	function isFinished(s: Session) {
		return !isLive(s);
	}
	// §05: a low-confidence wait ("may be waiting") is never promoted to
	// the needs-you rank — AGENTS.md: unknown shows the hint only.
	function needsYouId(id: string) {
		const a = $agentStates[id];
		return a?.state === 'waiting-for-input' && a.confidence !== 'low';
	}
	function needsYou(s: Session) {
		return needsYouId(s.id);
	}

	function fmtAgo(iso?: string) {
		if (!iso) return '';
		const sec = Math.max(0, (now - Date.parse(iso)) / 1000);
		if (sec < 10) return 'now';
		if (sec < 60) return `${Math.floor(sec)}s`;
		if (sec < 3600) return `${Math.floor(sec / 60)}m`;
		if (sec < 86400) return `${Math.floor(sec / 3600)}h`;
		return `${Math.floor(sec / 86400)}d`;
	}

	// §05 row subline: waiting → how long it's been waiting; finished →
	// terminal state + when it ended; live → foreground process/state.
	function subLabel(s: Session) {
		const a = $agentStates[s.id];
		if (needsYou(s)) return `waiting ${fmtAgo(a?.since)}`;
		if (isFinished(s)) {
			const ago = fmtAgo(s.ended_at);
			return s.state + (ago ? ` ${ago}` : '');
		}
		return a?.process ?? s.state;
	}
	// The detached overlay flag: a live session nobody has watched for
	// longer than TU_RECONNECT_WINDOW_S.
	function detachedOf(s: Session) {
		return s.detached && isLive(s);
	}
	function toolBadge(s: Session) {
		return $agentStates[s.id]?.tool ?? '';
	}

	function scheduleSave() {
		if (saveTimer) clearTimeout(saveTimer);
		saveTimer = setTimeout(() => void saveWorkspace(), 1200);
	}

	let shellPick = $state(false);
	let pickX = $state(0);
	let pickY = $state(0);
	function toggleShellPick(e: MouseEvent) {
		if (shellPick) {
			shellPick = false;
			return;
		}
		const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
		pickX = Math.min(r.left, innerWidth - 180);
		// 3 items × 40px + 8px padding — flip above the trigger when
		// opening downward would clip at the viewport bottom.
		const menuH = 3 * 40 + 8;
		pickY =
			r.bottom + 6 + menuH > innerHeight
				? Math.max(6, r.top - menuH - 6)
				: r.bottom + 6;
		shellPick = true;
	}

	async function spawn(shell?: string) {
		shellPick = false;
		const s = await api.spawn({ cols: 120, rows: 30, shell });
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

	// DELETE /{id}: live session → terminate (lands in finished, log kept);
	// finished session → hard-delete the log row. Server picks the branch.
	async function endOrDelete(id: string, e: Event) {
		e.stopPropagation();
		await api.kill(id);
		for (const p of $panes.filter((p) => p.sessionId === id)) closePane(p.id);
		await refreshSessions();
		await refreshAgents().catch(() => {});
	}

	// DELETE /{id}/log — purge only the state history; row + scrollback
	// stay so the session output remains viewable.
	async function purgeLog(id: string, e: Event) {
		e.stopPropagation();
		await api.clearLog(id);
	}

	// DELETE /api/sessions — clear all finished rows + their logs at once.
	async function clearFinished(e: Event) {
		e.stopPropagation();
		const res = await api.clearFinished();
		if (res.cleared > 0) {
			const gone = new Set($orderedSessions.filter(isFinished).map((s) => s.id));
			for (const p of $panes.filter((p) => gone.has(p.sessionId))) closePane(p.id);
		}
		await refreshSessions();
		await refreshAgents().catch(() => {});
	}

	function inject(seq: string) {
		const ap = $activePane;
		if (ap) surfaces[ap]?.inject(seq);
	}

	// InputLine path — must not steal focus back to xterm's textarea or
	// the system keyboard jumps between fields.
	function injectRaw(seq: string) {
		const ap = $activePane;
		if (ap) surfaces[ap]?.send(seq);
	}

	async function paste(paneId?: string) {
		try {
			const t = await navigator.clipboard.readText();
			if (t) surfaces[paneId ?? $activePane ?? '']?.pasteText(t);
		} catch {
			/* clipboard perm denied */
		}
	}

	function label(s: Session | undefined): string {
		if (!s) return '?';
		return s.name || s.shell.split('/').pop() || s.id;
	}

	// Tab auto-name: an explicit name always wins; otherwise show the
	// app actually running — curated tool name when an adapter matched,
	// else the live foreground process; shell basename as the floor.
	function tabLabel(p: Pane): string {
		const s = sessionOf(p.sessionId);
		if (!s) return '?';
		if (s.name) return s.name;
		const a = $agentStates[p.sessionId];
		return a?.tool || a?.process || s.shell.split('/').pop() || s.id;
	}

	function sessionOf(id: string): Session | undefined {
		return $sessions.find((s) => s.id === id);
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions — gesture layer, not keyboard -->
<div
	bind:this={shellEl}
	class="shell"
	style:height={shellH > 0 ? `${shellH}px` : undefined}
	onpointerdown={ptrDown}
	onpointermove={ptrMove}
	onpointerup={ptrEnd}
	onpointercancel={ptrEnd}
>
	{#if pull > 0 || ptrBusy}
		<div class="ptr" style:transform={`translateY(${ptrBusy ? 40 : Math.min(pull, 80)}px)`}>
			{ptrBusy ? 'refreshing…' : pull >= PTR_TRIP ? 'release to refresh' : '↓ pull to refresh'}
		</div>
	{/if}
	<!-- top bar: sessions drawer + panes + mode toggle -->
	<header class="topbar">
		<button class="tb" aria-label="sessions" onclick={() => drawerOpen.update((v) => !v)}>
			☰
		</button>
		<div class="panes">
			{#each $panes as p (p.id)}
				<div
					class="pane-tab"
					class:active={$activePane === p.id}
					class:ongrid={$onGrid.has(p.id)}
					data-pid={p.id}
					role="button"
					tabindex="0"
					onclick={() => {
						if (justDragged) {
							justDragged = false;
							return;
						}
						// Route the tab's session into the armed frame —
						// displaces its occupant, or swaps slots when the
						// tab already sits in a frame.
						const k = armedCell ?? '0:0';
						const [r, c] = k.split(':').map(Number);
						assignCell(p.id, r, c);
						focusCell = k;
						activePane.set(p.id);
						scheduleSave();
					}}
					onkeydown={() => activePane.set(p.id)}
				>
					{#if needsYouId(p.sessionId)}<span class="tabdot needs"></span>{/if}
					{tabLabel(p)}
					<button
						class="tabx"
						aria-label="close tab"
						onclick={(e) => {
							e.stopPropagation();
							closePane(p.id);
							scheduleSave();
						}}>✕</button
					>
				</div>
			{/each}
			<button class="tb add" aria-label="new session" onclick={toggleShellPick}>+</button>
		</div>
	</header>

	{#if shellPick}
		<!-- svelte-ignore a11y_no_static_element_interactions — backdrop only -->
		<div class="shade" onpointerdown={() => (shellPick = false)} oncontextmenu={(e) => e.preventDefault()}></div>
		<div class="shellpick" style:left="{pickX}px" style:top="{pickY}px" role="menu">
			<button class="cx" role="menuitem" onclick={() => spawn('/bin/bash')}>bash</button>
			<button class="cx" role="menuitem" onclick={() => spawn('/bin/zsh')}>zsh</button>
			<button class="cx" role="menuitem" onclick={() => spawn('/bin/fish')}>fish</button>
		</div>
	{/if}

	<!-- session drawer: alerts, then grouped needs-you / running / finished -->
	{#if $drawerOpen}
		{@const unacked = $alerts.filter((a) => !a.acked_at)}
		{@const needs = $orderedSessions.filter(needsYou)}
		{@const running = $orderedSessions.filter((s) => isLive(s) && !needsYou(s))}
		{@const finished = $orderedSessions.filter(isFinished)}
		<!-- outside-press dismiss — the shade sits under the drawer and
		     below the topbar so the ☰ toggle still works. -->
		<div class="dshade" onpointerdown={() => drawerOpen.set(false)} oncontextmenu={(e) => e.preventDefault()}></div>
		<div class="drawer">
			<div class="dhead">
				<span>sessions</span>
				<button class="tb" onclick={toggleShellPick}>+ new</button>
			</div>

			<div class="ghead"><span>layout</span></div>
			<div class="presets">
				{#each coarsePtr() ? MOBILE_PRESETS : PRESETS as pre (pre.name)}
					{@const cur = $grid.map((r) => r.cells.length).join(',') === pre.shape.join(',')}
					<button
						class="preset"
						class:cur
						title={pre.name}
						onclick={() => { applyPreset(pre.shape); scheduleSave(); }}
					>
						<span class="pg">
							{#each pre.shape as n}<span class="pgrow">{#each Array(n) as _}<i></i>{/each}</span>{/each}
						</span>
					</button>
				{/each}
			</div>

			{#if unacked.length > 0}
				<div class="ghead"><span>alerts</span></div>
				{#each unacked as a (a.id)}
					<div class="arow">
						<span class="dot" class:live={a.level === 'action'} class:dead={a.level === 'warn'} class:needs={a.level === 'action'}></span>
						<span class="smain">
							<span class="sname">{a.transition}</span>
							<span class="ssub">{a.process || a.session_id} · {fmtAgo(a.at)}</span>
						</span>
						<button class="sact hist" title="acknowledge alert" onclick={(e) => { e.stopPropagation(); void ackAlert(a.id); }}>ack</button>
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
					<button class="clearall" onclick={clearFinished}>clear all</button>
				</div>
				{#each finished as s (s.id)}
					{@render srow(s)}
				{/each}
			{/if}

			{#if $sessions.length === 0}
				<div class="empty">no sessions — tap + new</div>
			{/if}
		</div>
	{/if}

	<!-- terminal grid: preset-shaped rows of slots. Each slot holds one
	     pane from the pool (or waits for a tab drop); slot/row weights
	     belong to the slot, so swapping occupants keeps proportions. -->
	<main class="grid" class:single={cellCount <= 1} bind:this={gridEl}>
		{#each $grid as row, ri}
			{#if ri > 0}
				<!-- svelte-ignore a11y_no_static_element_interactions — splitter -->
				<div class="divider rowdiv" onpointerdown={(e) => rowDivDown(e, ri)}></div>
			{/if}
			<div class="grow" bind:this={rowEls[ri]} style:flex="{row.size ?? 1} 1 0">
				{#each row.cells as cell, ci}
					{#if ci > 0}
						<!-- svelte-ignore a11y_no_static_element_interactions — splitter -->
						<div class="divider" onpointerdown={(e) => divDown(e, ri, ci)}></div>
					{/if}
					{@const p = cell.paneId ? $panes.find((x) => x.id === cell.paneId) : undefined}
					<section
						class="pane"
						class:empty={!p}
						data-cell="{ri}:{ci}"
						style:flex="{cell.size ?? 1} 1 0"
						class:focused={armedCell === `${ri}:${ci}`}
						role="button"
						tabindex="0"
						onclick={() => {
							focusCell = `${ri}:${ci}`;
							if (p) activePane.set(p.id);
						}}
						onkeydown={(e) => {
							if (e.key === 'Enter' || e.key === ' ') {
								focusCell = `${ri}:${ci}`;
								if (p) activePane.set(p.id);
							}
						}}
					>
						{#if p}
							<div class="pterm">
								{#key cell.paneId}
									<TerminalSurface
										bind:this={surfaces[p.id]}
										sessionId={p.sessionId}
										active={$activePane === p.id}
										onrequestinput={() => (kbdOpen = true)}
										onpull={termPull}
										onpullrelease={termPullEnd}
									/>
								{/key}
							</div>
							<div
								class="fgrab"
								data-pid={p.id}
								role="button"
								tabindex="-1"
								title="drag onto another frame or a tab to swap">⠿</div
							>
						{:else}
							<button
								class="pempty"
								onclick={(e) => {
									e.stopPropagation();
									// A drop landing here also fires a click —
									// swallow it so the picker doesn't pop.
									if (justDragged) {
										justDragged = false;
										return;
									}
									const k = `${ri}:${ci}`;
									focusCell = k;
									pickCell = pickCell === k ? null : k;
								}}>drop a tab here, or pick a session ▾</button
							>
						{/if}
						{#if pickCell === `${ri}:${ci}`}
							<!-- svelte-ignore a11y_no_static_element_interactions — click-away -->
							<div class="pickback" onpointerdown={(e) => { e.stopPropagation(); pickCell = null; }}></div>
							<div class="cellpick">
								{#each $panes as pp}
									<button
										class="pick"
										class:cur={pp.id === cell.paneId}
										onclick={(e) => {
											e.stopPropagation();
											assignCell(pp.id, ri, ci);
											pickCell = null;
											activePane.set(pp.id);
											scheduleSave();
										}}
									>
										<span class="plabel">{tabLabel(pp)}</span>
										<span class="pwhere">{cellPos($grid, pp.id) ?? 'pool'}</span>
									</button>
								{/each}
							</div>
						{/if}
					</section>
				{/each}
			</div>
		{/each}
		{#if $panes.length === 0}
			<div class="empty-main">
				<button class="bigspawn" onclick={toggleShellPick}>open a session</button>
			</div>
		{/if}
	</main>

	{#if kbdOpen}
		<InputLine
			bind:this={iline}
			oninject={injectRaw}
			ghost={coarsePtr()}
			armed={!coarsePtr() || kbdArmed}
			onclose={() => (kbdOpen = false)}
		/>
	{/if}
	<KeyBar
		oninject={inject}
		onpaste={() => paste()}
		ontogglekbd={() => {
			if (coarsePtr()) {
				kbdArmed = !kbdArmed;
				if (kbdArmed) iline?.focus();
				else iline?.blur();
			} else {
				kbdOpen = !kbdOpen;
			}
		}}
		kbdOn={coarsePtr() ? kbdArmed : kbdOpen}
	/>
</div>

<!-- one session row — dot, name + tool badge, state/wait subline, and the
     lifecycle action (live → end, finished → del). -->
{#snippet srow(s: Session)}
	<div
		class="srow"
		class:needs={needsYou(s)}
		role="button"
		tabindex="0"
		onclick={() => openSession(s)}
		onkeydown={() => openSession(s)}
	>
		<span class="dot" class:live={isLive(s)} class:dead={isFinished(s)} class:needs={needsYou(s)}></span>
		<span class="smain">
			<span class="stop">
				<span class="sname">{label(s)}</span>
				{#if toolBadge(s)}<span class="badge">{toolBadge(s)}</span>{/if}
			</span>
			<span class="ssub" class:needs={needsYou(s)}>
				{subLabel(s)}{#if detachedOf(s)}<span class="det"> · unattended</span>{/if}
			</span>
		</span>
		{#if isLive(s)}
			<button class="sact end" title="terminate session — keeps the log row" onclick={(e) => endOrDelete(s.id, e)}>end</button>
		{:else}
			<button class="sact hist" title="purge state history only — keeps row + output" onclick={(e) => purgeLog(s.id, e)}>hist</button>
			<button class="sact del" title="delete session + log + output" onclick={(e) => endOrDelete(s.id, e)}>del</button>
		{/if}
	</div>
{/snippet}

<style>
	.shell {
		position: relative;
		height: 100dvh;
		display: flex;
		flex-direction: column;
		background: #0a0e14;
	}
	.shade {
		position: fixed;
		inset: 0;
		z-index: 55;
	}
	.shellpick {
		position: fixed;
		z-index: 56;
		min-width: 120px;
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
	.ptr {
		position: fixed;
		top: 0;
		left: 50%;
		margin-left: -70px;
		z-index: 60;
		background: #131a26f2;
		border: 1px solid #2b3b55;
		border-radius: 999px;
		color: #9db2ce;
		font: 12px/1 ui-monospace, monospace;
		padding: 8px 14px;
		pointer-events: none;
		transition: transform 0.05s;
	}
	.topbar {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 6px 8px 0;
		padding-top: calc(6px + env(safe-area-inset-top, 0px));
		background: #0d1117;
		border-bottom: 1px solid #1a2334;
		flex-shrink: 0;
	}
	.tb {
		min-width: 44px;
		height: 44px;
		margin-bottom: 6px;
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
		align-items: flex-end;
		gap: 4px;
		overflow-x: auto;
		overflow-x: overlay; /* float over tabs where supported — no gutter band */
		scrollbar-width: auto;
		scrollbar-color: rgba(255, 255, 255, 0.3) transparent;
	}
	.panes::-webkit-scrollbar {
		height: 12px;
		background: transparent;
	}
	.panes::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.3);
		border-radius: 4px;
	}
	.panes::-webkit-scrollbar-thumb:hover {
		background: rgba(255, 255, 255, 0.45);
	}
	.panes::-webkit-scrollbar-track {
		background: transparent;
	}
	/* Flat underline tabs (japa.to/blog style) — no chip; the active
	   tab's 2px accent bar rides on the topbar's baseline border. */
	.pane-tab {
		position: relative;
		height: 44px;
		display: flex;
		align-items: center;
		margin-bottom: 1px;
		border: 0;
		background: transparent;
		color: #6b7f9e;
		font: 500 13px ui-monospace, monospace;
		padding: 0 4px 0 14px;
		white-space: nowrap;
		flex-shrink: 0;
		cursor: pointer;
		transition: color 0.15s;
		/* Horizontal swipes keep scrolling the strip; vertical moves
		   stay ours so an armed drag can't be stolen. */
		touch-action: pan-x;
		user-select: none;
		-webkit-user-select: none;
		-webkit-touch-callout: none;
	}
	.pane-tab:hover {
		color: #9db2ce;
	}
	.pane-tab::after {
		content: '';
		position: absolute;
		left: 10px;
		right: 10px;
		bottom: 0;
		height: 2px;
		border-radius: 999px;
		background: transparent;
		transition: background-color 0.15s;
	}
	.pane-tab.active {
		color: #e6f2ff;
	}
	.pane-tab.active::after {
		background: #4cc9f0;
	}
	/* On-grid marker — a small square "frame" tick before the label. */
	.pane-tab.ongrid::before {
		content: '';
		width: 5px;
		height: 5px;
		border-radius: 1.5px;
		background: #3d5a80;
		margin-right: 7px;
		flex-shrink: 0;
	}
	.tabx {
		border: 0;
		background: transparent;
		color: #4a5568;
		font-size: 11px;
		padding: 0 12px;
		margin-left: 4px;
		align-self: stretch;
		min-width: 40px;
	}
	.tabx:active {
		color: #ff8080;
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
		overflow-y: overlay; /* float over the list where supported */
		scrollbar-width: auto;
		scrollbar-color: rgba(255, 255, 255, 0.3) transparent;
		/* keep downward drags at scroll-top for pull-to-refresh —
		   don't let the browser claim them as overscroll */
		overscroll-behavior: contain;
	}
	.drawer::-webkit-scrollbar {
		width: 12px;
		background: transparent;
	}
	.drawer::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.3);
		border-radius: 4px;
	}
	.drawer::-webkit-scrollbar-thumb:hover {
		background: rgba(255, 255, 255, 0.45);
	}
	.drawer::-webkit-scrollbar-track {
		background: transparent;
	}
	.dshade {
		position: fixed;
		top: calc(50px + env(safe-area-inset-top, 0px));
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 19;
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
	/* layout preset menu — mini grid glyph + name, current shape lit */
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
	/* glyph: rows of mini cells mirroring the shape */
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
	.tabdot {
		display: inline-block;
		width: 7px;
		height: 7px;
		border-radius: 50%;
		margin-right: 6px;
		vertical-align: 1px;
	}
	.tabdot.needs {
		background: #f0b429;
		box-shadow: 0 0 5px #f0b42980;
	}
	.empty,
	.empty-main {
		padding: 24px;
		text-align: center;
		color: #33415c;
		font: 13px ui-monospace, monospace;
	}
	/* Grid = column of rows (preset shape). Cells split horizontally on
	   desktop, stack vertically on small screens; row splits stay
	   vertical on every screen. */
	.grid {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-height: 0;
		background: #06090d;
	}
	.grow {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-height: 0;
		min-width: 0;
	}
	@media (min-width: 900px) {
		.grow {
			flex-direction: row;
		}
	}
	/* Splitter overlays the 2px gap — zero layout cost, 8px hit zone. */
	.divider {
		flex-shrink: 0;
		z-index: 3;
		touch-action: none;
	}
	@media (min-width: 900px) {
		.divider {
			width: 8px;
			margin: 0 -4px;
			cursor: col-resize;
		}
	}
	@media (max-width: 899.5px) {
		.divider {
			height: 8px;
			margin: -4px 0;
			cursor: row-resize;
		}
	}
	/* Between rows the split is always vertical, at every width. */
	.rowdiv {
		height: 8px;
		width: auto;
		margin: -4px 0;
		cursor: row-resize;
	}
	.divider:hover {
		background: #22304a;
	}
	:global(.fgrab.dnd-drag) {
		z-index: 50;
		opacity: 0.95;
		background: #16233a;
		border-color: #4cc9f0;
		color: #4cc9f0;
		box-shadow: 0 4px 14px #000c;
		cursor: grabbing;
		will-change: transform;
		/* The ghost follows the pointer — without this it masks the
		   drop target in hit-tests. */
		pointer-events: none;
	}
	:global(.pane-tab.hint-swap) {
		outline: 1px solid #4cc9f0;
	}
	:global(.pane.hint-assign) {
		outline: 1px solid #4cc9f0;
		outline-offset: -1px;
	}
	.pane {
		display: flex;
		flex-direction: column;
		min-height: 0;
		min-width: 0;
		position: relative;
		background: #0a0e14;
		border: 1px solid transparent;
	}
	.pane.empty {
		border-style: dashed;
		border-color: #1c2634;
		background: transparent;
	}
	/* Frame grab handle — the only drag source; sits bottom-right
	   clear of the search icon and the 12px scrollbar. 44px box to
	   match the touch-target convention; dimmed until hovered so it
	   doesn't cover bottom-row TUI content louder than it needs to. */
	.fgrab {
		position: absolute;
		bottom: 4px;
		right: 12px;
		z-index: 20;
		width: 44px;
		height: 44px;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 9px;
		background: #11161fcc;
		border: 1px solid #22304a;
		color: #3d5a80;
		font-size: 15px;
		line-height: 1;
		cursor: grab;
		opacity: 0.45;
		transition:
			opacity 0.15s,
			color 0.15s,
			border-color 0.15s;
		touch-action: none;
		user-select: none;
		-webkit-user-select: none;
		-webkit-touch-callout: none;
	}
	.fgrab:hover {
		opacity: 1;
		color: #4cc9f0;
		border-color: #4cc9f0;
	}
	/* The drag ghost is pointer-events:none — the browser picks its
	   cursor from whatever is under the pointer, so the grabbing palm
	   has to live on body for the duration of the drag. */
	:global(body.dnd-live),
	:global(body.dnd-live *) {
		cursor: grabbing !important;
	}
	.fgrab:active {
		cursor: grabbing;
	}
	.pempty {
		flex: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		color: #39485e;
		font: 11px ui-monospace, monospace;
		user-select: none;
		border: 0;
		background: transparent;
		appearance: none;
		padding: 0;
		cursor: pointer;
	}
	.pempty:hover {
		color: #5a6f92;
	}
	/* Per-frame session switcher: lists every open tab, swaps the pick
	   into this frame. pickback is a fixed click-away scrim. */
	.pickback {
		position: fixed;
		inset: 0;
		z-index: 40;
	}
	.cellpick {
		position: absolute;
		top: 26px;
		right: 4px;
		z-index: 41;
		display: flex;
		flex-direction: column;
		min-width: 140px;
		background: #0d1117;
		border: 1px solid #1c2940;
		border-radius: 8px;
		overflow: hidden;
		box-shadow: 0 6px 24px #000a;
	}
	.pick {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 14px;
		border: 0;
		background: transparent;
		color: #9fb4d0;
		font: 12px ui-monospace, monospace;
		padding: 9px 12px;
		text-align: left;
		cursor: pointer;
	}
	.pick:hover {
		background: #141d2e;
	}
	.pick.cur {
		color: #cfe3ff;
		background: #16233a;
	}
	.pwhere {
		color: #3d4f6d;
		font-size: 10px;
	}
	.pick.cur .pwhere {
		color: #6f8ab5;
	}
	.pane.focused {
		border-color: #22304a;
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
