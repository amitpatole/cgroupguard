# How it works

**Evidence, not guesses.** A node's cgroup mode is `v1`, `v2`, `hybrid` or `unknown`, and each value comes with the evidence behind it
(a filesystem probe, the kubelet metric, a kubelet Event, a local check). A node's OS image or kernel version is never treated as proof.
If two sources disagree the node is reported `unknown`.

**Workload rules.** Images are matched by repository and tag against versioned rules for runtimes and agents (JVMs, Node.js, .NET, Go,
monitoring and logging agents and more). Each rule is `verified` (every claim is stated by its cited source) or `unverified` (the tool may
only say "review", never pass or fail it). Tags are mutable hints; they are not measurements of the installed binary. See
[Runtime coverage](runtime-coverage.md).

**Findings.** Each carries a severity, the evidence, why it matters, a source link, a confidence, and a **blast radius**.

**Blast radius** is computed from the inventory, not guessed: pods and nodes affected, scope (workload, node, node pool, cluster), the way it
fails (node unavailable, crash, silent degradation, observability loss), how well the workload tolerates disruption (replicas,
PodDisruptionBudget, persistent state) and a criticality hint. When the inventory lacks the data, fields say `unknown` instead of a number.

**Pods are folded into their controllers**, so one Deployment is one finding, not one per pod.

**Readiness values.** `pass` means every assessed Linux node has direct v2 evidence; it is **not** permission to upgrade. `blocked` means a node is
on v1 or hybrid for a target of 1.35 or later. `unknown` means the evidence is missing.

**What it does not do.** It does not run code inside your workloads, read your Secrets or ConfigMaps, inspect image layers, or check
other upgrade prerequisites (kubelet configuration, API deprecations, add-on compatibility). Use it next to tools such as kubent and Pluto.
