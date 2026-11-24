<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { me } from '$lib/stores';

	let username = $state('');
	let password = $state('');
	let err = $state('');
	let busy = $state(false);

	async function submit(e: Event) {
		e.preventDefault();
		busy = true;
		err = '';
		try {
			await api.login(username, password);
			me.set(await api.me());
		} catch (e) {
			err =
				e instanceof ApiError && e.code === 'rate_limited'
					? 'too many attempts — try again shortly'
					: e instanceof ApiError && e.code !== 'unauthorized'
						? e.message
						: 'invalid credentials';
		} finally {
			busy = false;
		}
	}
</script>

<div class="wrap">
	<form class="card" onsubmit={submit}>
		<div class="logo">terminu</div>
		<div class="sub">control room for your agents</div>
		<input
			bind:value={username}
			placeholder="username"
			autocomplete="username"
			autocapitalize="off"
		/>
		<input
			bind:value={password}
			type="password"
			placeholder="password"
			autocomplete="current-password"
		/>
		{#if err}<div class="err">{err}</div>{/if}
		<button disabled={busy}>{busy ? '…' : 'sign in'}</button>
	</form>
</div>

<style>
	.wrap {
		min-height: 100dvh;
		display: grid;
		place-items: center;
		background: #0a0e14;
		padding: 20px;
	}
	.card {
		width: 100%;
		max-width: 340px;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.logo {
		color: #4cc9f0;
		font: 600 22px ui-monospace, monospace;
		letter-spacing: 0.25em;
		text-align: center;
	}
	.sub {
		color: #4a5568;
		font: 12px ui-monospace, monospace;
		text-align: center;
		margin-bottom: 12px;
	}
	input {
		height: 48px;
		border: 1px solid #22304a;
		border-radius: 10px;
		background: #11161f;
		color: #e6e6e6;
		padding: 0 14px;
		font: 15px ui-monospace, monospace;
	}
	input:focus {
		outline: none;
		border-color: #4cc9f0;
	}
	button {
		height: 48px;
		border: 0;
		border-radius: 10px;
		background: #1f4d3a;
		color: #c9f7dd;
		font: 600 15px ui-monospace, monospace;
		margin-top: 4px;
	}
	button:active {
		background: #2a6b51;
	}
	.err {
		color: #ff8080;
		font: 12px ui-monospace, monospace;
		text-align: center;
	}
</style>
