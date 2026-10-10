# Run it in the cluster and alert on it

The controller runs the same scan on a schedule and publishes the result three ways: a `CgroupReadiness` resource you can `kubectl get`, Kubernetes Events, and
Prometheus metrics. It is **read-only toward your cluster**: it lists nodes and workload metadata and writes only the status of its own resources and its Events.
It has no access to Secrets or ConfigMaps.

## 1. Install with Helm

The chart is published as an OCI artifact. The image must be provided: there is no default registry in the chart.

```console
$ helm install cgroupguard oci://ghcr.io/amitpatole/charts/cgroupguard --version 0.1.0 \
    --namespace cgroupguard-system --create-namespace \
    --set image.repository=ghcr.io/amitpatole/cgroupguard \
    --set image.digest=sha256:DIGEST_OF_THE_RELEASE_IMAGE \
    --set controller.prometheusURL=http://prometheus.monitoring:9090 \
    --set 'networkPolicy.metricsAllowFrom[0].namespaceSelector.matchLabels.kubernetes\.io/metadata\.name=monitoring'
```

Pin the image by `digest` in production. Every digest is listed on the release page, and the signature can be checked as shown in [Install](../install.md).

The most useful settings (`values.yaml` documents all of them):

| Value | Default | What it does |
|---|---|---|
| `controller.prometheusURL` | empty | Your Prometheus. The controller reads `kubelet_cgroup_version` from it, so node modes are **measured**. Without it, nodes stay `unknown`. |
| `controller.nodeEvents` | `false` | Also read kubelet `CgroupV1` Events (needs `list` on events cluster-wide). |
| `controller.workloads` | `true` | Scan workload metadata. Turn off for a node-only scan. |
| `defaultReadiness.enabled` | `false` | Create one `CgroupReadiness` named `default` for you. |
| `metrics.serviceMonitor.enabled` | `false` | Create a `ServiceMonitor` (needs the Prometheus Operator CRDs). |
| `networkPolicy.metricsAllowFrom` | `[]` | Who may reach the metrics port. **Ingress is denied until you list a source.** |
| `podDisruptionBudget.enabled` | `false` | Create a PodDisruptionBudget (useful with `replicaCount` above 1). |
| `replicaCount`, `leaderElection` | `1`, `true` | With several replicas, only the elected leader scans. |

The pod runs as a non-root user with a read-only root filesystem, all capabilities dropped and the default seccomp profile. The chart deliberately has **no switch**
for reading kubelet metrics through the API-server node proxy, because that permission reaches every kubelet endpoint. Use Prometheus.

## 2. Ask for a scan

A `CgroupReadiness` is a cluster-scoped resource (short name `cgr`).

```console
$ kubectl apply -f - <<'YAML'
apiVersion: cgroupguard.io/v1alpha1
kind: CgroupReadiness
metadata:
  name: default
spec:
  target: "1.35"
  intervalSeconds: 3600
YAML
$ kubectl get cgr
NAME      TARGET   NODEREADINESS   V1   UNKNOWN   HIGH   LASTSCAN
default   1.35     blocked         3    0         3      2026-10-09T10:00:00Z
```

(The numbers above are illustrative; the columns are real.) The spec fields:

| Field | Meaning |
|---|---|
| `target` | The Kubernetes version you plan to upgrade to. |
| `intervalSeconds` | Time between scans (default 3600). |
| `includeWorkloads` | Also scan workload metadata (needs the workload read permissions). |
| `workloadNamespace` | Restrict the workload scan to one namespace. |
| `statusDetail` | `summary` shows counts only; `preview` adds up to 20 finding locations (names of workloads and nodes). |

Evidence sources are **chart values, not resource fields**, so nobody who can create a `CgroupReadiness` can make the controller read anything new.

## 3. Read the status

```console
$ kubectl get cgr default -o jsonpath='{.status.nodes}{"\n"}{.status.findings}{"\n"}'
```

`status` contains `nodeReadiness`, `workloadReadiness`, `clusterReadiness` (always `unknown`), `nodes` (`total`, `cgroupV1`, `cgroupV2`, `hybrid`, `unknown`),
`findings` (`high`, `warning`, `note`), `limitations`, `lastScanTime` and `conditions`. With `statusDetail: preview`, `findingsPreview` lists up to 20 findings.

## 4. Metrics

The controller serves Prometheus metrics on port 8080. They contain counts only, never node or workload names. The label `name` is the `CgroupReadiness` name.

| Metric | Labels | Meaning |
|---|---|---|
| `cgroupguard_nodes_cgroup_v1` | `name` | Linux nodes measured on cgroup v1 |
| `cgroupguard_nodes` | `name`, `cgroup` | Nodes by mode (`v1`, `v2`, `hybrid`, `unknown`) |
| `cgroupguard_findings` | `name`, `severity` | Findings by severity from the last successful scan |
| `cgroupguard_last_scan_timestamp_seconds` | `name` | Unix time of the last successful scan |
| `cgroupguard_scan_duration_seconds` | histogram | How long scans take |
| `cgroupguard_scan_failures_total` | `name` | Scans that failed |

Health endpoints `/healthz` and `/readyz` are on port 8081.

## 5. Alerts

```yaml
groups:
  - name: cgroupguard
    rules:
      - alert: CgroupV1NodesRemain
        expr: sum by (name) (cgroupguard_nodes_cgroup_v1) > 0
        for: 1h
        labels: {severity: warning}
        annotations:
          summary: "{{ $value }} nodes are still on cgroup v1"
      - alert: CgroupNodesUnmeasured
        expr: cgroupguard_nodes{cgroup="unknown"} > 0
        for: 6h
        labels: {severity: info}
        annotations:
          summary: "Some nodes have no cgroup evidence; check controller.prometheusURL"
      - alert: CgroupGuardScanStale
        expr: time() - cgroupguard_last_scan_timestamp_seconds > 3 * 3600
        for: 15m
        labels: {severity: warning}
        annotations:
          summary: "No successful cgroupguard scan for three hours"
      - alert: CgroupGuardScanFailing
        expr: increase(cgroupguard_scan_failures_total[1h]) > 0
        labels: {severity: warning}
        annotations:
          summary: "cgroupguard scans are failing"
```

A stale-scan alert matters: a dashboard that says "0 nodes on v1" because the scanner stopped is worse than no dashboard.

## 6. Events

The controller raises an Event on the `CgroupReadiness` object only when something changed since the previous status, so a quiet cluster stays quiet. They show in `kubectl describe cgr default`.

| Reason | Type | When |
|---|---|---|
| `NodesOnCgroupV1` | Warning | On the first scan, and whenever the number of cgroup v1 or hybrid nodes changes while some remain |
| `NoNodesOnCgroupV1` | Normal | No measured v1 or hybrid nodes remain (nodes with an unknown mode may still exist; the message says how many) |
| `ScanCompleted` | Normal | The first successful scan, when there is nothing on v1 to report |

## 7. Upgrade and uninstall

```console
$ helm upgrade cgroupguard oci://ghcr.io/amitpatole/charts/cgroupguard --version NEW_VERSION -n cgroupguard-system --reuse-values
$ helm uninstall cgroupguard -n cgroupguard-system
$ kubectl delete crd cgroupreadinesses.cgroupguard.io       # optional: removes the CRD and every CgroupReadiness
```

Helm keeps the CRD on uninstall by design, so you do not lose your resources by accident.
