package rules

import (
	"github.com/amitpatole/cgroupguard/internal/model"
	"testing"
)

func i32(n int32) *int32 { return &n }

func TestUnverifiedFamiliesCannotBecomeThresholds(t *testing.T) {
	bad := ImageRules{Version: 2, Families: []ImageRule{{Family: "x", Class: "runtime", Status: "unverified", Mode: "threshold", RiskID: "CG999", FailureMode: "unknown", Repositories: []string{"docker.io/library/x"}, Source: "https://example.org", Minimums: map[string]string{"all": "1.0.0"}}}}
	if validateImages(bad) == nil {
		t.Fatal("unverified threshold accepted")
	}
	bad.Families[0].Mode = "native-from"
	bad.Families[0].NativeFrom = "1.2"
	if validateImages(bad) == nil {
		t.Fatal("unverified native-from accepted")
	}
	bad.Families[0].Mode = "review"
	if e := validateImages(bad); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadImages(); e != nil {
		t.Fatal(e)
	}
}

func TestWorkloadBlastRadiusMeasured(t *testing.T) {
	r := model.Report{Nodes: []model.Node{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	w := []model.Workload{
		{Namespace: "kube-system", Kind: "DaemonSet", Name: "agent", Container: "c", ContainerType: "regular", Image: "gcr.io/cadvisor/cadvisor:v0.42.0", Replicas: i32(3), PDB: "none", Placements: map[string]int{"a": 1, "b": 1, "c": 1}},
		{Namespace: "shop", Kind: "Deployment", Name: "api", Container: "c", ContainerType: "regular", Image: "openjdk:11.0.15", Replicas: i32(3), PDB: "present", Placements: map[string]int{"a": 2, "b": 1}},
		{Namespace: "shop", Kind: "StatefulSet", Name: "db", Container: "c", ContainerType: "regular", Image: "openjdk:11.0.15", Replicas: i32(1), Stateful: true, PDB: "none", Placements: map[string]int{"c": 1}},
	}
	if e := AddWorkloads(&r, w); e != nil {
		t.Fatal(e)
	}
	got := map[string]model.BlastRadius{}
	for _, f := range r.Findings {
		got[f.Resource] = f.BlastRadius
	}
	ds := got["kube-system/DaemonSet/agent/regular/c"]
	if ds.Scope != "cluster" || *ds.PodsAffected != 3 || *ds.NodesAffected != 3 || ds.Criticality != "system" || ds.Basis != "counted" || ds.FailureMode != "observability-loss" || ds.Recoverability.Rating != "fragile" {
		t.Fatalf("daemonset %+v", ds)
	}
	api := got["shop/Deployment/api/regular/c"]
	if api.Scope != "workload" || *api.PodsAffected != 3 || *api.NodesAffected != 2 || api.Recoverability.Rating != "protected" {
		t.Fatalf("deployment %+v", api)
	}
	if db := got["shop/StatefulSet/db/regular/c"]; db.Recoverability.Rating != "fragile" || !db.Recoverability.Stateful {
		t.Fatalf("statefulset %+v", db)
	}
	// Biggest impact leads within a severity.
	if r.Findings[0].Resource != "kube-system/DaemonSet/agent/regular/c" && r.Findings[0].Resource != "shop/Deployment/api/regular/c" {
		t.Fatalf("not ordered by pods affected: %s", r.Findings[0].Resource)
	}
}

func TestBlastRadiusUnknownWithoutEvidence(t *testing.T) {
	r := model.Report{}
	w := model.Workload{Namespace: "n", Kind: "Deployment", Name: "d", Container: "c", ContainerType: "regular", Image: "openjdk:11.0.15"}
	if e := AddWorkloads(&r, []model.Workload{w}); e != nil {
		t.Fatal(e)
	}
	b := r.Findings[0].BlastRadius
	if b.PodsAffected != nil || b.NodesAffected != nil || b.Basis != "unknown" || b.Recoverability.Rating != "unknown" {
		t.Fatalf("guessed counts: %+v", b)
	}
}

func TestNodeBlastRadiusCountsPods(t *testing.T) {
	s, _ := Load()
	r, e := Evaluate(s, []model.Node{{Name: "a", OS: "linux", Cgroup: "v1", Evidence: "x"}, {Name: "b", OS: "linux", Cgroup: "v2", Evidence: "x"}})
	if e != nil {
		t.Fatal(e)
	}
	if b := r.Findings[0].BlastRadius; b.Basis != "unknown" || b.PodsAffected != nil || b.FailureMode != "node-unavailable" {
		t.Fatalf("%+v", b)
	}
	w := []model.Workload{
		{Namespace: "n1", Kind: "Deployment", Name: "d", Container: "c1", ContainerType: "regular", Image: "x/y:1", Replicas: i32(2), PDB: "none", Placements: map[string]int{"a": 2}},
		{Namespace: "n1", Kind: "Deployment", Name: "d", Container: "c2", ContainerType: "regular", Image: "x/y:1", Replicas: i32(2), PDB: "none", Placements: map[string]int{"a": 2}},
		{Namespace: "n2", Kind: "StatefulSet", Name: "s", Container: "c", ContainerType: "regular", Image: "x/y:1", Replicas: i32(1), Stateful: true, Placements: map[string]int{"a": 1, "b": 1}},
	}
	if e = AddWorkloads(&r, w); e != nil {
		t.Fatal(e)
	}
	var b model.BlastRadius
	for _, f := range r.Findings {
		if f.Node == "a" {
			b = f.BlastRadius
		}
	}
	if b.Basis != "counted" || *b.PodsAffected != 3 || len(b.Namespaces) != 2 || !b.Recoverability.Stateful {
		t.Fatalf("pods double-counted or wrong: %+v", b)
	}
}

func TestTargetVersionScalesNodeSeverity(t *testing.T) {
	s, _ := Load()
	node := []model.Node{{Name: "a", OS: "linux", Cgroup: "v1", Evidence: "x"}}
	r, _ := EvaluateFor(s, node, "1.35")
	if r.NodeReadiness != "blocked" || r.Findings[0].Severity != "high" {
		t.Fatalf("%+v", r)
	}
	r, _ = EvaluateFor(s, node, "v1.34.2")
	if r.NodeReadiness != "needs-migration" || r.Findings[0].Severity != "warning" || r.Target != "1.34" {
		t.Fatalf("%+v", r)
	}
	for _, bad := range []string{"", "abc", "2.0", "1.5", "1.35.x", "1"} {
		if _, e := EvaluateFor(s, node, bad); e == nil {
			t.Fatalf("accepted target %q", bad)
		}
	}
}

func TestNodePoolScopeAndGrouping(t *testing.T) {
	nodes := []model.Node{{Name: "a", Pool: "pool-1"}, {Name: "b", Pool: "pool-1"}, {Name: "c", Pool: "pool-2"}, {Name: "d"}}
	mk := func(place map[string]int) model.BlastRadius {
		r := model.Report{Nodes: nodes}
		w := model.Workload{Namespace: "n", Kind: "DaemonSet", Name: "agent", Container: "c", ContainerType: "regular", Image: "gcr.io/cadvisor/cadvisor:v0.42.0", Replicas: i32(int32(len(place))), Placements: place}
		if e := AddWorkloads(&r, []model.Workload{w}); e != nil {
			t.Fatal(e)
		}
		return r.Findings[0].BlastRadius
	}
	if b := mk(map[string]int{"a": 1, "b": 1}); b.Scope != "nodePool" || len(b.NodePools) != 1 || b.NodePools[0] != "pool-1" {
		t.Fatalf("%+v", b)
	}
	if b := mk(map[string]int{"a": 1, "c": 1}); b.Scope != "node" || len(b.NodePools) != 2 {
		t.Fatalf("%+v", b)
	}
	// A node without a known pool must not be silently attributed to a pool scope.
	if b := mk(map[string]int{"a": 1, "d": 1}); b.Scope != "node" {
		t.Fatalf("%+v", b)
	}
	if b := mk(map[string]int{"a": 1, "b": 1, "c": 1, "d": 1}); b.Scope != "cluster" {
		t.Fatalf("%+v", b)
	}
}

func FuzzImageRulesAssess(f *testing.F) {
	for _, s := range []string{"openjdk:11.0.15", "node:20", "node:20.3", "golang:1.25.1-bookworm", "private/x@sha256:abcd", "registry:5000/a/b:latest", "8u372-b07-jre", ""} {
		f.Add(s)
	}
	set, e := LoadImages()
	if e != nil {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, image string) {
		repo, tag := imageParts(image)
		for _, rule := range set.Families {
			state, why := assess(rule, tag)
			switch state {
			case "risk", "hint", "review", "info":
			default:
				t.Fatalf("invalid state %q for %q (%s)", state, image, repo)
			}
			// An unverified family must never produce a threshold verdict.
			if rule.Status == "unverified" && (state == "risk" || state == "hint") {
				t.Fatalf("unverified family %s returned %s for %q", rule.Family, state, image)
			}
			_ = why
		}
	})
}

func FuzzParseTarget(f *testing.F) {
	for _, s := range []string{"1.35", "v1.35.2", "", "1.5", "9.9", "1.35.x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if v, e := ParseTarget(in); e == nil && !targetPattern.MatchString(v) && v != "" {
			if _, ok := majorMinor(v); !ok {
				t.Fatalf("bad normalized target %q", v)
			}
		}
	})
}
