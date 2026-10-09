## cgroupguard check

Read node inventory; probe nodes only with --probe

```
cgroupguard check [flags]
```

### Options

```
      --context string              Kubeconfig context
  -e, --exit-error                  kubent-style gating: exit 200 on findings at or above --fail-on, else 0 (default mode exits 1 while coverage is incomplete)
      --fail-on string              Lowest severity that counts as a failing finding: high, warning, note or none (default "warning")
      --format string               table, json, markdown, sarif or kubent-compatible (default "table")
  -h, --help                        help for check
      --input string                Offline snapshot JSON (schemaVersion 1)
      --kubeconfig string           Kubeconfig path; defaults to standard client-go loading
      --kubelet-metrics             Read kubelet_cgroup_version through the API-server node proxy (needs nodes/proxy; opt-in)
      --local                       Inspect this Linux mount namespace; no cluster connection
      --namespace string            Existing namespace for probe pods
      --node-events                 Also read kubelet CgroupV1 Warning Events (needs list on events; absence is never evidence of v2)
      --nodes-only                  Skip the live workload inventory and read node information only
  -o, --output string               Alias of --format (kubent-style) (default "table")
      --path string                 Cgroup root for --local (default "/sys/fs/cgroup")
      --probe                       Explicitly authorize temporary hostPath probe pod creation/deletion
      --probe-image string          Trusted cgroupguard image pinned by digest
      --prometheus-url string       Read kubelet_cgroup_version from an existing Prometheus (http/https URL)
  -t, --target-version string       Planned Kubernetes upgrade target; scales finding severity (for example 1.35) (default "1.35")
      --timeout duration            Overall live scan timeout (default 5m0s)
      --workload-namespace string   Only scan workloads in this namespace (default: all)
```

### SEE ALSO

* [cgroupguard](cgroupguard.md)	 - Evidence-based cgroup v1 to v2 readiness checker

