import { createPublicClient, createWalletClient, custom, http, type WalletClient } from 'viem';
import { sepolia } from 'viem/chains';

const SEPOLIA_CHAIN_ID = sepolia.id;

export const publicClient = createPublicClient({
	chain: sepolia,
	transport: http()
});

class WalletState {
	address = $state<`0x${string}` | null>(null);
	chainId = $state<number | null>(null);
	client = $state<WalletClient | null>(null);
	error = $state<string | null>(null);

	get connected() {
		return this.address !== null;
	}

	get correctChain() {
		return this.chainId === SEPOLIA_CHAIN_ID;
	}

	get chainName() {
		if (this.chainId === SEPOLIA_CHAIN_ID) return 'Sepolia';
		if (this.chainId !== null) return `Chain ${this.chainId}`;
		return null;
	}

	async connect() {
		this.error = null;

		if (!window.ethereum) {
			this.error = 'No wallet detected. Install MetaMask.';
			return;
		}

		try {
			const accounts = await window.ethereum.request({ method: 'eth_requestAccounts' });
			if (!accounts.length) {
				this.error = 'No account selected.';
				return;
			}
			const account = accounts[0];

			const chainIdHex = await window.ethereum.request({ method: 'eth_chainId' });
			const chainId = Number(chainIdHex);

			if (chainId !== SEPOLIA_CHAIN_ID) {
				await this.switchToSepolia();
				const verifiedHex = await window.ethereum!.request({ method: 'eth_chainId' });
				if (Number(verifiedHex) !== SEPOLIA_CHAIN_ID) {
					this.error = 'Please switch to the Sepolia network.';
					return;
				}
			}

			this.client = createWalletClient({
				account: account as `0x${string}`,
				chain: sepolia,
				transport: custom(window.ethereum)
			});
			this.address = account as `0x${string}`;
			this.chainId = SEPOLIA_CHAIN_ID;

			this.unlisten();
			this.listen();
		} catch (e) {
			this.reset();
			this.error = e instanceof Error ? e.message : 'Failed to connect wallet';
		}
	}

	disconnect() {
		this.unlisten();
		this.reset();
	}

	private reset() {
		this.address = null;
		this.chainId = null;
		this.client = null;
		this.error = null;
	}

	private async switchToSepolia() {
		if (!window.ethereum) return;

		try {
			await window.ethereum.request({
				method: 'wallet_switchEthereumChain',
				params: [{ chainId: `0x${SEPOLIA_CHAIN_ID.toString(16)}` }]
			});
		} catch (e: unknown) {
			const err = e as { code?: number };
			if (err.code === 4902) {
				await window.ethereum.request({
					method: 'wallet_addEthereumChain',
					params: [
						{
							chainId: `0x${SEPOLIA_CHAIN_ID.toString(16)}`,
							chainName: sepolia.name,
							nativeCurrency: sepolia.nativeCurrency,
						rpcUrls: [sepolia.rpcUrls.default.http[0]],
						blockExplorerUrls: [sepolia.blockExplorers.default.url]
						}
					]
				});
			} else {
				throw e;
			}
		}
	}

	private onAccountsChanged = (accounts: string[]) => {
		if (accounts.length === 0) {
			this.disconnect();
		} else {
			this.address = accounts[0] as `0x${string}`;
			if (this.client && window.ethereum) {
				this.client = createWalletClient({
					account: this.address,
					chain: sepolia,
					transport: custom(window.ethereum)
				});
			}
		}
	};

	private onChainChanged = (chainIdHex: string) => {
		this.chainId = Number(chainIdHex);
		if (this.chainId === SEPOLIA_CHAIN_ID) {
			this.error = null;
		} else {
			this.switchToSepolia()
				.then(async () => {
					const hex = await window.ethereum!.request({ method: 'eth_chainId' });
					const verified = Number(hex);
					if (verified === SEPOLIA_CHAIN_ID) {
						this.chainId = SEPOLIA_CHAIN_ID;
						this.error = null;
					} else {
						this.error = 'Please switch to Sepolia network.';
					}
				})
				.catch(() => {
					this.error = 'Please switch to Sepolia network.';
				});
		}
	};

	private listen() {
		if (!window.ethereum) return;
		window.ethereum.on('accountsChanged', this.onAccountsChanged);
		window.ethereum.on('chainChanged', this.onChainChanged);
	}

	private unlisten() {
		if (!window.ethereum) return;
		window.ethereum.removeListener('accountsChanged', this.onAccountsChanged);
		window.ethereum.removeListener('chainChanged', this.onChainChanged);
	}
}

export const wallet = new WalletState();
