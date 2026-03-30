<script lang="ts">
	import { wallet } from '$lib/wallet.svelte';
</script>

<div class="mx-auto max-w-xl">
	{#if wallet.connected}
		<h1 class="mb-4 text-2xl font-bold">Dashboard</h1>
		<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
			<dl class="space-y-3">
				<div class="flex justify-between">
					<dt class="text-zinc-400">Address</dt>
					<dd class="font-mono text-sm">{wallet.address}</dd>
				</div>
				<div class="flex justify-between">
					<dt class="text-zinc-400">Network</dt>
					<dd>{wallet.chainName}</dd>
				</div>
			</dl>
		</div>
	{:else}
		<div class="py-24 text-center">
			<h1 class="mb-3 text-2xl font-bold">Bastion</h1>
			<p class="text-zinc-400">Connect your wallet to get started.</p>
		</div>
	{/if}
</div>
