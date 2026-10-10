# Apply drains yourself, or open a pull request

`cgroupguard fix` always starts from a fresh scan and runs **exactly one mode**: `--plan`, `--apply` or `--pr`. `--plan` is covered in the two migration guides.
This page covers the two modes that act.

## Apply: cordon, drain and uncordon

`--apply` performs only three kinds of Kubernetes action, through the API and one step at a time: **cordon**, **drain** and **uncordon**. It never runs a cloud command,
touches a node's files or reboots anything. Those steps stay in the runbook for you to run.

```console
$ cgroupguard fix --apply --context my-cluster --audit-log apply-$(date +%F).jsonl --dry-run
```

`--dry-run` previews every step with a server-side dry run. Then the real thing:

```console
$ cgroupguard fix --apply --context my-cluster --audit-log apply-$(date +%F).jsonl
```

For each step it describes what it will do and asks for the step's ID:

```text
  type M1.2 to run this step, or press Enter to stop:
```

Pressing Enter stops the run cleanly; completed steps are listed in the audit log.

The safety rules, all enforced by the tool:

| Rule | What it means |
|---|---|
| A live scan is required | `--apply` refuses `--input`: it acts on the cluster you are connected to. |
| A real terminal is required | Confirmations are typed. Without a terminal, only `--dry-run` works. |
| An audit log is mandatory | `--audit-log` must name a **new** file. Every request and action is recorded, hash-chained so edits are detectable. |
| Drains use evictions | PodDisruptionBudgets are respected. There is no `--force`. A drain that is blocked times out and tells you which budgets block it. |
| Cordon first | A node that is not cordoned is never drained, so evicted pods are not rescheduled onto it. |
| Only its own cordons | Uncordon is refused for a node that was not cordoned by cgroupguard (no `cgroupguard.io/migrating` marker). |
| Unmanaged pods stop it | A node running pods without a controller (which a drain would lose) is refused. |
| Only the plan's nodes | A step that names a node outside the plan's groups is refused. |

Check the log afterwards:

```console
$ cgroupguard agent verify-log apply-2026-10-09.jsonl
```

`verify-log` detects any edit, insertion or removal in the file.

## Pull request: let your team review the plan

```console
$ export GITHUB_TOKEN=...                       # a token that can create a branch and a pull request
$ cgroupguard fix --pr --context my-cluster --pr-repo my-org/infrastructure \
      --prometheus-url http://prometheus.monitoring:9090 --audit-log pr-$(date +%F).jsonl --dry-run
```

`--dry-run` shows what would be created and makes **no network access**. For the sample it prints:

```text
DRY RUN, no network access:
github repository acme/infra: create branch cgroupguard/cgroup-v2-plan-6a0be67200 from main, add 4 file(s), open a DRAFT request titled "cgroup v2 remediation plan for ..."
  + cgroupguard-remediation/RUNBOOK.md (24276 bytes)
  + cgroupguard-remediation/eks/workers-al2-nodegroup.yaml (387 bytes)
  + cgroupguard-remediation/gke/pool-legacy-system-config.yaml (44 bytes)
  + cgroupguard-remediation/plan.json (32282 bytes)
```

Without `--dry-run` it asks you to type the repository name, then creates a **new branch** and a **draft** pull request. It never pushes to an existing branch.

| Option | Use |
|---|---|
| `--pr-forge github` or `gitlab` | which service (default `github`) |
| `--pr-repo owner/name` | the repository; for GitLab the full project path |
| `--pr-base main` | the target branch |
| `--pr-branch NAME` | a branch name (default: derived from the content, so the same plan gives the same name) |
| `--pr-base-url URL` | GitHub Enterprise or self-hosted GitLab |
| `--pr-token-env NAME` | the environment variable holding the token (default `GITHUB_TOKEN` or `GITLAB_TOKEN`) |

Everything goes under a `cgroupguard-remediation/` folder. Paths are checked strictly, so a hostile node name cannot write outside it. The token is read from the
environment and never printed or logged.

Add `--emit terraform,ansible` (with `--output-dir`) to include the generated modules and the Ansible project in the request.
