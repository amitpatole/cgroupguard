# Changelog

## 0.1.0 — 2026-10-09

First public release.

- Node and workload scanning for cgroup v1, hybrid and v2 layouts, with evidence, confidence, blast radius and cited sources; read-only by default.
- Remediation plans: runbook, Terraform (GKE, EKS, AKS) and Ansible; guarded apply and draft pull request/merge request creation.
- In-cluster controller with metrics, Events and a custom resource; Helm chart with least-privilege RBAC.
- Optional read-only assistant with redaction, budgets and an audit log.
- Signed release archives, packages, container image and chart; Apache License 2.0.
