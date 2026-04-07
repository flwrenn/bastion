/**
 * Centralised environment configuration for contract addresses and Pimlico.
 *
 * All helpers read from SvelteKit's $env/dynamic/public at call time so they
 * pick up runtime values (e.g. from .env or the hosting platform).
 */

import { env } from '$env/dynamic/public';
import { isAddress } from 'viem';

function requireAddress(name: string): `0x${string}` {
	const raw = env[name];
	if (!raw) throw new Error(`${name} is not set`);
	if (!isAddress(raw)) throw new Error(`${name} is not a valid address: ${raw}`);
	return raw;
}

function requireString(name: string): string {
	const val = env[name];
	if (!val) throw new Error(`${name} is not set`);
	return val;
}

/** SmartAccountFactory deployment address. */
export function factoryAddress(): `0x${string}` {
	return requireAddress('PUBLIC_FACTORY_ADDRESS');
}

/** Counter contract address. */
export function counterAddress(): `0x${string}` {
	return requireAddress('PUBLIC_COUNTER_ADDRESS');
}

/** FaucetToken (BFT) contract address. */
export function faucetTokenAddress(): `0x${string}` {
	return requireAddress('PUBLIC_FAUCET_TOKEN_ADDRESS');
}

/** Pimlico bundler/paymaster RPC URL for Sepolia. */
export function pimlicoUrl(): string {
	const key = requireString('PUBLIC_PIMLICO_API_KEY');
	return `https://api.pimlico.io/v2/sepolia/rpc?apikey=${encodeURIComponent(key)}`;
}
