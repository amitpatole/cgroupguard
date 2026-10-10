# FAQ

## Does a clean result mean I can upgrade?

No. Cluster readiness is always `unknown`. A clean result means no known finding was found with the
evidence available; other upgrade prerequisites are not assessed.

## Why are my nodes "unknown"?

The Kubernetes node API does not expose the cgroup mode. Add evidence (see the [Quickstart](quickstart.md)).

## Does it change my cluster?

Not by default. `fix --apply` and `--probe` are the only things that touch the cluster, and each needs an explicit flag and,
for apply, a typed confirmation per step.

## Does it send my data anywhere?

Only if you run `agent` with a provider you chose, and then only pseudonymized, secret-free data.

## Which platforms have been tested?

Continuous integration runs the full test suite on real Linux amd64, Linux arm64, macOS and Windows runners for every change. Beyond that:

- Linux amd64: a complete node migration and rollback on real cgroup v1 nodes (Ubuntu 24.04 and Rocky Linux 9, with kubeadm and k3s), a multi-node kind cluster, the controller on a real cluster, deb and rpm installs.
- Linux arm64: the probe on a real arm64 kernel in v1, hybrid and v2 modes.
- Windows: the binary runs and passes its tests; local probing needs Linux and is refused with a clear message.
- macOS: the binary passes its tests on a real runner. It is for reading snapshots and clusters; local probing needs Linux.
- **Not tested:** real managed GKE, EKS and AKS clusters (their plans are generated from the providers' documentation), the Ansible role on other distributions or on clusters with a separate control plane, and the container image on arm64 hardware.

## What about hybrid cgroup layouts?

They are reported as `hybrid` and treated as blocking, like v1: a hybrid layout is not the unified v2 hierarchy.

## Why does it say a rule is "unverified"?

The project only turns a claim into a pass or fail when a primary source states it. Everything else says "review".

## Is it free? What is the license?

Yes. Apache License 2.0; see the `LICENSE` and `NOTICE` files. Please keep the `NOTICE` file when you redistribute it. If you use it in published work, `CITATION.cff` has the citation.

## How do I report a bug or a wrong result?

Open an issue at https://github.com/amitpatole/cgroupguard/issues. Include the command, `cgroupguard --version` and the report's Limitations, with anything private removed.

## How do I report a security problem?

See [Security](security.md); do not open a public issue.
