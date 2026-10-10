# Install

Releases are published at https://github.com/amitpatole/cgroupguard/releases. Pick one channel.

| Channel | What you get |
|---|---|
| [Release archive](#release-archive) | `cgroupguard_VERSION_OS_ARCH.tar.gz` (`.zip` on Windows) for Linux, macOS and Windows on amd64 and arm64 |
| [Debian and Ubuntu package](#linux-packages) | `cgroupguard_VERSION_linux_ARCH.deb` |
| [RHEL and Fedora package](#linux-packages) | `cgroupguard_VERSION_linux_ARCH.rpm` |
| [Container image](#container-image) | `ghcr.io/amitpatole/cgroupguard:VERSION` for linux/amd64 and linux/arm64 |
| [Helm chart](#helm-chart) | `oci://ghcr.io/amitpatole/charts/cgroupguard`, the in-cluster controller |
| [From source](#from-source) | `go build`, with Go 1.27.2 or newer |

Installing the CLI never starts a controller and never touches a cluster.

## Verify what you downloaded

Every release publishes `checksums.txt` (SHA-256 of every file) and `checksums.txt.sigstore.json` (a signature of that file made with a key held in
Google Cloud KMS on a hardware security module). The matching public key is below and in the repository at `distribution/cosign.pub`. **Compare the two**: if they
differ, stop.

```text
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEPm5agPLHIQ96o6i1R2grltbKaJYC
uAXURW2SmvscTkFSj4dcN68BI76mp96gFFqi+icRMZFyoJIxWKzcukTLwA==
-----END PUBLIC KEY-----
```

Verification works offline. Nothing is looked up in a public transparency log, because the signature is made with a key and is checked against that key.

```console
$ cosign verify-blob --key cosign.pub --bundle checksums.txt.sigstore.json --insecure-ignore-tlog=true checksums.txt
Verified OK
$ sha256sum -c checksums.txt --ignore-missing
cgroupguard_0.1.1_linux_amd64.tar.gz: OK
```

`--insecure-ignore-tlog=true` only says that no transparency log is consulted. The signature itself is still checked against the key. A modified `checksums.txt` or archive
fails one of the two commands. On macOS use `shasum -a 256 -c checksums.txt --ignore-missing`.

What is signed: the checksum list, and through it every archive, package and SBOM; and the container image and the Helm chart, each by digest, with the same key.
Verify an image or a chart (the signature is stored next to it in the registry, and nothing is looked up in a public log):

```console
$ cosign verify --key cosign.pub --insecure-ignore-tlog=true ghcr.io/amitpatole/cgroupguard:0.1.1
$ cosign verify --key cosign.pub --insecure-ignore-tlog=true ghcr.io/amitpatole/charts/cgroupguard:0.1.1
```

A tag can be moved; pin by digest in production (see below), and verify that digest.

## Release archive

```console
$ VERSION=0.1.1
$ curl -fsSLO https://github.com/amitpatole/cgroupguard/releases/download/v$VERSION/cgroupguard_${VERSION}_linux_amd64.tar.gz
$ curl -fsSLO https://github.com/amitpatole/cgroupguard/releases/download/v$VERSION/checksums.txt
$ curl -fsSLO https://github.com/amitpatole/cgroupguard/releases/download/v$VERSION/checksums.txt.sigstore.json
# verify as shown above, then:
$ tar -xzf cgroupguard_${VERSION}_linux_amd64.tar.gz
$ sudo install -m 0755 cgroupguard /usr/local/bin/
$ cgroupguard --version
cgroupguard version 0.1.1
```

The archive contains `cgroupguard`, `LICENSE`, `NOTICE`, `THIRD-PARTY-NOTICES.txt`, a short `README.txt` and the read-only role manifests in `deploy/`.
On Windows, extract the `.zip` and run `cgroupguard.exe`.

An automated, fail-closed way to do the download and both checks in a pipeline is described in [CI gating](guides/ci-gating.md#install-the-tool-in-ci-and-verify-it).

macOS: the binary is not Apple-notarized. After verifying the signature, remove the quarantine flag with `xattr -dr com.apple.quarantine cgroupguard`.

## Linux packages

```console
$ sudo dpkg -i cgroupguard_0.1.1_linux_amd64.deb      # Debian, Ubuntu
$ sudo rpm -i cgroupguard_0.1.1_linux_amd64.rpm       # RHEL, Fedora
$ cgroupguard --version
```

The packages install the binary and the license files under `/usr/share/doc/cgroupguard/`. They contain no installation scripts. Remove them with
`sudo dpkg -r cgroupguard` or `sudo rpm -e cgroupguard`.

## Container image

```console
$ docker run --rm ghcr.io/amitpatole/cgroupguard:0.1.1 --version
cgroupguard version 0.1.1
```

The image is built from a minimal base with no shell, runs as a non-root user and contains only the binary and the license notices. To pin it by digest:

```console
$ docker buildx imagetools inspect ghcr.io/amitpatole/cgroupguard:0.1.1      # note the Digest: line
$ docker run --rm ghcr.io/amitpatole/cgroupguard@sha256:DIGEST --version
```

To scan a cluster from the container, mount a kubeconfig read-only. The image runs as a non-root user, so run it as the user who owns the kubeconfig (a kubeconfig readable only by you cannot be read by another user):

```console
$ docker run --rm --user "$(id -u):$(id -g)" -v "$HOME/.kube/config:/kube/config:ro" ghcr.io/amitpatole/cgroupguard@sha256:DIGEST \
      check --kubeconfig /kube/config --context my-cluster
```

## Helm chart

The controller and its chart are described in [Controller and monitoring](guides/controller-and-monitoring.md).

```console
$ helm show chart oci://ghcr.io/amitpatole/charts/cgroupguard --version 0.1.1
```

## From source

```console
$ git clone https://github.com/amitpatole/cgroupguard
$ cd cgroupguard
$ go build -o cgroupguard ./cmd/cgroupguard
$ ./cgroupguard --version
```

## Short command name (from version 0.1.1)

`cgg` runs the same program as `cgroupguard`, and its help shows the name you typed. The Debian and RPM packages install a `cgg` link, and the verifying installer
creates one next to the binary. With an archive or a source build, make it yourself:

```console
$ ln -s cgroupguard /usr/local/bin/cgg        # Linux and macOS
$ cgg check --context my-cluster
```

On Windows, copy `cgroupguard.exe` to `cgg.exe`. Everything in this documentation works with either name.

## Software bill of materials

Every archive has an SPDX document next to it (`cgroupguard_VERSION_OS_ARCH.tar.gz.spdx.json`) listing the components built into it. The licenses of those components are in
`THIRD-PARTY-NOTICES.txt` inside the archive.

## Upgrade and uninstall

To upgrade, install the new version over the old one. To uninstall, delete the binary (or remove the package). cgroupguard stores nothing on your machine except files you
explicitly ask it to write (reports, plans, audit logs).

## Platforms

Linux, macOS and Windows on amd64 and arm64 are built and tested in continuous integration on real runners for each platform. Local probing (`--local`, `probe`) works on
Linux only. See the [FAQ](faq.md#which-platforms-have-been-tested) for exactly what has been exercised.
