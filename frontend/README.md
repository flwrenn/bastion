# Bastion — Frontend

SvelteKit 2 app (Svelte 5 runes, Tailwind, viem, `permissionless.js`) shared between the smart-account and indexer flows.

For setup, demo walkthrough, and architecture notes, see the **[root README](../README.md)**.

## Commands

```sh
pnpm install    # install dependencies
pnpm dev        # dev server on http://localhost:5173
pnpm build      # production build
pnpm preview    # preview the production build
pnpm lint       # ESLint + Prettier check
```

## Environment

Copy [`.env.example`](./.env.example) to `.env`. `PUBLIC_PIMLICO_API_KEY` is required; `PUBLIC_INDEXER_URL` defaults to `http://localhost:3001`. The `PUBLIC_*_ADDRESS` values are optional — the app falls back to the committed Sepolia addresses in `src/lib/contracts/addresses.ts`. See [Quick Start](../README.md#quick-start) in the root README for details. After a redeploy, `make export-addresses` (run from the repo root) regenerates `addresses.ts` from `contracts/deployments/<chainId>.json`.
