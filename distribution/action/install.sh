#!/usr/bin/env bash
# Downloads a cgroupguard release, verifies the Cosign signature of checksums.txt against a pinned
# public key and the SHA-256 of the archive, then installs the binary. Fails closed on any problem.
# Inputs come from environment variables only: VERSION, BASE_URL, PUBKEY, DEST.
set -euo pipefail

VERSION="${VERSION:-}"; BASE_URL="${BASE_URL:-}"; PUBKEY="${PUBKEY:-}"; DEST="${DEST:-}"
for need in VERSION BASE_URL PUBKEY DEST; do
  [ -n "${!need}" ] || { echo "$need is required" >&2; exit 2; }
done

[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "invalid version" >&2; exit 2; }
# https only, except loopback (used by the test suite).
[[ "$BASE_URL" =~ ^https://[A-Za-z0-9._-]+(:[0-9]+)?(/[A-Za-z0-9._/-]*)?$ || "$BASE_URL" =~ ^http://(127\.0\.0\.1|localhost)(:[0-9]+)?(/[A-Za-z0-9._/-]*)?$ ]] || { echo "BASE_URL must be https (or loopback http)" >&2; exit 2; }
[ -f "$PUBKEY" ] || { echo "public key file not found" >&2; exit 2; }
command -v cosign >/dev/null || { echo "cosign is required (install it with a pinned installer action first)" >&2; exit 2; }

case "$(uname -s)" in Linux) os=linux;; Darwin) os=darwin;; *) echo "unsupported OS" >&2; exit 2;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo "unsupported architecture" >&2; exit 2;; esac

archive="cgroupguard_${VERSION}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
base="${BASE_URL%/}/v${VERSION}"
fetch() { curl --fail --silent --show-error --location --proto '=https,http' --max-redirs 3 --retry 3 --max-time 300 -o "$2" "$1"; }
fetch "$base/checksums.txt" "$tmp/checksums.txt"
fetch "$base/checksums.txt.sigstore.json" "$tmp/checksums.txt.sigstore.json"
fetch "$base/$archive" "$tmp/$archive"

# 1. The checksum list must be signed by our key. Verification is offline: nothing is looked up in a public log.
cosign verify-blob --key "$PUBKEY" --bundle "$tmp/checksums.txt.sigstore.json" --insecure-ignore-tlog=true "$tmp/checksums.txt" >/dev/null 2>&1 \
  || { echo "signature verification FAILED: refusing to install" >&2; exit 1; }
# 2. The archive must match the signed list exactly (exactly one matching line).
want="$(grep -E "^[0-9a-f]{64}  ${archive//./\\.}\$" "$tmp/checksums.txt" || true)"
[ "$(printf '%s\n' "$want" | grep -c .)" -eq 1 ] || { echo "$archive is not listed exactly once in the signed checksums" >&2; exit 1; }
(cd "$tmp" && printf '%s\n' "$want" | sha256sum -c - >/dev/null 2>&1) || { echo "checksum mismatch: refusing to install" >&2; exit 1; }

# 3. Extract only the binary, with no path tricks.
mkdir -p "$DEST"
tar -xzf "$tmp/$archive" -C "$tmp" cgroupguard
install -m 0755 "$tmp/cgroupguard" "$DEST/cgroupguard"
ln -sf cgroupguard "$DEST/cgg"    # the short command name
echo "cgroupguard $VERSION installed and verified"
