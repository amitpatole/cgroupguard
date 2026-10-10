# Offline snapshots

A snapshot is a small JSON file describing nodes (and optionally workloads). Use it to scan without a cluster connection, to share a scan with someone who cannot
reach the cluster, in tests, and in air-gapped environments. `cgroupguard check --input snapshot.json` produces exactly the same report as a live scan would for that data.

> **Trust.** A snapshot is user-supplied evidence. The report says so in its limitations: it is not authenticated and not checked for freshness.

## The smallest valid snapshot

```json
{
  "schemaVersion": 1,
  "nodes": [
    {
      "name": "n1",
      "os": "linux",
      "provider": "unknown",
      "cgroup": "v2",
      "evidence": "stat -fc %T /sys/fs/cgroup/ printed cgroup2fs"
    }
  ]
}
```

```console
$ cgroupguard check --input snapshot.json
Node cgroup readiness: pass
Workload assessment: unassessed
Cluster upgrade readiness: unknown
...
```

## Node fields

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | The node name |
| `os` | yes | `linux` or `windows` (Windows nodes are not assessed) |
| `provider` | yes | `gke`, `eks`, `aks` or `unknown`; selects the kind of migration plan |
| `cgroup` | yes | `v1`, `v2`, `hybrid` or `unknown` |
| `evidence` | yes | **Non-empty text** saying how you know. A node without evidence is rejected. |
| `kubelet` | no | Kubelet version, for example `v1.34.1` |
| `pool`, `arch`, `instanceType` | no | Node pool, architecture, machine type: used to group nodes and fill in plans |
| `osImage`, `kernel`, `runtime`, `uid` | no | Informational |

## Workload fields

Add a `workloads` array to also assess images and manifest paths. One entry per container:

```json
{
  "schemaVersion": 1,
  "nodes": [ { "name": "n1", "os": "linux", "provider": "unknown", "cgroup": "v2", "evidence": "cgroup2fs" } ],
  "workloads": [
    {
      "namespace": "shop", "kind": "Deployment", "name": "api",
      "container": "app", "containerType": "regular",
      "image": "eclipse-temurin:11.0.15-jre",
      "replicas": 3, "pdb": "present"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `namespace`, `kind`, `name`, `container`, `containerType` | Identify the container (`containerType` is `regular`, `init` or `ephemeral`) |
| `image` | The image reference as written in the manifest |
| `pathSignals` | Allow-listed legacy cgroup v1 path strings found in the manifest (for example `/sys/fs/cgroup/memory/`) |
| `replicas` | Number of replicas, if known |
| `pdb` | `present`, `none` or `unknown` (a PodDisruptionBudget covers it) |
| `stateful` | `true` if it uses persistent state |
| `placements` | Map of node name to number of pods, to compute the blast radius |

Working samples ship in the repository: `examples/mixed-nodes.json` (nodes only), `examples/workloads.json` and `examples/plan-mixed.json` (nodes and workloads).

## Validation

The file must be a regular file of at most 16 MiB containing exactly one JSON document. Typos are rejected, not ignored:

```console
$ cgroupguard check --input bad.json
Error: json: unknown field "colour"
Error: node n1: invalid cgroup value "v3"
Error: node n1: evidence is required
Error: unsupported snapshot schemaVersion 2
Error: no nodes found; empty scans cannot pass
```

(One error is printed per run, with exit code 2.) An empty scan can never pass.

`--nodes-only` applies to live scans only; with `--input` simply leave out the `workloads` array.

## Make a snapshot from a live cluster

Write your own from `kubectl get nodes -o json` and your measurements, or take the JSON report of a live scan and keep it as your record:
`cgroupguard check --context my-cluster --format json > report.json`. A report is not a snapshot file, but it contains the same nodes with their evidence,
so you can copy the `nodes` and `workloads` arrays out of it.
