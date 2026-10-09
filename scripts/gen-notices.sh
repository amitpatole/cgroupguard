#!/usr/bin/env bash
# Generates distribution/THIRD-PARTY-NOTICES.txt from the dependency tree of the shipped binary.
# Binary redistribution requires these license texts (Apache-2.0, BSD, MIT, ISC).
# Usage: scripts/gen-notices.sh [--check]   (--check fails if the committed file is stale)
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$HOME/bin:$HOME/go/bin:$PATH"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
out="$tmp/notices.txt"
go-licenses save ./cmd/cgroupguard --save_path="$tmp/lic" --force >/dev/null 2>&1 || { echo "go-licenses failed" >&2; exit 2; }
go-licenses report ./cmd/cgroupguard --template <(printf '{{range .}}{{.Name}}\t{{.LicenseName}}\n{{end}}') 2>/dev/null | LC_ALL=C sort > "$tmp/list.tsv"
if grep -v -E $'\t(Apache-2.0|BSD-2-Clause|BSD-3-Clause|MIT|ISC)$' "$tmp/list.tsv" | grep -q .; then
  echo "a dependency has a license outside the reviewed permissive set:" >&2
  grep -v -E $'\t(Apache-2.0|BSD-2-Clause|BSD-3-Clause|MIT|ISC)$' "$tmp/list.tsv" >&2
  exit 3
fi
{
  echo "THIRD-PARTY NOTICES"
  echo "cgroupguard includes the following open source components. Their licenses follow."
  echo
  while IFS=$'\t' read -r name lic; do
    echo "================================================================================"
    echo "$name  ($lic)"
    echo "================================================================================"
    dir="$tmp/lic/$name"
    if [ -d "$dir" ]; then
      for f in $(find "$dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) | LC_ALL=C sort); do
        echo "--- $(basename "$f") ---"; cat "$f"; echo
      done
    else
      echo "(license text not found in module; see the module's repository)"
    fi
  done < "$tmp/list.tsv"
} > "$out"
if [ "${1:-}" = "--check" ]; then
  diff -q "$out" distribution/THIRD-PARTY-NOTICES.txt >/dev/null || { echo "distribution/THIRD-PARTY-NOTICES.txt is stale; run scripts/gen-notices.sh" >&2; exit 1; }
else
  cp "$out" distribution/THIRD-PARTY-NOTICES.txt
fi
