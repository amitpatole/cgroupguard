# Troubleshooting

## Every node shows cgroup `unknown`

Expected on a first run: the Kubernetes API does not report the cgroup version, and the tool will not guess from the OS image or kernel. Add evidence.
See [Evidence sources](guides/evidence-sources.md). The quickest is `--prometheus-url` if you already scrape kubelets.

If you gave `--prometheus-url` and nodes are still `unknown`, read the **Limitations** at the end of the report. Typical reasons are in the table.

| Limitation text | Cause | Fix |
|---|---|---|
| `Prometheus has no kubelet_cgroup_version sample for node X` | Prometheus does not scrape that kubelet, or the node label does not match the node name | Check `curl '<prometheus>/api/v1/query?query=kubelet_cgroup_version'`; the sample needs a `node`, `nodename` or `instance` label equal to the node name |
| `... was stale, malformed or conflicting; left unknown` | The sample is older than 5 minutes, has a value other than 1 or 2, or two samples disagree | Fix the scrape; check for duplicate targets |
| `Kubelet metrics unavailable for node X` | `--kubelet-metrics` is on but `nodes/proxy` is not permitted or the kubelet does not expose the metric | Prefer Prometheus; see [Permissions](permissions.md) |
| Events could not be read | `--node-events` without `list` on events | Bind `cgroupguard-node-events` from `deploy/rbac.yaml` |

The metric is an ALPHA kubelet metric: older kubelets may not export it.

## "forbidden" errors from the API

The Kubernetes message names the missing permission, for example the resource and verb. The default scan needs `list` on nodes and, for workloads, on pods, deployments,
statefulsets, daemonsets, jobs, cronjobs and poddisruptionbudgets. Bind the two roles in `deploy/` ([Scan a cluster](guides/scan-a-cluster.md)) or use `--nodes-only`
to scan nodes only. The tool exits with code 2 and does not guess.

## Command-line errors, and what they mean

| Message | Meaning |
|---|---|
| `format must be table, json, markdown, sarif or kubent-compatible` | An unsupported `--format` / `-o` |
| `--fail-on must be high, warning, note or none` | An unsupported severity |
| `--output and --format disagree; use one` | `-o` and `--format` are aliases; give one |
| `if any flags in the group [input local] are set none of the others can be` | `--input` and `--local` cannot be combined |
| `metrics options require a live cluster scan` | `--prometheus-url`, `--kubelet-metrics`, `--node-events` need a cluster, not a snapshot |
| `--nodes-only applies only to live cluster scans` | With `--input`, leave out the `workloads` array instead |
| `invalid --target-version "banana" (use a Kubernetes version such as 1.35)` | Use a version like `1.35` or `v1.35.0` |
| `fix needs exactly one of --plan, --apply or --pr` | Choose one mode per run |
| `--emit requires --output-dir (generated files are never printed to the terminal)` | Give a directory |
| `RUNBOOK.md already exists in <dir> (use --overwrite to replace)` | The tool never overwrites silently |
| `--apply needs a live cluster scan (it acts on the cluster you are connected to)` | Not with `--input` |
| `--apply needs an interactive terminal to confirm each step; use --dry-run to preview without one` | Confirmations are typed by a person |
| `--audit-log is required with --apply and --pr: every action must be recorded` | Name a new file |
| `local cgroup inspection requires Linux; use check --input or a Kubernetes context` | `--local` and `probe` work on Linux only |
| `no nodes found; empty scans cannot pass` | The snapshot or cluster returned no nodes |
| `node X: evidence is required` / `invalid cgroup value` | Fix the snapshot, see [Offline snapshots](guides/offline-snapshots.md) |

## The check exits `1` even when it printed no findings

That is the documented behavior of the default mode: coverage is always incomplete, so it never exits `0`. Use `--exit-error` for gating, see [CI gating](guides/ci-gating.md).

## A timeout

The default overall limit for a live scan is 5 minutes. Raise it with `--timeout 15m` on big clusters, or narrow the scan with `--workload-namespace` or `--nodes-only`.

## The workload findings look wrong

- Image-tag findings are heuristics (`confidence: low`). A custom image tagged `latest` or a private mirror name may not match any rule, which gives `CG104` ("unrecognized"), not "fine".
- Rules match the repository path. A mirror such as `registry.example.com/eclipse-temurin` is treated as unrecognized on purpose: a lookalike name must not inherit a verified rule.
- Verify the real runtime inside the container, then tell us if a rule is wrong: open an issue with the image reference and the evidence.

## A generated plan does not fit my platform

Plans for GKE, EKS and AKS are generated from the providers' documentation and have not been run against real managed clusters by the project. Read the `RUNBOOK.md`
sources, compare with your provider's current documentation, and test in a non-production cluster. Values the tool could not measure are written as `REPLACE_WITH_*`.

## `terraform` or `ansible` validation fails with `--validate`

`--validate` downloads provider plugins and Ansible collections and runs `terraform`, `tflint`, `trivy`, `yamllint`, `ansible-playbook --syntax-check` and `ansible-lint`. A tool that
is not installed is reported as *SKIPPED*, which counts as "not validated", never as a pass. Install the missing tool or use `--no-validate` and validate in your own pipeline.

## The controller shows `unknown` for everything

Set `controller.prometheusURL` in the Helm values (or run it with `--prometheus-url`). Without an evidence source the controller reads node metadata only.
See [Controller and monitoring](guides/controller-and-monitoring.md). If Prometheus cannot reach the controller's metrics port, list the source in `networkPolicy.metricsAllowFrom`:
the chart denies ingress until you do.

## Still stuck

Open an issue at https://github.com/amitpatole/cgroupguard/issues with the command you ran, the version (`cgroupguard --version`) and the **Limitations** section. Remove
anything private first. Do not post kubeconfigs, tokens or real cluster reports. Security problems: [Security](security.md).
