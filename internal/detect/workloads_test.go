package detect

import (
	"context"
	"encoding/json"
	"fmt"
	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
)

func TestAllResourcesAndPrivacy(t *testing.T) {
	m := meta.ObjectMeta{Name: "app", Namespace: "n"}
	spec := core.PodSpec{Containers: []core.Container{{Name: "app", Image: "openjdk:11.0.15", Command: []string{"cat /sys/fs/cgroup/memory/memory.limit_in_bytes; SECRET"}, Env: []core.EnvVar{{Name: "TOKEN", Value: "SECRET"}}}}, InitContainers: []core.Container{{Name: "init", Image: "busybox:1"}}, EphemeralContainers: []core.EphemeralContainer{{EphemeralContainerCommon: core.EphemeralContainerCommon{Name: "debug", Image: "busybox:1"}}}}
	// Ephemeral containers only exist on Pods, not controller templates.
	templateSpec := spec
	templateSpec.EphemeralContainers = nil
	template := core.PodTemplateSpec{Spec: templateSpec}
	c := fake.NewClientset(&core.Pod{ObjectMeta: m, Spec: spec}, &apps.Deployment{ObjectMeta: m, Spec: apps.DeploymentSpec{Template: template}}, &apps.StatefulSet{ObjectMeta: m, Spec: apps.StatefulSetSpec{Template: template}}, &apps.DaemonSet{ObjectMeta: m, Spec: apps.DaemonSetSpec{Template: template}}, &batch.Job{ObjectMeta: m, Spec: batch.JobSpec{Template: template}}, &batch.CronJob{ObjectMeta: m, Spec: batch.CronJobSpec{JobTemplate: batch.JobTemplateSpec{Spec: batch.JobSpec{Template: template}}}})
	w, _, e := Workloads(context.Background(), c, "n")
	if e != nil {
		t.Fatal(e)
	}
	if len(w) != 13 {
		t.Fatalf("containers: %d", len(w))
	}
	data, _ := json.Marshal(w)
	if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), "TOKEN") {
		t.Fatal("raw sensitive data leaked")
	}
	for _, a := range c.Actions() {
		if a.GetVerb() != "list" || a.GetNamespace() != "n" {
			t.Fatalf("unexpected action %v", a)
		}
	}
}

func TestWorkloadPermissionFailureIsError(t *testing.T) {
	c := fake.NewClientset()
	c.PrependReactor("list", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("forbidden") })
	if w, _, e := Workloads(context.Background(), c, ""); e == nil || w != nil {
		t.Fatal("partial scan returned as success")
	}
}

func TestPathBoundaryAndBareMount(t *testing.T) {
	for _, value := range []string{"/sys/fs/cgroup/memory", "cat /sys/fs/cgroup/memory/memory.limit_in_bytes", "cat '/sys/fs/cgroup/memory'"} {
		if len(PathSignals([]string{value})) != 1 {
			t.Fatalf("missed %s", value)
		}
	}
	if len(PathSignals([]string{"/sys/fs/cgroup/memory-pressure"})) != 0 {
		t.Fatal("matched unrelated path")
	}
	if len(PathSignals([]string{"/sys/fs/cgroup/cpu,cpuacct/cpu.stat"})) != 1 {
		t.Fatal("missed combined controller mount")
	}
}

func TestWorkloadPagination(t *testing.T) {
	c := fake.NewClientset()
	calls := 0
	c.PrependReactor("list", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		calls++
		l := &core.PodList{}
		if calls == 1 {
			l.Continue = "next"
		} else if a.(ktesting.ListActionImpl).GetListOptions().Continue != "next" {
			t.Fatal("missing pagination token")
		}
		l.Items = []core.Pod{{ObjectMeta: meta.ObjectMeta{Name: fmt.Sprint(calls), Namespace: "n"}, Spec: core.PodSpec{Containers: []core.Container{{Name: "c", Image: "node:20"}}}}}
		return true, l, nil
	})
	w, _, e := Workloads(context.Background(), c, "")
	if e != nil || len(w) != 2 || calls != 2 {
		t.Fatal(w, e, calls)
	}
}
