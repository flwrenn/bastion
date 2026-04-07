<script lang="ts">
	import { encodeFunctionData, formatUnits } from 'viem';
	import { publicClient, wallet } from '$lib/wallet.svelte';
	import { CounterAbi } from '$lib/contracts/Counter';
	import { counterAddress } from '$lib/config';
	import { sendUserOp } from '$lib/userOp';
	import { etherscanTx } from '$lib/explorer';

	let { accountAddress }: { accountAddress: `0x${string}` } = $props();

	let count = $state<bigint | null>(null);
	let loading = $state(false);
	let sending = $state(false);
	let error = $state<string | null>(null);
	let lastOpHash = $state<`0x${string}` | null>(null);

	async function loadCount() {
		loading = true;
		error = null;
		try {
			count = await publicClient.readContract({
				address: counterAddress(),
				abi: CounterAbi,
				functionName: 'getCount',
				args: [accountAddress]
			});
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			error = err.shortMessage ?? err.message ?? 'Failed to read counter';
		} finally {
			loading = false;
		}
	}

	async function increment() {
		const walletClient = wallet.client;
		if (!walletClient) {
			error = 'Wallet not connected';
			return;
		}

		sending = true;
		error = null;
		lastOpHash = null;

		try {
			const result = await sendUserOp(walletClient, accountAddress, [
				{
					to: counterAddress(),
					data: encodeFunctionData({
						abi: CounterAbi,
						functionName: 'increment'
					})
				}
			]);

			lastOpHash = result.userOpHash;

			if (!result.success) {
				error = 'UserOperation reverted on-chain';
				return;
			}

			await loadCount();
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			error = err.shortMessage ?? err.message ?? 'Increment failed';
		} finally {
			sending = false;
		}
	}

	$effect(() => {
		// Reload when accountAddress changes.
		accountAddress;
		loadCount();
	});
</script>

<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
	<h3 class="text-lg font-semibold">Counter</h3>

	<dl class="mt-4 space-y-3">
		<div class="flex justify-between">
			<dt class="text-zinc-400">Count</dt>
			<dd class="font-mono text-sm">
				{#if loading && count === null}
					<span class="text-zinc-500">Loading…</span>
				{:else if count !== null}
					{count.toString()}
				{:else}
					<span class="text-zinc-500">—</span>
				{/if}
			</dd>
		</div>
	</dl>

	{#if lastOpHash}
		<p class="mt-3 text-xs text-zinc-500">
			Last op:
			<a
				href={etherscanTx(lastOpHash)}
				target="_blank"
				rel="noopener noreferrer"
				class="text-indigo-400 hover:text-indigo-300"
			>
				{lastOpHash.slice(0, 10)}…{lastOpHash.slice(-6)}
			</a>
		</p>
	{/if}

	{#if error}
		<p class="mt-3 text-sm text-red-400">{error}</p>
	{/if}

	<button
		onclick={increment}
		disabled={sending}
		class="mt-4 w-full cursor-pointer rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
	>
		{sending ? 'Sending UserOp…' : 'Increment'}
	</button>
</div>
