/**
 * Shared utility for submitting UserOperations through the Bastion SmartAccount.
 *
 * Creates a fresh SmartAccount adapter + Pimlico bundler client per call,
 * sends the UserOp, and waits for the on-chain receipt.
 */

import type { Account, Address, Chain, Hex, LocalAccount, Transport, WalletClient } from 'viem';
import { http } from 'viem';
import { sepolia } from 'viem/chains';
import { entryPoint07Address, type SmartAccount } from 'viem/account-abstraction';
import { createSmartAccountClient } from 'permissionless';
import { createPimlicoClient } from 'permissionless/clients/pimlico';
import { publicClient } from '$lib/wallet.svelte';
import { toBastionSmartAccount, toSessionKeySmartAccount } from '$lib/smartAccount';
import { factoryAddress, pimlicoUrl } from '$lib/config';

export type UserOpCall = {
	to: `0x${string}`;
	value?: bigint;
	data?: `0x${string}`;
};

export type UserOpResult = {
	userOpHash: `0x${string}`;
	txHash: `0x${string}`;
	success: boolean;
};

/** Create a SmartAccountClient (bundler + paymaster) for a given SmartAccount adapter.
 *
 * Uses Pimlico's `pimlico_*` paymaster methods (free-tier compatible) via
 * `createPimlicoClient`, NOT the ERC-7677 `pm_*` methods that viem's
 * `createPaymasterClient` would call (those are paid-tier on Pimlico).
 */
export async function createBundlerClientFor(account: SmartAccount) {
	const pimlico = pimlicoUrl();

	const pimlicoClient = createPimlicoClient({
		transport: http(pimlico),
		entryPoint: { address: entryPoint07Address, version: '0.7' }
	});

	return createSmartAccountClient({
		account,
		paymaster: pimlicoClient,
		chain: sepolia,
		bundlerTransport: http(pimlico),
		userOperation: {
			estimateFeesPerGas: async () => (await pimlicoClient.getUserOperationGasPrice()).fast
		}
	});
}

/** Create a SmartAccountClient for the owner flow (signs via WalletClient). */
async function createOwnerBundlerClient(
	owner: WalletClient<Transport, Chain, Account>,
	accountAddress: Address
) {
	const smartAccount = await toBastionSmartAccount({
		client: publicClient,
		owner,
		factoryAddress: factoryAddress(),
		accountAddress
	});

	return createBundlerClientFor(smartAccount);
}

/**
 * Send a UserOperation with one or more calls via the owner's SmartAccount.
 * Calls are encoded via `execute` / `executeBatch`.
 *
 * @param owner           Connected wallet client (signs the UserOp).
 * @param accountAddress  Deployed SmartAccount address.
 * @param calls           One or more calls to execute.
 * @returns               The UserOp hash, on-chain tx hash, and whether it succeeded.
 */
export async function sendUserOp(
	owner: WalletClient<Transport, Chain, Account>,
	accountAddress: `0x${string}`,
	calls: UserOpCall[]
): Promise<UserOpResult> {
	const bundlerClient = await createOwnerBundlerClient(owner, accountAddress);

	const hash = await bundlerClient.sendUserOperation({ calls });
	const receipt = await bundlerClient.waitForUserOperationReceipt({ hash });

	return {
		userOpHash: hash,
		txHash: receipt.receipt.transactionHash,
		success: receipt.success
	};
}

/**
 * Send a UserOperation with raw callData (bypasses `execute` encoding).
 * Use this for SmartAccount self-calls like `registerSessionKey` / `revokeSessionKey`
 * where the EntryPoint must call the function directly.
 *
 * @param owner           Connected wallet client (signs the UserOp).
 * @param accountAddress  Deployed SmartAccount address.
 * @param callData        Pre-encoded callData for the UserOp.
 * @returns               The UserOp hash, on-chain tx hash, and whether it succeeded.
 */
export async function sendRawUserOp(
	owner: WalletClient<Transport, Chain, Account>,
	accountAddress: Address,
	callData: Hex
): Promise<UserOpResult> {
	const bundlerClient = await createOwnerBundlerClient(owner, accountAddress);

	const hash = await bundlerClient.sendUserOperation({ callData });
	const receipt = await bundlerClient.waitForUserOperationReceipt({ hash });

	return {
		userOpHash: hash,
		txHash: receipt.receipt.transactionHash,
		success: receipt.success
	};
}

/**
 * Send a UserOperation signed by a session key (LocalAccount).
 * Calls are encoded via `execute` — session key validation requires this encoding.
 *
 * @param sessionKey      Local account holding the session key private key.
 * @param ownerAddress    Owner of the SmartAccount (needed for factory args).
 * @param accountAddress  Deployed SmartAccount address.
 * @param call            Single call to execute (session key validation requires `execute`, not `executeBatch`).
 * @returns               The UserOp hash, on-chain tx hash, and whether it succeeded.
 */
export async function sendSessionKeyUserOp(
	sessionKey: LocalAccount,
	ownerAddress: Address,
	accountAddress: Address,
	call: UserOpCall
): Promise<UserOpResult> {
	const smartAccount = await toSessionKeySmartAccount({
		client: publicClient,
		sessionKey,
		factoryAddress: factoryAddress(),
		ownerAddress,
		accountAddress
	});

	const bundlerClient = await createBundlerClientFor(smartAccount);

	const hash = await bundlerClient.sendUserOperation({ calls: [call] });
	const receipt = await bundlerClient.waitForUserOperationReceipt({ hash });

	return {
		userOpHash: hash,
		txHash: receipt.receipt.transactionHash,
		success: receipt.success
	};
}
