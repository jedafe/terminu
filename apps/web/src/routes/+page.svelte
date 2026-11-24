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
		refreshAgents,
		saveWorkspace,
		restoreWorkspace,
		wireEvents,
		drawerOpen,
		touchMode,
		orderedSessions,
		agentStates,
		alerts,
		refreshAlerts,
		ackAlert
	} from '$lib/stores';
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
					role="button"
					tabindex="0"
					onclick={() => {
						activePane.set(p.id);
						scheduleSave();
					}}
					onkeydown={() => activePane.set(p.id)}
				>
					{#if needsYouId(p.sessionId)}<span class="tabdot needs"></span>{/if}
					{label(sessionOf(p.sessionId))}
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

	<!-- session drawer: alerts, then grouped needs-you / running / finished -->
	{#if $drawerOpen}
		{@const unacked = $alerts.filter((a) => !a.acked_at)}
		{@const needs = $orderedSessions.filter(needsYou)}
		{@const running = $orderedSessions.filter((s) => isLive(s) && !needsYou(s))}
		{@const finished = $orderedSessions.filter(isFinished)}
		<div class="drawer">
			<div class="dhead">
				<span>sessions</span>
				<button class="tb" onclick={spawn}>+ new</button>
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

	<!-- terminal grid -->
	<main class="grid" class:single={$panes.length <= 1}>
		{#each $panes as p (p.id)}
			<section
				class="pane"
				class:focused={$activePane === p.id}
				role="button"
				tabindex="0"
				onclick={() => activePane.set(p.id)}
				onkeydown={(e) => {
					if (e.key === 'Enter' || e.key === ' ') activePane.set(p.id);
				}}
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
						onrequestinput={() => (kbdOpen = true)}
						onpull={termPull}
						onpullrelease={termPullEnd}
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
		padding: 6px 8px;
		padding-top: calc(6px + env(safe-area-inset-top, 0px));
		background: #0d1117;
		border-bottom: 1px solid #1a2334;
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
		height: 44px;
		display: flex;
		align-items: center;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #11161f;
		color: #6b7f9e;
		font: 13px ui-monospace, monospace;
		padding: 0 6px 0 14px;
		white-space: nowrap;
		flex-shrink: 0;
		cursor: pointer;
	}
	.pane-tab.active {
		background: #1c2a44;
		border-color: #4cc9f0;
		color: #e6f2ff;
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
		/* keep downward drags at scroll-top for pull-to-refresh —
		   don't let the browser claim them as overscroll */
		overscroll-behavior: contain;
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
		padding: 12px 14px;
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
