# Bastion

ERC-4337 smart account system with session key support and an EVM event indexer, sharing a single SvelteKit frontend.

Built with Foundry (Solidity), Go (`net/http`), and SvelteKit 2 (Svelte 5, viem, permissionless.js).

## Architecture

```
┌──────────────┐     UserOps      ┌─────────────┐     handleOps     ┌────────────┐
│   Frontend   │ ───────────────► │   Bundler   │ ────────────────► │ EntryPoint │
│  (SvelteKit) │                  │  (Pimlico)  │                   │   (v0.7)   │
└──────┬───────┘                  └─────────────┘                   └─────┬──────┘
       │                                                                  │
       │  REST / WebSocket                              validateUserOp + execute
       │                                                                  │
┌──────▼───────┐                                                  ┌───────▼──────┐
│   Indexer    │   eth_getLogs / eth_subscribe                    │ SmartAccount │
│    (Go)      │ ◄──────────────────────────────────────────────  │  (Solidity)  │
└──────────────┘                                                  └──────────────┘
```

**Part 1 — Smart Contracts:** ERC-4337 compliant SmartAccount with ECDSA owner validation and session key support, deployed via CREATE2 factory. Demo contracts (Counter, FaucetToken) for interaction.

**Part 2 — EVM Indexer:** Go backend that indexes `UserOperationEvent` from EntryPoint v0.7, with PostgreSQL persistence, REST API, and WebSocket live feed.

**Shared Frontend:** SvelteKit app for wallet connection, smart account deployment, owner and session key interactions, and indexer dashboard.

## Quick Start

Get from clone to all services running against Sepolia in under 10 minutes.

### 1. Root env

```sh
cp .env.example .env
```

Fill in:

- `SEPOLIA_RPC_URL` — Alchemy/Infura/QuickNode Sepolia HTTPS endpoint
- `ETHERSCAN_API_KEY` — only required if you plan to redeploy with `make forge-deploy`
- `DEPLOYER_PRIVATE_KEY` — only required if you plan to redeploy
- `DATABASE_URL` — leave as-is if using the bundled `make db-up`
- `RPC_URL` — same Sepolia endpoint as `SEPOLIA_RPC_URL`
- `INDEXER_START_BLOCK` — any recent Sepolia block before the first deployed `UserOperationEvent` you want indexed

### 2. Frontend env

```sh
cp frontend/.env.example frontend/.env
```

Fill in:

- `PUBLIC_FACTORY_ADDRESS`, `PUBLIC_COUNTER_ADDRESS`, `PUBLIC_FAUCET_TOKEN_ADDRESS` — use the addresses from the [Deployed on Sepolia](#deployed-on-sepolia) table, or run `make export-addresses` after a redeploy to regenerate them automatically
- `PUBLIC_PIMLICO_API_KEY` — create one at [pimlico.io](https://pimlico.io); the free tier covers the demo
- `PUBLIC_INDEXER_URL` — leave as `http://localhost:3001`

### 3. Build, then run

```sh
make db-up          # Postgres via docker compose
make forge-build    # Compile contracts
make export-abis    # Bridge ABIs into frontend/src/lib/contracts/
make dev            # Starts indexer + frontend (contracts already compiled)
```

- Frontend: http://localhost:5173
- Indexer API: http://localhost:3001

Running a fresh deployment is **not** required — the Sepolia addresses in the status table below are live and verified. If you do want to redeploy, `make forge-deploy` handles build, broadcast, and Etherscan verification in one step (requires `ETHERSCAN_API_KEY` and `DEPLOYER_PRIVATE_KEY`).

## Directory Structure

```
bastion/
├── contracts/          # Foundry — Solidity sources, tests, deploy scripts
│   ├── src/            # Contract source files
│   ├── test/           # Foundry tests (*.t.sol)
│   ├── script/         # Deployment scripts
│   └── lib/            # Git submodule deps (forge-std, OZ, account-abstraction, solady)
├── indexer/            # Go module — EVM event indexer
│   ├── cmd/indexer/    # Entry point (main.go)
│   └── internal/       # Internal packages (api/, indexer/, db/)
├── frontend/           # SvelteKit 2 — shared between both pillars
│   └── src/lib/        # Utilities, stores, contract ABIs
├── scripts/            # Build/tooling scripts (export-abis.sh)
└── Makefile            # Orchestrates all three components
```

## Prerequisites

- [Foundry](https://getfoundry.sh/) (forge, cast, anvil)
- [Node.js](https://nodejs.org/) v20+ and [pnpm](https://pnpm.io/) v9+
- [Go](https://go.dev/) 1.25+
- [PostgreSQL](https://www.postgresql.org/) 15+

## Setup

### Contracts

```sh
cd contracts
forge build       # Compile
forge test -vvv   # Run tests
```

### Frontend

```sh
cd frontend
pnpm install
pnpm dev          # Dev server on http://localhost:5173
```

### Indexer

```sh
cd indexer
go build ./cmd/indexer
go run ./cmd/indexer   # Starts on http://localhost:3001
```

Required env vars:

- `DATABASE_URL` — PostgreSQL DSN
- `RPC_URL` — chain JSON-RPC endpoint
- `INDEXER_START_BLOCK` — required on first run (no cursor) to define historical backfill start block

Optional indexer env vars:

- `WS_RPC_URL` — WebSocket RPC endpoint for `eth_subscribe` new-head triggers (falls back to poll-only when unset)
- `ENTRYPOINT` — override EntryPoint address (default: canonical v0.7)
- `INDEXER_BATCH_SIZE` — max block span per `eth_getLogs` batch (default `500`)
- `INDEXER_CONFIRMATIONS` — confirmation lag before indexing (default `3`)
- `INDEXER_REORG_WINDOW` — rewind window from cursor each loop (default = confirmations)
- `INDEXER_POLL_INTERVAL` — polling interval (default `4s`)
- `INDEXER_REQUEST_TIMEOUT` — per-RPC request timeout (default `15s`)
- `INDEXER_RPC_CONCURRENCY` — max concurrent RPC calls for tx/block enrichment (default `8`)
- `INDEXER_RPC_RESPONSE_MAX_BYTES` — max RPC response size before adaptive range splitting (default `8388608`)
- `INDEXER_RPC_MAX_RETRIES` — total attempts per RPC call including the initial request (default `5`, max `20`)
- `INDEXER_RPC_RETRY_BASE_DELAY` — initial backoff delay between retries (default `500ms`)
- `INDEXER_RPC_RETRY_MAX_DELAY` — maximum backoff delay cap (default `30s`)
- `INDEXER_ENABLE_TX_ENRICHMENT` — toggle tx input decoding for `target`/`calldata` enrichment (default `true`)
- `INDEXER_ALLOW_CURSOR_TRIM` — allow destructive trim when cursor is ahead of safe head (default `false`)

### Makefile (all components)

```sh
make build    # Build contracts + frontend + indexer
make test     # Run all test suites
```

## Contracts

| Contract | Description | Status |
|----------|-------------|--------|
| `SmartAccount` | ERC-4337 account — ECDSA owner validation, `execute`/`executeBatch`, proxy-compatible | Implemented |
| `SmartAccountFactory` | CREATE2 deployment of SmartAccount proxies | Not started |
| `Counter` | Scaffold — needs rewrite to per-account spec (`getCount(address)`) | Scaffold |
| `FaucetToken` | ERC-20 with `claim()` faucet | Not started |

**EntryPoint v0.7:** `0x0000000071727De22E5E9d8BAf0edAc6f37da032`

<!-- ## Contract Addresses (Sepolia)

Deployed addresses will be added after Issue #7. -->

<!-- ## Demo Walkthrough

Step-by-step demo guide will be added once the frontend is functional. -->
