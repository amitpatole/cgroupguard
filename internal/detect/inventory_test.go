package detect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/amitpatole/cgroupguard/internal/model"
)

func owner(kind, name string) []meta.OwnerReference {
	yes := true
	return []meta.OwnerReference{{Kind: kind, Name: name, Controller: &yes}}
}

func spec(node, image string) core.PodSpec {
	return core.PodSpec{NodeName: node, Containers: []core.Container{{Name: "app", Image: image}}}
}

func TestPodsFoldIntoControllersAndPlacementsAreCounted(t *testing.T) {
	two := int32(2)
	lbl := map[string]string{"app": "api"}
	objs := []runtime.Object{
		&apps.Deployment{ObjectMeta: meta.ObjectMeta{Name: "api", Namespace: "n"}, Spec: apps.DeploymentSpec{Replicas: &two, Template: core.PodTemplateSpec{ObjectMeta: meta.ObjectMeta{Labels: lbl}, Spec: spec("", "openjdk:11.0.15")}}},
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "api-abc12-x1", Namespace: "n", OwnerReferences: owner("ReplicaSet", "api-abc12")}, Spec: spec("node-a", "openjdk:11.0.15")},
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "api-abc12-x2", Namespace: "n", OwnerReferences: owner("ReplicaSet", "api-abc12")}, Spec: spec("node-b", "openjdk:11.0.15")},
		&policy.PodDisruptionBudget{ObjectMeta: meta.ObjectMeta{Name: "api", Namespace: "n"}, Spec: policy.PodDisruptionBudgetSpec{Selector: &meta.LabelSelector{MatchLabels: lbl}}},
		&batch.CronJob{ObjectMeta: meta.ObjectMeta{Name: "nightly", Namespace: "n"}, Spec: batch.CronJobSpec{JobTemplate: batch.JobTemplateSpec{Spec: batch.JobSpec{Template: core.PodTemplateSpec{Spec: spec("", "node:20.13.1")}}}}},
		&batch.Job{ObjectMeta: meta.ObjectMeta{Name: "nightly-123", Namespace: "n", OwnerReferences: owner("CronJob", "nightly")}, Spec: batch.JobSpec{Template: core.PodTemplateSpec{Spec: spec("", "node:20.13.1")}}},
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "nightly-123-p", Namespace: "n", OwnerReferences: owner("Job", "nightly-123")}, Spec: spec("node-a", "node:20.13.1")},
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "lonely", Namespace: "n"}, Spec: spec("node-b", "busybox:1")},
		// ReplicaSet-owned pod whose Deployment is not listed stays visible.
		&core.Pod{ObjectMeta: meta.ObjectMeta{Name: "ghost-abc-1", Namespace: "n", OwnerReferences: owner("ReplicaSet", "ghost-abc")}, Spec: spec("node-b", "busybox:2")},
	}
	w, limits, e := Workloads(context.Background(), fake.NewClientset(objs...), "")
	if e != nil || len(limits) != 0 {
		t.Fatal(e, limits)
	}
	byName := map[string]model.Workload{}
	for _, x := range w {
		byName[x.Kind+"/"+x.Name] = x
	}
	if len(w) != 4 {
		t.Fatalf("expected Deployment, CronJob, 2 standalone pods; got %d: %v", len(w), keys(w))
	}
	d := byName["Deployment/api"]
	if d.Placements["node-a"] != 1 || d.Placements["node-b"] != 1 || *d.Replicas != 2 || d.PDB != "present" {
		t.Fatalf("%+v", d)
	}
	cj := byName["CronJob/nightly"]
	if cj.Placements["node-a"] != 1 || cj.PDB != "none" {
		t.Fatalf("cronjob %+v", cj)
	}
	if _, dup := byName["Job/nightly-123"]; dup {
		t.Fatal("cronjob child job reported twice")
	}
	if l := byName["Pod/lonely"]; l.Placements["node-b"] != 1 || *l.Replicas != 1 {
		t.Fatalf("%+v", l)
	}
	if _, ok := byName["Pod/ghost-abc-1"]; !ok {
		t.Fatal("orphan replicaset pod hidden")
	}
	if _, ok := byName["Pod/api-abc12-x1"]; ok {
		t.Fatal("controller-owned pod reported separately")
	}
}

func keys(w []model.Workload) []string {
	var k []string
	for _, x := range w {
		k = append(k, WorkloadKey(x))
	}
	return k
}

func TestPDBReadFailureIsLimitationNotError(t *testing.T) {
	c := fake.NewClientset(&apps.Deployment{ObjectMeta: meta.ObjectMeta{Name: "d", Namespace: "n"}, Spec: apps.DeploymentSpec{Template: core.PodTemplateSpec{Spec: spec("", "x/y:1")}}})
	c.PrependReactor("list", "poddisruptionbudgets", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("forbidden") })
	w, limits, e := Workloads(context.Background(), c, "")
	if e != nil || len(w) != 1 || w[0].PDB != "unknown" || len(limits) != 1 || !strings.Contains(limits[0], "PodDisruptionBudgets") {
		t.Fatal(w, limits, e)
	}
}

func TestStatefulDetection(t *testing.T) {
	sp := spec("", "x/y:1")
	sp.Volumes = []core.Volume{{Name: "d", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "c"}}}}
	c := fake.NewClientset(&apps.Deployment{ObjectMeta: meta.ObjectMeta{Name: "d", Namespace: "n"}, Spec: apps.DeploymentSpec{Template: core.PodTemplateSpec{Spec: sp}}}, &apps.StatefulSet{ObjectMeta: meta.ObjectMeta{Name: "s", Namespace: "n"}, Spec: apps.StatefulSetSpec{VolumeClaimTemplates: []core.PersistentVolumeClaim{{}}, Template: core.PodTemplateSpec{Spec: spec("", "x/y:1")}}})
	w, _, _ := Workloads(context.Background(), c, "")
	for _, x := range w {
		if !x.Stateful {
			t.Fatalf("%s not stateful", x.Name)
		}
	}
}

func TestParseCgroupVersionMetric(t *testing.T) {
	ok := "# HELP kubelet_cgroup_version x\n# TYPE kubelet_cgroup_version gauge\nkubelet_cgroup_version 2\nkubelet_cgroup_version_other 9\n"
	if v, e := ParseCgroupVersionMetric(strings.NewReader(ok)); e != nil || v != 2 {
		t.Fatal(v, e)
	}
	if v, e := ParseCgroupVersionMetric(strings.NewReader(`kubelet_cgroup_version{a="b"} 1`)); e != nil || v != 1 {
		t.Fatal(v, e)
	}
	for _, bad := range []string{"", "other_metric 2\n", "kubelet_cgroup_version 3\n", "kubelet_cgroup_version NaN\n", "kubelet_cgroup_version\n", "kubelet_cgroup_version 1\nkubelet_cgroup_version 2\n"} {
		if _, e := ParseCgroupVersionMetric(strings.NewReader(bad)); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestApplyKubeletMetricsKeepsUnknownOnFailure(t *testing.T) {
	nodes := []model.Node{{Name: "a", OS: "linux", Cgroup: "unknown"}, {Name: "b", OS: "linux", Cgroup: "unknown"}, {Name: "c", OS: "linux", Cgroup: "v1"}, {Name: "w", OS: "windows", Cgroup: "unknown"}}
	fetch := func(_ context.Context, n string) (io.ReadCloser, error) {
		if n == "b" {
			return nil, fmt.Errorf("forbidden")
		}
		return io.NopCloser(strings.NewReader("kubelet_cgroup_version 2\n")), nil
	}
	limits := ApplyKubeletMetrics(context.Background(), nodes, fetch)
	if nodes[0].Cgroup != "v2" || nodes[1].Cgroup != "unknown" || nodes[2].Cgroup != "v1" || nodes[3].Cgroup != "unknown" || len(limits) != 1 {
		t.Fatalf("%+v %v", nodes, limits)
	}
}

func TestPrometheusValidation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := func(samples ...string) string {
		return `{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(samples, ",") + `]}}`
	}
	sample := func(label, node string, age time.Duration, val string) string {
		return fmt.Sprintf(`{"metric":{%q:%q},"value":[%d,%q]}`, label, node, now.Add(-age).Unix(), val)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" || r.URL.Query().Get("query") != "kubelet_cgroup_version" {
			http.Error(w, "bad", 400)
			return
		}
		io.WriteString(w, body(
			sample("node", "fresh", time.Minute, "2"),
			sample("instance", "inst:10250", time.Minute, "1"),
			sample("node", "stale", time.Hour, "2"),
			sample("node", "dup", time.Minute, "1"), sample("node", "dup", time.Minute, "2"),
			sample("node", "other-cluster-node", time.Minute, "2"),
			sample("node", "weird", time.Minute, "7"),
		))
	}))
	defer srv.Close()
	nodes := []model.Node{}
	for _, n := range []string{"fresh", "inst", "stale", "dup", "weird", "missing"} {
		nodes = append(nodes, model.Node{Name: n, OS: "linux", Cgroup: "unknown"})
	}
	limits, e := PrometheusCgroup(context.Background(), srv.Client(), srv.URL, nodes, now)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]string{"fresh": "v2", "inst": "v1", "stale": "unknown", "dup": "unknown", "weird": "unknown", "missing": "unknown"}
	for _, n := range nodes {
		if n.Cgroup != want[n.Name] {
			t.Fatalf("%s=%s want %s", n.Name, n.Cgroup, want[n.Name])
		}
	}
	if len(limits) != 4 {
		t.Fatalf("limits %v", limits)
	}
	for _, bad := range []string{"ftp://x", "http://user:pw@x", "notaurl", "http://"} {
		if _, e := PrometheusCgroup(context.Background(), srv.Client(), bad, nodes, now); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestProbeConflictsWithMetricsAreUnknown(t *testing.T) {
	n := model.Node{Name: "a", Cgroup: "v2", Evidence: "metric says v2"}
	mergeProbe(&n, "v1", "statfs tmpfs", "uid1")
	if n.Cgroup != "unknown" || !strings.Contains(n.Evidence, "conflicting evidence") {
		t.Fatalf("%+v", n)
	}
	n = model.Node{Name: "a", Cgroup: "v2", Evidence: "metric says v2"}
	mergeProbe(&n, "v2", "statfs cgroup2fs", "uid1")
	if n.Cgroup != "v2" || !strings.Contains(n.Evidence, "uid1") {
		t.Fatalf("%+v", n)
	}
	n = model.Node{Name: "a", Cgroup: "unknown", Evidence: "inventory"}
	mergeProbe(&n, "v1", "statfs tmpfs", "uid1")
	if n.Cgroup != "v1" {
		t.Fatalf("%+v", n)
	}
}

func TestNodeEventsUpgradeUnknownOnly(t *testing.T) {
	ev := func(node, reason, kind string) *core.Event {
		return &core.Event{ObjectMeta: meta.ObjectMeta{Name: node + reason, Namespace: "default"}, Reason: reason, InvolvedObject: core.ObjectReference{Kind: kind, Name: node}, LastTimestamp: meta.NewTime(time.Unix(1_800_000_000, 0))}
	}
	c := fake.NewClientset(ev("a", "CgroupV1", "Node"), ev("b", "CgroupV1", "Node"), ev("c", "Other", "Node"), ev("d", "CgroupV1", "Pod"))
	nodes := []model.Node{{Name: "a", OS: "linux", Cgroup: "unknown"}, {Name: "b", OS: "linux", Cgroup: "v2", Evidence: "metric"}, {Name: "c", OS: "linux", Cgroup: "unknown"}, {Name: "d", OS: "linux", Cgroup: "unknown"}}
	limits, e := ApplyNodeEvents(context.Background(), c, nodes)
	if e != nil {
		t.Fatal(e)
	}
	if nodes[0].Cgroup != "v1" || nodes[1].Cgroup != "v2" || nodes[2].Cgroup != "unknown" || nodes[3].Cgroup != "unknown" || len(limits) != 1 {
		t.Fatalf("%+v %v", nodes, limits)
	}
}
