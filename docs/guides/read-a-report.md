# Read a report

Every finding answers six questions: what, where, how sure, why, how bad, and where it comes from. Here is one finding in the JSON format
(`--format json`), from the sample snapshot:

```json
{
  "ruleId": "CG001",
  "node": "aks-c-1",
  "severity": "high",
  "evidence": "EXAMPLE ONLY: probe",
  "why": "Kubernetes 1.35 defaults to rejecting cgroup v1 nodes. Migrate before upgrading; provider policies may disallow temporary overrides.",
  "blastRadius": {
    "scope": "node",
    "nodesAffected": 1,
    "totalNodes": 8,
    "podsAffected": 2,
    "namespaces": [
      "obs",
      "shop"
    ],
    "nodePools": [
      "userpool1"
    ],
    "failureMode": "node-unavailable",
    "recoverability": {
      "rating": "fragile",
      "pdb": "unknown",
      "stateful": true
    },
    "criticality": "unknown",
    "criticalityBasis": "not assessed for nodes",
    "basis": "counted"
  },
  "confidence": "high",
  "source": "https://kubernetes.io/docs/concepts/architecture/cgroups/"
}
```

## The fields

| Field | Meaning |
|---|---|
| `ruleId` | `CG001` and so on. Each is explained in the [rule catalogue](../reference/rules.md). |
| `node` / `resource` | A node finding names the node. A workload finding names `namespace/Kind/name/container-type/container`. |
| `severity` | `high`: blocks the target version. `warning`: likely trouble or needs review. `note`: informational or meets the documented baseline. |
| `evidence` | What was actually observed, in plain words. For snapshots you wrote yourself it is your text. |
| `why` | Why it matters for the chosen `--target-version`. |
| `confidence` | `high`: a direct measurement or an exact text match. `low`: a heuristic, such as an image tag. `unknown`: nothing could be established. |
| `source` | The primary document the rule rests on. |
| `blastRadius` | How much is affected and how well it would cope, below. |

## Blast radius

The blast radius is **counted from the inventory**, not guessed. When the inventory does not contain the data, the field says `unknown`; it never
shows a made-up number.

| Field | Meaning |
|---|---|
| `scope` | `workload`, `node`, `nodePool` or `cluster`: the largest thing this finding can affect (a pool when all affected nodes belong to one known pool). |
| `nodesAffected` / `totalNodes` | Nodes where the affected pods run, out of all nodes. |
| `podsAffected` | Pods behind the finding. Pods are folded into their controller, so a Deployment is one finding, not one per pod. |
| `namespaces`, `nodePools` | Where it lives. |
| `failureMode` | `node-unavailable` (the kubelet will not start), `crash`, `silent-degradation` (it runs but behaves differently, for example wrong memory limits), `observability-loss` (an agent stops reporting), or `unknown`. |
| `recoverability.rating` | `protected`: a PodDisruptionBudget covers it and the replica count is known. `fragile`: a single replica, a bare Pod, a DaemonSet, persistent state, or no PodDisruptionBudget. `unknown`: not enough data, and always for Jobs and CronJobs. |
| `recoverability.pdb`, `.stateful` | Whether a PodDisruptionBudget covers it (`present`, `none` or `unknown`), and whether it uses persistent volumes or is a StatefulSet. |
| `criticality`, `criticalityBasis` | A hint, with its reason: `system` (a namespace such as `kube-system`), `node-wide` (a DaemonSet), `workload` (the default; no importance signal is collected) or `unknown`. |
| `basis` | `counted`, or `unknown` when the numbers could not be derived. |

Use it to order the work: fix the findings with `fragile` recoverability and a large `podsAffected` first, and start migrating nodes where
`podsAffected` is small and `recoverability` is `protected`. The generated plan does this ordering for you.

## Limitations

The report ends with a list of `limitations`. Read them. They state what the scan did not look at: for example that workload analysis uses
image tags and manifest paths only, that kubelet configuration is not assessed, or that an offline snapshot is unauthenticated evidence.
A limitation is not an error, but it is the reason the cluster line says `unknown`.

## A workload finding

```text
CG101 [warning] shop/Deployment/api/regular/app
  Evidence: Image-tag heuristic for hotspot (runtime, verified rule): eclipse-temurin:11.0.15-jre
  Why: Image tag suggests a version below the documented cgroup v2 support threshold 11.0.16; verify the actual binary and vendor backports.
  Blast radius: scope=workload; 3 pods; 3 of 8 nodes; failure=silent-degradation; recoverability=protected (pdb=present, stateful=false); ...
```

`confidence` is `low` here: an image **tag** says what the image claims to be, not what is inside it. Treat the finding as "go and verify
this", not as proof. A JVM below the threshold may not detect the container's limits on cgroup v2.
