# Install

Pick one. All channels carry the same signed release; see [Verify what you downloaded](#verify-what-you-downloaded).

| Channel | Command |
|---|---|
| Release archive | download `cgroupguard_VERSION_OS_ARCH.tar.gz` (`.zip` on Windows) from the releases page, verify, unpack |
| kubectl plugin (Krew) | `kubectl krew install cgroupguard`, then `kubectl cgroupguard check` |
| Debian/Ubuntu | `sudo dpkg -i cgroupguard_VERSION_linux_ARCH.deb` |
| RHEL/Fedora | `sudo rpm -i cgroupguard_VERSION_linux_ARCH.rpm` |
| Container image | `ghcr.io/amitpatole/cgroupguard:VERSION` (pin by digest in production) |
| In-cluster controller | `helm install cgroupguard oci://ghcr.io/amitpatole/charts/cgroupguard --version VERSION` (see [Controller](controller.md)) |
| GitHub Actions | `amitpatole/cgroupguard-action` (verifies the download itself) |

Installing the CLI never starts a controller and never touches a cluster. Linux, macOS and Windows on amd64 and arm64 are built;
see the [FAQ](faq.md#which-platforms-have-been-tested) for what has actually been run.

## Verify what you downloaded

Every release publishes `checksums.txt` and `checksums.txt.sigstore.json` (a Cosign signature made with a key held in a managed
key service) and the public key `cosign.pub` (also committed in the action repository). Verification works offline: nothing is looked up
in a public log.

```bash
# 1. The list of checksums must be signed by the release key.
cosign verify-blob --key cosign.pub --bundle checksums.txt.sigstore.json --insecure-ignore-tlog=true checksums.txt
# 2. Your download must match the signed list.
sha256sum -c checksums.txt --ignore-missing
```

`--insecure-ignore-tlog=true` only says that no transparency log is consulted; the signature itself is checked against the key.
Also published per archive: an SPDX software bill of materials (`*.spdx.json`). Container images and the Helm chart are signed the same
way (`cosign verify --key cosign.pub --insecure-ignore-tlog=true IMAGE@sha256:...`).

macOS note: the binary is not Apple-notarized. Verify the signature as above; after verifying, remove the quarantine flag with `xattr -dr com.apple.quarantine cgroupguard`.
