/**
 * Custom smart account adapters for the Bastion SmartAccount contract.
 *
 * Two variants:
 * - `toBastionSmartAccount` — owner flow, signs via WalletClient (MetaMask)
 * - `toSessionKeySmartAccount` — session key flow, signs via LocalAccount (private key)
 *
 * Both use the project's SmartAccount and SmartAccountFactory ABIs for call
 * encoding, factory initCode, and UserOp signing against EntryPoint v0.7.
 */

import type {
	Address,
	Chain,
	Client,
	Hex,
	LocalAccount,
	Transport,
	WalletClient,
	Account
} from 'viem';
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

// ── Shared internals ────────────────────────────────────────────────

const entryPoint = {
	address: entryPoint07Address,
	abi: entryPoint07Abi,
	version: '0.7' as const
};

type SharedParams = {
	client: Client<Transport, Chain | undefined>;
	factoryAddress: Address;
	ownerAddress: Address;
	salt: bigint;
	accountAddress: Address;
	signHash: (hash: Hex) => Promise<Hex>;
};

/** Build the SmartAccount adapter with a generic signing function. */
async function buildSmartAccount(params: SharedParams): Promise<SmartAccount> {
	const { client, factoryAddress, ownerAddress, salt, accountAddress, signHash } = params;

	let resolvedChainId: number | undefined;

	const getResolvedChainId = async (): Promise<number> => {
		if (resolvedChainId !== undefined) return resolvedChainId;
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

			return signHash(hash);
		}
	});
}

// ── Owner adapter (WalletClient) ────────────────────────────────────

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
 * Create a SmartAccount adapter for the owner flow.
 * Signs UserOperations via the owner's connected wallet (e.g. MetaMask).
 */
export async function toBastionSmartAccount(
	parameters: ToBastionSmartAccountParameters
): Promise<SmartAccount> {
	const { client, owner, factoryAddress, salt = 0n, accountAddress } = parameters;

	return buildSmartAccount({
		client,
		factoryAddress,
		ownerAddress: owner.account.address,
		salt,
		accountAddress,
		signHash: (hash) => owner.signMessage({ message: { raw: hash } })
	});
}

// ── Session key adapter (LocalAccount) ──────────────────────────────

export type ToSessionKeySmartAccountParameters = {
	/** Public client for on-chain reads and chain ID resolution. */
	client: Client<Transport, Chain | undefined>;
	/** Session key local account (signs UserOps with the private key). */
	sessionKey: LocalAccount;
	/** Deployed SmartAccountFactory address. */
	factoryAddress: Address;
	/** Owner address (needed for factory initCode, though account is already deployed). */
	ownerAddress: Address;
	/** Pre-computed SmartAccount address. */
	accountAddress: Address;
};

/**
 * Create a SmartAccount adapter for the session key flow.
 * Signs UserOperations locally with the session key's private key.
 */
export async function toSessionKeySmartAccount(
	parameters: ToSessionKeySmartAccountParameters
): Promise<SmartAccount> {
	const { client, sessionKey, factoryAddress, ownerAddress, accountAddress } = parameters;

	return buildSmartAccount({
		client,
		factoryAddress,
		ownerAddress,
		salt: 0n,
		accountAddress,
		signHash: (hash) => sessionKey.signMessage({ message: { raw: hash } })
	});
}
