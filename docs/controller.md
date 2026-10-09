# Controller

An optional controller runs the same checks on a schedule and publishes the result as a `CgroupReadiness` resource, Kubernetes Events and
Prometheus metrics.

```bash
helm install cgroupguard oci://ghcr.io/amitpatole/charts/cgroupguard --version VERSION \
    -n cgroupguard-system --create-namespace --set image.repository=ghcr.io/amitpatole/cgroupguard --set image.digest=sha256:...
kubectl apply -f - <<'YAML'
apiVersion: cgroupguard.io/v1alpha1
kind: CgroupReadiness
metadata: {name: default}
spec: {target: "1.35", intervalSeconds: 3600}
YAML
kubectl get cgroupreadinesses
```

- **Read-only toward the cluster.** It lists nodes and workload metadata and writes only the status of its own resources and Events. It has no access to Secrets or ConfigMaps.
- **Evidence sources are chart values**, not resource fields: set `controller.prometheusURL` to measure node cgroup modes (otherwise they stay `unknown`).
- **Metrics:** `cgroupguard_nodes_cgroup_v1`, `cgroupguard_nodes{cgroup}`, `cgroupguard_findings{severity}`, `cgroupguard_last_scan_timestamp_seconds`,
  `cgroupguard_scan_duration_seconds`, `cgroupguard_scan_failures_total`. Counts only; no names. Ingress to the metrics port is denied until you list a
  source in `networkPolicy.metricsAllowFrom`.
- **Uninstall:** `helm uninstall` removes the controller; the CRD stays by Helm's design (`kubectl delete crd cgroupreadinesses.cgroupguard.io` removes it and all resources).
