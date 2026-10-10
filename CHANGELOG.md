# Changelog

## 0.1.1 — 2026-10-09

- New short command name: `cgg` runs the same program. The Debian and RPM packages and the verifying installer create the link.
- The container image and Helm chart are signed with the release key, and releases do this automatically. Verify with `cosign verify` (see docs/install.md).
- The release page now carries the notes from this changelog.
- The generated runbook no longer shows stray backslashes in its text.
- Documentation: new guides for every task, a rule catalogue, an environment reference, a glossary and troubleshooting.

## 0.1.0 — 2026-10-09

First public release.

- Node and workload scanning for cgroup v1, hybrid and v2 layouts, with evidence, confidence, blast radius and cited sources; read-only by default.
- Remediation plans: runbook, Terraform (GKE, EKS, AKS) and Ansible; guarded apply and draft pull request/merge request creation.
- In-cluster controller with metrics, Events and a custom resource; Helm chart with least-privilege RBAC.
- Optional read-only assistant with redaction, budgets and an audit log.
- Release archives and packages with a signed checksum list and SBOMs; a signed container image and Helm chart; Apache License 2.0.
