#!/usr/bin/env bash
# Copy each skill in third-party-skills.tsv from its upstream repo into
# .opencode/skills/<skill>/, byte for byte, at the pinned commit.
#
#   .opencode/update-third-party-skills.sh           copy the pinned commits
#   .opencode/update-third-party-skills.sh --latest  copy upstream HEAD and repin
#
# Review the result with `git diff`. .opencode/ is gitignored, so stage with
# `git add -f .opencode`.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TSV="$HERE/third-party-skills.tsv"
LATEST=0; [ "${1:-}" = "--latest" ] && LATEST=1
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

tail -n +2 "$TSV" | cut -f2 | sort -u | while read -r repo; do
  git clone -q "https://github.com/$repo.git" "$TMP/${repo//\//_}"
done

mkdir -p "$HERE/skills"
out="$TMP/manifest"; head -1 "$TSV" > "$out"
tail -n +2 "$TSV" | while IFS=$'\t' read -r skill repo path commit license; do
  src="$TMP/${repo//\//_}"
  [ $LATEST = 1 ] && commit="$(git -C "$src" rev-parse --short HEAD)"
  git -C "$src" checkout -q "$commit"
  [ -f "$src/$path/SKILL.md" ] || { echo "$repo@$commit has no $path/SKILL.md" >&2; exit 1; }
  rm -rf "$HERE/skills/$skill"
  cp -R "$src/$path" "$HERE/skills/$skill"
  if ! ls "$HERE/skills/$skill" | grep -qi '^license' && [ -f "$src/LICENSE" ]; then
    cp "$src/LICENSE" "$HERE/skills/$skill/LICENSE"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$skill" "$repo" "$path" "$commit" "$license" >> "$out"
  echo "$skill <- $repo@$commit"
done
cp "$out" "$TSV"
