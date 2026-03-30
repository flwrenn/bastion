<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import ConnectButton from '$lib/components/ConnectButton.svelte';

	let { children } = $props();
</script>

<svelte:head><link rel="icon" href={favicon} /></svelte:head>

<div class="flex min-h-screen flex-col bg-zinc-900 text-zinc-100">
	<header class="flex items-center justify-between border-b border-zinc-800 px-6 py-4">
		<a href="/" class="text-lg font-semibold tracking-tight">Bastion</a>
		<ConnectButton />
	</header>
	<main class="flex-1 px-6 py-8">
		{@render children()}
	</main>
</div>
