package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amitpatole/cgroupguard/internal/model"
)

const (
	srcCgroups   = "https://kubernetes.io/docs/concepts/architecture/cgroups/"
	srcRuntimes  = "https://kubernetes.io/docs/setup/production-environment/container-runtimes/"
	srcDrain     = "https://kubernetes.io/docs/tasks/administer-cluster/safely-drain-node/"
	srcKubeletV1 = "https://kubernetes.io/docs/reference/config-api/kubelet-config.v1beta1/"
)

// Options tune plan generation. Nothing in Options can make the plan execute.
type Options struct {
	// StopgapUntil (YYYY-MM-DD) includes the temporary failCgroupV1=false step.
	// Without an explicit expiry owned by the user, the stopgap is never emitted.
	StopgapUntil string
}

// Build turns a scan report into a deterministic plan. The same report always
// yields byte-identical output; nothing here reads the clock or the network.
func Build(r model.Report, o Options) (Plan, error) {
	if r.SchemaVersion != 2 {
		return Plan{}, fmt.Errorf("report schema %d is not supported by the planner", r.SchemaVersion)
	}
	p := Plan{SchemaVersion: SchemaVersion, Target: r.Target, Assumptions: []string{}, Limitations: append([]string{}, r.Limitations...)}
	if o.StopgapUntil != "" && !datePattern.MatchString(o.StopgapUntil) {
		return Plan{}, fmt.Errorf("--stopgap-until must be a date such as 2026-12-31")
	}
	groups, unknown, unsafe := groupNodes(r)
	p.Groups = groups
	if len(groups) == 0 && len(unknown) == 0 {
		p.Assumptions = append(p.Assumptions, "No Linux node with cgroup v1 or hybrid evidence was found; no node migration is planned.")
	}
	if len(unknown) > 0 {
		p.Assumptions = append(p.Assumptions, fmt.Sprintf("%d Linux node(s) have an unknown cgroup mode. They are not migrated by this plan until the mode is established (step P1.2).", len(unknown)))
	}
	p.Phases = append(p.Phases, prepPhase(r, groups, unknown, unsafe))
	if wl := workloadPhase(r); len(wl.Steps) > 0 {
		p.Phases = append(p.Phases, wl)
	}
	if len(groups) > 0 {
		p.Phases = append(p.Phases, migrationPhase(r, groups, &p))
	}
	if o.StopgapUntil != "" && len(groups) > 0 && atLeast(r.Target, "1.35") {
		p.Phases = append(p.Phases, stopgapPhase(o.StopgapUntil))
	}
	p.Phases = append(p.Phases, verifyPhase(r))
	if e := p.Validate(); e != nil {
		return Plan{}, fmt.Errorf("internal error: generated plan is invalid: %w", e)
	}
	return p, nil
}

// atLeast compares "major.minor" strings; anything unparsable is not "at least".
func atLeast(target, want string) bool {
	var a, b, c, d int
	if n, err := fmt.Sscanf(target, "%d.%d", &a, &b); n != 2 || err != nil {
		return false
	}
	if n, err := fmt.Sscanf(want, "%d.%d", &c, &d); n != 2 || err != nil {
		return false
	}
	return a > c || a == c && b >= d
}

// groupNodes returns migration groups (v1/hybrid Linux nodes), nodes whose mode is
// unknown, and names that failed validation (never interpolated into commands).
func groupNodes(r model.Report) ([]Group, []string, []string) {
	byKey := map[string]*Group{}
	var unknown, unsafe []string
	for _, n := range r.Nodes {
		if n.OS != "linux" {
			continue
		}
		if !SafeName(n.Name) || n.Pool != "" && !SafeName(n.Pool) {
			unsafe = append(unsafe, n.Name)
			continue
		}
		switch n.Cgroup {
		case "unknown":
			unknown = append(unknown, n.Name)
		case "v1", "hybrid":
			prov := canonProvider(n.Provider)
			key := prov + "\x00" + n.Pool
			g := byKey[key]
			if g == nil {
				g = &Group{Provider: prov, Pool: n.Pool}
				byKey[key] = g
			}
			g.Nodes = append(g.Nodes, n.Name)
		}
	}
	podsOnNode := map[string]*int{}
	statefulNode := map[string]bool{}
	for _, f := range r.Findings {
		if f.Node != "" && f.Resource == "" {
			podsOnNode[f.Node] = f.BlastRadius.PodsAffected
			statefulNode[f.Node] = f.BlastRadius.Recoverability.Stateful
		}
	}
	nodeByName := map[string]model.Node{}
	for _, n := range r.Nodes {
		nodeByName[n.Name] = n
	}
	var groups []Group
	for _, g := range byKey {
		sort.Strings(g.Nodes)
		total, known := 0, true
		arch, inst, fam := "", "", ""
		for i, n := range g.Nodes {
			nd := nodeByName[n]
			if i == 0 {
				arch, inst, fam = nd.Arch, nd.Instance, osFamily(nd.OSImage)
			} else {
				if nd.Arch != arch {
					arch = ""
				}
				if nd.Instance != inst {
					inst = ""
				}
				if osFamily(nd.OSImage) != fam {
					fam = "other"
				}
			}
			g.Stateful = g.Stateful || statefulNode[n]
		}
		g.Arch, g.OSFamily = arch, fam
		if inst != "" && SafeName(inst) {
			g.Instance = inst
		}
		for _, n := range g.Nodes {
			if p := podsOnNode[n]; p != nil {
				total += *p
			} else {
				known = false
			}
		}
		if known {
			t := total
			g.Pods = &t
		}
		groups = append(groups, *g)
	}
	// Smallest blast radius first (the canary); unknown counts sort last; ties by name.
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		switch {
		case (a.Pods == nil) != (b.Pods == nil):
			return a.Pods != nil
		case a.Pods != nil && *a.Pods != *b.Pods:
			return *a.Pods < *b.Pods
		}
		return a.Provider+"/"+a.Pool < b.Provider+"/"+b.Pool
	})
	if len(groups) > 0 {
		groups[0].Canary = true
	}
	sort.Strings(unknown)
	sort.Strings(unsafe)
	return groups, unknown, unsafe
}

func canonProvider(p string) string {
	switch p {
	case "gke", "eks", "aks", "openshift":
		return p
	}
	return "self-managed"
}

func prepPhase(r model.Report, groups []Group, unknown, unsafe []string) Phase {
	ph := Phase{ID: "P1", Title: "Prepare and assess"}
	ph.Steps = append(ph.Steps, Step{ID: "P1.1", Title: "Set the variables the commands in this plan use", Kind: "manual", Disruption: DisruptNone,
		Details: "Commands reference shell variables instead of guessed values. Set them yourself: CLUSTER_NAME for managed clusters, LOCATION (GKE region or zone), RESOURCE_GROUP (AKS), and your kubectl context. Nothing in this plan reads or changes them."})
	if len(unknown) > 0 || len(unsafe) > 0 {
		st := Step{ID: "P1.2", Title: "Establish the cgroup mode of nodes that were not measured", Kind: "inspect", Disruption: DisruptNone,
			Details:  "Unknown nodes: " + strings.Join(limitList(unknown, 40), ", ") + ". On each node run the command below, or re-run the scan with --prometheus-url, --kubelet-metrics (see docs/optional-permissions.md) or the guarded --probe.",
			Commands: []string{"stat -fc %T /sys/fs/cgroup/   # cgroup2fs = v2, tmpfs = v1"}, Sources: []string{srcCgroups}}
		if len(unsafe) > 0 {
			st.Details += " Nodes with names that failed validation were excluded from every command and file in this plan: " + strings.Join(limitList(displayAll(unsafe), 20), ", ") + "."
		}
		ph.Steps = append(ph.Steps, st)
	}
	ph.Steps = append(ph.Steps, Step{ID: "P1.3", Title: "Review disruption protection before touching any node", Kind: "inspect", Disruption: DisruptNone,
		Details:  "Draining respects PodDisruptionBudgets; a workload with one replica, no budget or persistent state is the most exposed. " + riskSummary(r),
		Commands: []string{"kubectl get pdb --all-namespaces", "kubectl get nodes -o wide"}, Sources: []string{srcDrain}})
	ph.Steps = append(ph.Steps, Step{ID: "P1.4", Title: "Take a restorable backup of everything this plan can change", Kind: "manual", Disruption: DisruptNone,
		Details: "Export or snapshot infrastructure-as-code state, node pool definitions, kubelet and container runtime configuration, and cluster state (for example etcd) in the way your platform documents. Keep the backup until the post-migration soak period ends."})
	if len(groups) > 1 {
		ph.Steps = append(ph.Steps, Step{ID: "P1.5", Title: "Agree the migration order and a canary", Kind: "manual", Disruption: DisruptNone,
			Details: "Groups are ordered by measured blast radius, smallest first. The first group is the canary: complete and verify it, and let it soak, before starting the next."})
	}
	return ph
}

func riskSummary(r model.Report) string {
	fragile := map[string]bool{}
	for _, f := range r.Findings {
		if f.Resource != "" && f.BlastRadius.Recoverability.Rating == "fragile" && f.BlastRadius.Criticality != "system" {
			parts := strings.SplitN(f.Resource, "/", 5)
			if len(parts) >= 3 {
				fragile[parts[0]+"/"+parts[1]+"/"+parts[2]] = true
			}
		}
	}
	if len(fragile) == 0 {
		return "No fragile workload was identified among the findings."
	}
	var l []string
	for k := range fragile {
		l = append(l, k)
	}
	sort.Strings(l)
	return "Fragile workloads among the findings (single replica, no budget, or persistent state): " + strings.Join(limitList(displayAll(l), 15), ", ") + "."
}

func limitList(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	return append(append([]string{}, l[:n]...), fmt.Sprintf("... and %d more", len(l)-n))
}

// workloadPhase groups warning/high workload findings by rule. It proposes no
// invented image tags: it names the resources and the documented minimum.
func workloadPhase(r model.Report) Phase {
	ph := Phase{ID: "P2", Title: "Fix workloads that cgroup v2 can break"}
	byRule := map[string][]model.Finding{}
	for _, f := range r.Findings {
		if f.Resource != "" && (f.Severity == "warning" || f.Severity == "high") {
			byRule[f.RuleID] = append(byRule[f.RuleID], f)
		}
	}
	var ids []string
	for id := range byRule {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fs := byRule[id]
		var res []string
		seen := map[string]bool{}
		for _, f := range fs {
			if !seen[f.Resource] {
				seen[f.Resource] = true
				res = append(res, f.Resource)
			}
		}
		sort.Strings(res)
		st := Step{ID: "W." + id, Title: workloadTitle(id), Kind: "manual", Disruption: DisruptRolling, Approval: true,
			Scope: fmt.Sprintf("%d container(s)", len(res)), Details: fs[0].Why + " Resources (namespace/kind/name/containerType/container): " + strings.Join(limitList(displayAll(res), 30), "; "),
			Preconditions: []string{"Change the manifests in your source repository (GitOps) rather than editing live objects."},
			Verification:  []string{"kubectl rollout status <kind>/<name> -n <namespace>", "Re-run cgroupguard check; the finding for this rule should be gone or reduced to a note."},
			Rollback:      []string{"Revert the manifest change in source control and let your delivery pipeline roll back, or run kubectl rollout undo <kind>/<name> -n <namespace>."},
			Sources:       []string{fs[0].Source}}
		ph.Steps = append(ph.Steps, st)
	}
	return ph
}

func workloadTitle(id string) string {
	switch id {
	case "CG101":
		return "Update JVM images to a build with cgroup v2 container support"
	case "CG102":
		return "Update cAdvisor to 0.43.0 or newer"
	case "CG103":
		return "Replace direct reads of cgroup v1 paths"
	case "CG104":
		return "Identify the runtime of unrecognized images"
	case "CG105":
		return "Confirm cgroup v2 behavior for runtimes and agents that need a version check"
	case "CG108":
		return "Update Node.js to 20.3.0 or newer"
	case "CG109":
		return "Move .NET workloads to .NET 5 or newer"
	case "CG110":
		return "Update monitoring and logging agents to cgroup v2 capable versions"
	}
	return "Review findings for rule " + id
}

func stopgapPhase(until string) Phase {
	return Phase{ID: "P4", Title: "Stopgap (not recommended): allow the kubelet on cgroup v1", Steps: []Step{{
		ID: "S.1", Title: "Temporarily set failCgroupV1: false until " + until, Kind: "config", Disruption: DisruptConfig, Approval: true,
		Details:      "This only lets the kubelet start on a cgroup v1 node. It does not make workloads or the node compatible, and it is not available on every managed platform. Kubernetes deprecates cgroup v1 and plans removal, so treat " + until + " as a hard deadline owned by a named person. Remove the setting when the node is migrated.",
		Commands:     []string{"# In the kubelet configuration file on each affected node, add:", "#   failCgroupV1: false", "sudo systemctl restart kubelet"},
		Files:        []File{{Path: "stopgap/kubelet-failcgroupv1.yaml", Content: "# TEMPORARY. Remove by " + until + ".\napiVersion: kubelet.config.k8s.io/v1beta1\nkind: KubeletConfiguration\nfailCgroupV1: false\n"}},
		Verification: []string{"kubectl get node <node> -o wide", "systemctl status kubelet"},
		Rollback:     []string{"Remove failCgroupV1: false (the default is true) and restart the kubelet; the node must be on cgroup v2 first or the kubelet will refuse to start."},
		Sources:      []string{srcKubeletV1}}}}
}

func verifyPhase(r model.Report) Phase {
	return Phase{ID: "P5", Title: "Verify", Steps: []Step{
		{ID: "V.1", Title: "Re-scan the cluster", Kind: "inspect", Disruption: DisruptNone,
			Commands: []string{"cgroupguard check --target-version " + r.Target + " --exit-error --format json"},
			Details:  "Expected: every Linux node reports cgroup v2 from direct evidence (metric, Prometheus or probe). A clean scan is still not an upgrade certification."},
		{ID: "V.2", Title: "Confirm workload health", Kind: "inspect", Disruption: DisruptNone,
			Commands: []string{"kubectl get nodes -o wide", "kubectl get pods --all-namespaces --field-selector=status.phase!=Running,status.phase!=Succeeded"}},
	}}
}

var datePattern = dateRe()

func osFamily(osImage string) string {
	l := strings.ToLower(osImage)
	switch {
	case strings.Contains(l, "ubuntu"):
		return "ubuntu"
	case strings.Contains(l, "azure linux"), strings.Contains(l, "mariner"):
		return "azurelinux"
	}
	return "other"
}
