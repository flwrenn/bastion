<script lang="ts">
	import { encodeFunctionData, formatUnits } from 'viem';
	import { publicClient, wallet } from '$lib/wallet.svelte';
	import { FaucetTokenAbi } from '$lib/contracts/FaucetToken';
	import { faucetTokenAddress } from '$lib/config';
	import { sendUserOp } from '$lib/userOp';
	import { etherscanTx, truncateHex } from '$lib/explorer';

	let { accountAddress }: { accountAddress: `0x${string}` } = $props();

	let balance = $state<bigint | null>(null);
	let decimals = $state<number>(18);
	let symbol = $state<string>('BFT');
	let loading = $state(false);
	let claiming = $state(false);
	let error = $state<string | null>(null);
	let lastUserOpHash = $state<`0x${string}` | null>(null);
	let lastTxHash = $state<`0x${string}` | null>(null);

	async function loadBalance() {
		loading = true;
		error = null;
		try {
			const tokenAddr = faucetTokenAddress();

			const [bal, dec, sym] = await Promise.all([
				publicClient.readContract({
					address: tokenAddr,
					abi: FaucetTokenAbi,
					functionName: 'balanceOf',
					args: [accountAddress]
				}),
				publicClient.readContract({
					address: tokenAddr,
					abi: FaucetTokenAbi,
					functionName: 'decimals'
				}),
				publicClient.readContract({
					address: tokenAddr,
					abi: FaucetTokenAbi,
					functionName: 'symbol'
				})
			]);

			balance = bal;
			decimals = dec;
			symbol = sym;
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			error = err.shortMessage ?? err.message ?? 'Failed to read token balance';
		} finally {
			loading = false;
		}
	}

	async function claim() {
		const walletClient = wallet.client;
		if (!walletClient) {
			error = 'Wallet not connected';
			return;
		}

		claiming = true;
		error = null;
		lastUserOpHash = null;
		lastTxHash = null;

		try {
			const result = await sendUserOp(walletClient, accountAddress, [
				{
					to: faucetTokenAddress(),
					data: encodeFunctionData({
						abi: FaucetTokenAbi,
						functionName: 'claim'
					})
				}
			]);

			lastUserOpHash = result.userOpHash;
			lastTxHash = result.txHash;

			if (!result.success) {
				error = 'UserOperation reverted on-chain';
				return;
			}

			await loadBalance();
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			error = err.shortMessage ?? err.message ?? 'Claim failed';
		} finally {
			claiming = false;
		}
	}

	/** Format a token balance string-based to avoid Number precision loss. */
	function formatBalance(raw: bigint, dec: number): string {
		const str = formatUnits(raw, dec);
		const dot = str.indexOf('.');
		if (dot === -1) return str;
		// Keep up to 2 decimal places, strip trailing zeros.
		const trimmed = str.slice(0, dot + 3).replace(/\.?0+$/, '');
		return trimmed || '0';
	}

	$effect(() => {
		accountAddress;
		loadBalance();
	});
</script>

<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
	<h3 class="text-lg font-semibold">Faucet Token</h3>

	<dl class="mt-4 space-y-3">
		<div class="flex justify-between">
			<dt class="text-zinc-400">Balance</dt>
			<dd class="font-mono text-sm">
				{#if loading && balance === null}
					<span class="text-zinc-500">Loading…</span>
				{:else if balance !== null}
					{formatBalance(balance, decimals)}
					<span class="text-zinc-400">{symbol}</span>
				{:else}
					<span class="text-zinc-500">—</span>
				{/if}
			</dd>
		</div>
	</dl>

	{#if lastUserOpHash || lastTxHash}
		<div class="mt-3 space-y-1 text-xs text-zinc-500">
			{#if lastUserOpHash}
				<p>
					UserOp:
					<a
						href="https://jiffyscan.xyz/userOpHash/{lastUserOpHash}?network=sepolia"
						target="_blank"
						rel="noopener noreferrer"
						class="text-indigo-400 hover:text-indigo-300"
					>
						{truncateHex(lastUserOpHash)}
					</a>
				</p>
			{/if}
			{#if lastTxHash}
				<p>
					Tx:
					<a
						href={etherscanTx(lastTxHash)}
						target="_blank"
						rel="noopener noreferrer"
						class="text-indigo-400 hover:text-indigo-300"
					>
						{truncateHex(lastTxHash)}
					</a>
				</p>
			{/if}
		</div>
	{/if}

	{#if error}
		<p class="mt-3 text-sm text-red-400">{error}</p>
	{/if}

	<button
		onclick={claim}
		disabled={claiming}
		class="mt-4 w-full cursor-pointer rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
	>
		{claiming ? 'Sending UserOp…' : 'Claim Tokens'}
	</button>
</div>
