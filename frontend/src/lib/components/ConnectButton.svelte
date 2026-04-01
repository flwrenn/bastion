<script lang="ts">
	import { wallet } from '$lib/wallet.svelte';

	function truncate(addr: string) {
		return addr.slice(0, 6) + '…' + addr.slice(-4);
	}
</script>

{#if wallet.connected}
	<div class="flex items-center gap-3">
		{#if !wallet.correctChain}
			<span class="text-sm text-red-400">{wallet.error ?? 'Wrong network'}</span>
		{:else}
			<span class="text-sm text-zinc-400">{wallet.chainName}</span>
		{/if}
		<span class="font-mono text-sm text-zinc-200">{truncate(wallet.address!)}</span>
		<button
			type="button"
			class="cursor-pointer rounded bg-zinc-700 px-3 py-1.5 text-sm text-zinc-200 hover:bg-zinc-600"
			onclick={() => wallet.disconnect()}
		>
			Disconnect
		</button>
	</div>
{:else}
	<div class="flex items-center gap-3">
		{#if wallet.error}
			<span class="text-sm text-red-400">{wallet.error}</span>
		{/if}
		<button
			type="button"
			class="cursor-pointer rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-indigo-500"
			onclick={() => wallet.connect()}
		>
			Connect Wallet
		</button>
	</div>
{/if}
