<!-- Ref: .teos/sections/11-web-ui.md — InputLine: the reliable mobile
     typing path. xterm's hidden textarea fails on Android (IME composition,
     focus lost to text-selection in select mode). A real DOM input resolves
     composition inside the field; we diff its value into PTY bytes. Enter →
     \r + clear, Backspace on empty → \x7f (PTY buffer), Tab → \t, Esc → \x1b. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';
	import { kbdMods, modKey, disarmMods } from '$lib/stores';
	let { oninject, onclose }: { oninject: (seq: string) => void; onclose?: () => void } = $props();

	let inp: HTMLInputElement;
	let prev = '';
	let focused = $state(false);
	let hasText = $state(false);

	onMount(() => {
		inp.focus();
		// Mobile browsers refuse programmatic focus without a gesture —
		// retry on real pointer contact so the first touch lands the
		// caret (and raises the keyboard) instead of needing a second tap.
		const arm = () => {
			inp.focus();
			if (document.activeElement === inp) window.removeEventListener('pointerdown', arm);
		};
		window.addEventListener('pointerdown', arm);
		return () => window.removeEventListener('pointerdown', arm);
	});

	// Armed keybar modifiers apply to the next keystroke from here too —
	// keybar-Ctrl + keyboard-c must send \x03. Only the first char takes
	// the mod (one sticky = one keypress); then they disarm.
	function injectMod(seq: string) {
		const armed = get(kbdMods);
		if (armed.ctrl || armed.alt || armed.shift) {
			oninject(modKey(seq));
			disarmMods();
			return;
		}
		oninject(seq);
	}

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
		if (add) {
			const armed = get(kbdMods);
			if (armed.ctrl || armed.alt || armed.shift) {
				const [first, ...rest] = Array.from(add);
				oninject(modKey(first) + rest.join(''));
				disarmMods();
			} else {
				oninject(add);
			}
		}
		prev = v;
		hasText = v !== '';
	}

	// Mobile IMEs deliver Enter three different ways: a real keydown,
	// a beforeinput insertLineBreak (Gboard), or a bare form submit —
	// each path funnels here, deduped so one press = one \r.
	let lastEnter = 0;
	function sendEnter() {
		const t = Date.now();
		if (t - lastEnter < 80) return;
		lastEnter = t;
		injectMod('\r');
		inp.value = '';
		prev = '';
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			sendEnter();
		} else if (e.key === 'Backspace' && inp.value === '') {
			e.preventDefault();
			injectMod('\x7f'); // field is empty but the PTY line isn't
		} else if (e.key === 'Tab') {
			e.preventDefault();
			injectMod('\t');
		} else if (e.key === 'Escape') {
			e.preventDefault();
			injectMod('\x1b');
		}
	}

	function onBeforeInput(e: InputEvent) {
		if (e.inputType === 'insertLineBreak' || e.inputType === 'insertParagraph') {
			e.preventDefault();
			sendEnter();
		}
	}
</script>

<form
	class="iline"
	onsubmit={(e) => {
		e.preventDefault();
		sendEnter();
	}}
>
	{#if !focused && !hasText}
		<!-- fake block cursor: a real caret needs focus, which mobile
		     browsers won't grant without a gesture — this marks the
		     typing surface until the first touch lands it -->
		<span class="fakecaret" aria-hidden="true"></span>
	{/if}
	<input
		bind:this={inp}
		class="iin"
		oninput={onInput}
		onkeydown={onKey}
		onbeforeinput={onBeforeInput}
		placeholder="type — sends to session"
		autocapitalize="off"
		autocorrect="off"
		autocomplete="off"
		spellcheck="false"
		enterkeyhint="send"
		onfocus={() => (focused = true)}
		onblur={() => (focused = false)}
	/>
	{#if onclose}
		<button
			type="button"
			class="ix"
			aria-label="hide keyboard bar"
			onpointerdown={(e) => e.preventDefault()}
			onclick={onclose}>✕</button
		>
	{/if}
</form>

<style>
	form.iline {
		position: relative;
		display: flex;
		gap: 6px;
		padding: 6px 8px;
		margin: 0;
		background: #0d1117;
		border-top: 1px solid #1a2334;
		flex-shrink: 0;
	}
	.iin {
		flex: 1;
		height: 44px;
		border: 1px solid #2b3b55;
		border-radius: 9px;
		background: #11161f;
		color: #e6e6e6;
		padding: 0 12px 0 24px; /* left room for the fake caret */
		font: 15px ui-monospace, monospace;
	}
	.fakecaret {
		position: absolute;
		left: 17px;
		top: 50%;
		transform: translateY(-50%);
		width: 9px;
		height: 19px;
		background: #4cc9f0;
		border-radius: 1px;
		pointer-events: none;
		animation: caretblink 1.1s step-end infinite;
	}
	@keyframes caretblink {
		50% {
			opacity: 0;
		}
	}
	.iin:focus {
		outline: none;
		border-color: #4cc9f0;
	}
	.ix {
		min-width: 44px;
		height: 44px;
		border: 1px solid #22304a;
		border-radius: 9px;
		background: #141b26;
		color: #9db2ce;
		font-size: 14px;
		flex-shrink: 0;
	}
</style>
