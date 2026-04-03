import { env } from '$env/dynamic/public';
import { formatEther } from 'viem';

/** Resolve the indexer base URL lazily so $env/dynamic/public is read at call time. */
function indexerUrl(): string {
	return env.PUBLIC_INDEXER_URL ?? 'http://localhost:3001';
}

const POLL_INTERVAL = 5_000;
const RECONNECT_DELAY = 3_000;
const MAX_RECONNECT_DELAY = 30_000;
const MAX_OPERATIONS = 200;
const MAX_POLL_FAILURES = 3;

/** Shape returned by the indexer REST API and WebSocket feed. */
export interface UserOperation {
	userOpHash: string;
	sender: string;
	paymaster: string;
	target?: string;
	calldata?: string;
	nonce: string;
	success: boolean;
	actualGasCost: string;
	actualGasUsed: string;
	txHash: string;
	blockNumber: number;
	blockTimestamp: number;
	logIndex: number;
}

export type FeedStatus = 'disconnected' | 'connecting' | 'connected' | 'polling';

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

/**
 * Reactive indexer feed that connects to the WebSocket live feed and falls
 * back to REST API polling when the WebSocket is unavailable.
 *
 * Usage (in a Svelte component):
 *   const feed = new IndexerFeed();
 *   $effect(() => { feed.connect(); return () => feed.disconnect(); });
 */
class IndexerFeed {
	operations = $state<UserOperation[]>([]);
	status = $state<FeedStatus>('disconnected');

	private ws: WebSocket | null = null;
	private pollTimer: ReturnType<typeof setInterval> | null = null;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	private seenHashes = new Set<string>();
	private abort: AbortController | null = null;
	private pollFailures = 0;
	private reconnectDelay = RECONNECT_DELAY;

	/** Open the WebSocket connection and load initial data from REST. */
	connect() {
		this.disconnect(); // Idempotent — tear down any previous session first.
		this.abort = new AbortController();
		this.reconnectDelay = RECONNECT_DELAY;
		this.status = 'connecting';
		this.loadInitial();
		this.openWS();
	}

	/** Tear down all connections and timers. */
	disconnect() {
		this.stopReconnect();
		this.stopPolling();
		this.abort?.abort();
		this.abort = null;
		if (this.ws) {
			this.ws.onclose = null; // Prevent reconnect on intentional close.
			this.ws.close();
			this.ws = null;
		}
		this.pollFailures = 0;
		this.status = 'disconnected';
	}

	// --- WebSocket ---

	private openWS() {
		try {
			const wsUrl = indexerUrl().replace(/^http/, 'ws') + '/ws';
			const ws = new WebSocket(wsUrl);

			ws.onopen = () => {
				this.status = 'connected';
				this.reconnectDelay = RECONNECT_DELAY;
				this.stopPolling();
			};

			ws.onmessage = (e: MessageEvent) => {
				try {
					const op: UserOperation = JSON.parse(e.data as string);
					this.addOperation(op);
				} catch {
					// Ignore malformed messages.
				}
			};

			ws.onclose = () => {
				this.ws = null;
				this.startPolling();
				this.scheduleReconnect();
			};

			ws.onerror = () => {
				// onerror is always followed by onclose; just let onclose handle it.
			};

			this.ws = ws;
		} catch {
			// WebSocket constructor can throw if the URL is invalid.
			this.startPolling();
			this.scheduleReconnect();
		}
	}

	private scheduleReconnect() {
		this.stopReconnect();
		const delay = this.reconnectDelay;
		this.reconnectDelay = Math.min(this.reconnectDelay * 2, MAX_RECONNECT_DELAY);
		this.reconnectTimer = setTimeout(() => {
			this.openWS();
		}, delay);
	}

	private stopReconnect() {
		if (this.reconnectTimer !== null) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}
	}

	// --- REST polling fallback ---

	private startPolling() {
		if (this.pollTimer !== null) return;
		this.pollFailures = 0;
		this.status = 'polling';
		this.poll();
		this.pollTimer = setInterval(() => this.poll(), POLL_INTERVAL);
	}

	private stopPolling() {
		if (this.pollTimer !== null) {
			clearInterval(this.pollTimer);
			this.pollTimer = null;
		}
	}

	private async poll() {
		try {
			const apiUrl = indexerUrl() + '/api/operations';
			const res = await fetch(`${apiUrl}?limit=20`, { signal: this.abort?.signal });
			if (!res.ok) {
				this.trackPollFailure();
				return;
			}
			this.pollFailures = 0;
			const body: { data: UserOperation[] } = await res.json();
			// REST returns newest first; reverse so addOperation prepends correctly.
			for (const op of body.data.reverse()) {
				this.addOperation(op);
			}
		} catch {
			// Ignore AbortError from disconnect(); track real failures.
			if (!this.abort?.signal.aborted) {
				this.trackPollFailure();
			}
		}
	}

	private trackPollFailure() {
		this.pollFailures++;
		if (this.pollFailures >= MAX_POLL_FAILURES) {
			// Stop hammering a dead REST API, but keep the WS reconnect loop
			// running so recovery happens automatically when the indexer restarts.
			this.stopPolling();
			this.status = 'disconnected';
		}
	}

	/** Load the most recent operations from the REST API (initial page load). */
	private async loadInitial() {
		try {
			const apiUrl = indexerUrl() + '/api/operations';
			const res = await fetch(`${apiUrl}?limit=50`, { signal: this.abort?.signal });
			if (!res.ok) return;
			const body: { data: UserOperation[] } = await res.json();
			// REST returns newest first — add in reverse so newest ends up at index 0.
			for (const op of body.data.reverse()) {
				this.addOperation(op);
			}
		} catch {
			// Will be populated by WS or polling.
		}
	}

	// --- shared ---

	private addOperation(op: UserOperation) {
		if (this.seenHashes.has(op.userOpHash)) return;
		this.seenHashes.add(op.userOpHash);
		this.operations = [op, ...this.operations].slice(0, MAX_OPERATIONS);
		// Trim the dedup set to avoid unbounded growth.
		if (this.seenHashes.size > MAX_OPERATIONS * 2) {
			const keep = this.operations.map((o) => o.userOpHash);
			this.seenHashes = new Set(keep);
		}
	}
}

export const feed = new IndexerFeed();
