# cgroupguard

Evidence-based readiness checker for the Kubernetes move from **cgroup v1 to cgroup v2**.

From Kubernetes 1.35 the kubelet refuses to start on a cgroup v1 node by default, and cgroup v1 support is planned for removal.
cgroupguard tells you which nodes are still on cgroup v1 (or a hybrid layout), which workloads and agents are likely to break when their
node moves, how many pods are affected and how recoverable they are, and can write a reviewable migration plan (runbook, Terraform for
GKE/EKS/AKS, Ansible for self-managed nodes).

- **Read-only by default.** No permissions beyond reading nodes, pods and PodDisruptionBudgets; nothing is created unless you opt in.
- **Never certifies.** Output is advisory. Missing evidence is reported as *unknown*, never as *ready*.
- **Explains itself.** Every finding carries evidence, why it matters, blast radius, confidence and a primary source.
- **Remediation you can review.** `fix --plan` changes nothing; applying is a separate, confirmed, audited step.

![demo](docs/media/demo.gif)

## Install

Download a release archive, package, container image or Helm chart (see [docs/install.md](docs/install.md), including how to verify signatures),
or build from source with Go 1.27.2 or newer:

```sh
go build -o cgroupguard ./cmd/cgroupguard
```

## Use

```sh
cgroupguard check --context my-cluster            # scan nodes and workloads
cgroupguard check --input snapshot.json -o json   # scan a saved snapshot
cgroupguard fix --plan --context my-cluster       # write a remediation runbook, changing nothing
```

See the [quickstart](docs/quickstart.md), [permissions](docs/permissions.md) and the full [documentation](docs/README.md).

## Status

Pre-1.0. Rules are versioned data with cited sources; runtimes without verified documentation are marked *review*, never pass or fail.
Managed-cloud (GKE, EKS, AKS) plans are generated from provider documentation and have not yet been run on those platforms.

## License and citation

Apache License 2.0 (see [LICENSE](LICENSE)). Redistributions must keep the [NOTICE](NOTICE) file, which credits the author.
If you use cgroupguard in published work, please cite it using [CITATION.cff](CITATION.cff).
