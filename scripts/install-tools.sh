#!/usr/bin/env bash
# Installs the analysis and validation tools used by scripts/security-gate.sh and the
# generated-IaC validators, at pinned versions, into ${GOBIN:-$HOME/bin}.
# Release downloads are verified against the project's published checksums.
set -euo pipefail
BIN="${GOBIN:-$HOME/bin}"
mkdir -p "$BIN"
export GOBIN="$BIN"
TRIVY_VERSION=0.75.0
TFLINT_VERSION=0.64.0
GORELEASER_VERSION=2.18.2
SYFT_VERSION=1.54.1
COSIGN_VERSION=3.1.3
HELM_VERSION=4.3.0
ACTIONLINT_VERSION=1.7.12
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

(cd "$tmp" && go install golang.org/x/vuln/cmd/govulncheck@v1.8.0 \
  && go install github.com/securego/gosec/v2/cmd/gosec@v2.29.0 \
  && go install github.com/google/go-licenses/v2@latest)

# staticcheck 2026.2.1 bundles an x/tools that cannot read the compiler data of Go 1.27.2,
# so it is built against a newer x/tools (module checksums are verified by the Go toolchain).
mkdir "$tmp/staticcheck" && (cd "$tmp/staticcheck" && go mod init staticcheck-build >/dev/null 2>&1 \
  && printf 'package main\nimport _ "honnef.co/go/tools/cmd/staticcheck"\n' > tools.go \
  && go get honnef.co/go/tools@2026.2.1 golang.org/x/tools@v0.51.0 \
  && go mod tidy \
  && go build -o "${GOBIN:-$(go env GOPATH)/bin}/staticcheck" honnef.co/go/tools/cmd/staticcheck)

fetch() { # url dest
  curl -fsSL --retry 4 --retry-delay 3 -o "$2" "$1"
}
base="https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}"
fetch "$base/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz" "$tmp/trivy.tgz"
fetch "$base/trivy_${TRIVY_VERSION}_checksums.txt" "$tmp/trivy.sums"
(cd "$tmp" && grep "trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz" trivy.sums | sed 's# .*# trivy.tgz#' | sha256sum -c - && tar xzf trivy.tgz trivy && install -m 0755 trivy "$BIN/trivy")

base="https://github.com/terraform-linters/tflint/releases/download/v${TFLINT_VERSION}"
fetch "$base/tflint_linux_amd64.zip" "$tmp/tflint.zip"
fetch "$base/checksums.txt" "$tmp/tflint.sums"
(cd "$tmp" && grep tflint_linux_amd64.zip tflint.sums | sed 's# .*# tflint.zip#' | sha256sum -c - && unzip -q -o tflint.zip tflint && install -m 0755 tflint "$BIN/tflint")

# Release toolchain (used by the release workflow and by local snapshot builds).
only_release() { [ "${INSTALL_RELEASE_TOOLS:-1}" = "1" ]; }
if only_release; then
  gh_base="https://github.com"
  # goreleaser
  fetch "$gh_base/goreleaser/goreleaser/releases/download/v${GORELEASER_VERSION}/goreleaser_Linux_x86_64.tar.gz" "$tmp/gr.tgz"
  fetch "$gh_base/goreleaser/goreleaser/releases/download/v${GORELEASER_VERSION}/checksums.txt" "$tmp/gr.sums"
  (cd "$tmp" && grep ' goreleaser_Linux_x86_64.tar.gz$' gr.sums | sed 's# .*# gr.tgz#' | sha256sum -c - && tar xzf gr.tgz goreleaser && install -m 0755 goreleaser "$BIN/goreleaser")
  # syft
  fetch "$gh_base/anchore/syft/releases/download/v${SYFT_VERSION}/syft_${SYFT_VERSION}_linux_amd64.tar.gz" "$tmp/syft.tgz"
  fetch "$gh_base/anchore/syft/releases/download/v${SYFT_VERSION}/syft_${SYFT_VERSION}_checksums.txt" "$tmp/syft.sums"
  (cd "$tmp" && grep " syft_${SYFT_VERSION}_linux_amd64.tar.gz\$" syft.sums | sed 's# .*# syft.tgz#' | sha256sum -c - && tar xzf syft.tgz syft && install -m 0755 syft "$BIN/syft")
  # cosign
  fetch "$gh_base/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-amd64" "$tmp/cosign"
  fetch "$gh_base/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign_checksums.txt" "$tmp/cosign.sums"
  (cd "$tmp" && grep ' cosign-linux-amd64$' cosign.sums | sed 's# .*# cosign#' | sha256sum -c - && install -m 0755 cosign "$BIN/cosign")
  # helm
  fetch "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz" "$tmp/helm.tgz"
  fetch "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz.sha256sum" "$tmp/helm.sum"
  (cd "$tmp" && sed 's#helm-.*#helm.tgz#' helm.sum | sha256sum -c - && tar xzf helm.tgz linux-amd64/helm && install -m 0755 linux-amd64/helm "$BIN/helm")
  # actionlint
  fetch "$gh_base/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz" "$tmp/al.tgz"
  fetch "$gh_base/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_checksums.txt" "$tmp/al.sums"
  (cd "$tmp" && grep " actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz\$" al.sums | sed 's# .*# al.tgz#' | sha256sum -c - && tar xzf al.tgz actionlint && install -m 0755 actionlint "$BIN/actionlint")
fi
echo "installed into $BIN"
