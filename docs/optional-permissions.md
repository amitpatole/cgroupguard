# Optional permissions and guardrails

The default scan needs only read access (`the `deploy/` directory of the release archive`, `deploy/workload-rbac.yaml`).
Three optional features need more. cgroupguard does **not** ship ready-made roles for the two
risky ones; create them deliberately, in the smallest scope, after reading this page.

## Node events (`--node-events`) — low risk
Needs `list` on `events`. The shipped `cgroupguard-node-events` ClusterRole grants exactly that.
Events expire (about an hour by default): a missing event is never evidence of cgroup v2.

## Kubelet metrics through the API server (`--kubelet-metrics`) — high risk
Needs `get` on `nodes/proxy`. That permission lets the holder reach **every kubelet endpoint**
through the API server, not only `/metrics`, and is a known privilege-escalation path. Prefer
`--prometheus-url` against a Prometheus you already run (no extra cluster permission). If you
still want it, create the role yourself, bind it only to a trusted short-lived identity, and
remove it afterwards.

## Probe pods (`--probe`) — medium risk, guardrail mandatory
The probe creates one short-lived pod per Linux node with a read-only `hostPath` of
`/sys/fs/cgroup`. The identity needs `create`, `get` and `delete` on `pods` and `get` on
`pods/log` in one dedicated namespace. Pod-create rights could otherwise run any workload there,
so **first** apply `deploy/probe-guardrail-policy.yaml` (a ValidatingAdmissionPolicy, GA in
Kubernetes 1.30) and label the namespace:

```bash
kubectl create namespace cgroupguard
kubectl label namespace cgroupguard cgroupguard.io/probe-namespace=true
kubectl apply -f deploy/probe-guardrail-policy.yaml
```

The policy denies every pod in that namespace except one with exactly the probe's shape: a single
container, a digest-pinned image, the fixed command, no environment, no host namespaces, no service
account token, non-root, no privilege escalation, all capabilities dropped, a read-only root
filesystem and one read-only `/sys/fs/cgroup` mount. Only then create a namespaced Role such as:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: cgroupguard-probe, namespace: cgroupguard}
rules:
  - {apiGroups: [""], resources: [pods], verbs: [create, get, delete]}
  - {apiGroups: [""], resources: [pods/log], verbs: [get]}
```

Test the guardrail before trusting it: a pod with another command or any extra privilege in that
namespace must be rejected. Managed platforms may block `hostPath`; that is reported as an error,
never as a pass.
