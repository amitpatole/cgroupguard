# Output and exit codes

## Exit codes

| Code | Meaning |
|---|---|
| 0 | A utility command succeeded. With `--exit-error`: nothing at or above `--fail-on` (a note says coverage is still incomplete). |
| 1 | Default `check`: findings at or above `--fail-on`, or coverage is incomplete (always true today). |
| 2 | Bad input, permission or API failure, or a timeout. |
| 200 | `--exit-error` only: findings at or above `--fail-on` (the same convention as kubent's `--exit-error`). |

`--fail-on` is `high`, `warning` (default), `note` or `none`. An exit code of 0 is never an upgrade certification. Pipelines: [CI gating](guides/ci-gating.md).

## Formats

Choose with `--format` or `-o` (they are aliases; give one).

| Format | Use it for |
|---|---|
| `table` (default) | reading in a terminal |
| `json` | scripts, `jq`, storing and diffing |
| `markdown` | tickets and pull requests |
| `sarif` | code-scanning dashboards (SARIF 2.1.0) |
| `kubent-compatible` | tools that already read kubent's JSON |

Reports are **deterministic**: the same evidence always produces byte-identical output.

## JSON report

Top level:

| Field | Type | Meaning |
|---|---|---|
| `schemaVersion` | number | `1` |
| `target` | string | the `--target-version` used |
| `scope` | string | what was scanned |
| `nodeReadiness`, `workloadReadiness`, `clusterReadiness` | string | the three verdicts, see [How it works](concepts.md#readiness-values) |
| `limitations` | array of strings | what the scan did not look at |
| `nodes` | array | one object per node: `name`, `os`, `provider`, `cgroup`, `evidence`, and optional `kubelet`, `pool`, `arch`, `instanceType` |
| `findings` | array | see below |
| `workloads` | array | the containers that were assessed (omitted for node-only scans) |

A finding:

| Field | Meaning |
|---|---|
| `ruleId` | `CG001` and so on, see the [rule catalogue](reference/rules.md) |
| `node` or `resource` | the node, or `namespace/Kind/name/container-type/container` |
| `severity` | `high`, `warning`, `note` |
| `evidence`, `why` | what was observed, and why it matters |
| `confidence` | `high`, `low` or `unknown` |
| `source` | a primary-source URL |
| `blastRadius` | `scope`, `nodesAffected`, `totalNodes`, `podsAffected`, `namespaces`, `nodePools`, `failureMode`, `recoverability {rating, replicas, pdb, stateful}`, `criticality`, `criticalityBasis`, `basis`. See [Read a report](guides/read-a-report.md). |

A complete finding from the sample:

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
    "namespaces": ["obs", "shop"],
    "nodePools": ["userpool1"],
    "failureMode": "node-unavailable",
    "recoverability": { "rating": "fragile", "pdb": "unknown", "stateful": true },
    "criticality": "unknown",
    "criticalityBasis": "not assessed for nodes",
    "basis": "counted"
  },
  "confidence": "high",
  "source": "https://kubernetes.io/docs/concepts/architecture/cgroups/"
}
```

## SARIF

Each finding becomes one SARIF result: `high` maps to level `error` and `warning` to `warning`. The node or workload is a logical location, a stable
fingerprint (`partialFingerprints.cgroupguard/v1`) lets dashboards track the same finding between runs, and the blast radius is in the result's `properties`.
The rules are listed in the tool driver (`CG001`, `CG002`, ...).

## kubent-compatible

A JSON list shaped like kubent's output, for tools that already consume it. It is shape-compatible only: cgroupguard findings are not API deprecations.

```json
{
  "ApiVersion": "CG001",
  "Kind": "Node",
  "Name": "aks-c-1",
  "Namespace": "",
  "ReplaceWith": "Kubernetes 1.35 defaults to rejecting cgroup v1 nodes. Migrate before upgrading; provider policies may disallow temporary overrides.",
  "RuleSet": "cgroupguard",
  "Since": "1.35.0"
}
```

`ApiVersion` carries the cgroupguard rule ID.
