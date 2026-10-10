# cgroupguard

![demo](media/demo.gif)

cgroupguard checks a Kubernetes cluster, or a saved snapshot of one, for what a move from **cgroup v1 to cgroup v2** will break.
From Kubernetes 1.35 the kubelet refuses to start on a cgroup v1 node by default, and cgroup v1 support is planned for removal.
The risk is not only the nodes: workloads and agents that read v1 paths or use old runtimes can break once their node moves to v2.

It is **read-only by default**, it **never certifies** that a cluster can be upgraded, and it says **unknown** when it could not measure something.

## Start here

| If you want to... | Read |
|---|---|
| Install it and check the download | [Install](install.md) |
| See what it does in five minutes, with no cluster | [Quickstart](quickstart.md) |
| Understand evidence, confidence and blast radius | [How it works](concepts.md) |

## Guides

| Task | Guide |
|---|---|
| Scan a real cluster, step by step | [Scan a cluster](guides/scan-a-cluster.md) |
| Turn "unknown" nodes into measured ones | [Evidence sources](guides/evidence-sources.md) |
| Understand every part of a finding | [Read a report](guides/read-a-report.md) |
| Fail a pipeline before an upgrade | [CI gating](guides/ci-gating.md) |
| Plan the migration of GKE, EKS or AKS node pools | [Managed clusters](guides/plan-managed-clusters.md) |
| Plan the migration of self-managed nodes | [Self-managed nodes](guides/plan-self-managed-nodes.md) |
| Apply drains yourself or open a pull request | [Apply and pull requests](guides/apply-and-pull-requests.md) |
| Run it inside the cluster and alert on it | [Controller and monitoring](guides/controller-and-monitoring.md) |
| Get plain-language explanations from a model | [Assistant](guides/assistant.md) |
| Scan without a cluster connection | [Offline snapshots](guides/offline-snapshots.md) |

## Reference

| | |
|---|---|
| [Command reference](cli/cgroupguard.md) | every command and flag |
| [Rule catalogue](reference/rules.md) | every finding ID, what triggers it and what to do |
| [Output and exit codes](output.md) | formats, exit codes, JSON fields |
| [Permissions](permissions.md) and [optional permissions](optional-permissions.md) | the exact access each feature needs |
| [Runtime coverage](runtime-coverage.md) | which runtimes and agents have rules, and how sure we are |
| [Sources](sources.md) | where each rule and plan fact comes from |
| [Environment variables](reference/environment.md) | |
| [Glossary](reference/glossary.md) | |

## Help

[Troubleshooting](troubleshooting.md) · [FAQ](faq.md) · [Security](security.md) · [Prior art](prior-art.md)

Found a bug or a wrong result? Open an issue at https://github.com/amitpatole/cgroupguard/issues. Security problems: see [Security](security.md).
