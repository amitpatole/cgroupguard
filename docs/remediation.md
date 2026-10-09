# Remediation

`cgroupguard fix` always starts from a fresh scan. Exactly one mode per run:

- **`--plan`**: writes a runbook (`RUNBOOK.md`) and `plan.json`. Every step that disrupts anything has an approval marker, a verification and a rollback.
  Groups of nodes are ordered by measured blast radius, smallest first, so the first group is your canary. The temporary
  `failCgroupV1: false` step is included only when you pass `--stopgap-until DATE`.
- **`--plan --emit terraform,ansible --output-dir DIR`**: also generates files. Terraform modules (GKE, EKS, AKS) add a replacement node
  pool or group on a cgroup v2 image and contain no credentials. The Ansible project (self-managed nodes) migrates one node at a time,
  canary first, stops on the first failure, can be dry-run with `--check --diff`, and ships a rollback playbook.
  Add `--validate` to check the files with terraform, tflint, trivy, yamllint and ansible-lint before anything is written (this downloads and
  runs provider plugins and Ansible collections), or `--no-validate` if you will validate them yourself.
- **`--apply`**: performs only cordon, drain and uncordon through the Kubernetes API, one step at a time, each confirmed by typing its ID.
  Drains use evictions, respect PodDisruptionBudgets and never force. `--dry-run` previews with server-side dry-run. An audit log is required.
- **`--pr`**: opens a **draft** pull or merge request containing the plan and generated files, after you type the repository name.

Cloud-provider commands, node-local configuration and reboots are never executed by cgroupguard; they are written for you to review and run.
Provider facts used by the plans are listed with their sources in [Sources](sources.md). Plans are generated from documentation, not applied to
managed clusters by the project's own tests: review them against your platform before use.
