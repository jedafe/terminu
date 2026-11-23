<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { api } from '$lib/api';
	import { me } from '$lib/stores';

	let { children } = $props();
	let checked = $state(false);

	onMount(async () => {
		try {
			me.set(await api.me());
		} catch {
			me.set(null);
		}
		checked = true;
	});

	$effect(() => {
		if (!checked) return;
		const onLogin = $page.url.pathname === '/login';
		if (!$me && !onLogin) goto('/login');
		if ($me && onLogin) goto('/');
	});
</script>

{#if checked}
	{@render children()}
{:else}
	<div class="boot">terminu</div>
{/if}

<style>
	.boot {
		height: 100dvh;
		display: grid;
		place-items: center;
		background: #0a0e14;
		color: #33415c;
		font: 15px ui-monospace, monospace;
		letter-spacing: 0.3em;
	}
</style>
