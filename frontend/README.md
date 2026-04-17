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

Copy [`.env.example`](./.env.example) to `.env` and fill in the `PUBLIC_*` values. See [Quick Start](../README.md#quick-start) in the root README for which values to use; `make export-addresses` regenerates contract addresses after a redeploy.
