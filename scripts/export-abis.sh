#!/usr/bin/env bash
set -euo pipefail

CONTRACTS_OUT="contracts/out"
FRONTEND_ABIS="frontend/src/lib/contracts"

mkdir -p "$FRONTEND_ABIS"

count=0

for sol_file in contracts/src/*.sol; do
  name=$(basename "$sol_file" .sol)
  artifact="$CONTRACTS_OUT/$name.sol/$name.json"

  if [ ! -f "$artifact" ]; then
    echo "warning: no artifact for $name, skipping"
    continue
  fi

  abi=$(jq '.abi' "$artifact")

  cat > "$FRONTEND_ABIS/$name.ts" <<EOF
export const ${name}Abi = ${abi} as const;
EOF

  count=$((count + 1))
  echo "exported $name"
done

echo "done: $count ABIs written to $FRONTEND_ABIS/"
