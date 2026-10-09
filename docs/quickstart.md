# Quickstart

```bash
cgroupguard check --context YOUR_CONTEXT              # nodes + workload metadata, read-only
cgroupguard check --context YOUR_CONTEXT --format json > report.json
cgroupguard check --input snapshot.json               # offline, from a saved snapshot
cgroupguard check --local                             # this Linux machine's cgroup mode
```

1. **Read the verdict lines.** `nodeReadiness`, `workloadReadiness` and `clusterReadiness`. Cluster readiness is **always** `unknown`:
   image tags and metadata cannot prove an application works on cgroup v2.
2. **Unknown nodes are normal at first.** The Kubernetes node API does not show the cgroup mode. Add evidence:
   - `--prometheus-url http://prometheus:9090` reads the kubelet's `kubelet_cgroup_version` metric from a Prometheus you already run (no extra cluster permission);
   - `--node-events` reads the kubelet's `CgroupV1` warning Events (they expire after about an hour);
   - `--kubelet-metrics` reads the metric through the API server's node proxy (needs a powerful permission; see [Permissions](permissions.md));
   - `--probe` runs a short-lived pod per node (needs a guardrail policy; see [Permissions](permissions.md)).
3. **Pick a target.** `--target-version 1.35` (default) makes cgroup v1 nodes `high`; an earlier target reports them as `needs-migration`.
4. **Gate CI.** `--exit-error --fail-on warning` exits 200 on findings; see [Exit codes and output](output.md).
5. **Plan the fix.** `cgroupguard fix --plan --context YOUR_CONTEXT` writes a runbook with verification and rollback for every step; see [Remediation](remediation.md).

To check a single node by hand: `stat -fc %T /sys/fs/cgroup/` prints `cgroup2fs` for v2 and `tmpfs` for v1.
