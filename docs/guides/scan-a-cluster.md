# Scan a cluster

This guide takes you from "I have a kubeconfig" to a report you can act on. Nothing here changes your cluster.

## Before you start

You need `kubectl` access to the cluster and a way to bind two read-only roles. The scan **lists** nodes and workload metadata. It never reads
Secrets, ConfigMaps, logs or pod exec, and it never runs anything in your workloads.

The roles are in the `deploy/` folder of every release archive:

```console
$ kubectl apply -f deploy/rbac.yaml -f deploy/workload-rbac.yaml
$ kubectl create clusterrolebinding cgroupguard-scan-nodes     --clusterrole=cgroupguard-inventory --user=YOUR_USER
$ kubectl create clusterrolebinding cgroupguard-scan-workloads --clusterrole=cgroupguard-workloads --user=YOUR_USER
```

`cgroupguard-inventory` lets you list nodes, `cgroupguard-workloads` lets you list pods, deployments, statefulsets, daemonsets, jobs, cronjobs and
PodDisruptionBudgets. Without the second one, use `--nodes-only`. Nothing is bound for you: read the files and bind them to your own identity.
Details of every permission are in [Permissions](../permissions.md).

## 1. Choose the cluster and the target version

```console
$ kubectl config get-contexts
$ cgroupguard check --context my-cluster --target-version 1.35
```

- `--context` picks a kubeconfig context. Without it the usual kubeconfig loading rules apply (`KUBECONFIG`, then `~/.kube/config`).
- `--target-version` is the Kubernetes version you plan to upgrade to (default `1.35`). With a target of 1.35 or later a cgroup v1 node is `high`;
  with an earlier target it is a `warning` and the node readiness says `needs-migration`.

## 2. Read the three verdicts

```text
Node cgroup readiness: blocked
Workload assessment: review-required
Cluster upgrade readiness: unknown
```

| Line | Values | Meaning |
|---|---|---|
| Node cgroup readiness | `pass`, `needs-migration`, `blocked`, `unknown`, `not-applicable` | `pass` only when every Linux node has direct evidence of cgroup v2. `blocked`: a node is on v1 or hybrid and the target is 1.35 or later. `unknown`: at least one node is unmeasured. `not-applicable`: no Linux nodes. |
| Workload assessment | `no-known-findings`, `review-required`, `unassessed` | Based on image tags and manifest paths only. `no-known-findings` is not proof the application works on v2. |
| Cluster upgrade readiness | always `unknown` | A scan reads metadata. It cannot prove an upgrade is safe. |

## 3. Deal with "unknown" nodes first

On a first run most nodes show cgroup `unknown`, and each produces a `CG003` warning. The Kubernetes node API simply does not say which cgroup
version a node uses, and the tool refuses to guess from the OS image or kernel version. Pick an evidence source in
[Evidence sources](evidence-sources.md) and run again:

```console
$ cgroupguard check --context my-cluster --prometheus-url http://prometheus.monitoring:9090
```

## 4. Limit the scope when the cluster is big

```console
$ cgroupguard check --context my-cluster --workload-namespace shop        # workloads of one namespace only
$ cgroupguard check --context my-cluster --nodes-only                      # skip the workload inventory entirely
$ cgroupguard check --context my-cluster --timeout 10m                     # the default overall limit is 5 minutes
```

## 5. Keep the result

```console
$ cgroupguard check --context my-cluster --format json     > report.json   # for tools and diffs
$ cgroupguard check --context my-cluster --format markdown > report.md     # for tickets and review
$ cgroupguard check --context my-cluster --format sarif    > report.sarif  # for code-scanning dashboards
```

The same evidence always produces byte-identical output, so reports can be compared with `diff` between runs. See [Output and exit codes](../output.md).

## 6. Decide what to do next

| You see | Next step |
|---|---|
| `CG001` / `CG002` (v1 or hybrid nodes) | Plan the node migration: [managed clusters](plan-managed-clusters.md) or [self-managed nodes](plan-self-managed-nodes.md). |
| `CG101`–`CG110` (workloads) | Update the image or check the vendor's notes, see the [rule catalogue](../reference/rules.md). |
| `CG003` (unknown) | Add evidence, see [Evidence sources](evidence-sources.md). |
| A clean report | It means no *known* problem was found with the evidence you gave it. Other upgrade prerequisites are not assessed. |

Finally, `cgroupguard fix --plan --context my-cluster` writes a runbook for all of the above.
