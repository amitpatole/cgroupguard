package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/amitpatole/cgroupguard/internal/scan"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeScanner struct {
	ev    scan.Evidence
	err   error
	calls int
}

func (f *fakeScanner) Run(context.Context, Spec) (scan.Evidence, error) {
	f.calls++
	return f.ev, f.err
}

func cr(spec map[string]any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "default", "generation": int64(1)}, "spec": spec}}
	o.SetGroupVersionKind(GVK)
	return o
}

type fakeEmitter struct{ Events chan string }

func (f *fakeEmitter) Eventf(_ runtime.Object, eventType, reason, format string, args ...any) {
	f.Events <- eventType + " " + reason + " " + fmt.Sprintf(format, args...)
}

func setup(t *testing.T, obj *unstructured.Unstructured, sc *fakeScanner, now time.Time) (*Reconciler, *fakeEmitter, client.Client, *prometheus.Registry) {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()
	rec := &fakeEmitter{Events: make(chan string, 20)}
	reg := prometheus.NewRegistry()
	return &Reconciler{Client: c, Recorder: rec, Metrics: NewMetrics(reg), Scanner: sc, Now: func() time.Time { return now }}, rec, c, reg
}

func evidence(modes ...string) scan.Evidence {
	var ev scan.Evidence
	for i, m := range modes {
		ev.Nodes = append(ev.Nodes, model.Node{Name: fmt.Sprintf("n%d", i), OS: "linux", Cgroup: m, Evidence: "test statfs"})
	}
	return ev
}

func get(t *testing.T, c client.Client) *unstructured.Unstructured {
	t.Helper()
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(GVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: "default"}, o); err != nil {
		t.Fatal(err)
	}
	return o
}

func gauge(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) (float64, bool) {
	t.Helper()
	fams, _ := reg.Gather()
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if matches(m, labels) {
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func matches(m *dto.Metric, labels map[string]string) bool {
	got := map[string]string{}
	for _, l := range m.GetLabel() {
		got[l.GetName()] = l.GetValue()
	}
	for k, v := range labels {
		if got[k] != v {
			return false
		}
	}
	return true
}

func TestSpecValidation(t *testing.T) {
	good := map[string]any{"target": "1.35", "intervalSeconds": int64(600), "includeWorkloads": true, "workloadNamespace": "shop", "statusDetail": "preview"}
	if sp, err := ParseSpec(good); err != nil || sp.Interval != 10*time.Minute || sp.WorkloadNamespace != "shop" {
		t.Fatalf("%+v %v", sp, err)
	}
	if sp, err := ParseSpec(nil); err != nil || sp.Interval != time.Hour || sp.Target != "1.35" || sp.StatusDetail != "summary" {
		t.Fatalf("defaults: %+v %v", sp, err)
	}
	for name, bad := range map[string]map[string]any{
		"unknown field":    {"prometheusURL": "http://169.254.169.254/"},
		"bad target":       {"target": "2.0"},
		"short interval":   {"intervalSeconds": int64(5)},
		"long interval":    {"intervalSeconds": int64(10_000_000)},
		"bad namespace":    {"workloadNamespace": "Not A Name"},
		"namespace w/o wl": {"workloadNamespace": "shop", "includeWorkloads": false},
		"bad detail":       {"statusDetail": "everything"},
		"wrong type":       {"intervalSeconds": "600"},
	} {
		if _, err := ParseSpec(bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSuccessfulScanWritesStatusMetricsAndEvents(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	sc := &fakeScanner{ev: evidence("v1", "v2", "unknown")}
	r, rec, c, reg := setup(t, cr(map[string]any{"target": "1.35"}), sc, now)
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}})
	if err != nil || res.RequeueAfter != time.Hour {
		t.Fatalf("%v %+v", err, res)
	}
	st, _, _ := unstructured.NestedMap(get(t, c).Object, "status")
	if v, _, _ := unstructured.NestedInt64(st, "nodes", "cgroupV1"); v != 1 {
		t.Fatalf("status: %v", st)
	}
	if st["nodeReadiness"] != "blocked" || st["clusterReadiness"] != "unknown" {
		t.Fatalf("readiness: %v", st)
	}
	if _, has := st["findingsPreview"]; has {
		t.Fatal("summary detail leaked finding locations")
	}
	if v, ok := gauge(t, reg, "cgroupguard_nodes_cgroup_v1", map[string]string{"name": "default"}); !ok || v != 1 {
		t.Fatalf("metric %v %v", v, ok)
	}
	if v, _ := gauge(t, reg, "cgroupguard_findings", map[string]string{"name": "default", "severity": "high"}); v != 1 {
		t.Fatalf("findings metric %v", v)
	}
	if ev := <-rec.Events; !strings.Contains(ev, "NodesOnCgroupV1") {
		t.Fatalf("event %q", ev)
	}
	// A second reconcile inside the interval does not scan again.
	if res, _ := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}}); sc.calls != 1 || res.RequeueAfter <= 0 {
		t.Fatalf("rescanned while fresh: calls=%d %+v", sc.calls, res)
	}
}

func TestMetricsNeverCarryNames(t *testing.T) {
	r, _, _, reg := setup(t, cr(nil), &fakeScanner{ev: evidence("v1")}, time.Unix(1_800_000_000, 0))
	_, _ = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}})
	fams, _ := reg.Gather()
	for _, f := range fams {
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if strings.Contains(l.GetValue(), "n0") {
					t.Fatalf("node name in metric label %s=%s", l.GetName(), l.GetValue())
				}
			}
		}
	}
}

func TestFailedScanKeepsTheLastGoodStatus(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	sc := &fakeScanner{ev: evidence("v2", "v2")}
	r, rec, c, reg := setup(t, cr(nil), sc, now)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}}
	_, _ = r.Reconcile(context.Background(), req)
	<-rec.Events
	// Force the next reconcile to scan again, and make it fail.
	obj := get(t, c)
	_ = unstructured.SetNestedField(obj.Object, int64(2), "metadata", "generation")
	_ = c.Update(context.Background(), obj)
	r.Now = func() time.Time { return now.Add(2 * time.Hour) }
	sc.err = fmt.Errorf("list nodes: forbidden")
	res, err := r.Reconcile(context.Background(), req)
	if err != nil || res.RequeueAfter != 5*time.Minute {
		t.Fatalf("%v %+v", err, res)
	}
	st, _, _ := unstructured.NestedMap(get(t, c).Object, "status")
	if v, _, _ := unstructured.NestedInt64(st, "nodes", "cgroupV2"); v != 2 {
		t.Fatalf("last good result was blanked: %v", st)
	}
	conds := st["conditions"].([]any)
	if c0 := conds[0].(map[string]any); c0["status"] != "False" || c0["reason"] != "ScanFailed" {
		t.Fatalf("%v", c0)
	}
	if v, ok := counter(reg, "cgroupguard_scan_failures_total"); !ok || v != 1 {
		t.Fatalf("failure counter %v %v", v, ok)
	}
	if ev := <-rec.Events; !strings.Contains(ev, "ScanFailed") {
		t.Fatalf("%q", ev)
	}
}

func counter(reg *prometheus.Registry, name string) (float64, bool) {
	fams, _ := reg.Gather()
	for _, f := range fams {
		if f.GetName() == name && len(f.GetMetric()) > 0 {
			return f.GetMetric()[0].GetCounter().GetValue(), true
		}
	}
	return 0, false
}

func TestInvalidSpecIsReportedNotRetried(t *testing.T) {
	sc := &fakeScanner{}
	r, rec, c, _ := setup(t, cr(map[string]any{"intervalSeconds": int64(1)}), sc, time.Unix(1_800_000_000, 0))
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}})
	if err != nil || res.RequeueAfter != 0 || sc.calls != 0 {
		t.Fatalf("%v %+v calls=%d", err, res, sc.calls)
	}
	st, _, _ := unstructured.NestedMap(get(t, c).Object, "status")
	if cond := st["conditions"].([]any)[0].(map[string]any); cond["reason"] != "InvalidSpec" {
		t.Fatalf("%v", cond)
	}
	if ev := <-rec.Events; !strings.Contains(ev, "InvalidSpec") {
		t.Fatalf("%q", ev)
	}
}

func TestEventsOnlyOnChange(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	sc := &fakeScanner{ev: evidence("v1")}
	r, rec, c, _ := setup(t, cr(nil), sc, now)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}}
	_, _ = r.Reconcile(context.Background(), req)
	<-rec.Events
	bump := func(gen int64, at time.Time) {
		obj := get(t, c)
		_ = unstructured.SetNestedField(obj.Object, gen, "metadata", "generation")
		_ = c.Update(context.Background(), obj)
		r.Now = func() time.Time { return at }
	}
	bump(2, now.Add(2*time.Hour))
	_, _ = r.Reconcile(context.Background(), req) // same picture: no event
	select {
	case ev := <-rec.Events:
		t.Fatalf("event on an unchanged picture: %q", ev)
	default:
	}
	sc.ev = evidence("v2")
	bump(3, now.Add(4*time.Hour))
	_, _ = r.Reconcile(context.Background(), req)
	if ev := <-rec.Events; !strings.Contains(ev, "NoNodesOnCgroupV1") {
		t.Fatalf("%q", ev)
	}
}

func TestDeletionForgetsMetrics(t *testing.T) {
	r, _, c, reg := setup(t, cr(nil), &fakeScanner{ev: evidence("v1")}, time.Unix(1_800_000_000, 0))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "default"}}
	_, _ = r.Reconcile(context.Background(), req)
	if _, ok := gauge(t, reg, "cgroupguard_nodes_cgroup_v1", map[string]string{"name": "default"}); !ok {
		t.Fatal("metric missing")
	}
	_ = c.Delete(context.Background(), get(t, c))
	_, _ = r.Reconcile(context.Background(), req)
	if _, ok := gauge(t, reg, "cgroupguard_nodes_cgroup_v1", map[string]string{"name": "default"}); ok {
		t.Fatal("series of a deleted resource remained")
	}
}

func TestPreviewDetailIsCappedAndClipped(t *testing.T) {
	var rep model.Report
	for i := 0; i < 50; i++ {
		rep.Findings = append(rep.Findings, model.Finding{RuleID: "CG104", Severity: "warning", Resource: strings.Repeat("x", 500) + "\x1b[31m"})
	}
	st := BuildStatus(rep, Spec{StatusDetail: "preview"}, 1, time.Unix(1, 0), nil, nil)
	prev := st["findingsPreview"].([]any)
	if len(prev) != maxPreview {
		t.Fatalf("preview not capped: %d", len(prev))
	}
	if res := prev[0].(map[string]any)["resource"].(string); len(res) > 210 || strings.Contains(res, "\x1b") {
		t.Fatalf("resource not clipped: %d %q", len(res), res)
	}
}

func FuzzParseSpec(f *testing.F) {
	f.Add("1.35", int64(600), "shop", "summary")
	f.Add("v1.34.2", int64(300), "", "preview")
	f.Add("x", int64(-1), "a b", "z")
	f.Fuzz(func(t *testing.T, target string, secs int64, ns, detail string) {
		sp, err := ParseSpec(map[string]any{"target": target, "intervalSeconds": secs, "workloadNamespace": ns, "statusDetail": detail})
		if err != nil {
			return
		}
		if sp.Interval < MinInterval || sp.Interval > MaxInterval {
			t.Fatalf("interval %v accepted", sp.Interval)
		}
		if sp.WorkloadNamespace != "" && !nsRe.MatchString(sp.WorkloadNamespace) {
			t.Fatalf("namespace %q accepted", sp.WorkloadNamespace)
		}
		if sp.StatusDetail != "summary" && sp.StatusDetail != "preview" {
			t.Fatalf("detail %q accepted", sp.StatusDetail)
		}
	})
}
