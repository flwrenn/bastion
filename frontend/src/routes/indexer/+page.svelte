<script lang="ts">
	import {
		feed,
		etherscanTx,
		etherscanBlock,
		etherscanAddress,
		formatGasCost,
		truncateHex,
		type FeedStatus
	} from '$lib/indexer.svelte';
	import StatsPanel from '$lib/components/StatsPanel.svelte';

	$effect(() => {
		feed.connect();
		return () => feed.disconnect();
	});

	const statusLabel: Record<FeedStatus, string> = {
		disconnected: 'Disconnected',
		connecting: 'Connecting\u2026',
		connected: 'Live',
		polling: 'Polling (REST)'
	};

	const statusColor: Record<FeedStatus, string> = {
		disconnected: 'bg-zinc-500',
		connecting: 'bg-yellow-500',
		connected: 'bg-green-500',
		polling: 'bg-yellow-500'
	};

	function formatTimestamp(unix: number): string {
		if (!unix) return '\u2014';
		return new Date(unix * 1000).toLocaleString();
	}

	/** True when the paymaster field indicates a sponsored (non-zero) paymaster. */
	function isSponsored(paymaster: string): boolean {
		return !!paymaster && paymaster !== '0x' && paymaster !== '0x' + '0'.repeat(40);
	}

	/**
	 * Best-effort decode of revert reason bytes into a human-readable string.
	 * Handles the standard Solidity Error(string) selector (0x08c379a0) plus
	 * plain UTF-8 bytes. Falls back to raw hex when the decoded string would
	 * contain control characters.
	 */
	function formatRevertReason(hex: string | undefined): string {
		if (!hex || hex === '0x') return '';
		const raw = hex.startsWith('0x') ? hex.slice(2) : hex;

		// Solidity Error(string): 0x08c379a0 + abi.encode(string)
		if (raw.startsWith('08c379a0') && raw.length >= 8 + 128) {
			try {
				// Skip selector (4 bytes) + offset word (32 bytes); next word = length
				const lenHex = raw.slice(8 + 64, 8 + 128);
				const strLen = parseInt(lenHex, 16);
				const dataHex = raw.slice(8 + 128, 8 + 128 + strLen * 2);
				const bytes = new Uint8Array(dataHex.match(/.{2}/g)?.map((b) => parseInt(b, 16)) ?? []);
				const text = new TextDecoder('utf-8', { fatal: false }).decode(bytes);
				if (!/[\x00-\x08\x0e-\x1f]/.test(text)) return text;
			} catch {
				// fall through to raw hex
			}
		}

		// Plain UTF-8 sniff
		try {
			const bytes = new Uint8Array(raw.match(/.{2}/g)?.map((b) => parseInt(b, 16)) ?? []);
			const text = new TextDecoder('utf-8', { fatal: false }).decode(bytes);
			if (text.length > 0 && !/[\x00-\x08\x0e-\x1f]/.test(text)) return text;
		} catch {
			// fall through
		}

		return hex;
	}

	/** Auto-scroll: keep the page scrolled to top when new ops arrive (if user is near top). */
	let prevCount = 0;

	$effect(() => {
		const count = feed.operations.length;
		if (count > prevCount) {
			// Only auto-scroll if the user hasn't scrolled down more than 100px.
			if (window.scrollY < 100) {
				window.scrollTo({ top: 0, behavior: 'smooth' });
			}
		}
		prevCount = count;
	});
</script>

<svelte:head><title>Indexer Live Feed | Bastion</title></svelte:head>

<div class="mx-auto max-w-7xl">
	<!-- Header -->
	<div class="mb-6 flex items-center justify-between">
		<h1 class="text-2xl font-bold">Indexer Live Feed</h1>
		<div class="flex items-center gap-2 text-sm text-zinc-400">
			<span class="inline-block h-2 w-2 rounded-full {statusColor[feed.status]}"></span>
			{statusLabel[feed.status]}
		</div>
	</div>

	<!-- Stats -->
	<StatsPanel stats={feed.stats} />

	{#if feed.operations.length === 0}
		<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 py-16 text-center">
			<p class="text-zinc-400">
				{#if feed.status === 'connected' || feed.status === 'polling'}
					Waiting for new UserOperations&hellip;
				{:else if feed.status === 'connecting'}
					Connecting to indexer&hellip;
				{:else}
					Not connected to indexer.
				{/if}
			</p>
		</div>
	{:else}
		<div class="overflow-x-auto rounded-lg border border-zinc-800">
			<table class="w-full text-left text-sm">
				<thead
					class="border-b border-zinc-700 bg-zinc-800/80 text-xs tracking-wider text-zinc-400 uppercase"
				>
					<tr>
						<th class="px-4 py-3">UserOp Hash</th>
						<th class="px-4 py-3">Sender</th>
						<th class="px-4 py-3">Paymaster</th>
						<th class="px-4 py-3">Status</th>
						<th class="px-4 py-3 text-right">Gas Cost</th>
						<th class="px-4 py-3 text-right">Block</th>
						<th class="px-4 py-3">Time</th>
						<th class="px-4 py-3">Tx</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each feed.operations as op (op.userOpHash)}
						<tr class="hover:bg-zinc-800/40">
							<!-- UserOp Hash -->
							<td class="px-4 py-3 font-mono text-xs text-zinc-300">
								{truncateHex(op.userOpHash, 10, 6)}
							</td>

							<!-- Sender -->
							<td class="px-4 py-3 font-mono text-xs">
								<a
									href={etherscanAddress(op.sender)}
									target="_blank"
									rel="noopener noreferrer"
									class="text-indigo-400 hover:text-indigo-300"
								>
									{truncateHex(op.sender)}
								</a>
							</td>

							<!-- Paymaster -->
							<td class="px-4 py-3 font-mono text-xs">
								{#if isSponsored(op.paymaster)}
									<a
										href={etherscanAddress(op.paymaster)}
										target="_blank"
										rel="noopener noreferrer"
										class="text-indigo-400 hover:text-indigo-300"
									>
										{truncateHex(op.paymaster)}
									</a>
									<span
										class="ml-1.5 inline-block rounded bg-emerald-900/60 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300"
									>
										Sponsored
									</span>
								{:else}
									<span class="text-zinc-500">&mdash;</span>
								{/if}
							</td>

							<!-- Status -->
							<td class="px-4 py-3">
								<div class="flex items-center gap-1.5">
									{#if op.success}
										<span
											class="inline-block rounded bg-green-900/60 px-2 py-0.5 text-xs font-medium text-green-300"
										>
											Success
										</span>
									{:else}
										<span
											class="inline-block rounded bg-red-900/60 px-2 py-0.5 text-xs font-medium text-red-300"
											title={op.revertReason ? formatRevertReason(op.revertReason) : undefined}
										>
											Reverted
										</span>
									{/if}
									{#if op.accountDeployed}
										<span
											class="inline-block rounded bg-blue-900/60 px-2 py-0.5 text-xs font-medium text-blue-300"
											title="Account deployed in this UserOp"
										>
											Deployed
										</span>
									{/if}
								</div>
							</td>

							<!-- Gas Cost -->
							<td class="px-4 py-3 text-right font-mono text-xs text-zinc-300">
								{formatGasCost(op.actualGasCost)}
							</td>

							<!-- Block -->
							<td class="px-4 py-3 text-right font-mono text-xs">
								<a
									href={etherscanBlock(op.blockNumber)}
									target="_blank"
									rel="noopener noreferrer"
									class="text-indigo-400 hover:text-indigo-300"
								>
									{op.blockNumber}
								</a>
							</td>

							<!-- Timestamp -->
							<td class="px-4 py-3 text-xs text-zinc-400">
								{formatTimestamp(op.blockTimestamp)}
							</td>

							<!-- Tx Link -->
							<td class="px-4 py-3">
								<a
									href={etherscanTx(op.txHash)}
									target="_blank"
									rel="noopener noreferrer"
									class="text-indigo-400 hover:text-indigo-300"
									title={op.txHash}
								>
									{truncateHex(op.txHash)}
								</a>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<p class="mt-3 text-right text-xs text-zinc-500">
			Showing {feed.operations.length} operation{feed.operations.length === 1 ? '' : 's'}
		</p>
	{/if}
</div>
