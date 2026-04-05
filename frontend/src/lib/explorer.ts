import { formatEther } from 'viem';

const ETHERSCAN_BASE = 'https://sepolia.etherscan.io';

/** Returns a Sepolia Etherscan link for a transaction hash. */
export function etherscanTx(txHash: string): string {
	return `${ETHERSCAN_BASE}/tx/${txHash}`;
}

/** Returns a Sepolia Etherscan link for a block number. */
export function etherscanBlock(blockNumber: number): string {
	return `${ETHERSCAN_BASE}/block/${blockNumber}`;
}

/** Returns a Sepolia Etherscan link for an address. */
export function etherscanAddress(address: string): string {
	return `${ETHERSCAN_BASE}/address/${address}`;
}

/** Formats a wei string as a human-readable ETH value (up to 6 decimals). */
export function formatGasCost(wei: string): string {
	if (!wei || wei === '0') return '0 ETH';
	const eth = formatEther(BigInt(wei));
	// Trim trailing zeros but keep at least one decimal for clarity.
	const [whole, frac] = eth.split('.');
	if (!frac) return `${whole} ETH`;
	const trimmed = frac.slice(0, 6).replace(/0+$/, '');
	return trimmed ? `${whole}.${trimmed} ETH` : `${whole} ETH`;
}

/** Truncates a hex string: 0xabcd…ef12 */
export function truncateHex(hex: string, head = 6, tail = 4): string {
	if (hex.length <= head + tail + 1) return hex;
	return hex.slice(0, head) + '\u2026' + hex.slice(-tail);
}
