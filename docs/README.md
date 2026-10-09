# cgroupguard documentation

![demo](media/demo.gif)

> **Pre-release documentation.** Download locations below become valid when the first release is published.

cgroupguard checks a Kubernetes cluster, or a saved snapshot of one, for what a move from **cgroup v1 to cgroup v2** will break.
From Kubernetes 1.35 the kubelet refuses to start on a cgroup v1 node by default, and cgroup v1 support is planned for removal.
The risk is not only the nodes: workloads and agents that read v1 paths or use old runtimes can break once their node moves to v2.

It is **read-only by default**, it **never certifies** that a cluster can be upgraded, and it says **unknown** when it could not measure something.

| Read | For |
|---|---|
| [Install](install.md) | Every way to install, and how to verify what you downloaded |
| [Quickstart](quickstart.md) | Your first scan in five minutes |
| [How it works](concepts.md) | Evidence, confidence, blast radius, what "unknown" means |
| [Permissions](permissions.md) | The exact access each feature needs |
| [Exit codes and output](output.md) | CI gating, JSON, SARIF, Markdown |
| [Remediation](remediation.md) | `fix --plan`, Terraform and Ansible output, `--apply`, `--pr` |
| [Controller](controller.md) | Run it inside a cluster with metrics |
| [Assistant](assistant.md) | Optional model-written explanations (off unless you ask) |
| [Runtime coverage](runtime-coverage.md) | Which runtimes and agents have rules, and how sure we are |
| [Command reference](cli/cgroupguard.md) | Every command and flag |
| [FAQ](faq.md) | Common questions |
| [Optional permissions](optional-permissions.md) | The risky permissions, and the guardrail for probe pods |
| [Sources](sources.md) | Where each rule and plan fact comes from |
| [Security](security.md) | How to report a problem |
| [Prior art](prior-art.md) | Related tools and what this adds |
