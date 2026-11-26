<!-- Ref: .teos/sections/11-web-ui.md — the mobile-first shell:
     session drawer + pane grid + key bar. Panes are views over
     server-owned sessions — closing one never kills the process. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';
	import interact from 'interactjs';
	import { api, ApiError, type Session } from '$lib/api';
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
		orderedSessions,
		refreshAlerts,
		grid,
		onGrid,
		assignCell,
		cellPos,
		applyPreset,
		sizeCellPair,
		sizeRowPair,
		panels,
		togglePanel,
		setPanelWidth,
		resetPanelWidth,
		toggleCommands,
		prompts,
		wideLayout
	} from '$lib/stores';
	import { isLive, isFinished, needsYouId, pinUnv, tabLabel } from '$lib/sessionfmt';
	import TerminalSurface from '$lib/components/TerminalSurface.svelte';
	import KeyBar from '$lib/components/KeyBar.svelte';
	import InputLine from '$lib/components/InputLine.svelte';
	import LeftPanel from '$lib/components/LeftPanel.svelte';
	import RightPanel from '$lib/components/RightPanel.svelte';

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

	// Pane resize + reorder: dividers redistribute a pair's weight,
	// tab drags swap panes into grid slots. Both persist via scheduleSave.
	let cellCount = $derived($grid.reduce((n, r) => n + r.cells.length, 0));
	// Which frame's session-switcher is open ("r:c"), or null.
	let pickCell = $state<string | null>(null);
	// Boot auto-spawn failure — the empty grid stays up with this under
	// the "open a session" button so a dead spawn isn't silent.
	let bootErr = $state('');
	// The armed frame — tab clicks route their session here.
	let focusCell = $state<string | null>(null);
	let armedCell = $derived(focusCell ?? ($activePane ? cellPos($grid, $activePane) : null));
	// The palette's context scope — the session in the armed pane feeds
	// context-first ranking + {session.cwd} resolution (§04).
	let activeSessionId = $derived($panes.find((p) => p.id === $activePane)?.sessionId ?? '');
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
		// ⌘K / Ctrl-K toggles the commands view in the right panel —
		// same surface as the topbar ⌘ button.
		const pal = (e: KeyboardEvent) => {
			if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
				e.preventDefault();
				toggleCommands();
			}
		};
		window.addEventListener('keydown', pal);
		// Crossing below the breakpoint turns both docked panels into
		// stacked overlays — collapse the right one so the nav rail
		// (the likelier intent on a small screen) stays reachable.
		const syncWide = () => {
			wide = wideLayout();
			const p = get(panels);
			if (!wide && p.left.open && p.right.open)
				panels.update((q) => ({ ...q, right: { ...q.right, open: false } }));
		};
		window.addEventListener('resize', syncWide);
		// Panel open/close/resize/view state persists through the
		// workspace layout — one subscription covers every mutation
		// path (topbar toggles, ⌘K, scrim, drags). Suppressed until the
		// first restore settles so the hydrated value isn't written back.
		let panelsReady = false;
		const unsubPanels = panels.subscribe(() => {
			if (panelsReady) scheduleSave();
		});
		const clock = setInterval(() => (now = Date.now()), 15000);
		void (async () => {
			await refreshSessions();
			await refreshAgents();
			await refreshAlerts();
			try {
				await restoreWorkspace();
			} finally {
				panelsReady = true;
			}
			// A workspace saved on desktop with both panels open restores
			// as two stacked overlays below 900px — run the collapse now
			// (resize only fires on changes, not at boot).
			syncWide();
			wireEvents();
			if ($panes.length === 0) {
				const live = $sessions.find((s) => isLive(s));
				if (live) newPane(live.id);
				else
					await spawn().catch((e) => {
						bootErr = e instanceof ApiError ? e.message : 'spawn failed';
					});
			}
		})();
		return () => {
			clearInterval(clock);
			unsubPanels();
			cleanupDnd();
			window.removeEventListener('keydown', pal);
			window.removeEventListener('resize', syncWide);
			shellEl.removeEventListener('touchmove', guard);
			vv?.removeEventListener('resize', syncVV);
			vv?.removeEventListener('scroll', syncVV);
		};
	});

	function scheduleSave() {
		if (saveTimer) clearTimeout(saveTimer);
		saveTimer = setTimeout(() => void saveWorkspace(), 1200);
	}

	// ≥900px the panels dock as resizable columns; below that they're
	// overlay drawers — `wide` tracks the breakpoint for row-click and
	// openNew behavior (CSS handles the actual positioning).
	let wide = $state(wideLayout());
	// Bound into LeftPanel — the +new block expands when the topbar
	// "+" or empty-grid "open a session" lands here.
	let lpNew = $state(false);
	function openNew() {
		panels.update((p) => ({
			left: { ...p.left, open: true },
			right: wide ? p.right : { ...p.right, open: false }
		}));
		lpNew = true;
	}
	// Overlay-mode helpers: a session row tap dismisses the drawer it
	// came from; the backdrop dismisses whichever panel is up.
	function closeLeftOverlay() {
		if (!wide) panels.update((p) => ({ ...p, left: { ...p.left, open: false } }));
	}
	function closeBoth() {
		panels.update((p) => ({
			left: { ...p.left, open: false },
			right: { ...p.right, open: false }
		}));
	}
	function closeRight() {
		panels.update((p) => ({ ...p, right: { ...p.right, open: false } }));
	}

	// An opening overlay panel covers the grid — a cell picker left
	// floating above it (z40 > panel z21) would be unreachable UI.
	$effect(() => {
		if (!wide && ($panels.left.open || $panels.right.open)) pickCell = null;
	});
	// prompts entries belong to live sessions — prune on list refresh
	// so ended sessions don't accumulate as null tombstones. Panes
	// count as live: a ws frame can land before refreshSessions lists
	// a just-spawned session.
	$effect(() => {
		const ids = new Set([...$sessions.map((s) => s.id), ...$panes.map((p) => p.sessionId)]);
		prompts.update((m) => {
			let changed = false;
			const n: typeof m = {};
			for (const k of Object.keys(m)) {
				if (ids.has(k)) n[k] = m[k];
				else changed = true;
			}
			return changed ? n : m;
		});
	});

	// Panel edge drag — same idiom as pairDrag: clamped px width, a
	// tap without drag resets to the default.
	function panelDrag(e: PointerEvent, side: 'left' | 'right') {
		if (e.button !== 0 || !wideLayout()) return;
		e.preventDefault();
		const d = e.currentTarget as HTMLElement;
		try {
			d.setPointerCapture(e.pointerId);
		} catch {
			// synthetic/stale pointer — capture is a nicety, not a requirement
		}
		const startX = e.clientX;
		const startW = get(panels)[side].w;
		const sign = side === 'left' ? 1 : -1;
		let moved = false;
		const move = (ev: PointerEvent) => {
			const w = startW + sign * (ev.clientX - startX);
			if (!moved && Math.abs(w - startW) > 4) moved = true;
			if (moved) setPanelWidth(side, w);
		};
		const up = () => {
			d.removeEventListener('pointermove', move);
			d.removeEventListener('pointerup', up);
			d.removeEventListener('pointercancel', up);
			if (!moved) resetPanelWidth(side);
			scheduleSave();
		};
		d.addEventListener('pointermove', move);
		d.addEventListener('pointerup', up);
		d.addEventListener('pointercancel', up);
	}

	async function spawn(shell?: string) {
		const s = await api.spawn({ cols: 120, rows: 30, shell });
		await spawned(s);
	}

	// Shared spawn landing — profile launches arrive as a fresh session
	// that gets its own tab + frame.
	async function spawned(s: Session) {
		// The session exists server-side even if the refetch fails — the
		// next ws event resyncs; never drop the tab on a read error.
		await refreshSessions().catch(() => {});
		newPane(s.id);
		scheduleSave();
	}

	// Spawn/launch rejections surface in the left panel's +new block —
	// these are the onspawn/onlaunch bodies, errors must propagate.
	function onspawn(shell?: string) {
		return spawn(shell);
	}
	function onlaunch(id: number, o?: { variant?: string; resume?: boolean }) {
		return launchProf(id, o?.resume, o?.variant);
	}

	async function launchProf(id: number, resume = false, variant?: string) {
		const res = await api.launchProfile(id, { resume, variant, cols: 120, rows: 30 });
		await spawned(res.session);
	}

	function onopen(s: Session) {
		const existing = $panes.find((p) => p.sessionId === s.id);
		if (existing) {
			activePane.set(existing.id);
		} else {
			newPane(s.id);
		}
		closeLeftOverlay();
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

	function onend(id: string, e: Event) {
		return endOrDelete(id, e);
	}
	function onpurge(id: string, e: Event) {
		return purgeLog(id, e);
	}
	function onclearfin(e: Event) {
		return clearFinished(e);
	}
	function onpreset(shape: number[]) {
		applyPreset(shape);
		scheduleSave();
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
	<!-- top bar: left panel toggle + panes + right toggles -->
	<header class="topbar">
		<button class="tb" aria-label="sessions panel" onclick={() => togglePanel('left')}>
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
					{tabLabel(p)}{#if pinUnv(p.sessionId)}<span class="tabpin" title="adapter pin unverified">!</span>{/if}
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
			<button class="tb add" aria-label="new session" onclick={openNew}>+</button>
			<button class="tb" aria-label="commands" title="commands (⌘K)" onclick={toggleCommands}>⌘</button>
		</div>
		<button class="tb" aria-label="info panel" onclick={() => togglePanel('right')}>◧</button>
	</header>

	<!-- overlay-mode scrim — docked panels never need one; drawers get
	     a tap-outside dismiss under the same z-order the panels use. -->
	{#if !wide && ($panels.left.open || $panels.right.open)}
		<!-- svelte-ignore a11y_no_static_element_interactions — backdrop only -->
		<div class="dshade" onpointerdown={closeBoth} oncontextmenu={(e) => e.preventDefault()}></div>
	{/if}

	<!-- body row: left panel | grab | terminal grid | grab | right panel.
	     Docked ≥900px (in-flow, grid shrinks); overlay drawers below. -->
	<div class="body">
		{#if $panels.left.open}
			<aside class="panel left" style:width={wide ? `${$panels.left.w}px` : undefined}>
				<LeftPanel
					{now}
					{onopen}
					{onspawn}
					{onlaunch}
					{onend}
					{onpurge}
					{onclearfin}
					{onpreset}
					onrow={closeLeftOverlay}
					bind:newopen={lpNew}
				/>
			</aside>
			<!-- svelte-ignore a11y_no_static_element_interactions — splitter -->
			<div class="grab" role="separator" onpointerdown={(e) => panelDrag(e, 'left')}></div>
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
										onprompt={(pr) =>
											prompts.update((m) => ({ ...m, [p.sessionId]: pr }))}
										onpromptclear={() =>
											prompts.update((m) => ({ ...m, [p.sessionId]: null }))}
									/>
								{/key}
							</div>
							{#if cellCount > 1}
								<div
									class="fgrab"
									data-pid={p.id}
									role="button"
									tabindex="-1"
									title="drag onto another frame or a tab to swap">⠿</div
								>
							{/if}
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
				<button class="bigspawn" onclick={openNew}>open a session</button>
				{#if bootErr}<div class="sperr">{bootErr}</div>{/if}
			</div>
		{/if}
	</main>

		{#if $panels.right.open}
			<!-- svelte-ignore a11y_no_static_element_interactions — splitter -->
			<div class="grab" role="separator" onpointerdown={(e) => panelDrag(e, 'right')}></div>
			<aside class="panel right" style:width={wide ? `${$panels.right.w}px` : undefined}>
				<RightPanel sessionId={activeSessionId} onclose={closeRight} />
			</aside>
		{/if}
	</div>

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

<style>
	.shell {
		position: relative;
		height: 100dvh;
		display: flex;
		flex-direction: column;
		background: #0a0e14;
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
	/* Panels dock in the body row ≥900px — in-flow, so the grid shrinks
	   rather than being overlaid. Below the breakpoint they become
	   fixed overlay drawers with the .dshade scrim. */
	.body {
		flex: 1;
		display: flex;
		min-height: 0;
		min-width: 0;
	}
	.panel {
		flex-shrink: 0;
		min-width: 0;
		background: #0d1117;
		overflow: hidden;
		display: flex;
	}
	.panel.left {
		border-right: 1px solid #1a2334;
	}
	.panel.right {
		border-left: 1px solid #1a2334;
	}
	.grab {
		flex-shrink: 0;
		width: 8px;
		margin: 0 -4px;
		z-index: 5;
		cursor: col-resize;
		touch-action: none;
	}
	.grab:hover,
	.grab:active {
		background: #22304a;
	}
	@media (max-width: 899.5px) {
		.panel {
			position: fixed;
			top: calc(50px + env(safe-area-inset-top, 0px));
			bottom: 0;
			width: min(85vw, 360px);
			z-index: 21;
			box-shadow: 0 0 32px #000a;
		}
		.panel.left {
			left: 0;
			border-right: 1px solid #22304a;
		}
		.panel.right {
			right: 0;
			border-left: 1px solid #22304a;
		}
		.grab {
			display: none;
		}
	}
	.dshade {
		position: fixed;
		top: calc(50px + env(safe-area-inset-top, 0px));
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 19;
	}
	.tabpin {
		color: #f0b429;
		font: 11px ui-monospace, monospace;
		margin-left: 5px;
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
		/* Flex-row child of .body — without this a wide terminal's
		   min-content defeats the shrink and pushes the panels out. */
		min-width: 0;
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
	.sperr {
		margin-top: 14px;
		font: 11px ui-monospace, monospace;
		color: #ff8080;
	}
</style>
