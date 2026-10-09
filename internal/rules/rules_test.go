package rules

import (
	"github.com/amitpatole/cgroupguard/internal/model"
	"testing"
)

func TestReadiness(t *testing.T) {
	s, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		mode, want string
		findings   int
	}{{"v1", "blocked", 1}, {"hybrid", "blocked", 1}, {"unknown", "unknown", 1}, {"v2", "pass", 0}} {
		t.Run(tc.mode, func(t *testing.T) {
			r, e := Evaluate(s, []model.Node{{Name: "worker", OS: "linux", Cgroup: tc.mode, Evidence: "test evidence"}})
			if e != nil {
				t.Fatal(e)
			}
			if r.NodeReadiness != tc.want || len(r.Findings) != tc.findings || r.ClusterReadiness != "unknown" {
				t.Fatalf("unexpected report: %+v", r)
			}
		})
	}
	for _, nodes := range [][]model.Node{nil, {{Name: "x", OS: "linux", Cgroup: "v2"}}, {{Name: "x", OS: "linux", Cgroup: "bogus", Evidence: "x"}}, {{Name: "x", OS: "linux", Cgroup: "v2", Evidence: "x"}, {Name: "x", OS: "linux", Cgroup: "v2", Evidence: "x"}}} {
		if _, e := Evaluate(s, nodes); e == nil {
			t.Fatalf("accepted invalid snapshot %+v", nodes)
		}
	}
}

func TestBlockedDominatesUnknown(t *testing.T) {
	s, _ := Load()
	r, e := Evaluate(s, []model.Node{{Name: "a", OS: "linux", Cgroup: "v1", Evidence: "v1"}, {Name: "b", OS: "linux", Cgroup: "unknown", Evidence: "unprobed"}})
	if e != nil || r.NodeReadiness != "blocked" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestWindowsNotApplicable(t *testing.T) {
	s, _ := Load()
	r, e := Evaluate(s, []model.Node{{Name: "win", OS: "windows"}})
	if e != nil || r.NodeReadiness != "not-applicable" {
		t.Fatalf("%+v %v", r, e)
	}
}
