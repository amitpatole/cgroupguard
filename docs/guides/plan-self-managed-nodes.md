# Plan the migration of self-managed nodes

For nodes you run yourself (bare metal, virtual machines, kubeadm, k3s and similar), moving to cgroup v2 means changing the kernel boot arguments, the
container runtime's cgroup driver and the kubelet's cgroup driver, then rebooting. cgroupguard writes an **Ansible project** that does this one node at a time,
and a runbook with the same steps by hand.

> **Tested on:** Ubuntu 24.04 and Rocky Linux 9, each with a real cgroup v1 node (kubeadm and k3s): preflight, dry run, migration and rollback. The role handles Debian-like
> hosts (GRUB) and RedHat-like hosts (`grubby`). It has **not** been tried on other distributions, on a multi-node cluster with a separate control plane, or with other
> container runtimes than containerd. Rehearse on a disposable node first.

## 1. Generate the project

```console
$ cgroupguard fix --plan --context my-cluster --prometheus-url http://prometheus.monitoring:9090 \
      --emit ansible --output-dir ./cgroupv2-plan
```

Add `--validate` to check it with `yamllint`, `ansible-playbook --syntax-check` and `ansible-lint` first (it installs the pinned collection from the network).

```text
ansible/
├── README.md
├── ansible.cfg  .ansible-lint  .yamllint  requirements.yml
├── inventory.yml                 fill in your addresses
├── cgroupv2-migrate.yml          the migration, canary first
├── cgroupv2-rollback.yml         the way back, one node at a time
└── roles/cgroupv2/               tasks: preflight, migrate, verify, rollback
```

## 2. Fill in the inventory

```yaml
all:
  vars:
    ansible_user: REPLACE_WITH_SSH_USER
    cgroupv2_control_host: localhost
  children:
    cgroupv2_canary:
      hosts:
        'onprem-1':
          ansible_host: REPLACE_WITH_SSH_ADDRESS
          cgroupv2_node_name: 'onprem-1'
    cgroupv2_rest:
      hosts:
        'onprem-2':
          ansible_host: REPLACE_WITH_SSH_ADDRESS
          cgroupv2_node_name: 'onprem-2'
```

The **canary** group is the node with the smallest blast radius and runs first. Keep secrets out of this file: use SSH keys in an agent, Ansible Vault for
anything else, and a least-privilege `sudo` rule for the connecting user.

## 3. Prepare the control host

```console
$ ansible-galaxy collection install -r requirements.yml
$ export KUBECONFIG=/path/to/kubeconfig
$ kubectl auth can-i patch nodes && kubectl auth can-i create pods/eviction
```

The identity in the kubeconfig needs `get`, `list` and `patch` on nodes, `get` and `list` on pods, and `create` on `pods/eviction`. That is exactly what a drain uses.

## 4. Look before you leap

```console
$ ansible-playbook -i inventory.yml cgroupv2-migrate.yml --tags preflight       # read-only checks
$ ansible-playbook -i inventory.yml cgroupv2-migrate.yml --check --diff         # dry run: shows the exact edits
```

The preflight does not change anything. The dry run shows the file edits it *would* make, for example:

```diff
-            SystemdCgroup = false
+            SystemdCgroup = true
-cgroupDriver: cgroupfs
+cgroupDriver: systemd
```

Preflight stops the play for a node when:

- the distribution family is not Debian-like or RedHat-like;
- the Linux kernel is older than 5.8;
- a GRUB drop-in file forces cgroup v1 on the kernel command line (those must be edited by hand);
- the container runtime or kubelet configuration file cannot be found.

## 5. Migrate

```console
$ ansible-playbook -i inventory.yml cgroupv2-migrate.yml
```

For each node, in order, **one at a time** and stopping on the first failure, the role:

1. runs the preflight checks above and notes whether the node still needs the migration (a node already on cgroup v2 is skipped);
2. cordons the node (marking it with the annotation `cgroupguard.io/migrating`) and drains it with evictions, respecting PodDisruptionBudgets and never forcing;
3. backs up the boot configuration, the container runtime configuration and the kubelet configuration (the original kernel arguments are recorded too);
4. enables cgroup v2 and the systemd cgroup driver, in the runtime and in the kubelet;
5. reboots, then **requires** the filesystem to be cgroup v2 and the node to be `Ready` before moving on;
6. uncordons the node, but only if this role cordoned it, and removes its marker.

Between the canary and the rest the play pauses so you can check the canary before continuing.

## 6. Verify

```console
$ cgroupguard check --context my-cluster --prometheus-url http://prometheus.monitoring:9090
Node cgroup readiness: pass
```

`pass` means every Linux node now has direct evidence of cgroup v2. The workloads still need their own checks: see the `W.` sections of the runbook.

## 7. Roll back

```console
$ ansible-playbook -i inventory.yml cgroupv2-rollback.yml --limit onprem-1
```

The rollback restores the recorded kernel arguments and the runtime and kubelet files exactly, reboots, and brings the node back on cgroup v1. On Debian-like
hosts it restores the GRUB configuration backup, on RedHat-like hosts it re-applies the recorded `grubby` arguments.

## Without Ansible

`RUNBOOK.md` contains the same steps as plain commands (`M3.1` to `M3.6` for self-managed groups), each with approval, verification and rollback. Use it when you have another
configuration-management tool, or when you want to do the first node by hand.
