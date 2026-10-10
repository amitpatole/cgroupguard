# Glossary

**Blast radius.** How much a finding can affect and how well it would cope: pods and nodes affected, scope, failure mode, recoverability and criticality. Counted from the
inventory, or `unknown`. See [Read a report](../guides/read-a-report.md).

**cgroup.** A Linux kernel feature that limits and measures a group of processes' CPU, memory and other resources. Containers use it.

**cgroup v1 / v2.** The two versions of the interface. v1 has a separate directory tree per resource controller. v2 has one unified tree. From Kubernetes 1.35 the kubelet
refuses to start on a v1 node by default, and v1 support is planned for removal.

**Confidence.** How sure a finding is: `high` (a direct measurement or exact text match), `low` (a heuristic such as an image tag), `unknown`.

**Drain.** Evicting the pods from a node so it can be changed. Respects PodDisruptionBudgets.

**Evidence.** The observation behind a node's cgroup mode (a filesystem probe, a metric, an Event). Every node in a report has some.

**Family.** A group of related images that share a rule (for example the HotSpot JVMs).

**Hybrid.** A node with cgroup v1 controllers and also a v2 mount. Not the unified v2 layout; treated as blocking, like v1.

**Hint, heuristic.** A conclusion drawn from an image tag or a path in a manifest rather than from the running software.

**Kubelet.** The agent on each node that runs pods. Its `failCgroupV1` option controls whether it refuses to start on cgroup v1.

**PodDisruptionBudget (PDB).** A Kubernetes object that limits how many replicas of an application may be down at once during voluntary disruptions such as a drain.

**Readiness.** The verdict lines of a report. *Node readiness*: `pass`, `needs-migration`, `blocked`, `unknown`, `not-applicable`. *Workload assessment*: `no-known-findings`,
`review-required`, `unassessed`. *Cluster readiness*: always `unknown`.

**Recoverability.** Part of the blast radius: `protected`, `fragile` or `unknown`.

**SARIF.** A standard format for static-analysis results that code-scanning dashboards understand.

**Snapshot.** A JSON file describing nodes and workloads, used for offline scans. See [Offline snapshots](../guides/offline-snapshots.md).

**Target version.** The Kubernetes version you plan to upgrade to (`--target-version`, default 1.35). It scales the severity of node findings.

**Unknown.** The honest answer when something could not be measured. cgroupguard never turns "unknown" into "ready".
