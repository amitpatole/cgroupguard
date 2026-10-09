package rules

import (
	"sort"

	"github.com/amitpatole/cgroupguard/internal/model"
)

var systemNamespaces = map[string]bool{"kube-system": true, "kube-public": true, "kube-node-lease": true}

func intPtr(n int) *int { return &n }

// workloadBlast measures impact from the inventory. Fields stay nil/"unknown"
// when the snapshot carries no placement evidence; nothing is guessed.
func workloadBlast(w model.Workload, nodes []model.Node, failureMode string) model.BlastRadius {
	totalNodes := len(nodes)
	pools := map[string]string{}
	for _, n := range nodes {
		pools[n.Name] = n.Pool
	}
	b := model.BlastRadius{Scope: "workload", TotalNodes: totalNodes, Namespaces: []string{w.Namespace}, FailureMode: failureMode, Basis: "unknown"}
	collected := w.Replicas != nil || w.Placements != nil
	if collected {
		pods := 0
		for _, n := range w.Placements {
			pods += n
		}
		b.NodesAffected, b.PodsAffected, b.Basis = intPtr(len(w.Placements)), intPtr(pods), "counted"
		poolSet, allKnown := map[string]bool{}, len(w.Placements) > 0
		for node := range w.Placements {
			if pool := pools[node]; pool != "" {
				poolSet[pool] = true
			} else {
				allKnown = false
			}
		}
		for pool := range poolSet {
			b.NodePools = append(b.NodePools, pool)
		}
		sort.Strings(b.NodePools)
		if w.Kind == "DaemonSet" {
			switch {
			case totalNodes > 0 && len(w.Placements) >= totalNodes:
				b.Scope = "cluster"
			case allKnown && len(poolSet) == 1:
				b.Scope = "nodePool"
			default:
				b.Scope = "node"
			}
		}
	}
	switch {
	case systemNamespaces[w.Namespace]:
		b.Criticality, b.CriticalityBasis = "system", "namespace name"
	case w.Kind == "DaemonSet":
		b.Criticality, b.CriticalityBasis = "node-wide", "controller kind"
	default:
		b.Criticality, b.CriticalityBasis = "workload", "default; no criticality signal collected"
	}
	b.Recoverability = model.Recoverability{Rating: "unknown", Replicas: w.Replicas, PDB: w.PDB, Stateful: w.Stateful}
	if b.Recoverability.PDB == "" {
		b.Recoverability.PDB = "unknown"
	}
	if collected {
		switch {
		case w.Kind == "DaemonSet" || w.Kind == "Pod" || w.Stateful || w.Replicas != nil && *w.Replicas <= 1:
			b.Recoverability.Rating = "fragile"
		case w.Kind == "Job" || w.Kind == "CronJob":
			b.Recoverability.Rating = "unknown"
		case w.PDB == "none":
			b.Recoverability.Rating = "fragile"
		case w.PDB == "present" && w.Replicas != nil:
			b.Recoverability.Rating = "protected"
		}
	}
	return b
}

// FinalizeBlastRadius fills node findings from the workload placements and orders
// all findings by severity, then by pods affected, so the biggest impact leads.
func FinalizeBlastRadius(r *model.Report) {
	for i := range r.Findings {
		f := &r.Findings[i]
		if f.Node == "" || f.Resource != "" {
			continue
		}
		b := model.BlastRadius{Scope: "node", TotalNodes: len(r.Nodes), NodesAffected: intPtr(1), FailureMode: "unknown", Criticality: "unknown", CriticalityBasis: "not assessed for nodes", Basis: "unknown", Recoverability: model.Recoverability{Rating: "unknown", PDB: "unknown"}}
		if f.Severity == "high" {
			b.FailureMode = "node-unavailable"
		}
		if r.Workloads != nil {
			pods, stateful := 0, false
			ns := map[string]bool{}
			counted := map[string]bool{}
			for _, w := range r.Workloads {
				id := w.Namespace + "/" + w.Kind + "/" + w.Name
				if counted[id] {
					continue
				}
				counted[id] = true
				if n := w.Placements[f.Node]; n > 0 {
					pods += n
					ns[w.Namespace] = true
					stateful = stateful || w.Stateful
				}
			}
			for _, n := range r.Nodes {
				if n.Name == f.Node && n.Pool != "" {
					b.NodePools = []string{n.Pool}
				}
			}
			b.PodsAffected, b.Basis, b.Recoverability.Stateful = intPtr(pods), "counted", stateful
			for n := range ns {
				b.Namespaces = append(b.Namespaces, n)
			}
			sort.Strings(b.Namespaces)
			if stateful {
				b.Recoverability.Rating = "fragile"
			}
		}
		f.BlastRadius = b
	}
	rank := map[string]int{"high": 0, "warning": 1, "note": 2}
	pods := func(f model.Finding) int {
		if f.BlastRadius.PodsAffected == nil {
			return -1
		}
		return *f.BlastRadius.PodsAffected
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		if pods(a) != pods(b) {
			return pods(a) > pods(b)
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Resource+a.Node != b.Resource+b.Node {
			return a.Resource+a.Node < b.Resource+b.Node
		}
		return a.Evidence < b.Evidence
	})
}
