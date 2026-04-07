import { env } from '$env/dynamic/public';
import { http, isAddress } from 'viem';
import { sepolia } from 'viem/chains';
import { createPaymasterClient } from 'viem/account-abstraction';
import { createSmartAccountClient } from 'permissionless';
import { publicClient, wallet } from '$lib/wallet.svelte';
import { SmartAccountFactoryAbi } from '$lib/contracts/SmartAccountFactory';
import { toBastionSmartAccount } from '$lib/smartAccount';

/** Resolve factory address lazily so $env/dynamic/public is read at call time. */
function factoryAddress(): `0x${string}` {
	const addr = env.PUBLIC_FACTORY_ADDRESS;
	if (!addr) throw new Error('PUBLIC_FACTORY_ADDRESS is not set');
	if (!isAddress(addr)) throw new Error(`PUBLIC_FACTORY_ADDRESS is not a valid address: ${addr}`);
	return addr;
}

/** Resolve Pimlico API key lazily. */
function pimlicoApiKey(): string {
	const key = env.PUBLIC_PIMLICO_API_KEY;
	if (!key) throw new Error('PUBLIC_PIMLICO_API_KEY is not set');
	return key;
}

/** Build the Pimlico RPC URL for Sepolia. */
function pimlicoUrl(): string {
	return `https://api.pimlico.io/v2/sepolia/rpc?apikey=${encodeURIComponent(pimlicoApiKey())}`;
}

class AccountState {
	smartAccountAddress = $state<`0x${string}` | null>(null);
	deployed = $state<boolean>(false);
	deploying = $state<boolean>(false);
	deployUserOpHash = $state<`0x${string}` | null>(null);
	error = $state<string | null>(null);

	/** Monotonically increasing ID to discard results from stale load() calls. */
	private loadId = 0;
	/** Monotonically increasing ID to discard results from stale deploy() calls. */
	private deployId = 0;

	/** Compute the counterfactual address and check if already deployed. */
	async load(ownerAddress: `0x${string}`) {
		const id = ++this.loadId;

		this.error = null;
		this.deploying = false;
		this.deployUserOpHash = null;
		this.smartAccountAddress = null;
		this.deployed = false;

		try {
			const address = await publicClient.readContract({
				address: factoryAddress(),
				abi: SmartAccountFactoryAbi,
				functionName: 'getAddress',
				args: [ownerAddress, 0n]
			});

			if (id !== this.loadId) return;

			this.smartAccountAddress = address;

			const code = await publicClient.getCode({ address });

			if (id !== this.loadId) return;

			this.deployed = !!code && code !== '0x';
		} catch (e: unknown) {
			if (id !== this.loadId) return;
			const err = e as { shortMessage?: string; message?: string };
			this.error = err.shortMessage ?? err.message ?? 'Failed to load smart account';
		}
	}

	/** Deploy the smart account by sending a no-op UserOp (first UserOp auto-includes initCode). */
	async deploy() {
		if (this.deploying) return;

		const walletClient = wallet.client;

		if (!walletClient || !wallet.address) {
			this.error = 'Wallet not connected';
			return;
		}

		if (this.deployed) {
			this.error = 'Account already deployed';
			return;
		}

		const accountAddr = this.smartAccountAddress;
		if (!accountAddr) {
			this.error = 'Load account address first';
			return;
		}

		const id = ++this.deployId;
		this.deploying = true;
		this.error = null;
		this.deployUserOpHash = null;

		try {
			const smartAccount = await toBastionSmartAccount({
				client: publicClient,
				owner: walletClient,
				factoryAddress: factoryAddress(),
				salt: 0n,
				accountAddress: accountAddr
			});

			if (id !== this.deployId) return;

			const paymaster = createPaymasterClient({
				transport: http(pimlicoUrl())
			});

			const bundlerClient = createSmartAccountClient({
				account: smartAccount,
				paymaster,
				chain: sepolia,
				bundlerTransport: http(pimlicoUrl())
			});

			// Send a no-op call to self — the first UserOp auto-deploys via initCode.
			const hash = await bundlerClient.sendUserOperation({
				calls: [{ to: accountAddr, value: 0n, data: '0x' }]
			});

			if (id !== this.deployId) return;

			this.deployUserOpHash = hash;

			// Wait for the UserOp to be included on-chain.
			const receipt = await bundlerClient.waitForUserOperationReceipt({ hash });

			if (id !== this.deployId) return;

			if (!receipt.success) {
				this.error = 'UserOperation reverted on-chain';
				this.deploying = false;
				return;
			}

			// Verify deployment by checking bytecode.
			const code = await publicClient.getCode({ address: accountAddr });

			if (id !== this.deployId) return;

			this.deployed = !!code && code !== '0x';

			if (!this.deployed) {
				this.error = 'Deployment transaction succeeded but no bytecode found';
			}
		} catch (e: unknown) {
			if (id !== this.deployId) return;
			const err = e as { shortMessage?: string; message?: string };
			this.error = err.shortMessage ?? err.message ?? 'Deployment failed';
		} finally {
			if (id === this.deployId) this.deploying = false;
		}
	}

	/** Clear all state (called on wallet disconnect). */
	reset() {
		++this.loadId;
		++this.deployId;
		this.smartAccountAddress = null;
		this.deployed = false;
		this.deploying = false;
		this.deployUserOpHash = null;
		this.error = null;
	}
}

export const account = new AccountState();
