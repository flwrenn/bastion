<script lang="ts">
	import { encodeFunctionData, toFunctionSelector } from 'viem';
	import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
	import { publicClient, wallet } from '$lib/wallet.svelte';
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
	let selectedTargetId = $state(TARGETS[0].id);
	let selectedFnIndex = $state(0);
	let validMinutes = $state(60);
	let registering = $state(false);
	let registerError = $state<string | null>(null);

	// ── Registered keys ─────────────────────────────────────────────────

	type SessionKeyEntry = {
		address: `0x${string}`;
		privateKey: string | null;
		targetName: string;
		functionName: string;
		validAfter: number;
		validUntil: number;
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

	// ── Actions ─────────────────────────────────────────────────────────

	function generateKey() {
		const pk = generatePrivateKey();
		const account = privateKeyToAccount(pk);
		keyAddress = account.address;
		generatedPrivateKey = pk;
	}

	async function register() {
		const walletClient = wallet.client;
		if (!walletClient) {
			registerError = 'Wallet not connected';
			return;
		}
		if (!keyAddress || !keyAddress.startsWith('0x') || keyAddress.length !== 42) {
			registerError = 'Enter a valid address or generate a key';
			return;
		}

		registering = true;
		registerError = null;

		const now = Math.floor(Date.now() / 1000);
		const validAfter = now;
		const validUntil = now + validMinutes * 60;

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
					revoking: false
				}
			];

			// Reset form.
			keyAddress = '';
			generatedPrivateKey = null;
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
		if (!entry) return;

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

			keys = keys.filter((_, i) => i !== index);
		} catch (e: unknown) {
			const err = e as { shortMessage?: string; message?: string };
			revokeError = err.shortMessage ?? err.message ?? 'Revoke failed';
			keys[index].revoking = false;
		}
	}

	function formatTime(unix: number): string {
		return new Date(unix * 1000).toLocaleString();
	}

	function isExpired(entry: SessionKeyEntry): boolean {
		return Date.now() / 1000 > entry.validUntil;
	}

	function copyToClipboard(text: string) {
		navigator.clipboard.writeText(text);
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

			<div class="mt-4 space-y-3">
				{#each keys as entry, i}
					<div class="rounded border border-zinc-700 p-3">
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
									{#if isExpired(entry)}
										<span class="ml-1 text-red-400">Expired</span>
									{:else}
										<span class="ml-1 text-green-400">Active</span>
									{/if}
								</p>
								{#if entry.privateKey}
									<button
										type="button"
										onclick={() => copyToClipboard(entry.privateKey!)}
										class="mt-1 cursor-pointer text-xs text-indigo-400 hover:text-indigo-300"
									>
										Copy private key
									</button>
								{/if}
							</div>
							<button
								type="button"
								onclick={() => revoke(i)}
								disabled={entry.revoking}
								class="cursor-pointer rounded bg-red-900/60 px-2.5 py-1 text-xs font-medium text-red-300 hover:bg-red-900/80 disabled:cursor-not-allowed disabled:opacity-50"
							>
								{entry.revoking ? 'Revoking…' : 'Revoke'}
							</button>
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
