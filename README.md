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

Per-component details. See [Quick Start](#quick-start) for the condensed zero-to-running flow.

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
| `SmartAccount` | ERC-4337 account — ECDSA owner validation, session keys, `execute`/`executeBatch`, proxy-compatible via `Initializable` | Implemented |
| `SmartAccountFactory` | CREATE2 deployment of SmartAccount proxies; `createAccount(owner, salt)` and `getAddress(owner, salt)` | Implemented |
| `Counter` | Demo target — per-account counters, `increment()` and `getCount(address)` | Implemented |
| `FaucetToken` | ERC-20 (symbol `BFT`) with `claim()` faucet mint | Implemented |

**EntryPoint v0.7 (external, canonical):** [`0x0000000071727De22E5E9d8BAf0edAc6f37da032`](https://sepolia.etherscan.io/address/0x0000000071727De22E5E9d8BAf0edAc6f37da032)

### Deployed on Sepolia

All four project contracts are deployed and verified on Sepolia. Addresses come from [`contracts/deployments/11155111.json`](contracts/deployments/11155111.json) — re-run `make export-addresses` after a redeploy to sync them into `frontend/src/lib/contracts/addresses.ts`.

| Contract | Address | Etherscan |
|----------|---------|-----------|
| `SmartAccountFactory` | `0x903794183FB881FC78dCA8c9CEB63EC7F10BD5Fd` | [View](https://sepolia.etherscan.io/address/0x903794183FB881FC78dCA8c9CEB63EC7F10BD5Fd#code) |
| `SmartAccount` (implementation) | `0x436365cBED02eFBf3F7adb3Da35FbA8098A94a52` | [View](https://sepolia.etherscan.io/address/0x436365cBED02eFBf3F7adb3Da35FbA8098A94a52#code) |
| `Counter` | `0x1bFe2EE14a1AFac835bB4C3Dc61d8f3520335e94` | [View](https://sepolia.etherscan.io/address/0x1bFe2EE14a1AFac835bB4C3Dc61d8f3520335e94#code) |
| `FaucetToken` (`BFT`) | `0x7EFb41d61f894e787405c5D7E114dB86542adafF` | [View](https://sepolia.etherscan.io/address/0x7EFb41d61f894e787405c5D7E114dB86542adafF#code) |

> The `SmartAccount` entry is the implementation contract behind every proxy the factory deploys; per-user accounts are deterministic CREATE2 addresses derived from `(owner, salt)` and are counterfactual until first UserOp.

## Demo Walkthrough

End-to-end flow an evaluator can reproduce in ~5 minutes after completing [Quick Start](#quick-start). Every UserOp below is sponsored by the Pimlico paymaster — **no Sepolia ETH required** in the connecting wallet.

### Prerequisites

- Browser wallet (MetaMask, Rabby, …) with any Sepolia-capable account. No funding required.
- A Pimlico API key with paymaster sponsorship enabled (free tier is sufficient).
- Indexer, frontend, and Postgres running locally (Quick Start covers this).

### Steps

1. **Open `http://localhost:5173/` and connect your wallet.** The app issues `wallet_switchEthereumChain` to Sepolia automatically — approve if prompted.

2. **Observe the counterfactual SmartAccount address.** Derived via `SmartAccountFactory.getAddress(owner, 0)`. An `eth_getCode` call returns `0x`, confirming the account has not been deployed yet — this is an important property of ERC-4337: the address is usable before deployment.

3. **Click *Deploy Account*.** The frontend submits a sponsored no-op UserOp with `initCode` set to the factory + `createAccount` calldata. The EntryPoint deploys the proxy in the same transaction that runs `validateUserOp`. Post-deploy, `getCode` returns non-empty bytecode. The Jiffyscan link in the success toast shows the UserOp lifecycle (validation → deploy → execute).

4. **Open the Counter card, click *Increment*.** Encoded as `SmartAccount.execute(counter, 0, abi.encodeCall(Counter.increment, ()))`, signed by the owner, sponsored by Pimlico. `Counter.getCount(smartAccount)` reflects the new value. The `UserOperationEvent` emitted by the EntryPoint appears in the indexer feed (step 11) within one poll cycle.

5. **Open the Faucet card, click *Claim Tokens*.** Same pattern, targeting `FaucetToken.claim()`. The card refreshes the connecting wallet's `BFT` balance after the UserOp lands. Useful as a second sponsored owner-flow data point before moving to session keys.

6. **Open *Session Keys*, click *Generate*.** The browser creates a fresh secp256k1 keypair entirely in memory — the private key never leaves the page and is **not** persisted. Pick:
   - **Target:** `Counter` or `FaucetToken`
   - **Selector:** one of `increment()`, `claim()`, or `transfer(address,uint256)`
   - **Valid window:** `validAfter` / `validUntil` (UNIX seconds)

7. **Click *Register*.** The frontend submits an owner-signed UserOp whose inner `execute` call targets the SmartAccount itself and invokes `registerSessionKey(publicKey, target, selector, validAfter, validUntil)`. Emits `SessionKeyAdded`. **Copy the session-key private key now** — it only exists in browser memory; a page reload loses it forever.

8. **Open `http://localhost:5173/session` in a new tab.** Paste the SmartAccount address and the session-key private key, click *Load*. The page reads `sessionKeys(publicKey)` on-chain to verify scope (target, selector, window) and cross-checks that the caller's public key matches.

9. **In the Permissions card, click *Execute*.** The session-key-flow helper signs a UserOp locally with the session-key private key (never prompting the wallet), submits it through Pimlico's bundler, and the paymaster sponsors gas. The SmartAccount's `validateUserOp` walks into `_validateSessionKey`, which checks signer ∈ active session keys, inner call target matches `target`, 4-byte selector matches `selector`, and `block.timestamp ∈ [validAfter, validUntil]`. Jiffyscan + Etherscan links appear on success.

10. **Return to the first tab, click *Revoke* on the session key.** Owner-signed UserOp calling `revokeSessionKey(publicKey)`. Emits `SessionKeyRevoked`. Re-running step 9 now fails at `validateUserOp` — the EntryPoint surfaces `SIG_VALIDATION_FAILED` and the bundler rejects the op.

11. **Open `http://localhost:5173/indexer`.** The WebSocket feed streams every `UserOperationEvent` from the demo in order. The stats panel shows total ops, success rate, sponsored-% (should be 100% — paymaster covered all ops), and unique senders. Etherscan links on each row let the evaluator cross-check the on-chain transaction.

12. **(Optional) Observe reorg resilience.** Kill the indexer mid-demo (`Ctrl-C` on `make indexer-dev` / `make dev`) and restart it. On boot it resumes from the persisted cursor and rewinds `INDEXER_REORG_WINDOW` blocks to re-index anything that might have reorged in that window. The feed catches back up without double-counting — atomic `ReplaceOperationsAndSetCursor` deletes the rewound range and re-inserts in one transaction.

## Architecture Decisions

One bullet per decision, in the order an evaluator is likely to ask about them.

- **Pimlico as bundler and paymaster.** Single endpoint for ERC-4337 v0.7 bundling and Sepolia gas sponsorship; `permissionless.js` has first-class client support. Avoids standing up our own bundler just for the demo.
- **Sepolia as the target chain.** Only stable public testnet with the canonical v0.7 EntryPoint deployed, active paymaster support, reliable Alchemy/Infura endpoints, and free Etherscan verification.
- **CREATE2 factory for SmartAccount deployment.** Counterfactual addresses let the UI display and interact with an account *before* it exists on-chain. `initCode` on the first UserOp deploys the proxy atomically via the EntryPoint — deployment and the first action share a single user signature.
- **Session-key scope enforced at the account, not the key.** Scope checks (target address, 4-byte selector, `validAfter`/`validUntil`) live inside `_validateSessionKey` on `SmartAccount`. A session key holds no authority outside the account that registered it — compromising one doesn't grant access to any other account that happens to authorize the same public key.
- **Session keys are single-call only.** `validateUserOp` requires the outer call to be `execute` (not `executeBatch`). Scoping a batch would mean iterating every inner call inside `validateUserOp`, inflating verification gas and making the scope check non-atomic under partial-revert semantics. Single-call keeps the security model simple.
- **In-memory session-key list in the frontend.** Deliberate. Making the mapping enumerable on-chain would add storage and gas on every register/revoke. The `SessionKeyAdded` / `SessionKeyRevoked` events are the auditable source of truth; a production UI would index them (which is exactly what Part 2 demonstrates at the EntryPoint level).
- **Go + `net/http` stdlib for the indexer.** No web framework, no ORM. The scope is small enough that the stdlib's ergonomics are fine, and it keeps the binary + dependency surface minimal.
- **PostgreSQL, not SQLite.** We need atomic reorg handling in a single transaction (`ReplaceOperationsAndSetCursor` deletes all rows above `fromBlock`, inserts the re-scanned range, and updates the cursor as one unit). PG also gives real concurrency so the API and the indexer loop can share the DB without locking contention.
- **Polling + WebSocket hybrid for event ingestion.** `eth_getLogs` polling is the authoritative range scanner and the only path that handles reorgs. `eth_subscribe` over `WS_RPC_URL` is a *trigger* — a `newHeads` event causes the loop to wake immediately instead of sleeping for `INDEXER_POLL_INTERVAL`. If the WS drops, polling continues unchanged. WS is optimization, not a dependency.
- **REST for history, WebSocket for live.** REST is cacheable, paginated, and easy to test; the frontend hydrates with REST then overlays the WS stream for deltas. Falls back to REST polling if WS is unavailable.
- **Reorg strategy: confirmation lag + rewind window.** Only index blocks at `safeHead = latest − INDEXER_CONFIRMATIONS`. Each loop rewinds `INDEXER_REORG_WINDOW` blocks from the cursor before the next `getLogs` call, so any range that reorged within the window is re-indexed. Replacement is atomic (delete-above + insert + cursor update in one transaction), so readers never observe a partial state.

## Limitations & Trade-offs

- **Session keys are single-call only.** The account validates that the outer call is `execute`, not `executeBatch`. Lifting this would require scoping every inner call individually during `validateUserOp`.
- **Session-key list is not enumerable on-chain.** Registered keys are stored in a non-iterable mapping; the UI tracks the set in memory and relies on `SessionKeyAdded` / `SessionKeyRevoked` events as the source of truth. A production UI would index these.
- **Single EntryPoint on a single chain.** The indexer is configured for one `ENTRYPOINT` on one `RPC_URL`. Scaling to multiple contracts or chains would need a worker-per-chain model sharing the Postgres instance — orthogonal to the rest of the design, but not implemented.
- **No frontend test suite.** Deliberate scope choice — frontend behavior is covered by the manual [Demo Walkthrough](#demo-walkthrough). Contracts have Foundry tests, indexer has Go tests (`make test`).

## Running Tests

```sh
make test          # forge test -vvv  +  go test ./...
make forge-test    # contracts only
make indexer-test  # indexer only
```

The frontend has no automated test suite — see [Limitations & Trade-offs](#limitations--trade-offs). Verification there is manual via the [Demo Walkthrough](#demo-walkthrough).
