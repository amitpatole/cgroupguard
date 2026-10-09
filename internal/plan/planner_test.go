package plan_test

import (
	"os"
	"strings"
	"testing"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/plan"
	"github.com/amitpatole/cgroupguard/internal/rules"
)

func report(t *testing.T, file, target string) model.Report {
	t.Helper()
	data, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	var s model.Snapshot
	if e = jsonUnmarshalStrict(data, &s); e != nil {
		t.Fatal(e)
	}
	set, e := rules.Load()
	if e != nil {
		t.Fatal(e)
	}
	r, e := rules.EvaluateFor(set, s.Nodes, target)
	if e != nil {
		t.Fatal(e)
	}
	if e = rules.AddWorkloads(&r, s.Workloads); e != nil {
		t.Fatal(e)
	}
	return r
}

func TestPlanGolden(t *testing.T) {
	r := report(t, "../../examples/plan-mixed.json", "1.35")
	p, e := plan.Build(r, plan.Options{StopgapUntil: "2026-12-31"})
	if e != nil {
		t.Fatal(e)
	}
	j, e := p.JSON()
	if e != nil {
		t.Fatal(e)
	}
	again, _ := plan.Build(r, plan.Options{StopgapUntil: "2026-12-31"})
	j2, _ := again.JSON()
	if string(j) != string(j2) {
		t.Fatal("plan is not deterministic")
	}
	for name, got := range map[string]string{"plan.json.golden": string(j), "runbook.md.golden": p.Runbook()} {
		path := "testdata/" + name
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if e := os.WriteFile(path, []byte(got), 0o644); e != nil {
				t.Fatal(e)
			}
		}
		want, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if string(want) != got {
			t.Fatalf("%s differs from reviewed golden output", name)
		}
	}
}

func TestPlanContent(t *testing.T) {
	r := report(t, "../../examples/plan-mixed.json", "1.35")
	p, e := plan.Build(r, plan.Options{})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Groups) != 4 {
		t.Fatalf("groups: %+v", p.Groups)
	}
	if !p.Groups[0].Canary || p.Groups[1].Canary {
		t.Fatalf("canary flags: %+v", p.Groups)
	}
	// smallest measured blast radius first
	for i := 1; i < len(p.Groups); i++ {
		a, b := p.Groups[i-1].Pods, p.Groups[i].Pods
		if a != nil && b != nil && *a > *b {
			t.Fatalf("groups not ordered by pods: %+v", p.Groups)
		}
	}
	text := p.Runbook()
	for _, want := range []string{"--system-config-from-file", "CGROUP_MODE_V2", "amiFamily: AmazonLinux2023", "--os-sku Ubuntu2204", "systemd.unified_cgroup_hierarchy=1", "kubectl drain", "--ignore-daemonsets", "approval required", "Rollback"} {
		if !strings.Contains(text, want) {
			t.Errorf("runbook lacks %q", want)
		}
	}
	// No generated command may bypass eviction protection or relax the kubelet.
	for _, ph := range p.Phases {
		for _, s := range ph.Steps {
			for _, c := range s.Commands {
				for _, forbidden := range []string{"--force", "--disable-eviction", "failCgroupV1"} {
					if strings.Contains(c, forbidden) {
						t.Errorf("step %s command contains %s: %s", s.ID, forbidden, c)
					}
				}
			}
		}
	}
	// No stopgap without an explicit expiry.
	for _, ph := range p.Phases {
		for _, s := range ph.Steps {
			if strings.HasPrefix(s.ID, "S.") {
				t.Fatal("stopgap emitted without --stopgap-until")
			}
		}
	}
}

func TestStopgapNeedsExpiryAndTarget(t *testing.T) {
	r := report(t, "../../examples/plan-mixed.json", "1.35")
	if p, _ := plan.Build(r, plan.Options{StopgapUntil: "2026-12-31"}); !strings.Contains(p.Runbook(), "Temporarily set failCgroupV1: false until 2026-12-31") {
		t.Fatal("stopgap missing")
	}
	if _, e := plan.Build(r, plan.Options{StopgapUntil: "soon"}); e == nil {
		t.Fatal("accepted a non-date expiry")
	}
	old := report(t, "../../examples/plan-mixed.json", "1.34")
	if p, _ := plan.Build(old, plan.Options{StopgapUntil: "2026-12-31"}); strings.Contains(p.Runbook(), "failCgroupV1") {
		t.Fatal("stopgap emitted for a target below 1.35")
	}
}

func TestHostileNamesNeverReachCommands(t *testing.T) {
	r := report(t, "../../examples/plan-mixed.json", "1.35")
	r.Nodes = append(r.Nodes, model.Node{Name: "evil; rm -rf /", OS: "linux", Provider: "unknown", Cgroup: "v1", Evidence: "x"},
		model.Node{Name: "ok-1", OS: "linux", Provider: "gke", Pool: "p; $(id)", Cgroup: "v1", Evidence: "x"})
	p, e := plan.Build(r, plan.Options{})
	if e != nil {
		t.Fatal(e)
	}
	all := p.Runbook()
	j, _ := p.JSON()
	for _, bad := range []string{"rm -rf", "$(id)", "evil;"} {
		if strings.Contains(string(j), bad) || strings.Contains(all, bad) {
			t.Fatalf("hostile text %q reached the plan", bad)
		}
	}
	if !strings.Contains(all, "failed validation") {
		t.Fatal("excluded names are not reported")
	}
}

func TestReportSchemaGate(t *testing.T) {
	if _, e := plan.Build(model.Report{SchemaVersion: 1}, plan.Options{}); e == nil {
		t.Fatal("accepted an old report schema")
	}
}

func TestNoV1MeansNoMigration(t *testing.T) {
	set, _ := rules.Load()
	r, _ := rules.EvaluateFor(set, []model.Node{{Name: "n", OS: "linux", Cgroup: "v2", Evidence: "x"}}, "1.35")
	p, e := plan.Build(r, plan.Options{})
	if e != nil || len(p.Groups) != 0 {
		t.Fatal(e, p.Groups)
	}
	for _, ph := range p.Phases {
		if ph.ID == "P3" {
			t.Fatal("migration phase for a v2-only cluster")
		}
	}
}

func TestDisplayIsInert(t *testing.T) {
	for _, in := range []string{"<script>", "[x](javascript:alert(1))", "a\x1b[31m", "`id`", "a b", "x|y"} {
		out := plan.Display(in)
		if strings.ContainsAny(out, "<>[]()`|\x1b \n") {
			t.Fatalf("%q -> %q", in, out)
		}
	}
	if plan.Display("ns/Deployment/api") != "ns/Deployment/api" {
		t.Fatal("display altered a normal key")
	}
}

func FuzzDisplayAndQuote(f *testing.F) {
	for _, s := range []string{"node-1", "a; rm -rf /", "$(id)", "a'b", "\x1b[31m", "<script>", "`x`", "é", "", "a\nb"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for _, r := range plan.Display(in) {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-/:?.", r)) {
				t.Fatalf("Display(%q) emitted %q", in, r)
			}
		}
		q := plan.Quote(in)
		if plan.SafeName(in) {
			if q != in {
				t.Fatalf("safe name altered: %q -> %q", in, q)
			}
			return
		}
		// Anything else must come back as one single-quoted shell word.
		if len(q) < 2 || q[0] != '\'' || q[len(q)-1] != '\'' {
			t.Fatalf("Quote(%q)=%q is not single-quoted", in, q)
		}
		inner := q[1 : len(q)-1]
		inner = strings.ReplaceAll(inner, `'\''`, "")
		if strings.Contains(inner, "'") {
			t.Fatalf("Quote(%q)=%q has an unescaped quote", in, q)
		}
	})
}
