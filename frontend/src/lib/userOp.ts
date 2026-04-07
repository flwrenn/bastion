/**
 * Shared utility for submitting UserOperations through the Bastion SmartAccount.
 *
 * Creates a fresh SmartAccount adapter + Pimlico bundler client per call,
 * sends the UserOp, and waits for the on-chain receipt.
 */

import type { Account, Chain, Transport, WalletClient } from 'viem';
import { http } from 'viem';
import { sepolia } from 'viem/chains';
import { createPaymasterClient } from 'viem/account-abstraction';
import { createSmartAccountClient } from 'permissionless';
import { publicClient } from '$lib/wallet.svelte';
import { toBastionSmartAccount } from '$lib/smartAccount';
import { factoryAddress, pimlicoUrl } from '$lib/config';

export type UserOpCall = {
	to: `0x${string}`;
	value?: bigint;
	data?: `0x${string}`;
};

export type UserOpResult = {
	userOpHash: `0x${string}`;
	success: boolean;
};

/**
 * Send a UserOperation with one or more calls via the owner's SmartAccount.
 *
 * @param owner       Connected wallet client (signs the UserOp).
 * @param accountAddress  Deployed SmartAccount address.
 * @param calls       One or more calls to execute.
 * @returns           The UserOp hash and whether it succeeded on-chain.
 */
export async function sendUserOp(
	owner: WalletClient<Transport, Chain, Account>,
	accountAddress: `0x${string}`,
	calls: UserOpCall[]
): Promise<UserOpResult> {
	const smartAccount = await toBastionSmartAccount({
		client: publicClient,
		owner,
		factoryAddress: factoryAddress(),
		accountAddress
	});

	const pimlico = pimlicoUrl();

	const paymaster = createPaymasterClient({
		transport: http(pimlico)
	});

	const bundlerClient = createSmartAccountClient({
		account: smartAccount,
		paymaster,
		chain: sepolia,
		bundlerTransport: http(pimlico)
	});

	const hash = await bundlerClient.sendUserOperation({ calls });
	const receipt = await bundlerClient.waitForUserOperationReceipt({ hash });

	return { userOpHash: hash, success: receipt.success };
}
