# Quickstart

This takes five minutes and needs **no cluster**. It uses a small sample snapshot, `examples/plan-mixed.json`, that ships in the source
repository (clone it, or copy the file from the repository page). Every output below is real output of the tool.

## 1. Check the install

```console
$ cgroupguard --version
cgroupguard version 0.1.1
```

## 2. Run a check on the sample

```console
$ cgroupguard check --input examples/plan-mixed.json
Node cgroup readiness: blocked
Workload assessment: review-required
Cluster upgrade readiness: unknown

NODE       PROVIDER  CGROUP   KUBELET
aks-c-1    aks       v1       
eks-b-1    eks       v1       
gke-a-1    gke       v1       
gke-a-2    gke       v1       
modern-1   unknown   v2       
mystery-1  unknown   unknown  
onprem-1   unknown   hybrid   
...
```

The first three lines are the verdicts:

- **Node cgroup readiness: `blocked`** means at least one node is on cgroup v1 or a hybrid layout and the target version is 1.35 or later.
- **Workload assessment: `review-required`** means some workloads need a human look (an old runtime, or an image the tool has no rule for).
- **Cluster upgrade readiness: `unknown`.** This line is *always* `unknown`. A scan reads metadata, not your applications, so it cannot prove an upgrade is safe.

Then comes one row per node, and then the findings.

## 3. Read one finding

```text

CG001 [high] aks-c-1
  Evidence: EXAMPLE ONLY: probe
  Why: Kubernetes 1.35 defaults to rejecting cgroup v1 nodes. Migrate before upgrading; provider policies may disallow temporary overrides.
  Blast radius: scope=node; 2 pods; 1 of 8 nodes; failure=node-unavailable; recoverability=fragile (pdb=unknown, stateful=true); criticality=unknown (not assessed for nodes); basis=counted
  Source: https://kubernetes.io/docs/concepts/architecture/cgroups/
```

| Part | Meaning |
|---|---|
| `CG001` | the rule ID; every ID is explained in the [rule catalogue](reference/rules.md) |
| `[high]` | severity: `high`, `warning` or `note` |
| `aks-c-1` | the node (for workloads you see `namespace/Kind/name/container-type/container`) |
| Evidence | what was observed. In the sample files it is labelled `EXAMPLE ONLY` |
| Why | why this matters for the target version |
| Blast radius | how much is affected and how well it would cope, see [Read a report](guides/read-a-report.md) |
| Source | the primary document the rule is based on |

A workload finding looks like this:

```text
CG101 [warning] shop/Deployment/api/regular/app
  Evidence: Image-tag heuristic for hotspot (runtime, verified rule): eclipse-temurin:11.0.15-jre
  Why: Image tag suggests a version below the documented cgroup v2 support threshold 11.0.16; verify the actual binary and vendor backports.
  Blast radius: scope=workload; 3 pods; 3 of 8 nodes; failure=silent-degradation; recoverability=protected (pdb=present, stateful=false); criticality=workload (default; no criticality signal collected); basis=counted
  Source: https://kubernetes.io/docs/concepts/architecture/cgroups/
```

## 4. Try the other things it can do

```console
$ cgroupguard check --input examples/plan-mixed.json --format json > report.json      # machine-readable
$ cgroupguard check --input examples/plan-mixed.json --format markdown > report.md    # paste into a ticket
$ cgroupguard check --input examples/plan-mixed.json --target-version 1.34            # an earlier target is gentler
Node cgroup readiness: needs-migration
Workload assessment: review-required
Cluster upgrade readiness: unknown
...
$ cgroupguard check --input examples/plan-mixed.json --exit-error; echo "exit code: $?"
exit code: 200
$ cgroupguard fix --plan --input examples/plan-mixed.json --output-dir ./plan          # write a runbook; changes nothing
Wrote 4 file(s) to plan. Nothing was executed; read RUNBOOK.md first.
```

`--exit-error` makes the command exit with **200** when it finds something at or above `--fail-on` (default `warning`) and **0** when it does not,
which is how you gate a pipeline. See [CI gating](guides/ci-gating.md).

## 5. Point it at a real cluster

```console
$ cgroupguard check --context my-cluster
```

You will probably see many nodes with cgroup `unknown`. That is normal: the Kubernetes API does not say which cgroup version a node uses.
[Evidence sources](guides/evidence-sources.md) shows the four ways to measure it.

To check one machine by hand: `stat -fc %T /sys/fs/cgroup/` prints `cgroup2fs` for v2 and `tmpfs` for v1.
