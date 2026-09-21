#!/usr/bin/env bash
# dump-dev-files.sh
# Writes the pathname and contents of every text-based dev file (go, html, js,
# css, json, md, sql, yaml, toml, sh, templ, go.mod/go.sum) under this script's
# directory — recursing down — into dev-files.txt, next to this script.
#
# Skips .gitignore'd junk and everything an LLM doesn't need: .git, build
# output, vendored third-party assets, local data/media/logs, binaries, the
# .gitignore file itself, and local config.json.

set -u

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="$root/dev-files.txt"

skip_dirs=(.git node_modules bin build dist data tmp backups vendor logs exports wailsjs .vscode .idea .pi .claude .agents .opencode)

prune=()
first=1
for d in "${skip_dirs[@]}"; do
  [ $first -eq 0 ] && prune+=(-o)
  prune+=(-name "$d")
  first=0
done

find "$root" \
  \( -type d \( "${prune[@]}" \) -prune \) -o \
  \( -type f \( \
      -name '*.go' -o -name '*.templ' -o -name '*.html' -o -name '*.js' -o -name '*.css' \
      -o -name '*.json' -o -name '*.md' -o -name '*.sql' -o -name '*.yaml' -o -name '*.yml' \
      -o -name '*.toml' -o -name '*.sh' -o -name '*.txt' -o -name 'go.mod' -o -name 'go.sum' \
    \) \
    ! -name '.gitignore' ! -name 'config.json' ! -name 'dev-files.txt' ! -name '*_templ.txt' \
    -print \) 2>/dev/null | sort > "$out.list"

count=$(wc -l < "$out.list" | tr -d ' ')

{
  echo "vid-build 0.01 — dev files ($count files under $root)"
  echo
  while IFS= read -r f; do
    rel="${f#"$root"/}"
    printf '===== %s =====\n' "$rel"
    cat "$f"
    printf '\n'
  done < "$out.list"
} > "$out"

rm -f "$out.list"
echo "wrote $out ($count files)"
