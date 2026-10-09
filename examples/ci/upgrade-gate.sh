#!/usr/bin/env bash
# Runs kubent, Pluto and cgroupguard in one CI stage. Each tool keeps its own exit
# semantics; this script reports each result and fails if ANY tool fails or errors.
# cgroupguard complements the other two: it does not check API deprecations.
#
# Requires kubent, pluto and cgroupguard on PATH and a kubeconfig for the target cluster.
# Usage: TARGET=1.35 [KUBE_CONTEXT=ctx] [OUT=./reports] ./upgrade-gate.sh
set -u
TARGET="${TARGET:?set TARGET to the planned Kubernetes minor version, for example 1.35}"
OUT="${OUT:-./reports}"
CTX_ARGS=()
KUBENT_CTX=()
if [ -n "${KUBE_CONTEXT:-}" ]; then
  CTX_ARGS=(--context "$KUBE_CONTEXT")
  KUBENT_CTX=(-x "$KUBE_CONTEXT")
fi
mkdir -p "$OUT"
fail=0

# kubent: --exit-error makes it exit 200 when deprecated APIs are found.
kubent ${KUBENT_CTX[@]+"${KUBENT_CTX[@]}"} -t "$TARGET" -o json -e >"$OUT/kubent.json" 2>"$OUT/kubent.log"
k=$?
# Pluto: exit 2 deprecated, 3 removed, 4 deprecated without replacement; others are errors.
pluto detect-all-in-cluster --target-versions "k8s=v${TARGET}.0" -o json >"$OUT/pluto.json" 2>"$OUT/pluto.log"
p=$?
# cgroupguard: -e exits 200 on findings at or above --fail-on; 2 is an error.
cgroupguard check ${CTX_ARGS[@]+"${CTX_ARGS[@]}"} -t "$TARGET" -e --fail-on warning --format sarif >"$OUT/cgroupguard.sarif" 2>"$OUT/cgroupguard.log"
c=$?

echo "kubent exit=$k  pluto exit=$p  cgroupguard exit=$c"
[ "$k" -ne 0 ] && { echo "kubent: gate failed (200 = deprecated APIs, other = error); see $OUT/kubent.log"; fail=1; }
[ "$p" -ne 0 ] && { echo "pluto: gate failed (2/3/4 = findings, other = error); see $OUT/pluto.log"; fail=1; }
[ "$c" -ne 0 ] && { echo "cgroupguard: gate failed (200 = findings, 2 = error); see $OUT/cgroupguard.log"; fail=1; }
[ "$c" -eq 0 ] && echo "note: cgroupguard exit 0 means no findings at or above --fail-on; coverage is still incomplete and this is not an upgrade certification."
exit "$fail"
