<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { isAddress, type Hex } from 'viem';
	import { type LocalAccount, privateKeyToAccount } from 'viem/accounts';
	import { publicClient } from '$lib/wallet.svelte';
	import { SmartAccountAbi } from '$lib/contracts/SmartAccount';
	import { sendSessionKeyUserOp } from '$lib/userOp';
	import { etherscanTx, truncateHex } from '$lib/explorer';

	// ── Known selectors ─────────────────────────────────────────────────

	/** Selectors that take no arguments — safe to execute with just the 4-byte selector. */
	const NO_ARG_SELECTORS = new Set(['0xd09de08a', '0x4e71d92d']);

	const SELECTOR_LABELS: Record<string, string> = {
		'0xd09de08a': 'increment()',
		'0x4e71d92d': 'claim()',
		'0xa9059cbb': 'transfer(address,uint256)'
	};

	// ── Form state ──────────────────────────────────────────────────────

	let accountAddress = $state('');
	let privateKeyInput = $state('');

	// ── Session key data (loaded from contract) ─────────────────────────

	type SessionKeyInfo = {
		address: `0x${string}`;
		allowedTarget: `0x${string}`;
		allowedSelector: `0x${string}`;
		validAfter: number;
		validUntil: number;
	};

	let keyInfo = $state<SessionKeyInfo | null>(null);
	let ownerAddress = $state<`0x${string}` | null>(null);
	/** LocalAccount derived at load time — reused for signing to prevent mismatch. */
	let loadedAccount = $state<LocalAccount | null>(null);
	let loading = $state(false);
	let loadError = $state<string | null>(null);

	// ── Execution state ─────────────────────────────────────────────────

	let sending = $state(false);
	let sendError = $state<string | null>(null);
	let lastUserOpHash = $state<`0x${string}` | null>(null);
	let lastTxHash = $state<`0x${string}` | null>(null);

	// ── Live clock for expiry tracking ──────────────────────────────────

	let now = $state(Date.now() / 1000);
	let timer: ReturnType<typeof setInterval> | undefined;
	onMount(() => {
		timer = setInterval(() => (now = Date.now() / 1000), 1000);
	});
	onDestroy(() => clearInterval(timer));

	// ── Derived ─────────────────────────────────────────────────────────

	const isActive = $derived(
		keyInfo !== null &&
			keyInfo.validUntil > 0 &&
			now >= keyInfo.validAfter &&
			now < keyInfo.validUntil
	);

	const isPending = $derived(
		keyInfo !== null && keyInfo.validUntil > 0 && now < keyInfo.validAfter
	);

	const selectorLabel = $derived(
		keyInfo ? (SELECTOR_LABELS[keyInfo.allowedSelector] ?? keyInfo.allowedSelector) : ''
	);

	/** Whether the allowed function can be executed (no-arg selectors only). */
	const canExecute = $derived(keyInfo !== null && NO_ARG_SELECTORS.has(keyInfo.allowedSelector));

	// ── Load session key from contract ──────────────────────────────────

	async function loadKey() {
		if (!isAddress(accountAddress)) {
			loadError = 'Enter a valid SmartAccount address';
			return;
		}
		if (!privateKeyInput.startsWith('0x') || privateKeyInput.length !== 66) {
			loadError = 'Enter a valid private key (0x + 64 hex chars)';
			return;
		}

		loading = true;
		loadError = null;
		keyInfo = null;
		ownerAddress = null;
		loadedAccount = null;
		lastUserOpHash = null;
		lastTxHash = null;
		sendError = null;

		try {
			const account = privateKeyToAccount(privateKeyInput as `0x${string}`);

			const [skData, owner] = await Promise.all([
				publicClient.readContract({
					address: accountAddress as `0x${string}`,
					abi: SmartAccountAbi,
					functionName: 'sessionKeys',
					args: [account.address]
				}),
				publicClient.readContract({
					address: accountAddress as `0x${string}`,
					abi: SmartAccountAbi,
					functionName: 'owner'
				})
			]);

			const [allowedTarget, allowedSelector, validAfter, validUntil] = skData;

			if (validUntil === 0) {
				loadError = 'No session key registered for this address on the SmartAccount';
				return;
			}

			ownerAddress = owner;
			loadedAccount = account;
			keyInfo = {
				address: account.address,
				allowedTarget,
				allowedSelector,
				validAfter,
				validUntil
			};
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			loadError = err.shortMessage ?? err.message ?? 'Failed to load session key';
		} finally {
			loading = false;
		}
	}

	// ── Execute allowed action ──────────────────────────────────────────

	async function execute() {
		if (!keyInfo || !ownerAddress || !loadedAccount || !canExecute) return;

		sending = true;
		sendError = null;
		lastUserOpHash = null;
		lastTxHash = null;

		try {
			const result = await sendSessionKeyUserOp(
				loadedAccount,
				ownerAddress,
				accountAddress as `0x${string}`,
				{
					to: keyInfo.allowedTarget,
					value: 0n,
					data: keyInfo.allowedSelector as Hex
				}
			);

			lastUserOpHash = result.userOpHash;
			lastTxHash = result.txHash;

			if (!result.success) {
				sendError = 'UserOperation reverted on-chain';
			}
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			sendError = err.shortMessage ?? err.message ?? 'Execution failed';
		} finally {
			sending = false;
		}
	}

	function formatTime(unix: number): string {
		return new Date(unix * 1000).toLocaleString();
	}
</script>

<div class="mx-auto max-w-xl">
	<h1 class="mb-4 text-2xl font-bold">Session Key Demo</h1>
	<p class="mb-6 text-sm text-zinc-400">
		Use a session key to submit UserOperations on behalf of a SmartAccount — no owner wallet
		required.
	</p>

	<!-- Load form -->
	<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
		<h2 class="text-lg font-semibold">Connect Session Key</h2>

		<div class="mt-4 space-y-4">
			<div>
				<label for="sa-address" class="mb-1 block text-sm text-zinc-400">SmartAccount Address</label
				>
				<input
					id="sa-address"
					type="text"
					bind:value={accountAddress}
					placeholder="0x…"
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-2 font-mono text-sm text-zinc-100 placeholder-zinc-600 focus:border-indigo-500 focus:outline-none"
				/>
			</div>

			<div>
				<label for="sk-pk" class="mb-1 block text-sm text-zinc-400">Session Key Private Key</label>
				<input
					id="sk-pk"
					type="password"
					bind:value={privateKeyInput}
					placeholder="0x…"
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-2 font-mono text-sm text-zinc-100 placeholder-zinc-600 focus:border-indigo-500 focus:outline-none"
				/>
			</div>

			{#if loadError}
				<p class="text-sm text-red-400">{loadError}</p>
			{/if}

			<button
				type="button"
				onclick={loadKey}
				disabled={loading}
				class="w-full cursor-pointer rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{loading ? 'Loading…' : 'Load Session Key'}
			</button>
		</div>
	</div>

	<!-- Session key info + execute -->
	{#if keyInfo}
		<div class="mt-6 rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
			<h2 class="text-lg font-semibold">Session Key Permissions</h2>

			<dl class="mt-4 space-y-3">
				<div class="flex justify-between">
					<dt class="text-zinc-400">Key Address</dt>
					<dd class="font-mono text-sm">{truncateHex(keyInfo.address)}</dd>
				</div>
				<div class="flex justify-between">
					<dt class="text-zinc-400">Allowed Target</dt>
					<dd class="font-mono text-sm">{truncateHex(keyInfo.allowedTarget)}</dd>
				</div>
				<div class="flex justify-between">
					<dt class="text-zinc-400">Allowed Function</dt>
					<dd class="font-mono text-sm">{selectorLabel}</dd>
				</div>
				<div class="flex justify-between">
					<dt class="text-zinc-400">Valid From</dt>
					<dd class="text-sm">{formatTime(keyInfo.validAfter)}</dd>
				</div>
				<div class="flex justify-between">
					<dt class="text-zinc-400">Valid Until</dt>
					<dd class="text-sm">{formatTime(keyInfo.validUntil)}</dd>
				</div>
				<div class="flex justify-between">
					<dt class="text-zinc-400">Status</dt>
					<dd>
						{#if isActive}
							<span class="text-green-400">Active</span>
						{:else if isPending}
							<span class="text-yellow-400">Not yet active</span>
						{:else}
							<span class="text-red-400">Expired</span>
						{/if}
					</dd>
				</div>
			</dl>

			{#if lastUserOpHash || lastTxHash}
				<div class="mt-4 space-y-1 text-xs text-zinc-500">
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

			{#if sendError}
				<p class="mt-4 text-sm text-red-400">{sendError}</p>
			{/if}

			{#if !canExecute}
				<p class="mt-4 text-sm text-yellow-400">
					This function requires arguments — execution from this page is not supported.
				</p>
			{/if}

			<button
				type="button"
				onclick={execute}
				disabled={sending || !isActive || isPending || !canExecute}
				class="mt-4 w-full cursor-pointer rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{#if sending}
					Sending UserOp…
				{:else if isPending}
					Not Yet Active
				{:else if !isActive}
					Key Expired
				{:else if !canExecute}
					Requires Arguments
				{:else}
					Execute {selectorLabel}
				{/if}
			</button>
		</div>
	{/if}
</div>
