package apply

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amitpatole/cgroupguard/internal/audit"
	"github.com/amitpatole/cgroupguard/internal/plan"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func yes() *bool { t := true; return &t }

func node(name string, cordoned bool, marked bool) *core.Node {
	n := &core.Node{ObjectMeta: meta.ObjectMeta{Name: name}, Spec: core.NodeSpec{Unschedulable: cordoned}}
	if marked {
		n.Annotations = map[string]string{Marker: "true"}
	}
	return n
}

func pod(ns, name, nodeName, ownerKind string) *core.Pod {
	p := &core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: ns, Name: name}, Spec: core.PodSpec{NodeName: nodeName}, Status: core.PodStatus{Phase: core.PodRunning}}
	if ownerKind != "" {
		p.OwnerReferences = []meta.OwnerReference{{Kind: ownerKind, Name: "owner", Controller: yes()}}
	}
	return p
}

// evictions installs an eviction reactor. blockFirst makes the first N attempts per pod
// fail like a PodDisruptionBudget (429); pods are removed when an eviction is allowed.
func evictions(c *fake.Clientset, blockFirst int, always bool) *map[string]int {
	tries := map[string]int{}
	c.PrependReactor("create", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		ev := a.(ktesting.CreateAction).GetObject().(*policy.Eviction)
		key := ev.Namespace + "/" + ev.Name
		tries[key]++
		if always || tries[key] <= blockFirst {
			return true, nil, apierrors.NewTooManyRequests("Cannot evict pod as it would violate the pod's disruption budget.", 1)
		}
		_ = c.Tracker().Delete(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, ev.Namespace, ev.Name)
		return true, nil, nil
	})
	return &tries
}

type scriptedConfirm struct {
	answers []bool
	asked   []string
}

func (s *scriptedConfirm) Confirm(st plan.Step) (bool, error) {
	s.asked = append(s.asked, st.ID)
	if len(s.answers) == 0 {
		return false, nil
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a, nil
}

func opts(conf Confirmer) (*Options, *bytes.Buffer, *bytes.Buffer) {
	var out, log bytes.Buffer
	return &Options{Confirm: conf, Audit: audit.New(&log, nil), Out: &out, Sleep: func(time.Duration) {}, Poll: time.Millisecond}, &out, &log
}

func TestCordonMarksAndUncordonOnlyReleasesOwnCordons(t *testing.T) {
	c := fake.NewClientset(node("a", false, false), node("b", true, false), node("c", true, true))
	o, _, _ := opts(nil)
	ctx := context.Background()
	if err := setCordon(ctx, c, "a", true, o); err != nil {
		t.Fatal(err)
	}
	a, _ := c.CoreV1().Nodes().Get(ctx, "a", meta.GetOptions{})
	if !a.Spec.Unschedulable || a.Annotations[Marker] != "true" {
		t.Fatalf("%+v", a.Spec)
	}
	// Someone else's cordon is left alone and never marked.
	if err := setCordon(ctx, c, "b", true, o); err != nil {
		t.Fatal(err)
	}
	if b, _ := c.CoreV1().Nodes().Get(ctx, "b", meta.GetOptions{}); b.Annotations[Marker] != "" {
		t.Fatal("marked a node cordoned by someone else")
	}
	if err := setCordon(ctx, c, "b", false, o); err == nil {
		t.Fatal("released a node cgroupguard did not cordon")
	}
	if err := setCordon(ctx, c, "c", false, o); err != nil {
		t.Fatal(err)
	}
	if cc, _ := c.CoreV1().Nodes().Get(ctx, "c", meta.GetOptions{}); cc.Spec.Unschedulable || cc.Annotations[Marker] != "" {
		t.Fatalf("not released: %+v %v", cc.Spec, cc.Annotations)
	}
	if err := setCordon(ctx, c, "missing", true, o); err == nil {
		t.Fatal("cordoned a node that does not exist")
	}
}

func TestDrainEvictsOnlyWhatItShould(t *testing.T) {
	mirror := pod("kube-system", "etcd", "n1", "")
	mirror.Annotations = map[string]string{core.MirrorPodAnnotationKey: "x"}
	done := pod("batch", "finished", "n1", "Job")
	done.Status.Phase = core.PodSucceeded
	c := fake.NewClientset(node("n1", true, true), pod("shop", "web-1", "n1", "ReplicaSet"), pod("shop", "web-2", "n1", "StatefulSet"), pod("obs", "agent", "n1", "DaemonSet"), mirror, done, pod("shop", "elsewhere", "n2", "ReplicaSet"))
	tries := evictions(c, 0, false)
	o, _, _ := opts(nil)
	if err := drain(context.Background(), c, "n1", time.Minute, o); err != nil {
		t.Fatal(err)
	}
	if len(*tries) != 2 || (*tries)["shop/web-1"] != 1 || (*tries)["shop/web-2"] != 1 {
		t.Fatalf("evicted the wrong pods: %v", *tries)
	}
}

func TestDrainRefusesUnmanagedPodsAndUncordonedNodes(t *testing.T) {
	c := fake.NewClientset(node("n1", true, true), pod("shop", "lonely", "n1", ""), pod("shop", "web", "n1", "ReplicaSet"), node("n2", false, false))
	tries := evictions(c, 0, false)
	o, _, _ := opts(nil)
	err := drain(context.Background(), c, "n1", time.Minute, o)
	if err == nil || !strings.Contains(err.Error(), "shop/lonely") || len(*tries) != 0 {
		t.Fatalf("%v %v", err, *tries)
	}
	if err := drain(context.Background(), c, "n2", time.Minute, o); err == nil || !strings.Contains(err.Error(), "not cordoned") {
		t.Fatalf("drained a schedulable node: %v", err)
	}
}

func TestDrainWaitsForDisruptionBudgetsButNeverForces(t *testing.T) {
	c := fake.NewClientset(node("n1", true, true), pod("shop", "web-1", "n1", "ReplicaSet"))
	tries := evictions(c, 2, false)
	o, out, _ := opts(nil)
	if err := drain(context.Background(), c, "n1", time.Minute, o); err != nil {
		t.Fatal(err)
	}
	if (*tries)["shop/web-1"] != 3 || !strings.Contains(out.String(), "drained") {
		t.Fatalf("retries: %v", *tries)
	}
	// A budget that never opens ends in a clear error and the pod is still there.
	c2 := fake.NewClientset(node("n1", true, true), pod("shop", "web-1", "n1", "ReplicaSet"))
	evictions(c2, 0, true)
	err := drain(context.Background(), c2, "n1", 10*time.Millisecond, o)
	if err == nil || !strings.Contains(err.Error(), "PodDisruptionBudgets still block") || !strings.Contains(err.Error(), "never forces") {
		t.Fatalf("%v", err)
	}
	if _, e := c2.CoreV1().Pods("shop").Get(context.Background(), "web-1", meta.GetOptions{}); e != nil {
		t.Fatal("the blocked pod was removed anyway")
	}
	for _, a := range c2.Actions() {
		if a.GetVerb() == "delete" {
			t.Fatal("a direct delete was attempted instead of an eviction")
		}
	}
}

func testPlan(t *testing.T) plan.Plan {
	t.Helper()
	p := plan.Plan{SchemaVersion: plan.SchemaVersion, Target: "1.35", Groups: []plan.Group{{Provider: "eks", Pool: "old", Nodes: []string{"n1"}}},
		Phases: []plan.Phase{{ID: "P3", Steps: []plan.Step{
			{ID: "M1.1", Title: "Cordon", Kind: "command", Disruption: plan.DisruptDrain, Approval: true, Rollback: []string{"uncordon"}, Verification: []string{"v"}, Action: &plan.Action{Type: "cordon", Nodes: []string{"n1"}}},
			{ID: "M1.2", Title: "Drain", Kind: "command", Disruption: plan.DisruptDrain, Approval: true, Rollback: []string{"uncordon"}, Verification: []string{"v"}, Action: &plan.Action{Type: "drain", Nodes: []string{"n1"}, TimeoutMinutes: 5}},
			{ID: "M1.3", Title: "Delete the old group", Kind: "command", Disruption: plan.DisruptReplace, Approval: true, Rollback: []string{"cannot undo"}, Verification: []string{"v"}},
		}}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunNeedsConfirmationPerStepAndStopsWhenDeclined(t *testing.T) {
	c := fake.NewClientset(node("n1", false, false), pod("shop", "web", "n1", "ReplicaSet"))
	evictions(c, 0, false)
	conf := &scriptedConfirm{answers: []bool{true, false}}
	o, out, log := opts(conf)
	err := Run(context.Background(), c, testPlan(t), *o)
	if err != ErrDeclined {
		t.Fatalf("%v", err)
	}
	if strings.Join(conf.asked, ",") != "M1.1,M1.2" {
		t.Fatalf("asked %v", conf.asked)
	}
	n, _ := c.CoreV1().Nodes().Get(context.Background(), "n1", meta.GetOptions{})
	if !n.Spec.Unschedulable {
		t.Fatal("the confirmed cordon was not performed")
	}
	if p, _ := c.CoreV1().Pods("shop").Get(context.Background(), "web", meta.GetOptions{}); p == nil {
		t.Fatal("the declined drain evicted a pod")
	}
	if !strings.Contains(out.String(), "rollback: uncordon") {
		t.Fatalf("%s", out.String())
	}
	if n, err := audit.Verify(strings.NewReader(log.String())); err != nil || n < 4 || !strings.Contains(log.String(), `"kind":"declined"`) {
		t.Fatalf("%d %v\n%s", n, err, log.String())
	}
}

func TestRunCompletesAllSteps(t *testing.T) {
	c := fake.NewClientset(node("n1", false, false), pod("shop", "web", "n1", "ReplicaSet"))
	evictions(c, 0, false)
	conf := &scriptedConfirm{answers: []bool{true, true}}
	o, out, _ := opts(conf)
	if err := Run(context.Background(), c, testPlan(t), *o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[manual] M1.3") {
		t.Fatalf("a step apply cannot perform was not reported as manual: %s", out.String())
	}
	if left, _, _ := evictable(context.Background(), c, "n1"); len(left) != 0 {
		t.Fatal("pods remain after a completed drain")
	}
}

func TestRunRefusesBadPlansAndMissingConfirmer(t *testing.T) {
	c := fake.NewClientset(node("n1", false, false))
	o, _, _ := opts(nil)
	if err := Run(context.Background(), c, testPlan(t), *o); err == nil {
		t.Fatal("a real run started without a confirmer")
	}
	bad := testPlan(t)
	bad.Phases[0].Steps[0].Action.Nodes = []string{"someone-elses-node"}
	conf := &scriptedConfirm{answers: []bool{true}}
	o2, _, _ := opts(conf)
	if err := Run(context.Background(), c, bad, *o2); err == nil || len(conf.asked) != 0 {
		t.Fatalf("applied an action on a node outside the plan groups: %v asked=%v", err, conf.asked)
	}
	invalid := testPlan(t)
	invalid.Phases[0].Steps[0].Action.Type = "delete-node"
	if err := Run(context.Background(), c, invalid, *o2); err == nil {
		t.Fatal("applied an unknown action type")
	}
	invalid2 := testPlan(t)
	invalid2.Phases[0].Steps[0].Action.Nodes = []string{"x; reboot"}
	if err := Run(context.Background(), c, invalid2, *o2); err == nil {
		t.Fatal("applied an action with an unsafe node name")
	}
}

func TestDryRunNeverAsksForConfirmation(t *testing.T) {
	c := fake.NewClientset(node("n1", false, false), pod("shop", "web", "n1", "ReplicaSet"))
	evictions(c, 0, false)
	conf := &scriptedConfirm{}
	o, out, _ := opts(conf)
	o.DryRun = true
	if err := Run(context.Background(), c, testPlan(t), *o); err != nil {
		t.Fatal(err)
	}
	if len(conf.asked) != 0 || !strings.Contains(out.String(), "DRY RUN") {
		t.Fatalf("asked=%v out=%s", conf.asked, out.String())
	}
}

var _ = fmt.Sprint
