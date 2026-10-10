# Evidence sources

A node's cgroup mode is `v1`, `v2`, `hybrid` or `unknown`. The Kubernetes node API does not report it, so cgroupguard needs a measurement.
It never infers the mode from the operating system image, the kernel version or the Kubernetes version. If a probe disagrees with an earlier
measurement of the same node, the node is reported `unknown`.

Choose the source that fits what you are allowed to run. They can be combined. Prometheus, the node proxy and events are applied in that order and each fills in only nodes that are still `unknown`; a probe that disagrees with an earlier measurement turns the node `unknown`.

| Source | Flag | Needs | Risk | Best for |
|---|---|---|---|---|
| Existing Prometheus | `--prometheus-url` | network access to Prometheus | low | most clusters that already scrape kubelets |
| Kubelet warning events | `--node-events` | `list` on `events` | low | a quick look at v1 nodes, nothing more |
| Node proxy | `--kubelet-metrics` | `get` on `nodes/proxy` | **high** | only when nothing else is possible |
| Probe pods | `--probe` | create pods in one namespace, plus a guardrail policy | medium | clusters without Prometheus |
| This machine | `--local` | nothing | none | a single Linux host, no cluster |

## Prometheus (recommended)

The kubelet exports a gauge named `kubelet_cgroup_version`: the value `1` means cgroup v1 and `2` means cgroup v2. If you already scrape
kubelets, cgroupguard can read it without any extra permission in the cluster.

```console
$ cgroupguard check --context my-cluster --prometheus-url http://prometheus.monitoring:9090
```

Check the metric exists first:

```console
$ curl -s 'http://prometheus.monitoring:9090/api/v1/query?query=kubelet_cgroup_version'
```

Rules cgroupguard applies to what Prometheus returns, so a stale or odd answer never becomes "ready":

- a sample must name exactly one node of the inventory (labels `node`, `nodename` or `instance`), otherwise it is skipped;
- samples older than 5 minutes, values other than 1 and 2, and conflicting duplicates are rejected: the node stays `unknown` and the report's limitations say why;
- a node already measured is not overwritten.

The metric is marked ALPHA by Kubernetes and depends on the kubelet version. It cannot express a hybrid layout, so a hybrid node is never reported by it.
The URL must be `http` or `https` without credentials in it, and redirects are not followed.

## Kubelet warning events

Kubelets on cgroup v1 nodes emit a `CgroupV1` Warning event.

```console
$ cgroupguard check --context my-cluster --node-events
```

Events expire after about an hour, and the **absence** of an event is never taken as evidence of v2. Use this to confirm v1 nodes, not to clear them.

## Probe pods (opt-in)

A probe is a short-lived pod on each node that reads the node's cgroup filesystem type and prints it. It needs permission to create pods, so
cgroupguard requires you to install a **guardrail policy** first. The policy rejects every pod in the probe namespace except one of exactly the probe's shape
(one container, a digest-pinned image, a fixed command, no host namespaces, no service account token, only the read-only cgroup path mounted, unprivileged with all capabilities dropped, running as non-root).

```console
$ kubectl create namespace cgroupguard
$ kubectl label namespace cgroupguard cgroupguard.io/probe-namespace=true
$ kubectl apply -f deploy/probe-guardrail-policy.yaml          # needs ValidatingAdmissionPolicy (Kubernetes 1.30 or later)
$ cgroupguard check --context my-cluster --probe \
      --namespace cgroupguard \
      --probe-image ghcr.io/amitpatole/cgroupguard@sha256:DIGEST_OF_THE_RELEASE_IMAGE
```

The image must be pinned by digest. The exact roles and the policy are explained in [Optional permissions](../optional-permissions.md).

## Node proxy (last resort)

`--kubelet-metrics` reads the same metric through the API server's node proxy. The permission `get` on `nodes/proxy` reaches **every** kubelet endpoint, so
no ready-made role is shipped for it. Prefer Prometheus.

## This machine

```console
$ cgroupguard check --local
$ cgroupguard probe
{"cgroup":"v2","evidence":"statfs: cgroup2fs at /sys/fs/cgroup"}
```

`probe` prints one JSON line. The three results you can get:

```json
{"cgroup":"v2","evidence":"statfs: cgroup2fs at /sys/fs/cgroup"}
{"cgroup":"v1","evidence":"tmpfs root; direct cgroup v1 mounts=true; direct cgroup v2 mounts=false"}
{"cgroup":"hybrid","evidence":"tmpfs root; direct cgroup v1 mounts=true; direct cgroup v2 mounts=true"}
```

`--local` and `probe` work on Linux only. Without any of these, a snapshot you write by hand works too, see [Offline snapshots](offline-snapshots.md).

To check a node by hand: `stat -fc %T /sys/fs/cgroup/` prints `cgroup2fs` on v2 and `tmpfs` on v1.
