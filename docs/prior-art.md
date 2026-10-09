# Prior art

Surveyed on 2026-10-08 with web searches; the survey is limited and may have missed tools. Please tell us about any we missed.

- **Kubernetes project.** The cgroup v2 page and the v1.35 release notes describe the change; the `kubelet_cgroup_version` metric and the `CgroupV1` Event
  let operators see which nodes are on v1. KEP-5573 describes removal of cgroup v1 support no earlier than 1.38. These are signals, not a scanner.
- **Migration guides.** Chkk's "cgroup v1 to v2 Migration in Kubernetes" explains the change and checks a node by hand with `stat -fc %T /sys/fs/cgroup/`; it recommends
  no scanning tool. Cloud providers publish their own migration pages (GKE, EKS, AKS).
- **API-deprecation tools.** kubent, Pluto and Kubepug find removed or deprecated Kubernetes APIs. They do not look at cgroups. cgroupguard borrows kubent's
  command-line conventions (`--exit-error`, `-t/--target-version`, `-o`) and is meant to run next to them in the same CI stage.
- **What we did not find.** An open-source tool that inventories nodes, analyses workloads and agents for cgroup v2 risk, computes blast radius, and generates
  reviewable remediation for several platforms. We did not re-verify AWS's EKS upgrade-check skill mentioned in earlier notes.
