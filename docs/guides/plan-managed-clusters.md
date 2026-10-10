# Plan the migration of GKE, EKS or AKS node pools

On a managed cluster you do not edit kernel arguments on the nodes. You replace the node pool (or group) with one that runs a cgroup v2 image, move the
workloads over, and delete the old pool. cgroupguard writes that plan for you to review. **It never creates, changes or deletes a cloud resource.**

> **Status.** The plans for managed clouds are generated from the providers' documentation, and every provider fact is listed with its source in
> [Sources](../sources.md). They have not been run against real GKE, EKS or AKS clusters by the project. Review them against your platform and test in a non-production
> cluster first. The Terraform modules ship with tests that run against mock providers.

## 1. Scan and write the plan

```console
$ cgroupguard fix --plan --context my-cluster \
      --prometheus-url http://prometheus.monitoring:9090 \
      --emit terraform --output-dir ./cgroupv2-plan
```

`--emit terraform` writes one module per node pool that needs migration. Add `--validate` to check the files with `terraform`, `tflint`, `trivy`, `yamllint`
and `ansible-lint` before anything is written. That downloads provider plugins from the network and runs them, so it is off by default; use `--no-validate` if you
will validate in your own pipeline.

For the sample snapshot the tool says:

```console
$ cgroupguard fix --plan --input examples/plan-mixed.json --emit terraform,ansible --no-validate --output-dir plan-out
Wrote 32 file(s) to plan-out. Nothing was executed; read RUNBOOK.md first.
```

`--output-dir` is created if missing. It refuses to overwrite existing files unless you pass `--overwrite`.

## 2. What you get

```text
plan-out/
├── RUNBOOK.md                        read this first
├── plan.json                         the same plan as data
├── eks/workers-al2-nodegroup.yaml    an eksctl config for the replacement group
├── gke/pool-legacy-system-config.yaml
└── terraform/
    ├── README.md                     review checklist for the pull request
    ├── aks-userpool1/    main.tf  variables.tf  versions.tf  tests/cgroup_v2.tftest.hcl
    ├── eks-workers-al2/  main.tf  variables.tf  versions.tf  tests/cgroup_v2.tftest.hcl
    └── gke-pool-legacy/  main.tf  variables.tf  versions.tf  tests/cgroup_v2.tftest.hcl
```

Open `RUNBOOK.md` first. It starts with the ground rules and the **migration order**, smallest blast radius first, so the first group is your canary:

```text
| # | Provider | Pool | Nodes | Pods affected | Note |
|---|---|---|---|---|---|
| 1 | aks | userpool1 | 1 | 2 | canary |
| 2 | eks | workers-al2 | 1 | 2 |  |
| 3 | self-managed |  | 2 | 2 |  |
| 4 | gke | pool-legacy | 2 | 4 |  |
```

Then the phases:

| Phase | What it covers |
|---|---|
| P1 Prepare and assess | Set variables, establish unknown nodes, review PodDisruptionBudgets, take a backup, agree the order. |
| P2 Fix workloads | One section per workload finding (`W.CG101` and so on) with how to fix it. |
| P3 Migrate node groups | Per group: add the replacement pool, cordon the old nodes, drain one at a time, delete the old pool after a soak period. |
| P5 Verify | Re-run the check and confirm every node reports cgroup v2. |

Every step that disrupts anything is marked **approval required** and always carries a **verification** and a **rollback**. Commands use variables such as
`$CLUSTER_NAME`, `$LOCATION` and `$RESOURCE_GROUP`; anything the tool could not measure is written as `REPLACE_WITH_*`, which you must fill in.

## 3. The Terraform modules

Each module **adds** a replacement pool on a cgroup v2 image and nothing else. This is the EKS one for the sample:

```hcl
resource "aws_eks_node_group" "cgroup_v2" {
  cluster_name    = var.cluster_name
  node_group_name = "workers-al2-al2023"
  node_role_arn   = var.node_role_arn
  subnet_ids      = var.subnet_ids
  ami_type        = var.ami_type
  instance_types  = var.instance_types

  scaling_config {
    desired_size = var.desired_size
    min_size     = var.desired_size
    max_size     = var.desired_size * 2
  }
  update_config { max_unavailable = 1 }
  lifecycle {
    create_before_destroy = true
    prevent_destroy       = false
  }
}
```

- Provider versions are pinned **exactly**. Run `terraform init` and commit the `.terraform.lock.hcl` it creates.
- The modules contain **no credentials**. Authenticate with workload identity, OIDC or an SSO role, never keys in variables.
- The `terraform plan` for a module should show **additions only**. If it shows a destroy or a replace of something existing, stop.
- Stateful pools get `prevent_destroy`. The control plane is never managed by these files.
- The old pool is removed **by you**, in your own configuration, only after the cordon, drain, verification and soak in the runbook.

Run the module tests (they use mock providers and need no cloud account):

```console
$ cd plan-out/terraform/eks-workers-al2 && terraform init -backend=false && terraform test
```

## 4. Provider specifics

| Provider | How the plan makes the new pool cgroup v2 |
|---|---|
| GKE | `linuxConfig: cgroupMode: CGROUP_MODE_V2` in a node system config file, applied with `gcloud container node-pools update --system-config-from-file`, or the module's equivalent. Nodes are re-created. |
| EKS | A managed node group with an Amazon Linux 2023 AMI (`ami_type` AL2023), which uses cgroup v2. An `eksctl` config is provided as an alternative. |
| AKS | A node pool with `os_sku` `Ubuntu2204` or `AzureLinux3`, which AKS lists as cgroup v2. |

If your pools are not in this table or the tool cannot tell, they are treated as self-managed and you get the Ansible plan instead,
see [Self-managed nodes](plan-self-managed-nodes.md).

## 5. Make the temporary stop-gap explicit

Kubernetes lets you keep cgroup v1 nodes running for a while with the kubelet option `failCgroupV1: false`. cgroupguard includes that step **only** if you ask for it,
and only with an expiry date, so a stop-gap cannot quietly become permanent:

```console
$ cgroupguard fix --plan --context my-cluster --stopgap-until 2027-03-31 --output-dir ./cgroupv2-plan
```

Some managed providers disallow overriding it. The runbook says so wherever it matters.

## 6. Review in a pull request

Put the output directory in a repository and let the team review it like code. `fix --pr` can do that for you, see
[Apply and pull requests](apply-and-pull-requests.md).
