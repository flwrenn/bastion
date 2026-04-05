<script lang="ts">
	import { wallet } from '$lib/wallet.svelte';
	import { account } from '$lib/account.svelte';
	import { etherscanAddress } from '$lib/indexer.svelte';

	function truncate(hex: string, head = 6, tail = 4): string {
		if (hex.length <= head + tail + 1) return hex;
		return hex.slice(0, head) + '\u2026' + hex.slice(-tail);
	}

	$effect(() => {
		if (wallet.address && wallet.correctChain) {
			account.load(wallet.address);
		} else {
			account.reset();
		}
	});
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

		{#if wallet.correctChain}
			<h2 class="mt-8 mb-4 text-xl font-bold">Smart Account</h2>
			<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
				<dl class="space-y-3">
					{#if account.smartAccountAddress}
						<div class="flex justify-between">
							<dt class="text-zinc-400">Account Address</dt>
							<dd class="font-mono text-sm">
								<a
									href={etherscanAddress(account.smartAccountAddress)}
									target="_blank"
									rel="noopener noreferrer"
									class="text-indigo-400 hover:text-indigo-300"
								>
									{truncate(account.smartAccountAddress)}
								</a>
							</dd>
						</div>
						<div class="flex justify-between">
							<dt class="text-zinc-400">Status</dt>
							<dd>
								{#if account.deploying}
									<span class="text-yellow-400">Deploying...</span>
								{:else if account.deployed}
									<span class="text-green-400">Deployed</span>
								{:else}
									<span class="text-zinc-400">Not deployed</span>
								{/if}
							</dd>
						</div>
					{:else if !account.error}
						<div class="text-zinc-400">Loading account...</div>
					{/if}

					{#if account.deployTxHash}
						<div class="flex justify-between">
							<dt class="text-zinc-400">UserOp Hash</dt>
							<dd class="font-mono text-sm">
								<a
									href="https://jiffyscan.xyz/userOpHash/{account.deployTxHash}?network=sepolia"
									target="_blank"
									rel="noopener noreferrer"
									class="text-indigo-400 hover:text-indigo-300"
								>
									{truncate(account.deployTxHash)}
								</a>
							</dd>
						</div>
					{/if}
				</dl>

				{#if account.error}
					<p class="mt-4 text-sm text-red-400">{account.error}</p>
				{/if}

				{#if account.smartAccountAddress && !account.deployed && !account.deploying}
					<button
						type="button"
						class="mt-4 w-full cursor-pointer rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
						onclick={() => account.deploy()}
					>
						Deploy Account
					</button>
				{/if}
			</div>
		{/if}
	{:else}
		<div class="py-24 text-center">
			<h1 class="mb-3 text-2xl font-bold">Bastion</h1>
			<p class="text-zinc-400">Connect your wallet to get started.</p>
		</div>
	{/if}
</div>
