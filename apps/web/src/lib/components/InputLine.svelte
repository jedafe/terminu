<!-- Ref: .teos/sections/11-web-ui.md — InputLine: the reliable mobile
     typing path. xterm's hidden textarea fails on Android (IME composition,
     focus lost to text-selection in select mode). A real DOM input resolves
     composition inside the field; we diff its value into PTY bytes. Enter →
     \r + clear, Backspace on empty → \x7f (PTY buffer), Tab → \t, Esc → \x1b. -->
<script lang="ts">
	import { onMount } from 'svelte';
	let { oninject, onclose }: { oninject: (seq: string) => void; onclose?: () => void } = $props();

	let inp: HTMLInputElement;
	let prev = '';

	onMount(() => inp.focus());

	// Prefix-diff the field's value into keystrokes: deleted tail →
	// backspaces, added tail → bytes. Middle edits/paste fall out of the
	// same math. Code-point aware (emoji/CJK count as one key each way).
	function onInput() {
		const v = inp.value;
		if (v === prev) return;
		const a = Array.from(prev);
		const b = Array.from(v);
		let p = 0;
		while (p < a.length && p < b.length && a[p] === b[p]) p++;
		const back = '\x7f'.repeat(a.length - p);
		const add = b.slice(p).join('');
		if (back) oninject(back);
		if (add) oninject(add);
		prev = v;
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			oninject('\r');
			inp.value = '';
			prev = '';
		} else if (e.key === 'Backspace' && inp.value === '') {
			e.preventDefault();
			oninject('\x7f'); // field is empty but the PTY line isn't
		} else if (e.key === 'Tab') {
			e.preventDefault();
			oninject('\t');
		} else if (e.key === 'Escape') {
			e.preventDefault();
			oninject('\x1b');
		}
	}
</script>

<div class="iline">
	<input
		bind:this={inp}
		class="iin"
		oninput={onInput}
		onkeydown={onKey}
		placeholder="type — sends to session"
		autocapitalize="off"
		autocorrect="off"
		autocomplete="off"
		spellcheck="false"
		enterkeyhint="send"
	/>
	{#if onclose}
		<button
			class="ix"
			aria-label="hide keyboard bar"
			onpointerdown={(e) => e.preventDefault()}
			onclick={onclose}>✕</button
		>
	{/if}
</div>

<style>
	.iline {
		display: flex;
		gap: 6px;
		padding: 6px 8px;
		background: #0d1117;
		border-top: 1px solid #1a2334;
		flex-shrink: 0;
	}
	.iin {
		flex: 1;
		height: 42px;
		border: 1px solid #2b3b55;
		border-radius: 9px;
		background: #11161f;
		color: #e6e6e6;
		padding: 0 12px;
		font: 15px ui-monospace, monospace;
	}
	.iin:focus {
		outline: none;
		border-color: #4cc9f0;
	}
	.ix {
		min-width: 42px;
		height: 42px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #141b26;
		color: #9db2ce;
		font-size: 14px;
		flex-shrink: 0;
	}
</style>
