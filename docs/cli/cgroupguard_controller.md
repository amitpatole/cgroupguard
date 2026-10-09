## cgroupguard controller

Run the optional in-cluster controller (CgroupReadiness resources, metrics, Events)

### Synopsis

Runs inside a cluster. It reads nodes and workload metadata, writes only the status of CgroupReadiness resources and Events, and serves Prometheus metrics. Evidence sources beyond the inventory (--prometheus-url, --node-events, --kubelet-metrics) are chosen here by the operator and cannot be selected through a custom resource.

```
cgroupguard controller [flags]
```

### Options

```
      --health-probe-bind-address string   Address of the /healthz and /readyz endpoints (default ":8081")
  -h, --help                               help for controller
      --kubelet-metrics                    Read kubelet metrics through the API-server node proxy (needs nodes/proxy; high risk)
      --leader-elect                       Use leader election so only one replica scans (default true)
      --leader-election-namespace string   Namespace for the leader election lease (default: the controller's own)
      --metrics-bind-address string        Address of the Prometheus metrics endpoint (default ":8080")
      --node-events                        Also read kubelet CgroupV1 Events (needs list on events)
      --prometheus-url string              Read kubelet_cgroup_version from this Prometheus (operator-controlled)
```

### SEE ALSO

* [cgroupguard](cgroupguard.md)	 - Evidence-based cgroup v1 to v2 readiness checker

