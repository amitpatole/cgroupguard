package rules

import (
	"github.com/amitpatole/cgroupguard/internal/model"
	"testing"
)

func TestImageBoundaries(t *testing.T) {
	s, e := LoadImages()
	if e != nil {
		t.Fatal(e)
	}
	by := map[string]ImageRule{}
	for _, r := range s.Families {
		by[r.Family] = r
	}
	for _, tc := range []struct{ family, tag, state string }{
		{"hotspot", "8u371-jre", "risk"}, {"hotspot", "8u372-jre", "hint"}, {"hotspot", "11.0.15-jre", "risk"}, {"hotspot", "11.0.16-jre", "hint"}, {"hotspot", "11.0.16-ea", "review"}, {"hotspot", "15.0.0", "hint"}, {"hotspot", "17.0.10-jre", "hint"}, {"hotspot", "11", "review"}, {"hotspot", "latest", "review"}, {"hotspot", "14.0.2", "review"}, {"hotspot", "11.0.16-openj9", "review"},
		{"cadvisor", "v0.42.0", "risk"}, {"cadvisor", "v0.43.0", "hint"}, {"cadvisor", "v0.43.0-rc1", "review"}, {"cadvisor", "", "review"}, {"node", "20.13.1", "hint"}, {"node", "20.3.0", "hint"}, {"node", "20.2.0", "risk"}, {"node", "18.20.4", "risk"}, {"node", "20", "review"}, {"dotnet", "8.0.0", "hint"}, {"dotnet", "6.0", "hint"}, {"dotnet", "3.1.32", "risk"}, {"dotnet", "3.1", "risk"}, {"dotnet", "latest", "review"}, {"node", "20.3", "hint"}, {"node", "20.2", "risk"}, {"node", "22.1", "hint"}, {"hotspot", "11.0", "review"}, {"hotspot", "17.0", "hint"}, {"zulu", "8u372-8.70.0.23", "hint"}, {"zulu", "8u362-8.68.0.19", "risk"}, {"zulu", "11.0.15-11.56.19", "risk"}, {"zulu", "11-latest", "review"}, {"liberica", "11.0.16+8", "hint"}, {"liberica", "8u371", "risk"}, {"fluent-bit", "2.1.10", "risk"}, {"fluent-bit", "2.2.0", "hint"}, {"fluent-bit", "3.0.4", "hint"}, {"elastic-beats", "7.14.2", "risk"}, {"elastic-beats", "8.11.0", "hint"}, {"datadog-agent", "7.50.0", "review"}, {"jvm-vendor", "8u372-al2", "review"}, {"go", "1.25", "hint"}, {"go", "1.25.3-bookworm", "hint"}, {"go", "1.24.9", "review"}, {"go", "latest", "review"}, {"ibm-semeru", "open-17.0.8_7-jre", "review"}, {"monitoring-agent", "v1.7.0", "review"}, {"python", "3.13", "info"}, {"kubernetes-core", "v1.37.0", "info"},
	} {
		t.Run(tc.family+"/"+tc.tag, func(t *testing.T) {
			got, _ := assess(by[tc.family], tc.tag)
			if got != tc.state {
				t.Fatalf("got %s want %s", got, tc.state)
			}
		})
	}
}

func TestRegistryMatching(t *testing.T) {
	for _, tc := range []struct{ image, repo, tag string }{{"node:20.0.0", "docker.io/library/node", "20.0.0"}, {"docker.io/node:20.0.0", "docker.io/library/node", "20.0.0"}, {"index.docker.io/library/node:20", "docker.io/library/node", "20"}, {"evil.example/node:20", "evil.example/node", "20"}, {"index.gcr.io/cadvisor/cadvisor:v0.42.0", "index.gcr.io/cadvisor/cadvisor", "v0.42.0"}, {"registry:5000/node@sha256:abcd", "registry:5000/node", ""}} {
		r, tag := imageParts(tc.image)
		if r != tc.repo || tag != tc.tag {
			t.Fatalf("%s => %s %s", tc.image, r, tag)
		}
	}
}

func TestEveryWorkloadFindingAndCoverage(t *testing.T) {
	for _, tc := range []struct{ image, path, id string }{{"openjdk:11.0.15", "", "CG101"}, {"gcr.io/cadvisor/cadvisor:v0.42.0", "", "CG102"}, {"openjdk:17.0.1", "/sys/fs/cgroup/memory/", "CG103"}, {"private/image:1", "", "CG104"}, {"node:20.0.0", "", "CG108"}, {"dotnet/x:1", "", "CG104"}, {"mcr.microsoft.com/dotnet/runtime:3.1.0", "", "CG109"}, {"docker.io/fluent/fluent-bit:2.1.0", "", "CG110"}, {"python:3.13", "", "CG107"}, {"openjdk:17.0.1", "", "CG106"}} {
		w := model.Workload{Namespace: "n", Kind: "Pod", Name: "p", Container: "c", ContainerType: "regular", Image: tc.image}
		if tc.path != "" {
			w.PathSignals = []string{tc.path}
		}
		r := model.Report{}
		if e := AddWorkloads(&r, []model.Workload{w}); e != nil {
			t.Fatal(e)
		}
		found := false
		for _, f := range r.Findings {
			found = found || f.RuleID == tc.id
		}
		if !found || r.ClusterReadiness != "unknown" {
			t.Fatalf("%+v", r)
		}
	}
	r := model.Report{}
	if e := AddWorkloads(&r, nil); e != nil || r.WorkloadReadiness != "unassessed" {
		t.Fatal(r, e)
	}
	if e := AddWorkloads(&r, []model.Workload{}); e != nil || r.WorkloadReadiness != "no-known-findings" || r.ClusterReadiness != "unknown" {
		t.Fatal(r, e)
	}
}

func TestRejectUntrustedPathAndDuplicates(t *testing.T) {
	w := model.Workload{Namespace: "n", Kind: "Pod", Name: "p", Container: "c", ContainerType: "regular", Image: "node:20", PathSignals: []string{"SECRET"}}
	r := model.Report{}
	if AddWorkloads(&r, []model.Workload{w}) == nil {
		t.Fatal("invalid path accepted")
	}
	w.PathSignals = nil
	if AddWorkloads(&r, []model.Workload{w, w}) == nil {
		t.Fatal("duplicate accepted")
	}
}
