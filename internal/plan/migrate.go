package plan

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/model"
)

func dateRe() *regexp.Regexp {
	return regexp.MustCompile(`^20[0-9]{2}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$`)
}

func migrationPhase(r model.Report, groups []Group, p *Plan) Phase {
	ph := Phase{ID: "P3", Title: "Migrate node groups to cgroup v2 (one group at a time, canary first)"}
	for i, g := range groups {
		prefix := fmt.Sprintf("M%d", i+1)
		var steps []Step
		switch g.Provider {
		case "gke":
			steps = gkeSteps(prefix, g, r)
		case "eks":
			steps = eksSteps(prefix, g, r, p)
		case "aks":
			steps = aksSteps(prefix, g, r, p)
		case "openshift":
			steps = []Step{{ID: prefix + ".1", Title: "OpenShift node migration needs a platform-specific procedure", Kind: "manual", Disruption: DisruptNone,
				Scope: groupScope(g), Details: "cgroupguard has no verified remediation source for OpenShift, so it generates no commands. Follow Red Hat's documentation for changing the cgroup mode of the machine config pool that owns these nodes."}}
			p.Limitations = append(p.Limitations, "No remediation was generated for OpenShift nodes.")
		default:
			steps = selfManagedSteps(prefix, g)
		}
		if g.Canary && len(groups) > 1 {
			steps[0].Details = "CANARY GROUP (smallest measured blast radius). " + steps[0].Details
		}
		ph.Steps = append(ph.Steps, steps...)
	}
	return ph
}

func groupScope(g Group) string {
	s := g.Provider
	if g.Pool != "" {
		s += " pool " + g.Pool
	}
	s += fmt.Sprintf(", %d nodes", len(g.Nodes))
	if g.Pods != nil {
		s += fmt.Sprintf(", %d pods", *g.Pods)
	}
	return s
}

func cordonDrain(prefix string, n int, nodes []string, old string) []Step {
	cordon := []string{"kubectl cordon " + strings.Join(quoteAll(nodes), " ")}
	var drain []string
	for _, nd := range nodes {
		drain = append(drain, "kubectl drain "+Quote(nd)+" --ignore-daemonsets --delete-emptydir-data --timeout=30m")
	}
	return []Step{
		{ID: fmt.Sprintf("%s.%d", prefix, n), Title: "Cordon the old nodes", Kind: "command", Disruption: DisruptDrain, Approval: true, Scope: old, Action: &Action{Type: "cordon", Nodes: nodes},
			Commands: cordon, Verification: []string{"kubectl get nodes   # old nodes show SchedulingDisabled"},
			Rollback: []string{"kubectl uncordon " + strings.Join(quoteAll(nodes), " ")}, Sources: []string{srcDrain}},
		{ID: fmt.Sprintf("%s.%d", prefix, n+1), Title: "Drain the old nodes one at a time", Kind: "command", Disruption: DisruptDrain, Approval: true, Scope: old, Action: &Action{Type: "drain", Nodes: nodes, TimeoutMinutes: 30},
			Details:  "Draining uses the eviction API and respects PodDisruptionBudgets. Do not use --disable-eviction or --force. If a drain blocks on a budget, fix the cause instead of bypassing it.",
			Commands: drain, Verification: []string{"kubectl get pods --all-namespaces -o wide --field-selector spec.nodeName=" + Quote(nodes[0]), "Confirm replacement pods are Ready on the new nodes before the next drain."},
			Rollback: []string{"kubectl uncordon " + strings.Join(quoteAll(nodes), " ") + "   # pods are rescheduled by their controllers"}, Sources: []string{srcDrain}},
	}
}

func quoteAll(l []string) []string {
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = Quote(s)
	}
	return out
}

func sources(s ...string) []string { return s }

const (
	srcGKE   = "https://docs.cloud.google.com/kubernetes-engine/docs/how-to/migrate-cgroupv2"
	srcGKEns = "https://docs.cloud.google.com/kubernetes-engine/docs/how-to/node-system-config"
	srcEKS   = "https://docs.aws.amazon.com/eks/latest/userguide/al2023.html"
	srcEKSfq = "https://docs.aws.amazon.com/eks/latest/userguide/eks-ami-deprecation-faqs.html"
	srcEKSmv = "https://docs.aws.amazon.com/eks/latest/userguide/migrate-stack.html"
	srcEKSct = "https://docs.aws.amazon.com/eks/latest/eksctl/custom-ami-support.html"
	srcAKSos = "https://learn.microsoft.com/en-us/azure/aks/upgrade-os-version"
	srcAKSnp = "https://learn.microsoft.com/en-us/azure/aks/create-node-pools"
	srcAKSrs = "https://learn.microsoft.com/en-us/azure/aks/resize-node-pool"
)

// ---- GKE: update the node pool's system configuration (surge upgrade by GKE) ----

func gkeSteps(prefix string, g Group, r model.Report) []Step {
	pool := g.Pool
	if pool == "" {
		return []Step{{ID: prefix + ".1", Title: "GKE nodes without a node pool label cannot be planned", Kind: "manual", Disruption: DisruptNone, Scope: groupScope(g),
			Details: "These nodes carry no cloud.google.com/gke-nodepool label, so no pool name can be put into a command. Identify the pool by hand."}}
	}
	cfgPath := "gke/" + pool + "-system-config.yaml"
	file := File{Path: cfgPath, Content: "linuxConfig:\n  cgroupMode: 'CGROUP_MODE_V2'\n"}
	return []Step{
		{ID: prefix + ".1", Title: "Review the node pool and its upgrade strategy", Kind: "inspect", Disruption: DisruptNone, Scope: groupScope(g),
			Details: "GKE applies a cgroup mode change by re-creating nodes with its surge upgrade strategy, immediately and without respecting maintenance policies. Choose max-surge or blue-green settings first if the default is too aggressive. If you prefer a blue/green replacement, generate the module with --emit terraform (terraform/gke-<pool>/). Autopilot clusters and auto-provisioned pools use a cluster-level command instead: gcloud container clusters update \"$CLUSTER_NAME\" --autoprovisioning-cgroup-mode=v2.",
			Commands: []string{"gcloud container node-pools describe " + Quote(pool) + " --cluster \"$CLUSTER_NAME\" --location \"$LOCATION\"",
				"# optional: gcloud container node-pools update " + Quote(pool) + " --cluster \"$CLUSTER_NAME\" --location \"$LOCATION\" --max-surge-upgrade=1 --max-unavailable-upgrade=0"},
			Sources: sources(srcGKE, "https://docs.cloud.google.com/kubernetes-engine/docs/how-to/node-pool-upgrade-strategies")},
		{ID: prefix + ".2", Title: "Review the node system configuration file", Kind: "config", Disruption: DisruptNone, Scope: groupScope(g),
			Details: "Save this file and review it. It sets only the cgroup mode.", Files: []File{file}, Sources: sources(srcGKEns)},
		{ID: prefix + ".3", Title: "Apply cgroup v2 to the node pool (nodes are re-created)", Kind: "command", Disruption: DisruptReplace, Approval: true, Scope: groupScope(g),
			Details:       "Documented by Google: \"This change requires re-creating the nodes, which can cause disruption to your running workloads.\" Run during an agreed window; protect workloads with PodDisruptionBudgets first.",
			Preconditions: []string{"Backup from P1.4 is complete.", "PodDisruptionBudgets reviewed in P1.3.", "The pool runs a GKE version that supports the cgroup mode setting."},
			Commands:      []string{"gcloud container node-pools update " + Quote(pool) + " --cluster \"$CLUSTER_NAME\" --location \"$LOCATION\" --system-config-from-file=" + cfgPath},
			Verification:  []string{"gcloud container node-pools describe " + Quote(pool) + " --cluster \"$CLUSTER_NAME\" --location \"$LOCATION\"", "cgroupguard check --target-version " + r.Target + " --kubelet-metrics   # nodes should report v2"},
			Rollback:      []string{"Re-apply a system configuration file with cgroupMode: 'CGROUP_MODE_V1' using the same command; this re-creates the nodes again. GKE removes cgroup v1 support with minor version 1.35, so on 1.35 and later rollback to v1 is not available: roll back workload changes instead.", "If nodes fail to become Ready, stop and open a Google Cloud support case; do not delete the pool."},
			Sources:       sources(srcGKE, srcGKEns)},
	}
}

// ---- EKS: blue/green managed node group on AL2023 ----

func eksSteps(prefix string, g Group, r model.Report, p *Plan) []Step {
	pool := g.Pool
	if pool == "" {
		return []Step{{ID: prefix + ".1", Title: "EKS nodes without a node group label cannot be planned", Kind: "manual", Disruption: DisruptNone, Scope: groupScope(g),
			Details: "These nodes carry no eks.amazonaws.com/nodegroup label (self-managed or Karpenter nodes). Change the AMI family in the owning tool: for Karpenter set the EC2NodeClass amiFamily; for self-managed groups use a cgroup v2 capable AMI."}}
	}
	newName := pool + "-al2023"
	if len(newName) > 63 || !SafeName(newName) {
		p.Limitations = append(p.Limitations, "Node group "+pool+" has a name too long to derive a replacement name.")
		return []Step{{ID: prefix + ".1", Title: "Choose a name for the replacement node group", Kind: "manual", Disruption: DisruptNone, Scope: groupScope(g), Details: "The derived name would be invalid; pick one by hand."}}
	}
	instance, nodeCount := g.Instance, len(g.Nodes)
	cfg := "# Review before use. Sizes are copied from the current node count; set min/max to match the old group.\napiVersion: eksctl.io/v1alpha5\nkind: ClusterConfig\nmetadata:\n  name: REPLACE_WITH_CLUSTER_NAME\n  region: REPLACE_WITH_REGION\nmanagedNodeGroups:\n  - name: " + newName + "\n    amiFamily: AmazonLinux2023\n"
	if instance != "" {
		cfg += "    instanceType: " + instance + "\n"
	} else {
		cfg += "    instanceType: REPLACE_WITH_INSTANCE_TYPE\n"
	}
	cfg += fmt.Sprintf("    desiredCapacity: %d\n    minSize: %d\n    maxSize: %d\n", nodeCount, nodeCount, nodeCount*2)
	cfgPath := "eks/" + pool + "-nodegroup.yaml"
	steps := []Step{
		{ID: prefix + ".1", Title: "Review the replacement managed node group", Kind: "config", Disruption: DisruptNone, Scope: groupScope(g),
			Details: "AL2 is cgroup v1; AWS recommends AL2023 or Bottlerocket, which enable cgroup v2 by default. For managed node groups without a custom-AMI launch template AWS requires a blue/green migration: create a new group with the new AMI type, move workloads, delete the old group. Replace the REPLACE_WITH_* placeholders (they are not known from the cluster). For Bottlerocket use amiFamily: Bottlerocket instead.",
			Files:   []File{{Path: cfgPath, Content: cfg}}, Sources: sources(srcEKS, srcEKSfq, srcEKSct)},
		{ID: prefix + ".2", Title: "Create the replacement node group", Kind: "command", Disruption: DisruptNone, Approval: true, Scope: groupScope(g),
			Details:       "Creates new nodes next to the old ones; nothing is drained yet. AL2023 needs eksctl 0.176.0 or newer and VPC CNI 1.16.2 or newer; nodeadm replaces bootstrap.sh for custom launch templates.",
			Commands:      []string{"eksctl create nodegroup --config-file=" + cfgPath},
			Preconditions: []string{"Placeholders in " + cfgPath + " are replaced and the file is reviewed.", "CoreDNS has at least 2 replicas and any cluster autoscaler is paused for the move."},
			Verification:  []string{"kubectl get nodes -L eks.amazonaws.com/nodegroup", "cgroupguard check --target-version " + r.Target + " --kubelet-metrics   # new nodes should report v2"},
			Rollback:      []string{"Delete only the new node group: eksctl delete nodegroup --cluster \"$CLUSTER_NAME\" --name " + newName},
			Sources:       sources(srcEKS, srcEKSmv)},
	}
	steps[1].Disruption = DisruptPlatform
	steps = append(steps, cordonDrain(prefix, 3, g.Nodes, "old node group "+pool)...)
	steps = append(steps, Step{ID: prefix + ".5", Title: "Delete the old node group after a soak period", Kind: "command", Disruption: DisruptReplace, Approval: true, Scope: "old node group " + pool,
		Details:       "Irreversible. Keep the old group, drained and cordoned, through an agreed soak period before deleting it.",
		Preconditions: []string{"Workloads run healthy on the new group.", "The old group's definition is saved in source control or backed up."},
		Commands:      []string{"eksctl delete nodegroup --cluster \"$CLUSTER_NAME\" --name " + Quote(pool)},
		Verification:  []string{"kubectl get nodes -L eks.amazonaws.com/nodegroup"},
		Rollback:      []string{"Deletion cannot be undone. Recreate the group from the saved definition and re-run the migration steps."},
		Sources:       sources(srcEKSmv)})
	return steps
}

// ---- AKS: new node pool on a cgroup v2 documented OS SKU ----

func aksSteps(prefix string, g Group, r model.Report, p *Plan) []Step {
	pool := g.Pool
	if pool == "" {
		return []Step{{ID: prefix + ".1", Title: "AKS nodes without an agent pool label cannot be planned", Kind: "manual", Disruption: DisruptNone, Scope: groupScope(g),
			Details: "These nodes carry no kubernetes.azure.com/agentpool label. Identify the pool by hand."}}
	}
	base := pool
	if len(base) > 9 {
		base = base[:9]
	}
	newName := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(base)) + "v2"
	if !SafeName(newName) || len(newName) > 12 {
		return []Step{{ID: prefix + ".1", Title: "Choose a name for the replacement node pool", Kind: "manual", Disruption: DisruptNone, Scope: groupScope(g), Details: "The derived name would be invalid (Linux pool names are 1-12 characters); pick one by hand."}}
	}
	for _, n := range r.Nodes {
		if n.Pool == newName {
			p.Limitations = append(p.Limitations, "The derived AKS pool name "+newName+" already exists; pick one by hand.")
			return []Step{{ID: prefix + ".1", Title: "Choose a name for the replacement node pool", Kind: "manual", Disruption: DisruptNone, Scope: groupScope(g), Details: "The derived name " + newName + " is already in use."}}
		}
	}
	sku := "Ubuntu2204"
	osNote := "Ubuntu 22.04 is listed as cgroup v2 on the AKS supported-versions page."
	if g.OSFamily == "azurelinux" {
		sku = "AzureLinux3"
		osNote = "Azure Linux 3.0 is listed as cgroup v2 on the AKS supported-versions page; AzureLinux3 is the documented SKU for moving off Azure Linux 2.0."
	}
	add := "az aks nodepool add --resource-group \"$RESOURCE_GROUP\" --cluster-name \"$CLUSTER_NAME\" --name " + newName + " --os-type Linux --os-sku " + sku + " --mode User --node-count " + fmt.Sprint(len(g.Nodes))
	if g.Instance != "" {
		add += " --node-vm-size " + g.Instance
	}
	steps := []Step{{ID: prefix + ".1", Title: "Add a replacement node pool on a cgroup v2 OS image", Kind: "command", Disruption: DisruptPlatform, Approval: true, Scope: groupScope(g),
		Details:       osNote + " If the old pool is the cluster's only system pool, create the replacement with --mode System first: a system pool can only be deleted if another system pool exists. Labels, taints, zones, max pods and autoscaling settings are not copied; add the matching flags from the old pool definition. Linux pool names are limited to 12 characters.",
		Preconditions: []string{"Backup from P1.4 is complete.", "Regional quota exists for the extra nodes."},
		Commands:      []string{add},
		Verification:  []string{"kubectl get nodes -L kubernetes.azure.com/agentpool", "cgroupguard check --target-version " + r.Target + " --kubelet-metrics   # new nodes should report v2"},
		Rollback:      []string{"Delete only the new pool: az aks nodepool delete --resource-group \"$RESOURCE_GROUP\" --cluster-name \"$CLUSTER_NAME\" --name " + newName},
		Sources:       sources(srcAKSos, srcAKSnp)}}
	steps = append(steps, cordonDrain(prefix, 2, g.Nodes, "old pool "+pool)...)
	steps = append(steps, Step{ID: prefix + ".4", Title: "Delete the old node pool after a soak period", Kind: "command", Disruption: DisruptReplace, Approval: true, Scope: "old pool " + pool,
		Details:       "Irreversible. Microsoft documents: \"When you delete a node pool, AKS doesn't perform cordon and drain\", so the previous steps are mandatory.",
		Preconditions: []string{"Workloads run healthy on the new pool.", "The old pool definition is saved in source control or backed up."},
		Commands:      []string{"az aks nodepool delete --resource-group \"$RESOURCE_GROUP\" --cluster-name \"$CLUSTER_NAME\" --name " + Quote(pool)},
		Verification:  []string{"kubectl get nodes -L kubernetes.azure.com/agentpool"},
		Rollback:      []string{"Deletion cannot be undone. Recreate the pool from the saved definition and re-run the migration steps."},
		Sources:       sources(srcAKSrs)})
	return steps
}

// ---- Self-managed: per-node rolling change of kernel, runtime and kubelet ----

func selfManagedSteps(prefix string, g Group) []Step {
	if len(g.Nodes) == 0 {
		return nil
	}
	scope := groupScope(g)
	order := strings.Join(g.Nodes, ", ")
	node := Quote(g.Nodes[0])
	steps := []Step{
		{ID: prefix + ".1", Title: "Check that each node can run cgroup v2", Kind: "inspect", Disruption: DisruptNone, Scope: scope,
			Details:  "Requirements: Linux kernel 5.8 or newer, containerd 1.4+ or cri-o 1.20+, and the systemd cgroup driver in both the kubelet and the runtime. Nodes, in the order they should be changed (one at a time): " + order + ".",
			Commands: []string{"uname -r", "containerd --version   # or: crio --version", "stat -fc %T /sys/fs/cgroup/   # tmpfs = v1, cgroup2fs = v2"}, Sources: sources(srcCgroups, srcRuntimes)},
		{ID: prefix + ".2", Title: "Cordon and drain one node", Kind: "command", Disruption: DisruptDrain, Approval: true, Scope: scope,
			Details:      "Repeat steps " + prefix + ".2 to " + prefix + ".6 for each node in the order above, finishing and verifying one node before the next. Example node: " + g.Nodes[0] + ".",
			Commands:     []string{"kubectl cordon " + node, "kubectl drain " + node + " --ignore-daemonsets --delete-emptydir-data --timeout=30m"},
			Verification: []string{"kubectl get node " + node, "Replacement pods are Ready elsewhere."},
			Rollback:     []string{"kubectl uncordon " + node}, Sources: sources(srcDrain)},
		{ID: prefix + ".3", Title: "Back up the node's boot, runtime and kubelet configuration", Kind: "command", Disruption: DisruptConfig, Approval: true, Scope: scope,
			Commands:     []string{"sudo cp -a /etc/default/grub /etc/default/grub.cgroupguard.bak", "sudo cp -a /etc/containerd/config.toml /etc/containerd/config.toml.cgroupguard.bak", "sudo cp -a /var/lib/kubelet/config.yaml /var/lib/kubelet/config.yaml.cgroupguard.bak"},
			Verification: []string{"ls -l /etc/default/grub.cgroupguard.bak /etc/containerd/config.toml.cgroupguard.bak /var/lib/kubelet/config.yaml.cgroupguard.bak"},
			Rollback:     []string{"Nothing to undo: these are copies. Paths vary by distribution and installer; adjust them to your node."}},
		{ID: prefix + ".4", Title: "Enable cgroup v2 and the systemd cgroup driver on the node", Kind: "config", Disruption: DisruptConfig, Approval: true, Scope: scope,
			Details:      "Debian/Ubuntu: add systemd.unified_cgroup_hierarchy=1 to GRUB_CMDLINE_LINUX in /etc/default/grub, then run update-grub (RHEL-like systems: use grubby --update-kernel=ALL --args=\"systemd.unified_cgroup_hierarchy=1\"). In containerd set SystemdCgroup = true under [plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]; in the kubelet configuration set cgroupDriver: systemd. The kubelet and the runtime must use the same cgroup driver.",
			Commands:     []string{"sudo sed -i 's/^GRUB_CMDLINE_LINUX=\"\\(.*\\)\"/GRUB_CMDLINE_LINUX=\"\\1 systemd.unified_cgroup_hierarchy=1\"/' /etc/default/grub", "sudo update-grub", "# set SystemdCgroup = true in /etc/containerd/config.toml and cgroupDriver: systemd in /var/lib/kubelet/config.yaml"},
			Verification: []string{"grep unified_cgroup_hierarchy /etc/default/grub", "grep -n SystemdCgroup /etc/containerd/config.toml", "grep -n cgroupDriver /var/lib/kubelet/config.yaml"},
			Rollback:     []string{"Restore the *.cgroupguard.bak copies and run update-grub (or grubby) again before rebooting."}, Sources: sources(srcCgroups, srcRuntimes)},
		{ID: prefix + ".5", Title: "Reboot the node and confirm cgroup v2", Kind: "command", Disruption: DisruptReboot, Approval: true, Scope: scope,
			Commands:     []string{"sudo systemctl reboot", "# after it returns:", "stat -fc %T /sys/fs/cgroup/"},
			Verification: []string{"stat prints cgroup2fs", "kubectl get node " + node + "   # Ready"},
			Rollback:     []string{"Boot the previous kernel command line from the GRUB menu, or restore the backups from step " + prefix + ".3, then reboot. Do not uncordon a node that is not Ready."}, Sources: sources(srcCgroups)},
		{ID: prefix + ".6", Title: "Uncordon the node and let it soak", Kind: "command", Disruption: DisruptNone, Scope: scope,
			Commands: []string{"kubectl uncordon " + node}, Verification: []string{"cgroupguard check --target-version <target> --kubelet-metrics   # the node reports v2"}},
	}
	return steps
}
