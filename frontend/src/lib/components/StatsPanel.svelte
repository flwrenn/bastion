<script lang="ts">
	import type { IndexerStats } from '$lib/indexer.svelte';

	interface Props {
		stats: IndexerStats | null;
	}

	let { stats }: Props = $props();

	function pct(rate: number): string {
		return (rate * 100).toFixed(1) + '%';
	}
</script>

<div class="mb-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
	<!-- Total UserOps -->
	<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 px-4 py-4">
		<p class="text-xs tracking-wider text-zinc-400 uppercase">Total UserOps</p>
		<p class="mt-1 text-2xl font-semibold tabular-nums">
			{stats ? stats.totalOps.toLocaleString() : '\u2014'}
		</p>
	</div>

	<!-- Success Rate -->
	<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 px-4 py-4">
		<p class="text-xs tracking-wider text-zinc-400 uppercase">Success Rate</p>
		<p class="mt-1 text-2xl font-semibold tabular-nums">
			{#if stats}
				<span
					class={stats.successRate >= 0.9
						? 'text-green-400'
						: stats.successRate >= 0.5
							? 'text-yellow-400'
							: 'text-red-400'}
				>
					{pct(stats.successRate)}
				</span>
			{:else}
				{'\u2014'}
			{/if}
		</p>
		{#if stats && stats.totalOps > 0}
			<p class="mt-0.5 text-xs text-zinc-500">
				{stats.successCount} / {stats.totalOps}
			</p>
		{/if}
	</div>

	<!-- Sponsored -->
	<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 px-4 py-4">
		<p class="text-xs tracking-wider text-zinc-400 uppercase">Sponsored</p>
		<p class="mt-1 text-2xl font-semibold tabular-nums">
			{#if stats}
				<span class={stats.sponsoredRate > 0 ? 'text-emerald-400' : ''}>
					{pct(stats.sponsoredRate)}
				</span>
			{:else}
				{'\u2014'}
			{/if}
		</p>
		{#if stats && stats.totalOps > 0}
			<p class="mt-0.5 text-xs text-zinc-500">
				{stats.sponsoredCount} paymaster-funded
			</p>
		{/if}
	</div>

	<!-- Unique Senders -->
	<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 px-4 py-4">
		<p class="text-xs tracking-wider text-zinc-400 uppercase">Unique Senders</p>
		<p class="mt-1 text-2xl font-semibold tabular-nums">
			{stats ? stats.uniqueSenders.toLocaleString() : '\u2014'}
		</p>
	</div>
</div>
