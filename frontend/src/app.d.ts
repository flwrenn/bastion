import type { EIP1193Provider } from 'viem';

declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}

	interface Window {
		ethereum?: EIP1193Provider;
	}
}

export {};
