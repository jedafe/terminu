<!-- Ref: .teos/sections/11-web-ui.md — KeyBar: the missing-modifier layer.
     All targets ≥44px; every key funnels through inject() → same input
     path as physical keys. Ctrl/Shift/Alt are sticky — one tap arms,
     the next key from either surface sends modified, then disarms. -->
<script lang="ts">
	import { kbdMods, modKey, disarmMods } from '$lib/stores';

	let {
		oninject,
		onpaste,
		ontogglekbd,
		kbdOn = false
	}: {
		oninject: (seq: string) => void;
		onpaste: () => void;
		ontogglekbd?: () => void;
		kbdOn?: boolean;
	} = $props();

	// Sticky modifiers live in kbdMods so armed state is shared with the
	// InputLine — keybar-Ctrl + keyboard-c sends \x03. One keypress from
	// either surface consumes them.
	function toggle(k: 'ctrl' | 'alt' | 'shift') {
		kbdMods.update((m) => ({ ...m, [k]: !m[k] }));
	}

	function fire(seq: string) {
		oninject(modKey(seq));
		disarmMods();
	}

	// ⏎ sends Alt+Enter (\x1b\r) — devin's documented multiline binding
	// for terminals without kitty keyboard protocol (it shows an
	// "Alt+Enter for multiline prompts" tip on non-kitty terminals;
	// xterm.js is one). Shift+Enter's CSI-u only exists under kitty.
	const keys: { label: string; seq: string; wide?: boolean }[] = [
		{ label: 'Esc', seq: '\x1b' },
		{ label: '⏎', seq: '\x1b\r' },
		{ label: 'Tab', seq: '\t' },
		{ label: '↑', seq: '\x1b[A' },
		{ label: '↓', seq: '\x1b[B' },
		{ label: '←', seq: '\x1b[D' },
		{ label: '→', seq: '\x1b[C' },
		{ label: 'Home', seq: '\x1b[H' },
		{ label: 'End', seq: '\x1b[F' },
		{ label: 'PgUp', seq: '\x1b[5~' },
		{ label: 'PgDn', seq: '\x1b[6~' },
		{ label: '|', seq: '|' },
		{ label: '~', seq: '~' },
		{ label: '/', seq: '/' },
		{ label: '-', seq: '-' },
		{ label: '^C', seq: '\x03' }
	];
</script>

<div class="keybar">
	{#if ontogglekbd}
		<button
			class="k mod"
			class:armed={kbdOn}
			title="type — opens the input line (reliable mobile input)"
			onpointerdown={(e) => e.preventDefault()}
			onclick={ontogglekbd}>⌨</button
		>
	{/if}
	<!-- Every button swallows pointerdown's focus move — blurring the
	     InputLine would drop the OS keyboard mid-combo. -->
	<button class="k mod" class:armed={$kbdMods.ctrl} onpointerdown={(e) => e.preventDefault()} onclick={() => toggle('ctrl')}>Ctrl</button>
	<button class="k mod" class:armed={$kbdMods.shift} onpointerdown={(e) => e.preventDefault()} onclick={() => toggle('shift')}>Shift</button>
	<button class="k mod" class:armed={$kbdMods.alt} onpointerdown={(e) => e.preventDefault()} onclick={() => toggle('alt')}>Alt</button>
	{#each keys as k}
		<button class="k" onpointerdown={(e) => e.preventDefault()} onclick={() => fire(k.seq)}
			>{k.label}</button
		>
	{/each}
	<button class="k" onpointerdown={(e) => e.preventDefault()} onclick={onpaste}>Paste</button>
</div>

<style>
	.keybar {
		display: flex;
		gap: 5px;
		padding: 6px 8px;
		padding-bottom: calc(6px + env(safe-area-inset-bottom, 0px));
		background: #0d1117;
		overflow-x: auto;
		scrollbar-width: none;
		flex-shrink: 0;
	}
	.keybar::-webkit-scrollbar {
		display: none;
	}
	/* The keybar is a touch affordance — a mouse-primary device has a
	   physical keyboard for everything on it. */
	@media (pointer: fine) {
		.keybar {
			display: none;
		}
	}
	.k {
		min-width: 44px;
		min-height: 44px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #141b26;
		color: #9db2ce;
		font: 14px/1 ui-monospace, monospace;
		padding: 0 10px;
		flex-shrink: 0;
		touch-action: manipulation;
	}
	.k:active {
		background: #2a3956;
		color: #e6e6e6;
	}
	.mod.armed {
		background: #1f4d3a;
		border-color: #2f7d5b;
		color: #9df0c6;
	}
</style>
