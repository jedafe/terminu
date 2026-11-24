<!-- Ref: .teos/sections/11-web-ui.md — InputLine: the reliable mobile
     typing path. xterm's hidden textarea fails on Android (IME composition,
     focus lost to text-selection in select mode). A real DOM input resolves
     composition inside the field; we diff its value into PTY bytes. Enter →
     \r + clear, Backspace on empty → \x7f (PTY buffer), Tab → \t, Esc → \x1b. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';
	import { kbdMods, modKey, disarmMods } from '$lib/stores';
	let {
		oninject,
		onclose,
		ghost = false,
		armed = true
	}: {
		oninject: (seq: string) => void;
		onclose?: () => void;
		ghost?: boolean;
		armed?: boolean;
	} = $props();

	let inp: HTMLInputElement;
	let prev = '';
	let focused = $state(false);
	let hasText = $state(false);

	const coarse = () =>
		typeof matchMedia !== 'undefined' && matchMedia('(pointer: coarse)').matches;

	// "Always active" on touch devices: the InputLine is the typing
	// surface — pull focus back on every blur except a deliberate move
	// to another field (search box, prompt reply) or the session drawer.
	// xterm's own helper textarea does NOT count — its focus is the
	// unreliable path this component exists to replace.
	export function focus() {
		inp?.focus();
	}
	export function blur() {
		inp?.blur();
	}

	function onBlur() {
		focused = false;
		if (!coarse() || !armed) return;
		const el = document.activeElement as HTMLElement | null;
		// Deliberate focus moves stand — any real control (button,
		// link, field, drawer/keybar/menu contents) keeps it. We only
		// pull focus back when it falls to the page/terminal itself.
		if (el && el !== document.body) {
			if (
				el.closest('.drawer, .topbar') ||
				el.matches(
					'input:not(.xterm-helper-textarea), textarea:not(.xterm-helper-textarea), [contenteditable="true"], button, a[href], select, [role="button"]'
				)
			)
				return;
		}
		requestAnimationFrame(() => inp?.focus());
	}

	onMount(() => {
		// verbatim → Android TYPE_TEXT_FLAG_NO_SUGGESTIONS (kills the
		// suggestion strip); valid per spec but not in TS's DOM lib yet.
		inp.setAttribute('inputmode', 'verbatim');
		if (armed) inp.focus();
		// Re-arm on real pointer contact — persistent, not one-shot —
		// but only on dead space / the terminal surface. Taps on real
		// controls (buttons, links, menus, keybar, drawer) belong to
		// those controls and must not resurrect the keyboard.
		const rearm = (e: PointerEvent) => {
			if (!armed) return;
			const t = e.target as HTMLElement | null;
			if (
				t?.closest(
					'button, a[href], input, textarea, select, [role="button"], .keybar, .drawer, .tools, .poverlay, .topbar'
				)
			)
				return;
			inp.focus();
		};
		window.addEventListener('pointerdown', rearm);
		return () => window.removeEventListener('pointerdown', rearm);
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

<!-- div, not form: an input inside <form> is an autofill candidate —
     Chrome/Android then pins a password/payment/address row above the
     keyboard. Enter is already covered by keydown + beforeinput. -->
<div class:ghost class="iline">
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
		name="tukbd"
		type="search"
		autocapitalize="off"
		autocorrect="off"
		autocomplete="off"
		data-form-type="other"
		data-lpignore="true"
		data-1p-ignore="true"
		data-bwignore
		spellcheck="false"
		enterkeyhint="send"
		onfocus={() => (focused = true)}
		onblur={onBlur}
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
</div>

<style>
	.iline {
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
	/* Ghost: input stays mounted + focused (the typing path stays
	   armed) but the form costs zero screen space. height/opacity —
	   not display/visibility — keep it focusable. Keystrokes already
	   echo in the terminal, so nothing visible is lost. */
	.iline.ghost {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		height: 1px;
		padding: 0;
		border: none;
		pointer-events: none;
		z-index: -1;
	}
	.iline.ghost .iin {
		height: 1px;
		opacity: 0;
		padding: 0;
		border: none;
		caret-color: transparent;
	}
	.iline.ghost .fakecaret,
	.iline.ghost .ix {
		display: none;
	}
</style>
