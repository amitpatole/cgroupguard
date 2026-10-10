# How it works

## The problem in one minute

Linux has two generations of the control-group interface that containers use for CPU and memory limits. Kubernetes is moving to the second (**cgroup v2**). From
Kubernetes 1.35 the kubelet refuses to start on a node that uses the first (**cgroup v1**) by default, and v1 support is planned for removal. The node is the obvious
risk. The less obvious one is software that behaves differently on v2: an old JVM or Node.js that no longer sees its memory limit, a monitoring agent that reads v1 files
and goes silent, a script that reads `/sys/fs/cgroup/memory/...`.

cgroupguard answers three questions: **which nodes** are on v1 or a hybrid layout, **which workloads** are likely to misbehave once their node moves, and
**how to move** with the least risk.

## Evidence, not guesses

A node's cgroup mode is `v1`, `v2`, `hybrid` or `unknown`, and each value comes with the evidence behind it: a filesystem probe, the kubelet metric, a kubelet Event, a local check.
A node's OS image or kernel version is never treated as proof, because the same image can run either way. If a probe disagrees with an earlier measurement, the node is `unknown`.
See [Evidence sources](guides/evidence-sources.md).

## Workload rules

Images are matched by repository and tag against versioned rules for runtimes and agents (JVMs, Node.js, .NET, Go, monitoring and logging agents and more).
Manifests are checked for references to legacy v1 paths. Each rule is `verified` (every claim is stated by its cited source) or `unverified` (the tool may only say "review", never
pass or fail it). Tags are mutable hints, not measurements of what is installed. See [Runtime coverage](runtime-coverage.md) and the [rule catalogue](reference/rules.md).

## Findings

Each finding carries a severity, the evidence, why it matters, a source link, a confidence and a **blast radius**. Pods are folded into their controllers, so one Deployment is one
finding, not one per pod. See [Read a report](guides/read-a-report.md).

## Readiness values

| Line | Values | Notes |
|---|---|---|
| Node cgroup readiness | `pass`, `needs-migration`, `blocked`, `unknown`, `not-applicable` | `pass` means every assessed Linux node has direct v2 evidence. It is **not** permission to upgrade. `blocked` means a node is on v1 or hybrid for a target of 1.35 or later. |
| Workload assessment | `no-known-findings`, `review-required`, `unassessed` | Image tags and manifest paths only. |
| Cluster upgrade readiness | `unknown`, always | A scan reads metadata, not your applications. |

## The principles it holds to

1. **Never turn missing evidence into readiness.** Unknown stays unknown. An empty scan cannot pass.
2. **Read-only by default.** Anything that creates, changes or runs something is an explicit flag, and the risky ones need a typed confirmation and an audit log.
3. **Cite the source.** Every rule and every plan fact names a primary document ([Sources](sources.md)).
4. **Be reproducible.** The same evidence always produces byte-identical output, so reports can be diffed and gated.
5. **Say what was not looked at.** Every report ends with its limitations.

## What it does not do

It does not run code inside your workloads, read your Secrets or ConfigMaps, inspect image layers, or check other upgrade prerequisites such as kubelet configuration, API deprecations or
add-on compatibility. Use it next to tools such as kubent and Pluto: [CI gating](guides/ci-gating.md) shows how to run all three together, and [Prior art](prior-art.md) says what each covers.

## The pieces

| Piece | What it is | Guide |
|---|---|---|
| `check` | Scan and report | [Scan a cluster](guides/scan-a-cluster.md) |
| `fix --plan` | Runbook, Terraform and Ansible, written for review | [Managed](guides/plan-managed-clusters.md), [self-managed](guides/plan-self-managed-nodes.md) |
| `fix --apply`, `--pr` | Guarded cordon/drain, or a draft pull request | [Apply and pull requests](guides/apply-and-pull-requests.md) |
| Controller | Scheduled scans, a resource, metrics and Events | [Controller and monitoring](guides/controller-and-monitoring.md) |
| `agent` | Optional explanations from a model you choose | [Assistant](guides/assistant.md) |
