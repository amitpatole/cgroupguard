# Permissions

The default scan is read-only and needs only `list`:

- nodes (always); pods, deployments, statefulsets, daemonsets, jobs, cronjobs and, optionally, poddisruptionbudgets (workload scan; `--nodes-only` skips them).

Ready-to-bind read-only roles are in the `deploy/` directory of every release archive (`rbac.yaml`, `workload-rbac.yaml`). Nothing in the default scan reads Secrets, ConfigMaps, logs or exec.

Optional features, off unless you ask:

| Feature | What it needs | Risk |
|---|---|---|
| `--prometheus-url` | network access to your Prometheus; no cluster permission | low |
| `--node-events` | `list` on `events` | low |
| `--kubelet-metrics` | `get` on `nodes/proxy`, which reaches every kubelet endpoint through the API server | **high**: prefer `--prometheus-url`; no ready-made role is shipped |
| `--probe` | create/get/delete pods and get pod logs in one dedicated namespace | medium: apply the shipped guardrail policy first, which denies any pod that is not the exact probe |
| `fix --apply` | patch nodes, evict pods (cordon, drain, uncordon) | disruptive by nature; every step needs a typed confirmation |
| `fix --pr` | a GitHub or GitLab token in an environment variable | writes a branch and opens a draft request in the repository you name |

The probe guardrail policy (`deploy/probe-guardrail-policy.yaml`) and the exact roles are described in [Optional permissions](optional-permissions.md); the Helm chart's `rbac.yaml` lists the controller's permissions one by one.
