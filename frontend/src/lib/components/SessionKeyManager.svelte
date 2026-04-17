<script lang="ts">
	import { encodeFunctionData, isAddress, toFunctionSelector } from 'viem';
	import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
	import { wallet } from '$lib/wallet.svelte';
	import { SmartAccountAbi } from '$lib/contracts/SmartAccount';
	import { counterAddress, faucetTokenAddress } from '$lib/config';
	import { sendRawUserOp } from '$lib/userOp';
	import { truncateHex } from '$lib/explorer';

	let { accountAddress }: { accountAddress: `0x${string}` } = $props();

	// ── Targets & selectors ─────────────────────────────────────────────

	const TARGETS = [
		{
			id: 'counter',
			label: 'Counter',
			address: () => counterAddress(),
			functions: [{ label: 'increment()', selector: toFunctionSelector('function increment()') }]
		},
		{
			id: 'faucet',
			label: 'FaucetToken',
			address: () => faucetTokenAddress(),
			functions: [
				{ label: 'claim()', selector: toFunctionSelector('function claim()') },
				{
					label: 'transfer(address,uint256)',
					selector: toFunctionSelector('function transfer(address,uint256)')
				}
			]
		}
	];

	// ── Registration form state ─────────────────────────────────────────

	let keyAddress = $state('');
	let generatedPrivateKey = $state<string | null>(null);
	/** The address that was generated — used to detect manual edits. */
	let generatedAddress = $state<string | null>(null);
	let selectedTargetId = $state(TARGETS[0].id);
	let selectedFnIndex = $state(0);
	let validMinutes = $state(60);
	let registering = $state(false);
	let registerError = $state<string | null>(null);

	// ── Registered keys ─────────────────────────────────────────────────
	// NOTE: Keys are tracked in-memory only (the contract's sessionKeys mapping
	// is not enumerable). Refreshing the page will clear this list.

	type SessionKeyEntry = {
		address: `0x${string}`;
		privateKey: string | null;
		targetName: string;
		functionName: string;
		validAfter: number;
		validUntil: number;
		revoked: boolean;
		revoking: boolean;
	};

	let keys = $state<SessionKeyEntry[]>([]);
	let revokeError = $state<string | null>(null);

	// ── Derived ─────────────────────────────────────────────────────────

	const selectedTarget = $derived(TARGETS.find((t) => t.id === selectedTargetId) ?? TARGETS[0]);
	const selectedFn = $derived(
		selectedTarget.functions[selectedFnIndex] ?? selectedTarget.functions[0]
	);

	// Reset function index when target changes.
	$effect(() => {
		selectedTargetId;
		selectedFnIndex = 0;
	});

	// Clear generated private key if the user manually edits the address.
	$effect(() => {
		if (generatedAddress && keyAddress !== generatedAddress) {
			generatedPrivateKey = null;
			generatedAddress = null;
		}
	});

	// ── Actions ─────────────────────────────────────────────────────────

	function generateKey() {
		const pk = generatePrivateKey();
		const account = privateKeyToAccount(pk);
		keyAddress = account.address;
		generatedAddress = account.address;
		generatedPrivateKey = pk;
	}

	async function register() {
		const walletClient = wallet.client;
		if (!walletClient) {
			registerError = 'Wallet not connected';
			return;
		}
		if (!isAddress(keyAddress)) {
			registerError = 'Enter a valid Ethereum address or generate a key';
			return;
		}
		if (!Number.isFinite(validMinutes) || validMinutes < 1) {
			registerError = 'Duration must be at least 1 minute';
			return;
		}

		registering = true;
		registerError = null;

		const now = Math.floor(Date.now() / 1000);
		const validAfter = now;
		const validUntil = now + Math.floor(validMinutes) * 60;

		try {
			const callData = encodeFunctionData({
				abi: SmartAccountAbi,
				functionName: 'registerSessionKey',
				args: [
					keyAddress as `0x${string}`,
					selectedTarget.address(),
					selectedFn.selector as `0x${string}`,
					validAfter,
					validUntil
				]
			});

			const result = await sendRawUserOp(walletClient, accountAddress, callData);

			if (!result.success) {
				registerError = 'UserOperation reverted on-chain';
				return;
			}

			keys = [
				...keys,
				{
					address: keyAddress as `0x${string}`,
					privateKey: generatedPrivateKey,
					targetName: selectedTarget.label,
					functionName: selectedFn.label,
					validAfter,
					validUntil,
					revoked: false,
					revoking: false
				}
			];

			// Reset form.
			keyAddress = '';
			generatedPrivateKey = null;
			generatedAddress = null;
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			registerError = err.shortMessage ?? err.message ?? 'Registration failed';
		} finally {
			registering = false;
		}
	}

	async function revoke(index: number) {
		const walletClient = wallet.client;
		if (!walletClient) return;

		const entry = keys[index];
		if (!entry || entry.revoked) return;

		keys[index].revoking = true;
		revokeError = null;

		try {
			const callData = encodeFunctionData({
				abi: SmartAccountAbi,
				functionName: 'revokeSessionKey',
				args: [entry.address]
			});

			const result = await sendRawUserOp(walletClient, accountAddress, callData);

			if (!result.success) {
				revokeError = `Failed to revoke ${truncateHex(entry.address)}`;
				keys[index].revoking = false;
				return;
			}

			keys[index].revoked = true;
			keys[index].revoking = false;
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			revokeError = err.shortMessage ?? err.message ?? 'Revoke failed';
			keys[index].revoking = false;
		}
	}

	function formatTime(unix: number): string {
		return new Date(unix * 1000).toLocaleString();
	}

	function keyStatus(entry: SessionKeyEntry): { label: string; color: string } {
		if (entry.revoked) return { label: 'Revoked', color: 'text-zinc-400' };
		if (Date.now() / 1000 > entry.validUntil) return { label: 'Expired', color: 'text-red-400' };
		return { label: 'Active', color: 'text-green-400' };
	}

	async function copyToClipboard(text: string) {
		try {
			await navigator.clipboard.writeText(text);
		} catch {
			// Fallback: select a temporary textarea (insecure context).
			const el = document.createElement('textarea');
			el.value = text;
			el.style.position = 'fixed';
			el.style.opacity = '0';
			document.body.appendChild(el);
			el.select();
			document.execCommand('copy');
			document.body.removeChild(el);
		}
	}
</script>

<div class="space-y-6">
	<!-- Register form -->
	<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
		<h3 class="text-lg font-semibold">Register Session Key</h3>

		<div class="mt-4 space-y-4">
			<!-- Key address -->
			<div>
				<label for="sk-address" class="mb-1 block text-sm text-zinc-400">Key Address</label>
				<div class="flex gap-2">
					<input
						id="sk-address"
						type="text"
						bind:value={keyAddress}
						placeholder="0x…"
						class="flex-1 rounded border border-zinc-700 bg-zinc-900 px-3 py-2 font-mono text-sm text-zinc-100 placeholder-zinc-600 focus:border-indigo-500 focus:outline-none"
					/>
					<button
						type="button"
						onclick={generateKey}
						class="cursor-pointer rounded bg-zinc-700 px-3 py-2 text-sm whitespace-nowrap text-zinc-200 hover:bg-zinc-600"
					>
						Generate
					</button>
				</div>
				{#if generatedPrivateKey}
					<div class="mt-2 rounded border border-yellow-800/50 bg-yellow-900/20 p-2">
						<p class="text-xs text-yellow-400">Private key (save for session key demo):</p>
						<div class="mt-1 flex items-center gap-2">
							<code class="flex-1 font-mono text-xs break-all text-yellow-300">
								{generatedPrivateKey}
							</code>
							<button
								type="button"
								onclick={() => copyToClipboard(generatedPrivateKey!)}
								class="cursor-pointer rounded bg-zinc-700 px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-600"
							>
								Copy
							</button>
						</div>
					</div>
				{/if}
			</div>

			<!-- Target contract -->
			<div>
				<label for="sk-target" class="mb-1 block text-sm text-zinc-400">Target Contract</label>
				<select
					id="sk-target"
					bind:value={selectedTargetId}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 focus:border-indigo-500 focus:outline-none"
				>
					{#each TARGETS as target}
						<option value={target.id}>{target.label}</option>
					{/each}
				</select>
			</div>

			<!-- Allowed function -->
			<div>
				<label for="sk-fn" class="mb-1 block text-sm text-zinc-400">Allowed Function</label>
				<select
					id="sk-fn"
					bind:value={selectedFnIndex}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-2 font-mono text-sm text-zinc-100 focus:border-indigo-500 focus:outline-none"
				>
					{#each selectedTarget.functions as fn, i}
						<option value={i}>{fn.label}</option>
					{/each}
				</select>
			</div>

			<!-- Validity duration -->
			<div>
				<label for="sk-duration" class="mb-1 block text-sm text-zinc-400">Valid for (minutes)</label
				>
				<input
					id="sk-duration"
					type="number"
					min="1"
					max="10080"
					bind:value={validMinutes}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 focus:border-indigo-500 focus:outline-none"
				/>
			</div>

			{#if registerError}
				<p class="text-sm text-red-400">{registerError}</p>
			{/if}

			<button
				type="button"
				onclick={register}
				disabled={registering}
				class="w-full cursor-pointer rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{registering ? 'Sending UserOp…' : 'Register Key'}
			</button>
		</div>
	</div>

	<!-- Registered keys list -->
	{#if keys.length > 0}
		<div class="rounded-lg border border-zinc-800 bg-zinc-800/50 p-6">
			<h3 class="text-lg font-semibold">Registered Keys</h3>
			<p class="mt-1 text-xs text-zinc-500">
				Keys shown are from this session only. Refreshing the page will clear this list.
			</p>

			<div class="mt-4 space-y-3">
				{#each keys as entry, i}
					{@const status = keyStatus(entry)}
					<div
						class="rounded border p-3"
						class:border-zinc-700={!entry.revoked}
						class:border-zinc-800={entry.revoked}
						class:opacity-60={entry.revoked}
					>
						<div class="flex items-start justify-between gap-2">
							<div class="min-w-0 flex-1">
								<p class="font-mono text-sm text-zinc-200">
									{truncateHex(entry.address)}
								</p>
								<p class="mt-1 text-xs text-zinc-400">
									{entry.targetName} · <code>{entry.functionName}</code>
								</p>
								<p class="mt-1 text-xs text-zinc-500">
									{formatTime(entry.validAfter)} → {formatTime(entry.validUntil)}
									<span class="ml-1 {status.color}">{status.label}</span>
								</p>
								{#if entry.privateKey && !entry.revoked}
									<button
										type="button"
										onclick={() => copyToClipboard(entry.privateKey!)}
										class="mt-1 cursor-pointer text-xs text-indigo-400 hover:text-indigo-300"
									>
										Copy private key
									</button>
								{/if}
							</div>
							{#if !entry.revoked}
								<button
									type="button"
									onclick={() => revoke(i)}
									disabled={entry.revoking}
									class="cursor-pointer rounded bg-red-900/60 px-2.5 py-1 text-xs font-medium text-red-300 hover:bg-red-900/80 disabled:cursor-not-allowed disabled:opacity-50"
								>
									{entry.revoking ? 'Revoking…' : 'Revoke'}
								</button>
							{/if}
						</div>
					</div>
				{/each}
			</div>

			{#if revokeError}
				<p class="mt-3 text-sm text-red-400">{revokeError}</p>
			{/if}
		</div>
	{/if}
</div>
