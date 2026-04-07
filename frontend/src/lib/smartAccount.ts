/**
 * Custom smart account adapter for the Bastion SmartAccount contract.
 *
 * Replaces permissionless.js's generic `toSimpleSmartAccount` with an adapter
 * built on viem's `toSmartAccount` that uses the project's own SmartAccount and
 * SmartAccountFactory ABIs for call encoding, factory initCode, and UserOp signing.
 */

import type { Address, Chain, Client, Transport, WalletClient, Account } from 'viem';
import { encodeFunctionData } from 'viem';
import {
	type SmartAccount,
	type UserOperation,
	entryPoint07Abi,
	entryPoint07Address,
	getUserOperationHash,
	toSmartAccount
} from 'viem/account-abstraction';
import { getChainId } from 'viem/actions';
import { SmartAccountAbi } from '$lib/contracts/SmartAccount';
import { SmartAccountFactoryAbi } from '$lib/contracts/SmartAccountFactory';

export type ToBastionSmartAccountParameters = {
	/** Public client for on-chain reads and chain ID resolution. */
	client: Client<Transport, Chain | undefined>;
	/** Owner wallet client — signs UserOps via the connected wallet (e.g. MetaMask). */
	owner: WalletClient<Transport, Chain | undefined, Account>;
	/** Deployed SmartAccountFactory address. */
	factoryAddress: Address;
	/** CREATE2 salt (default 0). */
	salt?: bigint;
	/** Pre-computed counterfactual address (from factory.getAddress). */
	accountAddress: Address;
};

/**
 * Create a viem `SmartAccount` adapter for the Bastion SmartAccount contract.
 *
 * - Encodes `execute` / `executeBatch` calls using the SmartAccount ABI.
 * - Generates factory initCode using the SmartAccountFactory ABI.
 * - Signs UserOperations with ECDSA via the owner's wallet.
 * - Targets EntryPoint v0.7.
 */
export async function toBastionSmartAccount(
	parameters: ToBastionSmartAccountParameters
): Promise<SmartAccount> {
	const { client, owner, factoryAddress, salt = 0n, accountAddress } = parameters;

	const ownerAddress = owner.account.address;

	const entryPoint = {
		address: entryPoint07Address,
		abi: entryPoint07Abi,
		version: '0.7' as const
	};

	// Memoised chain ID — resolved once, reused for all subsequent UserOp signatures.
	let resolvedChainId: number | undefined;

	const getResolvedChainId = async (): Promise<number> => {
		if (resolvedChainId) return resolvedChainId;
		resolvedChainId = client.chain?.id ?? (await getChainId(client));
		return resolvedChainId;
	};

	return toSmartAccount({
		client,
		entryPoint,

		async getAddress() {
			return accountAddress;
		},

		async getFactoryArgs() {
			return {
				factory: factoryAddress,
				factoryData: encodeFunctionData({
					abi: SmartAccountFactoryAbi,
					functionName: 'createAccount',
					args: [ownerAddress, salt]
				})
			};
		},

		async encodeCalls(calls) {
			if (calls.length > 1) {
				return encodeFunctionData({
					abi: SmartAccountAbi,
					functionName: 'executeBatch',
					args: [
						calls.map((c) => c.to),
						calls.map((c) => c.value ?? 0n),
						calls.map((c) => c.data ?? '0x')
					]
				});
			}

			const call = calls[0];
			if (!call) throw new Error('No calls to encode');

			return encodeFunctionData({
				abi: SmartAccountAbi,
				functionName: 'execute',
				args: [call.to, call.value ?? 0n, call.data ?? '0x']
			});
		},

		async getStubSignature() {
			// 65-byte dummy ECDSA signature used for gas estimation.
			return '0xfffffffffffffffffffffffffffffff0000000000000000000000000000000007aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1c';
		},

		async signMessage(_) {
			throw new Error('Bastion SmartAccount is not ERC-1271 compliant');
		},

		async signTypedData(_) {
			throw new Error('Bastion SmartAccount is not ERC-1271 compliant');
		},

		async signUserOperation(parameters) {
			const { chainId: chainIdOverride, ...userOperation } = parameters;
			const chainId = chainIdOverride ?? (await getResolvedChainId());

			const hash = getUserOperationHash({
				userOperation: {
					...userOperation,
					sender: userOperation.sender ?? accountAddress,
					signature: '0x'
				} as UserOperation<'0.7'>,
				entryPointAddress: entryPoint.address,
				entryPointVersion: '0.7',
				chainId
			});

			return owner.signMessage({
				message: { raw: hash }
			});
		}
	});
}
