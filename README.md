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
