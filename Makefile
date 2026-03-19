.PHONY: forge-build forge-test forge-deploy forge-deploy-dry chain \
       export-abis export-addresses dev build test lint \
       indexer-build indexer-test indexer-dev \
       frontend-dev frontend-build frontend-lint

# ── Foundry ──────────────────────────────────────────────────
forge-build:
	cd contracts && forge build

forge-test:
	cd contracts && forge test -vvv

forge-deploy:
	cd contracts && forge script script/Deploy.s.sol:Deploy \
		--rpc-url sepolia \
		--broadcast \
		--verify \
		--etherscan-api-key $(ETHERSCAN_API_KEY) \
		-vvvv

forge-deploy-dry:
	cd contracts && forge script script/Deploy.s.sol:Deploy \
		--rpc-url sepolia \
		-vvvv

chain:
	anvil

# ── Go indexer ───────────────────────────────────────────────
indexer-build:
	cd indexer && go build -o bin/indexer ./cmd/indexer

indexer-test:
	cd indexer && go test ./...

indexer-dev:
	cd indexer && go run ./cmd/indexer

# ── Frontend ─────────────────────────────────────────────────
frontend-dev:
	cd frontend && pnpm dev

frontend-build:
	cd frontend && pnpm build

frontend-lint:
	cd frontend && pnpm lint

# ── ABI bridge ───────────────────────────────────────────────
export-abis: forge-build
	./scripts/export-abis.sh

export-addresses:
	./scripts/export-addresses.sh

# ── Full stack ───────────────────────────────────────────────
dev:
	$(MAKE) -j3 indexer-dev forge-build frontend-dev

build: forge-build indexer-build frontend-build

test: forge-test indexer-test

lint: frontend-lint
