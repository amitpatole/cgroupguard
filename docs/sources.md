# Sources

Every rule threshold and plan step is tied to a primary source. Claims we could not verify from a primary source are not encoded as pass or fail.
Checked on 2026-10-08; quotes may be lightly paraphrased.

## Runtime and detection facts

| Claim | Source |
|---|---|
| Documented cgroup v2 minimums: OpenJDK 8u372/11.0.16/15+, IBM Semeru Runtimes 8.0.382.0/11.0.20.0/17.0.8.0, IBM Java 8.0.8.6, automaxprocs v1.5.1, Node.js v20.3.0 (v18 not reliable); third-party agents must be updated | https://kubernetes.io/docs/concepts/architecture/cgroups/ (read 2026-10-08) |
| Go 1.25 sets GOMAXPROCS from the cgroup CPU bandwidth limit and updates it periodically | https://go.dev/doc/go1.25 |
| libuv 1.45.0 (2023-05-19) taught uv_get_constrained_memory() cgroup v2 | https://raw.githubusercontent.com/libuv/libuv/v1.x/ChangeLog |
| `kubelet_cgroup_version` is an ALPHA gauge, "cgroup version on the hosts" (observed value 2 on a v2 kind node, Kubernetes 1.37.0). Value 1 for v1 is read from kubelet_linux.go (switch on 1 and 2), not yet observed on a live v1 node | https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/metrics/metrics.go and live kubelet |
| kubent: `-o text\|json\|csv`, `-t/--target-version`, `--exit-error` exits 200 on findings | https://github.com/doitintl/kube-no-trouble |
| Python os.cpu_count ignores cgroup quotas (discussion) | https://github.com/python/cpython/issues/109595 |
| Rust available_parallelism handles cgroup limits | https://doc.rust-lang.org/stable/std/thread/fn.available_parallelism.html |
| Unverified (community reports only, no threshold encoded): BEAM quota handling; monitoring-agent minimum versions; vendor JVM backport timing | see runtime-coverage.md |
| Zulu 8.70.0.23 (8u372-b05) lists JDK-8230305 "Cgroups v2: Container awareness" | https://docs.azul.com/core/release/april-2023/release-notes |
| Liberica 11.0.16+8 lists JDK-8230305 and JDK-8231111 | https://docs.bell-sw.com/liberica-jdk/11.0.16b8/general/release-notes/ |
| Fluent Bit 2.2: "adds support cgroups v2 for Docker containers" | https://fluentbit.io/blog/2023/11/16/meet-fluent-bit-v2.2/ |
| Beats 7.15.0: "Add cgroups V2 support" (Affecting all Beats) | https://www.elastic.co/guide/en/beats/libbeat/7.15/release-notes-7.15.0.html |
| .NET cgroup v2 support milestone 5.0.0 | https://github.com/dotnet/runtime/issues/30337 |
| kubelet sets kubelet_cgroup_version from the node config version (1 = v1, 2 = v2) and emits a Warning Event reason `CgroupV1` on v1 | https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_linux.go |
| kubent result fields (Name, Namespace, Kind, ApiVersion, RuleSet, ReplaceWith, Since) | https://github.com/doitintl/kube-no-trouble/blob/master/pkg/judge/judge.go |
| Pluto exit codes: 2 deprecated, 3 removed, 4 deprecated without replacement (default behavior) | pluto 5.24.4 `detect-all-in-cluster --help` |

## Platform and remediation facts

| Provider | Claim | Source |
|---|---|---|
| GKE | "For versions earlier than 1.26, cgroupv1 was the default for nodes. For versions 1.26 or later, cgroupv2 is the default for new nodes." Autopilot clusters created with 1.25 or earlier run cgroupv1 until migrated. "With minor version 1.35, GKE removes support for cgroupv1." | https://docs.cloud.google.com/kubernetes-engine/docs/how-to/migrate-cgroupv2 |
| GKE | Autopilot/auto-provisioned: `gcloud container clusters update CLUSTER_NAME --autoprovisioning-cgroup-mode=v2`; check with `gcloud container clusters describe CLUSTER_NAME --format='value(nodePools[0].config.effectiveCgroupMode)'`; the update uses the surge upgrade strategy and ignores maintenance policies | same page |
| GKE | Standard node pools: `gcloud container node-pools update POOL_NAME --cluster=CLUSTER_NAME --location=LOCATION --system-config-from-file=SYSTEM_CONFIG_PATH` and the same flag on `node-pools create`; YAML `linuxConfig: cgroupMode: 'CGROUP_MODE_V2'`; "This change requires re-creating the nodes, which can cause disruption to your running workloads."; Terraform `linux_node_config { cgroup_mode = "CGROUP_MODE_V2" }` | https://docs.cloud.google.com/kubernetes-engine/docs/how-to/node-system-config |
| GKE | Surge/blue-green settings: `gcloud container node-pools update NODE_POOL_NAME --cluster=CLUSTER_NAME --max-surge-upgrade=N --max-unavailable-upgrade=N`; `--enable-blue-green-upgrade --standard-rollout-policy=batch-node-count=2,batch-soak-duration=10s --node-pool-soak-duration=600s` | https://docs.cloud.google.com/kubernetes-engine/docs/how-to/node-pool-upgrade-strategies |
| EKS | AL2 is cgroup v1; "we recommend migrating to AL2023 or Bottlerocket, which enable cgroupv2 by default"; AL2023 can be switched back to v1 with `grubby ... systemd.unified_cgroup_hierarchy=0` | https://docs.aws.amazon.com/eks/latest/userguide/eks-ami-deprecation-faqs.html |
| EKS | EKS stopped publishing AL2 AMIs on November 26, 2025; Kubernetes 1.32 is the last version with AL2 AMIs | https://docs.aws.amazon.com/eks/latest/userguide/al2023.html and .../kubernetes-versions-extended.html |
| EKS | "Kubernetes 1.35 ... the kubelet will refuse to start by default on nodes using cgroup v1"; AL2023 users who forced v1 must migrate or set `failCgroupV1: false`; Bottlerocket 1.35 uses v2 and sets `failCgroupV1: false`; "Fargate continues to use cgroup v1"; containerd 1.x end of support after 1.35 | https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions-standard.html |
| EKS | Managed node groups without a custom AMI in the launch template "are required to upgrade using a blue/green strategy ... a new node group where you would specify AL2023 as the AMI type"; a custom-AMI launch template can swap the AMI ID in place; AL2023 uses nodeadm (needs eksctl >= 0.176.0) | https://docs.aws.amazon.com/eks/latest/userguide/al2023.html |
| EKS | `amiType` values include AL2023_x86_64_STANDARD, AL2023_ARM_64_STANDARD, BOTTLEROCKET_x86_64, BOTTLEROCKET_ARM_64 | https://docs.aws.amazon.com/eks/latest/APIReference/API_CreateNodegroup.html |
| EKS | eksctl `--node-ami-family` takes AmazonLinux2023, Bottlerocket, Ubuntu2204, Ubuntu2404 (and others) and works with `eksctl create nodegroup`; config file `managedNodeGroups: - name: ... amiFamily: ...` | https://docs.aws.amazon.com/eks/latest/eksctl/custom-ami-support.html (fetched) |
| EKS | Move workloads off an old group: `eksctl delete nodegroup --cluster my-cluster --name standard-nodes-old`; drain with `kubectl drain node_name --ignore-daemonsets --delete-emptydir-data` | https://docs.aws.amazon.com/eks/latest/userguide/migrate-stack.html |
| AKS | Ubuntu 22.04 and Azure Linux 3.0 are listed as "Cgroups V2"; Ubuntu 20.04 and Azure Linux 2.0 are not described on any fetched AKS page. | https://learn.microsoft.com/en-us/azure/aks/supported-kubernetes-versions |
| AKS | os_sku values and defaults: Ubuntu 22.04 default for 1.25-1.34, Ubuntu 24.04 default for 1.35+; Ubuntu2204 supported 1.25-1.36; AzureLinux3 supported 1.28-1.36 and "use this OS SKU to migrate from Azure Linux 2.0 to Azure Linux 3.0"; `az aks nodepool update` can migrate between Linux os-sku values but "might trigger an automatic reimage"; Ubuntu 20.04 support ends 2027-03-17; Azure Linux 2.0 node images removed from 2026-10-31 | https://learn.microsoft.com/en-us/azure/aks/upgrade-os-version |
| AKS | `az aks nodepool add ... --os-sku ...`; Linux pool names 1-12 characters; "When you delete a node pool, AKS doesn't perform cordon and drain."; ensure PDBs allow one replica to move at a time | https://learn.microsoft.com/en-us/azure/aks/create-node-pools and .../resize-node-pool |
| AKS | surge: default one extra node, 33% recommended for production; `--max-surge`; `--drain-timeout` default 30 minutes | https://learn.microsoft.com/en-us/azure/aks/upgrade-aks-node-pools-rolling |
| Self-managed | kernel 5.8+, containerd 1.4+/cri-o 1.20+, systemd driver; `stat -fc %T /sys/fs/cgroup/`; `systemd.unified_cgroup_hierarchy=1` in `GRUB_CMDLINE_LINUX` then `update-grub`; distributions with v2: Ubuntu 21.10+ (22.04+ recommended), Debian 11+, Fedora 31+, RHEL 9+, COS M97+ | https://kubernetes.io/docs/concepts/architecture/cgroups/ |
| Self-managed | containerd `SystemdCgroup = true`; kubelet `cgroupDriver: systemd`; "It's critical that the kubelet and the container runtime use the same cgroup driver" | https://kubernetes.io/docs/setup/production-environment/container-runtimes/ |
| Kubernetes | `FailCgroupV1` default true; "To disable this setting a cluster admin should set failCgroupV1 to false in the kubelet configuration file." (no official "temporary" wording found) | https://kubernetes.io/docs/reference/config-api/kubelet-config.v1beta1/ |
| Kubernetes | `kubectl drain --ignore-daemonsets <node name>`; evictions "respect the PodDisruptionBudgets"; `kubectl uncordon <node name>` | https://kubernetes.io/docs/tasks/administer-cluster/safely-drain-node/ |

Not verified (do not rely on): Terraform `create_before_destroy` behavior for node pools (a Terraform convention, not a provider-doc quote), whether a changed `ami_type` or `cgroup_mode` forces replacement, RHEL 8 default cgroup mode (search summary only), AKS cgroup version of Ubuntu 20.04 and Azure Linux 2.0, `eksctl delete nodegroup --drain` flag text.
