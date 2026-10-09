package model

import "fmt"

type Node struct {
	Name     string `json:"name"`
	UID      string `json:"uid,omitempty"`
	OS       string `json:"os"`
	OSImage  string `json:"osImage,omitempty"`
	Kernel   string `json:"kernel,omitempty"`
	Kubelet  string `json:"kubelet,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Provider string `json:"provider"`
	Pool     string `json:"pool,omitempty"`
	Arch     string `json:"arch,omitempty"`         // kubernetes node architecture, e.g. amd64
	Instance string `json:"instanceType,omitempty"` // node.kubernetes.io/instance-type label
	Cgroup   string `json:"cgroup"`
	Evidence string `json:"evidence"`
}

type Snapshot struct {
	SchemaVersion int        `json:"schemaVersion"`
	Nodes         []Node     `json:"nodes"`
	Workloads     []Workload `json:"workloads,omitempty"`
}

// Workload deliberately excludes raw commands, environment, annotations and secrets.
type Workload struct {
	Namespace     string   `json:"namespace"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Container     string   `json:"container"`
	ContainerType string   `json:"containerType"`
	Image         string   `json:"image"`
	PathSignals   []string `json:"pathSignals,omitempty"`
	// Optional placement evidence. Pods owned by a listed controller are folded
	// into that controller; Placements counts pods per node name.
	Replicas   *int32         `json:"replicas,omitempty"`
	Placements map[string]int `json:"placements,omitempty"`
	Stateful   bool           `json:"stateful,omitempty"`
	// PDB is "present", "none" or "unknown" (empty means not collected).
	PDB string `json:"pdb,omitempty"`
}

// Recoverability describes how well the affected workload tolerates disruption.
type Recoverability struct {
	Rating   string `json:"rating"` // protected | fragile | unknown
	Replicas *int32 `json:"replicas,omitempty"`
	PDB      string `json:"pdb"`
	Stateful bool   `json:"stateful"`
}

// BlastRadius is computed from inventory; Basis says how much of it was measured.
type BlastRadius struct {
	Scope            string         `json:"scope"` // workload | node | nodePool | cluster
	NodesAffected    *int           `json:"nodesAffected,omitempty"`
	TotalNodes       int            `json:"totalNodes"`
	PodsAffected     *int           `json:"podsAffected,omitempty"`
	Namespaces       []string       `json:"namespaces,omitempty"`
	NodePools        []string       `json:"nodePools,omitempty"` // known pools of the affected nodes
	FailureMode      string         `json:"failureMode"`         // node-unavailable | crash | silent-degradation | observability-loss | unknown
	Recoverability   Recoverability `json:"recoverability"`
	Criticality      string         `json:"criticality"` // system | node-wide | workload | unknown
	CriticalityBasis string         `json:"criticalityBasis"`
	Basis            string         `json:"basis"` // counted | unknown
}

type Finding struct {
	RuleID      string      `json:"ruleId"`
	Node        string      `json:"node"`
	Resource    string      `json:"resource,omitempty"`
	Severity    string      `json:"severity"`
	Evidence    string      `json:"evidence"`
	Why         string      `json:"why"`
	BlastRadius BlastRadius `json:"blastRadius"`
	Confidence  string      `json:"confidence"`
	Source      string      `json:"source"`
}

type Report struct {
	SchemaVersion     int        `json:"schemaVersion"`
	Target            string     `json:"target"`
	Scope             string     `json:"scope"`
	NodeReadiness     string     `json:"nodeReadiness"`
	ClusterReadiness  string     `json:"clusterReadiness"`
	Limitations       []string   `json:"limitations"`
	Nodes             []Node     `json:"nodes"`
	Findings          []Finding  `json:"findings"`
	Workloads         []Workload `json:"workloads,omitempty"`
	WorkloadReadiness string     `json:"workloadReadiness"`
}

// Summary is a one-line human description of the blast radius.
func (b BlastRadius) Summary() string {
	count := func(p *int, unit string) string {
		if p == nil {
			return "unknown " + unit
		}
		return fmt.Sprintf("%d %s", *p, unit)
	}
	pods := count(b.PodsAffected, "pods")
	nodes := count(b.NodesAffected, "nodes")
	if b.NodesAffected != nil && b.TotalNodes > 0 {
		nodes = fmt.Sprintf("%d of %d nodes", *b.NodesAffected, b.TotalNodes)
	}
	return fmt.Sprintf("scope=%s; %s; %s; failure=%s; recoverability=%s (pdb=%s, stateful=%t); criticality=%s (%s); basis=%s",
		b.Scope, pods, nodes, b.FailureMode, b.Recoverability.Rating, b.Recoverability.PDB, b.Recoverability.Stateful, b.Criticality, b.CriticalityBasis, b.Basis)
}
